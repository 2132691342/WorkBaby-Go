package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"WorkBaby/internal/capability"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/llm/registry"
	"WorkBaby/internal/memory"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/runtime"
	"WorkBaby/internal/tool"
)

// ChatService 会话 + 消息编排；接入 harness.Runner 调用真实 LLM。
type ChatService struct {
	sessions    *repo.ChatSessionRepo
	messages    *repo.MessageRepo
	provRepo    *repo.AiProviderRepo
	setRepo     *repo.SystemSettingRepo
	usages      *repo.TokenUsageRepo // token 明细（仪表盘三线图的唯一数据源）
	bus         *event.Bus
	reg         *registry.Registry
	tools       *ToolService
	mem         *memory.Service            // 上下文占用统计（/context 分段）读取长期记忆
	trust       *TrustService              // 目录信任；nil = 关闭
	mu          sync.Mutex                 // 串行化 session seq 自增与并发 run 拒绝
	runs        *runRegistry               // 活动 run 注册中心（按 sessionID → cancel）；用于前端「停止」按钮
	checkpoints harness.CheckpointStore    // 检查点存储（SQL 默认；nil = 关闭）
	events      *event.RunEventLog         // run 事件日志：分配序号 + 缓存，供 SSE 断线重放
	execs       *harness.ExecutionRegistry // 执行平面
	blocks      *repo.MessageBlockRepo     // 消息块持久化；nil = 不落块
	approval    *ApprovalService           // 工具策略门 ask 决策的人工审批；nil = 策略门不启用
	steers      *steerQueue                // run 中用户新消息的注入队列（steering / follow-up）
	seqMu       sync.Mutex                 // 保护 seqs
	seqs        map[string]int64           // 会话消息序号分配水位（工具消息与注入消息统一分配，防撞号）
	dataHome    string                     // 数据根（paths.Home）；目录策略默认根由此派生
	caps        *capability.Registry       // 能力注册表：上下文装配 / 工具暴露 / run 后沉淀三条通道
}

// memoryCaptureTimeout run 后沉淀（记忆形成等）的独立超时。
const memoryCaptureTimeout = 30 * time.Second

// WithCapabilities 接入能力注册表；未接入时上下文装配与沉淀均为空操作。
func (s *ChatService) WithCapabilities(caps *capability.Registry) *ChatService {
	s.caps = caps
	return s
}

// WithExecutionRegistry 启用执行平面：登记 run 的 scope/state，供统一执行拓扑查询。
func (s *ChatService) WithExecutionRegistry(reg *harness.ExecutionRegistry) *ChatService {
	s.execs = reg
	return s
}

// finishExec 幂等标记执行终态（执行平面）。
func (s *ChatService) finishExec(runID string, state harness.ExecutionState) {
	if s.execs != nil {
		s.execs.Finish(runID, state)
	}
}

// execState 把 Runner 终止原因映射为执行状态。
func execState(reason harness.RunStopReason) harness.ExecutionState {
	switch reason {
	case harness.ReasonCancelled:
		return harness.StateCancelled
	case harness.ReasonError:
		return harness.StateFailed
	default: // end_turn / stagnation / max_turns / budget_exceeded 均视为正常终态
		return harness.StateCompleted
	}
}

// WithCheckpointStore 启用 harness 检查点（SQL 默认，C2）；nil = 关闭。
func (s *ChatService) WithCheckpointStore(store harness.CheckpointStore) *ChatService {
	s.checkpoints = store
	return s
}

// WithEventLog 启用 run 事件日志：每条事件分配单调序号并缓存，SSE 断线可重放。
func (s *ChatService) WithEventLog(log *event.RunEventLog) *ChatService { s.events = log; return s }

// WithMessageBlocks 启用消息块持久化：工具调用/结果/产物随事件落 message_blocks，
// 刷新或切会话后历史消息可复现完整工具过程。
func (s *ChatService) WithMessageBlocks(r *repo.MessageBlockRepo) *ChatService {
	s.blocks = r
	return s
}

// WithApprovalService 注入审批服务：工具策略门 ask 决策经 chat:approval 事件等用户回执。
func (s *ChatService) WithApprovalService(a *ApprovalService) *ChatService { s.approval = a; return s }

// WithTrustService 注入目录信任；仅在 harness 装配 PathTrust 时才生效。
func (s *ChatService) WithTrustService(t *TrustService) *ChatService { s.trust = t; return s }

// WithDataHome 注入数据根（paths.Home）；目录策略（记忆/快照默认根）由此派生。
// 必须在装配会话级目录解析闭包前调用。
func (s *ChatService) WithDataHome(home string) *ChatService {
	s.dataHome = home
	return s
}

// memoryEnabled 全局记忆开关：system_settings memory.enabled（默认 true；空值按 true）。
func (s *ChatService) memoryEnabled(ctx context.Context) bool {
	if s.setRepo == nil {
		return true
	}
	row, err := s.setRepo.Get(ctx, domain.SettingKeyMemoryEnabled)
	if err != nil || row == nil {
		return true
	}
	return strings.TrimSpace(row.V) != "false"
}

// MemoryEnabled 记忆全局开关（能力装配方回调用）。
func (s *ChatService) MemoryEnabled(ctx context.Context) bool { return s.memoryEnabled(ctx) }

// SessionDataDirs 某会话的目录布局（目录策略唯一数据源）：
//
//	绑定本地工作区 {dir}：
//	  memory   → {dir}/.workbaby/memory/{sessionID}/MEMORY.md
//	  snapshot → {dir}/.workbaby/snapshots/{sessionID}
//	默认工作区（未绑定）：
//	  memory   → {dataHome}/memory/{sessionID}/MEMORY.md
//	  snapshot → {dataHome}/snapshots/{sessionID}
//
// 会话记忆与写前快照是与该工作区强绑定的「过程数据」，跟工作区走（如同 .git）；
// 系统级数据（知识库 / 日志 / 全局记忆）不受影响，始终在用户数据目录。
// 目录按需创建（只有真正写记忆 / 快照时才建），绑定工作区不预建空目录树。
func (s *ChatService) SessionDataDirs(ctx context.Context, sessionID string) (memoryFile, snapshotDir string) {
	home := s.dataHome
	if home == "" {
		home = "."
	}
	memoryFile = filepath.Join(home, "memory", sessionID, "MEMORY.md")
	snapshotDir = filepath.Join(home, "snapshots", sessionID)
	if sessionID == "" || s.sessions == nil {
		return
	}
	row, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil || row == nil {
		return
	}
	if wp := strings.TrimSpace(row.WorkspacePath); wp != "" {
		sb := runtime.SandboxOf(wp)
		memoryFile = filepath.Join(sb.Memory, sessionID, "MEMORY.md")
		snapshotDir = filepath.Join(sb.Snapshots, sessionID)
	}
	return
}

// gateInternalAllowTools 工具策略门显式放行清单：内部自带 fail-closed 审批（exec / skill 命令级门）
// 或纯只读/信息型工具，避免与命令级审批双重弹窗；其余工具按 SessionMode × 风险默认裁决。
var gateInternalAllowTools = []string{
	"exec", "run_skill_script", "delegate_task", // 命令级白名单/危险正则 + 审批已在工具内 fail-closed
	"websearch", "webfetch", "http", // 信息型网络读取
	"knowledge_search", "doc_reader", "todo", // 只读 / 会话内计划
}

// turnAdjuster 自动降级策略：本轮 LLM 建流失败 → 切到 chat.fallback_model 重试。
// 仅在流式失败时触发；未配置或与当前模型相同时不切换；切换次数由 harness 侧熔断（StagnationLimit）。
// 备用模型须与主模型同 Provider（跨 Provider 切换需要连带切换凭据/参数，v2 再做）。
func (s *ChatService) turnAdjuster(ctx context.Context) harness.TurnAdjuster {
	fallback := s.fallbackModel(ctx)
	return func(_ context.Context, sig *harness.TurnSignal) harness.TurnUpdate {
		if fallback == "" || sig == nil || sig.StreamErr == nil {
			return harness.TurnUpdate{}
		}
		m := fallback
		return harness.TurnUpdate{Model: &m}
	}
}

// fallbackModel 读 chat.fallback_model 设置；未配置返回空。
func (s *ChatService) fallbackModel(ctx context.Context) string {
	if s.setRepo == nil {
		return ""
	}
	if row, err := s.setRepo.Get(ctx, domain.SettingKeyChatFallbackModel); err == nil && row != nil {
		return strings.TrimSpace(row.V)
	}
	return ""
}

// toolGate 构造工具策略门；每次 run 重建，改模式即下轮生效。
// 优先级：会话级 permission_mode（前端顶栏切换）→ system_settings 全局默认 → default。
func (s *ChatService) toolGate(ctx context.Context, sessionMode string) *tool.Gate {
	if s.approval == nil {
		return nil
	}
	mode := tool.SessionModeDefault
	if v := strings.TrimSpace(sessionMode); v != "" {
		mode = tool.ParseSessionMode(v)
	} else if s.setRepo != nil {
		if row, err := s.setRepo.Get(ctx, domain.SettingKeyAgentSessionMode); err == nil && row != nil {
			mode = tool.ParseSessionMode(row.V)
		}
	}
	g := tool.NewGate(mode)
	for _, n := range gateInternalAllowTools {
		g.Allow(n)
	}
	return g
}

// pathTrust 工具执行前的目录信任闸门。
//
// 只有少数工具真的需要目标目录信任：exec 的 cwd 是工作目录选择器，
// file_* / doc_reader 已被工具内 safePath 沙箱到工作区根、archive 操作 workspace 内路径。
// 空 cwd（执行用进程默认目录）默认放行——避免每次 exec 都问「你信任 app 目录吗」。
func (s *ChatService) pathTrust() harness.PathTrust {
	if s.trust == nil {
		return nil
	}
	return func(ctx context.Context, toolName string, args json.RawMessage) (bool, string) {
		dir, ok := extractTargetDir(toolName, args)
		if !ok || strings.TrimSpace(dir) == "" {
			return true, ""
		}
		return s.trust.Ensure(ctx, dir)
	}
}

// extractTargetDir 从工具入参抽取目标目录。
//
// 返回 (dir, ok)；ok=false 表示该工具与目录无关，调用方应跳过信任检查。
func extractTargetDir(toolName string, args json.RawMessage) (string, bool) {
	switch toolName {
	case "exec":
		// cwd 优先；为空 → 走进程默认目录（app home），跳过信任检查
		var v struct {
			Cwd string `json:"cwd"`
		}
		if err := json.Unmarshal(args, &v); err != nil {
			return "", true
		}
		return v.Cwd, true
	case "file_read", "file_write", "file_list", "doc_reader":
		// 这些工具已被工作区根沙箱约束，目录信任在工具内部处理
		return "", false
	case "archive":
		// archive 的 source/target 是工作区相对路径，不涉及外部目录
		return "", false
	default:
		return "", false
	}
}

// todoToolName 计划工具名；其结构化产出单独发 chat:todo 事件（前端进度卡）。
const todoToolName = "todo"

// emit 发布 run 事件：注入 run_id/session_id、经事件日志分配 seq，再广播到总线。
// 载荷字段一律 snake_case（CLAUDE.md §2.5）。
func (s *ChatService) emit(runID, sessionID, name string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["run_id"] = runID
	payload["session_id"] = sessionID
	if s.events != nil {
		s.events.Append(runID, name, payload)
	}
	s.bus.Publish(name, payload)
}

// NewChatService 注入 repo、Registry、工具、记忆与 Skill 服务。
func NewChatService(sessions *repo.ChatSessionRepo, messages *repo.MessageRepo, provRepo *repo.AiProviderRepo, setRepo *repo.SystemSettingRepo, usages *repo.TokenUsageRepo, bus *event.Bus, reg *registry.Registry, tools *ToolService, mem *memory.Service) *ChatService {
	return &ChatService{
		sessions: sessions, messages: messages, provRepo: provRepo, setRepo: setRepo, usages: usages,
		bus: bus, reg: reg, tools: tools, mem: mem, runs: newRunRegistry(),
		steers: newSteerQueue(), seqs: map[string]int64{},
	}
}

// CancelStream 中断指定 session 正在跑的 run（前端「停止」按钮）。
//
// 幂等：无 run 或 sessionID 为空直接 nil；harness 收到 ctx 取消后 emit ReasonCancelled → chat:done。
func (s *ChatService) CancelStream(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	s.runs.cancel(sessionID)
	s.steers.clear(sessionID) // run 已终止，排队中的注入消息一并作废
	return nil
}

func (s *ChatService) ListSessions(ctx context.Context, page, pageSize int) (*domain.SessionListRESP, error) {
	total, err := s.sessions.CountByUser(ctx, domain.LocalUserID)
	if err != nil {
		return nil, err
	}
	rows, err := s.sessions.ListByUserPage(ctx, domain.LocalUserID, page, pageSize)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ChatSessionRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toSessionRESP(&rows[i]))
	}
	return &domain.SessionListRESP{Items: out, Total: total}, nil
}

func toSessionRESP(s *domain.ChatSessionDO) domain.ChatSessionRESP {
	return domain.ChatSessionRESP{
		ID:             s.ID,
		Name:           s.Name,
		UserID:         s.UserID,
		ProviderID:     s.ProviderID,
		Model:          s.Model,
		WorkspaceID:    s.WorkspaceID,
		WorkspacePath:  s.WorkspacePath,
		Active:         s.Status == domain.SessionStatusActive,
		MessageCount:   s.MessageCount,
		LastMessageAt:  s.LastMessageAt,
		MetadataJSON:   s.MetadataJSON,
		Status:         s.Status,
		PermissionMode: s.PermissionMode,
		ParentID:       s.ParentID,
		BranchPoint:    s.BranchPoint,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}

func toMessageRESP(m *domain.MessageDO) domain.MessageRESP {
	return domain.MessageRESP{
		ID:           m.ID,
		SessionID:    m.SessionID,
		RunID:        m.RunID,
		Role:         m.Role,
		Content:      m.Content,
		Thinking:     m.Thinking,
		ToolCallID:   m.ToolCallID,
		ToolCalls:    m.ToolCalls,
		Status:       m.Status,
		StopReason:   m.StopReason,
		Model:        m.Model,
		InputTokens:  m.InputTokens,
		OutputTokens: m.OutputTokens,
		CacheRead:    m.CacheRead,
		TotalTokens:  m.TotalTokens,
		LatencyMs:    m.LatencyMs,
		Cost:         m.Cost,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

// CreateSession 新建会话。
//
// provider_id/model 为空时回退到第一个 enabled Provider（避免会话创建后因缺模型立即 5003）。
// 自动解析：用户在前端只看到「模型」（如 gpt-4o / claude-sonnet-4-5），不知道具体 provider id；
// 这里用 provider_id 为空 + model 非空 的情况下，从 ai_providers 表查 model 字段精确匹配；
// 都找不到则取第一个 enabled 的 provider。
// workspace_path 非空时创建即绑定（校验 + 信任登记），保证「选目录 → 建会话」一次完成不丢动作。
func (s *ChatService) CreateSession(ctx context.Context, req *domain.ChatSessionREQ) (*domain.ChatSessionRESP, error) {
	wp, err := s.validateWorkspace(ctx, req.WorkspacePath)
	if err != nil {
		return nil, err
	}
	row := &domain.ChatSessionDO{
		ID:            pkg.NewID(domain.IDSession),
		Name:          req.Name,
		UserID:        domain.LocalUserID,
		ProviderID:    req.ProviderID,
		Model:         req.Model,
		WorkspaceID:   req.WorkspaceID,
		WorkspacePath: wp,
		Status:        domain.SessionStatusActive,
	}
	if row.Name == "" {
		row.Name = DefaultSessionName
	}
	// 兜底：缺 provider_id / model 时自动选一个 enabled 的 Provider（聊天可用）
	if row.ProviderID == "" || row.Model == "" {
		if pid, model, ok := s.resolveDefaultProviderModel(ctx, row.Model); ok {
			row.ProviderID = pid
			row.Model = model
		}
	}
	if err := s.sessions.Create(ctx, row); err != nil {
		return nil, err
	}
	r := toSessionRESP(row)
	return &r, nil
}

// resolveDefaultProviderModel 选择一个 enabled 的 Provider 与模型：
//   - 优先按 row.Model 精确匹配 ai_providers.model 字段（前端只传 model 时回查 provider）
//   - 退路：取第一个 enabled 的 Provider
//
// 返回 (providerID, model, found)；found=false 时调用方应继续走 5003 错误（用户根本没配 Provider）。
func (s *ChatService) resolveDefaultProviderModel(ctx context.Context, modelHint string) (string, string, bool) {
	if s.provRepo == nil {
		return "", "", false
	}
	all, err := s.provRepo.List(ctx)
	if err != nil || len(all) == 0 {
		return "", "", false
	}
	// 1) model 精确匹配（trim 后比较；优先 tier1）
	if modelHint != "" {
		hint := strings.TrimSpace(modelHint)
		var hit *domain.AiProviderDO
		for i := range all {
			p := all[i]
			if !p.Enabled {
				continue
			}
			if strings.TrimSpace(p.Model) == hint {
				if hit == nil || p.Tier < hit.Tier {
					hit = &p
				}
			}
		}
		if hit != nil {
			return hit.ID, hit.Model, true
		}
	}
	// 2) 退路：第一个 enabled（按 created_at ASC）
	for i := range all {
		if all[i].Enabled {
			return all[i].ID, all[i].Model, true
		}
	}
	return "", "", false
}

func (s *ChatService) GetSession(ctx context.Context, id string) (*domain.ChatSessionRESP, error) {
	row, err := s.sessions.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	r := toSessionRESP(row)
	return &r, nil
}

func (s *ChatService) RenameSession(ctx context.Context, id, name string) (*domain.ChatSessionRESP, error) {
	row, err := s.sessions.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	row.Name = name
	if err := s.sessions.Update(ctx, row); err != nil {
		return nil, err
	}
	r := toSessionRESP(row)
	return &r, nil
}

// SetSessionPermission 切换会话工具权限模式（restricted/default/auto_edit/yolo；空 = 跟随全局设置）。
// 归一化在 tool 层做，未知值回落 default，避免脏数据意外升权。
func (s *ChatService) SetSessionPermission(ctx context.Context, id, mode string) (*domain.ChatSessionRESP, error) {
	row, err := s.sessions.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(mode); v == "" {
		row.PermissionMode = ""
	} else {
		row.PermissionMode = string(tool.ParseSessionMode(v))
	}
	if err := s.sessions.Update(ctx, row); err != nil {
		return nil, err
	}
	r := toSessionRESP(row)
	return &r, nil
}

// UpdateWorkspace 绑定/解绑会话的外部工作目录。
//
// 绑定即授权：目录经对话框显式选择，登记为 allow 信任（后续工具不再为它弹审批）。
// 解绑（空路径）只清字段、不撤销已有信任登记——用户可能还要继续在原目录工作。
func (s *ChatService) UpdateWorkspace(ctx context.Context, id, workspacePath string) (*domain.ChatSessionRESP, error) {
	row, err := s.sessions.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	p, err := s.validateWorkspace(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	row.WorkspacePath = p
	if err := s.sessions.Update(ctx, row); err != nil {
		return nil, err
	}
	r := toSessionRESP(row)
	return &r, nil
}

// validateWorkspace 校验并规范化外部工作目录（空 = 未绑定，返回空串）。
// 非空时：NormalizeDir → 必须是真实存在的目录 → 登记 allow 信任（绑定即授权）。
// CreateSession 与 UpdateWorkspace 共用，保证两条绑定路径行为一致。
func (s *ChatService) validateWorkspace(ctx context.Context, workspacePath string) (string, error) {
	p := strings.TrimSpace(workspacePath)
	if p == "" {
		return "", nil
	}
	dir, derr := NormalizeDir(p)
	if derr != nil {
		return "", derr
	}
	info, serr := os.Stat(dir)
	if serr != nil || !info.IsDir() {
		return "", pkg.New(1021, "workspace directory does not exist", dir)
	}
	if s.trust != nil {
		if _, terr := s.trust.Decide(ctx, domain.WorkspaceTrustREQ{Path: dir, State: string(domain.TrustStateAllow)}); terr != nil {
			pkg.L.Warn("auto trust workspace failed", "path", dir, "err", terr.Error())
		}
	}
	return dir, nil
}

// WorkspaceRoot 某会话工具链的工作区根：绑定了外部目录用外部目录，否则用默认根。
// handler 装配期把它包成 tool.RootResolver 注入文件类工具；默认根为空时返回空串
// （由 tool.ResolveRoot 的调用方兜底），避免把「未配置」误判成「当前目录」。
func (s *ChatService) WorkspaceRoot(ctx context.Context, sessionID, defRoot string) string {
	if sessionID == "" {
		return defRoot
	}
	row, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil || row == nil {
		if err != nil {
			pkg.L.Warn("workspace root resolve failed, fallback to default", "session", sessionID, "err", err.Error())
		}
		return defRoot
	}
	if p := strings.TrimSpace(row.WorkspacePath); p != "" {
		return p
	}
	return defRoot
}

// UpdateSessionModel 切换会话使用的 Provider/模型（问题2：聊天输入框的思考强度/温度展示跟随所选模型）。
// 规则：ProviderID 非空时以它为准（校验 enabled）；否则按 Model 名字回查 provider；
// 两者都未命中返回 3003 便于前端提示「模型不可用」。
func (s *ChatService) UpdateSessionModel(ctx context.Context, id string, req *domain.ChatSessionModelREQ) (*domain.ChatSessionRESP, error) {
	row, err := s.sessions.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.TrimSpace(req.ProviderID) != "":
		p, perr := s.provRepo.GetByID(ctx, req.ProviderID)
		if perr != nil || p == nil || !p.Enabled {
			return nil, domain.ErrProviderNotReady
		}
		row.ProviderID = p.ID
		if strings.TrimSpace(req.Model) != "" {
			row.Model = req.Model
		} else {
			row.Model = p.Model
		}
	default:
		if pid, model, ok := s.resolveDefaultProviderModel(ctx, req.Model); ok {
			row.ProviderID = pid
			row.Model = model
		} else {
			return nil, domain.ErrProviderNotReady
		}
	}
	if err := s.sessions.Update(ctx, row); err != nil {
		return nil, err
	}
	r := toSessionRESP(row)
	return &r, nil
}

func (s *ChatService) DeleteSession(ctx context.Context, id string) error {
	return s.sessions.SoftDelete(ctx, id, time.Now().UnixMilli())
}

// DeleteSessions 批量软删。
func (s *ChatService) DeleteSessions(ctx context.Context, ids []string) (ok, failed []string, err error) {
	for _, id := range ids {
		if e := s.DeleteSession(ctx, id); e != nil {
			failed = append(failed, id)
			continue
		}
		ok = append(ok, id)
	}
	return ok, failed, nil
}

func (s *ChatService) ClearMessages(ctx context.Context, sessionID string) error {
	if s.blocks != nil {
		_ = s.blocks.DeleteBySession(ctx, sessionID) // 块级联清理；失败不阻断消息清空
	}
	return s.messages.DeleteAll(ctx, sessionID)
}

// DeleteMessage 删除单条消息；校验归属防止跨会话误删。
func (s *ChatService) DeleteMessage(ctx context.Context, sessionID, messageID string) error {
	m, err := s.messages.GetByID(ctx, messageID)
	if err != nil {
		return err
	}
	if m.SessionID != sessionID {
		return domain.ErrMessageNotInSession
	}
	if s.blocks != nil {
		_ = s.blocks.DeleteByMessage(ctx, messageID)
	}
	return s.messages.DeleteByID(ctx, messageID)
}

// TruncateMessages 从指定消息起（含）截断该会话后续消息，用于「从此处重新生成」。
func (s *ChatService) TruncateMessages(ctx context.Context, sessionID, messageID string) error {
	m, err := s.messages.GetByID(ctx, messageID)
	if err != nil {
		return err
	}
	if m.SessionID != sessionID {
		return domain.ErrMessageNotInSession
	}
	victims, lerr := s.messages.ListBySession(ctx, sessionID, m.Seq-1, 0)
	if lerr != nil {
		return lerr
	}
	if err := s.messages.DeleteFromSeq(ctx, sessionID, m.Seq); err != nil {
		return err
	}
	if s.blocks != nil {
		for i := range victims {
			_ = s.blocks.DeleteByMessage(ctx, victims[i].ID)
		}
	}
	return nil
}

// ForkSession 从指定消息处分叉：新建会话并复制 ≤ 该 seq 的全部消息。
// 复制体重置 run 关联（RunID 留空），避免新会话继承旧 run 的审批/取消语义。
//
// 分叉血缘写进 ParentID / BranchPoint：分支记住「从哪个会话的哪一条消息长出来」，
// 前端据此把会话组织成树而不是平铺列表。分叉可递归（分支再分叉），不额外记 root。
func (s *ChatService) ForkSession(ctx context.Context, sessionID, messageID string, name string) (*domain.ChatSessionRESP, error) {
	ses, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	at, err := s.messages.GetByID(ctx, messageID)
	if err != nil {
		return nil, err
	}
	if at.SessionID != sessionID {
		return nil, domain.ErrMessageNotInSession
	}
	src, err := s.messages.ListUpTo(ctx, sessionID, at.Seq)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = ses.Name + "（分叉）"
	}
	newSes := &domain.ChatSessionDO{
		ID:            pkg.NewID(domain.IDSession),
		Name:          name,
		UserID:        ses.UserID,
		ProviderID:    ses.ProviderID,
		Model:         ses.Model,
		WorkspaceID:   ses.WorkspaceID,
		Status:        domain.SessionStatusActive,
		MessageCount:  len(src),
		LastMessageAt: time.Now().UnixMilli(),
		ParentID:      ses.ID,
		BranchPoint:   at.Seq,
	}
	if err := s.sessions.Create(ctx, newSes); err != nil {
		return nil, err
	}
	for i := range src {
		cp := src[i]
		cp.ID = pkg.NewID(domain.IDMessage)
		cp.SessionID = newSes.ID
		cp.RunID = ""
		if err := s.messages.Insert(ctx, &cp); err != nil {
			return nil, err
		}
		// 过程块随消息复制：分叉会话保留完整工具过程
		if s.blocks != nil {
			srcBlocks, berr := s.blocks.ListByMessage(ctx, src[i].ID)
			if berr != nil {
				return nil, berr
			}
			for _, b := range srcBlocks {
				b.ID = pkg.NewID(domain.IDMessageBlock)
				b.MessageID = cp.ID
				b.SessionID = newSes.ID
				if berr := s.blocks.Create(ctx, &b); berr != nil {
					return nil, berr
				}
			}
		}
	}
	r := toSessionRESP(newSes)
	return &r, nil
}

// ListMessages 增量分页；assistant 消息附带持久化过程块（单次批量查询防 N+1）。
func (s *ChatService) ListMessages(ctx context.Context, sessionID string, afterSeq int64, limit int) (*domain.MessageListRESP, error) {
	rows, err := s.messages.ListBySession(ctx, sessionID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	blockMap, err := s.blocksByMessage(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := &domain.MessageListRESP{
		Items:   make([]domain.MessageRESP, 0, len(rows)),
		Total:   len(rows),
		NextSeq: -1,
	}
	var maxSeq int64
	for i := range rows {
		item := toMessageRESP(&rows[i])
		if bs, ok := blockMap[item.ID]; ok {
			item.Blocks = bs
		}
		out.Items = append(out.Items, item)
		if rows[i].Seq > maxSeq {
			maxSeq = rows[i].Seq
		}
	}
	if len(rows) > 0 {
		out.NextSeq = maxSeq + 1
	}
	return out, nil
}

// blocksByMessage 会话级批量取块并按 message_id 分组；未启用块持久化时返回 nil。
func (s *ChatService) blocksByMessage(ctx context.Context, sessionID string) (map[string][]domain.MessageBlockRESP, error) {
	if s.blocks == nil {
		return nil, nil
	}
	rows, err := s.blocks.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]domain.MessageBlockRESP, len(rows))
	for i := range rows {
		out[rows[i].MessageID] = append(out[rows[i].MessageID], domain.MessageBlockRESP{
			ID:        rows[i].ID,
			MessageID: rows[i].MessageID,
			Seq:       rows[i].Seq,
			Kind:      rows[i].Kind,
			Payload:   rows[i].Payload,
			CreatedAt: rows[i].CreatedAt,
		})
	}
	return out, nil
}

// SendStream 立即返回 runID/消息 ID；流式事件经 harness → event.Bus → api 层 → chat:* 推前端。
// params 为请求级采样参数（温度/思考开关；零值 = 不覆盖）。
func (s *ChatService) SendStream(ctx context.Context, sessionID, content string, params harness.RequestParams) (*domain.SendStreamResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ses, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	// 兜底：历史会话可能缺 provider/model（首次启动升级 / 旧数据），运行时补一个默认 Provider
	if ses.ProviderID == "" || ses.Model == "" {
		if pid, model, ok := s.resolveDefaultProviderModel(ctx, ses.Model); ok {
			ses.ProviderID = pid
			ses.Model = model
			_ = s.sessions.Update(ctx, ses)
		} else {
			return nil, pkg.Wrap(domain.ErrSessionInvalid.Code, domain.ErrSessionInvalid.Message, domain.ErrSessionInvalid)
		}
	}

	// v1：chat 固定走 default Agent；前端 Agent 选择器待 P3 接入。
	// 首条用户消息自动命名（无历史消息可推导时），让会话在列表里可辨认。
	firstTurn := ses.MessageCount == 0
	ids, err := s.prepareRun(ctx, ses, content, harness.ScopeChatTurn, defaultAgentName)
	if err != nil {
		return nil, err
	}
	if firstTurn {
		s.autoTitleSession(ctx, ses, content)
	}

	// 异步跑；用可取消 ctx（前端「停止」→ CancelStream 触发）
	runCtx, cancel := context.WithCancel(context.Background())
	s.runs.set(ses.ID, ids.RunID, cancel)
	go func() {
		defer s.runs.delete(ses.ID)
		s.runLLM(runCtx, ses, ids.RunID, ids.AssistantMsgID, content, params, harness.Agent(defaultAgentName), false)
	}()

	return &domain.SendStreamResult{
		RunID:          ids.RunID,
		SessionID:      sessionID,
		UserMsgID:      ids.UserMsgID,
		AssistantMsgID: ids.AssistantMsgID,
	}, nil
}

// defaultAgentName chat 主入口的默认 Agent。
const defaultAgentName = "default"

// runIDs 一次 run 的身份（runID + user/assistant 消息 ID）。
type runIDs struct {
	RunID          string
	UserMsgID      string
	AssistantMsgID string
}

// prepareRun 落 user + assistant 占位消息并登记执行平面 run。
//
// chat / 后台任务共用：同一会话串行落 seq，run 身份在消息落库前就已确定
// （runID 需先写入消息行，供前端按 run 过滤事件）。调用方负责持 s.mu。
func (s *ChatService) prepareRun(ctx context.Context, ses *domain.ChatSessionDO, content string, scope harness.Scope, agentName string) (runIDs, error) {
	seqStart, err := s.allocSeq(ctx, ses.ID, 2)
	if err != nil {
		return runIDs{}, err
	}

	runID := pkg.NewID("RUN")
	if s.execs != nil {
		s.execs.Register(&harness.ExecutionRun{
			RunID:     runID,
			SessionID: ses.ID,
			Scope:     scope,
			AgentName: agentName,
			State:     harness.StateRunning,
		})
	}
	now := time.Now().UnixMilli()
	userMsg := &domain.MessageDO{
		ID:        pkg.NewID(domain.IDMessage),
		SessionID: ses.ID,
		Seq:       seqStart,
		RunID:     runID,
		Role:      domain.MessageRoleUser,
		Content:   content,
		Status:    domain.MessageStatusCompleted,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.messages.Insert(ctx, userMsg); err != nil {
		return runIDs{}, err
	}
	assistantMsg := &domain.MessageDO{
		ID:        pkg.NewID(domain.IDMessage),
		SessionID: ses.ID,
		Seq:       seqStart + 1,
		RunID:     runID,
		Role:      domain.MessageRoleAssistant,
		Status:    domain.MessageStatusStreaming,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.messages.Insert(ctx, assistantMsg); err != nil {
		return runIDs{}, err
	}
	ses.MessageCount += 2
	ses.LastMessageAt = now
	_ = s.sessions.Update(ctx, ses)
	return runIDs{RunID: runID, UserMsgID: userMsg.ID, AssistantMsgID: assistantMsg.ID}, nil
}

// nextSeq 会话下一条消息序号（数据库当前最大值 + 1）。
func (s *ChatService) nextSeq(ctx context.Context, sessionID string) (int64, error) {
	existing, err := s.messages.ListBySession(ctx, sessionID, 0, 0)
	if err != nil {
		return 0, err
	}
	var next int64 = 1
	for i := range existing {
		if existing[i].Seq >= next {
			next = existing[i].Seq + 1
		}
	}
	return next, nil
}

// allocSeq 分配会话内连续 n 个消息序号（进程内单调水位，首次取数据库最大值 + 1）。
//
// 运行中的工具结果消息与 steering 注入消息都会落库，必须走同一分配器：
// 序号撞号会让增量分页（after_seq 游标）漏消息。
func (s *ChatService) allocSeq(ctx context.Context, sessionID string, n int64) (int64, error) {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	start, ok := s.seqs[sessionID]
	if !ok {
		dbSeq, err := s.nextSeq(ctx, sessionID)
		if err != nil {
			return 0, err
		}
		start = dbSeq
	}
	s.seqs[sessionID] = start + n
	return start, nil
}

// QueueSteer run 进行中插入一条用户指令（注入缝）：
//   - steering：本轮工具跑完后、下一轮前注入 → 长任务中途纠偏；
//   - follow-up：模型说完了（本轮无工具调用）后注入 → run 自动续接下一波。
//
// 消息立即落库（前端可见），内容进注入队列由 harness 消费；
// 会话当前没有活动 run 时返回 ErrRunNotActive，前端退回普通发送。
func (s *ChatService) QueueSteer(ctx context.Context, sessionID, content string) (*domain.SteerResultRESP, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, domain.ErrMessageInvalid
	}
	runID, ok := s.runs.lookup(sessionID)
	if !ok {
		return nil, domain.ErrRunNotActive
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ses, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	seq, err := s.allocSeq(ctx, sessionID, 1)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	msg := &domain.MessageDO{
		ID:        pkg.NewID(domain.IDMessage),
		SessionID: sessionID,
		Seq:       seq,
		RunID:     runID,
		Role:      domain.MessageRoleUser,
		Content:   content,
		Status:    domain.MessageStatusCompleted,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.messages.Insert(ctx, msg); err != nil {
		return nil, err
	}
	ses.MessageCount++
	ses.LastMessageAt = now
	_ = s.sessions.Update(ctx, ses)

	s.steers.push(sessionID, llm.UserMessage(content))
	s.emit(runID, sessionID, "chat:steer", map[string]any{
		"message_id": msg.ID,
		"seq":        seq,
		"role":       string(domain.MessageRoleUser),
		"content":    content,
	})
	return &domain.SteerResultRESP{RunID: runID, SessionID: sessionID, MessageID: msg.ID, Queued: true}, nil
}

// RunAgent 同步跑一轮 Agent（后台任务入口； 后台任务队列）。
//
// 与 SendStream 的区别：不占用会话的活动 run 槽（后台任务与前台聊天可并行），
// 阻塞直到 run 结束并返回结果摘要。
func (s *ChatService) RunAgent(ctx context.Context, sessionID, userInput, agentName string) (*AgentRunOutcome, error) {
	s.mu.Lock()
	ses, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if ses.ProviderID == "" || ses.Model == "" {
		if pid, model, ok := s.resolveDefaultProviderModel(ctx, ses.Model); ok {
			ses.ProviderID = pid
			ses.Model = model
			_ = s.sessions.Update(ctx, ses)
		} else {
			s.mu.Unlock()
			return nil, pkg.Wrap(domain.ErrSessionInvalid.Code, domain.ErrSessionInvalid.Message, domain.ErrSessionInvalid)
		}
	}
	if agentName == "" {
		agentName = defaultAgentName
	}
	ids, err := s.prepareRun(ctx, ses, userInput, harness.ScopeTask, agentName)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	def := harness.Agent(agentName)
	res := s.executeAgent(ctx, ses, ids.RunID, ids.AssistantMsgID, userInput, harness.RequestParams{}, def, false)
	return &AgentRunOutcome{
		RunID:          ids.RunID,
		SessionID:      sessionID,
		AssistantMsgID: ids.AssistantMsgID,
		Content:        res.Content,
		Reason:         string(res.Reason),
		Err:            res.Err,
	}, nil
}

// AgentRunOutcome 一次 Agent 运行的成品（后台任务回执）。
type AgentRunOutcome struct {
	RunID          string
	SessionID      string
	AssistantMsgID string
	Content        string
	Reason         string
	Err            error
}

// runLLM 异步跑 LLM（多轮 ReAct）：墙钟预算 + 结果落库 + 发 chat:done / chat:error。
// resume=true 时从检查点续跑同一 runID（中断/崩溃后恢复）。
func (s *ChatService) runLLM(parentCtx context.Context, ses *domain.ChatSessionDO, runID, assistantMsgID, userInput string, params harness.RequestParams, def harness.Definition, resume bool) {
	wall := 10 * time.Minute
	if def.Budget.MaxWallTime > 0 {
		wall = def.Budget.MaxWallTime
	}
	ctx, cancel := context.WithTimeout(parentCtx, wall)
	defer cancel()
	// run 结束（含取消/失败）后队列作废：注入消息只属于本次 run
	defer s.steers.clear(ses.ID)
	s.executeAgent(ctx, ses, runID, assistantMsgID, userInput, params, def, resume)
}

// ResumeRun 从检查点续跑：中断/崩溃后以同一 runID 恢复，前端重挂 SSE 续渲染。
// 已完成工具调用经 StepRecords 复用，不重放副作用；审批/补充输入会重新发起。
func (s *ChatService) ResumeRun(ctx context.Context, runID string) (*domain.SendStreamResult, error) {
	if s.checkpoints == nil {
		return nil, pkg.New(5007, "检查点未启用", "")
	}
	cp, err := s.checkpoints.LoadLast("", runID)
	if err != nil {
		return nil, err
	}
	ses, err := s.sessions.GetByID(ctx, cp.SessionID)
	if err != nil {
		return nil, err
	}
	if cp.AssistantMsgID == "" {
		return nil, pkg.New(5007, "检查点无关联消息，无法续跑", runID)
	}
	if _, busy := s.runs.lookup(ses.ID); busy {
		return nil, pkg.New(5002, "会话忙（已有运行中的 run）", ses.ID)
	}
	// 中断终态回置 streaming，前端继续渲染同一条消息
	_ = s.messages.UpdateStatus(ctx, cp.AssistantMsgID, map[string]any{
		"status": domain.MessageStatusStreaming, "stop_reason": nil, "updated_at": time.Now().UnixMilli(),
	})
	runCtx, cancel := context.WithCancel(context.Background())
	s.runs.set(ses.ID, runID, cancel)
	go func() {
		defer s.runs.delete(ses.ID)
		s.runLLM(runCtx, ses, runID, cp.AssistantMsgID, "", harness.RequestParams{}, harness.Agent(defaultAgentName), true)
	}()
	return &domain.SendStreamResult{RunID: runID, SessionID: ses.ID, AssistantMsgID: cp.AssistantMsgID}, nil
}

// ReapInterrupted 启动排空：崩溃时卡在 streaming 的 assistant 消息标 failed + interrupted。
func (s *ChatService) ReapInterrupted(ctx context.Context) {
	n, err := s.messages.ReapStreaming(ctx, domain.MessageStatusFailed, "interrupted")
	if err != nil {
		pkg.L.Warn("reap interrupted messages failed", "err", err.Error())
		return
	}
	if n > 0 {
		pkg.L.Info("reaped interrupted streaming messages", "count", n)
	}
}

// executeAgent 跑一次 Agent：装配上下文 → 跑 harness → 落库/计量/记忆。
// chat 与后台任务共用同一条路径（唯一差异是调用方给的 ctx 与 Agent 定义）。
func (s *ChatService) executeAgent(ctx context.Context, ses *domain.ChatSessionDO, runID, assistantMsgID, userInput string, params harness.RequestParams, def harness.Definition, resume bool) harness.RunResult {
	prov, err := s.reg.Get(ses.ProviderID)
	if err != nil {
		s.failRun(ctx, runID, ses.ID, assistantMsgID, err)
		return harness.RunResult{Reason: harness.ReasonError, Err: err}
	}

	// 拉历史 messages → 组装给 LLM（含工具调用上下文）
	hists, err := s.messages.ListBySession(ctx, ses.ID, 0, 0)
	if err != nil {
		s.failRun(ctx, runID, ses.ID, assistantMsgID, err)
		return harness.RunResult{Reason: harness.ReasonError, Err: err}
	}
	llmMsgs, err := s.toLLMMessages(hists)
	if err != nil {
		s.failRun(ctx, runID, ses.ID, assistantMsgID, err)
		return harness.RunResult{Reason: harness.ReasonError, Err: err}
	}

	// 上下文装配：能力注册表按序注入（人格 → 工作区 → 记忆 → 知识库 → Skill → 工作流）。
	// 新增能力只需实现 Capability 并注册，装配代码不随能力增加而膨胀。
	asm := harness.NewContextAssembler()
	preload := &capability.PreloadCtx{
		SessionID: ses.ID,
		RunID:     runID,
		UserInput: userInput,
		Session:   ses,
		Def:       def,
	}
	for _, piece := range s.caps.PreloadAll(ctx, preload) {
		asm.Add(piece)
	}
	activeSkillTools := preload.State.SkillTools
	// 压缩保留指示：写进会话元数据，每次 run 都装进 system——不受历史折叠影响；
	// 置于末段，优先级高于各能力注入的内容
	if ins := compactInstructions(ses); ins != "" {
		asm.Add(harness.ContextPiece{Key: "compact", Title: "压缩保留指示", Body: ins})
	}
	if sys := asm.Build(); sys != nil {
		llmMsgs = append([]*llm.Message{sys}, llmMsgs...)
	}

	// 收集本轮 assistant 消息的工具调用（run 结束后写入 tool_calls 落库）
	var toolCalls []llm.ToolCall

	// 终态事件延迟到 assistant 消息落库后再发：前端收 chat:done 会立刻拉权威快照，
	// 早于落库会让「空 content」覆盖已渲染内容，过程块一并丢失。
	var doneEvent map[string]any
	defer func() {
		if doneEvent != nil {
			s.emit(runID, ses.ID, "chat:done", doneEvent)
		}
	}()

	// 消息块持久化：工具调用/结果/产物随事件落 message_blocks，刷新后可复现。
	// 仅父 run 落块（e.RunID == runID），子 Agent 委派事件不归属本消息。
	blockSeq := int64(0)
	persistBlock := func(e harness.Event, kind domain.MessageBlockKind, payload map[string]any) {
		if s.blocks == nil || assistantMsgID == "" || e.RunID != runID {
			return
		}
		blockSeq++
		bs, err := json.Marshal(payload)
		if err != nil {
			return
		}
		row := domain.MessageBlockDO{
			ID:        pkg.NewID(domain.IDMessageBlock),
			MessageID: assistantMsgID,
			SessionID: ses.ID,
			Seq:       blockSeq,
			Kind:      kind,
			Payload:   string(bs),
		}
		if err := s.blocks.Create(ctx, &row); err != nil {
			pkg.L.Warn("persist message block failed", "runID", runID, "err", err.Error())
		}
	}

	runStart := time.Now()
	sink := harness.FuncSink(func(e harness.Event) {
		// 子 Agent 委派（delegate.go）会把子 run 事件转发进父 sink，e.RunID 为子 run。
		// 生命周期事件（start/done/error）绝不能复用父 run 的 chat:stream.start/done/error：
		// 前端收到 chat:done 即关闭 SSE，父 run 后续输出将全部丢失。
		// 子事件走独立 chat:subagent-* 通道；子工具事件实时推 chat:tool*（带 agent 标签），
		// 但不写父 run 的 toolCalls / 消息块 / role=tool 历史（孤儿 tool 消息会破坏下轮上下文协议）。
		isChild := e.RunID != runID
		switch e.Kind {
		case harness.EventRunStart:
			if isChild {
				s.emit(runID, ses.ID, "chat:subagent-start", map[string]any{"sub_run_id": e.RunID, "agent": e.Agent})
				return
			}
			s.emit(runID, ses.ID, "chat:stream.start", map[string]any{"model": ses.Model})
		case harness.EventTurnDelta:
			if isChild {
				return
			}
			if p, ok := e.Payload.(harness.TurnDeltaPayload); ok && p.Kind == "content" {
				s.emit(runID, ses.ID, "chat:stream", map[string]any{"delta": p.Text})
			}
		case harness.EventTurnThinking:
			if isChild {
				return
			}
			if p, ok := e.Payload.(harness.TurnDeltaPayload); ok && p.Kind == "thinking" {
				s.emit(runID, ses.ID, "chat:thinking", map[string]any{"delta": p.Text})
			}
		case harness.EventTurnEnd:
			if isChild {
				return
			}
			// 轮次结束推累计用量（chat:stats）：前端 streamingStats 实时驱动上下文进度与消息用量行
			if p, ok := e.Payload.(harness.UsagePayload); ok {
				s.emit(runID, ses.ID, "chat:stats", map[string]any{
					"turn":                  e.Turn + 1,
					"input_tokens":          p.InputTokens,
					"output_tokens":         p.OutputTokens,
					"cache_read_tokens":     p.CacheRead,
					"cache_creation_tokens": p.CacheWrite,
					"total_tokens":          p.Total,
					"latency_ms":            time.Since(runStart).Milliseconds(),
				})
			}
		case harness.EventToolCall:
			if p, ok := e.Payload.(harness.ToolCallPayload); ok {
				if !isChild {
					toolCalls = append(toolCalls, llm.ToolCall{
						ID:   p.ID,
						Type: "function",
						Function: llm.FunctionCall{
							Name:      p.Name,
							Arguments: p.Arguments,
						},
					})
				}
				s.emit(runID, ses.ID, "chat:tool", map[string]any{"id": p.ID, "name": p.Name, "arguments": p.Arguments, "agent": e.Agent})
				if !isChild {
					persistBlock(e, domain.BlockToolCall, map[string]any{"id": p.ID, "name": p.Name, "arguments": p.Arguments})
				}
			}
		case harness.EventToolStart:
			if p, ok := e.Payload.(harness.ToolCallPayload); ok {
				s.emit(runID, ses.ID, "chat:tool-start", map[string]any{"id": p.ID, "name": p.Name, "agent": e.Agent})
			}
		case harness.EventToolResult:
			if p, ok := e.Payload.(harness.ToolResultPayload); ok {
				s.emit(runID, ses.ID, "chat:tool-result", map[string]any{
					"id": p.ToolCallID, "name": p.Name,
					"content": p.Content, "error": p.Err, "duration_ms": p.DurationMs,
					"agent": e.Agent,
				})
				if isChild {
					return
				}
				// 计划快照独立成事件：前端进度卡直接消费，无需解析工具文本
				if p.Name == todoToolName {
					if st, ok := p.Data["session_todo"].(domain.TodoStateRESP); ok {
						s.emit(runID, ses.ID, "chat:todo", map[string]any{"state": st})
					}
				}
				// 结果 + 产物落块：审批拒绝（refused）同样落块，历史可复现完整过程
				persistBlock(e, domain.BlockToolResult, map[string]any{
					"tool_call_id": p.ToolCallID, "name": p.Name,
					"content": p.Content, "error": p.Err,
					"duration_ms": p.DurationMs, "refused": p.Refused,
				})
				if len(p.Data) > 0 {
					persistBlock(e, domain.BlockArtifact, map[string]any{"name": p.Name, "data": p.Data})
				}
				// 工具结果落库（role=tool），供后续轮次/下次 run 重建上下文
				// 序号统一走分配器：与 steering 注入消息共享水位，避免撞号
				seq, serr := s.allocSeq(ctx, ses.ID, 1)
				if serr != nil {
					pkg.L.Warn("alloc tool message seq failed", "err", serr)
				}
				toolMsg := &domain.MessageDO{
					ID:         pkg.NewID(domain.IDMessage),
					SessionID:  ses.ID,
					Seq:        seq,
					RunID:      runID,
					Role:       domain.MessageRoleTool,
					Content:    toolResultContent(p),
					ToolCallID: p.ToolCallID,
					Status:     domain.MessageStatusCompleted,
					CreatedAt:  time.Now().UnixMilli(),
					UpdatedAt:  time.Now().UnixMilli(),
				}
				_ = s.messages.Insert(ctx, toolMsg)
			}
		case harness.EventError:
			if p, ok := e.Payload.(harness.ErrorPayload); ok {
				if isChild {
					s.emit(runID, ses.ID, "chat:subagent-error", map[string]any{"sub_run_id": e.RunID, "agent": e.Agent, "message": p.Message})
					return
				}
				s.emit(runID, ses.ID, "chat:error", map[string]any{"code": p.Code, "message": p.Message})
			}
		case harness.EventRetry:
			if p, ok := e.Payload.(harness.RetryPayload); ok {
				s.emit(runID, ses.ID, "chat:retry", map[string]any{"attempt": p.Attempt, "delay_ms": p.DelayMs, "reason": p.Reason})
			}
		case harness.EventRunDone:
			if p, ok := e.Payload.(harness.RunDonePayload); ok {
				if isChild {
					s.emit(runID, ses.ID, "chat:subagent-done", map[string]any{
						"sub_run_id": e.RunID, "agent": e.Agent,
						"reason": string(domain.MapHarnessReason(p.Reason)),
					})
					return
				}
				doneEvent = map[string]any{
					"status":      "completed",
					"reason":      string(domain.MapHarnessReason(p.Reason)),
					"stop_reason": p.StopReason,
					"message_id":  p.MessageID,
					"usage":       p.Usage,
				}
			}
		}
	})

	// 工具两级过滤：Skill 白名单（命中 skill 时）→ Agent 工具策略
	toolDefs := s.tools.LLMDefinitionsFiltered(ctx, activeSkillTools)
	toolDefs = def.FilterTools(toolDefs)
	cfg := harness.DefaultConfig()
	def.Budget.Apply(&cfg)
	// 上下文预算按「上下文窗口 × 压缩比例」重算：Agent 内置 120k 是静态值，
	// 小窗口模型会撑爆、大窗口模型又过早压缩，交给 provider/全局设置决定。
	provRow := s.providerDO(ctx, ses.ProviderID)
	window := s.contextWindow(ctx, ses.ProviderID)
	if window <= 0 {
		window = defaultContextWindow
	}
	if budget := s.contextBudget(ctx, window, provRow); budget > 0 {
		cfg.ContextBudget = budget
	}
	// 上下文压缩升级：Auto（watermark + LLM 六段交接摘要）优先，失败降级 Micro；
	// 压缩发生发可见事件，让用户知道上下文被主动管理过
	auto := harness.NewAutoCompressor(prov, ses.Model)
	auto.Notify = func(removed int, summary string) {
		head := summary
		if r := []rune(head); len(r) > 120 {
			head = string(r[:120]) + "…"
		}
		pkg.L.Info("context compressed", "runID", runID, "removed", removed)
		s.emit(runID, ses.ID, "chat:compressed", map[string]any{"removed_messages": removed, "summary_head": head})
	}
	r := harness.NewRunner(prov, sink, cfg).
		WithCompressor(auto).
		WithRequestParams(params).
		WithProviderParams(s.providerParams(ctx, ses.ProviderID)).
		WithDefaults(s.defaults(ctx)).
		WithTools(s.tools.Registry(), toolDefs).
		WithCheckpointStore(s.checkpoints).
		WithExecutionRegistry(s.execs)
	if gate := s.toolGate(ctx, ses.PermissionMode); gate != nil {
		r = r.WithToolGate(gate, s.approval.Approve)
	}
	// 目录信任闸门：挂在工具策略门之前——目录都没授权，不必再问命令白名单
	if trustHook := s.pathTrust(); trustHook != nil {
		r = r.WithPathTrust(trustHook)
	}
	// 注入缝：同一队列供两条缝消费——跑工具中途插话 / 说完后自动续接
	drain := func(context.Context) []*llm.Message { return s.steers.drain(ses.ID) }
	r = r.WithSteering(drain).WithFollowUp(drain)
	// 自动降级：LLM 建流失败 → 切到 chat.fallback_model 重试本轮
	r = r.WithTurnAdjuster(s.turnAdjuster(ctx))
	// 补充输入能力注入 ctx：request_input 工具暂停 run 问用户
	if s.approval != nil {
		ctx = tool.WithInputRequester(ctx, s.approval)
	}

	pkg.L.Info("chat run start",
		"runID", runID, "sessionID", ses.ID, "providerID", ses.ProviderID, "model", ses.Model,
		"history", len(llmMsgs), "tools", len(toolDefs), "skill", strings.Join(activeSkillTools, ","),
		"resume", resume)
	var res harness.RunResult
	if resume {
		// 续跑：前端按同一 runID 挂 SSE；Resume 不重发 RunStart，这里补 stream.start
		s.emit(runID, ses.ID, "chat:stream.start", map[string]any{"model": ses.Model, "resumed": true})
		res = r.Resume(ctx, runID, ses.ID, assistantMsgID, ses.Model)
	} else {
		res = r.RunMessages(ctx, runID, ses.ID, assistantMsgID, ses.Model, llmMsgs)
	}
	elapsed := time.Since(runStart).Milliseconds()

	s.persistUsage(ctx, ses, runID, assistantMsgID, res)

	if res.Err != nil {
		pkg.L.Error("chat run failed",
			"runID", runID, "sessionID", ses.ID, "reason", string(res.Reason),
			"latencyMs", elapsed, "err", res.Err.Error())
	} else {
		pkg.L.Info("chat run done",
			"runID", runID, "sessionID", ses.ID, "reason", string(res.Reason),
			"stopReason", res.StopReason, "turns", len(res.Turns), "toolCalls", len(toolCalls),
			"latencyMs", elapsed,
			"input", res.Usage.InputTokens, "output", res.Usage.OutputTokens,
			"cacheRead", res.Usage.CacheReadTokens, "total", res.Usage.TotalTokens)
	}

	nowMs := time.Now().UnixMilli()
	// 终止原因统一口径：harness 枚举 → 领域 stop_reason，
	// max_turns / stagnation / token_budget 不再被硬写成 completed，前端可差异化收尾。
	stopReason := domain.MapHarnessReason(string(res.Reason))
	toolCallsJSON := ""
	if len(toolCalls) > 0 {
		if bs, err := json.Marshal(toolCalls); err == nil {
			toolCallsJSON = string(bs)
		}
	}
	if res.Err != nil {
		s.finishExec(runID, harness.StateFailed)
		_ = s.messages.UpdateStatus(ctx, assistantMsgID, map[string]any{
			"content":       res.Content,
			"thinking":      res.Thinking,
			"tool_calls":    toolCallsJSON,
			"status":        domain.MessageStatusFailed,
			"stop_reason":   stopReason,
			"output_tokens": res.Usage.OutputTokens,
			"updated_at":    nowMs,
		})
		return res
	}
	s.finishExec(runID, execState(res.Reason))
	_ = s.messages.UpdateStatus(ctx, assistantMsgID, map[string]any{
		"content":       res.Content,
		"thinking":      res.Thinking,
		"tool_calls":    toolCallsJSON,
		"status":        domain.MessageStatusCompleted,
		"stop_reason":   stopReason,
		"input_tokens":  res.Usage.InputTokens,
		"output_tokens": res.Usage.OutputTokens,
		"cache_read":    res.Usage.CacheReadTokens,
		"total_tokens":  res.Usage.TotalTokens,
		"latency_ms":    nowMs - ses.LastMessageAt,
		"updated_at":    nowMs,
	})

	// run 后沉淀（记忆形成等）：异步执行、独立超时，不阻塞响应；
	// 各能力按自身策略决定是否沉淀（如 Agent 定义关闭 Formation 时记忆能力直接跳过）
	s.caps.CaptureAll(&capability.CaptureCtx{
		SessionID:  ses.ID,
		RunID:      runID,
		UserInput:  userInput,
		Reply:      res.Content,
		Transcript: captureTranscript(llmMsgs, userInput, res.Content),
		Def:        def,
	}, memoryCaptureTimeout)
	return res
}

// captureTranscript 组装沉淀用的对话副本：本轮发送给 LLM 的消息 + 用户输入 + 最终回答。
func captureTranscript(sent []*llm.Message, userInput, reply string) []llm.Message {
	transcript := make([]llm.Message, 0, len(sent)+2)
	for _, m := range sent {
		if m == nil {
			continue
		}
		transcript = append(transcript, *m)
	}
	if userInput != "" {
		transcript = append(transcript, *llm.UserMessage(userInput))
	}
	if reply != "" {
		transcript = append(transcript, *llm.AssistantMessage(reply, nil))
	}
	return transcript
}

// persistUsage 把每次 LLM 调用的用量落 token_usages。
//
// 先落明细再统计：明细是仪表盘三线图的唯一数据源，失败只告警不阻断（不因计量丢回答）。
func (s *ChatService) persistUsage(ctx context.Context, ses *domain.ChatSessionDO, runID, assistantMsgID string, res harness.RunResult) {
	if s.usages == nil || len(res.Turns) == 0 {
		return
	}
	nowMs := time.Now().UnixMilli()
	rows := make([]domain.TokenUsageDO, 0, len(res.Turns))
	for _, t := range res.Turns {
		rows = append(rows, domain.TokenUsageDO{
			ID:               pkg.NewID("USAGE"),
			SessionID:        ses.ID,
			RunID:            runID,
			MessageID:        assistantMsgID,
			ProviderID:       ses.ProviderID,
			Model:            ses.Model,
			Source:           domain.UsageSourceChat,
			Turn:             t.Turn,
			InputTokens:      t.Usage.InputTokens,
			OutputTokens:     t.Usage.OutputTokens,
			CacheReadTokens:  t.Usage.CacheReadTokens,
			CacheWriteTokens: t.Usage.CacheWriteTokens,
			TotalTokens:      t.Usage.TotalTokens,
			CreatedAt:        nowMs,
		})
	}
	if err := s.usages.BatchCreate(ctx, rows); err != nil {
		pkg.L.Warn("persist token usage failed", "runID", runID, "turns", len(rows), "err", err.Error())
	}
}

// toLLMMessages 历史消息 → llm.Message；重建 assistant 的工具调用与 tool 消息上下文。
//
// <p>同时按全列表里所有 assistant 的 tool_calls 预先索引，过滤掉孤儿
// {@code role=tool}（tool_call_id 在上下文中没有匹配的 assistant tool_call）。
// 孤儿通常由以下路径产生：续跑/编辑重发后旧 tool 结果残留、压缩器只摘掉
// assistant 段而保留 tool 段、checkpoint 续跑把过期 turn 的 tool 结果回放。
// 不剥掉的话上游 LLM 会以「tool result's tool id not found」400 拒绝整轮。
func (s *ChatService) toLLMMessages(hists []domain.MessageDO) ([]*llm.Message, error) {
	knownToolIDs := make(map[string]struct{}, len(hists))
	for _, m := range hists {
		if m.Role != domain.MessageRoleAssistant || m.ToolCalls == "" {
			continue
		}
		var calls []llm.ToolCall
		if err := json.Unmarshal([]byte(m.ToolCalls), &calls); err != nil {
			continue
		}
		for _, c := range calls {
			if c.ID != "" {
				knownToolIDs[c.ID] = struct{}{}
			}
		}
	}
	out := make([]*llm.Message, 0, len(hists))
	for i := range hists {
		m := hists[i]
		if m.Status == domain.MessageStatusStreaming {
			// 当前 assistant 占位不回填（避免循环引用）
			continue
		}
		if m.Role == domain.MessageRoleTool {
			if m.ToolCallID == "" {
				continue
			}
			if _, ok := knownToolIDs[m.ToolCallID]; !ok {
				continue
			}
		}
		lm := &llm.Message{Role: llm.RoleType(m.Role), Content: m.Content, Thinking: m.Thinking}
		if m.Role == domain.MessageRoleTool {
			lm.ToolCallID = m.ToolCallID
		}
		if m.Role == domain.MessageRoleAssistant && m.ToolCalls != "" {
			var calls []llm.ToolCall
			if err := json.Unmarshal([]byte(m.ToolCalls), &calls); err != nil {
				return nil, pkg.Wrap(2062, "parse tool_calls failed", err)
			}
			lm.ToolCalls = calls
		}
		out = append(out, lm)
	}
	return out, nil
}

// nextSeqAfter 计算当前最大 seq（供工具消息续接）。
func nextSeqAfter(hists []domain.MessageDO) int64 {
	var max int64
	for i := range hists {
		if hists[i].Seq > max {
			max = hists[i].Seq
		}
	}
	return max
}

// toolResultContent 组装落库的 tool 消息正文（含错误信息）。
func toolResultContent(p harness.ToolResultPayload) string {
	if p.Err != "" {
		return "error: " + p.Err + "\n" + p.Content
	}
	return p.Content
}

// failRun 错误落库：标 failed + 发 chat:error。
func (s *ChatService) failRun(ctx context.Context, runID, sessionID, assistantMsgID string, err error) {
	s.finishExec(runID, harness.StateFailed)
	now := time.Now().UnixMilli()
	_ = s.messages.UpdateStatus(ctx, assistantMsgID, map[string]any{
		"status":      domain.MessageStatusFailed,
		"stop_reason": domain.StopReasonError,
		"updated_at":  now,
	})
	s.emit(runID, sessionID, "chat:error", map[string]any{"code": 5000, "message": err.Error()})
	s.emit(runID, sessionID, "chat:done", map[string]any{
		"status": "failed", "reason": string(domain.StopReasonError), "message_id": assistantMsgID,
	})
}

// truncateRunes 按 rune 截断（中文安全）；统一走叶子工具包实现。
func truncateRunes(s string, n int) string { return pkg.TruncateRunes(s, n) }

// truncate / splitReply。
var _ = strings.Builder{}

// providerParams 拉 Provider DO → *llm.ProviderParams；DO 缺/取失败返回 nil（走全局默认）。
func (s *ChatService) providerParams(ctx context.Context, providerID string) *llm.ProviderParams {
	if row := s.providerDO(ctx, providerID); row != nil {
		return llm.ProviderParamsFromDO(row)
	}
	return nil
}

// providerDO 取 Provider 实体；缺失/查失败返回 nil（调用方各自兜底，不因配置缺失中断 run）。
func (s *ChatService) providerDO(ctx context.Context, providerID string) *domain.AiProviderDO {
	if s.provRepo == nil || providerID == "" {
		return nil
	}
	row, err := s.provRepo.GetByID(ctx, providerID)
	if err != nil || row == nil {
		return nil
	}
	return row
}

// defaults 从 system_settings 读 chat 默认温度/思考；读失败回退到 config.yaml 内置默认（0.2 / medium）。
func (s *ChatService) defaults(ctx context.Context) llm.Defaults {
	d := llm.Defaults{Temperature: 0.2, Thinking: llm.ThinkingFromEffort("medium")}
	if s.setRepo == nil {
		return d
	}
	if row, err := s.setRepo.Get(ctx, domain.SettingKeyChatDefaultTemperature); err == nil && row != nil {
		if t, perr := strconv.ParseFloat(strings.TrimSpace(row.V), 64); perr == nil {
			d.Temperature = t
		}
	}
	if row, err := s.setRepo.Get(ctx, domain.SettingKeyChatDefaultThinking); err == nil && row != nil {
		if tc := llm.ThinkingFromEffort(strings.TrimSpace(row.V)); tc != nil {
			d.Thinking = tc
		}
	}
	return d
}

// 上下文与压缩相关默认值（system_settings 未配置时的兜底）。
const (
	defaultCompressionRatio = 0.9   // 上下文占用达窗口 90% 触发压缩
	defaultMaxInputChars    = 32000 // 单条用户输入上限（含附件展开文本）
)

// settingFloat 读数值型系统设置；缺失/非法返回 fallback。
func (s *ChatService) settingFloat(ctx context.Context, key string, fallback float64) float64 {
	if s.setRepo == nil {
		return fallback
	}
	row, err := s.setRepo.Get(ctx, key)
	if err != nil || row == nil {
		return fallback
	}
	v, perr := strconv.ParseFloat(strings.TrimSpace(row.V), 64)
	if perr != nil {
		return fallback
	}
	return v
}

// compressionRatio 压缩触发比例：Provider 级 compress_ratio 优先，其次全局 chat.compressionRatio。
func (s *ChatService) compressionRatio(ctx context.Context, prov *domain.AiProviderDO) (ratio float64, from string) {
	if prov != nil && prov.CompressRatio > 0 && prov.CompressRatio <= 1 {
		return prov.CompressRatio, "provider"
	}
	return s.settingFloat(ctx, domain.SettingKeyChatCompressionRatio, defaultCompressionRatio), "default"
}

// contextBudget 本轮上下文预算：上下文窗口 × 压缩比例；压缩器在估算 token 超此值时触发。
func (s *ChatService) contextBudget(ctx context.Context, window int, prov *domain.AiProviderDO) int {
	ratio, _ := s.compressionRatio(ctx, prov)
	if window <= 0 {
		return 0
	}
	return int(float64(window) * ratio)
}

// EffectiveParams 当前会话实际生效参数快照：合并「Provider 级 → 全局默认 → 内置兜底」三层，
// 并标注每层来源。前端输入框与设置页共用此接口，杜绝两处显示不一致。
func (s *ChatService) EffectiveParams(ctx context.Context, sessionID string) (*domain.EffectiveParamsRESP, error) {
	ses, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	prov := s.providerDO(ctx, ses.ProviderID)
	def := s.defaults(ctx)
	// 思考强度按字符串透传（effort 是前端与 Provider 表共用的语义值，无需经 ThinkingConfig 反解）
	effort, effortFrom := "medium", "builtin"
	if s.setRepo != nil {
		if row, err := s.setRepo.Get(ctx, domain.SettingKeyChatDefaultThinking); err == nil && row != nil {
			if v := strings.TrimSpace(row.V); v != "" {
				effort, effortFrom = v, "default"
			}
		}
	}

	out := &domain.EffectiveParamsRESP{
		SessionID:         ses.ID,
		ProviderID:        ses.ProviderID,
		Model:             ses.Model,
		Temperature:       def.Temperature,
		TemperatureFrom:   "default",
		ThinkingEffort:    effort,
		ThinkingFrom:      effortFrom,
		ContextWindow:     defaultContextWindow,
		ContextWindowFrom: "builtin",
	}
	if prov != nil {
		if prov.Temperature > 0 {
			out.Temperature = prov.Temperature
			out.TemperatureFrom = "provider"
		}
		if prov.ThinkingEffort != "" {
			out.ThinkingEffort = prov.ThinkingEffort
			out.ThinkingFrom = "provider"
		}
		if prov.ContextWindow > 0 {
			out.ContextWindow = prov.ContextWindow
			out.ContextWindowFrom = "provider"
		}
	}
	ratio, ratioFrom := s.compressionRatio(ctx, prov)
	out.CompressionRatio = ratio
	out.CompressionFrom = ratioFrom
	out.ContextBudget = s.contextBudget(ctx, out.ContextWindow, prov)
	out.MaxInputChars = int(s.settingFloat(ctx, domain.SettingKeyChatMaxInputChars, defaultMaxInputChars))
	return out, nil
}
