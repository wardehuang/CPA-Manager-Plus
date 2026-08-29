package wxaiinspection

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	CreateRun(ctx context.Context, run model.WxaiInspectionRun) (model.WxaiInspectionRun, error)
	UpdateRun(ctx context.Context, run model.WxaiInspectionRun) error
	InsertResult(ctx context.Context, result model.WxaiInspectionResult) (model.WxaiInspectionResult, error)
	InsertLog(ctx context.Context, entry model.WxaiInspectionLog) (model.WxaiInspectionLog, error)
	UpsertAccountStatusDetail(ctx context.Context, detail model.WxaiAccountStatusDetail) error
	UpdateAccountScheduleGroups(ctx context.Context, runID int64, groups map[string]int) error
	UpsertAccountProfile(ctx context.Context, profile model.WxaiAccountProfile) error
	ListAccountProfiles(ctx context.Context) ([]model.WxaiAccountProfile, error)
	ListRuns(ctx context.Context, limit int) ([]model.WxaiInspectionRun, error)
	GetRun(ctx context.Context, id int64) (model.WxaiInspectionRun, bool, error)
	GetLatestRun(ctx context.Context) (model.WxaiInspectionRun, bool, error)
	GetLatestCompletedRunByTriggerType(ctx context.Context, triggerType string) (model.WxaiInspectionRun, bool, error)
	GetLatestRunByTrigger(ctx context.Context, triggerType string, triggerKey string) (model.WxaiInspectionRun, bool, error)
	ListResults(ctx context.Context, runID int64) ([]model.WxaiInspectionResult, error)
	ListLogs(ctx context.Context, runID int64) ([]model.WxaiInspectionLog, error)
	ListAccountStatusItems(ctx context.Context, runID int64) ([]model.WxaiAccountStatusItem, error)
	GetSettings(ctx context.Context) (model.ManagerWxaiInspectionConfig, bool, error)
	SaveSettings(ctx context.Context, settings model.ManagerWxaiInspectionConfig) (model.ManagerWxaiInspectionConfig, error)
}

type repository struct {
	db *sql.DB
}

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (repository *repository) CreateRun(ctx context.Context, run model.WxaiInspectionRun) (model.WxaiInspectionRun, error) {
	now := time.Now().UnixMilli()
	if run.StartedAtMS <= 0 {
		run.StartedAtMS = now
	}
	if run.CreatedAtMS <= 0 {
		run.CreatedAtMS = now
	}
	run.UpdatedAtMS = now
	if run.Status == "" {
		run.Status = model.WxaiInspectionStatusRunning
	}
	if run.SettingsJSON == "" {
		run.SettingsJSON = model.MarshalWxaiInspectionSettings(run.Settings)
	}
	result, err := repository.db.ExecContext(ctx, `insert into wxai_inspection_runs (
		trigger_type, trigger_key, status, started_at_ms, finished_at_ms,
		total_files, probe_set_count, sampled_count, disabled_count, enabled_count,
		delete_count, disable_count, enable_count, reauth_count, keep_count,
		quota_exhausted_count, abnormal_count, error, settings_json, created_at_ms, updated_at_ms
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.TriggerType, nullableString(run.TriggerKey), run.Status, run.StartedAtMS, nullablePositiveInt64(run.FinishedAtMS),
		run.TotalFiles, run.ProbeSetCount, run.SampledCount, run.DisabledCount, run.EnabledCount,
		run.DeleteCount, run.DisableCount, run.EnableCount, run.ReauthCount, run.KeepCount,
		run.QuotaExhaustedCount, run.AbnormalCount,
		nullableString(run.Error), run.SettingsJSON, run.CreatedAtMS, run.UpdatedAtMS,
	)
	if err != nil {
		return model.WxaiInspectionRun{}, err
	}
	run.ID, err = result.LastInsertId()
	if err != nil {
		return model.WxaiInspectionRun{}, err
	}
	return run, nil
}

func (repository *repository) UpdateRun(ctx context.Context, run model.WxaiInspectionRun) error {
	if run.ID <= 0 {
		return errors.New("wxai inspection run id is required")
	}
	run.UpdatedAtMS = time.Now().UnixMilli()
	if run.SettingsJSON == "" {
		run.SettingsJSON = model.MarshalWxaiInspectionSettings(run.Settings)
	}
	_, err := repository.db.ExecContext(ctx, `update wxai_inspection_runs set
		status = ?, finished_at_ms = ?, total_files = ?, probe_set_count = ?, sampled_count = ?,
		disabled_count = ?, enabled_count = ?, delete_count = ?, disable_count = ?, enable_count = ?,
		reauth_count = ?, keep_count = ?, quota_exhausted_count = ?, abnormal_count = ?,
		error = ?, settings_json = ?, updated_at_ms = ? where id = ?`,
		run.Status, nullablePositiveInt64(run.FinishedAtMS), run.TotalFiles, run.ProbeSetCount, run.SampledCount,
		run.DisabledCount, run.EnabledCount, run.DeleteCount, run.DisableCount, run.EnableCount,
		run.ReauthCount, run.KeepCount, run.QuotaExhaustedCount, run.AbnormalCount,
		nullableString(run.Error), run.SettingsJSON, run.UpdatedAtMS, run.ID,
	)
	return err
}

func (repository *repository) InsertResult(ctx context.Context, result model.WxaiInspectionResult) (model.WxaiInspectionResult, error) {
	if result.CreatedAtMS <= 0 {
		result.CreatedAtMS = time.Now().UnixMilli()
	}
	if result.QuotaWindowsJSON == "" && len(result.QuotaWindows) > 0 {
		result.QuotaWindowsJSON = model.MarshalWxaiInspectionQuotaWindows(result.QuotaWindows)
	}
	result.ActionStatus = model.NormalizeWxaiInspectionActionStatus(result.ActionStatus, result.Action)
	disabled := boolInteger(result.Disabled)
	isQuota := boolInteger(result.IsQuota)
	databaseResult, err := repository.db.ExecContext(ctx, `insert into wxai_inspection_results (
		run_id, account_key, file_name, display_account, auth_index, account_id, provider,
		disabled, status, state, action, action_reason, action_status, executed_action, action_error,
		status_code, used_percent, is_quota, error, plan_type, quota_windows_json, monthly_limit_cents,
		monthly_used_cents, error_kind, error_detail, created_at_ms
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(run_id, account_key) do update set
		file_name = excluded.file_name, display_account = excluded.display_account,
		auth_index = excluded.auth_index, account_id = excluded.account_id, provider = excluded.provider,
		disabled = excluded.disabled, status = excluded.status, state = excluded.state,
		action = excluded.action, action_reason = excluded.action_reason, action_status = excluded.action_status,
		executed_action = excluded.executed_action, action_error = excluded.action_error,
		status_code = excluded.status_code, used_percent = excluded.used_percent, is_quota = excluded.is_quota,
		error = excluded.error, plan_type = excluded.plan_type, quota_windows_json = excluded.quota_windows_json,
		monthly_limit_cents = excluded.monthly_limit_cents, monthly_used_cents = excluded.monthly_used_cents,
		error_kind = excluded.error_kind, error_detail = excluded.error_detail, created_at_ms = excluded.created_at_ms`,
		result.RunID, result.AccountKey, result.FileName, result.DisplayAccount, nullableString(result.AuthIndex), nullableString(result.AccountID), result.Provider,
		disabled, nullableString(result.Status), nullableString(result.State), result.Action, nullableString(result.ActionReason), nullableString(result.ActionStatus),
		nullableString(result.ExecutedAction), nullableString(result.ActionError), nullableInt(result.StatusCode), nullableFloat(result.UsedPercent), isQuota,
		nullableString(result.Error), nullableString(result.PlanType), nullableString(result.QuotaWindowsJSON), nullableFloat(result.MonthlyLimitCents),
		nullableFloat(result.MonthlyUsedCents), nullableString(result.ErrorKind), nullableString(result.ErrorDetail), result.CreatedAtMS,
	)
	if err != nil {
		return model.WxaiInspectionResult{}, err
	}
	result.ID, _ = databaseResult.LastInsertId()
	return result, nil
}

func (repository *repository) InsertLog(ctx context.Context, entry model.WxaiInspectionLog) (model.WxaiInspectionLog, error) {
	if entry.CreatedAtMS <= 0 {
		entry.CreatedAtMS = time.Now().UnixMilli()
	}
	if entry.DetailJSON == "" && entry.Detail != nil {
		data, err := json.Marshal(entry.Detail)
		if err == nil {
			entry.DetailJSON = string(data)
		}
	}
	databaseResult, err := repository.db.ExecContext(ctx, `insert into wxai_inspection_logs (
		run_id, level, message, detail_json, created_at_ms
	) values (?, ?, ?, ?, ?)`, entry.RunID, entry.Level, entry.Message, nullableString(entry.DetailJSON), entry.CreatedAtMS)
	if err != nil {
		return model.WxaiInspectionLog{}, err
	}
	entry.ID, _ = databaseResult.LastInsertId()
	return entry, nil
}

func (repository *repository) UpsertAccountStatusDetail(ctx context.Context, detail model.WxaiAccountStatusDetail) error {
	now := time.Now().UnixMilli()
	if detail.CreatedAtMS <= 0 {
		detail.CreatedAtMS = now
	}
	detail.UpdatedAtMS = now
	_, err := repository.db.ExecContext(ctx, `insert into wxai_account_status_details (
		run_id, account_key, priority, schedule_group, account_type, weekly_used_percent, weekly_reset_at_ms,
		monthly_used_percent, monthly_reset_at_ms, monthly_limit_cents, monthly_used_cents,
		checked_at_ms, created_at_ms, updated_at_ms
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(run_id, account_key) do update set
		priority = excluded.priority, schedule_group = excluded.schedule_group, account_type = excluded.account_type,
		weekly_used_percent = excluded.weekly_used_percent, weekly_reset_at_ms = excluded.weekly_reset_at_ms,
		monthly_used_percent = excluded.monthly_used_percent, monthly_reset_at_ms = excluded.monthly_reset_at_ms,
		monthly_limit_cents = excluded.monthly_limit_cents, monthly_used_cents = excluded.monthly_used_cents,
		checked_at_ms = excluded.checked_at_ms, updated_at_ms = excluded.updated_at_ms`,
		detail.RunID, detail.AccountKey, nullableInt(detail.Priority), nullableInt(detail.ScheduleGroup), nullableString(detail.AccountType),
		nullableFloat(detail.WeeklyUsedPercent), nullablePositiveInt64(detail.WeeklyResetAtMS),
		nullableFloat(detail.MonthlyUsedPercent), nullablePositiveInt64(detail.MonthlyResetAtMS),
		nullableFloat(detail.MonthlyLimitCents), nullableFloat(detail.MonthlyUsedCents),
		nullablePositiveInt64(detail.CheckedAtMS), detail.CreatedAtMS, detail.UpdatedAtMS,
	)
	return err
}

func (repository *repository) UpdateAccountScheduleGroups(ctx context.Context, runID int64, groups map[string]int) error {
	transaction, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for accountKey, group := range groups {
		result, updateErr := transaction.ExecContext(
			ctx,
			`update wxai_account_status_details set schedule_group = ?, updated_at_ms = ? where run_id = ? and account_key = ?`,
			group,
			time.Now().UnixMilli(),
			runID,
			accountKey,
		)
		if updateErr != nil {
			return updateErr
		}
		updated, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if updated != 1 {
			return fmt.Errorf("wxai account status detail not found: run_id=%d account_key=%s", runID, accountKey)
		}
	}
	return transaction.Commit()
}

func (repository *repository) UpsertAccountProfile(ctx context.Context, profile model.WxaiAccountProfile) error {
	now := time.Now().UnixMilli()
	if profile.CreatedAtMS <= 0 {
		profile.CreatedAtMS = now
	}
	profile.UpdatedAtMS = now
	_, err := repository.db.ExecContext(ctx, `insert into wxai_account_profiles (
		account_key, account_type, created_at_ms, updated_at_ms
	) values (?, ?, ?, ?)
	on conflict(account_key) do update set
		account_type = excluded.account_type,
		updated_at_ms = excluded.updated_at_ms`,
		profile.AccountKey,
		strings.ToUpper(strings.TrimSpace(profile.AccountType)),
		profile.CreatedAtMS,
		profile.UpdatedAtMS,
	)
	return err
}

func (repository *repository) ListAccountProfiles(ctx context.Context) ([]model.WxaiAccountProfile, error) {
	rows, err := repository.db.QueryContext(ctx, `select account_key, account_type, created_at_ms, updated_at_ms
		from wxai_account_profiles order by account_key asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := make([]model.WxaiAccountProfile, 0)
	for rows.Next() {
		var profile model.WxaiAccountProfile
		if err := rows.Scan(
			&profile.AccountKey,
			&profile.AccountType,
			&profile.CreatedAtMS,
			&profile.UpdatedAtMS,
		); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (repository *repository) ListRuns(ctx context.Context, limit int) ([]model.WxaiInspectionRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := repository.db.QueryContext(ctx, runSelectSQL()+` order by started_at_ms desc, id desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]model.WxaiInspectionRun, 0)
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (repository *repository) GetRun(ctx context.Context, id int64) (model.WxaiInspectionRun, bool, error) {
	run, err := scanRun(repository.db.QueryRowContext(ctx, runSelectSQL()+` where id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.WxaiInspectionRun{}, false, nil
	}
	return run, err == nil, err
}

func (repository *repository) GetLatestRun(ctx context.Context) (model.WxaiInspectionRun, bool, error) {
	run, err := scanRun(repository.db.QueryRowContext(ctx, runSelectSQL()+` order by started_at_ms desc, id desc limit 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return model.WxaiInspectionRun{}, false, nil
	}
	return run, err == nil, err
}

func (repository *repository) GetLatestCompletedRunByTriggerType(ctx context.Context, triggerType string) (model.WxaiInspectionRun, bool, error) {
	run, err := scanRun(repository.db.QueryRowContext(
		ctx,
		runSelectSQL()+` where trigger_type = ? and status = ? order by finished_at_ms desc, id desc limit 1`,
		triggerType,
		model.WxaiInspectionStatusCompleted,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return model.WxaiInspectionRun{}, false, nil
	}
	return run, err == nil, err
}

func (repository *repository) GetLatestRunByTrigger(ctx context.Context, triggerType string, triggerKey string) (model.WxaiInspectionRun, bool, error) {
	run, err := scanRun(repository.db.QueryRowContext(
		ctx,
		runSelectSQL()+` where trigger_type = ? and trigger_key = ? order by started_at_ms desc, id desc limit 1`,
		triggerType,
		triggerKey,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return model.WxaiInspectionRun{}, false, nil
	}
	return run, err == nil, err
}

func (repository *repository) ListResults(ctx context.Context, runID int64) ([]model.WxaiInspectionResult, error) {
	rows, err := repository.db.QueryContext(ctx, resultSelectSQL()+` where run_id = ? order by file_name asc, display_account asc, id asc`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]model.WxaiInspectionResult, 0)
	for rows.Next() {
		result, scanErr := scanResult(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func (repository *repository) ListLogs(ctx context.Context, runID int64) ([]model.WxaiInspectionLog, error) {
	rows, err := repository.db.QueryContext(ctx, `select id, run_id, level, message, detail_json, created_at_ms
		from wxai_inspection_logs where run_id = ? order by created_at_ms asc, id asc`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]model.WxaiInspectionLog, 0)
	for rows.Next() {
		entry, scanErr := scanLog(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

func (repository *repository) ListAccountStatusItems(ctx context.Context, runID int64) ([]model.WxaiAccountStatusItem, error) {
	rows, err := repository.db.QueryContext(ctx, `select
		r.id, r.run_id, r.account_key, r.file_name, r.display_account, r.auth_index, r.account_id,
		r.provider, r.disabled, r.status, r.state, r.action, r.action_reason, r.action_status,
		r.executed_action, r.action_error, r.status_code, r.used_percent, r.is_quota, r.error, r.plan_type, r.quota_windows_json,
		r.monthly_limit_cents, r.monthly_used_cents, r.error_kind, r.error_detail, r.created_at_ms,
		d.priority, d.schedule_group, d.account_type, d.weekly_used_percent, d.weekly_reset_at_ms,
		d.monthly_used_percent, d.monthly_reset_at_ms, d.checked_at_ms
	from wxai_inspection_results r
	left join wxai_account_status_details d on d.run_id = r.run_id and d.account_key = r.account_key
	where r.run_id = ? order by r.file_name asc, r.display_account asc, r.id asc`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.WxaiAccountStatusItem, 0)
	for rows.Next() {
		item, scanErr := scanAccountStatusItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repository *repository) GetSettings(ctx context.Context) (model.ManagerWxaiInspectionConfig, bool, error) {
	var raw string
	err := repository.db.QueryRowContext(ctx, `select settings_json from wxai_inspection_settings where id = 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ManagerWxaiInspectionConfig{}, false, nil
	}
	if err != nil {
		return model.ManagerWxaiInspectionConfig{}, false, err
	}
	return model.UnmarshalWxaiInspectionSettings(raw), true, nil
}

func (repository *repository) SaveSettings(ctx context.Context, settings model.ManagerWxaiInspectionConfig) (model.ManagerWxaiInspectionConfig, error) {
	settings = model.NormalizeWxaiInspectionConfig(settings, model.DefaultWxaiInspectionConfig())
	now := time.Now().UnixMilli()
	_, err := repository.db.ExecContext(ctx, `insert into wxai_inspection_settings (id, settings_json, created_at_ms, updated_at_ms)
		values (1, ?, ?, ?) on conflict(id) do update set settings_json = excluded.settings_json, updated_at_ms = excluded.updated_at_ms`,
		model.MarshalWxaiInspectionSettings(settings), now, now)
	return settings, err
}

func runSelectSQL() string {
	return `select id, trigger_type, trigger_key, status, started_at_ms, finished_at_ms,
		total_files, probe_set_count, sampled_count, disabled_count, enabled_count,
		delete_count, disable_count, enable_count, reauth_count, keep_count,
		quota_exhausted_count, abnormal_count, error, settings_json, created_at_ms, updated_at_ms
	from wxai_inspection_runs`
}

func resultSelectSQL() string {
	return `select id, run_id, account_key, file_name, display_account, auth_index, account_id,
		provider, disabled, status, state, action, action_reason, action_status, executed_action,
		action_error, status_code, used_percent, is_quota, error, plan_type, quota_windows_json,
		monthly_limit_cents, monthly_used_cents, error_kind, error_detail, created_at_ms from wxai_inspection_results`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRun(row scanner) (model.WxaiInspectionRun, error) {
	var run model.WxaiInspectionRun
	var triggerKey, errorText sql.NullString
	var finishedAt sql.NullInt64
	err := row.Scan(&run.ID, &run.TriggerType, &triggerKey, &run.Status, &run.StartedAtMS, &finishedAt,
		&run.TotalFiles, &run.ProbeSetCount, &run.SampledCount, &run.DisabledCount, &run.EnabledCount,
		&run.DeleteCount, &run.DisableCount, &run.EnableCount, &run.ReauthCount, &run.KeepCount,
		&run.QuotaExhaustedCount, &run.AbnormalCount,
		&errorText, &run.SettingsJSON, &run.CreatedAtMS, &run.UpdatedAtMS)
	if err != nil {
		return model.WxaiInspectionRun{}, err
	}
	run.TriggerKey = triggerKey.String
	run.Error = errorText.String
	if finishedAt.Valid {
		run.FinishedAtMS = finishedAt.Int64
	}
	run.Settings = model.UnmarshalWxaiInspectionSettings(run.SettingsJSON)
	return run, nil
}

func scanResult(row scanner) (model.WxaiInspectionResult, error) {
	var result model.WxaiInspectionResult
	var authIndex, accountID, status, state, actionReason, actionStatus sql.NullString
	var executedAction, actionError, errorText, planType, quotaWindowsJSON, errorKind, errorDetail sql.NullString
	var statusCode sql.NullInt64
	var usedPercent, monthlyLimitCents, monthlyUsedCents sql.NullFloat64
	var disabled, isQuota int
	err := row.Scan(&result.ID, &result.RunID, &result.AccountKey, &result.FileName, &result.DisplayAccount,
		&authIndex, &accountID, &result.Provider, &disabled, &status, &state, &result.Action, &actionReason,
		&actionStatus, &executedAction, &actionError, &statusCode, &usedPercent, &isQuota, &errorText, &planType,
		&quotaWindowsJSON, &monthlyLimitCents, &monthlyUsedCents, &errorKind, &errorDetail, &result.CreatedAtMS)
	if err != nil {
		return model.WxaiInspectionResult{}, err
	}
	result.AuthIndex = authIndex.String
	result.AccountID = accountID.String
	result.Disabled = disabled != 0
	result.Status = status.String
	result.State = state.String
	result.ActionReason = actionReason.String
	result.ActionStatus = model.NormalizeWxaiInspectionActionStatus(actionStatus.String, result.Action)
	result.ExecutedAction = executedAction.String
	result.ActionError = actionError.String
	result.IsQuota = isQuota != 0
	result.Error = errorText.String
	result.PlanType = planType.String
	result.QuotaWindowsJSON = quotaWindowsJSON.String
	result.QuotaWindows = model.UnmarshalWxaiInspectionQuotaWindows(result.QuotaWindowsJSON)
	result.ErrorKind = errorKind.String
	result.ErrorDetail = errorDetail.String
	if statusCode.Valid {
		value := int(statusCode.Int64)
		result.StatusCode = &value
	}
	if usedPercent.Valid {
		value := usedPercent.Float64
		result.UsedPercent = &value
	}
	if monthlyLimitCents.Valid {
		value := monthlyLimitCents.Float64
		result.MonthlyLimitCents = &value
	}
	if monthlyUsedCents.Valid {
		value := monthlyUsedCents.Float64
		result.MonthlyUsedCents = &value
	}
	return result, nil
}

func scanLog(row scanner) (model.WxaiInspectionLog, error) {
	var entry model.WxaiInspectionLog
	var detailJSON sql.NullString
	if err := row.Scan(&entry.ID, &entry.RunID, &entry.Level, &entry.Message, &detailJSON, &entry.CreatedAtMS); err != nil {
		return model.WxaiInspectionLog{}, err
	}
	entry.DetailJSON = detailJSON.String
	if strings.TrimSpace(entry.DetailJSON) != "" {
		_ = json.Unmarshal([]byte(entry.DetailJSON), &entry.Detail)
	}
	return entry, nil
}

func scanAccountStatusItem(row scanner) (model.WxaiAccountStatusItem, error) {
	var item model.WxaiAccountStatusItem
	var authIndex, accountID, status, state, actionReason, actionStatus sql.NullString
	var executedAction, actionError, errorText, planType, quotaWindowsJSON, errorKind, errorDetail, accountType sql.NullString
	var statusCode, priority, scheduleGroup, weeklyResetAt, monthlyResetAt, checkedAt sql.NullInt64
	var usedPercent, monthlyLimitCents, monthlyUsedCents, weeklyUsedPercent, monthlyUsedPercent sql.NullFloat64
	var disabled, isQuota int
	err := row.Scan(&item.ID, &item.RunID, &item.AccountKey, &item.FileName, &item.DisplayAccount,
		&authIndex, &accountID, &item.Provider, &disabled, &status, &state, &item.Action, &actionReason,
		&actionStatus, &executedAction, &actionError, &statusCode, &usedPercent, &isQuota, &errorText, &planType,
		&quotaWindowsJSON, &monthlyLimitCents, &monthlyUsedCents, &errorKind, &errorDetail, &item.ResultCreatedAtMS,
		&priority, &scheduleGroup, &accountType, &weeklyUsedPercent, &weeklyResetAt, &monthlyUsedPercent, &monthlyResetAt, &checkedAt)
	if err != nil {
		return model.WxaiAccountStatusItem{}, err
	}
	item.CreatedAtMS = item.ResultCreatedAtMS
	item.AuthIndex = authIndex.String
	item.AccountID = accountID.String
	item.Disabled = disabled != 0
	item.Status = status.String
	item.State = state.String
	item.ActionReason = actionReason.String
	item.ActionStatus = model.NormalizeWxaiInspectionActionStatus(actionStatus.String, item.Action)
	item.ExecutedAction = executedAction.String
	item.ActionError = actionError.String
	item.IsQuota = isQuota != 0
	item.Error = errorText.String
	item.PlanType = planType.String
	item.QuotaWindowsJSON = quotaWindowsJSON.String
	item.QuotaWindows = model.UnmarshalWxaiInspectionQuotaWindows(item.QuotaWindowsJSON)
	item.ErrorKind = errorKind.String
	item.ErrorDetail = errorDetail.String
	item.AccountType = accountType.String
	if statusCode.Valid {
		value := int(statusCode.Int64)
		item.StatusCode = &value
	}
	if usedPercent.Valid {
		value := usedPercent.Float64
		item.UsedPercent = &value
	}
	if monthlyLimitCents.Valid {
		value := monthlyLimitCents.Float64
		item.MonthlyLimitCents = &value
	}
	if monthlyUsedCents.Valid {
		value := monthlyUsedCents.Float64
		item.MonthlyUsedCents = &value
	}
	if priority.Valid {
		value := int(priority.Int64)
		item.Priority = &value
	}
	if scheduleGroup.Valid {
		value := int(scheduleGroup.Int64)
		item.ScheduleGroup = &value
	}
	if weeklyUsedPercent.Valid {
		value := weeklyUsedPercent.Float64
		item.WeeklyUsedPercent = &value
	}
	if monthlyUsedPercent.Valid {
		value := monthlyUsedPercent.Float64
		item.MonthlyUsedPercent = &value
	}
	if weeklyResetAt.Valid {
		item.WeeklyResetAtMS = weeklyResetAt.Int64
	}
	if monthlyResetAt.Valid {
		item.MonthlyResetAtMS = monthlyResetAt.Int64
	}
	if checkedAt.Valid {
		item.CheckedAtMS = checkedAt.Int64
	}
	return item, nil
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullablePositiveInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
