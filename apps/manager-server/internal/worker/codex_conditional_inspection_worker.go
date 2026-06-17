package worker

import (
	"context"
	"log"
	"sync"
	"time"

	codexinspectionservice "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/codexinspection"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type CodexConditionalInspectionWorker struct {
	store   *store.Store
	service *codexinspectionservice.Service

	mu      sync.Mutex
	running bool
}

func NewCodexConditionalInspectionWorker(store *store.Store, service *codexinspectionservice.Service) *CodexConditionalInspectionWorker {
	return &CodexConditionalInspectionWorker{store: store, service: service}
}

func (w *CodexConditionalInspectionWorker) Start(ctx context.Context) {
	if w == nil || w.store == nil || w.service == nil {
		return
	}
	go w.run(ctx)
}

func (w *CodexConditionalInspectionWorker) run(ctx context.Context) {
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

func (w *CodexConditionalInspectionWorker) tick(ctx context.Context) {
	cfg, configured, err := w.service.ResolveConfig(ctx)
	if err != nil {
		log.Printf("resolve codex conditional inspection config: %v", err)
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
	runs, err := w.store.ListCodexInspectionRuns(ctx, 1)
	if err != nil {
		w.releaseRun()
		log.Printf("load latest codex inspection run for conditional inspection: %v", err)
		return
	}
	if len(runs) == 0 || runs[0].ID <= 0 {
		w.releaseRun()
		return
	}
	if runs[0].Status == "running" {
		w.releaseRun()
		return
	}
	runID := runs[0].ID
	go func() {
		defer w.releaseRun()
		if _, err := w.service.RunConditional(ctx, codexinspectionservice.ConditionalRunRequest{RunID: runID}); err != nil && err != codexinspectionservice.ErrRunAlreadyActive {
			log.Printf("run conditional codex inspection: %v", err)
		}
	}()
}

func (w *CodexConditionalInspectionWorker) acquireRun() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return false
	}
	w.running = true
	return true
}

func (w *CodexConditionalInspectionWorker) releaseRun() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.running = false
}
