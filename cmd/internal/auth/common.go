package authcmd

import (
	"context"
	"net/http"
	"time"

	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
)

const defaultRequestTimeout = 15 * time.Second

type credentialOptions struct {
	configPath string
	profile    string
	timeout    time.Duration
}

func (options *credentialOptions) bindFlags(command flagBinder) {
	command.StringVar(&options.configPath, "config", "", "认证配置路径，默认使用 "+remoteauth.DefaultConfigPathDisplay)
	command.StringVar(&options.profile, "profile", options.profile, "认证 profile，默认使用当前 profile")
	command.DurationVar(&options.timeout, "timeout", defaultRequestTimeout, "认证请求超时时间")
}

type flagBinder interface {
	StringVar(*string, string, string, string)
	DurationVar(*time.Duration, string, time.Duration, string)
}

func verifiedAccount(ctx context.Context, options credentialOptions) (remoteauth.Session, remoteauth.Account, string, string, error) {
	session, profile, path, err := remoteauth.ResolveSession(options.configPath, options.profile)
	if err != nil {
		return remoteauth.Session{}, remoteauth.Account{}, "", path, err
	}
	client := remoteauth.NewClient(&http.Client{Timeout: options.timeout})
	account, err := client.CurrentAccount(ctx, session)
	if err != nil {
		return remoteauth.Session{}, remoteauth.Account{}, profile, path, err
	}
	return session, account, profile, path, nil
}

func accountDisplay(account remoteauth.Account) string {
	if account.Nickname != "" {
		return account.Nickname
	}
	if account.Username != "" {
		return account.Username
	}
	if account.Email != "" {
		return account.Email
	}
	if account.Mobile != "" {
		return account.Mobile
	}
	return "未命名账号"
}
