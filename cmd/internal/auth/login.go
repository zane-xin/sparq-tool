package authcmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
	"golang.org/x/term"
)

const (
	maxIdentifierBytes = 512
	maxPasswordBytes   = 4096
)

var errInputTooLong = errors.New("input exceeds limit")

func newLoginCommand() *cobra.Command {
	options := credentialOptions{profile: remoteauth.DefaultProfile, timeout: defaultRequestTimeout}
	var identifierType string
	var identifier string
	var endpoint string
	var passwordStdin bool
	command := &cobra.Command{
		Use:   "login",
		Short: "登录 Sparq Cloud 账号",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			resolvedIdentifier, err := readIdentifier(command, identifierType, identifier, passwordStdin)
			if err != nil {
				return err
			}
			resolvedIdentifierType, err := resolveIdentifierType(identifierType, resolvedIdentifier)
			if err != nil {
				return err
			}
			password, err := readPassword(command, passwordStdin)
			if err != nil {
				return err
			}
			defer clear(password)
			client := remoteauth.NewClient(&http.Client{Timeout: options.timeout})
			session, err := client.Login(command.Context(), endpoint, resolvedIdentifierType, resolvedIdentifier, password)
			if err != nil {
				return err
			}
			path, err := remoteauth.SaveSession(options.configPath, options.profile, session)
			if err != nil {
				revokeErr := client.Logout(command.Context(), session)
				if revokeErr != nil && !remoteauth.IsUnauthorized(revokeErr) {
					return errors.Join(err, fmt.Errorf("撤销未保存的 Sparq Cloud 登录态失败: %w", revokeErr))
				}
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "已登录 Sparq Cloud\nprofile: %s\nendpoint: %s\naccount: %s\nconfig: %s\n", options.profile, session.Endpoint, accountDisplay(session.Account), path)
			return err
		},
	}
	options.bindFlags(command.Flags())
	command.Flags().StringVar(&endpoint, "url", remoteauth.DefaultEndpoint, "Sparq Cloud 地址")
	command.Flags().StringVar(&identifierType, "type", "auto", "登录标识类型: auto、email、username 或 mobile")
	command.Flags().StringVarP(&identifier, "name", "u", "", "邮箱、用户名或手机号")
	command.Flags().BoolVar(&passwordStdin, "password-stdin", false, "从标准输入读取密码")
	return command
}

func readIdentifier(command *cobra.Command, identifierType string, identifier string, passwordStdin bool) (string, error) {
	if !validIdentifierType(identifierType) {
		return "", fmt.Errorf("登录标识类型必须是 auto、email、username 或 mobile")
	}
	identifier = strings.TrimSpace(identifier)
	if identifier != "" {
		return identifier, nil
	}
	if passwordStdin {
		return "", fmt.Errorf("非交互登录必须通过 --name 指定邮箱、用户名或手机号")
	}
	input, ok := command.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return "", fmt.Errorf("当前环境无法交互读取账号，请通过 --name 指定")
	}
	if _, err := fmt.Fprintf(command.ErrOrStderr(), "%s: ", identifierPrompt(identifierType)); err != nil {
		return "", err
	}
	identifier, err := readLine(input, maxIdentifierBytes)
	if errors.Is(err, errInputTooLong) {
		return "", fmt.Errorf("登录账号超过 %d 字节限制", maxIdentifierBytes)
	}
	if err != nil {
		return "", fmt.Errorf("读取登录账号失败: %w", err)
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return "", fmt.Errorf("登录账号不能为空")
	}
	return identifier, nil
}

func validIdentifierType(identifierType string) bool {
	switch strings.TrimSpace(identifierType) {
	case "auto", "email", "username", "mobile":
		return true
	default:
		return false
	}
}

func identifierPrompt(identifierType string) string {
	switch strings.TrimSpace(identifierType) {
	case "auto":
		return "账号"
	case "username":
		return "用户名"
	case "mobile":
		return "手机号"
	default:
		return "邮箱"
	}
}

func resolveIdentifierType(identifierType string, identifier string) (string, error) {
	identifierType = strings.TrimSpace(identifierType)
	if !validIdentifierType(identifierType) {
		return "", fmt.Errorf("登录标识类型必须是 auto、email、username 或 mobile")
	}
	if identifierType != "auto" {
		return identifierType, nil
	}
	if strings.Contains(identifier, "@") {
		return "email", nil
	}
	if looksLikeMobile(identifier) {
		return "mobile", nil
	}
	return "username", nil
}

func looksLikeMobile(identifier string) bool {
	digits := strings.TrimSpace(identifier)
	digits = strings.TrimPrefix(digits, "+")
	if len(digits) < 7 || len(digits) > 15 {
		return false
	}
	for _, value := range digits {
		if value < '0' || value > '9' {
			return false
		}
	}
	return true
}

func readPassword(command *cobra.Command, passwordStdin bool) ([]byte, error) {
	if passwordStdin {
		password, err := readLine(command.InOrStdin(), maxPasswordBytes)
		if errors.Is(err, errInputTooLong) {
			return nil, fmt.Errorf("登录密码超过 %d 字节限制", maxPasswordBytes)
		}
		if err != nil {
			return nil, fmt.Errorf("从标准输入读取密码失败: %w", err)
		}
		if password == "" {
			return nil, fmt.Errorf("登录密码不能为空")
		}
		return []byte(password), nil
	}
	input, ok := command.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return nil, fmt.Errorf("当前环境无法安全读取密码，请通过 --password-stdin 输入")
	}
	if _, err := fmt.Fprint(command.ErrOrStderr(), "密码: "); err != nil {
		return nil, err
	}
	password, err := term.ReadPassword(int(input.Fd()))
	_, newlineErr := fmt.Fprintln(command.ErrOrStderr())
	if err != nil {
		return nil, fmt.Errorf("读取登录密码失败: %w", err)
	}
	if newlineErr != nil {
		clear(password)
		return nil, newlineErr
	}
	if len(password) == 0 {
		return nil, fmt.Errorf("登录密码不能为空")
	}
	if len(password) > maxPasswordBytes {
		clear(password)
		return nil, fmt.Errorf("登录密码超过 %d 字节限制", maxPasswordBytes)
	}
	return password, nil
}

func readLine(reader io.Reader, limit int) (string, error) {
	buffer := make([]byte, 0, min(limit, 64))
	oneByte := make([]byte, 1)
	for {
		count, err := reader.Read(oneByte)
		if count > 0 {
			if oneByte[0] == '\n' {
				break
			}
			if len(buffer) == limit {
				return "", errInputTooLong
			}
			buffer = append(buffer, oneByte[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", err
		}
	}
	return strings.TrimSuffix(string(buffer), "\r"), nil
}
