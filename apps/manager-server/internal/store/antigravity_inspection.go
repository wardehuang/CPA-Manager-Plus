package store

import (
	"context"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/antigravityaccountstatus"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/antigravityaccountwindowcost"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/antigravityinspection"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/antigravitypriority"
)

func (s *Store) antigravityInspections() antigravityinspection.Repository {
	return antigravityinspection.New(s.db)
}

func (s *Store) antigravityAccountStatus() antigravityaccountstatus.Repository {
	return antigravityaccountstatus.New(s.db)
}

func (s *Store) antigravityAccountWindowCosts() antigravityaccountwindowcost.Repository {
	return antigravityaccountwindowcost.New(s.db)
}

func (s *Store) antigravityPriorityAdjustments() antigravitypriority.Repository {
	return antigravitypriority.New(s.db)
}

func (s *Store) CreateAntigravityInspectionRun(ctx context.Context, run model.AntigravityInspectionRun) (model.AntigravityInspectionRun, error) {
	return s.antigravityInspections().CreateRun(ctx, run)
}

func (s *Store) UpdateAntigravityInspectionRun(ctx context.Context, run model.AntigravityInspectionRun) error {
	return s.antigravityInspections().UpdateRun(ctx, run)
}

func (s *Store) InsertAntigravityInspectionResult(ctx context.Context, result model.AntigravityInspectionResult) (model.AntigravityInspectionResult, error) {
	return s.antigravityInspections().InsertResult(ctx, result)
}

func (s *Store) InsertAntigravityInspectionLog(ctx context.Context, entry model.AntigravityInspectionLog) (model.AntigravityInspectionLog, error) {
	return s.antigravityInspections().InsertLog(ctx, entry)
}

func (s *Store) ListAntigravityInspectionRuns(ctx context.Context, limit int) ([]model.AntigravityInspectionRun, error) {
	return s.antigravityInspections().ListRuns(ctx, limit)
}

func (s *Store) GetAntigravityInspectionRun(ctx context.Context, id int64) (model.AntigravityInspectionRun, bool, error) {
	return s.antigravityInspections().GetRun(ctx, id)
}

func (s *Store) GetLatestAntigravityInspectionRunByProvider(ctx context.Context, targetProvider string) (model.AntigravityInspectionRun, bool, error) {
	return s.antigravityInspections().GetLatestRunByProvider(ctx, targetProvider)
}

func (s *Store) ListAntigravityInspectionResults(ctx context.Context, runID int64) ([]model.AntigravityInspectionResult, error) {
	return s.antigravityInspections().ListResults(ctx, runID)
}

func (s *Store) ListAntigravityInspectionLogs(ctx context.Context, runID int64) ([]model.AntigravityInspectionLog, error) {
	return s.antigravityInspections().ListLogs(ctx, runID)
}

func (s *Store) UpsertAntigravityAccountStatusDetail(ctx context.Context, detail model.AntigravityAccountStatusDetail) error {
	return s.antigravityAccountStatus().UpsertDetail(ctx, detail)
}

func (s *Store) ListAntigravityAccountStatusItems(ctx context.Context, runID int64, targetProvider string) ([]model.AntigravityAccountStatusItem, error) {
	return s.antigravityAccountStatus().ListItemsByRun(ctx, runID, targetProvider)
}

func (s *Store) ListAntigravityAccountWindowCostsByRun(ctx context.Context, runID int64, targetProvider string) ([]model.AntigravityAccountWindowCost, error) {
	return s.antigravityAccountWindowCosts().ListByRun(ctx, runID, targetProvider)
}

func (s *Store) UpsertAntigravityAccountWindowCost(ctx context.Context, cost model.AntigravityAccountWindowCost) error {
	return s.antigravityAccountWindowCosts().Upsert(ctx, cost)
}

func (s *Store) GetAntigravityPriorityAdjustment(ctx context.Context, accountKey string, targetProvider string) (model.AntigravityPriorityAdjustment, bool, error) {
	return s.antigravityPriorityAdjustments().Get(ctx, accountKey, targetProvider)
}

func (s *Store) UpsertAntigravityPriorityAdjustment(ctx context.Context, adjustment model.AntigravityPriorityAdjustment) error {
	return s.antigravityPriorityAdjustments().Upsert(ctx, adjustment)
}

func (s *Store) DeleteAntigravityPriorityAdjustment(ctx context.Context, accountKey string, targetProvider string) error {
	return s.antigravityPriorityAdjustments().Delete(ctx, accountKey, targetProvider)
}
