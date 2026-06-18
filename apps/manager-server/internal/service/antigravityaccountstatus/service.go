package antigravityaccountstatus

import (
	"context"
	"errors"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

var ErrNoAntigravityInspectionRun = errors.New("antigravity inspection run not found")

type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) Latest(ctx context.Context, targetProvider string) (model.AntigravityAccountStatusResponse, error) {
	targetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude)
	run, ok, err := s.store.GetLatestAntigravityInspectionRunByProvider(ctx, targetProvider)
	if err != nil {
		return model.AntigravityAccountStatusResponse{}, err
	}
	if !ok {
		return model.AntigravityAccountStatusResponse{}, ErrNoAntigravityInspectionRun
	}
	items, err := s.store.ListAntigravityAccountStatusItems(ctx, run.ID, targetProvider)
	if err != nil {
		return model.AntigravityAccountStatusResponse{}, err
	}
	costs, err := s.store.ListAntigravityAccountWindowCostsByRun(ctx, run.ID, targetProvider)
	if err != nil {
		return model.AntigravityAccountStatusResponse{}, err
	}
	costsByAccount := make(map[string][]model.AntigravityAccountWindowCost, len(costs))
	for _, cost := range costs {
		costsByAccount[cost.AccountKey] = append(costsByAccount[cost.AccountKey], cost)
	}
	for index := range items {
		items[index].WindowCosts = costsByAccount[items[index].AccountKey]
		if adjustment, ok, err := s.store.GetAntigravityPriorityAdjustment(ctx, items[index].AccountKey, targetProvider); err == nil && ok {
			items[index].OriginalPriority = adjustment.OriginalPriority
		}
	}
	return model.AntigravityAccountStatusResponse{Run: run, Items: items}, nil
}
