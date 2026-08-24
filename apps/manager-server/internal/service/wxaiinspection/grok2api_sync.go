package wxaiinspection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

const (
	grok2apiRequestTimeout  = 20 * time.Second
	grok2apiMaxResponseBody = 4 << 20 // 4MB
	grok2apiLoginPath       = "/api/admin/v1/auth/login"
	grok2apiConsoleSyncPath = "/api/admin/v1/cpa-auto-proxy/console-accounts/sync"

	Grok2apiSyncTriggerKeepalive = "keepalive"
	Grok2apiSyncTriggerManual    = "manual"
)

var (
	ErrGrok2apiNotConfigured      = errors.New("grok2api 同步未启用或配置不完整")
	ErrGrok2apiInvalidCredentials = errors.New("用户名或密码错误")
	ErrGrok2apiUnauthorized       = errors.New("grok2api 鉴权失败")
	ErrGrok2apiLoginRateLimited   = errors.New("登录过于频繁，请稍后重试")
	ErrGrok2apiNoHealthyAccounts  = errors.New("没有可同步的健康账号")
)

type Grok2apiSyncResponse struct {
	Trigger  string `json:"trigger"`
	Synced   int    `json:"synced"`
	Response any    `json:"response,omitempty"`
}

// Grok2apiTestRequest 测试连接请求：携带表单当前值；密码留空=用已存密码。
type Grok2apiTestRequest struct {
	BaseUrl  string `json:"baseUrl"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

// grok2apiSyncMutex 防并发：同一时刻只允许一次同步流程。
var grok2apiSyncMutex sync.Mutex

type grok2apiLoginResult struct {
	AccessToken string
}

// SyncHealthyAccountsToGrok2api 把指定邮箱账号推送到 grok2api console。
// 流程：login 拿 accessToken → 带 Bearer 调 console-accounts/sync →
// 若返回 401 则重新 login 再试一次；重登后仍失败即报错。
func (service *Service) SyncHealthyAccountsToGrok2api(
	ctx context.Context,
	trigger string,
	emails []string,
) (Grok2apiSyncResponse, error) {
	settings, _, err := service.resolveRuntime(ctx)
	if err != nil {
		return Grok2apiSyncResponse{}, err
	}
	if !isWxaiGrok2apiSyncConfigured(settings) {
		return Grok2apiSyncResponse{}, ErrGrok2apiNotConfigured
	}
	if len(emails) == 0 {
		return Grok2apiSyncResponse{}, ErrGrok2apiNoHealthyAccounts
	}

	grok2apiSyncMutex.Lock()
	defer grok2apiSyncMutex.Unlock()

	client := &http.Client{Timeout: grok2apiRequestTimeout}
	token, err := service.grok2apiLogin(ctx, client, settings)
	if err != nil {
		return Grok2apiSyncResponse{}, err
	}
	responseBody, err := service.grok2apiPushAccounts(ctx, client, settings, token, emails)
	if errors.Is(err, ErrGrok2apiUnauthorized) {
		token, loginErr := service.grok2apiLogin(ctx, client, settings)
		if loginErr != nil {
			return Grok2apiSyncResponse{}, loginErr
		}
		responseBody, err = service.grok2apiPushAccounts(ctx, client, settings, token, emails)
	}
	if err != nil {
		return Grok2apiSyncResponse{}, err
	}
	return Grok2apiSyncResponse{Trigger: trigger, Synced: len(emails), Response: responseBody}, nil
}

// syncGrok2apiAfterRun 巡检完成后自动同步一次健康账号（trigger=keepalive）。
// 同步失败只记日志，不影响巡检结果。
func (service *Service) syncGrok2apiAfterRun(ctx context.Context, runID int64, emails []string) {
	logger := runLogger{service: service, runID: runID}
	_, err := service.SyncHealthyAccountsToGrok2api(ctx, Grok2apiSyncTriggerKeepalive, emails)
	if err != nil {
		logger.error(context.WithoutCancel(ctx), "同步健康账号到 Grok2Api Console 失败", map[string]any{
			"trigger": Grok2apiSyncTriggerKeepalive,
			"count":   len(emails),
			"error":   err.Error(),
		})
		return
	}
	logger.success(context.WithoutCancel(ctx), "健康账号已同步到 Grok2Api Console", map[string]any{
		"trigger": Grok2apiSyncTriggerKeepalive,
		"count":   len(emails),
	})
}

// TestGrok2apiConnection 用表单当前值测试 grok2api 连通性：只执行 login。
// 密码留空=使用已存密码；URL/用户名留空时回退已存配置。
func (service *Service) TestGrok2apiConnection(ctx context.Context, request Grok2apiTestRequest) error {
	stored, _, err := service.store.GetWxaiInspectionSettings(ctx)
	if err != nil {
		return err
	}
	baseUrl := strings.TrimRight(strings.TrimSpace(request.BaseUrl), "/")
	if baseUrl == "" {
		baseUrl = strings.TrimRight(strings.TrimSpace(stored.Grok2apiBaseUrl), "/")
	}
	username := strings.TrimSpace(request.Username)
	if username == "" {
		username = strings.TrimSpace(stored.Grok2apiAdminUsername)
	}
	password := request.Password
	if strings.TrimSpace(password) == "" {
		password = stored.Grok2apiAdminPassword
	}
	settings := model.ManagerWxaiInspectionConfig{
		Grok2apiBaseUrl:       baseUrl,
		Grok2apiAdminUsername: username,
		Grok2apiAdminPassword: password,
	}
	if err := model.ValidateWxaiGrok2apiBaseUrl(baseUrl); err != nil {
		return err
	}
	if baseUrl == "" || username == "" || password == "" {
		return ErrGrok2apiNotConfigured
	}

	grok2apiSyncMutex.Lock()
	defer grok2apiSyncMutex.Unlock()
	client := &http.Client{Timeout: grok2apiRequestTimeout}
	_, err = service.grok2apiLogin(ctx, client, settings)
	if err != nil {
		return err
	}
	return nil
}

func isWxaiGrok2apiSyncConfigured(settings model.ManagerWxaiInspectionConfig) bool {
	if settings.Grok2apiSyncEnabled == nil || !*settings.Grok2apiSyncEnabled {
		return false
	}
	return strings.TrimSpace(settings.Grok2apiBaseUrl) != "" &&
		strings.TrimSpace(settings.Grok2apiAdminUsername) != "" &&
		strings.TrimSpace(settings.Grok2apiAdminPassword) != ""
}

func (service *Service) grok2apiLogin(
	ctx context.Context,
	client *http.Client,
	settings model.ManagerWxaiInspectionConfig,
) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"username": settings.Grok2apiAdminUsername,
		"password": settings.Grok2apiAdminPassword,
	})
	if err != nil {
		return "", err
	}
	statusCode, body, err := service.grok2apiDoJSON(ctx, client, http.MethodPost,
		settings.Grok2apiBaseUrl+grok2apiLoginPath, payload, "")
	if err != nil {
		return "", err
	}
	switch statusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrGrok2apiInvalidCredentials
	case http.StatusTooManyRequests:
		return "", ErrGrok2apiLoginRateLimited
	default:
		return "", fmt.Errorf("grok2api 登录失败: HTTP %d: %s", statusCode, truncate(string(body), 256))
	}

	var parsed struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"accessToken"`
			} `json:"tokens"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("解析 grok2api 登录响应失败: %w", err)
	}
	token := strings.TrimSpace(parsed.Data.Tokens.AccessToken)
	if token == "" {
		return "", fmt.Errorf("grok2api 登录响应缺少 data.tokens.accessToken")
	}
	return token, nil
}

func (service *Service) grok2apiPushAccounts(
	ctx context.Context,
	client *http.Client,
	settings model.ManagerWxaiInspectionConfig,
	accessToken string,
	emails []string,
) (any, error) {
	payload, err := json.Marshal(map[string]any{"emails": emails})
	if err != nil {
		return nil, err
	}
	statusCode, body, err := service.grok2apiDoJSON(ctx, client, http.MethodPost,
		settings.Grok2apiBaseUrl+grok2apiConsoleSyncPath, payload, accessToken)
	if err != nil {
		return nil, err
	}
	switch statusCode {
	case http.StatusOK, http.StatusCreated:
		var parsed any
		if err := json.Unmarshal(body, &parsed); err != nil {
			return map[string]any{"raw": string(body)}, nil
		}
		return parsed, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrGrok2apiUnauthorized
	default:
		return nil, formatGrok2apiHTTPError(statusCode, body)
	}
}

// formatGrok2apiHTTPError 把 grok2api 业务错误码映射为可读错误。
func formatGrok2apiHTTPError(statusCode int, body []byte) error {
	var parsed struct {
		Error   string `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)
	code := strings.TrimSpace(parsed.Code)
	message := strings.TrimSpace(parsed.Message)
	switch code {
	case "invalidCredentials":
		return ErrGrok2apiInvalidCredentials
	case "adminUnauthorized":
		return ErrGrok2apiUnauthorized
	case "loginRateLimited":
		return ErrGrok2apiLoginRateLimited
	}
	if message == "" {
		message = truncate(string(body), 256)
	}
	return fmt.Errorf("grok2api 同步失败: HTTP %d%s%s",
		statusCode, codePrefix(code), messageSuffix(message))
}

func codePrefix(code string) string {
	if strings.TrimSpace(code) == "" {
		return ""
	}
	return fmt.Sprintf(" [%s]", code)
}

func messageSuffix(message string) string {
	if strings.TrimSpace(message) == "" {
		return ""
	}
	return fmt.Sprintf(": %s", message)
}

func (service *Service) grok2apiDoJSON(
	ctx context.Context,
	client *http.Client,
	method string,
	url string,
	payload []byte,
	accessToken string,
) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("请求 grok2api 失败: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, grok2apiMaxResponseBody))
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("读取 grok2api 响应失败: %w", err)
	}
	return response.StatusCode, body, nil
}

// collectHealthyAccountEmails 从巡检结果里取健康账号的邮箱地址。
// 健康定义：非异常（无 Error/ErrorKind）且非额度耗尽。
func collectHealthyAccountEmails(results []model.WxaiInspectionResult) []string {
	seen := make(map[string]struct{}, len(results))
	emails := make([]string, 0, len(results))
	for _, result := range results {
		email := normalizeGrok2apiEmail(result.DisplayAccount)
		if email == "" {
			continue
		}
		if countSingleResultAbnormal(result) || result.IsQuota {
			continue
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}
	return emails
}

func countSingleResultAbnormal(result model.WxaiInspectionResult) bool {
	return result.Error != "" ||
		result.ErrorKind == "account_abnormal" ||
		result.ErrorKind == "request_error" ||
		result.ErrorKind == "missing_auth_index"
}

// normalizeGrok2apiEmail 只保留形如邮箱的 displayAccount。
func normalizeGrok2apiEmail(value string) string {
	trimmed := strings.TrimSpace(value)
	at := strings.Index(trimmed, "@")
	if at <= 0 || at == len(trimmed)-1 {
		return ""
	}
	return strings.ToLower(trimmed)
}
