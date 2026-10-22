package scaffold

const pluginYAMLTemplate = `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: {{ .Name }}
  code: {{ .Name }}
  name: {{ printf "%q" .Title }}
  version: 0.1.0
  description: {{ printf "%q" (printf "%s 插件。" .Title) }}
  # authors 表示插件发布者或维护者，请由插件开发者显式填写。
  # authors:
  #   - name: 维护者名称
  #     email: 可选联系邮箱
sparq:
  protocol: ">=0.1.0 <0.2.0"
`

const goModTemplate = `module {{ .Module }}

go 1.26.1

require github.com/zane-xin/sparq-protocol v0.0.0
`

const buildConfigTemplate = `version: 1

go:
  optimization: size

frontend:
  package_manager: pnpm
  script: build
  output: dist
`

const sourceIgnoreTemplate = `# 这里的规则会在 plugin publish 生成审核源码包时生效。
.DS_Store
.idea/
.vscode/
coverage/
tmp/
`

const readmeTemplate = `# {{ .Title }}

该插件由 ` + "`sparq-tool plugin init`" + ` 生成。

- ` + "`plugin.yaml`" + ` 只保存插件身份、介绍和协议兼容范围。
- ` + "`metadata.authors`" + ` 表示插件发布者或维护者，由插件开发者填写，不与安装用户绑定。
- ` + "`app/manifest/manifest.go`" + ` 通过固定的 ` + "`Compose`" + ` 函数组合组件、入口和宿主贡献。
- ` + "`bootstrap.go`" + ` 提供插件 code，并将实例装配委托给 ` + "`app/lifecycle`" + `。
- ` + "`app/lifecycle/register.go`" + ` 是插件运行时装配入口。按真实能力在这里构造服务，并注册 Provider 或 Route。
- ` + "`app/lifecycle/instance.go`" + ` 保存需要跨生命周期阶段复用的依赖；初始实例只实现协议要求的启动和停止方法。
{{ if .HasFrontend }}- ` + "`frontend/`" + ` 提供统一的 ` + "`/plugins/{{ .Name }}/`" + ` 管理前端入口。
{{ end }}
## 校验

` + "```bash" + `
sparq-tool manifest validate plugin.yaml
sparq-tool manifest generate plugin.yaml --check
go test ./...
` + "```" + `
{{ if .HasHTTP }}
## 联调

` + "`sparq.dev.example.yaml`" + ` 是配置模板，不包含对当前仓库目录的假设。根据注释调整宿主、插件和前端参数后，将它另存为实际开发配置：

` + "```bash" + `
cp sparq.dev.example.yaml sparq.dev.yaml
sparq-tool dev --file sparq.dev.yaml
` + "```" + `

` + "`plugin.secret`" + ` 必须与宿主插件配置中的凭证一致；示例值不得用于生产环境。
{{ end }}`

const bootstrapTemplate = `package {{ .PackageName }}

import (
	"context"

	"github.com/zane-xin/sparq-protocol/plugin"
	"{{ .Module }}/app/lifecycle"
)

const PluginCode = "{{ .Name }}"

// Register 将当前插件交给应用生命周期层装配。
func Register(ctx context.Context, runtime plugin.Runtime) (plugin.Instance, error) {
	return lifecycle.Register(ctx, runtime)
}
`

const registerTemplate = `package lifecycle

import (
	"context"

	"github.com/zane-xin/sparq-protocol/plugin"
)

// Register 是插件唯一的运行时装配入口；新增能力时在这里按依赖顺序完成构造和注册。
func Register(_ context.Context, _ plugin.Runtime) (plugin.Instance, error) {
	return Instance{}, nil
}
`

const instanceTemplate = `package lifecycle

import "context"

// Instance 是插件的最小生命周期实例；需要后台资源时由 Start 和 Stop 统一管理。
type Instance struct{}

// Start 在宿主启用插件后启动后台资源；初始实例没有需要启动的资源。
func (Instance) Start(context.Context) error { return nil }

// Stop 在宿主停用插件时释放后台资源；初始实例没有需要释放的资源。
func (Instance) Stop(context.Context) error { return nil }
`

const manifestModuleTemplate = `package manifest

import (
	"fmt"

	"github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-protocol/protocol"
)

const (
	BackendComponentID protocol.ComponentID = "backend"
{{- if .HasFrontend }}
	AdminComponentID protocol.ComponentID = "admin"
{{- end }}
)

// Compose 将当前插件的 Go 声明组合到 plugin.yaml 提供的基础元数据。
func Compose(base plugin.Manifest) (plugin.Manifest, error) {
	base.Components = []plugin.ComponentManifest{
		{
			ID: BackendComponentID, Kind: plugin.ComponentKindBackend, Language: "go",
			Runtime: []plugin.RuntimeKind{
{{- if .HasLocal }}
				plugin.RuntimeLocal,
{{- end }}
{{- if .HasHTTP }}
				plugin.RuntimeHTTP,
{{- end }}
			},
		},
{{- if .HasFrontend }}
		{
			ID: AdminComponentID, Kind: plugin.ComponentKindFrontend, Language: "typescript",
			Frontend: &plugin.FrontendComponent{},
		},
	{{- end }}
	}
{{- if .HasFrontend }}
	base.Frontend = &plugin.FrontendManifest{Component: AdminComponentID, HostAPI: plugin.FrontendHostAPIV1, RuntimeProfile: plugin.FrontendRuntimeProfileAdminV1}
{{- end }}
	if err := plugin.ValidateManifest(base); err != nil {
		return plugin.Manifest{}, fmt.Errorf("validate {{ .Name }} manifest: %w", err)
	}
	return base, nil
}
`

const remoteMainTemplate = `package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/zane-xin/sparq-protocol/remoteconfig"
	"github.com/zane-xin/sparq-protocol/remoteplugin"
	{{ .PackageName }} {{ printf "%q" .Module }}
	pluginmanifest {{ printf "%q" (printf "%s/app/manifest" .Module) }}
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	config := remoteconfig.Load(remoteconfig.Options{Prefix: "{{ .EnvPrefix }}", SharedPrefix: "SPARQ"})
	runner, err := remoteplugin.NewRunner(remoteplugin.RunnerOptions{
		Manifest: {{ .PackageName }}.Manifest(),
		Addr:     config.String(remoteconfig.Addr),
		Runtime: remoteplugin.Options{
			AppURL: config.String(remoteconfig.AppURL), BaseURL: config.String(remoteconfig.BaseURL),
			PluginID: {{ .PackageName }}.PluginCode, ComponentID: pluginmanifest.BackendComponentID,
			Secret: config.String(remoteconfig.Secret), Config: config,
		},
	})
	if err != nil {
		panic(err)
	}
	instance, err := {{ .PackageName }}.Register(ctx, runner.Runtime())
	if err != nil {
		panic(err)
	}
	if err := runner.Run(ctx, instance); err != nil {
		panic(err)
	}
}
`
