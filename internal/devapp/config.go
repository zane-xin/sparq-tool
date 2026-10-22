package devapp

import "github.com/zane-xin/sparq-tool/internal/appconfig"

// Config 描述二次开发应用的本地联调配置。
type Config = appconfig.Config
type TargetConfig = appconfig.TargetConfig
type AppConfig = appconfig.AppConfig
type BackendConfig = appconfig.BackendConfig
type FrontendConfig = appconfig.FrontendConfig
type StopConfig = appconfig.StopConfig

// LoadConfig 读取并严格解析应用联调配置。
func LoadConfig(path string) (Config, error) {
	return appconfig.Load(path)
}

func parseConfig(content []byte) (Config, error) {
	return appconfig.Parse(content)
}
