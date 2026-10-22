package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestClientUploadsPluginSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/cloud-api/v1/developer/applications/cms/source-uploads" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer access-token" || request.Header.Get("Idempotency-Key") != "sha256:digest" {
			t.Fatalf("headers = %+v", request.Header)
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if request.FormValue("changelog") != "新增文章审核" || request.FormValue("commit_sha") != "abc123" {
			t.Fatalf("form = %+v", request.MultipartForm.Value)
		}
		file, header, err := request.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		if err != nil || string(body) != "archive" || header.Filename != "cms-1.2.3-source.tar.gz" {
			t.Fatalf("file = %q, %+v, %v", body, header, err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"message":"操作成功","data":{"id":8,"version_id":9,"version":"1.2.3","origin":"ci","sha256":"digest","size_bytes":7,"status":"uploading","created_at":100}}`))
	}))
	defer server.Close()

	archivePath := filepath.Join(t.TempDir(), "cms-1.2.3-source.tar.gz")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := NewClient(server.Client()).UploadApplicationSource(context.Background(), UploadSourceInput{
		Session:         remoteauth.Session{Endpoint: server.URL, AccessToken: "access-token"},
		ApplicationCode: "cms", ArchivePath: archivePath, Changelog: "新增文章审核",
		CommitSHA: "abc123", IdempotencyKey: "sha256:digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != 8 || result.VersionID != 9 || result.Version != "1.2.3" {
		t.Fatalf("result = %+v", result)
	}
}

func TestClientPreflightsAndCancelsPluginPublish(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/cloud-api/v1/developer/applications/cms/publish-preflight":
			if request.Method != http.MethodPost || request.Header.Get("Idempotency-Key") != "attempt:test" {
				t.Fatalf("preflight request = %s %+v", request.Method, request.Header)
			}
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body["version"] != "1.2.3" || body["changelog"] != "版本说明" {
				t.Fatalf("preflight body = %+v, %v", body, err)
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"application_id":7,"application_code":"cms","version":"1.2.3","resumable_upload":{"id":8,"version_id":9,"version":"1.2.3","status":"uploading"}}}`))
		case "/cloud-api/v1/developer/application-source-uploads/8/cancel":
			if request.Method != http.MethodPost {
				t.Fatalf("cancel method = %s", request.Method)
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"id":8,"version_id":9,"version":"1.2.3","status":"canceled"}}`))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	session := remoteauth.Session{Endpoint: server.URL, AccessToken: "access-token"}
	preflight, err := client.PreflightApplicationPublish(context.Background(), PublishPreflightInput{
		Session: session, ApplicationCode: "cms", Version: "1.2.3", Changelog: "版本说明", IdempotencyKey: "attempt:test",
	})
	if err != nil || preflight.ApplicationID != 7 || preflight.ResumableUpload == nil || preflight.ResumableUpload.ID != 8 {
		t.Fatalf("PreflightApplicationPublish() = %+v, %v", preflight, err)
	}
	canceled, err := client.CancelApplicationSource(context.Background(), session, 8)
	if err != nil || canceled.Status != "canceled" {
		t.Fatalf("CancelApplicationSource() = %+v, %v", canceled, err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d", requestCount)
	}
}

func TestClientUploadsCandidateArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/cloud-api/v1/developer/application-source-uploads/8/candidate-artifacts" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer access-token" || request.Header.Get("Idempotency-Key") != "sha256:candidate" {
			t.Fatalf("headers = %+v", request.Header)
		}
		file, header, err := request.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil || string(body) != "package" || header.Filename != "cms-1.2.3-linux-amd64.tar.gz" ||
			request.FormValue("source_sha256") != "source-digest" || request.FormValue("format") != "sparq.plugin.package.v1" ||
			request.FormValue("kind") != ArtifactKindManagedRemote || request.FormValue("runtime") != "http" ||
			request.FormValue("target_os") != "linux" || request.FormValue("target_arch") != "amd64" {
			t.Fatalf("candidate body = %q, header = %+v, form = %+v, error = %v", body, header, request.MultipartForm.Value, readErr)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":{"id":21,"source_upload_id":8,"version_id":9,"source_sha256":"source-digest","sha256":"candidate-digest","size_bytes":7,"format":"sparq.plugin.package.v1","kind":"managed_remote","runtime":"http","target_os":"linux","target_arch":"amd64","verification_status":"verified","trust_decision":"rebuild_required","created_at":101}}`))
	}))
	defer server.Close()

	archivePath := filepath.Join(t.TempDir(), "cms-1.2.3-linux-amd64.tar.gz")
	if err := os.WriteFile(archivePath, []byte("package"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := NewClient(server.Client()).UploadCandidateArtifact(context.Background(), UploadCandidateArtifactInput{
		Session: remoteauth.Session{Endpoint: server.URL, AccessToken: "access-token"}, SourceUploadID: 8,
		SourceSHA256: "source-digest", ArchivePath: archivePath, Format: "sparq.plugin.package.v1",
		Kind: ArtifactKindManagedRemote, Runtime: "http", TargetOS: "linux", TargetArch: "amd64", IdempotencyKey: "sha256:candidate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != 21 || result.SourceUploadID != 8 || result.Kind != ArtifactKindManagedRemote || result.TargetOS != "linux" || result.VerificationStatus != "verified" || result.TrustDecision != "rebuild_required" {
		t.Fatalf("result = %+v", result)
	}
}

func TestClientReadsAndFinalizesPublishSessions(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/cloud-api/v1/developer/application-source-uploads/8":
			if request.Method != http.MethodGet {
				t.Fatalf("submission method = %s", request.Method)
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"upload":{"id":8,"version_id":9,"version":"1.2.3","sha256":"source-digest","status":"uploading"},"candidates":[{"id":21,"source_upload_id":8,"version_id":9,"source_sha256":"source-digest","sha256":"candidate-digest","format":"sparq.plugin.package.v1","kind":"managed_remote","runtime":"http","target_os":"linux","target_arch":"amd64"}]}}`))
		case "/cloud-api/v1/developer/application-source-uploads/8/finalize":
			if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("application finalize request = %s %+v", request.Method, request.Header)
			}
			var body struct {
				Candidates []CandidateExpectation `json:"candidates"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.Candidates) != 1 ||
				body.Candidates[0].Kind != ArtifactKindManagedRemote || body.Candidates[0].SHA256 != "candidate-digest" {
				t.Fatalf("application finalize body = %+v, %v", body, err)
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"upload":{"id":8,"version_id":9,"version":"1.2.3","sha256":"source-digest","status":"completed"},"candidates":[{"id":21,"source_upload_id":8,"version_id":9,"source_sha256":"source-digest","sha256":"candidate-digest","format":"sparq.plugin.package.v1","kind":"managed_remote","runtime":"http","target_os":"linux","target_arch":"amd64"}]}}`))
		case "/cloud-api/v1/developer/template-source-uploads/31/finalize":
			if request.Method != http.MethodPost {
				t.Fatalf("template finalize method = %s", request.Method)
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"id":31,"version_id":32,"version":"0.2.0","sha256":"theme-digest","status":"completed"}}`))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	session := remoteauth.Session{Endpoint: server.URL, AccessToken: "access-token"}
	submission, err := client.GetApplicationSourceSubmission(context.Background(), session, 8)
	if err != nil || submission.Upload.ID != 8 || len(submission.Candidates) != 1 {
		t.Fatalf("GetApplicationSourceSubmission() = %+v, %v", submission, err)
	}
	finalized, err := client.FinalizeApplicationSource(context.Background(), session, 8, []CandidateExpectation{{
		SHA256: "candidate-digest", Kind: ArtifactKindManagedRemote, Runtime: "http", TargetOS: "linux", TargetArch: "amd64",
	}})
	if err != nil || finalized.Upload.Status != "completed" {
		t.Fatalf("FinalizeApplicationSource() = %+v, %v", finalized, err)
	}
	theme, err := client.FinalizeTemplateSource(context.Background(), session, 31)
	if err != nil || theme.ID != 31 || theme.Status != "completed" {
		t.Fatalf("FinalizeTemplateSource() = %+v, %v", theme, err)
	}
	if requestCount != 3 {
		t.Fatalf("request count = %d", requestCount)
	}
}

func TestClientCandidateUploadPreservesConnectionError(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "cms-1.2.3-linux-amd64.tar.gz")
	if err := os.WriteFile(archivePath, make([]byte, 2<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	connectionErr := errors.New("server connection closed")
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		buffer := make([]byte, 128)
		_, _ = request.Body.Read(buffer)
		_ = request.Body.Close()
		return nil, connectionErr
	})})
	_, err := client.UploadCandidateArtifact(context.Background(), UploadCandidateArtifactInput{
		Session: remoteauth.Session{Endpoint: "http://marketplace.example.test", AccessToken: "access-token"}, SourceUploadID: 8,
		SourceSHA256: "source-digest", ArchivePath: archivePath, Format: "sparq.plugin.package.v1",
		Kind: ArtifactKindManagedRemote, Runtime: "http", TargetOS: "linux", TargetArch: "amd64", IdempotencyKey: "sha256:candidate",
	})
	if err == nil || !errors.Is(err, connectionErr) || !strings.Contains(err.Error(), "上传候选构建包失败") || strings.Contains(err.Error(), "closed pipe") {
		t.Fatalf("UploadCandidateArtifact() error = %v", err)
	}
}

func TestClientResolvesPluginDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/cloud-api/v1/application-downloads/resolve" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["application_code"] != "cms" || body["artifact_kind"] != "local_source_capsule" {
			t.Fatalf("body = %+v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":{"application":{"code":"cms","name":"CMS","version":"1.2.3"},"authorization":{"allowed":true,"license_id":9},"artifact":{"available":true,"id":12,"kind":"local_source_capsule","runtime":"local","format":"sparq.plugin.source-capsule.v1","filename":"cms-1.2.3.sparq","size_bytes":8,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","public_key_id":"key-1","protection_level":"sandboxed","download_url":"https://objects.example.test/cms","expires_at":100}}}`))
	}))
	defer server.Close()

	result, err := NewClient(server.Client()).ResolveApplicationDownload(context.Background(), ResolveApplicationDownloadInput{
		Session:         remoteauth.Session{Endpoint: server.URL, AccessToken: "access-token"},
		ApplicationCode: "cms", Version: "latest", ArtifactKind: "local_source_capsule",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Application.Version != "1.2.3" || !result.Authorization.Allowed || !result.Artifact.Available || result.Artifact.ID != 12 {
		t.Fatalf("result = %+v", result)
	}
}

func TestClientCreatesApplicationInstallGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/cloud-api/v1/application-install-grants" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var body map[string]map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["download"]["application_code"] != "cms" || body["download"]["artifact_kind"] != ArtifactKindManagedRemote || body["download"]["target"].(map[string]any)["app_instance_id"] != "site-1" {
			t.Fatalf("body = %+v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":{"grant":"spqig_test","app_instance_id":"site-1","application":{"code":"cms","name":"CMS","version":"1.2.3"},"artifact":{"available":true,"id":12,"kind":"managed_remote","runtime":"http","target_os":"linux","target_arch":"amd64","format":"sparq.plugin.package.v1","filename":"cms.tar.gz","size_bytes":8,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","download_url":"https://objects.example.test/cms","expires_at":100},"expires_at":100}}`))
	}))
	defer server.Close()

	grant, err := NewClient(server.Client()).CreateApplicationInstallGrant(context.Background(), ResolveApplicationDownloadInput{
		Session: remoteauth.Session{Endpoint: server.URL, AccessToken: "access-token"}, ApplicationCode: "cms",
		Version: "latest", ArtifactKind: ArtifactKindManagedRemote, Target: DownloadTarget{AppInstanceID: "site-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if grant.Grant != "spqig_test" || grant.AppInstanceID != "site-1" || grant.Application.Code != "cms" {
		t.Fatalf("grant = %+v", grant)
	}
}

func TestClientResolvesTemplateDownloadAndCreatesTemplateInstallGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/cloud-api/v1/template-downloads/resolve":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if request.Method != http.MethodPost || body["template_code"] != "studio" || body["version"] != "latest" {
				t.Fatalf("download request = %+v", body)
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"template":{"id":4,"code":"studio","name":"Studio","version_id":5,"release_id":6,"version":"1.0.0"},"authorization":{"allowed":true,"license_id":9},"artifact":{"available":true,"id":12,"template_id":4,"template_version_id":5,"template_release_id":6,"kind":"template_source","format":"zip","filename":"studio.zip","size_bytes":8,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","download_url":"https://objects.example.test/studio","expires_at":100}}}`))
		case "/cloud-api/v1/template-install-grants":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			download := body["download"].(map[string]any)
			if request.Method != http.MethodPost || download["template_code"] != "studio" || body["app_instance_id"] != "site-1" {
				t.Fatalf("grant request = %+v", body)
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"grant":"spqtg_test","template":{"id":4,"code":"studio","name":"Studio","version_id":5,"release_id":6,"version":"1.0.0"},"template_license_id":9,"artifact":{"available":true,"id":12,"kind":"template_source","format":"zip","filename":"studio.zip","size_bytes":8,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","download_url":"https://objects.example.test/studio","expires_at":100},"expires_at":100}}`))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()
	session := remoteauth.Session{Endpoint: server.URL, AccessToken: "access-token"}
	client := NewClient(server.Client())
	resolved, err := client.ResolveTemplateDownload(context.Background(), ResolveTemplateDownloadInput{Session: session, TemplateCode: "studio", Version: "latest"})
	if err != nil || resolved.Template.Code != "studio" || !resolved.Artifact.Available {
		t.Fatalf("resolution = %+v, %v", resolved, err)
	}
	grant, err := client.CreateTemplateInstallGrant(context.Background(), session, "studio", "latest", "site-1")
	if err != nil || grant.Grant != "spqtg_test" || grant.Template.Code != "studio" || grant.Artifact.Kind != ArtifactKindTemplateSource {
		t.Fatalf("grant = %+v, %v", grant, err)
	}
}

func TestClientListsPublicApplications(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/cloud-api/v1/applications" || request.URL.RawQuery != "" {
			t.Fatalf("request = %s %s?%s", request.Method, request.URL.Path, request.URL.RawQuery)
		}
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":[{"code":"cms","name":"CMS","summary":"内容管理","category":"content","delivery_mode":"sparq_plugin","current_version":"1.2.3","sales_plans":[{"id":20,"code":"standard","name":"标准版","charge_mode":"one_time","billing_unit":"product","price_cents":9900,"currency":"CNY","billing_interval":"none","included_quantity":1,"update_policy":"lifetime","is_default":true}]}]}`))
	}))
	defer server.Close()

	result, err := NewClient(server.Client()).ListApplications(context.Background(), remoteauth.Session{
		Endpoint: server.URL, AccessToken: "access-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Code != "cms" || result[0].DeliveryMode != "sparq_plugin" || result[0].CurrentVersion != "1.2.3" || len(result[0].SalesPlans) != 1 || result[0].SalesPlans[0].ID != 20 {
		t.Fatalf("result = %+v", result)
	}
}

func TestClientListsPublicTemplates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/cloud-api/v1/templates" || request.URL.RawQuery != "" {
			t.Fatalf("request = %s %s?%s", request.Method, request.URL.Path, request.URL.RawQuery)
		}
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":[{"code":"studio","name":"Studio","summary":"内容模板","category":"website","industries":["media"],"styles":["editorial"],"current_version":"0.1.2"}]}`))
	}))
	defer server.Close()

	result, err := NewClient(server.Client()).ListTemplates(context.Background(), remoteauth.Session{
		Endpoint: server.URL, AccessToken: "access-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Code != "studio" || result[0].Category != "website" || result[0].CurrentVersion != "0.1.2" {
		t.Fatalf("result = %+v", result)
	}
}

func TestClientCreatesAndReadsSalesPlanOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/cloud-api/v1/orders":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			items, ok := body["items"].([]any)
			if !ok || len(items) != 1 {
				t.Fatalf("body = %+v", body)
			}
			item := items[0].(map[string]any)
			if item["sales_plan_id"] != float64(20) || item["quantity"] != float64(2) || len(item) != 2 {
				t.Fatalf("item = %+v", item)
			}
		case request.Method == http.MethodGet && request.URL.Path == "/cloud-api/v1/orders/SPQ123":
		default:
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":{"order":{"id":1,"order_no":"SPQ123","status":"pending","payment_status":"pending","fulfillment_status":"pending","subtotal_amount":250,"payable_amount":250,"currency":"CNY"},"items":[{"id":2,"resource_type":"application","resource_id":3,"sales_plan_id":20,"unit_amount":125,"quantity":2,"subtotal_amount":250,"payable_amount":250,"fulfillment_status":"pending"}],"adjustments":[],"payments":[],"refunds":[]}}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	session := remoteauth.Session{Endpoint: server.URL, AccessToken: "access-token"}
	created, err := client.CreateOrder(context.Background(), CreateOrderInput{Session: session, Items: []CreateOrderItem{{SalesPlanID: 20, Quantity: 2}}})
	if err != nil || created.Order.OrderNo != "SPQ123" || created.Order.PayableAmount != 250 || len(created.Items) != 1 {
		t.Fatalf("CreateOrder() = %+v, %v", created, err)
	}
	loaded, err := client.GetOrder(context.Background(), session, "SPQ123")
	if err != nil || loaded.Items[0].ResourceType != "application" {
		t.Fatalf("GetOrder() = %+v, %v", loaded, err)
	}
}

func TestClientRejectsOrderWithoutSalesPlanOrQuantity(t *testing.T) {
	client := NewClient(http.DefaultClient)
	session := remoteauth.Session{Endpoint: "https://sparq.test", AccessToken: "access-token"}
	for _, item := range []CreateOrderItem{{Quantity: 1}, {SalesPlanID: 1}} {
		if _, err := client.CreateOrder(context.Background(), CreateOrderInput{Session: session, Items: []CreateOrderItem{item}}); err == nil {
			t.Fatalf("CreateOrder(%+v) error = nil", item)
		}
	}
}

func TestClientGetsPublicApplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.EscapedPath() != "/cloud-api/v1/applications/remote%20echo" {
			t.Fatalf("request = %s %s", request.Method, request.URL.EscapedPath())
		}
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":{"code":"remote echo","name":"Remote Echo","summary":"远程调用示例","category":"development","delivery_mode":"sparq_plugin","current_version":"0.4.0"}}`))
	}))
	defer server.Close()

	result, err := NewClient(server.Client()).GetApplication(context.Background(), remoteauth.Session{Endpoint: server.URL, AccessToken: "access-token"}, "remote echo")
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "remote echo" || result.Name != "Remote Echo" || result.DeliveryMode != "sparq_plugin" || result.CurrentVersion != "0.4.0" {
		t.Fatalf("result = %+v", result)
	}
}

func TestClientRejectsMarketplaceReadWithoutAuthToken(t *testing.T) {
	client := NewClient(http.DefaultClient)
	if _, err := client.ListApplications(context.Background(), remoteauth.Session{Endpoint: "https://sparq.test"}); err == nil {
		t.Fatal("ListApplications() should require an auth profile token")
	}
	if _, err := client.GetTemplate(context.Background(), remoteauth.Session{Endpoint: "https://sparq.test"}, "studio"); err == nil {
		t.Fatal("GetTemplate() should require an auth profile token")
	}
}

func TestClientReturnsMarketplaceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"code":40300,"message":"无权上传该应用源码","data":null}`))
	}))
	defer server.Close()
	archivePath := filepath.Join(t.TempDir(), "source.tar.gz")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewClient(server.Client()).UploadApplicationSource(context.Background(), UploadSourceInput{
		Session:         remoteauth.Session{Endpoint: server.URL, AccessToken: "token"},
		ApplicationCode: "cms", ArchivePath: archivePath, IdempotencyKey: "key",
	})
	if err == nil || err.Error() != "无权上传该应用源码" {
		t.Fatalf("error = %v", err)
	}
}

func TestMarketplaceErrorClassification(t *testing.T) {
	if !IsNotFound(&APIError{Status: http.StatusNotFound, Code: 40400}) {
		t.Fatal("404 Marketplace error should be classified as not found")
	}
	if IsNotFound(&APIError{Status: http.StatusForbidden, Code: 40300}) {
		t.Fatal("403 Marketplace error should not be classified as not found")
	}
}

func TestClientAcceptsIdempotentResponseWithoutMultipartParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":{"id":8,"version_id":9,"version":"1.2.3","origin":"ci","sha256":"digest","size_bytes":7,"status":"uploading","created_at":100}}`))
	}))
	defer server.Close()
	archivePath := filepath.Join(t.TempDir(), "source.tar.gz")
	if err := os.WriteFile(archivePath, make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := NewClient(server.Client()).UploadApplicationSource(context.Background(), UploadSourceInput{
		Session:         remoteauth.Session{Endpoint: server.URL, AccessToken: "token"},
		ApplicationCode: "cms", ArchivePath: archivePath, IdempotencyKey: "key",
	})
	if err != nil || result.ID != 8 {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}
