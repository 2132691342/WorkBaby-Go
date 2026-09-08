//go:build windows

package exec

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveCommand 把命令名解析为可直接 CreateProcess 的可执行文件与其前置参数。
//
// Windows 上有两类命令无法直接 CreateProcess，必须包一层 cmd /c：
//   - .cmd / .bat 脚本外壳（npm / npx / uvx 的真实形态），直接启动报 ERROR_BAD_EXE_FORMAT(193)；
//   - dir / type / echo / copy 等 cmd 内建命令：没有对应 exe，LookPath 必然失败。
//
// 白名单校验仍针对调用方传入的原始 command（用户意图的 basename），
// 包 shell 只是执行侧的实现细节，不因此放宽白名单。
func resolveCommand(name string) (string, []string) {
	lp, err := exec.LookPath(name)
	if err != nil {
		return "cmd", []string{"/c", name}
	}
	if ext := strings.ToLower(filepath.Ext(lp)); ext == ".cmd" || ext == ".bat" {
		return "cmd", []string{"/c", lp}
	}
	return lp, nil
}
