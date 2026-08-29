package worker

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	collectorpkg "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/collector"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialpolicy"
	wxaiinspectionservice "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/wxaiinspection"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

type WxaiConditionalInspectionWorker struct {
	service *wxaiinspectionservice.Service

	quotaApplyLock sync.Mutex
}

func NewWxaiConditionalInspectionWorker(service *wxaiinspectionservice.Service) *WxaiConditionalInspectionWorker {
	return &WxaiConditionalInspectionWorker{service: service}
}

// HandleUsageEvents treats every xAI HTTP 429 as quota exhaustion and persists
// the cooldown directly from the request event.
func (worker *WxaiConditionalInspectionWorker) HandleUsageEvents(
	ctx context.Context,
	_ collectorpkg.RuntimeConfig,
	events []usage.Event,
) {
	if worker == nil || worker.service == nil || len(events) == 0 {
		return
	}
	quotaRequests := make([]wxaiinspectionservice.UsageQuotaExhaustedRequest, 0)
	for _, event := range events {
		if !isWxaiRateLimitEvent(event) {
			continue
		}
		quotaRequests = append(quotaRequests, wxaiUsageQuotaRequestFromEvent(event))
	}
	if len(quotaRequests) == 0 {
		return
	}
	go worker.applyUsageQuotaEvents(ctx, quotaRequests)
}

func (worker *WxaiConditionalInspectionWorker) applyUsageQuotaEvents(
	ctx context.Context,
	requests []wxaiinspectionservice.UsageQuotaExhaustedRequest,
) {
	worker.quotaApplyLock.Lock()
	defer worker.quotaApplyLock.Unlock()

	for _, request := range requests {
		result, err := worker.applyUsageQuotaEvent(ctx, request)
		if err != nil {
			log.Printf(
				"apply xAI quota exhaustion directly from request event file=%q authIndex=%q event=%q: %v",
				request.FileName,
				request.AuthIndex,
				request.EventHash,
				err,
			)
			continue
		}
		if result.SkippedReason != "" {
			log.Printf(
				"skipped xAI quota request event file=%q accountType=%q reason=%q",
				result.FileName,
				result.AccountType,
				result.SkippedReason,
			)
			continue
		}
		if result.Applied {
			log.Printf(
				"applied xAI quota exhaustion directly from request event file=%q accountType=%q runID=%d recoverAtMs=%d recoverySource=%q creditsAttempted=%t creditsRecoverAtMs=%d creditsError=%q",
				result.FileName,
				result.AccountType,
				result.RunID,
				result.RecoverAtMS,
				result.RecoverySource,
				result.CreditsAttempted,
				result.CreditsRecoverAtMS,
				result.CreditsError,
			)
		}
	}
}

func (worker *WxaiConditionalInspectionWorker) applyUsageQuotaEvent(
	ctx context.Context,
	request wxaiinspectionservice.UsageQuotaExhaustedRequest,
) (wxaiinspectionservice.UsageQuotaExhaustedResult, error) {
	for {
		result, err := worker.service.ApplyUsageQuotaExhausted(ctx, request)
		if !errors.Is(err, wxaiinspectionservice.ErrRunAlreadyActive) {
			return result, err
		}
		select {
		case <-ctx.Done():
			return wxaiinspectionservice.UsageQuotaExhaustedResult{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func wxaiUsageQuotaRequestFromEvent(event usage.Event) wxaiinspectionservice.UsageQuotaExhaustedRequest {
	return wxaiinspectionservice.UsageQuotaExhaustedRequest{
		FileName:               strings.TrimSpace(event.AuthFileSnapshot),
		DisplayAccount:         firstNonEmpty(event.AccountSnapshot, event.AuthLabelSnapshot, event.Source),
		AuthIndex:              strings.TrimSpace(event.AuthIndex),
		Provider:               firstNonEmpty(event.Provider, event.AuthProviderSnapshot),
		StatusCode:             event.FailStatusCode,
		Detail:                 event.FailSummary,
		ResponseBody:           firstNonEmpty(event.FailBody, event.FailSummary),
		HeaderQuotaRecoverAtMS: event.HeaderQuotaRecoverAtMS,
		EventHash:              event.EventHash,
	}
}

func isWxaiRateLimitEvent(event usage.Event) bool {
	if !event.Failed || event.FailStatusCode != http.StatusTooManyRequests {
		return false
	}
	provider := credentialpolicy.NormalizeProvider(firstNonEmpty(
		event.Provider,
		event.AuthProviderSnapshot,
	))
	return provider == "xai"
}
