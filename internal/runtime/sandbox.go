package runtime

import "path/filepath"

// .workbaby 是「工作区自有数据根」：只承载与当前工作区绑定的会话过程数据
// （会话记忆 / 写前快照 / 过程脚本 / 缓存 / 临时文件），语义类似 .git 之于仓库。
//
// 边界：系统级数据不进工作区 —— 知识库在 {home}/knowledge、日志在 {home}/logs、
// 全局记忆在 {home}/memory、运行事件在 {home}/runs，均由 paths.go 统一管理。
//
// 目录一律「按需创建」：只有真正要写某类数据时才会出现对应子目录，
// 绑定工作区时不会预建空壳目录树（用户工作区保持干净）。
const (
	SandboxDirName = ".workbaby"

	// SubMemory 会话长期记忆（MEMORY.md，按 sessionID 分目录）。
	SubMemory = "memory"
	// SubSnapshots 文件写前快照（可回滚），按 sessionID 分目录。
	SubSnapshots = "snapshots"
	// SubScripts 本工作区内复用的过程脚本。
	SubScripts = "scripts"
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
		Cache:     filepath.Join(root, SubCache),
		Tmp:       filepath.Join(root, SubTmp),
	}
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
	case SubTmp:
		return sb.Tmp
	}
	return sb.Root
}
