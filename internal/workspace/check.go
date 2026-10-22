package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Status 表示工作区检查项状态。
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// CheckItem 描述单个工作区检查结果。
type CheckItem struct {
	Name    string
	Status  Status
	Message string
}

// CheckOptions 描述本地源码插件接入环境检查参数。
type CheckOptions struct {
	Root       string
	AppDir     string
	PluginsDir string
}

// CheckResult 汇总工作区检查结果。
type CheckResult struct {
	Root  string
	Items []CheckItem
}

// HasFailure 返回是否存在阻塞 local build 的错误。
func (r CheckResult) HasFailure() bool {
	for _, item := range r.Items {
		if item.Status == StatusFail {
			return true
		}
	}
	return false
}

// Check 校验 Sparq 工作区是否符合工具托管的基本约定。
func Check(options CheckOptions) (CheckResult, error) {
	root := strings.TrimSpace(options.Root)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return CheckResult{}, err
	}
	appDir := defaultString(strings.TrimSpace(options.AppDir), "sparq-app")
	pluginsDir := defaultString(strings.TrimSpace(options.PluginsDir), "plugins")
	result := CheckResult{Root: absRoot}
	goWork := filepath.Join(absRoot, "go.work")
	if exists(goWork) {
		result.add(StatusOK, "go.work", "已找到 go.work")
		content, err := os.ReadFile(goWork)
		if err != nil {
			result.add(StatusFail, "go.work 内容", err.Error())
		} else {
			text := string(content)
			result.requireContains("go.work module", text, "./"+filepath.ToSlash(appDir))
			result.requireContains("go.work tool", text, "./sparq-tool")
		}
	} else {
		result.add(StatusFail, "go.work", "工作区根目录缺少 go.work")
	}
	appPath := filepath.Join(absRoot, appDir)
	result.requirePath("app 目录", appPath)
	result.requirePath("app go.mod", filepath.Join(appPath, "go.mod"))
	result.requirePath("app 启动入口", filepath.Join(appPath, "cmd", "sparq", "main.go"))
	if exists(filepath.Join(appPath, "cmd", "sparq", "plugins_gen.go")) {
		result.add(StatusOK, "local", "已找到工具托管的本地插件 Package 装配文件")
	} else {
		result.add(StatusWarn, "local", "未找到 app/cmd/sparq/plugins_gen.go，local build 不会链接本地插件")
	}
	if exists(filepath.Join(absRoot, pluginsDir)) {
		result.add(StatusOK, "plugins 目录", "已找到插件目录")
	} else {
		result.add(StatusWarn, "plugins 目录", "未找到插件目录，后续 local install 会创建")
	}
	return result, nil
}

func (r *CheckResult) requirePath(name string, path string) {
	if exists(path) {
		r.add(StatusOK, name, path)
		return
	}
	r.add(StatusFail, name, fmt.Sprintf("缺少 %s", path))
}

func (r *CheckResult) requireContains(name string, text string, value string) {
	if strings.Contains(text, value) {
		r.add(StatusOK, name, "已声明 "+value)
		return
	}
	r.add(StatusFail, name, "go.work 未声明 "+value)
}

func (r *CheckResult) add(status Status, name string, message string) {
	r.Items = append(r.Items, CheckItem{
		Name:    name,
		Status:  status,
		Message: message,
	})
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
