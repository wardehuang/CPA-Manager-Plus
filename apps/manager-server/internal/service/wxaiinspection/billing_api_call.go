package wxaiinspection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const wxaiManagementAPICallResponseLimit = 8 * 1024 * 1024

type wxaiManagementAPICallRequest struct {
	AuthIndex string            `json:"authIndex"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Header    map[string]string `json:"header"`
}

type wxaiManagementAPICallResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       string              `json:"body"`
}

func (service *Service) performWxaiBillingAPICall(
	ctx context.Context,
	setup store.Setup,
	timeoutMilliseconds int,
	authIndex string,
	endpoint string,
	userID string,
	logger runLogger,
) (wxaiHTTPResponse, error) {
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
		logger.info(ctx, "wXAi billing api-call 请求诊断", requestDetail)
		response, err := service.performWxaiBillingAPICallOnce(
			ctx,
			setup,
			timeoutMilliseconds,
			authIndex,
			endpoint,
			userID,
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
			logger.info(ctx, "wXAi billing api-call 响应诊断", responseDetail)
			return response, nil
		}
		lastErr = err
		requestDetail["error"] = err.Error()
		requestDetail["timeout"] = isWxaiTimeoutError(err)
		logger.warning(ctx, "wXAi billing api-call 请求失败", requestDetail)
		if !isWxaiTimeoutError(err) || ctx.Err() != nil {
			return wxaiHTTPResponse{}, err
		}
	}
	return wxaiHTTPResponse{}, lastErr
}

func (service *Service) performWxaiBillingAPICallOnce(
	ctx context.Context,
	setup store.Setup,
	timeoutMilliseconds int,
	authIndex string,
	endpoint string,
	userID string,
) (wxaiHTTPResponse, error) {
	requestBody, err := json.Marshal(wxaiManagementAPICallRequest{
		AuthIndex: authIndex,
		Method:    http.MethodGet,
		URL:       endpoint,
		Header:    wxaiBillingHeaders("$TOKEN$", userID),
	})
	if err != nil {
		return wxaiHTTPResponse{}, err
	}

	requestContext := ctx
	cancel := func() {}
	if timeoutMilliseconds > 0 {
		requestContext, cancel = context.WithTimeout(ctx, time.Duration(timeoutMilliseconds)*time.Millisecond)
	}
	defer cancel()

	request, err := http.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		cpa.NormalizeBaseURL(setup.CPAUpstreamURL)+"/v0/management/api-call",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return wxaiHTTPResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+setup.ManagementKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := service.client.Do(request)
	if err != nil {
		return wxaiHTTPResponse{}, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, wxaiManagementAPICallResponseLimit+1))
	if err != nil {
		return wxaiHTTPResponse{}, err
	}
	if len(responseBody) > wxaiManagementAPICallResponseLimit {
		return wxaiHTTPResponse{}, fmt.Errorf("CPA api-call response exceeds %d bytes", wxaiManagementAPICallResponseLimit)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return wxaiHTTPResponse{}, fmt.Errorf(
			"CPA api-call returned HTTP %d: %s",
			response.StatusCode,
			truncate(string(responseBody), wxaiProbeDetailLimit),
		)
	}

	var apiCallResponse wxaiManagementAPICallResponse
	if err := json.Unmarshal(responseBody, &apiCallResponse); err != nil {
		return wxaiHTTPResponse{}, fmt.Errorf("decode CPA api-call response: %w", err)
	}
	if apiCallResponse.StatusCode <= 0 {
		return wxaiHTTPResponse{}, fmt.Errorf("CPA api-call response missing status_code")
	}

	upstreamBody := []byte(apiCallResponse.Body)
	bodyTruncated := len(upstreamBody) > wxaiProbeBodyLimit
	if bodyTruncated {
		upstreamBody = upstreamBody[:wxaiProbeBodyLimit]
	}
	upstreamResponse := wxaiHTTPResponse{
		StatusCode:    apiCallResponse.StatusCode,
		Header:        http.Header(apiCallResponse.Header),
		Body:          upstreamBody,
		FinalURL:      endpoint,
		BodyTruncated: bodyTruncated,
	}
	if err := service.captureWxaiHTTPResponse(ctx, http.MethodGet, endpoint, upstreamResponse); err != nil {
		return wxaiHTTPResponse{}, fmt.Errorf("保存 xAI 原始响应: %w", err)
	}
	return upstreamResponse, nil
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
