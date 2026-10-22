package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	menuprotocol "github.com/zane-xin/sparq-protocol/contracts/menu"
	permissionprotocol "github.com/zane-xin/sparq-protocol/contracts/permission"
	settingsprotocol "github.com/zane-xin/sparq-protocol/contracts/settings"
	"github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-protocol/protocol"
	protocolsite "github.com/zane-xin/sparq-protocol/site"
)

const (
	// DefaultFileName 是插件清单默认文件名。
	DefaultFileName = "plugin.yaml"
	// APIVersion 是当前工具支持的插件清单版本。
	APIVersion = "sparq.io/v1"
	// KindPlugin 是插件清单资源类型。
	KindPlugin = "Plugin"
)

var pluginCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Manifest 描述 Sparq 插件清单。
type Manifest struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Sparq      Sparq    `json:"sparq" yaml:"sparq"`
	Spec       Spec     `json:"spec" yaml:"spec"`
}

// Metadata 描述插件元数据。
type Metadata struct {
	ID          string   `json:"id" yaml:"id"`
	Code        string   `json:"code" yaml:"code"`
	Name        string   `json:"name" yaml:"name"`
	Version     string   `json:"version" yaml:"version"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Authors     []Author `json:"authors,omitempty" yaml:"authors,omitempty"`
	Docs        string   `json:"docs,omitempty" yaml:"docs,omitempty"`
}

// Author 描述插件作者。
type Author struct {
	Name  string `json:"name" yaml:"name"`
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
}

// Sparq 描述插件与 Sparq 宿主的兼容要求。
type Sparq struct {
	Protocol string `json:"protocol" yaml:"protocol"`
}

// Spec 描述插件运行时声明。
type Spec struct {
	Runtime        Runtime                             `json:"runtime" yaml:"runtime"`
	DependsOn      []protocol.PluginID                 `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Lifecycle      plugin.LifecycleManifest            `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	Components     []plugin.ComponentManifest          `json:"components,omitempty" yaml:"components,omitempty"`
	Entries        []plugin.EntryManifest              `json:"entries,omitempty" yaml:"entries,omitempty"`
	Frontend       *plugin.FrontendManifest            `json:"frontend,omitempty" yaml:"frontend,omitempty"`
	Capabilities   []plugin.FrontendCapability         `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Contributions  ContributionManifest                `json:"contributions,omitempty" yaml:"contributions,omitempty"`
	Provides       CapabilitySet                       `json:"provides,omitempty" yaml:"provides,omitempty"`
	Requires       CapabilitySet                       `json:"requires,omitempty" yaml:"requires,omitempty"`
	SiteDirectives []protocolsite.DirectiveDeclaration `json:"site_directives,omitempty" yaml:"site_directives,omitempty"`
}

// Runtime 描述插件支持的运行时模式。
type Runtime struct {
	Supported []string `json:"supported" yaml:"supported"`
}

// CapabilitySet 保留插件包声明的 capability code 列表。
type CapabilitySet struct {
	Capabilities []string            `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Components   map[string][]string `json:"components,omitempty" yaml:"components,omitempty"`
}

// ContributionManifest 是插件 Manifest 源文件集合的结构化输入模型，转换后再进入稳定 runtime 协议。
type ContributionManifest struct {
	Version           uint64                                   `json:"version" yaml:"version"`
	Features          []permissionprotocol.FeatureContribution `json:"features,omitempty" yaml:"features,omitempty"`
	Menus             []MenuItemContributionManifest           `json:"menus,omitempty" yaml:"menus,omitempty"`
	SchemaResources   []SchemaResourceContributionManifest     `json:"schema_resources,omitempty" yaml:"schema_resources,omitempty"`
	Settings          []settingsprotocol.Contribution          `json:"settings,omitempty" yaml:"settings,omitempty"`
	SettingsTemplates []SettingsTemplateContributionManifest   `json:"settings_templates,omitempty" yaml:"settings_templates,omitempty"`
	DesignExtensions  []plugin.DesignFlowContribution          `json:"design_extensions,omitempty" yaml:"design_extensions,omitempty"`
	DashboardWidgets  []plugin.DashboardWidgetContribution     `json:"dashboard_widgets,omitempty" yaml:"dashboard_widgets,omitempty"`
}

// SettingsTemplateContributionManifest accepts a structured YAML schema and converts it to the runtime JSON payload.
type SettingsTemplateContributionManifest struct {
	Code              string                        `json:"code" yaml:"code"`
	Title             string                        `json:"title" yaml:"title"`
	Description       string                        `json:"description,omitempty" yaml:"description,omitempty"`
	Source            string                        `json:"source,omitempty" yaml:"source,omitempty"`
	OwnerCode         string                        `json:"owner_code,omitempty" yaml:"owner_code,omitempty"`
	SchemaVersion     uint32                        `json:"schema_version" yaml:"schema_version"`
	Schema            SettingsSchemaPayloadManifest `json:"schema" yaml:"schema"`
	ActivationMode    string                        `json:"activation_mode,omitempty" yaml:"activation_mode,omitempty"`
	CatalogVisibility string                        `json:"catalog_visibility,omitempty" yaml:"catalog_visibility,omitempty"`
	ReadPermission    string                        `json:"read_permission,omitempty" yaml:"read_permission,omitempty"`
	WritePermission   string                        `json:"write_permission,omitempty" yaml:"write_permission,omitempty"`
}

// SettingsSchemaPayloadManifest is the author-facing representation of a Settings schema payload.
type SettingsSchemaPayloadManifest struct {
	Format string `json:"format" yaml:"format"`
	Data   any    `json:"data" yaml:"data"`
}

type SchemaResourceContributionManifest struct {
	File              string                    `json:"file,omitempty" yaml:"file,omitempty"`
	Resource          string                    `json:"resource" yaml:"resource"`
	TableName         string                    `json:"table_name,omitempty" yaml:"table_name,omitempty"`
	MigrationOwner    string                    `json:"migration_owner,omitempty" yaml:"migration_owner,omitempty"`
	RuntimeOwner      plugin.SchemaRuntimeOwner `json:"runtime_owner,omitempty" yaml:"runtime_owner,omitempty"`
	Schema            any                       `json:"schema" yaml:"schema"`
	ActionPermissions map[string]string         `json:"action_permissions" yaml:"action_permissions"`
}

type MenuItemContributionManifest struct {
	Code                  string                `json:"code" yaml:"code"`
	ParentCode            string                `json:"parent_code,omitempty" yaml:"parent_code,omitempty"`
	Type                  menuprotocol.ItemType `json:"type" yaml:"type"`
	Title                 string                `json:"title" yaml:"title"`
	Icon                  string                `json:"icon,omitempty" yaml:"icon,omitempty"`
	Sort                  int64                 `json:"sort" yaml:"sort"`
	Visible               bool                  `json:"visible" yaml:"visible"`
	Enabled               bool                  `json:"enabled" yaml:"enabled"`
	EntryCode             string                `json:"entry_code,omitempty" yaml:"entry_code,omitempty"`
	EntryPayload          any                   `json:"entry_payload,omitempty" yaml:"entry_payload,omitempty"`
	FeatureCode           string                `json:"feature_code,omitempty" yaml:"feature_code,omitempty"`
	EntryPermissionAction string                `json:"entry_permission_action,omitempty" yaml:"entry_permission_action,omitempty"`
}

// Load 读取并校验插件清单。
func Load(path string) (Manifest, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultFileName
	}
	item, err := Read(path)
	if err != nil {
		return Manifest{}, err
	}
	if err := item.loadSchemaResourceFiles(filepath.Dir(path)); err != nil {
		return Manifest{}, err
	}
	validate := Validate
	_, hasManifestModule, err := resolveManifestModule(filepath.Dir(path))
	if err != nil {
		return Manifest{}, err
	}
	if hasManifestModule {
		validate = ValidateBase
	}
	if err := validate(item); err != nil {
		return Manifest{}, err
	}
	return item, nil
}

// UsesManifestModule 返回插件是否通过 Go manifest.Compose 组合运行时清单。
func UsesManifestModule(manifestPath string) (bool, error) {
	_, enabled, err := resolveManifestModule(filepath.Dir(manifestPath))
	return enabled, err
}

// ValidateBase 校验由 Go Manifest 模块继续组合的 YAML 基础元数据。
func ValidateBase(item Manifest) error {
	issues := []Issue{}
	requireEqual(&issues, "apiVersion", item.APIVersion, APIVersion)
	requireEqual(&issues, "kind", item.Kind, KindPlugin)
	require(&issues, "metadata.id", item.Metadata.ID)
	require(&issues, "metadata.code", item.Metadata.Code)
	require(&issues, "metadata.name", item.Metadata.Name)
	require(&issues, "metadata.version", item.Metadata.Version)
	require(&issues, "sparq.protocol", item.Sparq.Protocol)
	if item.Metadata.ID != "" && item.Metadata.Code != "" && item.Metadata.ID != item.Metadata.Code {
		issues = append(issues, Issue{Path: "metadata.code", Message: "必须与 metadata.id 保持一致"})
	}
	if item.Metadata.Code != "" && !pluginCodePattern.MatchString(item.Metadata.Code) {
		issues = append(issues, Issue{Path: "metadata.code", Message: "必须匹配 ^[a-z][a-z0-9-]*$"})
	}
	if item.Sparq.Protocol != "" {
		if err := protocol.ValidateRange(item.Sparq.Protocol); err != nil {
			issues = append(issues, Issue{Path: "sparq.protocol", Message: err.Error()})
		}
	}
	if item.Spec.hasDeclarations() {
		issues = append(issues, Issue{
			Path: "spec", Message: "存在 manifest 模块时，plugin.yaml 只允许声明基础元数据和协议兼容范围",
		})
	}
	if len(issues) > 0 {
		return ValidationError{Issues: issues}
	}
	return nil
}

func (item Spec) hasDeclarations() bool {
	return len(item.Runtime.Supported) > 0 || len(item.Lifecycle.Operations) > 0 || len(item.Components) > 0 || len(item.Entries) > 0 || item.Frontend != nil ||
		len(item.Capabilities) > 0 || item.Contributions.Version != 0 || len(item.Contributions.Features) > 0 ||
		len(item.Contributions.Menus) > 0 || len(item.Contributions.SchemaResources) > 0 ||
		len(item.Contributions.Settings) > 0 || len(item.Contributions.SettingsTemplates) > 0 || len(item.Contributions.DesignExtensions) > 0 || len(item.Contributions.DashboardWidgets) > 0 || len(item.Provides.Capabilities) > 0 ||
		len(item.Provides.Components) > 0 || len(item.Requires.Capabilities) > 0 || len(item.Requires.Components) > 0 ||
		len(item.SiteDirectives) > 0
}

// ReferencedFiles 返回主清单加载的插件自有文件，供发布打包保留完整 Manifest 源文件集合。
func (item Manifest) ReferencedFiles() []string {
	files := make([]string, 0, len(item.Spec.Contributions.SchemaResources))
	seen := make(map[string]struct{}, len(item.Spec.Contributions.SchemaResources))
	for _, resource := range item.Spec.Contributions.SchemaResources {
		if resource.File == "" {
			continue
		}
		if _, exists := seen[resource.File]; exists {
			continue
		}
		seen[resource.File] = struct{}{}
		files = append(files, resource.File)
	}
	return files
}

// Read 读取插件清单但不执行语义校验。
func Read(path string) (Manifest, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultFileName
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		var item Manifest
		if err := json.Unmarshal(data, &item); err != nil {
			return Manifest{}, fmt.Errorf("解析 JSON 插件清单失败: %w", err)
		}
		return item, nil
	case ".yaml", ".yml", "":
		return parseYAML(data)
	default:
		return Manifest{}, fmt.Errorf("不支持的插件清单扩展名: %s", filepath.Ext(path))
	}
}

// Validate 校验插件清单的稳定基础字段。
func Validate(item Manifest) error {
	issues := []Issue{}
	requireEqual(&issues, "apiVersion", item.APIVersion, APIVersion)
	requireEqual(&issues, "kind", item.Kind, KindPlugin)
	require(&issues, "sparq.protocol", item.Sparq.Protocol)
	if item.Metadata.Code != "" && !pluginCodePattern.MatchString(item.Metadata.Code) {
		issues = append(issues, Issue{Path: "metadata.code", Message: "必须匹配 ^[a-z][a-z0-9-]*$"})
	}
	for _, runtime := range item.Spec.Runtime.Supported {
		switch runtime {
		case "local", "http":
		default:
			issues = append(issues, Issue{Path: "spec.runtime.supported", Message: fmt.Sprintf("不支持的运行时 %q", runtime)})
		}
	}
	protocolManifest, conversionErr := item.ProtocolManifest()
	if conversionErr != nil {
		issues = append(issues, Issue{Path: "spec.contributions.menus", Message: conversionErr.Error()})
	} else if err := plugin.ValidateManifest(protocolManifest); err != nil {
		validation, ok := err.(plugin.ManifestValidationError)
		if !ok {
			issues = append(issues, Issue{Path: "spec", Message: err.Error()})
		} else {
			for _, issue := range validation.Issues {
				issues = append(issues, Issue{Path: toolManifestPath(issue.Path), Message: issue.Message})
			}
		}
	}
	if len(issues) > 0 {
		return ValidationError{Issues: issues}
	}
	return nil
}

// ProtocolManifest 将包清单转换为宿主和插件 runtime 共用的稳定协议模型。
func (item Manifest) ProtocolManifest() (plugin.Manifest, error) {
	authors := make([]plugin.Author, 0, len(item.Metadata.Authors))
	for _, author := range item.Metadata.Authors {
		authors = append(authors, plugin.Author{Name: author.Name, Email: author.Email})
	}
	runtimes := make([]plugin.RuntimeKind, 0, len(item.Spec.Runtime.Supported))
	for _, runtime := range item.Spec.Runtime.Supported {
		runtimes = append(runtimes, plugin.RuntimeKind(runtime))
	}
	contributions, err := item.Spec.Contributions.protocolManifest()
	if err != nil {
		return plugin.Manifest{}, err
	}
	provides := make([]plugin.ProvideSpec, 0, len(item.Spec.Provides.Capabilities))
	for _, capabilityCode := range item.Spec.Provides.Capabilities {
		provides = append(provides, plugin.ProvideSpec{Capability: capabilityCode})
	}
	for _, componentCode := range sortedKeys(item.Spec.Provides.Components) {
		capabilityCodes := item.Spec.Provides.Components[componentCode]
		for _, capabilityCode := range capabilityCodes {
			provides = append(provides, plugin.ProvideSpec{
				Capability: capabilityCode, Component: protocol.ComponentID(componentCode),
			})
		}
	}
	requires := make([]plugin.RequireSpec, 0, len(item.Spec.Requires.Capabilities))
	for _, capabilityCode := range item.Spec.Requires.Capabilities {
		requires = append(requires, plugin.RequireSpec{Capability: capabilityCode})
	}
	for _, componentCode := range sortedKeys(item.Spec.Requires.Components) {
		capabilityCodes := item.Spec.Requires.Components[componentCode]
		for _, capabilityCode := range capabilityCodes {
			requires = append(requires, plugin.RequireSpec{
				Capability: capabilityCode, Component: protocol.ComponentID(componentCode),
			})
		}
	}
	return plugin.Manifest{
		ID: protocol.PluginID(item.Metadata.ID), Code: protocol.PluginID(item.Metadata.Code),
		Name: item.Metadata.Name, Version: item.Metadata.Version, Description: item.Metadata.Description,
		Authors: authors, DocsURL: item.Metadata.Docs, Protocol: protocol.Compatibility{Range: item.Sparq.Protocol},
		DependsOn: append([]protocol.PluginID(nil), item.Spec.DependsOn...),
		Runtime:   runtimes, Components: cloneComponents(item.Spec.Components), Provides: provides, Requires: requires, Entries: item.Spec.Entries,
		Lifecycle:    item.Spec.Lifecycle,
		Frontend:     cloneFrontend(item.Spec.Frontend),
		Capabilities: item.Spec.Capabilities, Contributions: contributions,
		SiteDirectives: append([]protocolsite.DirectiveDeclaration(nil), item.Spec.SiteDirectives...),
	}, nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneComponents(components []plugin.ComponentManifest) []plugin.ComponentManifest {
	output := append([]plugin.ComponentManifest(nil), components...)
	for index := range output {
		output[index].Runtime = append([]plugin.RuntimeKind(nil), output[index].Runtime...)
		output[index].Dependencies = append([]plugin.ComponentDependency(nil), output[index].Dependencies...)
		output[index].HostDependencies = append([]plugin.HostDependency(nil), output[index].HostDependencies...)
		if output[index].Frontend != nil {
			frontend := *output[index].Frontend
			output[index].Frontend = &frontend
		}
	}
	return output
}

func cloneFrontend(frontend *plugin.FrontendManifest) *plugin.FrontendManifest {
	if frontend == nil {
		return nil
	}
	value := *frontend
	if frontend.Artifact != nil {
		artifact := *frontend.Artifact
		artifact.Styles = append([]string(nil), frontend.Artifact.Styles...)
		artifact.Files = cloneStringMap(frontend.Artifact.Files)
		value.Artifact = &artifact
	}
	return &value
}

func (input ContributionManifest) protocolManifest() (plugin.ContributionManifest, error) {
	output := plugin.ContributionManifest{
		Version: input.Version, Features: input.Features,
		Settings:         append([]settingsprotocol.Contribution(nil), input.Settings...),
		DesignExtensions: append([]plugin.DesignFlowContribution(nil), input.DesignExtensions...),
		DashboardWidgets: append([]plugin.DashboardWidgetContribution(nil), input.DashboardWidgets...),
	}
	for _, template := range input.SettingsTemplates {
		schemaData, err := json.Marshal(template.Schema.Data)
		if err != nil {
			return plugin.ContributionManifest{}, fmt.Errorf("编码 Settings 模板 %s Schema: %w", template.Code, err)
		}
		output.SettingsTemplates = append(output.SettingsTemplates, settingsprotocol.TemplateContribution{
			Code: template.Code, Title: template.Title, Description: template.Description,
			Source: template.Source, OwnerCode: template.OwnerCode, SchemaVersion: template.SchemaVersion,
			Schema:         &settingsprotocol.SchemaPayload{Format: template.Schema.Format, Data: schemaData},
			ActivationMode: template.ActivationMode, CatalogVisibility: template.CatalogVisibility,
			ReadPermission: template.ReadPermission, WritePermission: template.WritePermission,
		})
	}
	for _, resource := range input.SchemaResources {
		schemaPayload, err := json.Marshal(resource.Schema)
		if err != nil {
			return plugin.ContributionManifest{}, fmt.Errorf("编码 Schema 资源 %s: %w", resource.Resource, err)
		}
		output.SchemaResources = append(output.SchemaResources, plugin.SchemaResourceContribution{
			Resource: resource.Resource, TableName: resource.TableName, MigrationOwner: resource.MigrationOwner,
			RuntimeOwner: resource.RuntimeOwner, Schema: schemaPayload,
			ActionPermissions: resource.ActionPermissions,
		})
	}
	for _, item := range input.Menus {
		payload, err := encodeEntryPayload(item.EntryPayload)
		if err != nil {
			return plugin.ContributionManifest{}, fmt.Errorf("编码菜单 %s entry_payload: %w", item.Code, err)
		}
		output.Menus = append(output.Menus, menuprotocol.ItemContribution{
			Code: item.Code, ParentCode: item.ParentCode, Type: item.Type, Title: item.Title, Icon: item.Icon,
			Sort: item.Sort, Visible: item.Visible, Enabled: item.Enabled, EntryCode: item.EntryCode,
			EntryPayload: payload, FeatureCode: item.FeatureCode, EntryPermissionAction: item.EntryPermissionAction,
		})
	}
	return output, nil
}

func encodeEntryPayload(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	if text, ok := value.(string); ok && json.Valid([]byte(text)) {
		return json.RawMessage(text), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func toolManifestPath(path string) string {
	switch {
	case path == "id":
		return "metadata.id"
	case path == "code":
		return "metadata.code"
	case path == "name":
		return "metadata.name"
	case path == "version":
		return "metadata.version"
	case path == "runtime" || strings.HasPrefix(path, "runtime."):
		return "spec.runtime.supported" + strings.TrimPrefix(path, "runtime")
	default:
		return "spec." + path
	}
}

func require(issues *[]Issue, path string, value string) {
	if strings.TrimSpace(value) == "" {
		*issues = append(*issues, Issue{Path: path, Message: "不能为空"})
	}
}

func requireEqual(issues *[]Issue, path string, value string, expected string) {
	require(issues, path, value)
	if strings.TrimSpace(value) != "" && value != expected {
		*issues = append(*issues, Issue{Path: path, Message: fmt.Sprintf("必须为 %q", expected)})
	}
}
