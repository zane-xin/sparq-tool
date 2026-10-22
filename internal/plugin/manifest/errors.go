package manifest

import "strings"

// Issue 描述插件清单校验问题。
type Issue struct {
	Path    string
	Message string
}

// ValidationError 聚合插件清单校验问题。
type ValidationError struct {
	Issues []Issue
}

func (err ValidationError) Error() string {
	if len(err.Issues) == 0 {
		return "插件清单校验失败"
	}
	parts := make([]string, 0, len(err.Issues))
	for _, issue := range err.Issues {
		parts = append(parts, issue.Path+" "+issue.Message)
	}
	return strings.Join(parts, "; ")
}
