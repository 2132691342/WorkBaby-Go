package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// AgentProfileService 自定义子智能体：CRUD + 启动同步进 harness 注册表。
//
// 注册表是运行期的唯一消费点：delegate_task 按名委派与会话 Agent 切换经 harness.Agent(name)
// 取定义，本服务只负责把 agent_profiles 表里 enabled 的行物化成 harness.Definition。
type AgentProfileService struct {
	repo *repo.AgentProfileRepo
}

// NewAgentProfileService 构造服务；注册表同步由调用方在启动期显式 Sync（失败不阻断启动）。
func NewAgentProfileService(repo *repo.AgentProfileRepo) *AgentProfileService {
	return &AgentProfileService{repo: repo}
}

// agentNamePattern kebab-case 唯一名（与 Skill 命名约定一致）。
var agentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Sync 把 enabled 的自定义子智能体物化进 harness 注册表（启动期与每次写操作后调用）。
func (s *AgentProfileService) Sync(ctx context.Context) error {
	rows, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return err
	}
	defs := make([]harness.Definition, 0, len(rows))
	for i := range rows {
		defs = append(defs, profileToDefinition(&rows[i]))
	}
	harness.SetCustomAgents(defs)
	return nil
}

// List 全量（含 disabled），设置页展示。
func (s *AgentProfileService) List(ctx context.Context) ([]domain.AgentProfileRESP, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.AgentProfileRESP, 0, len(rows))
	for i := range rows {
		out = append(out, profileToRESP(&rows[i]))
	}
	return out, nil
}

// Upsert 按 name 创建或整体更新自定义子智能体；写库成功即重同步注册表。
func (s *AgentProfileService) Upsert(ctx context.Context, req *domain.AgentProfileREQ) (*domain.AgentProfileRESP, error) {
	row, err := validateProfileReq(req)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Upsert(ctx, row); err != nil {
		return nil, err
	}
	if err := s.Sync(ctx); err != nil {
		return nil, err
	}
	out := profileToRESP(row)
	return &out, nil
}

// SetEnabled 启停；停用的行从注册表摘除（下次 run 起生效）。
func (s *AgentProfileService) SetEnabled(ctx context.Context, name string, enabled bool) error {
	if err := s.repo.SetEnabled(ctx, name, enabled); err != nil {
		return err
	}
	return s.Sync(ctx)
}

// Delete 软删并重同步注册表。
func (s *AgentProfileService) Delete(ctx context.Context, name string) error {
	if err := s.repo.Delete(ctx, name); err != nil {
		return err
	}
	return s.Sync(ctx)
}

// validateProfileReq 校验入参并物化 DO：name 唯一性与内置名互斥是硬约束。
func validateProfileReq(req *domain.AgentProfileREQ) (*domain.AgentProfileDO, error) {
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !agentNamePattern.MatchString(name) {
		return nil, pkg.New(8203, "agent 名需为 kebab-case（小写字母开头，字母/数字/连字符，≤64 字符）", req.Name)
	}
	for _, d := range harness.DefaultAgents() {
		if d.Name == name {
			return nil, pkg.New(8202, "该名称为内置 Agent 保留名", name)
		}
	}
	if req.MaxTurns < 0 || req.MaxTurns > 40 {
		return nil, pkg.New(8203, "max_turns 取值范围 0-40（0 用委派默认）", "")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return &domain.AgentProfileDO{
		ID:           pkg.NewID(domain.IDAgentProfile),
		Name:         name,
		Description:  strings.TrimSpace(req.Description),
		SystemPrompt: req.SystemPrompt,
		ToolsAllow:   marshalGlobList(req.ToolsAllow),
		ToolsDeny:    marshalGlobList(req.ToolsDeny),
		MemoryEnable: req.MemoryEnable,
		MaxTurns:     req.MaxTurns,
		Enabled:      enabled,
	}, nil
}

// profileToDefinition 物化成 harness.Definition；空 JSON 数组视为不限制。
func profileToDefinition(row *domain.AgentProfileDO) harness.Definition {
	def := harness.Definition{
		Name:        row.Name,
		Description: row.Description,
		Persona:     row.SystemPrompt,
		Tools: harness.ToolPolicy{
			Allow: unmarshalGlobList(row.ToolsAllow),
			Deny:  unmarshalGlobList(row.ToolsDeny),
		},
		Memory: harness.MemoryPolicy{Enabled: row.MemoryEnable},
	}
	if row.MaxTurns > 0 {
		def.Budget.MaxTurns = row.MaxTurns
	}
	return def
}

// profileToRESP DO → 出参。
func profileToRESP(row *domain.AgentProfileDO) domain.AgentProfileRESP {
	return domain.AgentProfileRESP{
		ID:           row.ID,
		Name:         row.Name,
		Description:  row.Description,
		SystemPrompt: row.SystemPrompt,
		ToolsAllow:   unmarshalGlobList(row.ToolsAllow),
		ToolsDeny:    unmarshalGlobList(row.ToolsDeny),
		MemoryEnable: row.MemoryEnable,
		MaxTurns:     row.MaxTurns,
		Enabled:      row.Enabled,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

// marshalGlobList 序列化工具 glob 列表；空列表存空串（= 不限制）。
func marshalGlobList(list []string) string {
	clean := make([]string, 0, len(list))
	for _, v := range list {
		if v = strings.TrimSpace(v); v != "" {
			clean = append(clean, v)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return ""
	}
	return string(b)
}

// unmarshalGlobList 反序列化工具 glob 列表；空串/坏数据返回 nil。
func unmarshalGlobList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
