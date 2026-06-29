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
	resultProvider := model.AntigravityTargetProviderServer
	run, ok, err := s.store.GetLatestAntigravityInspectionRunByProvider(ctx, resultProvider)
	if err != nil {
		return model.AntigravityAccountStatusResponse{}, err
	}
	if !ok {
		resultProvider = targetProvider
		run, ok, err = s.store.GetLatestAntigravityInspectionRunByProvider(ctx, resultProvider)
		if err != nil {
			return model.AntigravityAccountStatusResponse{}, err
		}
	}
	if !ok {
		return model.AntigravityAccountStatusResponse{Items: []model.AntigravityAccountStatusItem{}}, nil
	}
	items, err := s.store.ListAntigravityAccountStatusItemsWithDetailProvider(ctx, run.ID, resultProvider, targetProvider)
	if err != nil {
		return model.AntigravityAccountStatusResponse{}, err
	}
	if len(items) == 0 && resultProvider == model.AntigravityTargetProviderServer {
		providerRun, providerOK, err := s.store.GetLatestAntigravityInspectionRunByProvider(ctx, targetProvider)
		if err != nil {
			return model.AntigravityAccountStatusResponse{}, err
		}
		if providerOK {
			providerItems, err := s.store.ListAntigravityAccountStatusItemsWithDetailProvider(ctx, providerRun.ID, targetProvider, targetProvider)
			if err != nil {
				return model.AntigravityAccountStatusResponse{}, err
			}
			if len(providerItems) > 0 {
				run = providerRun
				resultProvider = targetProvider
				items = providerItems
			}
		}
	}
	s.refreshAntigravityAccountWindowCosts(ctx, items, targetProvider)
	costs, err := s.store.ListAntigravityAccountWindowCostsByRun(ctx, run.ID, targetProvider)
	if err != nil {
		return model.AntigravityAccountStatusResponse{}, err
	}
	costsByAccount := make(map[string][]model.AntigravityAccountWindowCost, len(costs))
	for _, cost := range costs {
		costsByAccount[cost.AccountKey] = append(costsByAccount[cost.AccountKey], cost)
	}
	for index := range items {
		items[index].TargetProvider = targetProvider
		items[index].WindowCosts = filterAntigravityWindowCosts(items[index], costsByAccount[items[index].AccountKey], targetProvider)
		if adjustment, ok, err := s.store.GetAntigravityPriorityAdjustment(ctx, items[index].AccountKey, targetProvider); err == nil && ok {
			items[index].OriginalPriority = adjustment.OriginalPriority
		}
	}
	return model.AntigravityAccountStatusResponse{Run: run, Items: items}, nil
}
