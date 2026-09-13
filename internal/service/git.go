// Package service Git 面板服务：封装工作区 git CLI（porcelain 解析），状态源是 .git，不落库。
// 只做查询与用户显式触发的变更（切换 / 暂存 / 提交 / 丢弃），不自动 commit。
package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// GitService git CLI 封装。
type GitService struct {
	// root 会话工作区根（api 层注入：跟随当前绑定的工作目录）。
	root func() string
}

// NewGitService 构造服务。
func NewGitService(root func() string) *GitService {
	return &GitService{root: root}
}

// git 在仓库根执行 git 子命令，继承进程环境并叠加状态查询降噪变量；
// exit 非 0 返回 ErrGitFailed（stderr 摘要进 detail）。
func (s *GitService) git(ctx context.Context, args ...string) (string, error) {
	root := s.root()
	if root == "" {
		return "", domain.ErrGitNotARepo
	}
	c := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotepath=false"}, args...)...)
	c.Dir = root
	c.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_ADVICE=0")
	out, err := c.Output()
	if err != nil {
		if len(args) > 0 && args[0] == "rev-parse" {
			return "", domain.ErrGitNotARepo
		}
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg = strings.TrimSpace(string(ee.Stderr))
			if len(msg) > 300 {
				msg = msg[:300]
			}
		}
		return "", pkg.Wrap(domain.ErrGitFailed.Code, "git "+args[0], fmt.Errorf("%s: %s", err, msg))
	}
	return string(out), nil
}

// Overview 概览一次取齐：分支 / ahead-behind / 变更文件 / 本地分支 / 最近 20 条提交。
func (s *GitService) Overview(ctx context.Context) (*domain.GitOverviewRESP, error) {
	if _, err := s.git(ctx, "rev-parse", "--show-toplevel"); err != nil {
		return nil, err
	}
	resp := &domain.GitOverviewRESP{Root: s.root()}
	resp.Branch = strings.TrimSpace(mustGit(s.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")))
	resp.Upstream = strings.TrimSpace(mustGit(s.git(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")))
	// 左侧计数 = HEAD 独有（ahead），右侧 = upstream 独有（behind）
	if counts := strings.Fields(mustGit(s.git(ctx, "rev-list", "--left-right", "--count", "HEAD...@{u}"))); len(counts) == 2 {
		resp.Ahead = atoiSafe(counts[0])
		resp.Behind = atoiSafe(counts[1])
	}
	resp.Files = s.Status(ctx)
	resp.Branches = s.Branches(ctx)
	resp.Log = s.Log(ctx, 20)
	return resp, nil
}

// Status 变更文件列表（porcelain v1）：每行 XY<space>path，
// X=暂存区状态 Y=工作区状态；未跟踪为 ??。同一文件两区都有变化拆两行。
func (s *GitService) Status(ctx context.Context) []domain.GitFileStatusRESP {
	out := mustGit(s.git(ctx, "status", "--porcelain"))
	if out == "" {
		return []domain.GitFileStatusRESP{}
	}
	files := make([]domain.GitFileStatusRESP, 0, 16)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		x, y := line[0], line[1]
		path := line[3:]
		if x == '?' && y == '?' {
			files = append(files, domain.GitFileStatusRESP{Path: path, Status: "?"})
			continue
		}
		if x != ' ' && x != '?' {
			files = append(files, domain.GitFileStatusRESP{Path: path, Status: string(x), Staged: true})
		}
		if y != ' ' && y != '?' {
			files = append(files, domain.GitFileStatusRESP{Path: path, Status: string(y)})
		}
	}
	return files
}

// Branches 本地分支（字典序）。
func (s *GitService) Branches(ctx context.Context) []string {
	out := mustGit(s.git(ctx, "branch", "--format=%(refname:short)"))
	if out == "" {
		return []string{}
	}
	list := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := range list {
		list[i] = strings.TrimSpace(list[i])
	}
	return list
}

// Switch 切换分支（Create 时不存在即新建）。
func (s *GitService) Switch(ctx context.Context, req *domain.GitSwitchREQ) error {
	name := strings.TrimSpace(req.Branch)
	if name == "" {
		return domain.ErrGitBranch
	}
	args := []string{"checkout"}
	if req.Create {
		args = append(args, "-b")
	}
	args = append(args, name)
	_, err := s.git(ctx, args...)
	return err
}

// Diff unified diff 原文（Path 空 = 全部变更）。Staged 时取暂存区 vs HEAD。
func (s *GitService) Diff(ctx context.Context, req *domain.GitDiffREQ) (string, error) {
	args := []string{"diff", "--no-color"}
	if req.Staged {
		args = append(args, "--cached")
	}
	if req.Path != "" {
		args = append(args, "--", req.Path)
	}
	out, err := s.git(ctx, args...)
	if err != nil {
		return "", err
	}
	return out, nil
}

// DiffStat 变更规模摘要（文件级 +N -M 行），审查入口用。
func (s *GitService) DiffStat(ctx context.Context, req *domain.GitDiffREQ) (string, error) {
	args := []string{"diff", "--no-color", "--stat"}
	if req.Staged {
		args = append(args, "--cached")
	}
	if req.Path != "" {
		args = append(args, "--", req.Path)
	}
	out, err := s.git(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Stage 暂存：path 非空加单个文件，否则 add -A。
func (s *GitService) Stage(ctx context.Context, path string) error {
	if strings.TrimSpace(path) == "" {
		_, err := s.git(ctx, "add", "-A")
		return err
	}
	_, err := s.git(ctx, "add", "--", path)
	return err
}

// Unstage 取消暂存（保留工作区改动）。
func (s *GitService) Unstage(ctx context.Context, path string) error {
	_, err := s.git(ctx, "reset", "HEAD", "--", path)
	return err
}

// Discard 丢弃单个文件的工作区改动（已跟踪 checkout --，未跟踪删除）。
func (s *GitService) Discard(ctx context.Context, path string) error {
	if _, err := s.git(ctx, "checkout", "--", path); err != nil {
		_, err = s.git(ctx, "clean", "-f", "--", path)
		return err
	}
	return nil
}

// Commit 提交（Message 必填；StageAll 先 add -A）；暂存区为空时 8402。
func (s *GitService) Commit(ctx context.Context, req *domain.GitCommitREQ) error {
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return domain.ErrGitCommitMsg
	}
	if req.StageAll {
		if _, err := s.git(ctx, "add", "-A"); err != nil {
			return err
		}
	}
	staged := mustGit(s.git(ctx, "status", "--porcelain"))
	hasStaged := false
	for _, line := range strings.Split(staged, "\n") {
		if len(line) >= 2 && line[0] != ' ' && line[0] != '?' {
			hasStaged = true
			break
		}
	}
	if !hasStaged {
		return domain.ErrGitNothing
	}
	_, err := s.git(ctx, "commit", "-m", msg)
	return err
}

// Log 最近 N 条提交（0x1f 分隔防 subject 含分隔符）。
func (s *GitService) Log(ctx context.Context, limit int) []domain.GitCommitRESP {
	if limit <= 0 {
		limit = 20
	}
	out := mustGit(s.git(ctx, "log", "-n", fmt.Sprint(limit), "--pretty=format:%h%x1f%an%x1f%cs%x1f%s"))
	if out == "" {
		return []domain.GitCommitRESP{}
	}
	commits := make([]domain.GitCommitRESP, 0, limit)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\x1f")
		if len(parts) != 4 {
			continue
		}
		commits = append(commits, domain.GitCommitRESP{
			Hash: parts[0], Author: parts[1], Date: parts[2], Subject: parts[3],
		})
	}
	return commits
}

// mustGit 吞错取输出：聚合阶段部分子命令允许失败（upstream 未配置等）。
func mustGit(out string, err error) string {
	if err != nil {
		return ""
	}
	return out
}

// atoiSafe 宽松解析非负整数，失败返回 0。
func atoiSafe(s string) int {
	n := 0
	for _, ch := range strings.TrimSpace(s) {
		if ch < '0' || ch > '9' {
			return n
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
