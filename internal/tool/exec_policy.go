package tool

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"WorkBaby/internal/pkg"
)

// ExecPolicy 命令执行策略：白名单 basename 匹配 + 危险命令正则拦截。
//
// 配置来源：构造时注入默认值；运行时可改。
type ExecPolicy struct {
	AllowedBinaries []string      // 白名单（basename 精确匹配）；空 = 全部拒绝
	DeniedPatterns  []string      // 危险命令正则（匹配整条 command + args）
	ApprovalLevel   RiskLevel     // 该等级及以上需审批
	Timeout         time.Duration // 默认单次执行超时
}

// DefaultExecPolicy 安全默认：白名单为空（任何命令都需先在设置面板显式放行），
// 危险命令默认拦截；审批门槛 = exec 级。
func DefaultExecPolicy() ExecPolicy {
	return ExecPolicy{
		AllowedBinaries: []string{},
		DeniedPatterns: []string{
			`(?i)rm\s+(-rf|/s)\s+/`,             // 删根
			`(?i)del\s+/f\s+/s`,                 // 批量强制删
			`(?i)rd\s+/s\s+/q`,                  // 静默删目录树
			`(?i)rmdir\s+/s`,                    // 删目录树
			`(?i)format\s+[a-z]:`,               // 格式化盘符
			`(?i)diskpart`,                      // 磁盘分区
			`(?i)cleanmgr`,                      // 磁盘清理（交互破坏）
			`(?i)reg\s+delete`,                  // 注册表删除
			`(?i)shutdown\s*$|(?i)shutdown\s+-`, // 关机
			`(?i)taskkill\s+/f`,                 // 强杀进程
			`(?i)>|(?i)\|`,                      // 重定向 / 管道（防组合）
			`(?i)&&`,                            // 命令串联
		},
		ApprovalLevel: RiskExec,
		Timeout:       5 * time.Minute,
	}
}

// Allow 校验命令是否允许执行：bin 必须在白名单，整条命令不得命中拒绝模式。
func (p ExecPolicy) Allow(command string) error {
	if ok, risk := p.Classify(command); ok {
		return nil
	} else if risk == RiskApprovalIrrev {
		return pkg.New(4002, ErrPatternDenied.Message, command)
	}
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return pkg.New(4001, ErrBinaryDenied.Message, "<empty>")
	}
	return pkg.New(4001, ErrBinaryDenied.Message, filepath.Base(fields[0]))
}

// Classify 对整条命令（command + args 拼接后）做安全分类：
//   - ok=true                          → 白名单内且未命中危险正则，直接放行
//   - ok=false, risk=RiskApprovalIrrev → 命中危险正则（不可逆），走审批（每次确认）
//   - ok=false, risk=RiskApprovalNeeds → 白名单外（可恢复），走审批
//   - ok=false, risk=""                → 直接拒绝（空命令），不进入审批
func (p ExecPolicy) Classify(command string) (ok bool, risk string) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false, ""
	}
	whitelisted := slicesContains(p.AllowedBinaries, filepath.Base(fields[0]))
	for _, pat := range p.DeniedPatterns {
		if regexp.MustCompile(pat).MatchString(command) {
			return false, RiskApprovalIrrev
		}
	}
	if whitelisted {
		return true, ""
	}
	return false, RiskApprovalNeeds
}

func slicesContains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
