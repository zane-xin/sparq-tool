package watch

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

type fileMatcher struct {
	extensions map[string]struct{}
	files      map[string]struct{}
	include    []*regexp.Regexp
	exclude    []*regexp.Regexp
}

func newFileMatcher(options WatchOptions) (fileMatcher, error) {
	include, err := compileGlobs("include", options.Include)
	if err != nil {
		return fileMatcher{}, err
	}
	exclude, err := compileGlobs("exclude_globs", options.ExcludeGlobs)
	if err != nil {
		return fileMatcher{}, err
	}
	return fileMatcher{
		extensions: stringSet(options.Extensions),
		files:      stringSet(options.Files),
		include:    include,
		exclude:    exclude,
	}, nil
}

func (m fileMatcher) matches(root, filename string) bool {
	relative, err := filepath.Rel(root, filename)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	relative = filepath.ToSlash(relative)
	if matchesAny(m.exclude, relative) {
		return false
	}
	if len(m.include) > 0 {
		return matchesAny(m.include, relative)
	}
	if _, ok := m.files[filepath.Base(filename)]; ok {
		return true
	}
	_, ok := m.extensions[filepath.Ext(filename)]
	return ok
}

func compileGlobs(field string, patterns []string) ([]*regexp.Regexp, error) {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
		pattern = strings.TrimPrefix(pattern, "./")
		if pattern == "" {
			return nil, fmt.Errorf("watch.%s 不能包含空 pattern", field)
		}
		compiled, err := regexp.Compile(globRegexp(pattern))
		if err != nil {
			return nil, fmt.Errorf("watch.%s pattern %q 无效: %w", field, pattern, err)
		}
		result = append(result, compiled)
	}
	return result, nil
}

func globRegexp(pattern string) string {
	var expression strings.Builder
	expression.WriteString("^")
	for index := 0; index < len(pattern); {
		switch pattern[index] {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index += 2
				if index < len(pattern) && pattern[index] == '/' {
					expression.WriteString("(?:.*/)?")
					index++
				} else {
					expression.WriteString(".*")
				}
			} else {
				expression.WriteString("[^/]*")
				index++
			}
		case '?':
			expression.WriteString("[^/]")
			index++
		default:
			value, size := utf8.DecodeRuneInString(pattern[index:])
			expression.WriteString(regexp.QuoteMeta(string(value)))
			index += size
		}
	}
	expression.WriteString("$")
	return expression.String()
}

func matchesAny(patterns []*regexp.Regexp, path string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
