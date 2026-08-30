package store

import (
	"context"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/wxaiinspection"
)

type RealtimeDegradedAttemptKey = wxaiinspection.RealtimeDegradedAttemptKey

func (s *Store) wxaiInspections() wxaiinspection.Repository {
	return wxaiinspection.New(s.db)
}

func (s *Store) CreateWxaiInspectionRun(ctx context.Context, run model.WxaiInspectionRun) (model.WxaiInspectionRun, error) {
	return s.wxaiInspections().CreateRun(ctx, run)
}

func (s *Store) UpdateWxaiInspectionRun(ctx context.Context, run model.WxaiInspectionRun) error {
	return s.wxaiInspections().UpdateRun(ctx, run)
}

func (s *Store) InsertWxaiInspectionResult(ctx context.Context, result model.WxaiInspectionResult) (model.WxaiInspectionResult, error) {
	return s.wxaiInspections().InsertResult(ctx, result)
}

func (s *Store) InsertWxaiInspectionLog(ctx context.Context, entry model.WxaiInspectionLog) (model.WxaiInspectionLog, error) {
	return s.wxaiInspections().InsertLog(ctx, entry)
}

func (s *Store) UpsertWxaiAccountStatusDetail(ctx context.Context, detail model.WxaiAccountStatusDetail) error {
	return s.wxaiInspections().UpsertAccountStatusDetail(ctx, detail)
}

func (s *Store) UpdateWxaiAccountScheduleGroups(ctx context.Context, runID int64, groups map[string]int) error {
	return s.wxaiInspections().UpdateAccountScheduleGroups(ctx, runID, groups)
}

func (s *Store) UpsertWxaiAccountProfile(ctx context.Context, profile model.WxaiAccountProfile) error {
	return s.wxaiInspections().UpsertAccountProfile(ctx, profile)
}

func (s *Store) ListWxaiAccountProfiles(ctx context.Context) ([]model.WxaiAccountProfile, error) {
	return s.wxaiInspections().ListAccountProfiles(ctx)
}

func (s *Store) ListWxaiInspectionRuns(ctx context.Context, limit int) ([]model.WxaiInspectionRun, error) {
	return s.wxaiInspections().ListRuns(ctx, limit)
}

func (s *Store) GetWxaiInspectionRun(ctx context.Context, runID int64) (model.WxaiInspectionRun, bool, error) {
	return s.wxaiInspections().GetRun(ctx, runID)
}

func (s *Store) GetLatestWxaiInspectionRun(ctx context.Context) (model.WxaiInspectionRun, bool, error) {
	return s.wxaiInspections().GetLatestRun(ctx)
}

func (s *Store) GetLatestCompletedWxaiInspectionRunByTriggerType(ctx context.Context, triggerType string) (model.WxaiInspectionRun, bool, error) {
	return s.wxaiInspections().GetLatestCompletedRunByTriggerType(ctx, triggerType)
}

func (s *Store) GetLatestWxaiInspectionRunByTrigger(ctx context.Context, triggerType string, triggerKey string) (model.WxaiInspectionRun, bool, error) {
	return s.wxaiInspections().GetLatestRunByTrigger(ctx, triggerType, triggerKey)
}

func (s *Store) ListWxaiInspectionResults(ctx context.Context, runID int64) ([]model.WxaiInspectionResult, error) {
	return s.wxaiInspections().ListResults(ctx, runID)
}

func (s *Store) ListWxaiInspectionLogs(ctx context.Context, runID int64) ([]model.WxaiInspectionLog, error) {
	return s.wxaiInspections().ListLogs(ctx, runID)
}

func (s *Store) FindWxaiRealtimeDegradedAttempts(ctx context.Context, attempts []RealtimeDegradedAttemptKey) (map[RealtimeDegradedAttemptKey]struct{}, error) {
	return s.wxaiInspections().FindRealtimeDegradedAttempts(ctx, attempts)
}

func (s *Store) ListWxaiAccountStatusItems(ctx context.Context, runID int64) ([]model.WxaiAccountStatusItem, error) {
	return s.wxaiInspections().ListAccountStatusItems(ctx, runID)
}

func (s *Store) GetWxaiInspectionSettings(ctx context.Context) (model.ManagerWxaiInspectionConfig, bool, error) {
	return s.wxaiInspections().GetSettings(ctx)
}

func (s *Store) SaveWxaiInspectionSettings(ctx context.Context, settings model.ManagerWxaiInspectionConfig) (model.ManagerWxaiInspectionConfig, error) {
	return s.wxaiInspections().SaveSettings(ctx, settings)
}

func (s *Store) GetWxaiRealtimeDegradationState(ctx context.Context, accountKey string) (model.WxaiRealtimeDegradationState, bool, error) {
	return s.WxaiRealtimeDegradations.Get(ctx, accountKey)
}

func (s *Store) UpsertWxaiRealtimeDegradationState(ctx context.Context, state model.WxaiRealtimeDegradationState) error {
	return s.WxaiRealtimeDegradations.Upsert(ctx, state)
}

func (s *Store) DeleteWxaiRealtimeDegradationState(ctx context.Context, accountKey string) error {
	return s.WxaiRealtimeDegradations.Delete(ctx, accountKey)
}
