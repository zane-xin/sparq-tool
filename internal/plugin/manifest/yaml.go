package manifest

import (
	"bytes"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

func parseYAML(data []byte) (Manifest, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var item Manifest
	if err := decoder.Decode(&item); err != nil {
		return Manifest{}, fmt.Errorf("解析 YAML 插件清单失败: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Manifest{}, fmt.Errorf("解析 YAML 插件清单失败: 只能包含一个文档")
		}
		return Manifest{}, fmt.Errorf("解析 YAML 插件清单失败: %w", err)
	}
	return item, nil
}
