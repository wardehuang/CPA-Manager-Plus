package wxaiinspection

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/toolcallcheck"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

var ErrWxaiToolCallCheckAccountNotFound = errors.New("wxai tool call check account not found")

type ToolCallCheckRequest struct {
	AccountKey string `json:"accountKey,omitempty"`
	FileName   string `json:"fileName,omitempty"`
	AuthIndex  string `json:"authIndex,omitempty"`
	Model      string `json:"model,omitempty"`
}

type ToolCallCheckConfigResponse struct {
	DefaultModel string                               `json:"defaultModel"`
	Policy       toolcallcheck.StreamingQualityPolicy `json:"policy"`
}

type xaiSwitcherToolCallSettingsResponse struct {
	Data struct {
		QualityProbeModel                            string  `json:"qualityProbeModel"`
		QualitySoftTPS                               float64 `json:"qualitySoftTPS"`
		QualityHardTPS                               float64 `json:"qualityHardTPS"`
		RealtimeGuardTTFBSeconds                     float64 `json:"realtimeGuardTTFBSeconds"`
		RealtimeGuardGenerationSeconds               float64 `json:"realtimeGuardGenerationSeconds"`
		RealtimeGuardTokenThreshold                  int     `json:"realtimeGuardTokenThreshold"`
		RealtimeGuardMinSummaryChars                 int     `json:"realtimeGuardMinSummaryChars"`
		RealtimeGuardMinEncryptedBytes               int     `json:"realtimeGuardMinEncryptedBytes"`
		RealtimeGuardEncryptedBytesPerReasoningToken int     `json:"realtimeGuardEncryptedBytesPerReasoningToken"`
		RealtimeGuardMinOutputTokens                 int     `json:"realtimeGuardMinOutputTokens"`
		RealtimeGuardBurstMinReasoningTokens         int     `json:"realtimeGuardBurstMinReasoningTokens"`
		RealtimeGuardBurstMaxVisibleTokens           int     `json:"realtimeGuardBurstMaxVisibleTokens"`
		RealtimeGuardBurstMaxWindowMS                int     `json:"realtimeGuardBurstMaxWindowMs"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type ToolCallCheckResponse struct {
	AccountKey     string               `json:"accountKey"`
	FileName       string               `json:"fileName"`
	DisplayAccount string               `json:"displayAccount"`
	AuthIndex      string               `json:"authIndex,omitempty"`
	Result         toolcallcheck.Result `json:"result"`
}

func (service *Service) GetToolCallCheckConfig(ctx context.Context) (ToolCallCheckConfigResponse, error) {
	_, setup, err := service.resolveRuntime(ctx)
	if err != nil {
		return ToolCallCheckConfigResponse{}, err
	}
	return service.loadToolCallCheckConfig(ctx, setup)
}

func (service *Service) loadToolCallCheckConfig(ctx context.Context, setup store.Setup) (ToolCallCheckConfigResponse, error) {
	var response xaiSwitcherToolCallSettingsResponse
	if err := service.doXaiSwitcherManagementRequest(ctx, setup, http.MethodGet, "/settings", nil, &response); err != nil {
		return ToolCallCheckConfigResponse{}, err
	}
	if response.Error != nil {
		return ToolCallCheckConfigResponse{}, fmt.Errorf("%s: %s", response.Error.Code, response.Error.Message)
	}
	defaultModel := strings.TrimSpace(response.Data.QualityProbeModel)
	if defaultModel == "" {
		return ToolCallCheckConfigResponse{}, errors.New("xAI IP Switcher qualityProbeModel 为空")
	}
	return ToolCallCheckConfigResponse{
		DefaultModel: defaultModel,
		Policy: toolcallcheck.StreamingQualityPolicy{
			SoftTokensPerSecond:             response.Data.QualitySoftTPS,
			HardTokensPerSecond:             response.Data.QualityHardTPS,
			TTFBSeconds:                     response.Data.RealtimeGuardTTFBSeconds,
			GenerationSeconds:               response.Data.RealtimeGuardGenerationSeconds,
			TokenThreshold:                  response.Data.RealtimeGuardTokenThreshold,
			MinSummaryChars:                 response.Data.RealtimeGuardMinSummaryChars,
			MinEncryptedBytes:               response.Data.RealtimeGuardMinEncryptedBytes,
			EncryptedBytesPerReasoningToken: response.Data.RealtimeGuardEncryptedBytesPerReasoningToken,
			MinOutputTokens:                 response.Data.RealtimeGuardMinOutputTokens,
			BurstMinReasoningTokens:         response.Data.RealtimeGuardBurstMinReasoningTokens,
			BurstMaxVisibleTokens:           response.Data.RealtimeGuardBurstMaxVisibleTokens,
			BurstMaxWindowMS:                response.Data.RealtimeGuardBurstMaxWindowMS,
		},
	}, nil
}

func (service *Service) RunToolCallCheck(ctx context.Context, request ToolCallCheckRequest) (ToolCallCheckResponse, error) {
	operationStartedAt := time.Now()
	executionID, err := toolcallcheck.NewExecutionID()
	if err != nil {
		logWxaiToolCallCheck("", operationStartedAt, "generate_check_id_failed", map[string]any{
			"error": err.Error(),
		})
		return ToolCallCheckResponse{}, err
	}
	logWxaiToolCallCheck(executionID, operationStartedAt, "started", map[string]any{
		"accountKey": request.AccountKey,
		"fileName":   request.FileName,
		"authIndex":  request.AuthIndex,
	})

	logWxaiToolCallCheck(executionID, operationStartedAt, "resolve_runtime_started", nil)
	settings, setup, err := service.resolveRuntime(ctx)
	if err != nil {
		logWxaiToolCallCheck(executionID, operationStartedAt, "resolve_runtime_failed", map[string]any{
			"error": err.Error(),
		})
		return ToolCallCheckResponse{}, err
	}
	logWxaiToolCallCheck(executionID, operationStartedAt, "resolve_runtime_completed", map[string]any{
		"timeoutMs": settings.Timeout,
	})
	toolCallConfig, err := service.loadToolCallCheckConfig(ctx, setup)
	if err != nil {
		return ToolCallCheckResponse{}, fmt.Errorf("读取 xAI IP Switcher 实时守护配置: %w", err)
	}
	selectedModel := strings.TrimSpace(request.Model)
	if selectedModel == "" {
		selectedModel = toolCallConfig.DefaultModel
	}

	logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_accounts_started", nil)
	accounts, err := service.fetchAccounts(ctx, setup)
	if err != nil {
		logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_accounts_failed", map[string]any{
			"error": err.Error(),
		})
		return ToolCallCheckResponse{}, err
	}
	logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_accounts_completed", map[string]any{
		"accountCount": len(accounts),
	})

	selectedAccount, matched := matchAccount(accounts, ManualRefreshRequest{
		AccountKey: request.AccountKey,
		FileName:   request.FileName,
		AuthIndex:  request.AuthIndex,
	})
	if !matched {
		err = fmt.Errorf(
			"%w: %s",
			ErrWxaiToolCallCheckAccountNotFound,
			firstNonEmpty(request.AccountKey, request.FileName, request.AuthIndex),
		)
		logWxaiToolCallCheck(executionID, operationStartedAt, "match_account_failed", map[string]any{
			"error": err.Error(),
		})
		return ToolCallCheckResponse{}, err
	}
	logWxaiToolCallCheck(executionID, operationStartedAt, "account_matched", map[string]any{
		"accountKey": selectedAccount.Key,
		"fileName":   selectedAccount.FileName,
		"authIndex":  selectedAccount.AuthIndex,
	})

	response := ToolCallCheckResponse{
		AccountKey:     selectedAccount.Key,
		FileName:       selectedAccount.FileName,
		DisplayAccount: selectedAccount.DisplayAccount,
		AuthIndex:      selectedAccount.AuthIndex,
	}
	logWxaiToolCallCheck(executionID, operationStartedAt, "download_auth_file_started", map[string]any{
		"fileName": selectedAccount.FileName,
	})
	authFile, err := cpaauthfiles.New(service.client).DownloadJSON(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
		selectedAccount.FileName,
	)
	if err != nil {
		logWxaiToolCallCheck(executionID, operationStartedAt, "download_auth_file_failed", map[string]any{
			"fileName": selectedAccount.FileName,
			"error":    err.Error(),
		})
		return response, err
	}
	accessToken := strings.TrimSpace(firstString(authFile, "access_token"))
	authProxyURL := firstString(authFile, "proxy_url", "proxyUrl", "proxy-url")
	logWxaiToolCallCheck(executionID, operationStartedAt, "download_auth_file_completed", map[string]any{
		"fileName":            selectedAccount.FileName,
		"accessTokenPresent":  accessToken != "",
		"authProxyConfigured": strings.TrimSpace(authProxyURL) != "",
	})
	if accessToken == "" {
		err = errors.New("xAI auth file access_token is missing")
		logWxaiToolCallCheck(executionID, operationStartedAt, "read_access_token_failed", map[string]any{
			"error": err.Error(),
		})
		return response, err
	}

	globalProxyURL := ""
	if strings.TrimSpace(authProxyURL) == "" {
		logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_global_proxy_started", nil)
		managementConfig, configErr := cpa.FetchManagementConfig(
			ctx,
			setup.CPAUpstreamURL,
			setup.ManagementKey,
		)
		if configErr != nil {
			logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_global_proxy_failed", map[string]any{
				"error": configErr.Error(),
			})
			return response, fmt.Errorf("读取 CPA 全局 proxy-url: %w", configErr)
		}
		globalProxyURL = managementConfig.ProxyURL
		logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_global_proxy_completed", map[string]any{
			"globalProxyConfigured": strings.TrimSpace(globalProxyURL) != "",
			"globalProxyURL":        toolcallcheck.RedactProxyURL(globalProxyURL),
		})
	} else {
		logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_global_proxy_skipped", map[string]any{
			"reason": "auth_proxy_configured",
		})
	}
	proxySelection := toolcallcheck.ResolveProxy(authProxyURL, globalProxyURL)
	logWxaiToolCallCheck(executionID, operationStartedAt, "proxy_resolved", map[string]any{
		"proxySource": proxySelection.Source,
		"proxyURL":    toolcallcheck.RedactProxyURL(proxySelection.URL),
	})

	logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_client_version_started", nil)
	clientVersion, err := cpa.FetchXAIClientVersion(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
	)
	if err != nil {
		logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_client_version_failed", map[string]any{
			"error": err.Error(),
		})
		return response, fmt.Errorf("读取 CPA xAI client version: %w", err)
	}
	logWxaiToolCallCheck(executionID, operationStartedAt, "fetch_client_version_completed", map[string]any{
		"clientVersion": clientVersion,
	})

	logWxaiToolCallCheck(executionID, operationStartedAt, "upstream_request_started", map[string]any{
		"endpoint":         wxaiResponsesURL,
		"model":            selectedModel,
		"prompt":           wxaiToolCallCheckPrompt,
		"expectedAnswer":   toolcallcheck.ExpectedAnswer,
		"stream":           true,
		"reasoningEffort":  "high",
		"reasoningSummary": "detailed",
		"temperature":      0,
		"maxOutputTokens":  wxaiQualityProbeMaxTokens,
		"tools":            "omitted",
		"timeoutMs":        settings.Timeout,
		"proxySource":      proxySelection.Source,
		"proxyURL":         toolcallcheck.RedactProxyURL(proxySelection.URL),
	})
	checkResult, err := toolcallcheck.Run(ctx, toolcallcheck.Request{
		CheckID:       executionID,
		Model:         selectedModel,
		Endpoint:      wxaiResponsesURL,
		AccessToken:   accessToken,
		Headers:       wxaiToolCallCheckHeaders(accessToken, clientVersion),
		Body:          buildWxaiResponsesStreamingProbePayload(selectedModel),
		Proxy:         proxySelection,
		Timeout:       time.Duration(settings.Timeout) * time.Millisecond,
		Stream:        true,
		QualityPolicy: toolCallConfig.Policy,
	})
	if err != nil {
		checkResult.Error = err.Error()
	}
	var outputTokensPerSecond any
	if checkResult.OutputTokensPerSecond != nil {
		outputTokensPerSecond = *checkResult.OutputTokensPerSecond
	}
	logWxaiToolCallCheck(executionID, operationStartedAt, "upstream_request_completed", map[string]any{
		"statusCode":                 checkResult.StatusCode,
		"ttfbMs":                     checkResult.TTFBMS,
		"firstTokenMs":               checkResult.FirstTokenMS,
		"generationMs":               checkResult.GenerationMS,
		"totalMs":                    checkResult.TotalMS,
		"outputTokensPerSecond":      outputTokensPerSecond,
		"outputTokens":               checkResult.OutputTokens,
		"reasoningTokens":            checkResult.ReasoningTokens,
		"visibleTokens":              checkResult.VisibleTokens,
		"thinkingDelta":              checkResult.ThinkingDelta,
		"summaryChars":               checkResult.SummaryChars,
		"encryptedBytes":             checkResult.EncryptedBytes,
		"encryptedFloor":             checkResult.EncryptedFloor,
		"toolCallDetected":           checkResult.ToolCallDetected,
		"toolCallNames":              checkResult.ToolCallNames,
		"completedFunctionCallCount": checkResult.CompletedFunctionCallCount,
		"toolCallOnly":               checkResult.ToolCallOnly,
		"completedMutationEvidence":  checkResult.CompletedMutationEvidence,
		"outputTextChars":            checkResult.OutputTextChars,
		"completedMessageCount":      checkResult.CompletedMessageCount,
		"refusalDetected":            checkResult.RefusalDetected,
		"expectedAnswer":             checkResult.ExpectedAnswer,
		"answerMatched":              checkResult.AnswerMatched,
		"classification":             checkResult.Classification,
		"qualityLevel":               checkResult.QualityLevel,
		"classificationReason":       checkResult.ClassificationReason,
		"errorCode":                  checkResult.ErrorCode,
		"proxyMode":                  checkResult.ProxyMode,
		"error":                      checkResult.Error,
		"operationContextError":      contextError(ctx),
	})
	response.Result = checkResult
	return response, nil
}

func logWxaiToolCallCheck(checkID string, startedAt time.Time, stage string, detail map[string]any) {
	if detail == nil {
		detail = make(map[string]any)
	}
	detail["checkId"] = checkID
	detail["elapsedMs"] = time.Since(startedAt).Milliseconds()
	log.Printf("wXAi 降智检测操作日志 stage=%s detail=%v", stage, detail)
}

func contextError(ctx context.Context) string {
	if err := ctx.Err(); err != nil {
		return err.Error()
	}
	return ""
}

func wxaiToolCallCheckHeaders(accessToken string, clientVersion string) map[string]string {
	headers := wxaiInspectionHeaders(accessToken, clientVersion)
	headers["Accept"] = "text/event-stream"
	return headers
}

const wxaiToolCallCheckPrompt = "用中文回答：17 × 23 等于多少？只输出计算过程和答案。"

func buildWxaiResponsesStreamingProbePayload(model string) map[string]any {
	return map[string]any{
		"model":             strings.TrimSpace(model),
		"input":             wxaiToolCallCheckPrompt,
		"stream":            true,
		"reasoning":         map[string]string{"effort": "high", "summary": "detailed"},
		"max_output_tokens": wxaiQualityProbeMaxTokens,
		"temperature":       0,
	}
}
