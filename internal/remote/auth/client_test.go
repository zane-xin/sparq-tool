package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientLoginCurrentAccountAndLogout(t *testing.T) {
	requests := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "POST /cloud-api/usercenter/v1/sessions":
			var input loginRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatalf("decode login request: %v", err)
			}
			if input.IdentifierType != "email" || input.Identifier != "user@example.com" || input.Password != "secret" {
				t.Fatalf("unexpected login request: %+v", input)
			}
			_, _ = writer.Write([]byte(`{"code":0,"message":"操作成功","data":{"account":{"id":7,"nickname":"开发者","email":"user@example.com","status":"active"},"access_token":"session-token","expires_at":1800000000}}`))
		case "GET /cloud-api/usercenter/v1/me":
			assertAuthorization(t, request)
			_, _ = writer.Write([]byte(`{"code":0,"message":"操作成功","data":{"account":{"id":7,"nickname":"开发者","email":"user@example.com","status":"active"}}}`))
		case "DELETE /cloud-api/usercenter/v1/session":
			assertAuthorization(t, request)
			_, _ = writer.Write([]byte(`{"code":0,"message":"操作成功","data":{"revoked":true}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	session, err := client.Login(context.Background(), server.URL+"/", "email", "user@example.com", []byte("secret"))
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if session.Endpoint != server.URL || session.AccessToken != "session-token" || session.Account.ID != 7 {
		t.Fatalf("unexpected session: %+v", session)
	}
	account, err := client.CurrentAccount(context.Background(), session)
	if err != nil {
		t.Fatalf("CurrentAccount failed: %v", err)
	}
	if account.Email != "user@example.com" {
		t.Fatalf("unexpected account: %+v", account)
	}
	if err := client.Logout(context.Background(), session); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if len(requests) != 3 {
		t.Fatalf("requests = %v", requests)
	}
}

func TestClientReturnsPublicAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"code":40100,"message":"登录凭据无效","data":null}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, err := client.CurrentAccount(context.Background(), Session{Endpoint: server.URL, AccessToken: "invalid"})
	if err == nil || !IsUnauthorized(err) || err.Error() != "验证 Sparq Cloud 登录态失败: 登录凭据无效" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientRejectsInvalidLoginInput(t *testing.T) {
	client := NewClient(http.DefaultClient)
	if _, err := client.Login(context.Background(), "example.com", "email", "user@example.com", []byte("secret")); err == nil {
		t.Fatal("Login should reject endpoint without scheme")
	}
	if _, err := client.Login(context.Background(), "https://example.com", "account", "user", []byte("secret")); err == nil {
		t.Fatal("Login should reject unknown identifier type")
	}
}

func assertAuthorization(t *testing.T, request *http.Request) {
	t.Helper()
	if authorization := request.Header.Get("Authorization"); authorization != "Bearer session-token" {
		t.Fatalf("authorization = %q", authorization)
	}
}
