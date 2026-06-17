package codexaccountstatus

import (
	"context"
	"errors"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

var ErrNoCodexInspectionRun = errors.New("codex inspection run not found")

type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) Latest(ctx context.Context) (model.CodexAccountStatusResponse, error) {
	runs, err := s.store.ListCodexInspectionRuns(ctx, 1)
	if err != nil {
		return model.CodexAccountStatusResponse{}, err
	}
	if len(runs) == 0 {
		return model.CodexAccountStatusResponse{}, ErrNoCodexInspectionRun
	}
	items, err := s.store.ListCodexAccountStatusItems(ctx, runs[0].ID)
	if err != nil {
		return model.CodexAccountStatusResponse{}, err
	}
	s.refreshCodexAccountWindowCosts(ctx, items)
	costs, err := s.store.ListCodexAccountWindowCostsByRun(ctx, runs[0].ID)
	if err != nil {
		return model.CodexAccountStatusResponse{}, err
	}
	costsByAccount := make(map[string][]model.CodexAccountWindowCost, len(costs))
	for _, cost := range costs {
		costsByAccount[cost.AccountKey] = append(costsByAccount[cost.AccountKey], cost)
	}
	for index := range items {
		items[index].WindowCosts = costsByAccount[items[index].AccountKey]
		if adjustment, ok, err := s.store.GetCodexPriorityAdjustment(ctx, items[index].AccountKey); err == nil && ok {
			items[index].OriginalPriority = adjustment.OriginalPriority
		}
	}
	return model.CodexAccountStatusResponse{Run: runs[0], Items: items}, nil
}
