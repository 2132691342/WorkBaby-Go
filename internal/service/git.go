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
	resp.Log = s.Log(ctx, &domain.GitLogREQ{Limit: 20})
	return resp, nil
}

// Status 变更文件列表（porcelain v1）：每行 XY<space>path，
// X=暂存区状态 Y=工作区状态；未跟踪为 ??。同一文件两区都有变化拆两行。
// 重命名/复制形如 `R  old -> new`，拆成 OrigPath + Path（不拆的话路径会带上箭头，
// 后续按路径 diff / 暂存全部失败）。
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
		orig, path := splitRenamePath(line[3:])
		if x == '?' && y == '?' {
			files = append(files, domain.GitFileStatusRESP{Path: path, Status: "?"})
			continue
		}
		if x != ' ' && x != '?' {
			files = append(files, domain.GitFileStatusRESP{Path: path, OrigPath: orig, Status: string(x), Staged: true})
		}
		if y != ' ' && y != '?' {
			files = append(files, domain.GitFileStatusRESP{Path: path, OrigPath: orig, Status: string(y)})
		}
	}
	return files
}

// splitRenamePath 拆解 porcelain 的重命名路径：`old -> new` 与 `dir/{old => new}/f`。
// 不适用时 orig 为空、path 为原文。
func splitRenamePath(raw string) (orig, path string) {
	arrow := " -> "
	if strings.Contains(raw, arrow) {
		i := strings.Index(raw, arrow)
		return strings.Trim(raw[:i], `"`), strings.Trim(strings.Trim(raw[i+len(arrow):], `"`), `"`)
	}
	// 部分重命名：src/{a => b}/x.ts
	open := strings.Index(raw, "{")
	if open < 0 || !strings.Contains(raw[open:], " => ") {
		return "", raw
	}
	close := strings.Index(raw[open:], "}")
	if close < 0 {
		return "", raw
	}
	close += open
	inner := raw[open+1 : close]
	segs := strings.SplitN(inner, " => ", 2)
	if len(segs) != 2 {
		return "", raw
	}
	return raw[:open] + strings.TrimSpace(segs[0]) + raw[close+1:], raw[:open] + strings.TrimSpace(segs[1]) + raw[close+1:]
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

// Log 提交历史：支持分页（Skip）与跨分支（All）；Refs 为 --decorate 的引用摘要
// （0x1f 分隔，防 subject 含分隔符）。
func (s *GitService) Log(ctx context.Context, req *domain.GitLogREQ) []domain.GitCommitRESP {
	limit, skip, all := 20, 0, false
	if req != nil {
		if req.Limit > 0 {
			limit = req.Limit
		}
		if req.Skip > 0 {
			skip = req.Skip
		}
		all = req.All
	}
	args := []string{"log", "-n", fmt.Sprint(limit)}
	if skip > 0 {
		args = append(args, "--skip", fmt.Sprint(skip))
	}
	if all {
		args = append(args, "--all")
	}
	args = append(args, "--pretty=format:%h%x1f%an%x1f%cs%x1f%s%x1f%D")
	out := mustGit(s.git(ctx, args...))
	if out == "" {
		return []domain.GitCommitRESP{}
	}
	commits := make([]domain.GitCommitRESP, 0, limit)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\x1f")
		if len(parts) != 5 {
			continue
		}
		commits = append(commits, domain.GitCommitRESP{
			Hash: parts[0], Author: parts[1], Date: parts[2], Subject: parts[3],
			Refs: strings.TrimSpace(parts[4]),
		})
	}
	return commits
}

// CommitDetail 提交详情：元信息 + 变更文件清单（文件名 / 状态 / 增删行数）。
//
// 用两次 git 调用而不是解析 --stat：numstat 给机器可读的行数、name-status 给准确的
// A/M/D/R 状态；只靠 numstat 的增删数推断状态，会把「纯追加的修改」误判成新增文件。
func (s *GitService) CommitDetail(ctx context.Context, hash string) (*domain.GitCommitDetailRESP, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, domain.ErrGitCommitHash
	}
	meta, err := s.git(ctx, "show", "-s", "--format=%H%x1f%h%x1f%an%x1f%ae%x1f%cs%x1f%s%x1f%P%x1f%D%x1f%b", hash)
	if err != nil {
		return nil, err
	}
	// %b 是最后一个字段：正文里的换行不会影响其它字段的切分
	parts := strings.SplitN(strings.TrimRight(meta, "\n"), "\x1f", 9)
	if len(parts) < 8 {
		return nil, domain.ErrGitFailed
	}
	out := &domain.GitCommitDetailRESP{
		Hash:        parts[0],
		ShortHash:   parts[1],
		Author:      parts[2],
		AuthorEmail: parts[3],
		Date:        parts[4],
		Subject:     parts[5],
		Refs:        strings.TrimSpace(parts[7]),
	}
	if p := strings.Fields(parts[6]); len(p) > 0 {
		for _, h := range p {
			if len(h) > 7 {
				h = h[:7]
			}
			out.Parents = append(out.Parents, h)
		}
	}
	if len(parts) == 9 {
		out.Body = strings.TrimSpace(parts[8])
	}
	out.Files = s.commitFiles(ctx, hash)
	for _, f := range out.Files {
		out.TotalAdded += f.Added
		out.TotalRemoved += f.Removed
	}
	return out, nil
}

// commitFiles 合并 name-status 与 numstat：前者给状态，后者给增删行数（按路径对齐）。
func (s *GitService) commitFiles(ctx context.Context, hash string) []domain.GitCommitFileRESP {
	type stat struct {
		added, removed int
		binary         bool
	}
	stats := map[string]stat{}
	if out := mustGit(s.git(ctx, "diff-tree", "--no-commit-id", "-r", "-M", "--root", "--numstat", hash)); out != "" {
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			cols := strings.SplitN(line, "\t", 3)
			if len(cols) != 3 {
				continue
			}
			_, path := splitRenamePath(cols[2])
			st := stat{}
			if cols[0] == "-" || cols[1] == "-" {
				st.binary = true
			} else {
				st.added = atoiSafe(cols[0])
				st.removed = atoiSafe(cols[1])
			}
			stats[path] = st
		}
	}
	out := mustGit(s.git(ctx, "diff-tree", "--no-commit-id", "-r", "-M", "--root", "--name-status", hash))
	if out == "" {
		return []domain.GitCommitFileRESP{}
	}
	files := make([]domain.GitCommitFileRESP, 0, 8)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) < 2 {
			continue
		}
		f := domain.GitCommitFileRESP{Status: strings.ToUpper(cols[0][:1])}
		if len(cols) == 3 {
			// R100 old new
			f.OrigPath, f.Path = cols[1], cols[2]
		} else {
			f.Path = cols[1]
		}
		if st, ok := stats[f.Path]; ok {
			f.Added, f.Removed, f.Binary = st.added, st.removed, st.binary
		}
		files = append(files, f)
	}
	return files
}

// CommitFileDiff 单次提交内某文件的 unified diff（重命名按新路径查询）。
func (s *GitService) CommitFileDiff(ctx context.Context, hash, path string) (string, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return "", domain.ErrGitCommitHash
	}
	args := []string{"show", "--no-color", "--pretty=format:", "-M", hash}
	if strings.TrimSpace(path) != "" {
		args = append(args, "--", path)
	}
	return s.git(ctx, args...)
}

// Graph 提交图谱（只读文本）：--graph --all 的原始输出，前端按等宽字体直接渲染。
// 不自己算图——git 的拓扑排序与分支视觉表达已经是最准确的实现。
func (s *GitService) Graph(ctx context.Context, limit int) (string, error) {
	if limit <= 0 || limit > 500 {
		limit = 120
	}
	out, err := s.git(ctx, "log", "--graph", "--oneline", "--decorate", "--all", "--no-color",
		"--max-count", fmt.Sprint(limit))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// DeleteBranch 删除本地分支（拒绝删除当前分支；force 对应 -D）。
func (s *GitService) DeleteBranch(ctx context.Context, name string, force bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.ErrGitBranch
	}
	current := strings.TrimSpace(mustGit(s.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")))
	if current == name {
		return domain.ErrGitBranchInUse
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := s.git(ctx, "branch", flag, name)
	return err
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
