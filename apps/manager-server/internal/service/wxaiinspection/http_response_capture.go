package wxaiinspection

import (
	"context"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

const wxaiRedactedHeaderValue = "[REDACTED]"

type wxaiInspectionRequestMetadata struct {
	RunID      int64
	AccountKey string
	FileName   string
}

type wxaiInspectionRequestMetadataContextKey struct{}

func withWxaiInspectionRequestMetadata(
	ctx context.Context,
	runID int64,
	currentAccount account,
) context.Context {
	return context.WithValue(ctx, wxaiInspectionRequestMetadataContextKey{}, wxaiInspectionRequestMetadata{
		RunID:      runID,
		AccountKey: currentAccount.Key,
		FileName:   currentAccount.FileName,
	})
}

func (service *Service) captureWxaiHTTPResponse(
	ctx context.Context,
	method string,
	endpoint string,
	response wxaiHTTPResponse,
) error {
	requestStage := resolveWxaiRequestStage(endpoint)
	metadata := ctx.Value(wxaiInspectionRequestMetadataContextKey{}).(wxaiInspectionRequestMetadata)
	_, err := service.store.InsertWxaiInspectionHTTPResponse(context.WithoutCancel(ctx), model.WxaiInspectionHTTPResponse{
		RunID:                   metadata.RunID,
		AccountKey:              metadata.AccountKey,
		FileName:                metadata.FileName,
		RequestStage:            requestStage,
		RequestMethod:           method,
		RequestURL:              endpoint,
		ResponseStatusCode:      response.StatusCode,
		FinalURL:                response.FinalURL,
		ResponseHeaders:         sanitizeWxaiResponseHeaders(response.Header),
		ResponseBody:            response.Body,
		BodyTruncated:           response.BodyTruncated,
		SensitiveFieldsRedacted: true,
	})
	return err
}

func sanitizeWxaiResponseHeaders(headers http.Header) map[string][]string {
	sanitizedHeaders := make(map[string][]string, len(headers))
	for headerName, headerValues := range headers {
		canonicalHeaderName := http.CanonicalHeaderKey(headerName)
		if isSensitiveWxaiResponseHeader(canonicalHeaderName) {
			sanitizedHeaders[canonicalHeaderName] = []string{wxaiRedactedHeaderValue}
			continue
		}
		sanitizedHeaders[canonicalHeaderName] = append([]string(nil), headerValues...)
	}
	return sanitizedHeaders
}

func isSensitiveWxaiResponseHeader(headerName string) bool {
	switch strings.ToLower(strings.TrimSpace(headerName)) {
	case "authorization", "proxy-authorization", "set-cookie", "set-cookie2":
		return true
	default:
		return false
	}
}

func resolveWxaiRequestStage(endpoint string) string {
	switch endpoint {
	case wxaiBillingURL:
		return "billing"
	case wxaiBillingCreditsURL:
		return "billing_credits"
	case wxaiResponsesURL:
		return "responses"
	case wxaiChatCompletionsURL:
		return "chat_completions"
	default:
		panic("unsupported xAI inspection endpoint: " + endpoint)
	}
}
