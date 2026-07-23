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
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	wxaiinspectionservice "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/wxaiinspection"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

type WxaiConditionalInspectionWorker struct {
	store   *store.Store
	service *wxaiinspectionservice.Service

	mutex          sync.Mutex
	running        bool
	quotaApplyLock sync.Mutex
}

func NewWxaiConditionalInspectionWorker(store *store.Store, service *wxaiinspectionservice.Service) *WxaiConditionalInspectionWorker {
	return &WxaiConditionalInspectionWorker{store: store, service: service}
}

func (worker *WxaiConditionalInspectionWorker) Start(ctx context.Context) {
	if worker == nil || worker.store == nil || worker.service == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *WxaiConditionalInspectionWorker) run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	worker.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.tick(ctx)
		}
	}
}

func (worker *WxaiConditionalInspectionWorker) tick(ctx context.Context) {
	worker.trigger(ctx)
}

// HandleUsageEvents directly persists proven xAI quota exhaustion. Generic xAI
// HTTP 429 events still trigger the normal recent-activity conditional run.
func (worker *WxaiConditionalInspectionWorker) HandleUsageEvents(
	ctx context.Context,
	_ collectorpkg.RuntimeConfig,
	events []usage.Event,
) {
	if worker == nil || worker.service == nil || len(events) == 0 {
		return
	}
	quotaRequests := make([]wxaiinspectionservice.UsageQuotaExhaustedRequest, 0)
	genericRateLimitFound := false
	for _, event := range events {
		if !isWxaiRateLimitEvent(event) {
			continue
		}
		if _, quotaExhausted := xaiFreeUsageResetTimeFromEvent(event, time.Now()); quotaExhausted {
			quotaRequests = append(quotaRequests, wxaiUsageQuotaRequestFromEvent(event))
			continue
		}
		genericRateLimitFound = true
	}
	if len(quotaRequests) > 0 {
		go worker.applyUsageQuotaEvents(ctx, quotaRequests, genericRateLimitFound)
		return
	}
	if genericRateLimitFound {
		log.Printf("trigger conditional wxai inspection after generic xAI HTTP 429")
		worker.trigger(ctx)
	}
}

func (worker *WxaiConditionalInspectionWorker) applyUsageQuotaEvents(
	ctx context.Context,
	requests []wxaiinspectionservice.UsageQuotaExhaustedRequest,
	triggerGenericInspection bool,
) {
	worker.quotaApplyLock.Lock()
	defer worker.quotaApplyLock.Unlock()

	for _, request := range requests {
		result, err := worker.applyUsageQuotaEvent(ctx, request)
		if err != nil {
			log.Printf(
				"apply xAI free usage exhaustion directly from request event file=%q authIndex=%q event=%q: %v",
				request.FileName,
				request.AuthIndex,
				request.EventHash,
				err,
			)
			continue
		}
		if result.Applied {
			log.Printf(
				"applied xAI free usage exhaustion directly from request event file=%q runID=%d recoverAtMs=%d",
				result.FileName,
				result.RunID,
				result.RecoverAtMS,
			)
		}
	}
	if triggerGenericInspection {
		log.Printf("trigger conditional wxai inspection after generic xAI HTTP 429")
		worker.trigger(ctx)
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

func (worker *WxaiConditionalInspectionWorker) trigger(ctx context.Context) {
	if worker == nil || worker.store == nil || worker.service == nil {
		return
	}
	config, configured, err := worker.service.ResolveConfig(ctx)
	if err != nil {
		log.Printf("resolve wxai conditional inspection config: %v", err)
		return
	}
	if !configured || config.Enabled == nil || !*config.Enabled || worker.service.IsRunning() || !worker.acquireRun() {
		return
	}
	runs, err := worker.store.ListWxaiInspectionRuns(ctx, 1)
	if err != nil {
		worker.releaseRun()
		log.Printf("load latest wxai inspection run for conditional inspection: %v", err)
		return
	}
	if len(runs) == 0 || runs[0].ID <= 0 || runs[0].Status == model.WxaiInspectionStatusRunning {
		worker.releaseRun()
		return
	}
	runID := runs[0].ID
	go func() {
		defer worker.releaseRun()
		if _, runErr := worker.service.RunConditional(ctx, wxaiinspectionservice.ConditionalRunRequest{RunID: runID}); runErr != nil && !errors.Is(runErr, wxaiinspectionservice.ErrRunAlreadyActive) {
			log.Printf("run conditional wxai inspection: %v", runErr)
		}
	}()
}

func isWxaiRateLimitEvent(event usage.Event) bool {
	if !event.Failed || event.FailStatusCode != http.StatusTooManyRequests {
		return false
	}
	provider := normalizeQuotaProvider(firstNonEmpty(
		event.Provider,
		event.AuthProviderSnapshot,
	))
	return provider == "xai"
}

func (worker *WxaiConditionalInspectionWorker) acquireRun() bool {
	worker.mutex.Lock()
	defer worker.mutex.Unlock()
	if worker.running {
		return false
	}
	worker.running = true
	return true
}

func (worker *WxaiConditionalInspectionWorker) releaseRun() {
	worker.mutex.Lock()
	defer worker.mutex.Unlock()
	worker.running = false
}
