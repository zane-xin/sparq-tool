package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (item *Manifest) loadSchemaResourceFiles(root string) error {
	seen := make(map[string]struct{}, len(item.Spec.Contributions.SchemaResources))
	for index := range item.Spec.Contributions.SchemaResources {
		resource := item.Spec.Contributions.SchemaResources[index]
		fieldPath := fmt.Sprintf("spec.contributions.schema_resources.%d", index)
		if resource.File == "" {
			return fmt.Errorf("%s.file: Schema Resource 必须通过插件内 JSON 文件声明", fieldPath)
		}
		if hasInlineSchemaResource(resource) {
			return fmt.Errorf("%s.file: file 不能与 resource、table_name、schema 或 action_permissions 同时声明", fieldPath)
		}
		reference, filename, err := resolveSchemaResourceFile(root, resource.File)
		if err != nil {
			return fmt.Errorf("%s.file: %w", fieldPath, err)
		}
		if _, exists := seen[reference]; exists {
			return fmt.Errorf("%s.file: 外部 Schema Resource 文件重复: %s", fieldPath, reference)
		}
		seen[reference] = struct{}{}

		loaded, err := readSchemaResourceFile(filename)
		if err != nil {
			return fmt.Errorf("%s.file: %w", fieldPath, err)
		}
		if loaded.File != "" {
			return fmt.Errorf("%s.file: 外部 Schema Resource 文件不能继续引用 file", fieldPath)
		}
		if resource.MigrationOwner != "" {
			loaded.MigrationOwner = resource.MigrationOwner
		}
		if resource.RuntimeOwner != "" {
			loaded.RuntimeOwner = resource.RuntimeOwner
		}
		loaded.File = reference
		item.Spec.Contributions.SchemaResources[index] = loaded
	}
	return nil
}

func hasInlineSchemaResource(resource SchemaResourceContributionManifest) bool {
	return resource.Resource != "" || resource.TableName != "" || resource.Schema != nil || resource.ActionPermissions != nil
}

func resolveSchemaResourceFile(root string, reference string) (string, string, error) {
	if strings.Contains(reference, "\\") {
		return "", "", fmt.Errorf("外部 Schema Resource 路径必须使用正斜杠")
	}
	cleaned := path.Clean(reference)
	if cleaned == "." || path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", "", fmt.Errorf("外部 Schema Resource 路径必须位于插件目录内: %s", reference)
	}
	if strings.ToLower(path.Ext(cleaned)) != ".json" {
		return "", "", fmt.Errorf("外部 Schema Resource 必须使用 .json 文件: %s", reference)
	}

	rootPath, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("解析插件目录 %s: %w", root, err)
	}
	filename := filepath.Join(rootPath, filepath.FromSlash(cleaned))
	realRoot, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return "", "", fmt.Errorf("解析插件目录 %s: %w", rootPath, err)
	}
	realFilename, err := filepath.EvalSymlinks(filename)
	if err != nil {
		return "", "", fmt.Errorf("读取外部 Schema Resource %s: %w", cleaned, err)
	}
	if !withinDirectory(realRoot, realFilename) {
		return "", "", fmt.Errorf("外部 Schema Resource 不能通过符号链接离开插件目录: %s", reference)
	}
	return cleaned, filename, nil
}

func withinDirectory(root string, filename string) bool {
	relative, err := filepath.Rel(root, filename)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func readSchemaResourceFile(filename string) (SchemaResourceContributionManifest, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return SchemaResourceContributionManifest{}, fmt.Errorf("读取外部 Schema Resource %s: %w", filename, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var resource SchemaResourceContributionManifest
	if err := decoder.Decode(&resource); err != nil {
		return SchemaResourceContributionManifest{}, fmt.Errorf("解析外部 Schema Resource %s: %w", filename, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return SchemaResourceContributionManifest{}, fmt.Errorf("解析外部 Schema Resource %s: 只能包含一个 JSON 对象", filename)
		}
		return SchemaResourceContributionManifest{}, fmt.Errorf("解析外部 Schema Resource %s: %w", filename, err)
	}
	return resource, nil
}
