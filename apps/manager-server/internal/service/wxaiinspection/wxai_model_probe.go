package wxaiinspection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const (
	wxaiResponsesURL        = "https://cli-chat-proxy.grok.com/v1/responses"
	wxaiChatCompletionsURL  = "https://cli-chat-proxy.grok.com/v1/chat/completions"
	wxaiProbeModel          = "grok-4.5"
	wxaiProbeInput          = "ping"
	wxaiProbeBodyLimit      = 1024 * 1024
	wxaiProbeDetailLimit    = 400
	wxaiTimeoutRetryBackoff = 400 * time.Millisecond
	wxaiTimeoutRetryCount   = 1
)

type wxaiProbeOutcome struct {
	Alive           bool
	Quota           bool
	StatusCode      int
	ErrorKind       string
	Detail          string
	QuotaRecovery   wxaiQuotaRecovery
	ResponseHeaders map[string][]string
}

type wxaiHTTPResponse struct {
	StatusCode    int
	Header        http.Header
	Body          []byte
	FinalURL      string
	BodyTruncated bool
}

type wxaiResponsesRequest struct {
	Model  string `json:"model"`
	Input  string `json:"input"`
	Stream bool   `json:"stream"`
}

type wxaiChatCompletionRequest struct {
	Model    string                      `json:"model"`
	Messages []wxaiChatCompletionMessage `json:"messages"`
	Stream   bool                        `json:"stream"`
}

type wxaiChatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wxaiProbeError struct {
	Code    string
	Message string
}

func (service *Service) inspectSingleAccount(
	ctx context.Context,
	setup store.Setup,
	settings model.ManagerWxaiInspectionConfig,
	xaiClient *http.Client,
	xaiClientVersion string,
	runID int64,
	currentAccount account,
	logger runLogger,
) (model.WxaiInspectionResult, *int) {
	ctx = withWxaiInspectionRequestMetadata(ctx, runID, currentAccount)
	inspectionTime := time.Now()
	result := model.WxaiInspectionResult{
		RunID:          runID,
		AccountKey:     currentAccount.Key,
		FileName:       currentAccount.FileName,
		DisplayAccount: currentAccount.DisplayAccount,
		AuthIndex:      currentAccount.AuthIndex,
		AccountID:      currentAccount.AccountID,
		Provider:       "xai",
		Disabled:       isWxaiServerAccountDisabled(currentAccount),
		Status:         currentAccount.Status,
		State:          currentAccount.State,
		Action:         "keep",
		ActionReason:   "xAI 对话探测成功",
		ActionStatus:   model.WxaiInspectionActionStatusNone,
		PlanType:       normalizeWxaiAccountType(currentAccount.AccountType),
		CreatedAtMS:    inspectionTime.UnixMilli(),
	}
	if result.Disabled {
		result.ActionReason = "账号已停用，跳过巡检"
		return result, currentAccount.Priority
	}

	authFile, err := cpaauthfiles.New(service.client).DownloadJSON(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
		currentAccount.FileName,
	)
	if err != nil {
		return service.applyWxaiProbeFailure(
			ctx,
			setup,
			currentAccount,
			result,
			wxaiRequestFailure("download auth file: "+err.Error()),
			inspectionTime,
			logger,
		)
	}
	accessToken := strings.TrimSpace(firstString(authFile, "access_token"))
	if accessToken == "" {
		return service.applyWxaiProbeFailure(
			ctx,
			setup,
			currentAccount,
			result,
			wxaiAccountFailure(0, "access-token-missing"),
			inspectionTime,
			logger,
		)
	}

	probeOutcome := service.probeWxaiConversation(ctx, xaiClient, settings.Timeout, accessToken, xaiClientVersion)
	if !probeOutcome.Alive {
		if probeOutcome.Quota && result.PlanType == "" {
			result.PlanType = wxaiAccountTypeFree
			if persistErr := service.persistWxaiAccountType(ctx, currentAccount.Key, result.PlanType); persistErr != nil {
				logger.warning(context.WithoutCancel(ctx), "保存 wXAi 账号类型失败", map[string]any{
					"fileName": currentAccount.FileName,
					"error":    persistErr.Error(),
				})
			}
		}
		return service.applyWxaiProbeFailure(
			ctx,
			setup,
			currentAccount,
			result,
			probeOutcome,
			inspectionTime,
			logger,
		)
	}

	result.StatusCode = intPointer(probeOutcome.StatusCode)
	billingSnapshot, resolvedAccountType, billingErr := service.refreshWxaiBillingMetadata(
		ctx,
		xaiClient,
		settings.Timeout,
		accessToken,
		result.PlanType,
	)
	if resolvedAccountType != "" {
		result.PlanType = resolvedAccountType
		if persistErr := service.persistWxaiAccountType(ctx, currentAccount.Key, resolvedAccountType); persistErr != nil {
			logger.warning(context.WithoutCancel(ctx), "保存 wXAi 账号类型失败", map[string]any{
				"fileName": currentAccount.FileName,
				"error":    persistErr.Error(),
			})
		}
	}
	result.QuotaWindows = billingSnapshot.QuotaWindows
	result.MonthlyLimitCents = billingSnapshot.MonthlyLimitCents
	result.MonthlyUsedCents = billingSnapshot.MonthlyUsedCents
	result.UsedPercent = primaryWxaiUsedPercent(result.QuotaWindows)
	if billingErr != nil {
		result.ActionReason = "xAI 对话探测成功；额度信息刷新失败"
		logger.warning(context.WithoutCancel(ctx), "刷新 wXAi billing 信息失败", map[string]any{
			"fileName": currentAccount.FileName,
			"error":    billingErr.Error(),
		})
	}

	result.ErrorKind = ""
	result.ErrorDetail = ""
	effectivePriority, restoreErr := service.restoreWxaiPriority(ctx, setup, currentAccount, logger)
	if restoreErr != nil {
		applyWxaiPriorityError(&result, "priority_restore_failed", restoreErr)
		result.ActionReason += "；priority 恢复失败"
	}
	return result, effectivePriority
}

func (service *Service) applyWxaiProbeFailure(
	ctx context.Context,
	setup store.Setup,
	currentAccount account,
	result model.WxaiInspectionResult,
	outcome wxaiProbeOutcome,
	inspectionTime time.Time,
	logger runLogger,
) (model.WxaiInspectionResult, *int) {
	if outcome.StatusCode > 0 {
		result.StatusCode = intPointer(outcome.StatusCode)
	}
	result.ErrorKind = outcome.ErrorKind
	result.ErrorDetail = truncate(outcome.Detail, maxStoredBodyText)
	if outcome.Quota {
		result.IsQuota = true
		result.ErrorKind = "quota_exhausted"
		result.ActionReason = "xAI FREE 额度耗尽，priority 已降为 -1"
		resolvedRecovery := resolveWxaiQuotaRecovery(outcome.QuotaRecovery, inspectionTime)
		logger.info(context.WithoutCancel(ctx), "wXAi 额度耗尽响应已记录", map[string]any{
			"fileName":            currentAccount.FileName,
			"displayAccount":      currentAccount.DisplayAccount,
			"statusCode":          outcome.StatusCode,
			"responseHeaders":     outcome.ResponseHeaders,
			"recoverySource":      resolvedRecovery.source,
			"upstreamRecoverAtMs": resolvedRecovery.upstreamRecoverAtMS,
			"recoverAtMs":         resolvedRecovery.recoverAtMS,
		})
		effectivePriority, priorityErr := service.lowerWxaiPriority(
			ctx,
			setup,
			currentAccount,
			wxaiQuotaPriorityValue,
			resolvedRecovery.recoverAtMS,
			logger,
		)
		if priorityErr != nil {
			applyWxaiPriorityError(&result, "priority_adjustment_failed", priorityErr)
			result.ActionReason = "xAI FREE 额度耗尽；priority 调整失败"
		}
		return result, effectivePriority
	}

	adjustedPriority := wxaiAbnormalPriorityValue
	if outcome.StatusCode == http.StatusUnauthorized {
		adjustedPriority = wxaiUnauthorizedPriorityValue
	}
	if outcome.ErrorKind == "request_error" {
		result.Error = outcome.Detail
	} else {
		result.ErrorKind = "account_abnormal"
	}
	if outcome.StatusCode > 0 {
		result.ActionReason = fmt.Sprintf("xAI 请求返回 HTTP %d，priority 已降为 %d", outcome.StatusCode, adjustedPriority)
	} else {
		result.ActionReason = fmt.Sprintf("xAI 请求失败，priority 已降为 %d", adjustedPriority)
	}
	effectivePriority, priorityErr := service.lowerWxaiPriority(
		ctx,
		setup,
		currentAccount,
		adjustedPriority,
		inspectionTime.Add(wxaiPriorityRecheckInterval).UnixMilli(),
		logger,
	)
	if priorityErr != nil {
		applyWxaiPriorityError(&result, "priority_adjustment_failed", priorityErr)
		result.ActionReason += "；priority 调整失败"
	}
	logger.warning(ctx, "wXAi 巡检请求异常", map[string]any{
		"fileName":   currentAccount.FileName,
		"statusCode": outcome.StatusCode,
		"errorKind":  result.ErrorKind,
	})
	return result, effectivePriority
}

func (service *Service) probeWxaiConversation(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	accessToken string,
	clientVersion string,
) wxaiProbeOutcome {
	responsesBody, err := json.Marshal(wxaiResponsesRequest{
		Model:  wxaiProbeModel,
		Input:  wxaiProbeInput,
		Stream: false,
	})
	if err != nil {
		return wxaiRequestFailure("encode responses request: " + err.Error())
	}

	primaryResponse, primaryErr := service.performWxaiRequest(
		ctx,
		client,
		timeoutMilliseconds,
		http.MethodPost,
		wxaiResponsesURL,
		responsesBody,
		wxaiInspectionHeaders(accessToken, clientVersion),
	)
	if primaryErr == nil {
		primaryOutcome, primaryDefinitive := classifyWxaiProbeResponse(primaryResponse)
		if primaryDefinitive {
			return primaryOutcome
		}
	}

	chatBody, err := json.Marshal(wxaiChatCompletionRequest{
		Model: wxaiProbeModel,
		Messages: []wxaiChatCompletionMessage{
			{Role: "user", Content: wxaiProbeInput},
		},
		Stream: false,
	})
	if err != nil {
		return wxaiRequestFailure("encode chat completions request: " + err.Error())
	}
	fallbackResponse, fallbackErr := service.performWxaiRequest(
		ctx,
		client,
		timeoutMilliseconds,
		http.MethodPost,
		wxaiChatCompletionsURL,
		chatBody,
		wxaiInspectionHeaders(accessToken, clientVersion),
	)
	if fallbackErr != nil {
		return wxaiRequestFailure(joinWxaiProbeErrors(primaryErr, fallbackErr))
	}
	fallbackOutcome, fallbackDefinitive := classifyWxaiProbeResponse(fallbackResponse)
	if fallbackDefinitive {
		return fallbackOutcome
	}
	return wxaiAccountFailure(
		fallbackResponse.StatusCode,
		truncate(strings.TrimSpace(string(fallbackResponse.Body)), wxaiProbeDetailLimit),
	)
}

func classifyWxaiProbeResponse(response wxaiHTTPResponse) (wxaiProbeOutcome, bool) {
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return wxaiProbeOutcome{Alive: true, StatusCode: response.StatusCode}, true
	}
	probeError := extractWxaiProbeError(response.Body)
	responseDetail := firstNonEmpty(probeError.Message, strings.TrimSpace(string(response.Body)))
	responseDetail = truncate(responseDetail, wxaiProbeDetailLimit)
	if response.StatusCode == http.StatusUnauthorized {
		return wxaiAccountFailure(response.StatusCode, responseDetail), true
	}
	if isWxaiFreeUsageExhausted(probeError.Code, probeError.Message) {
		quotaRecovery := extractWxaiQuotaRecovery(response)
		return wxaiProbeOutcome{
			Quota:           true,
			StatusCode:      response.StatusCode,
			ErrorKind:       "quota_exhausted",
			Detail:          responseDetail,
			QuotaRecovery:   quotaRecovery,
			ResponseHeaders: sanitizeWxaiResponseHeaders(response.Header),
		}, true
	}
	switch response.StatusCode {
	case http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests:
		return wxaiAccountFailure(response.StatusCode, responseDetail), true
	case http.StatusNotFound:
		return wxaiProbeOutcome{StatusCode: response.StatusCode, ErrorKind: "model_unavailable", Detail: responseDetail}, false
	default:
		if response.StatusCode >= http.StatusInternalServerError {
			return wxaiProbeOutcome{StatusCode: response.StatusCode, ErrorKind: "upstream_error", Detail: responseDetail}, false
		}
		return wxaiAccountFailure(response.StatusCode, responseDetail), true
	}
}

func extractWxaiProbeError(body []byte) wxaiProbeError {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return wxaiProbeError{Message: truncate(strings.TrimSpace(string(body)), wxaiProbeDetailLimit)}
	}
	probeError := wxaiProbeError{Code: wxaiStringValue(payload["code"])}
	switch errorValue := payload["error"].(type) {
	case map[string]any:
		if probeError.Code == "" {
			probeError.Code = wxaiStringValue(errorValue["code"])
		}
		probeError.Message = firstNonEmpty(
			wxaiStringValue(errorValue["message"]),
			wxaiStringValue(errorValue["error"]),
		)
	case string:
		probeError.Message = errorValue
	}
	if probeError.Message == "" {
		probeError.Message = wxaiStringValue(payload["message"])
	}
	probeError.Message = truncate(strings.TrimSpace(probeError.Message), wxaiProbeDetailLimit)
	return probeError
}

func isWxaiFreeUsageExhausted(code string, message string) bool {
	combined := strings.ToLower(strings.TrimSpace(code) + " " + strings.TrimSpace(message))
	return strings.Contains(combined, "free-usage-exhausted") ||
		strings.Contains(combined, "used all the included free usage") ||
		strings.Contains(combined, "included free usage has been exhausted")
}

func wxaiInspectionHeaders(accessToken string, clientVersion string) map[string]string {
	return map[string]string{
		"Authorization":         "Bearer " + accessToken,
		"Accept":                "application/json",
		"Content-Type":          "application/json",
		"X-XAI-Token-Auth":      "xai-grok-cli",
		"x-grok-client-version": clientVersion,
		"User-Agent":            "xai-grok-workspace/" + clientVersion,
	}
}

func (service *Service) performWxaiRequest(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	method string,
	endpoint string,
	body []byte,
	headers map[string]string,
) (wxaiHTTPResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= wxaiTimeoutRetryCount; attempt++ {
		if attempt > 0 {
			if err := waitForWxaiRetry(ctx, wxaiTimeoutRetryBackoff); err != nil {
				return wxaiHTTPResponse{}, err
			}
		}
		response, err := service.performWxaiRequestOnce(
			ctx,
			client,
			timeoutMilliseconds,
			method,
			endpoint,
			body,
			headers,
		)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !isWxaiTimeoutError(err) || ctx.Err() != nil {
			return wxaiHTTPResponse{}, err
		}
	}
	return wxaiHTTPResponse{}, lastErr
}

func (service *Service) performWxaiRequestOnce(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	method string,
	endpoint string,
	body []byte,
	headers map[string]string,
) (wxaiHTTPResponse, error) {
	requestContext := ctx
	cancel := func() {}
	if timeoutMilliseconds > 0 {
		requestContext, cancel = context.WithTimeout(ctx, time.Duration(timeoutMilliseconds)*time.Millisecond)
	}
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return wxaiHTTPResponse{}, err
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return wxaiHTTPResponse{}, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, wxaiProbeBodyLimit+1))
	if err != nil {
		return wxaiHTTPResponse{}, err
	}
	bodyTruncated := len(responseBody) > wxaiProbeBodyLimit
	if bodyTruncated {
		responseBody = responseBody[:wxaiProbeBodyLimit]
	}
	wxaiResponse := wxaiHTTPResponse{
		StatusCode:    response.StatusCode,
		Header:        response.Header.Clone(),
		Body:          responseBody,
		FinalURL:      response.Request.URL.String(),
		BodyTruncated: bodyTruncated,
	}
	if err := service.captureWxaiHTTPResponse(ctx, method, endpoint, wxaiResponse); err != nil {
		return wxaiHTTPResponse{}, fmt.Errorf("保存 xAI 原始响应: %w", err)
	}
	return wxaiResponse, nil
}

func waitForWxaiRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isWxaiTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

func joinWxaiProbeErrors(primaryErr error, fallbackErr error) string {
	parts := make([]string, 0, 2)
	if primaryErr != nil {
		parts = append(parts, "responses request: "+primaryErr.Error())
	}
	if fallbackErr != nil {
		parts = append(parts, "chat completions request: "+fallbackErr.Error())
	}
	return strings.Join(parts, "; ")
}

func wxaiStringValue(value any) string {
	switch typedValue := value.(type) {
	case string:
		return strings.TrimSpace(typedValue)
	case json.Number:
		return typedValue.String()
	case float64:
		return fmt.Sprintf("%g", typedValue)
	default:
		return ""
	}
}

func primaryWxaiUsedPercent(windows []model.WxaiInspectionQuotaWindow) *float64 {
	for _, window := range windows {
		if window.ID == model.WxaiAccountWindowTypeWeekly && window.UsedPercent != nil {
			return window.UsedPercent
		}
	}
	for _, window := range windows {
		if window.UsedPercent != nil {
			return window.UsedPercent
		}
	}
	return nil
}

func wxaiRequestFailure(detail string) wxaiProbeOutcome {
	return wxaiProbeOutcome{ErrorKind: "request_error", Detail: detail}
}

func wxaiAccountFailure(statusCode int, detail string) wxaiProbeOutcome {
	return wxaiProbeOutcome{StatusCode: statusCode, ErrorKind: "account_abnormal", Detail: detail}
}
