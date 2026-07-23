package store

import (
	"context"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/wxaiaccountwindowcost"
)

func (store *Store) wxaiAccountWindowCosts() wxaiaccountwindowcost.Repository {
	return wxaiaccountwindowcost.New(store.db)
}

func (store *Store) ListWxaiAccountWindowCostsByRun(
	ctx context.Context,
	runID int64,
	nowMS int64,
) ([]model.WxaiAccountWindowCost, error) {
	return store.wxaiAccountWindowCosts().ListByRun(ctx, runID, nowMS)
}

func (store *Store) SumWxaiAccountUsageByWindow(
	ctx context.Context,
	target model.WxaiAccountWindowCostTarget,
	fromMS int64,
	toMS int64,
) ([]model.WxaiAccountWindowUsageAggregate, error) {
	return store.wxaiAccountWindowCosts().SumUsageByWindow(ctx, target, fromMS, toMS)
}

func (store *Store) UpsertWxaiAccountWindowCost(ctx context.Context, cost model.WxaiAccountWindowCost) error {
	return store.wxaiAccountWindowCosts().Upsert(ctx, cost)
}
