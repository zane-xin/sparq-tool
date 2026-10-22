package hostplugin

import (
	"os"
	"time"

	"github.com/spf13/cobra"
)

type HostOptions struct {
	URL     string
	Session string
	CSRF    string
	Timeout time.Duration
}

func BindHostFlags(command *cobra.Command, options *HostOptions) {
	bindHostTargetFlags(command, options)
	command.Flags().StringVar(&options.Session, "host-session", os.Getenv("SPARQ_HOST_SESSION"), "宿主 admin session，建议使用 SPARQ_HOST_SESSION")
	command.Flags().StringVar(&options.CSRF, "host-csrf", os.Getenv("SPARQ_HOST_CSRF"), "宿主 CSRF token，建议使用 SPARQ_HOST_CSRF")
}

func bindHostTargetFlags(command *cobra.Command, options *HostOptions) {
	command.Flags().StringVar(&options.URL, "host-url", "http://127.0.0.1:19090", "Sparq 宿主管理 API 地址")
	command.Flags().DurationVar(&options.Timeout, "timeout", 15*time.Minute, "宿主请求或安装操作的最长等待时间")
}
