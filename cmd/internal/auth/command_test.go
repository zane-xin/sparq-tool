package authcmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthCommandLoginStatusWhoAmIAndLogout(t *testing.T) {
	var loginIdentifierType string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "POST /cloud-api/usercenter/v1/sessions":
			var input struct {
				IdentifierType string `json:"identifier_type"`
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Errorf("decode login request: %v", err)
			}
			loginIdentifierType = input.IdentifierType
			_, _ = writer.Write([]byte(`{"code":0,"data":{"account":{"id":9,"nickname":"Sparq Developer","email":"developer@example.com","status":"active"},"access_token":"command-token","expires_at":1800000000}}`))
		case "GET /cloud-api/usercenter/v1/me":
			if request.Header.Get("Authorization") != "Bearer command-token" {
				t.Fatalf("unexpected authorization: %s", request.Header.Get("Authorization"))
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"account":{"id":9,"nickname":"Sparq Developer","email":"developer@example.com","status":"active"}}}`))
		case "DELETE /cloud-api/usercenter/v1/session":
			_, _ = writer.Write([]byte(`{"code":0,"data":{"revoked":true}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "auth.json")
	output := executeAuthCommand(t, "secret\n", "login", "--url", server.URL, "--name", "developer@example.com", "--password-stdin", "--config", configPath)
	if !strings.Contains(output, "已登录 Sparq Cloud") || !strings.Contains(output, "account: Sparq Developer") ||
		!strings.Contains(output, "profile: default") {
		t.Fatalf("unexpected login output: %s", output)
	}
	if loginIdentifierType != "email" {
		t.Fatalf("login identifier type = %q, want email", loginIdentifierType)
	}
	output = executeAuthCommand(t, "", "status", "--config", configPath)
	if !strings.Contains(output, "登录态有效") || !strings.Contains(output, "developer@example.com") && !strings.Contains(output, "Sparq Developer") {
		t.Fatalf("unexpected status output: %s", output)
	}
	output = executeAuthCommand(t, "", "whoami", "--config", configPath)
	if !strings.Contains(output, "id: 9") || !strings.Contains(output, "email: developer@example.com") {
		t.Fatalf("unexpected whoami output: %s", output)
	}
	output = executeAuthCommand(t, "", "logout", "--config", configPath)
	if !strings.Contains(output, "已退出 Sparq Cloud") {
		t.Fatalf("unexpected logout output: %s", output)
	}
}

func TestAuthLoginRequiresPasswordStdinOutsideTerminal(t *testing.T) {
	command := New()
	command.SetIn(strings.NewReader("secret\n"))
	command.SetArgs([]string{"login", "--name", "developer@example.com"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "--password-stdin") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthLoginUsesBuiltInEndpoint(t *testing.T) {
	command := newLoginCommand()
	if command.Use != "login" {
		t.Fatalf("login use = %q", command.Use)
	}
	endpoint, err := command.Flags().GetString("url")
	if err != nil || endpoint != "https://sparq.xin" {
		t.Fatalf("endpoint = %q, %v", endpoint, err)
	}
}

func TestAuthLoginRequiresIdentifierWithPasswordStdin(t *testing.T) {
	command := New()
	command.SetIn(strings.NewReader("secret\n"))
	command.SetArgs([]string{"login", "--password-stdin"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthLoginInfersUsername(t *testing.T) {
	var identifierType string
	var identifier string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var input struct {
			IdentifierType string `json:"identifier_type"`
			Identifier     string `json:"identifier"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Errorf("decode login request: %v", err)
		}
		identifierType = input.IdentifierType
		identifier = input.Identifier
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":{"account":{"id":2,"username":"test","nickname":"Test","status":"active"},"access_token":"command-token","expires_at":1800000000}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "auth.json")
	executeAuthCommand(t, "secret\n", "login", "--url", server.URL, "-u", "test", "--password-stdin", "--config", configPath)
	if identifierType != "username" || identifier != "test" {
		t.Fatalf("login identifier = %s:%s, want username:test", identifierType, identifier)
	}
}

func TestAuthLoginRevokesRemoteSessionWhenConfigSaveFails(t *testing.T) {
	var revoked bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case http.MethodPost:
			_, _ = writer.Write([]byte(`{"code":0,"data":{"account":{"id":2,"username":"test","nickname":"Test","status":"active"},"access_token":"command-token","expires_at":1800000000}}`))
		case http.MethodDelete:
			revoked = request.Header.Get("Authorization") == "Bearer command-token"
			_, _ = writer.Write([]byte(`{"code":0,"data":{"revoked":true}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(configPath, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := New()
	command.SetIn(strings.NewReader("secret\n"))
	command.SetArgs([]string{
		"login", "--url", server.URL, "--name", "test", "--password-stdin", "--config", configPath,
	})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "解析认证配置") {
		t.Fatalf("login error = %v", err)
	}
	if !revoked {
		t.Fatal("remote session was not revoked after config save failed")
	}
}

func TestResolveIdentifierType(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		identifier string
		expected   string
	}{
		{name: "username", configured: "auto", identifier: "test", expected: "username"},
		{name: "email", configured: "auto", identifier: "developer@example.com", expected: "email"},
		{name: "mobile", configured: "auto", identifier: "+8613800138000", expected: "mobile"},
		{name: "explicit", configured: "username", identifier: "13800138000", expected: "username"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := resolveIdentifierType(test.configured, test.identifier)
			if err != nil || actual != test.expected {
				t.Fatalf("resolveIdentifierType(%q, %q) = %q, %v", test.configured, test.identifier, actual, err)
			}
		})
	}
}

func TestReadLineDoesNotConsumeFollowingLine(t *testing.T) {
	reader := strings.NewReader("developer@example.com\nsecret\n")
	identifier, err := readLine(reader, maxIdentifierBytes)
	if err != nil || identifier != "developer@example.com" {
		t.Fatalf("identifier = %q, %v", identifier, err)
	}
	password, err := readLine(reader, maxPasswordBytes)
	if err != nil || password != "secret" {
		t.Fatalf("password = %q, %v", password, err)
	}
}

func executeAuthCommand(t *testing.T, input string, args ...string) string {
	t.Helper()
	command := New()
	output := &bytes.Buffer{}
	command.SetIn(strings.NewReader(input))
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatalf("auth command %v failed: %v", args, err)
	}
	return output.String()
}
