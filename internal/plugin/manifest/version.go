package manifest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Version 是经过严格校验的 SemVer 版本号。
type Version struct {
	major      uint64
	minor      uint64
	patch      uint64
	prerelease string
	build      string
}

// ParseVersion 解析 MAJOR.MINOR.PATCH 及标准的预发布、构建元数据后缀。
func ParseVersion(value string) (Version, error) {
	if value == "" {
		return Version{}, fmt.Errorf("版本号不能为空")
	}
	if value != strings.TrimSpace(value) {
		return Version{}, invalidVersion(value)
	}
	coreAndBuild := strings.Split(value, "+")
	if len(coreAndBuild) > 2 {
		return Version{}, invalidVersion(value)
	}
	coreAndPrerelease := strings.SplitN(coreAndBuild[0], "-", 2)
	parts := strings.Split(coreAndPrerelease[0], ".")
	if len(parts) != 3 {
		return Version{}, invalidVersion(value)
	}
	major, err := parseCoreNumber(parts[0])
	if err != nil {
		return Version{}, invalidVersion(value)
	}
	minor, err := parseCoreNumber(parts[1])
	if err != nil {
		return Version{}, invalidVersion(value)
	}
	patch, err := parseCoreNumber(parts[2])
	if err != nil {
		return Version{}, invalidVersion(value)
	}
	parsed := Version{major: major, minor: minor, patch: patch}
	if len(coreAndPrerelease) == 2 {
		if err := validateIdentifiers(coreAndPrerelease[1], true); err != nil {
			return Version{}, invalidVersion(value)
		}
		parsed.prerelease = coreAndPrerelease[1]
	}
	if len(coreAndBuild) == 2 {
		if err := validateIdentifiers(coreAndBuild[1], false); err != nil {
			return Version{}, invalidVersion(value)
		}
		parsed.build = coreAndBuild[1]
	}
	return parsed, nil
}

func invalidVersion(value string) error {
	return fmt.Errorf("版本号 %q 不是有效的 SemVer，例如 1.2.3 或 1.2.3-beta.1", value)
}

func parseCoreNumber(value string) (uint64, error) {
	if value == "" || len(value) > 1 && value[0] == '0' {
		return 0, fmt.Errorf("invalid number")
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("invalid number")
		}
	}
	return strconv.ParseUint(value, 10, 64)
}

func validateIdentifiers(value string, rejectLeadingZero bool) error {
	if value == "" {
		return fmt.Errorf("empty identifiers")
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return fmt.Errorf("empty identifier")
		}
		numeric := true
		for _, character := range identifier {
			if character < '0' || character > '9' {
				numeric = false
			}
			if !isIdentifierCharacter(character) {
				return fmt.Errorf("invalid identifier")
			}
		}
		if rejectLeadingZero && numeric && len(identifier) > 1 && identifier[0] == '0' {
			return fmt.Errorf("numeric prerelease identifier has a leading zero")
		}
	}
	return nil
}

func isIdentifierCharacter(character rune) bool {
	return character >= '0' && character <= '9' ||
		character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' || character == '-'
}

func (version Version) String() string {
	value := fmt.Sprintf("%d.%d.%d", version.major, version.minor, version.patch)
	if version.prerelease != "" {
		value += "-" + version.prerelease
	}
	if version.build != "" {
		value += "+" + version.build
	}
	return value
}

// Bump 返回清除预发布和构建后缀后的下一个 patch、minor 或 major 版本。
func (version Version) Bump(kind string) (Version, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "patch":
		if version.patch == ^uint64(0) {
			return Version{}, fmt.Errorf("patch 版本号已达到上限")
		}
		return Version{major: version.major, minor: version.minor, patch: version.patch + 1}, nil
	case "minor":
		if version.minor == ^uint64(0) {
			return Version{}, fmt.Errorf("minor 版本号已达到上限")
		}
		return Version{major: version.major, minor: version.minor + 1}, nil
	case "major":
		if version.major == ^uint64(0) {
			return Version{}, fmt.Errorf("major 版本号已达到上限")
		}
		return Version{major: version.major + 1}, nil
	default:
		return Version{}, fmt.Errorf("版本递增类型必须是 patch、minor 或 major")
	}
}

// UpdateVersion 原子更新 plugin.yaml 的 metadata.version，并保留 YAML 注释。
func UpdateVersion(path string, target Version) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取插件清单失败: %w", err)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&document); err != nil {
		return "", fmt.Errorf("解析 YAML 插件清单失败: %w", err)
	}
	if len(document.Content) != 1 {
		return "", fmt.Errorf("解析 YAML 插件清单失败: 根节点必须是对象")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("解析 YAML 插件清单失败: 只能包含一个文档")
		}
		return "", fmt.Errorf("解析 YAML 插件清单失败: %w", err)
	}
	versionNode, err := mappingValue(document.Content[0], "metadata", "version")
	if err != nil {
		return "", err
	}
	current, err := ParseVersion(versionNode.Value)
	if err != nil {
		return "", fmt.Errorf("plugin.yaml metadata.version 无效: %w", err)
	}
	if current.String() == target.String() {
		return current.String(), nil
	}
	versionNode.Value = target.String()
	versionNode.Tag = "!!str"
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return "", fmt.Errorf("编码 YAML 插件清单失败: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("编码 YAML 插件清单失败: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("检查插件清单失败: %w", err)
	}
	if err := writeVersionAtomic(path, encoded.Bytes(), info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("更新插件清单版本失败: %w", err)
	}
	return current.String(), nil
}

func mappingValue(node *yaml.Node, path ...string) (*yaml.Node, error) {
	current := node
	for index, key := range path {
		if current.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("plugin.yaml %s 必须是对象", strings.Join(path[:index], "."))
		}
		var next *yaml.Node
		for position := 0; position+1 < len(current.Content); position += 2 {
			if current.Content[position].Value == key {
				next = current.Content[position+1]
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("plugin.yaml 缺少 %s", strings.Join(path[:index+1], "."))
		}
		current = next
	}
	if current.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("plugin.yaml %s 必须是字符串", strings.Join(path, "."))
	}
	return current, nil
}

func writeVersionAtomic(path string, body []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".plugin-version-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
