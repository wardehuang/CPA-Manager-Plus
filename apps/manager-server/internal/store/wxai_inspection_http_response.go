package store

import (
	"context"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/wxaiinspectionresponse"
)

func (store *Store) InsertWxaiInspectionHTTPResponse(
	ctx context.Context,
	response model.WxaiInspectionHTTPResponse,
) (model.WxaiInspectionHTTPResponse, error) {
	return wxaiinspectionresponse.New(store.db).Insert(ctx, response)
}
