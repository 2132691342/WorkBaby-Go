package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"WorkBaby/internal/capability"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/runtime"
	"WorkBaby/internal/tool"
	"WorkBaby/internal/tool/planmode"
)

// 本文件：装配与权限（With* 注入 / 工具门 / 目录信任 / 计划模式 / 轮间调整 / 事件发射）。

func (s *ChatService) WithCapabilities(caps *capability.Registry) *ChatService {
	s.caps = caps
	return s
}

// WithExecutionRegistry 启用执行平面：登记 run 的 scope/state，供统一执行拓扑查询。
func (s *ChatService) WithExecutionRegistry(reg *harness.ExecutionRegistry) *ChatService {
	s.execs = reg
	return s
}

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

// WithFileStore 注入附件读取能力（图片 → data URI）；nil 时消息附件只降级为文本提示。
func (s *ChatService) WithFileStore(f FileStore) *ChatService { s.files = f; return s }

// WithChangeService 注入文件变更服务（完成度证据：核对声明路径与本 run file_changes）。
func (s *ChatService) WithChangeService(c *FileChangeService) *ChatService { s.changeSvc = c; return s }

// WithDataHome 注入数据根（paths.Home）；目录策略（记忆/快照默认根）由此派生。
// 必须在装配会话级目录解析闭包前调用。
func (s *ChatService) WithDataHome(home string) *ChatService {
	s.dataHome = home
	return s
}

// WithSkillSync 注入技能目录同步钩子：run 前按会话工作区叠加技能。
func (s *ChatService) WithSkillSync(fn func(context.Context, string) error) *ChatService {
	s.skillSync = fn
	return s
}

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

// SessionDataDirs 某会话的记忆文件与快照目录（目录策略唯一数据源）。
// 绑定本地工作区 → {dir}/.workbaby/ 下（过程数据跟工作区走）；未绑定 → {dataHome} 下。
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

// gateInternalAllowTools 工具策略门显式放行清单（default 模式下本会走 ask 的那些）。
//
// 放行不等于无护栏：实现了 tool.RiskClassifier 的工具（exec / run_skill_script）仍由
// runner 拿 ClassifyArgs 的 per-call 风险做命令级裁决——白名单外与危险正则命中照样弹审批。

var gateInternalAllowTools = []string{
	"exec", "run_skill_script", "delegate_task", "run_workflow", // 命令级裁决走 RiskClassifier
	"websearch", "webfetch", "http", // 信息型网络读取
	"knowledge_search", "doc_reader", "todo", // 只读 / 会话内计划
	"file_write", "archive_manager", "memory_write", // 工作区沙箱内的本地写
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

// WithPlanStore 注入计划模式状态（enter/exit_plan_mode 工具与 Guard 共用同一 store）。
func (s *ChatService) WithPlanStore(p *planmode.Store) *ChatService {
	s.planStore = p
	return s
}

// planTrustHook 计划模式 Guard 与目录信任的组合闸门（挂在工具策略门之前）。
// 计划模式激活时非只读工具一律拒绝（执行器级硬拦，不靠 prompt 自觉）；
// 目录信任未注入且计划模式能力未装配时返回 nil（不改变既有行为）。
func (s *ChatService) planTrustHook() harness.PathTrust {
	base := s.pathTrust()
	if s.planStore == nil && base == nil {
		return nil
	}
	return func(ctx context.Context, toolName string, args json.RawMessage) (bool, string) {
		if reason := s.planStore.Guard(harness.SessionIDFromCtx(ctx), toolName); reason != "" {
			return false, reason
		}
		if base == nil {
			return true, ""
		}
		return base(ctx, toolName, args)
	}
}

// WorkflowGate 无人值守执行（工作流 LLM 节点 / 定时任务）的策略门工厂。
//
// 复用全局默认会话模式（system_settings.agent.sessionMode）：与聊天同一套权限语义，
// 全局为 default 时写/执行/网络需审批，为 yolo 时全放行。
// 返回工厂而非实例——工作流执行发生在装配之后，每次执行重建才能让设置页改动立即生效。
func (s *ChatService) WorkflowGate(ctx context.Context) *tool.Gate {
	return s.toolGate(ctx, "")
}

// WorkflowPathTrust 无人值守执行的目录信任闸门。
//
// 不复用 planTrustHook：计划模式是会话级状态，工作流没有会话，
// 带上只会让空 sessionID 反复查一张永远查不到的表。
func (s *ChatService) WorkflowPathTrust() harness.PathTrust {
	return s.pathTrust()
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
