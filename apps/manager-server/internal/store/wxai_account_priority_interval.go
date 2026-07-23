package store

import (
	"context"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/wxaiaccountpriorityinterval"
)

func (store *Store) wxaiAccountPriorityIntervals() wxaiaccountpriorityinterval.Repository {
	return wxaiaccountpriorityinterval.New(store.db)
}

func (store *Store) GetWxaiAccountPriorityInterval(
	ctx context.Context,
	accountKey string,
) (model.WxaiAccountPriorityInterval, bool, error) {
	return store.wxaiAccountPriorityIntervals().Get(ctx, accountKey)
}

func (store *Store) MarkWxaiAccountPriorityAbnormal(
	ctx context.Context,
	accountKey string,
	endedAtMS int64,
) error {
	return store.wxaiAccountPriorityIntervals().MarkAbnormal(ctx, accountKey, endedAtMS)
}

func (store *Store) MarkWxaiAccountPriorityRecovered(
	ctx context.Context,
	accountKey string,
	startedAtMS int64,
) error {
	return store.wxaiAccountPriorityIntervals().MarkRecovered(ctx, accountKey, startedAtMS)
}
