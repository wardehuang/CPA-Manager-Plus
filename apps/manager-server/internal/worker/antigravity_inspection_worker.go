package worker

import (
	"context"
	"log"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	antigravityinspectionservice "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/antigravityinspection"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type AntigravityInspectionWorker struct {
	store   *store.Store
	service *antigravityinspectionservice.Service
}

func NewAntigravityInspectionWorker(store *store.Store, service *antigravityinspectionservice.Service) *AntigravityInspectionWorker {
	return &AntigravityInspectionWorker{store: store, service: service}
}

func (w *AntigravityInspectionWorker) Start(ctx context.Context) {
	if w == nil || w.store == nil || w.service == nil {
		return
	}
	go w.run(ctx)
}

func (w *AntigravityInspectionWorker) run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *AntigravityInspectionWorker) tick(ctx context.Context) {
	cfg, configured, err := w.service.ResolveConfig(ctx)
	if err != nil {
		log.Printf("resolve antigravity inspection config: %v", err)
		return
	}
	if !configured || cfg.Enabled == nil || !*cfg.Enabled || w.service.IsRunning() {
		return
	}
	now := time.Now()
	last := w.lastScheduledRunTime(ctx, cfg.TargetProvider)
	interval := time.Duration(cfg.Schedule.IntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = time.Hour
	}
	if !last.IsZero() && now.Sub(last) < interval {
		return
	}
	triggerKey := now.Format("200601021504")
	go func() {
		if _, err := w.service.Run(ctx, antigravityinspectionservice.RunRequest{
			TriggerType:    model.AntigravityInspectionTriggerScheduled,
			TriggerKey:     triggerKey,
			TargetProvider: cfg.TargetProvider,
		}); err != nil && err != antigravityinspectionservice.ErrRunAlreadyActive {
			log.Printf("run scheduled antigravity inspection: %v", err)
		}
	}()
}

func (w *AntigravityInspectionWorker) lastScheduledRunTime(ctx context.Context, targetProvider string) time.Time {
	runs, err := w.store.ListAntigravityInspectionRuns(ctx, 50)
	if err != nil {
		return time.Time{}
	}
	targetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderServer)
	for _, run := range runs {
		if run.TriggerType != model.AntigravityInspectionTriggerScheduled || run.StartedAtMS <= 0 {
			continue
		}
		if model.NormalizeAntigravityTargetProvider(run.TargetProvider, model.AntigravityTargetProviderServer) != targetProvider {
			continue
		}
		return time.UnixMilli(run.StartedAtMS)
	}
	return time.Time{}
}
