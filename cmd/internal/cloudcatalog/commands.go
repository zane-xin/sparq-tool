package cloudcatalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
	remotemarketplace "github.com/zane-xin/sparq-tool/internal/remote/marketplace"
)

type ResourceKind string

const (
	KindApplication ResourceKind = "application"
	KindTemplate    ResourceKind = "template"
	pluginDelivery               = "sparq_plugin"
)

type Resource struct {
	Kind         ResourceKind
	DisplayName  string
	ArgumentName string
}

type catalogEntry struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Summary        string   `json:"summary"`
	Category       string   `json:"category"`
	DeliveryMode   string   `json:"delivery_mode,omitempty"`
	Industries     []string `json:"industries,omitempty"`
	Styles         []string `json:"styles,omitempty"`
	CurrentVersion string   `json:"current_version"`
	DownloadCount  uint64   `json:"download_count"`
	RatingScore    float64  `json:"rating_score"`
	RatingCount    uint64   `json:"rating_count"`
}

type options struct {
	configPath string
	profile    string
	timeout    time.Duration
	json       bool
}

func NewListCommand(resource Resource) *cobra.Command {
	commandOptions := defaultOptions()
	command := &cobra.Command{
		Use:   "list",
		Short: "列出线上" + resource.DisplayName,
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			entries, err := fetchCatalog(command, commandOptions, resource)
			if err != nil {
				return err
			}
			return writeCatalog(command, entries, commandOptions.json, resource.DisplayName)
		},
	}
	bindOptions(command, &commandOptions)
	return command
}

func NewSearchCommand(resource Resource) *cobra.Command {
	commandOptions := defaultOptions()
	command := &cobra.Command{
		Use:   "search [keyword]",
		Short: "搜索线上" + resource.DisplayName,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			entries, err := fetchCatalog(command, commandOptions, resource)
			if err != nil {
				return err
			}
			keyword := ""
			if len(args) == 1 {
				keyword = strings.ToLower(strings.TrimSpace(args[0]))
			}
			if keyword != "" {
				matched := make([]catalogEntry, 0, len(entries))
				for _, entry := range entries {
					if strings.Contains(strings.ToLower(entry.Code), keyword) || strings.Contains(strings.ToLower(entry.Name), keyword) || strings.Contains(strings.ToLower(entry.Summary), keyword) {
						matched = append(matched, entry)
					}
				}
				entries = matched
			}
			return writeCatalog(command, entries, commandOptions.json, resource.DisplayName)
		},
	}
	bindOptions(command, &commandOptions)
	return command
}

func NewInfoCommand(resource Resource) *cobra.Command {
	commandOptions := defaultOptions()
	command := &cobra.Command{
		Use:   "info <" + resource.ArgumentName + ">",
		Short: "查看线上" + resource.DisplayName + "详情",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			entry, err := fetchDetail(command, commandOptions, resource, strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			if commandOptions.json {
				encoder := json.NewEncoder(command.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(entry)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "编码: %s\n名称: %s\n简介: %s\n分类: %s\n当前版本: %s\n下载量: %d\n评分: %.2f (%d)\n", entry.Code, entry.Name, entry.Summary, entry.Category, entry.CurrentVersion, entry.DownloadCount, entry.RatingScore, entry.RatingCount)
			return err
		},
	}
	bindOptions(command, &commandOptions)
	return command
}

func defaultOptions() options {
	return options{timeout: 15 * time.Second}
}

func bindOptions(command *cobra.Command, commandOptions *options) {
	command.Flags().StringVar(&commandOptions.configPath, "config", "", "认证配置路径，默认使用 "+remoteauth.DefaultConfigPathDisplay)
	command.Flags().StringVar(&commandOptions.profile, "profile", "", "认证 profile，默认使用当前 profile")
	command.Flags().DurationVar(&commandOptions.timeout, "timeout", commandOptions.timeout, "请求超时时间")
	command.Flags().BoolVar(&commandOptions.json, "json", false, "以 JSON 输出")
}

func fetchCatalog(command *cobra.Command, commandOptions options, resource Resource) ([]catalogEntry, error) {
	client, session, err := clientFor(commandOptions)
	if err != nil {
		return nil, err
	}
	switch resource.Kind {
	case KindApplication:
		applications, err := client.ListApplications(command.Context(), session)
		if err != nil {
			return nil, err
		}
		entries := make([]catalogEntry, 0, len(applications))
		for _, application := range applications {
			if application.DeliveryMode == pluginDelivery {
				entries = append(entries, applicationEntry(application))
			}
		}
		return entries, nil
	case KindTemplate:
		templates, err := client.ListTemplates(command.Context(), session)
		if err != nil {
			return nil, err
		}
		entries := make([]catalogEntry, 0, len(templates))
		for _, template := range templates {
			entries = append(entries, templateEntry(template))
		}
		return entries, nil
	default:
		return nil, fmt.Errorf("不支持的 Cloud Catalog 资源 %q", resource.Kind)
	}
}

func fetchDetail(command *cobra.Command, commandOptions options, resource Resource, code string) (catalogEntry, error) {
	client, session, err := clientFor(commandOptions)
	if err != nil {
		return catalogEntry{}, err
	}
	switch resource.Kind {
	case KindApplication:
		application, err := client.GetApplication(command.Context(), session, code)
		if err != nil {
			return catalogEntry{}, err
		}
		if application.DeliveryMode != pluginDelivery {
			return catalogEntry{}, fmt.Errorf("Application %q 不能作为 Sparq Plugin 交付", application.Code)
		}
		return applicationEntry(application), nil
	case KindTemplate:
		template, err := client.GetTemplate(command.Context(), session, code)
		if err != nil {
			return catalogEntry{}, err
		}
		return templateEntry(template), nil
	default:
		return catalogEntry{}, fmt.Errorf("不支持的 Cloud Catalog 资源 %q", resource.Kind)
	}
}

func applicationEntry(application remotemarketplace.PublicApplication) catalogEntry {
	return catalogEntry{
		Code: application.Code, Name: application.Name, Summary: application.Summary, Category: application.Category,
		DeliveryMode: application.DeliveryMode, CurrentVersion: application.CurrentVersion, DownloadCount: application.DownloadCount,
		RatingScore: application.RatingScore, RatingCount: application.RatingCount,
	}
}

func templateEntry(template remotemarketplace.PublicTemplate) catalogEntry {
	return catalogEntry{
		Code: template.Code, Name: template.Name, Summary: template.Summary, Category: template.Category,
		Industries: template.Industries, Styles: template.Styles, CurrentVersion: template.CurrentVersion,
		DownloadCount: template.DownloadCount, RatingScore: template.RatingScore, RatingCount: template.RatingCount,
	}
}

func clientFor(commandOptions options) (remotemarketplace.Client, remoteauth.Session, error) {
	if commandOptions.timeout <= 0 {
		return remotemarketplace.Client{}, remoteauth.Session{}, fmt.Errorf("请求超时时间必须大于零")
	}
	session, profile, _, err := remoteauth.ResolveSession(commandOptions.configPath, commandOptions.profile)
	if err != nil {
		return remotemarketplace.Client{}, remoteauth.Session{}, err
	}
	if session.ExpiresAt > 0 && time.Now().Unix() >= session.ExpiresAt {
		return remotemarketplace.Client{}, remoteauth.Session{}, fmt.Errorf("认证 profile %q 已过期，请重新运行 sparq-tool auth login", profile)
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return remotemarketplace.Client{}, remoteauth.Session{}, fmt.Errorf("认证 profile %q 缺少 access token，请重新运行 sparq-tool auth login", profile)
	}
	return remotemarketplace.NewClient(&http.Client{Timeout: commandOptions.timeout}), session, nil
}

func writeCatalog(command *cobra.Command, entries []catalogEntry, asJSON bool, resourceName string) error {
	if asJSON {
		encoder := json.NewEncoder(command.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(entries)
	}
	if len(entries) == 0 {
		_, err := fmt.Fprintln(command.OutOrStdout(), "未找到线上"+resourceName)
		return err
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(command.OutOrStdout(), "%s\t%s\t%s\t当前版本:%s\n", entry.Code, entry.Name, entry.Category, entry.CurrentVersion); err != nil {
			return err
		}
	}
	return nil
}
