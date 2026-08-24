package wxaiinspection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// performWxaiBillingProxyCall 由 Manager 经 auth JSON proxy_url 请求 xAI billing/credits，
// 不经 CPA /v0/management/api-call。
func (service *Service) performWxaiBillingProxyCall(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	authIndex string,
	endpoint string,
	accessToken string,
	userID string,
	logger runLogger,
) (wxaiHTTPResponse, error) {
	if client == nil {
		return wxaiHTTPResponse{}, fmt.Errorf("xAI billing 代理 client 未初始化")
	}
	if strings.TrimSpace(accessToken) == "" {
		return wxaiHTTPResponse{}, fmt.Errorf("xAI billing 代理缺少 access_token")
	}

	requestMetadata := ctx.Value(wxaiInspectionRequestMetadataContextKey{}).(wxaiInspectionRequestMetadata)
	var lastErr error
	for attempt := 0; attempt <= wxaiTimeoutRetryCount; attempt++ {
		if attempt > 0 {
			if err := waitForWxaiRetry(ctx, wxaiTimeoutRetryBackoff); err != nil {
				return wxaiHTTPResponse{}, err
			}
		}
		requestDetail := map[string]any{
			"requestStage":        resolveWxaiRequestStage(endpoint),
			"transport":           "proxy",
			"viaCPAApiCall":       false,
			"proxyConfigured":     true,
			"endpoint":            endpoint,
			"method":              http.MethodGet,
			"accountKey":          requestMetadata.AccountKey,
			"fileName":            requestMetadata.FileName,
			"authIndex":           authIndex,
			"userIdHeaderPresent": strings.TrimSpace(userID) != "",
			"clientVersion":       wxaiBillingClientVersion,
			"userAgent":           wxaiBillingUserAgent,
			"timeoutMs":           timeoutMilliseconds,
			"attempt":             attempt + 1,
			"maxAttempts":         wxaiTimeoutRetryCount + 1,
		}
		logger.info(ctx, "wXAi billing 代理请求诊断", requestDetail)
		response, err := service.performWxaiRequestOnce(
			ctx,
			client,
			timeoutMilliseconds,
			http.MethodGet,
			endpoint,
			nil,
			wxaiBillingHeaders(accessToken, userID),
		)
		if err == nil {
			responseDetail := buildWxaiBillingResponseDiagnostic(
				endpoint,
				authIndex,
				response,
				attempt+1,
			)
			responseDetail["accountKey"] = requestMetadata.AccountKey
			responseDetail["fileName"] = requestMetadata.FileName
			responseDetail["userIdHeaderPresent"] = strings.TrimSpace(userID) != ""
			responseDetail["transport"] = "proxy"
			responseDetail["viaCPAApiCall"] = false
			responseDetail["proxyConfigured"] = true
			logger.info(ctx, "wXAi billing 代理响应诊断", responseDetail)
			return response, nil
		}
		lastErr = err
		requestDetail["error"] = err.Error()
		requestDetail["timeout"] = isWxaiTimeoutError(err)
		logger.warning(ctx, "wXAi billing 代理请求失败", requestDetail)
		if !isWxaiTimeoutError(err) || ctx.Err() != nil {
			return wxaiHTTPResponse{}, err
		}
	}
	return wxaiHTTPResponse{}, lastErr
}

func buildWxaiBillingResponseDiagnostic(
	endpoint string,
	authIndex string,
	response wxaiHTTPResponse,
	attempt int,
) map[string]any {
	detail := map[string]any{
		"requestStage":  resolveWxaiRequestStage(endpoint),
		"authIndex":     authIndex,
		"attempt":       attempt,
		"statusCode":    response.StatusCode,
		"bodyBytes":     len(response.Body),
		"bodyTruncated": response.BodyTruncated,
		"finalURL":      response.FinalURL,
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		detail["jsonValid"] = false
		detail["jsonError"] = err.Error()
		return detail
	}
	detail["jsonValid"] = true
	detail["topLevelKeys"] = sortedWxaiDiagnosticKeys(payload)

	config, configPresent := payload["config"].(map[string]any)
	detail["configPresent"] = configPresent
	if !configPresent {
		return detail
	}
	detail["configKeys"] = sortedWxaiDiagnosticKeys(config)

	if endpoint == wxaiBillingURL {
		appendWxaiDiagnosticField(detail, config, "monthlyLimit")
		appendWxaiDiagnosticField(detail, config, "used")
		appendWxaiDiagnosticField(detail, config, "billingPeriodStart")
		appendWxaiDiagnosticField(detail, config, "billingPeriodEnd")
		monthlyLimit, monthlyLimitPresent := wxaiDiagnosticNestedNumber(config["monthlyLimit"])
		_, monthlyUsedPresent := wxaiDiagnosticNestedNumber(config["used"])
		detail["quotaWindowEligible"] = monthlyLimitPresent && monthlyLimit > 0 &&
			monthlyUsedPresent && parseWxaiBillingTime(wxaiDiagnosticString(config["billingPeriodEnd"])) > 0
		return detail
	}

	appendWxaiDiagnosticField(detail, config, "creditUsagePercent")
	appendWxaiDiagnosticField(detail, config, "billingPeriodStart")
	appendWxaiDiagnosticField(detail, config, "billingPeriodEnd")
	currentPeriod, currentPeriodPresent := config["currentPeriod"].(map[string]any)
	detail["currentPeriodPresent"] = currentPeriodPresent
	if currentPeriodPresent {
		detail["currentPeriodKeys"] = sortedWxaiDiagnosticKeys(currentPeriod)
		appendWxaiDiagnosticField(detail, currentPeriod, "type")
		appendWxaiDiagnosticField(detail, currentPeriod, "start")
		appendWxaiDiagnosticField(detail, currentPeriod, "end")
	}
	periodType := strings.TrimSpace(wxaiDiagnosticString(currentPeriod["type"]))
	resetAtMS := parseWxaiBillingTime(firstNonEmpty(
		wxaiDiagnosticString(currentPeriod["end"]),
		wxaiDiagnosticString(config["billingPeriodEnd"]),
	))
	_, creditUsagePercentPresent := wxaiDiagnosticNestedNumber(config["creditUsagePercent"])
	detail["quotaWindowEligible"] = creditUsagePercentPresent && periodType != "" && resetAtMS > 0
	return detail
}

func appendWxaiDiagnosticField(detail map[string]any, source map[string]any, fieldName string) {
	value, present := source[fieldName]
	detail[fieldName+"Present"] = present
	if !present {
		return
	}
	detail[fieldName] = sanitizeWxaiDiagnosticValue(value)
}

func sanitizeWxaiDiagnosticValue(value any) any {
	switch typedValue := value.(type) {
	case nil, bool, float64, string:
		return typedValue
	case map[string]any:
		if nestedValue, present := typedValue["val"]; present {
			return sanitizeWxaiDiagnosticValue(nestedValue)
		}
		return map[string]any{"valueType": "object", "keys": sortedWxaiDiagnosticKeys(typedValue)}
	case []any:
		return map[string]any{"valueType": "array", "length": len(typedValue)}
	default:
		return fmt.Sprintf("%T", value)
	}
}

func wxaiDiagnosticNestedNumber(value any) (float64, bool) {
	switch typedValue := value.(type) {
	case float64:
		return typedValue, true
	case map[string]any:
		nestedValue, present := typedValue["val"]
		if !present {
			return 0, false
		}
		return wxaiDiagnosticNestedNumber(nestedValue)
	default:
		return 0, false
	}
}

func wxaiDiagnosticString(value any) string {
	stringValue, _ := value.(string)
	return strings.TrimSpace(stringValue)
}

func sortedWxaiDiagnosticKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
