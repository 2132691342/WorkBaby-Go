package service

import (
	"context"
	"encoding/json"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/skill"
)

// SkillService Skill 编排：skills 表唯一真相源 + Registry 运行时装载。
type SkillService struct {
	repo *repo.SkillRepo
	reg  *skill.Registry
}

// NewSkillService 注入仓储与注册中心。
func NewSkillService(r *repo.SkillRepo, reg *skill.Registry) *SkillService {
	return &SkillService{repo: r, reg: reg}
}

// SyncBuiltin 启动期：内置 Skill upsert 进表 + 重建 Registry。
//
// 用户关闭状态必须保留：内置 SKILL.md 每次启动重新解析（Enabled 恒为 true），
// 直接 upsert 全字段覆盖会把用户 SetSkillEnabled(false) 的结果重置回 true。
func (s *SkillService) SyncBuiltin(ctx context.Context) error {
	rows, err := skill.LoadBuiltin()
	if err != nil {
		return err
	}
	for i := range rows {
		if exist, gerr := s.repo.GetByName(ctx, rows[i].Name); gerr == nil && exist != nil {
			rows[i].Enabled = exist.Enabled
		}
		if err := s.repo.Upsert(ctx, &rows[i]); err != nil {
			return err
		}
	}
	return s.Reload(ctx)
}

// Reload 从表重建 Registry（增删改/启停后调用）。
func (s *SkillService) Reload(ctx context.Context) error {
	list, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	return s.reg.Reload(list)
}

// List 返回全部 Skill（含 disabled）。
func (s *SkillService) List(ctx context.Context) ([]domain.SkillRESP, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.SkillRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toSkillRESP(&rows[i]))
	}
	return out, nil
}

// SetEnabled 启停切换并重建 Registry。
func (s *SkillService) SetEnabled(ctx context.Context, name string, enabled bool) error {
	row, err := s.repo.GetByName(ctx, name)
	if err != nil {
		return err
	}
	row.Enabled = enabled
	if err := s.repo.Update(ctx, row); err != nil {
		return err
	}
	return s.Reload(ctx)
}

// marshalScripts 序列化脚本列表（空列表 → 空串 = 清除）。
func marshalScripts(scripts []domain.SkillScript) (string, error) {
	if len(scripts) == 0 {
		return "", nil
	}
	bs, err := json.Marshal(scripts)
	if err != nil {
		return "", pkg.Wrap(8002, "marshal scripts failed", err)
	}
	return string(bs), nil
}

// ImportCustom 导入自定义 Skill（UI 编辑 / SKILL.md 粘贴；scripts 随存）。
func (s *SkillService) ImportCustom(ctx context.Context, req *domain.SkillREQ) (*domain.SkillRESP, error) {
	if req.Name == "" || req.Body == "" {
		return nil, pkg.New(8002, "skill name and body are required", "")
	}
	tools, err := json.Marshal(req.AllowedTools)
	if err != nil {
		return nil, pkg.Wrap(8002, "marshal allowed_tools failed", err)
	}
	scripts, err := marshalScripts(req.Scripts)
	if err != nil {
		return nil, err
	}
	row := &domain.SkillDO{
		ID:           pkg.NewID(domain.IDSkill),
		Name:         req.Name,
		Description:  req.Description,
		WhenToUse:    req.WhenToUse,
		Body:         req.Body,
		AllowedTools: string(tools),
		ScriptsJSON:  scripts,
		SourceKind:   domain.SkillSourceKindCustom,
		Enabled:      true,
	}
	if err := s.repo.Upsert(ctx, row); err != nil {
		return nil, err
	}
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	out := toSkillRESP(row)
	return &out, nil
}

// UpdateCustom 更新自定义 Skill（内置 Skill 拒绝；不存在则按 name 新建）。
func (s *SkillService) UpdateCustom(ctx context.Context, name string, req *domain.SkillREQ) (*domain.SkillRESP, error) {
	if req.Body == "" {
		return nil, pkg.New(8002, "skill body is required", "")
	}
	row, err := s.repo.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if row != nil && row.SourceKind == domain.SkillSourceKindBuiltin {
		return nil, pkg.New(8003, "builtin skill is read-only", name)
	}
	tools, err := json.Marshal(req.AllowedTools)
	if err != nil {
		return nil, pkg.Wrap(8002, "marshal allowed_tools failed", err)
	}
	scripts, err := marshalScripts(req.Scripts)
	if err != nil {
		return nil, err
	}
	if row == nil {
		row = &domain.SkillDO{ID: pkg.NewID(domain.IDSkill), Name: name, SourceKind: domain.SkillSourceKindCustom, Enabled: true}
	}
	row.Description = req.Description
	row.WhenToUse = req.WhenToUse
	row.Body = req.Body
	row.AllowedTools = string(tools)
	row.ScriptsJSON = scripts
	if err := s.repo.Upsert(ctx, row); err != nil {
		return nil, err
	}
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	out := toSkillRESP(row)
	return &out, nil
}

// Delete 删除 Skill（内置 Skill 拒绝）。
func (s *SkillService) Delete(ctx context.Context, name string) error {
	row, err := s.repo.GetByName(ctx, name)
	if err != nil {
		return err
	}
	if row == nil {
		return nil // 幂等
	}
	if row.SourceKind == domain.SkillSourceKindBuiltin {
		return pkg.New(8003, "builtin skill is read-only", name)
	}
	if err := s.repo.Delete(ctx, name); err != nil {
		return err
	}
	return s.Reload(ctx)
}

// Match 关键词路由（chat 层调用）。
func (s *SkillService) Match(content string) string {
	if s.reg == nil {
		return ""
	}
	return s.reg.Match(content)
}

// Get 按名取 Skill 运行时态（chat 层注入 body / 工具白名单）。
func (s *SkillService) Get(name string) (*skill.LoadedSkill, bool) {
	if s.reg == nil {
		return nil, false
	}
	return s.reg.Get(name)
}

// GetScript 解析 (skill, script) → 脚本代码与语言（run_skill_script 工具的 resolver）。
func (s *SkillService) GetScript(ctx context.Context, skillName, scriptName string) (string, string, error) {
	row, err := s.repo.GetByName(ctx, skillName)
	if err != nil {
		return "", "", err
	}
	if row == nil {
		return "", "", pkg.New(8001, "skill not found", skillName)
	}
	if !row.Enabled {
		return "", "", pkg.New(8004, "skill is disabled", skillName)
	}
	if row.ScriptsJSON == "" {
		return "", "", pkg.New(8004, "skill has no scripts", skillName)
	}
	var scripts []domain.SkillScript
	if err := json.Unmarshal([]byte(row.ScriptsJSON), &scripts); err != nil {
		return "", "", pkg.Wrap(8004, "parse scripts failed", err)
	}
	for i := range scripts {
		if scripts[i].Name == scriptName {
			return scripts[i].Code, scripts[i].Language, nil
		}
	}
	return "", "", pkg.New(8004, "script not found", scriptName)
}

func toSkillRESP(sk *domain.SkillDO) domain.SkillRESP {
	var scripts []domain.SkillScript
	if sk.ScriptsJSON != "" {
		_ = json.Unmarshal([]byte(sk.ScriptsJSON), &scripts)
	}
	return domain.SkillRESP{
		ID:           sk.ID,
		Name:         sk.Name,
		Description:  sk.Description,
		WhenToUse:    sk.WhenToUse,
		Body:         sk.Body,
		AllowedTools: parseTools(sk.AllowedTools),
		Scripts:      scripts,
		SourceKind:   sk.SourceKind,
		SourceRef:    sk.SourceRef,
		Version:      sk.Version,
		Enabled:      sk.Enabled,
		CreatedAt:    sk.CreatedAt,
		UpdatedAt:    sk.UpdatedAt,
	}
}

func parseTools(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
