package runtime

import (
	"os"
	"path/filepath"
)

// .workbaby 是「工作区自有数据根」：只承载与当前工作区绑定的会话过程数据
// （会话记忆 / 写前快照 / 过程脚本 / 产出物 / 缓存 / 临时文件），语义类似 .git 之于仓库。
//
// 边界：系统级数据不进工作区 —— 知识库在 {home}/knowledge、日志在 {home}/logs、
// 全局记忆在 {home}/memory、运行事件在 {home}/runs，均由 paths.go 统一管理。
//
// 目录在绑定工作区那一刻就由 Ensure 建立：先有明确的落点，工具与模型才不会
// 把过程数据写进用户项目根，污染原有结构。
const (
	SandboxDirName = ".workbaby"

	// SubMemory 会话长期记忆（MEMORY.md，按 sessionID 分目录）。
	SubMemory = "memory"
	// SubSnapshots 文件写前快照（可回滚），按 sessionID 分目录。
	SubSnapshots = "snapshots"
	// SubScripts 本工作区内复用的过程脚本。
	SubScripts = "scripts"
	// SubOutput 本工作区的产出物（报告 / 导出 / 生成文件）。
	SubOutput = "output"
	// SubCache 本工作区的派生缓存（可重建，删了不影响正确性）。
	SubCache = "cache"
	// SubTmp 执行期临时文件（如 skill 脚本落盘），用完即删。
	SubTmp = "tmp"
)

// Sandbox 描述一个工作区的 .workbaby 目录集合。
type Sandbox struct {
	Root      string // <workspace>/.workbaby
	Memory    string
	Snapshots string
	Scripts   string
	Output    string
	Cache     string
	Tmp       string
}

// SandboxOf 计算沙箱各路径（不创建、不访问磁盘）。workspace 为空时返回零值。
func SandboxOf(workspace string) Sandbox {
	if workspace == "" {
		return Sandbox{}
	}
	root := filepath.Join(workspace, SandboxDirName)
	return Sandbox{
		Root:      root,
		Memory:    filepath.Join(root, SubMemory),
		Snapshots: filepath.Join(root, SubSnapshots),
		Scripts:   filepath.Join(root, SubScripts),
		Output:    filepath.Join(root, SubOutput),
		Cache:     filepath.Join(root, SubCache),
		Tmp:       filepath.Join(root, SubTmp),
	}
}

// Ensure 建立沙箱根与其下全部子目录，并写入 .gitignore 让整棵沙箱树对 Git 不可见。
// 幂等；workspace 未绑定（零值）时直接返回。
func (s Sandbox) Ensure() error {
	if s.Root == "" {
		return nil
	}
	for _, d := range []string{s.Root, s.Memory, s.Snapshots, s.Scripts, s.Output, s.Cache} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	mark := filepath.Join(s.Root, ".gitignore")
	if _, err := os.Stat(mark); err == nil {
		return nil
	}
	return os.WriteFile(mark, []byte("*\n"), 0o644)
}

// SandboxFile 返回沙箱子目录绝对路径；workspace 为空时返回空串（调用方据此回落系统临时目录）。
func SandboxFile(workspace, sub string) string {
	sb := SandboxOf(workspace)
	if sb.Root == "" {
		return ""
	}
	switch sub {
	case SubMemory:
		return sb.Memory
	case SubSnapshots:
		return sb.Snapshots
	case SubScripts:
		return sb.Scripts
	case SubCache:
		return sb.Cache
	case SubOutput:
		return sb.Output
	case SubTmp:
		return sb.Tmp
	}
	return sb.Root
}
