package worker

import (
	"context"
	"log"
	"strings"

	collectorpkg "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/collector"
	wxaiinspectionservice "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/wxaiinspection"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

type WxaiWindowCostRefreshWorker struct {
	service *wxaiinspectionservice.Service
}

func NewWxaiWindowCostRefreshWorker(service *wxaiinspectionservice.Service) *WxaiWindowCostRefreshWorker {
	return &WxaiWindowCostRefreshWorker{service: service}
}

func (worker *WxaiWindowCostRefreshWorker) HandleUsageEvents(
	ctx context.Context,
	_ collectorpkg.RuntimeConfig,
	events []usage.Event,
) {
	if worker == nil || worker.service == nil || len(events) == 0 {
		return
	}

	authIndexSet := make(map[string]struct{})
	for _, event := range events {
		provider := strings.ToLower(strings.TrimSpace(firstNonEmpty(
			event.Provider,
			event.AuthProviderSnapshot,
		)))
		if provider != "xai" {
			continue
		}
		authIndex := strings.TrimSpace(event.AuthIndex)
		if authIndex != "" {
			authIndexSet[authIndex] = struct{}{}
		}
	}
	if len(authIndexSet) == 0 {
		return
	}

	authIndexes := make([]string, 0, len(authIndexSet))
	for authIndex := range authIndexSet {
		authIndexes = append(authIndexes, authIndex)
	}
	if err := worker.service.RefreshAccountWindowCosts(context.WithoutCancel(ctx), authIndexes); err != nil {
		log.Printf("refresh wxai account window costs: %v", err)
	}
}
