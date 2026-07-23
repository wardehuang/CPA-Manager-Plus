package worker

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	wxaiinspectionservice "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/wxaiinspection"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type WxaiInspectionWorker struct {
	store   *store.Store
	service *wxaiinspectionservice.Service
}

func NewWxaiInspectionWorker(store *store.Store, service *wxaiinspectionservice.Service) *WxaiInspectionWorker {
	return &WxaiInspectionWorker{store: store, service: service}
}

func (worker *WxaiInspectionWorker) Start(ctx context.Context) {
	if worker == nil || worker.store == nil || worker.service == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *WxaiInspectionWorker) run(ctx context.Context) {
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

func (worker *WxaiInspectionWorker) tick(ctx context.Context) {
	config, configured, err := worker.service.ResolveConfig(ctx)
	if err != nil {
		log.Printf("resolve wxai inspection config: %v", err)
		return
	}
	if !configured || config.Enabled == nil || !*config.Enabled || worker.service.IsRunning() {
		return
	}
	now := time.Now()
	triggerKey, due := resolveWxaiScheduledTrigger(now, worker.lastScheduledRunTime(ctx), config)
	if !due {
		return
	}
	if _, exists, err := worker.store.GetLatestWxaiInspectionRunByTrigger(ctx, model.WxaiInspectionTriggerScheduled, triggerKey); err != nil {
		log.Printf("load wxai inspection trigger: %v", err)
		return
	} else if exists {
		return
	}
	go func() {
		if _, err := worker.service.Run(ctx, wxaiinspectionservice.RunRequest{
			TriggerType: model.WxaiInspectionTriggerScheduled,
			TriggerKey:  triggerKey,
		}); err != nil && !errors.Is(err, wxaiinspectionservice.ErrRunAlreadyActive) {
			log.Printf("run scheduled wxai inspection: %v", err)
		}
	}()
}

func (worker *WxaiInspectionWorker) lastScheduledRunTime(ctx context.Context) time.Time {
	runs, err := worker.store.ListWxaiInspectionRuns(ctx, 20)
	if err != nil {
		return time.Time{}
	}
	for _, run := range runs {
		if run.TriggerType != model.WxaiInspectionTriggerScheduled || run.StartedAtMS <= 0 {
			continue
		}
		return time.UnixMilli(run.StartedAtMS)
	}
	return time.Time{}
}
