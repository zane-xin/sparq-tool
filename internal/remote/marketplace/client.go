package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
)

const (
	maxResponseBytes               = 1 << 20
	ArtifactKindManagedRemote      = "managed_remote"
	ArtifactKindLocalSourceCapsule = "local_source_capsule"
	ArtifactKindTemplateSource     = "template_source"
	ArtifactKindFrontendBundle     = "frontend_bundle"
	CandidateKindPluginPackage     = "plugin_package"
)

// APIError 描述 Marketplace Cloud API 返回的公开错误。
type APIError struct {
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("Marketplace 接口请求失败: HTTP %d, code %d", e.Status, e.Code)
}

// IsUnauthorized 判断 Marketplace 请求是否因 Cloud 登录态失效而失败。
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Code == 40100)
}

// IsNotFound 判断 Marketplace 请求是否未找到当前命令指向的公开资源。
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Status == http.StatusNotFound || apiErr.Code == 40400)
}

// Client 调用 Sparq Cloud Marketplace 接口。
type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) Client {
	if httpClient == nil {
		panic("marketplace HTTP client is required")
	}
	return Client{httpClient: httpClient}
}

// UploadSourceInput 描述插件或主题源码的 multipart 上传请求。
type UploadSourceInput struct {
	Session         remoteauth.Session
	ApplicationCode string
	ArchivePath     string
	Changelog       string
	SourceRef       string
	CommitSHA       string
	PipelineID      string
	IdempotencyKey  string
}

// SourceUpload 是 Cloud API 返回的源码登记摘要。
type SourceUpload struct {
	ID          uint64 `json:"id"`
	VersionID   uint64 `json:"version_id"`
	Version     string `json:"version"`
	Origin      string `json:"origin"`
	SHA256      string `json:"sha256"`
	SizeBytes   uint64 `json:"size_bytes"`
	Status      string `json:"status"`
	CreatedAt   uint32 `json:"created_at"`
	CompletedAt uint32 `json:"completed_at"`
	ExpiresAt   uint32 `json:"expires_at"`
}

// PublishPreflightInput 描述本地构建前执行且不创建服务端资源的发布资格校验。
type PublishPreflightInput struct {
	Session         remoteauth.Session
	ApplicationCode string
	Version         string
	Changelog       string
	SourceRef       string
	CommitSHA       string
	PipelineID      string
	IdempotencyKey  string
}

// PublishPreflight 返回已确认的发布目标，以及相同幂等键已经绑定的可恢复服务端会话。
type PublishPreflight struct {
	ApplicationID   uint64        `json:"application_id"`
	ApplicationCode string        `json:"application_code"`
	Version         string        `json:"version"`
	ResumableUpload *SourceUpload `json:"resumable_upload"`
}

// UploadCandidateArtifactInput 描述与源码上传绑定的本地候选构建包。
type UploadCandidateArtifactInput struct {
	Session        remoteauth.Session
	SourceUploadID uint64
	SourceSHA256   string
	ArchivePath    string
	Format         string
	Kind           string
	Runtime        string
	TargetOS       string
	TargetArch     string
	IdempotencyKey string
}

// CandidateArtifact 是平台对候选构建包完成结构校验后的结果。
type CandidateArtifact struct {
	ID                 uint64 `json:"id"`
	SourceUploadID     uint64 `json:"source_upload_id"`
	VersionID          uint64 `json:"version_id"`
	SourceSHA256       string `json:"source_sha256"`
	SHA256             string `json:"sha256"`
	SizeBytes          uint64 `json:"size_bytes"`
	Format             string `json:"format"`
	Kind               string `json:"kind"`
	Runtime            string `json:"runtime"`
	TargetOS           string `json:"target_os"`
	TargetArch         string `json:"target_arch"`
	VerificationStatus string `json:"verification_status"`
	TrustDecision      string `json:"trust_decision"`
	CreatedAt          uint32 `json:"created_at"`
}

// SourceSubmission is the complete resumable Application publish session.
type SourceSubmission struct {
	Upload     SourceUpload        `json:"upload"`
	Candidates []CandidateArtifact `json:"candidates"`
}

// CandidateExpectation freezes one candidate variant during finalize.
type CandidateExpectation struct {
	SHA256     string `json:"sha256"`
	Kind       string `json:"kind"`
	Runtime    string `json:"runtime"`
	TargetOS   string `json:"target_os"`
	TargetArch string `json:"target_arch"`
}

type DownloadTarget struct {
	SparqVersion    string `json:"sparq_version,omitempty"`
	ProtocolVersion string `json:"protocol_version,omitempty"`
	OS              string `json:"os,omitempty"`
	Arch            string `json:"arch,omitempty"`
	AppInstanceID   string `json:"app_instance_id,omitempty"`
}

type ResolveApplicationDownloadInput struct {
	Session         remoteauth.Session
	ApplicationCode string
	Version         string
	ArtifactKind    string
	Target          DownloadTarget
}

type ResolveTemplateDownloadInput struct {
	Session      remoteauth.Session
	TemplateCode string
	Version      string
}
type TemplateDownloadIdentity struct {
	ID        uint64 `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	VersionID uint64 `json:"version_id"`
	ReleaseID uint64 `json:"release_id"`
	Version   string `json:"version"`
}
type TemplateDownloadArtifact struct {
	Available         bool   `json:"available"`
	Reason            string `json:"reason"`
	ID                uint64 `json:"id"`
	TemplateID        uint64 `json:"template_id"`
	TemplateVersionID uint64 `json:"template_version_id"`
	TemplateReleaseID uint64 `json:"template_release_id"`
	Kind              string `json:"kind"`
	Format            string `json:"format"`
	Filename          string `json:"filename"`
	SizeBytes         uint64 `json:"size_bytes"`
	SHA256            string `json:"sha256"`
	DownloadURL       string `json:"download_url"`
	ExpiresAt         uint32 `json:"expires_at"`
}
type TemplateDownloadResolution struct {
	Template      TemplateDownloadIdentity `json:"template"`
	Authorization DownloadAuthorization    `json:"authorization"`
	Artifact      TemplateDownloadArtifact `json:"artifact"`
}
type TemplateInstallGrant struct {
	Grant             string                   `json:"grant"`
	Template          TemplateDownloadIdentity `json:"template"`
	Artifact          TemplateDownloadArtifact `json:"artifact"`
	TemplateLicenseID uint64                   `json:"template_license_id"`
	ExpiresAt         uint32                   `json:"expires_at"`
}

type DownloadAuthorization struct {
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason"`
	LicenseID uint64 `json:"license_id"`
}

type ApplicationDownloadIdentity struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ApplicationDownloadArtifact struct {
	Available          bool   `json:"available"`
	Reason             string `json:"reason"`
	ID                 uint64 `json:"id"`
	Kind               string `json:"kind"`
	Runtime            string `json:"runtime"`
	TargetOS           string `json:"target_os"`
	TargetArch         string `json:"target_arch"`
	Format             string `json:"format"`
	Filename           string `json:"filename"`
	SizeBytes          uint64 `json:"size_bytes"`
	SHA256             string `json:"sha256"`
	PublicKeyID        string `json:"public_key_id"`
	ProtectionLevel    string `json:"protection_level"`
	BuilderEnvironment string `json:"builder_environment"`
	DownloadURL        string `json:"download_url"`
	ExpiresAt          uint32 `json:"expires_at"`
}

type ApplicationDownloadResolution struct {
	Application   ApplicationDownloadIdentity `json:"application"`
	Authorization DownloadAuthorization       `json:"authorization"`
	Artifact      ApplicationDownloadArtifact `json:"artifact"`
}

type ApplicationInstallGrant struct {
	Grant         string                      `json:"grant"`
	AppInstanceID string                      `json:"app_instance_id"`
	Application   ApplicationDownloadIdentity `json:"application"`
	Artifact      ApplicationDownloadArtifact `json:"artifact"`
	ExpiresAt     uint32                      `json:"expires_at"`
}

type responseEnvelope struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

type PublicApplication struct {
	Code           string            `json:"code"`
	Name           string            `json:"name"`
	Summary        string            `json:"summary"`
	Category       string            `json:"category"`
	DeliveryMode   string            `json:"delivery_mode"`
	CurrentVersion string            `json:"current_version"`
	DownloadCount  uint64            `json:"download_count"`
	RatingScore    float64           `json:"rating_score"`
	RatingCount    uint64            `json:"rating_count"`
	IsOfficial     bool              `json:"is_official"`
	IsRecommended  bool              `json:"is_recommended"`
	SalesPlans     []PublicSalesPlan `json:"sales_plans"`
}

type PublicTemplate struct {
	Code           string            `json:"code"`
	Name           string            `json:"name"`
	Summary        string            `json:"summary"`
	Category       string            `json:"category"`
	Industries     []string          `json:"industries"`
	Styles         []string          `json:"styles"`
	CurrentVersion string            `json:"current_version"`
	DownloadCount  uint64            `json:"download_count"`
	RatingScore    float64           `json:"rating_score"`
	RatingCount    uint64            `json:"rating_count"`
	IsOfficial     bool              `json:"is_official"`
	IsRecommended  bool              `json:"is_recommended"`
	SalesPlans     []PublicSalesPlan `json:"sales_plans"`
}

type PublicSalesPlan struct {
	ID               uint64 `json:"id"`
	Code             string `json:"code"`
	Name             string `json:"name"`
	ChargeMode       string `json:"charge_mode"`
	BillingUnit      string `json:"billing_unit"`
	MetricCode       string `json:"metric_code"`
	PriceCents       int64  `json:"price_cents"`
	Currency         string `json:"currency"`
	BillingInterval  string `json:"billing_interval"`
	IncludedQuantity uint64 `json:"included_quantity"`
	UpdatePolicy     string `json:"update_policy"`
	IsDefault        bool   `json:"is_default"`
}

type CreateOrderItem struct {
	SalesPlanID uint64 `json:"sales_plan_id"`
	Quantity    uint64 `json:"quantity"`
}

type CreateOrderInput struct {
	Session remoteauth.Session `json:"-"`
	Items   []CreateOrderItem  `json:"items"`
}

type Order struct {
	ID                uint64 `json:"id"`
	OrderNo           string `json:"order_no"`
	Status            string `json:"status"`
	PaymentStatus     string `json:"payment_status"`
	FulfillmentStatus string `json:"fulfillment_status"`
	SubtotalAmount    int64  `json:"subtotal_amount"`
	DiscountAmount    int64  `json:"discount_amount"`
	PointsAmount      int64  `json:"points_amount"`
	PayableAmount     int64  `json:"payable_amount"`
	PaidAmount        int64  `json:"paid_amount"`
	RefundedAmount    int64  `json:"refunded_amount"`
	Currency          string `json:"currency"`
	CreatedAt         uint32 `json:"created_at"`
	UpdatedAt         uint32 `json:"updated_at"`
}

type OrderItem struct {
	ID                   uint64 `json:"id"`
	ResourceType         string `json:"resource_type"`
	ResourceID           uint64 `json:"resource_id"`
	SalesPlanID          uint64 `json:"sales_plan_id"`
	ResourceCodeSnapshot string `json:"resource_code_snapshot"`
	ResourceNameSnapshot string `json:"resource_name_snapshot"`
	PlanCodeSnapshot     string `json:"plan_code_snapshot"`
	PlanNameSnapshot     string `json:"plan_name_snapshot"`
	UnitAmount           int64  `json:"unit_amount"`
	Quantity             uint64 `json:"quantity"`
	SubtotalAmount       int64  `json:"subtotal_amount"`
	DiscountAmount       int64  `json:"discount_amount"`
	PointsAmount         int64  `json:"points_amount"`
	PayableAmount        int64  `json:"payable_amount"`
	FulfillmentStatus    string `json:"fulfillment_status"`
}

type OrderAdjustment struct {
	ID             uint64 `json:"id"`
	OrderItemID    uint64 `json:"order_item_id"`
	AdjustmentType string `json:"adjustment_type"`
	Amount         int64  `json:"amount"`
	Points         int64  `json:"points"`
	Description    string `json:"description"`
}

type OrderPayment struct {
	ID                    uint64 `json:"id"`
	PaymentMethod         string `json:"payment_method"`
	Status                string `json:"status"`
	Amount                int64  `json:"amount"`
	Currency              string `json:"currency"`
	ExternalTransactionID string `json:"external_transaction_id"`
	PaidAt                uint32 `json:"paid_at"`
}

type Refund struct {
	ID             uint64 `json:"id"`
	OrderPaymentID uint64 `json:"order_payment_id"`
	RefundNo       string `json:"refund_no"`
	Status         string `json:"status"`
	Amount         int64  `json:"amount"`
	Reason         string `json:"reason"`
	RefundedAt     uint32 `json:"refunded_at"`
}

type OrderDetail struct {
	Order       Order             `json:"order"`
	Items       []OrderItem       `json:"items"`
	Adjustments []OrderAdjustment `json:"adjustments"`
	Payments    []OrderPayment    `json:"payments"`
	Refunds     []Refund          `json:"refunds"`
}

func (c Client) ListApplications(ctx context.Context, session remoteauth.Session) ([]PublicApplication, error) {
	var result []PublicApplication
	if err := c.readCatalog(ctx, session, "/cloud-api/v1/applications", "Application", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c Client) GetApplication(ctx context.Context, session remoteauth.Session, code string) (PublicApplication, error) {
	var result PublicApplication
	if err := c.readCatalogDetail(ctx, session, "/cloud-api/v1/applications/", "Application", code, &result); err != nil {
		return PublicApplication{}, err
	}
	return result, nil
}

func (c Client) ListTemplates(ctx context.Context, session remoteauth.Session) ([]PublicTemplate, error) {
	var result []PublicTemplate
	if err := c.readCatalog(ctx, session, "/cloud-api/v1/templates", "Template", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c Client) GetTemplate(ctx context.Context, session remoteauth.Session, code string) (PublicTemplate, error) {
	var result PublicTemplate
	if err := c.readCatalogDetail(ctx, session, "/cloud-api/v1/templates/", "Template", code, &result); err != nil {
		return PublicTemplate{}, err
	}
	return result, nil
}

func (c Client) readCatalog(ctx context.Context, session remoteauth.Session, path, resourceName string, output any) error {
	endpoint, err := remoteauth.NormalizeEndpoint(session.Endpoint)
	if err != nil {
		return err
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return fmt.Errorf("Marketplace 平台请求必须使用 auth profile access token")
	}
	if err := c.publicRequest(ctx, endpoint+path, session.AccessToken, output); err != nil {
		return fmt.Errorf("读取 Marketplace %s 列表失败: %w", resourceName, err)
	}
	return nil
}

func (c Client) readCatalogDetail(ctx context.Context, session remoteauth.Session, path, resourceName, code string, output interface{ GetCode() string }) error {
	endpoint, err := remoteauth.NormalizeEndpoint(session.Endpoint)
	if err != nil {
		return err
	}
	if code == "" {
		return fmt.Errorf("%s code 不能为空", resourceName)
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return fmt.Errorf("Marketplace 平台请求必须使用 auth profile access token")
	}
	if err := c.publicRequest(ctx, endpoint+path+url.PathEscape(code), session.AccessToken, output); err != nil {
		return fmt.Errorf("读取 Marketplace %s %q 失败: %w", resourceName, code, err)
	}
	if output.GetCode() == "" {
		return fmt.Errorf("Marketplace %s 详情响应不完整", resourceName)
	}
	return nil
}

func (application *PublicApplication) GetCode() string { return application.Code }
func (template *PublicTemplate) GetCode() string       { return template.Code }

func (c Client) CreateOrder(ctx context.Context, input CreateOrderInput) (OrderDetail, error) {
	if len(input.Items) == 0 {
		return OrderDetail{}, fmt.Errorf("订单至少需要一个订单项")
	}
	for _, item := range input.Items {
		if item.SalesPlanID == 0 || item.Quantity == 0 {
			return OrderDetail{}, fmt.Errorf("订单项的销售方案和数量不能为空")
		}
	}
	body, err := json.Marshal(struct {
		Items []CreateOrderItem `json:"items"`
	}{Items: input.Items})
	if err != nil {
		return OrderDetail{}, err
	}
	var result OrderDetail
	if err := c.orderRequest(ctx, input.Session, http.MethodPost, "/cloud-api/v1/orders", body, &result); err != nil {
		return OrderDetail{}, fmt.Errorf("创建 Marketplace 订单失败: %w", err)
	}
	return result, nil
}

func (c Client) GetOrder(ctx context.Context, session remoteauth.Session, orderNo string) (OrderDetail, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return OrderDetail{}, fmt.Errorf("订单号不能为空")
	}
	var result OrderDetail
	if err := c.orderRequest(ctx, session, http.MethodGet, "/cloud-api/v1/orders/"+url.PathEscape(orderNo), nil, &result); err != nil {
		return OrderDetail{}, fmt.Errorf("读取 Marketplace 订单失败: %w", err)
	}
	return result, nil
}

func (c Client) orderRequest(ctx context.Context, session remoteauth.Session, method, path string, body []byte, output *OrderDetail) error {
	endpoint, err := remoteauth.NormalizeEndpoint(session.Endpoint)
	if err != nil {
		return err
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return fmt.Errorf("Marketplace 订单请求必须使用 auth profile access token")
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)
	request.Header.Set("User-Agent", "sparq-tool")
	if len(body) != 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("Marketplace 接口响应超过 %d 字节限制", maxResponseBytes)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("解析 Marketplace 接口响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || envelope.Code != 0 {
		return &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return fmt.Errorf("解析 Marketplace 订单响应失败: %w", err)
	}
	if output.Order.OrderNo == "" || len(output.Items) == 0 {
		return fmt.Errorf("Marketplace 接口返回的订单不完整")
	}
	return nil
}

func (c Client) publicRequest(ctx context.Context, target, token string, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "sparq-tool")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("请求 Marketplace 接口失败: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("读取 Marketplace 接口响应失败: %w", err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("Marketplace 接口响应超过 %d 字节限制", maxResponseBytes)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("解析 Marketplace 接口响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || envelope.Code != 0 {
		return &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	if len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return fmt.Errorf("Marketplace 接口未返回应用数据")
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return fmt.Errorf("解析 Marketplace 应用数据失败: %w", err)
	}
	return nil
}

func (c Client) UploadApplicationSource(ctx context.Context, input UploadSourceInput) (SourceUpload, error) {
	return c.uploadSource(ctx, input, "/cloud-api/v1/developer/applications/", "Application")
}

func (c Client) PreflightApplicationPublish(ctx context.Context, input PublishPreflightInput) (PublishPreflight, error) {
	if input.ApplicationCode == "" || input.Version == "" || input.IdempotencyKey == "" {
		return PublishPreflight{}, fmt.Errorf("Application code、目标版本和幂等键不能为空")
	}
	body, err := json.Marshal(map[string]string{
		"version": input.Version, "changelog": input.Changelog, "source_ref": input.SourceRef,
		"commit_sha": input.CommitSHA, "pipeline_id": input.PipelineID,
	})
	if err != nil {
		return PublishPreflight{}, fmt.Errorf("编码 Application 发布预检请求失败: %w", err)
	}
	var result PublishPreflight
	path := "/cloud-api/v1/developer/applications/" + url.PathEscape(input.ApplicationCode) + "/publish-preflight"
	if err := c.marketplaceRequestWithHeaders(ctx, input.Session, http.MethodPost, path, body, map[string]string{
		"Idempotency-Key": input.IdempotencyKey,
	}, &result); err != nil {
		return PublishPreflight{}, fmt.Errorf("Application 发布预检失败: %w", err)
	}
	if result.ApplicationID == 0 || result.ApplicationCode != input.ApplicationCode || result.Version != input.Version {
		return PublishPreflight{}, fmt.Errorf("Marketplace 接口返回的 Application 发布预检结果不完整")
	}
	return result, nil
}

func (c Client) UploadTemplateSource(ctx context.Context, input UploadSourceInput) (SourceUpload, error) {
	return c.uploadSource(ctx, input, "/cloud-api/v1/developer/templates/", "Template")
}

// GetApplicationSourceSubmission returns the server-side state used to resume
// candidate uploads without re-uploading completed variants.
func (c Client) GetApplicationSourceSubmission(ctx context.Context, session remoteauth.Session, sourceUploadID uint64) (SourceSubmission, error) {
	if sourceUploadID == 0 {
		return SourceSubmission{}, fmt.Errorf("源码上传 ID 不能为空")
	}
	var result SourceSubmission
	path := fmt.Sprintf("/cloud-api/v1/developer/application-source-uploads/%d", sourceUploadID)
	if err := c.marketplaceRequest(ctx, session, http.MethodGet, path, nil, &result); err != nil {
		return SourceSubmission{}, fmt.Errorf("读取 Application 发布会话失败: %w", err)
	}
	if result.Upload.ID != sourceUploadID || result.Upload.VersionID == 0 || result.Upload.Version == "" {
		return SourceSubmission{}, fmt.Errorf("Marketplace 接口返回的 Application 发布会话不完整")
	}
	return result, nil
}

// FinalizeApplicationSource verifies and freezes the complete candidate list.
func (c Client) FinalizeApplicationSource(ctx context.Context, session remoteauth.Session, sourceUploadID uint64, candidates []CandidateExpectation) (SourceSubmission, error) {
	if sourceUploadID == 0 || len(candidates) == 0 {
		return SourceSubmission{}, fmt.Errorf("源码上传 ID 和候选构建包清单不能为空")
	}
	body, err := json.Marshal(struct {
		Candidates []CandidateExpectation `json:"candidates"`
	}{Candidates: candidates})
	if err != nil {
		return SourceSubmission{}, err
	}
	var result SourceSubmission
	path := fmt.Sprintf("/cloud-api/v1/developer/application-source-uploads/%d/finalize", sourceUploadID)
	if err := c.marketplaceRequest(ctx, session, http.MethodPost, path, body, &result); err != nil {
		return SourceSubmission{}, fmt.Errorf("完成 Application 发布会话失败: %w", err)
	}
	if result.Upload.ID != sourceUploadID || result.Upload.Status != "completed" {
		return SourceSubmission{}, fmt.Errorf("Marketplace 接口未完成 Application 发布会话")
	}
	return result, nil
}

func (c Client) CancelApplicationSource(ctx context.Context, session remoteauth.Session, sourceUploadID uint64) (SourceUpload, error) {
	if sourceUploadID == 0 {
		return SourceUpload{}, fmt.Errorf("源码上传 ID 不能为空")
	}
	var result SourceUpload
	path := fmt.Sprintf("/cloud-api/v1/developer/application-source-uploads/%d/cancel", sourceUploadID)
	if err := c.marketplaceRequest(ctx, session, http.MethodPost, path, []byte("{}"), &result); err != nil {
		return SourceUpload{}, fmt.Errorf("取消 Application 发布会话失败: %w", err)
	}
	if result.ID != sourceUploadID || result.Status != "canceled" {
		return SourceUpload{}, fmt.Errorf("Marketplace 接口未取消 Application 发布会话")
	}
	return result, nil
}

// FinalizeTemplateSource freezes a Template source upload before review.
func (c Client) FinalizeTemplateSource(ctx context.Context, session remoteauth.Session, sourceUploadID uint64) (SourceUpload, error) {
	if sourceUploadID == 0 {
		return SourceUpload{}, fmt.Errorf("源码上传 ID 不能为空")
	}
	var result SourceUpload
	path := fmt.Sprintf("/cloud-api/v1/developer/template-source-uploads/%d/finalize", sourceUploadID)
	if err := c.marketplaceRequest(ctx, session, http.MethodPost, path, []byte("{}"), &result); err != nil {
		return SourceUpload{}, fmt.Errorf("完成 Template 发布会话失败: %w", err)
	}
	if result.ID != sourceUploadID || result.Status != "completed" {
		return SourceUpload{}, fmt.Errorf("Marketplace 接口未完成 Template 发布会话")
	}
	return result, nil
}

func (c Client) marketplaceRequest(ctx context.Context, session remoteauth.Session, method, path string, body []byte, output any) error {
	return c.marketplaceRequestWithHeaders(ctx, session, method, path, body, nil, output)
}

func (c Client) marketplaceRequestWithHeaders(ctx context.Context, session remoteauth.Session, method, path string, body []byte, headers map[string]string, output any) error {
	endpoint, err := remoteauth.NormalizeEndpoint(session.Endpoint)
	if err != nil {
		return err
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return fmt.Errorf("本地登录凭据缺少 access token")
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)
	request.Header.Set("User-Agent", "sparq-tool")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("请求 Marketplace 接口失败: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("读取 Marketplace 接口响应失败: %w", err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("Marketplace 接口响应超过 %d 字节限制", maxResponseBytes)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("解析 Marketplace 接口响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || envelope.Code != 0 {
		return &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	if len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return fmt.Errorf("Marketplace 接口未返回发布会话结果")
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return fmt.Errorf("解析 Marketplace 发布会话结果失败: %w", err)
	}
	return nil
}

func (c Client) uploadSource(ctx context.Context, input UploadSourceInput, path, resourceName string) (SourceUpload, error) {
	endpoint, err := remoteauth.NormalizeEndpoint(input.Session.Endpoint)
	if err != nil {
		return SourceUpload{}, err
	}
	if input.Session.AccessToken == "" {
		return SourceUpload{}, fmt.Errorf("本地登录凭据缺少 access token")
	}
	if input.ApplicationCode == "" || input.ArchivePath == "" || input.IdempotencyKey == "" {
		return SourceUpload{}, fmt.Errorf("%s code、源码包路径和幂等键不能为空", resourceName)
	}
	archive, err := os.Open(input.ArchivePath)
	if err != nil {
		return SourceUpload{}, fmt.Errorf("打开源码包失败: %w", err)
	}
	defer archive.Close()

	body, contentType, err := createMultipartBody("sparq-source-upload-*", func(writer *multipart.Writer) error {
		return writeMultipart(writer, archive, filepath.Base(input.ArchivePath), input)
	})
	if err != nil {
		return SourceUpload{}, fmt.Errorf("准备 %s 源码上传请求失败: %w", resourceName, err)
	}
	defer body.Close()
	defer os.Remove(body.Name())

	target := endpoint + path + url.PathEscape(input.ApplicationCode) + "/source-uploads"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		return SourceUpload{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+input.Session.AccessToken)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Idempotency-Key", input.IdempotencyKey)
	request.Header.Set("User-Agent", "sparq-tool")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return SourceUpload{}, fmt.Errorf("上传 %s 源码失败: %w", resourceName, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return SourceUpload{}, fmt.Errorf("读取 Marketplace 接口响应失败: %w", err)
	}
	if len(data) > maxResponseBytes {
		return SourceUpload{}, fmt.Errorf("Marketplace 接口响应超过 %d 字节限制", maxResponseBytes)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return SourceUpload{}, fmt.Errorf("解析 Marketplace 接口响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || envelope.Code != 0 {
		return SourceUpload{}, &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	var result SourceUpload
	if len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return SourceUpload{}, fmt.Errorf("Marketplace 接口未返回源码上传结果")
	}
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return SourceUpload{}, fmt.Errorf("解析源码上传结果失败: %w", err)
	}
	if result.ID == 0 || result.VersionID == 0 || result.Version == "" {
		return SourceUpload{}, fmt.Errorf("Marketplace 接口返回的源码上传结果不完整")
	}
	return result, nil
}

// UploadCandidateArtifact 上传本地 package v1。该包是候选产物，不是正式 Marketplace Artifact。
func (c Client) UploadCandidateArtifact(ctx context.Context, input UploadCandidateArtifactInput) (CandidateArtifact, error) {
	endpoint, err := remoteauth.NormalizeEndpoint(input.Session.Endpoint)
	if err != nil {
		return CandidateArtifact{}, err
	}
	if input.Session.AccessToken == "" {
		return CandidateArtifact{}, fmt.Errorf("本地登录凭据缺少 access token")
	}
	if input.SourceUploadID == 0 || input.SourceSHA256 == "" || input.ArchivePath == "" || input.Format == "" || input.Kind == "" || input.IdempotencyKey == "" {
		return CandidateArtifact{}, fmt.Errorf("源码上传 ID、源码摘要、候选包路径、格式、类型和幂等键不能为空")
	}
	if input.Kind != CandidateKindPluginPackage && input.Kind != ArtifactKindFrontendBundle && input.Kind != ArtifactKindManagedRemote {
		return CandidateArtifact{}, fmt.Errorf("不支持的候选构建包类型 %q", input.Kind)
	}
	if input.Kind == ArtifactKindFrontendBundle && (input.Runtime != "" || input.TargetOS != "" || input.TargetArch != "") {
		return CandidateArtifact{}, fmt.Errorf("前端候选包不能设置 Runtime 或目标平台")
	}
	if input.Kind == ArtifactKindManagedRemote && (input.Runtime != "http" || input.TargetOS == "" || input.TargetArch == "") {
		return CandidateArtifact{}, fmt.Errorf("Managed Remote 候选包必须设置 http Runtime 和目标平台")
	}
	if input.Kind == CandidateKindPluginPackage && (input.Runtime != "" || input.TargetOS != "" || input.TargetArch != "") {
		return CandidateArtifact{}, fmt.Errorf("兼容候选包不能设置 Runtime 或目标平台")
	}
	archive, err := os.Open(input.ArchivePath)
	if err != nil {
		return CandidateArtifact{}, fmt.Errorf("打开候选构建包失败: %w", err)
	}
	defer archive.Close()

	body, contentType, err := createMultipartBody("sparq-candidate-upload-*", func(multipartWriter *multipart.Writer) error {
		writeErr := multipartWriter.WriteField("source_sha256", input.SourceSHA256)
		if writeErr == nil {
			writeErr = multipartWriter.WriteField("format", input.Format)
		}
		if writeErr == nil {
			writeErr = multipartWriter.WriteField("kind", input.Kind)
		}
		if writeErr == nil {
			writeErr = multipartWriter.WriteField("runtime", input.Runtime)
		}
		if writeErr == nil {
			writeErr = multipartWriter.WriteField("target_os", input.TargetOS)
		}
		if writeErr == nil {
			writeErr = multipartWriter.WriteField("target_arch", input.TargetArch)
		}
		if writeErr == nil {
			var part io.Writer
			part, writeErr = multipartWriter.CreateFormFile("file", filepath.Base(input.ArchivePath))
			if writeErr == nil {
				_, writeErr = io.Copy(part, archive)
			}
		}
		return writeErr
	})
	if err != nil {
		return CandidateArtifact{}, fmt.Errorf("准备候选构建包上传请求失败: %w", err)
	}
	defer body.Close()
	defer os.Remove(body.Name())
	target := fmt.Sprintf("%s/cloud-api/v1/developer/application-source-uploads/%d/candidate-artifacts", endpoint, input.SourceUploadID)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		return CandidateArtifact{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+input.Session.AccessToken)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Idempotency-Key", input.IdempotencyKey)
	request.Header.Set("User-Agent", "sparq-tool")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return CandidateArtifact{}, fmt.Errorf("上传候选构建包失败: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return CandidateArtifact{}, fmt.Errorf("读取 Marketplace 接口响应失败: %w", err)
	}
	if len(data) > maxResponseBytes {
		return CandidateArtifact{}, fmt.Errorf("Marketplace 接口响应超过 %d 字节限制", maxResponseBytes)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return CandidateArtifact{}, fmt.Errorf("解析 Marketplace 接口响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || envelope.Code != 0 {
		return CandidateArtifact{}, &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	var result CandidateArtifact
	if len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return CandidateArtifact{}, fmt.Errorf("Marketplace 接口未返回候选构建包结果")
	}
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return CandidateArtifact{}, fmt.Errorf("解析候选构建包结果失败: %w", err)
	}
	if result.ID == 0 || result.SourceUploadID != input.SourceUploadID || result.VersionID == 0 || result.Format == "" {
		return CandidateArtifact{}, fmt.Errorf("Marketplace 接口返回的候选构建包结果不完整")
	}
	return result, nil
}

func createMultipartBody(pattern string, write func(*multipart.Writer) error) (*os.File, string, error) {
	body, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, "", err
	}
	remove := true
	defer func() {
		if remove {
			_ = body.Close()
			_ = os.Remove(body.Name())
		}
	}()
	writer := multipart.NewWriter(body)
	contentType := writer.FormDataContentType()
	if err := write(writer); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	remove = false
	return body, contentType, nil
}

func (c Client) ResolveApplicationDownload(ctx context.Context, input ResolveApplicationDownloadInput) (ApplicationDownloadResolution, error) {
	endpoint, err := remoteauth.NormalizeEndpoint(input.Session.Endpoint)
	if err != nil {
		return ApplicationDownloadResolution{}, err
	}
	if input.Session.AccessToken == "" {
		return ApplicationDownloadResolution{}, fmt.Errorf("本地登录凭据缺少 access token")
	}
	if input.ApplicationCode == "" || input.ArtifactKind == "" {
		return ApplicationDownloadResolution{}, fmt.Errorf("Application code 和 Artifact 类型不能为空")
	}
	body, err := json.Marshal(map[string]any{
		"application_code": input.ApplicationCode, "version": input.Version,
		"artifact_kind": input.ArtifactKind, "target": input.Target,
	})
	if err != nil {
		return ApplicationDownloadResolution{}, fmt.Errorf("编码 Artifact 下载解析请求失败: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/cloud-api/v1/application-downloads/resolve", bytes.NewReader(body))
	if err != nil {
		return ApplicationDownloadResolution{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+input.Session.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "sparq-tool")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return ApplicationDownloadResolution{}, fmt.Errorf("解析 Application Artifact 下载失败: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return ApplicationDownloadResolution{}, fmt.Errorf("读取 Marketplace 接口响应失败: %w", err)
	}
	if len(data) > maxResponseBytes {
		return ApplicationDownloadResolution{}, fmt.Errorf("Marketplace 接口响应超过 %d 字节限制", maxResponseBytes)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ApplicationDownloadResolution{}, fmt.Errorf("解析 Marketplace 接口响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || envelope.Code != 0 {
		return ApplicationDownloadResolution{}, &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	var result ApplicationDownloadResolution
	if len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return ApplicationDownloadResolution{}, fmt.Errorf("Marketplace 接口未返回 Application 下载解析结果")
	}
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return ApplicationDownloadResolution{}, fmt.Errorf("解析 Application Artifact 下载结果失败: %w", err)
	}
	if result.Application.Code == "" {
		return ApplicationDownloadResolution{}, fmt.Errorf("Marketplace 接口返回的 Application 下载解析结果不完整")
	}
	return result, nil
}

func (c Client) CreateApplicationInstallGrant(ctx context.Context, input ResolveApplicationDownloadInput) (ApplicationInstallGrant, error) {
	endpoint, err := remoteauth.NormalizeEndpoint(input.Session.Endpoint)
	if err != nil {
		return ApplicationInstallGrant{}, err
	}
	if input.Session.AccessToken == "" || input.ApplicationCode == "" || input.ArtifactKind == "" || input.Target.AppInstanceID == "" {
		return ApplicationInstallGrant{}, fmt.Errorf("Cloud 登录态、Application code、Artifact 类型和宿主实例 ID 不能为空")
	}
	payload, err := json.Marshal(map[string]any{"download": map[string]any{
		"application_code": input.ApplicationCode, "version": input.Version, "artifact_kind": input.ArtifactKind, "target": input.Target,
	}})
	if err != nil {
		return ApplicationInstallGrant{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/cloud-api/v1/application-install-grants", bytes.NewReader(payload))
	if err != nil {
		return ApplicationInstallGrant{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+input.Session.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "sparq-tool")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return ApplicationInstallGrant{}, fmt.Errorf("创建 Website Application Install Grant 失败: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return ApplicationInstallGrant{}, fmt.Errorf("读取 Application Install Grant 响应失败: %w", err)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ApplicationInstallGrant{}, fmt.Errorf("解析 Application Install Grant 响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Code != 0 {
		return ApplicationInstallGrant{}, &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	var result ApplicationInstallGrant
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return ApplicationInstallGrant{}, err
	}
	if result.Grant == "" || result.AppInstanceID != input.Target.AppInstanceID || result.Application.Code == "" {
		return ApplicationInstallGrant{}, fmt.Errorf("Website 返回的 Application Install Grant 不完整或绑定了其他宿主")
	}
	return result, nil
}

func (c Client) ResolveTemplateDownload(ctx context.Context, input ResolveTemplateDownloadInput) (TemplateDownloadResolution, error) {
	endpoint, err := remoteauth.NormalizeEndpoint(input.Session.Endpoint)
	if err != nil {
		return TemplateDownloadResolution{}, err
	}
	if input.Session.AccessToken == "" || input.TemplateCode == "" {
		return TemplateDownloadResolution{}, fmt.Errorf("Cloud 登录态和 Template code 不能为空")
	}
	body, err := json.Marshal(map[string]any{"template_code": input.TemplateCode, "version": input.Version})
	if err != nil {
		return TemplateDownloadResolution{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/cloud-api/v1/template-downloads/resolve", bytes.NewReader(body))
	if err != nil {
		return TemplateDownloadResolution{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+input.Session.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "sparq-tool")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return TemplateDownloadResolution{}, fmt.Errorf("解析 Template 下载失败: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return TemplateDownloadResolution{}, fmt.Errorf("读取 Template 下载响应失败: %w", err)
	}
	var envelope responseEnvelope
	if err = json.Unmarshal(data, &envelope); err != nil {
		return TemplateDownloadResolution{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Code != 0 {
		return TemplateDownloadResolution{}, &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	var result TemplateDownloadResolution
	if err = json.Unmarshal(envelope.Data, &result); err != nil {
		return TemplateDownloadResolution{}, err
	}
	if result.Template.Code == "" {
		return TemplateDownloadResolution{}, fmt.Errorf("Template 下载响应不完整")
	}
	return result, nil
}

func (c Client) CreateTemplateInstallGrant(ctx context.Context, session remoteauth.Session, code, version, instanceID string) (TemplateInstallGrant, error) {
	endpoint, err := remoteauth.NormalizeEndpoint(session.Endpoint)
	if err != nil {
		return TemplateInstallGrant{}, err
	}
	if session.AccessToken == "" || code == "" || instanceID == "" {
		return TemplateInstallGrant{}, fmt.Errorf("Cloud 登录态、Template code 和宿主实例 ID 不能为空")
	}
	body, err := json.Marshal(map[string]any{"download": map[string]any{"template_code": code, "version": version}, "app_instance_id": instanceID})
	if err != nil {
		return TemplateInstallGrant{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/cloud-api/v1/template-install-grants", bytes.NewReader(body))
	if err != nil {
		return TemplateInstallGrant{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "sparq-tool")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return TemplateInstallGrant{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return TemplateInstallGrant{}, fmt.Errorf("读取 Template Install Grant 响应失败: %w", err)
	}
	var envelope responseEnvelope
	if err = json.Unmarshal(data, &envelope); err != nil {
		return TemplateInstallGrant{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Code != 0 {
		return TemplateInstallGrant{}, &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	var result TemplateInstallGrant
	if err = json.Unmarshal(envelope.Data, &result); err != nil {
		return TemplateInstallGrant{}, err
	}
	if result.Grant == "" || result.Template.Code != code || result.Template.Version == "" || !result.Artifact.Available || result.Artifact.Kind != ArtifactKindTemplateSource {
		return TemplateInstallGrant{}, fmt.Errorf("Template Install Grant 响应不完整")
	}
	return result, nil
}

func writeMultipart(writer *multipart.Writer, archive io.Reader, filename string, input UploadSourceInput) error {
	for name, value := range map[string]string{
		"changelog": input.Changelog, "source_ref": input.SourceRef,
		"commit_sha": input.CommitSHA, "pipeline_id": input.PipelineID,
	} {
		if value == "" {
			continue
		}
		if err := writer.WriteField(name, value); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, archive)
	return err
}
