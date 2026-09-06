// Package exec 提供命令执行工具；走 ExecPolicy 白名单 + 危险命令拦截。
package exec

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
)

// ExecTool 执行白名单内命令；参数数组形式，绝不拼 shell。
type ExecTool struct {
	policy    tool.ExecPolicy
	approver  tool.Approver                    // 兜底审批门；仅在脱离 runner 护栏链直调时生效
	pathDirs  func() []string                  // 内置运行时 bin 目录提供者；nil = 不增强 PATH
	whitelist func() []string                  // 可选；非 nil 时每次执行动态覆盖 policy.AllowedBinaries（运行时设置页白名单）
	root      func(ctx context.Context) string // 会话工作区根；nil = cwd 缺省用进程当前目录
}

// New 构造 ExecTool；policy 由装配方（service）注入。
func New(policy tool.ExecPolicy) *ExecTool { return &ExecTool{policy: policy} }

// WithApprover 注入审批门（service 层 ApprovalService 实现 tool.Approver）。
// 仅作为脱离 runner 护栏链直调时的兜底；链内调用由策略门统一裁决。
func (t *ExecTool) WithApprover(a tool.Approver) *ExecTool { t.approver = a; return t }

// ClassifyArgs 实现 tool.RiskClassifier：按本次命令给出审批描述与 per-call 风险。
// 白名单内安全命令返回 (command, "")——策略门据此免审放行。
func (t *ExecTool) ClassifyArgs(args json.RawMessage) (string, string) {
	var req execReq
	if err := json.Unmarshal(args, &req); err != nil {
		return "", ""
	}
	full := req.Command
	if len(req.Args) > 0 {
		full += " " + strings.Join(req.Args, " ")
	}
	if strings.TrimSpace(full) == "" {
		return "", ""
	}
	ok, risk := t.effectivePolicy().Classify(full)
	if ok {
		return full, ""
	}
	if risk == "" {
		return "", "" // 空命令：交 Execute 硬拒绝，不走审批
	}
	return full, risk
}

// WithRootResolver 注入会话工作区根解析器（tool.ResolveRoot 包装 RootResolver）；
// 入参 cwd 缺省且解析出有效根时，命令在该目录执行——与 file 系工具的沙箱根一致。
func (t *ExecTool) WithRootResolver(f func(context.Context) string) *ExecTool { t.root = f; return t }

// WithPathDirs 注入内置运行时 bin 目录提供者；执行时实时读取并前置到子进程 PATH，
// 实现 node/python/pwsh 内置环境隔离（不污染用户环境）。
func (t *ExecTool) WithPathDirs(f func() []string) *ExecTool { t.pathDirs = f; return t }

// WithWhitelist 注入运行时白名单提供者（system_settings 读取），每次执行实时生效。
func (t *ExecTool) WithWhitelist(f func() []string) *ExecTool { t.whitelist = f; return t }

// effectivePolicy 返回本次执行的生效策略（运行时白名单覆盖构造时默认值）。
func (t *ExecTool) effectivePolicy() tool.ExecPolicy {
	p := t.policy
	if t.whitelist != nil {
		p.AllowedBinaries = t.whitelist()
	}
	return p
}

func (t *ExecTool) Name() string              { return "exec" }
func (t *ExecTool) RiskLevel() tool.RiskLevel { return tool.RiskExec }

func (t *ExecTool) Description() string {
	return "执行白名单内的本机命令（参数数组形式，无 shell 拼接）。必须传入完整命令与参数列表。"
}

func (t *ExecTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["command"],
			"properties": {
				"command": {"type": "string", "description": "可执行文件路径或名称，必须命中白名单"},
				"args": {"type": "array", "items": {"type": "string"}, "description": "参数列表（数组形式，禁止 shell 拼接）"},
				"timeout": {"type": "integer", "description": "超时毫秒，缺省用策略默认值"},
				"cwd": {"type": "string", "description": "工作目录，缺省为会话工作区（绑定了外部目录时）"}
			}
		}`),
	}
}

// execReq 入参。
type execReq struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Timeout int      `json:"timeout"` // ms；<=0 用策略默认
	Cwd     string   `json:"cwd"`
}

// Execute 执行命令并返回 CombinedOutput。
func (t *ExecTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	var req execReq
	if err := json.Unmarshal(args, &req); err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4004, "exec args parse failed", err)}
	}
	// 白名单按 basename 匹配 command；危险模式检查完整命令串（command + args）
	full := req.Command
	if len(req.Args) > 0 {
		full += " " + strings.Join(req.Args, " ")
	}
	// 安全分类：白名单内直接放行；白名单外/危险命令按护栏链裁决。
	// 统一护栏链（runner 策略门 + 单次审批）已裁决时不再重复询问——单层闸门；
	// 脱链直调（测试/裸用）保留审批兜底，fail-closed 语义不变。
	policy := t.effectivePolicy()
	if ok, risk := policy.Classify(full); !ok {
		if risk == "" {
			return tool.ToolResult{Err: pkg.New(4001, tool.ErrBinaryDenied.Message, "<empty>")}
		}
		switch {
		case tool.GuardChainActive(ctx):
			// runner 护栏链已放行（含审批通过），不再二次询问
		case t.approver != nil:
			if !t.approver.Approve(ctx, full, risk) {
				return tool.ToolResult{Err: pkg.New(4003, tool.ErrApprovalNeeded.Message+", user denied or timed out", full)}
			}
		case risk == tool.RiskApprovalIrrev:
			return tool.ToolResult{Err: pkg.New(4002, tool.ErrPatternDenied.Message, full)}
		default:
			return tool.ToolResult{Err: pkg.New(4001, tool.ErrBinaryDenied.Message, req.Command)}
		}
	}

	execCtx := ctx
	timeout := policy.Timeout
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(execCtx, req.Command, req.Args...)
	switch {
	case req.Cwd != "":
		cmd.Dir = req.Cwd
	case t.root != nil:
		// 工作区联动：解析出的根必须真实存在才生效，否则维持进程当前目录
		//（默认会话隔离目录可能尚未创建，缺目录不应让命令直接失败）。
		if root := strings.TrimSpace(t.root(ctx)); root != "" {
			if info, serr := os.Stat(root); serr == nil && info.IsDir() {
				cmd.Dir = root
			}
		}
	}
	if t.pathDirs != nil {
		if dirs := t.pathDirs(); len(dirs) > 0 {
			cmd.Env = envWithPath(dirs)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} // Windows 不弹控制台

	output, err := cmd.CombinedOutput()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	meta := map[string]string{"exitCode": strconv.Itoa(exitCode)}
	if err != nil {
		if execCtx.Err() != nil {
			return tool.ToolResult{Content: string(output), Meta: meta, Err: pkg.Wrap(4006, "exec timeout or cancelled", err)}
		}
		return tool.ToolResult{Content: string(output), Meta: meta, Err: pkg.Wrap(4006, "exec failed", err)}
	}
	return tool.ToolResult{Content: string(output), Meta: meta}
}

// envWithPath 返回在现有环境基础上把 dirs 前置到 PATH 的环境切片
// （Windows 变量名不区分大小写，用前缀 PATH= 匹配替换）。
func envWithPath(dirs []string) []string {
	sep := string(os.PathListSeparator)
	extra := strings.Join(dirs, sep)
	env := make([]string, 0, len(os.Environ()))
	pathVal, hasPath := "", false
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") {
			pathVal = kv[len("PATH="):]
			hasPath = true
			continue
		}
		env = append(env, kv)
	}
	if hasPath {
		pathVal = extra + sep + pathVal
	} else {
		pathVal = extra
	}
	return append(env, "PATH="+pathVal)
}
