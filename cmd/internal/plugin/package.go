package plugincmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	protocolplugin "github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
	"github.com/zane-xin/sparq-tool/internal/release/packager"
)

func newPackageCommand() *cobra.Command {
	var dir string
	var output string
	var manifestJSON string
	command := &cobra.Command{
		Use:   "package [path]",
		Short: "将已构建产物生成本地插件包",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) > 0 {
				dir = args[0]
			}
			result, err := packagePlugin(pluginPackageOptions{Dir: dir, Output: output, ManifestJSON: manifestJSON})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "插件包已生成: %s\n包内校验清单: %s\n", result.ArchivePath, result.ChecksumPath)
			return err
		},
	}
	command.Flags().StringVar(&dir, "dir", ".", "插件项目目录")
	command.Flags().StringVar(&output, "output", "release", "插件包输出目录")
	command.Flags().StringVar(&manifestJSON, "manifest-json", "", "使用已经构建并校验的完整协议 Manifest JSON")
	return command
}

type pluginPackageOptions struct {
	Dir          string
	Output       string
	ManifestJSON string
	DeliveryKind string
	Runtime      string
	TargetOS     string
	TargetArch   string
	BackendPath  string
	FrontendPath string
	FrontendOnly bool
}

type pluginPackageResult struct {
	ArchivePath  string
	ChecksumPath string
	Kind         string
	Runtime      string
	TargetOS     string
	TargetArch   string
}

func packagePlugin(options pluginPackageOptions) (pluginPackageResult, error) {
	dir := commandDefault(strings.TrimSpace(options.Dir), ".")
	output := commandDefault(strings.TrimSpace(options.Output), "release")
	root, err := filepath.Abs(dir)
	if err != nil {
		return pluginPackageResult{}, fmt.Errorf("解析插件目录失败: %w", err)
	}
	item, err := manifest.Load(filepath.Join(root, manifest.DefaultFileName))
	if err != nil {
		return pluginPackageResult{}, err
	}
	manifestJSON, err := packageManifestJSON(item, root, options.ManifestJSON)
	if err != nil {
		return pluginPackageResult{}, err
	}
	result, err := packager.PackagePlugin(packager.PluginPackageOptions{
		SourceDir:       root,
		OutputDir:       filepath.Join(root, output),
		Code:            item.Metadata.Code,
		Name:            item.Metadata.Name,
		Version:         item.Metadata.Version,
		ProtocolRange:   item.Sparq.Protocol,
		ManifestJSON:    manifestJSON,
		ReferencedFiles: item.ReferencedFiles(),
		DeliveryKind:    options.DeliveryKind,
		Runtime:         options.Runtime,
		TargetOS:        options.TargetOS,
		TargetArch:      options.TargetArch,
		BackendPath:     options.BackendPath,
		FrontendPath:    options.FrontendPath,
		FrontendOnly:    options.FrontendOnly,
	})
	if err != nil {
		return pluginPackageResult{}, err
	}
	return pluginPackageResult{
		ArchivePath: result.ArchivePath, ChecksumPath: result.ChecksumPath,
		Kind: options.DeliveryKind, Runtime: options.Runtime,
		TargetOS: options.TargetOS, TargetArch: options.TargetArch,
	}, nil
}

func packageManifestJSON(item manifest.Manifest, root, source string) ([]byte, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		builtPath := filepath.Join(root, "release", "manifest.json")
		if regularFile(builtPath) {
			source = builtPath
		} else {
			usesCompose, err := manifest.UsesManifestModule(filepath.Join(root, manifest.DefaultFileName))
			if err != nil {
				return nil, err
			}
			if usesCompose {
				return nil, fmt.Errorf("插件使用 manifest.Compose，但缺少 %s，请先运行 plugin build", builtPath)
			}
		}
	}
	if source == "" {
		protocolManifest, err := item.ProtocolManifest()
		if err != nil {
			return nil, fmt.Errorf("转换插件协议清单失败: %w", err)
		}
		value, err := json.MarshalIndent(protocolManifest, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("编码插件协议清单失败: %w", err)
		}
		return value, nil
	}
	payload, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("读取完整插件协议清单失败: %w", err)
	}
	var value protocolplugin.Manifest
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, fmt.Errorf("解析完整插件协议清单失败: %w", err)
	}
	if err := protocolplugin.ValidateManifest(value); err != nil {
		return nil, fmt.Errorf("校验完整插件协议清单失败: %w", err)
	}
	if string(value.Code) != item.Metadata.Code || value.Version != item.Metadata.Version {
		return nil, fmt.Errorf("完整插件协议清单身份不一致: 得到 %s@%s，主清单要求 %s@%s", value.Code, value.Version, item.Metadata.Code, item.Metadata.Version)
	}
	result, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码完整插件协议清单失败: %w", err)
	}
	return result, nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func commandDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
