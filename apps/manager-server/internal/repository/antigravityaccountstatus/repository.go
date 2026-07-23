package antigravityaccountstatus

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	UpsertDetail(ctx context.Context, detail model.AntigravityAccountStatusDetail) error
	ListItemsByRun(ctx context.Context, runID int64, targetProvider string) ([]model.AntigravityAccountStatusItem, error)
	ListItemsByRunWithDetailProvider(ctx context.Context, runID int64, resultProvider string, detailProvider string) ([]model.AntigravityAccountStatusItem, error)
}

type repository struct {
	db *sql.DB
}

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (r *repository) UpsertDetail(ctx context.Context, detail model.AntigravityAccountStatusDetail) error {
	now := time.Now().UnixMilli()
	if detail.CreatedAtMS <= 0 {
		detail.CreatedAtMS = now
	}
	if detail.UpdatedAtMS <= 0 {
		detail.UpdatedAtMS = now
	}
	detail.TargetProvider = model.NormalizeAntigravityTargetProvider(detail.TargetProvider, model.AntigravityTargetProviderClaude)
	_, err := r.db.ExecContext(
		ctx,
		`insert into antigravity_account_status_details (
			run_id, account_key, target_provider, priority, account_type,
			used_percent, reset_at_ms, checked_at_ms, created_at_ms, updated_at_ms
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(run_id, account_key, target_provider) do update set
			priority = excluded.priority,
			account_type = excluded.account_type,
			used_percent = excluded.used_percent,
			reset_at_ms = excluded.reset_at_ms,
			checked_at_ms = excluded.checked_at_ms,
			updated_at_ms = excluded.updated_at_ms`,
		detail.RunID,
		detail.AccountKey,
		detail.TargetProvider,
		nullInt(detail.Priority),
		nullString(detail.AccountType),
		nullFloat(detail.UsedPercent),
		nullPositiveInt64(detail.ResetAtMS),
		nullPositiveInt64(detail.CheckedAtMS),
		detail.CreatedAtMS,
		detail.UpdatedAtMS,
	)
	return err
}

func (r *repository) ListItemsByRun(ctx context.Context, runID int64, targetProvider string) ([]model.AntigravityAccountStatusItem, error) {
	return r.ListItemsByRunWithDetailProvider(ctx, runID, targetProvider, targetProvider)
}

func (r *repository) ListItemsByRunWithDetailProvider(ctx context.Context, runID int64, resultProvider string, detailProvider string) ([]model.AntigravityAccountStatusItem, error) {
	resultProvider = model.NormalizeAntigravityTargetProvider(resultProvider, model.AntigravityTargetProviderClaude)
	detailProvider = model.NormalizeAntigravityTargetProvider(detailProvider, resultProvider)
	rows, err := r.db.QueryContext(
		ctx,
		`select
			r.id, r.run_id, r.account_key, r.file_name, r.display_account, r.auth_index, r.account_id,
			r.provider, r.target_provider, r.disabled, r.status, r.state, r.action, r.action_reason,
			r.status_code, r.used_percent, r.is_quota, r.error, r.action_status, r.executed_action,
			r.action_error, r.created_at_ms, r.quota_windows_json,
			d.priority, d.account_type, d.used_percent, d.reset_at_ms, d.checked_at_ms
		from antigravity_inspection_results r
		left join antigravity_account_status_details d
			on d.run_id = r.run_id and d.account_key = r.account_key and d.target_provider = ?
		where r.run_id = ? and r.target_provider = ?
		order by r.file_name asc, r.display_account asc, r.id asc`,
		detailProvider,
		runID,
		resultProvider,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AntigravityAccountStatusItem, 0)
	for rows.Next() {
		item, err := scanItem(rows, detailProvider)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanItem(row interface{ Scan(dest ...any) error }, detailProvider string) (model.AntigravityAccountStatusItem, error) {
	var item model.AntigravityAccountStatusItem
	var authIndex, accountID, provider, targetProvider, status, state, actionReason, errorText sql.NullString
	var actionStatus, executedAction, actionError, accountType, quotaWindowsJSON sql.NullString
	var statusCode, priority, resetAt, checkedAt sql.NullInt64
	var usedPercent, detailUsedPercent sql.NullFloat64
	var disabled, isQuota int
	if err := row.Scan(
		&item.ID,
		&item.RunID,
		&item.AccountKey,
		&item.FileName,
		&item.DisplayAccount,
		&authIndex,
		&accountID,
		&provider,
		&targetProvider,
		&disabled,
		&status,
		&state,
		&item.Action,
		&actionReason,
		&statusCode,
		&usedPercent,
		&isQuota,
		&errorText,
		&actionStatus,
		&executedAction,
		&actionError,
		&item.ResultCreatedAtMS,
		&quotaWindowsJSON,
		&priority,
		&accountType,
		&detailUsedPercent,
		&resetAt,
		&checkedAt,
	); err != nil {
		return model.AntigravityAccountStatusItem{}, err
	}
	item.AuthIndex = authIndex.String
	item.AccountID = accountID.String
	item.Provider = provider.String
	item.TargetProvider = model.NormalizeAntigravityTargetProvider(targetProvider.String, model.AntigravityTargetProviderClaude)
	item.Disabled = disabled != 0
	item.Status = status.String
	item.State = state.String
	item.ActionReason = actionReason.String
	item.IsQuota = isQuota != 0
	item.Error = errorText.String
	item.ActionStatus = model.NormalizeAntigravityInspectionActionStatus(actionStatus.String, item.Action)
	item.ExecutedAction = executedAction.String
	item.ActionError = actionError.String
	item.AccountType = accountType.String
	if statusCode.Valid {
		value := int(statusCode.Int64)
		item.StatusCode = &value
	}
	if priority.Valid {
		value := int(priority.Int64)
		item.Priority = &value
	}
	if detailUsedPercent.Valid {
		value := detailUsedPercent.Float64
		item.UsedPercent = &value
	} else if usedPercent.Valid {
		value := usedPercent.Float64
		item.UsedPercent = &value
	}
	if resetAt.Valid {
		item.ResetAtMS = resetAt.Int64
	}
	if checkedAt.Valid {
		item.CheckedAtMS = checkedAt.Int64
	}
	if quotaWindowsJSON.Valid && strings.TrimSpace(quotaWindowsJSON.String) != "" {
		if windows := model.UnmarshalAntigravityInspectionQuotaWindows(quotaWindowsJSON.String); len(windows) > 0 {
			providerWindows := model.FilterAntigravityQuotaWindows(windows, detailProvider)
			item.QuotaWindows = providerWindows
			applyAntigravityQuotaWindowsToStatusItem(&item, providerWindows)
		}
	}
	if detailProvider != model.AntigravityTargetProviderServer && len(item.QuotaWindows) == 0 {
		item.UsedPercent = nil
		item.ResetAtMS = 0
		if item.Priority != nil && *item.Priority == -1 {
			usedPercent := float64(100)
			item.UsedPercent = &usedPercent
		}
	}
	return item, nil
}

func applyAntigravityQuotaWindowsToStatusItem(item *model.AntigravityAccountStatusItem, windows []model.AntigravityInspectionQuotaWindow) {
	for _, window := range windows {
		id := strings.ToLower(strings.TrimSpace(window.ID))
		label := strings.ToLower(strings.TrimSpace(window.LabelKey + " " + window.ResetLabel))
		target := id + " " + label
		switch {
		case strings.Contains(target, "five") || strings.Contains(target, "hour"):
			item.FiveHourUsedPercent = window.UsedPercent
			item.FiveHourResetAtMS = window.ResetAtMS
		case strings.Contains(target, "month") || strings.Contains(target, "monthly") || strings.Contains(target, "gemini"):
			item.MonthlyUsedPercent = window.UsedPercent
			item.MonthlyResetAtMS = window.ResetAtMS
		case strings.Contains(target, "week") || strings.Contains(target, "weekly") || strings.Contains(target, "claude"):
			item.WeeklyUsedPercent = window.UsedPercent
			item.WeeklyResetAtMS = window.ResetAtMS
		default:
			if item.WeeklyUsedPercent == nil {
				item.WeeklyUsedPercent = window.UsedPercent
				item.WeeklyResetAtMS = window.ResetAtMS
			}
		}
	}
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullPositiveInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
