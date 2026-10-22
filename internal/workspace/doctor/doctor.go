package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

const (
	StatusOK   = "OK"
	StatusWarn = "WARN"
	StatusFail = "FAIL"
)

// Check 描述一项 doctor 检查结果。
type Check struct {
	Name   string
	Status string
	Detail string
}

// Run 执行 doctor 检查。
func Run(manifestPath string) []Check {
	return []Check{
		checkCommand("Go", "go", "version"),
		checkTarget(),
		checkOptionalCommand("Node", "node", "--version"),
		checkPackageManager(),
		checkCommand("Git", "git", "--version"),
		checkWorkspace(),
		checkManifest(manifestPath),
	}
}

func checkCommand(name string, command string, args ...string) Check {
	if _, err := exec.LookPath(command); err != nil {
		return Check{Name: name, Status: StatusFail, Detail: command + " 未安装或不在 PATH 中"}
	}
	output, err := exec.Command(command, args...).CombinedOutput()
	if err != nil {
		return Check{Name: name, Status: StatusFail, Detail: strings.TrimSpace(string(output))}
	}
	return Check{Name: name, Status: StatusOK, Detail: strings.TrimSpace(string(output))}
}

func checkOptionalCommand(name string, command string, args ...string) Check {
	if _, err := exec.LookPath(command); err != nil {
		return Check{Name: name, Status: StatusWarn, Detail: command + " 未安装或不在 PATH 中"}
	}
	output, err := exec.Command(command, args...).CombinedOutput()
	if err != nil {
		return Check{Name: name, Status: StatusWarn, Detail: strings.TrimSpace(string(output))}
	}
	return Check{Name: name, Status: StatusOK, Detail: strings.TrimSpace(string(output))}
}

func checkPackageManager() Check {
	for _, command := range []string{"pnpm", "npm"} {
		if _, err := exec.LookPath(command); err != nil {
			continue
		}
		output, err := exec.Command(command, "--version").CombinedOutput()
		if err != nil {
			return Check{Name: "前端包管理器", Status: StatusWarn, Detail: strings.TrimSpace(string(output))}
		}
		return Check{Name: "前端包管理器", Status: StatusOK, Detail: command + " " + strings.TrimSpace(string(output))}
	}
	return Check{Name: "前端包管理器", Status: StatusWarn, Detail: "未发现 pnpm 或 npm"}
}

func checkTarget() Check {
	return Check{Name: "目标平台", Status: StatusOK, Detail: runtime.GOOS + "/" + runtime.GOARCH}
}

func checkWorkspace() Check {
	current, err := os.Getwd()
	if err != nil {
		return Check{Name: "工作区", Status: StatusFail, Detail: err.Error()}
	}
	if exists(filepath.Join(current, "go.work")) {
		return Check{Name: "工作区", Status: StatusOK, Detail: "发现 go.work"}
	}
	if exists(filepath.Join(current, "go.mod")) {
		return Check{Name: "工作区", Status: StatusOK, Detail: "发现 go.mod"}
	}
	return Check{Name: "工作区", Status: StatusWarn, Detail: "未发现 go.work 或 go.mod"}
}

func checkManifest(path string) Check {
	path = strings.TrimSpace(path)
	if path == "" {
		path = manifest.DefaultFileName
	}
	if !exists(path) {
		return Check{Name: "插件清单", Status: StatusWarn, Detail: "未发现 " + path + "，已跳过"}
	}
	item, err := manifest.Load(path)
	if err != nil {
		return Check{Name: "插件清单", Status: StatusFail, Detail: err.Error()}
	}
	return Check{Name: "插件清单", Status: StatusOK, Detail: item.Metadata.Code + " " + item.Metadata.Version}
}

// HasFailed 报告检查结果中是否存在失败项。
func HasFailed(checks []Check) bool {
	for _, check := range checks {
		if check.Status == StatusFail {
			return true
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
