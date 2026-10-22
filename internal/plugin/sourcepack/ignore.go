package sourcepack

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

const (
	ignoreFileName    = ".sparqignore"
	maxIgnoreFileSize = 1 << 20
)

type ignoreRule struct {
	pattern   string
	negated   bool
	anchored  bool
	directory bool
}

type ignoreMatcher struct {
	rules    []ignoreRule
	required map[string]struct{}
}

func loadIgnoreMatcher(root string, referencedFiles []string) (ignoreMatcher, error) {
	matcher := ignoreMatcher{required: requiredSourcePaths(referencedFiles)}
	ignorePath := filepath.Join(root, ignoreFileName)
	info, err := os.Lstat(ignorePath)
	if os.IsNotExist(err) {
		return matcher, nil
	}
	if err != nil {
		return ignoreMatcher{}, fmt.Errorf("读取 %s 失败: %w", ignoreFileName, err)
	}
	if !info.Mode().IsRegular() {
		return ignoreMatcher{}, fmt.Errorf("%s 必须是普通文件", ignoreFileName)
	}
	if info.Size() > maxIgnoreFileSize {
		return ignoreMatcher{}, fmt.Errorf("%s 不能超过 %d 字节", ignoreFileName, maxIgnoreFileSize)
	}
	file, err := os.Open(ignorePath)
	if err != nil {
		return ignoreMatcher{}, fmt.Errorf("打开 %s 失败: %w", ignoreFileName, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxIgnoreFileSize+1)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		rule, included, parseErr := parseIgnoreRule(scanner.Text())
		if parseErr != nil {
			return ignoreMatcher{}, fmt.Errorf("解析 %s 第 %d 行失败: %w", ignoreFileName, lineNumber, parseErr)
		}
		if included {
			matcher.rules = append(matcher.rules, rule)
		}
	}
	if err := scanner.Err(); err != nil {
		return ignoreMatcher{}, fmt.Errorf("读取 %s 失败: %w", ignoreFileName, err)
	}
	return matcher, nil
}

func parseIgnoreRule(line string) (ignoreRule, bool, error) {
	line = strings.TrimSuffix(line, "\r")
	if line == "" || strings.HasPrefix(line, "#") {
		return ignoreRule{}, false, nil
	}
	escapedNegation := strings.HasPrefix(line, `\!`)
	if strings.HasPrefix(line, `\#`) || escapedNegation {
		line = line[1:]
	}
	rule := ignoreRule{}
	if !escapedNegation && strings.HasPrefix(line, "!") {
		rule.negated = true
		line = line[1:]
	}
	if line == "" {
		return ignoreRule{}, false, fmt.Errorf("反选规则缺少路径")
	}
	rule.anchored = strings.HasPrefix(line, "/")
	line = strings.TrimPrefix(line, "/")
	rule.directory = strings.HasSuffix(line, "/")
	line = strings.TrimSuffix(line, "/")
	line = path.Clean(line)
	if line == "." || line == ".." || strings.HasPrefix(line, "../") || strings.Contains(line, "\\") {
		return ignoreRule{}, false, fmt.Errorf("路径模式无效: %q", line)
	}
	for _, segment := range strings.Split(line, "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, ""); err != nil {
			return ignoreRule{}, false, fmt.Errorf("路径模式 %q 无效: %w", line, err)
		}
	}
	rule.pattern = line
	return rule, true, nil
}

func (matcher ignoreMatcher) excluded(relative string, directory bool) bool {
	if protectedSourcePath(relative, directory) {
		return true
	}
	if _, required := matcher.required[relative]; required {
		return false
	}
	excluded := false
	for _, rule := range matcher.rules {
		if rule.matches(relative, directory) {
			excluded = !rule.negated
		}
	}
	return excluded
}

func (rule ignoreRule) matches(relative string, directory bool) bool {
	segments := strings.Split(relative, "/")
	candidates := make([]string, 0, len(segments))
	limit := len(segments)
	if rule.directory && !directory {
		limit--
	}
	for index := 1; index <= limit; index++ {
		candidates = append(candidates, strings.Join(segments[:index], "/"))
	}
	if !rule.directory && len(candidates) == 0 {
		candidates = append(candidates, relative)
	}
	for _, candidate := range candidates {
		if rule.anchored || strings.Contains(rule.pattern, "/") {
			if matchIgnorePath(rule.pattern, candidate) {
				return true
			}
			continue
		}
		parts := strings.Split(candidate, "/")
		matched, err := path.Match(rule.pattern, parts[len(parts)-1])
		if err == nil && matched {
			return true
		}
	}
	return false
}

func matchIgnorePath(pattern, candidate string) bool {
	return matchIgnoreSegments(strings.Split(pattern, "/"), strings.Split(candidate, "/"))
}

func matchIgnoreSegments(pattern, candidate []string) bool {
	if len(pattern) == 0 {
		return len(candidate) == 0
	}
	if pattern[0] == "**" {
		if matchIgnoreSegments(pattern[1:], candidate) {
			return true
		}
		return len(candidate) > 0 && matchIgnoreSegments(pattern, candidate[1:])
	}
	if len(candidate) == 0 {
		return false
	}
	matched, err := path.Match(pattern[0], candidate[0])
	return err == nil && matched && matchIgnoreSegments(pattern[1:], candidate[1:])
}

func requiredSourcePaths(referencedFiles []string) map[string]struct{} {
	required := map[string]struct{}{
		manifest.DefaultFileName:          {},
		manifest.DefaultGeneratedFileName: {},
		"go.mod":                          {},
		"go.sum":                          {},
		"sparq.build.yaml":                {},
		ignoreFileName:                    {},
	}
	for _, relative := range referencedFiles {
		required[filepath.ToSlash(filepath.Clean(relative))] = struct{}{}
	}
	return required
}

func protectedSourcePath(relative string, directory bool) bool {
	segments := strings.Split(relative, "/")
	for _, segment := range segments {
		if segment == ".git" || segment == "node_modules" || segment == ".docker" || segment == ".aws" || segment == ".ssh" {
			return true
		}
	}
	if directory {
		switch relative {
		case "bin", "release", "signatures", "frontend/dist":
			return true
		}
		return false
	}
	base := filepath.Base(relative)
	if base == ".env" || (strings.HasPrefix(base, ".env.") && !strings.HasSuffix(base, ".example")) {
		return true
	}
	switch base {
	case ".npmrc", ".pypirc", ".netrc", "credentials.json", "service-account.json", "service_account.json",
		"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	switch relative {
	case "checksums.txt", "release.json":
		return true
	}
	lower := strings.ToLower(base)
	return strings.HasSuffix(lower, ".log") || strings.HasSuffix(lower, ".sqlite") ||
		strings.HasSuffix(lower, ".sqlite-shm") || strings.HasSuffix(lower, ".sqlite-wal") ||
		strings.HasSuffix(lower, ".key") || strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx")
}
