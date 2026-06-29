package worker

import (
	"context"
	"log"
	"sync"
	"time"

	antigravityinspectionservice "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/antigravityinspection"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type AntigravityConditionalInspectionWorker struct {
	store   *store.Store
	service *antigravityinspectionservice.Service

	mu      sync.Mutex
	running bool
}

func NewAntigravityConditionalInspectionWorker(store *store.Store, service *antigravityinspectionservice.Service) *AntigravityConditionalInspectionWorker {
	return &AntigravityConditionalInspectionWorker{store: store, service: service}
}

func (w *AntigravityConditionalInspectionWorker) Start(ctx context.Context) {
	if w == nil || w.store == nil || w.service == nil {
		return
	}
	go w.run(ctx)
}

func (w *AntigravityConditionalInspectionWorker) run(ctx context.Context) {
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

func (w *AntigravityConditionalInspectionWorker) tick(ctx context.Context) {
	cfg, configured, err := w.service.ResolveConfig(ctx)
	if err != nil {
		log.Printf("resolve antigravity conditional inspection config: %v", err)
		return
	}
	if !configured || cfg.Enabled == nil || !*cfg.Enabled {
		return
	}
	if w.service.IsRunning() {
		return
	}
	if !w.acquireRun() {
		return
	}
	runs, err := w.store.ListAntigravityInspectionRuns(ctx, 1)
	if err != nil {
		w.releaseRun()
		log.Printf("load latest antigravity inspection run for conditional inspection: %v", err)
		return
	}
	if len(runs) == 0 || runs[0].ID <= 0 || runs[0].Status == "running" {
		w.releaseRun()
		return
	}
	runID := runs[0].ID
	targetProvider := runs[0].TargetProvider
	go func() {
		defer w.releaseRun()
		if _, err := w.service.RunConditional(ctx, antigravityinspectionservice.ConditionalRunRequest{RunID: runID, TargetProvider: targetProvider}); err != nil && err != antigravityinspectionservice.ErrRunAlreadyActive {
			log.Printf("run conditional antigravity inspection: %v", err)
		}
	}()
}

func (w *AntigravityConditionalInspectionWorker) acquireRun() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return false
	}
	w.running = true
	return true
}

func (w *AntigravityConditionalInspectionWorker) releaseRun() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.running = false
}
