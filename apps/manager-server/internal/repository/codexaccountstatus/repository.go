package codexaccountstatus

import (
	"context"
	"database/sql"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	UpsertDetail(ctx context.Context, detail model.CodexAccountStatusDetail) error
	ListItemsByRun(ctx context.Context, runID int64) ([]model.CodexAccountStatusItem, error)
}

type repository struct {
	db *sql.DB
}

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (r *repository) UpsertDetail(ctx context.Context, detail model.CodexAccountStatusDetail) error {
	now := time.Now().UnixMilli()
	if detail.CreatedAtMS <= 0 {
		detail.CreatedAtMS = now
	}
	if detail.UpdatedAtMS <= 0 {
		detail.UpdatedAtMS = now
	}
	_, err := r.db.ExecContext(
		ctx,
		`insert into codex_account_status_details (
			run_id, account_key, priority, account_type,
			five_hour_used_percent, five_hour_reset_at_ms,
			weekly_used_percent, weekly_reset_at_ms,
			monthly_used_percent, monthly_reset_at_ms,
			rate_limit_reset_credits_available_count, checked_at_ms,
			created_at_ms, updated_at_ms
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(run_id, account_key) do update set
			priority = excluded.priority,
			account_type = excluded.account_type,
			five_hour_used_percent = excluded.five_hour_used_percent,
			five_hour_reset_at_ms = excluded.five_hour_reset_at_ms,
			weekly_used_percent = excluded.weekly_used_percent,
			weekly_reset_at_ms = excluded.weekly_reset_at_ms,
			monthly_used_percent = excluded.monthly_used_percent,
			monthly_reset_at_ms = excluded.monthly_reset_at_ms,
			rate_limit_reset_credits_available_count = excluded.rate_limit_reset_credits_available_count,
			checked_at_ms = excluded.checked_at_ms,
			updated_at_ms = excluded.updated_at_ms`,
		detail.RunID,
		detail.AccountKey,
		nullInt(detail.Priority),
		nullString(detail.AccountType),
		nullFloat(detail.FiveHourUsedPercent),
		nullPositiveInt64(detail.FiveHourResetAtMS),
		nullFloat(detail.WeeklyUsedPercent),
		nullPositiveInt64(detail.WeeklyResetAtMS),
		nullFloat(detail.MonthlyUsedPercent),
		nullPositiveInt64(detail.MonthlyResetAtMS),
		nullInt(detail.RateLimitResetCreditsAvailableCount),
		nullPositiveInt64(detail.CheckedAtMS),
		detail.CreatedAtMS,
		detail.UpdatedAtMS,
	)
	return err
}

func (r *repository) ListItemsByRun(ctx context.Context, runID int64) ([]model.CodexAccountStatusItem, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`select
			r.id, r.run_id, r.account_key, r.file_name, r.display_account, r.auth_index, r.account_id,
			r.provider, r.disabled, r.status, r.state, r.action, r.action_reason, r.status_code,
			r.used_percent, r.is_quota, r.error, r.action_status, r.executed_action, r.action_error,
			r.created_at_ms,
			d.priority, d.account_type, d.five_hour_used_percent, d.five_hour_reset_at_ms,
			d.weekly_used_percent, d.weekly_reset_at_ms,
			d.monthly_used_percent, d.monthly_reset_at_ms,
			d.rate_limit_reset_credits_available_count, d.checked_at_ms
		from codex_inspection_results r
		left join codex_account_status_details d on d.run_id = r.run_id and d.account_key = r.account_key
		where r.run_id = ?
		order by r.file_name asc, r.display_account asc, r.id asc`,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CodexAccountStatusItem, 0)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanItem(row interface{ Scan(dest ...any) error }) (model.CodexAccountStatusItem, error) {
	var item model.CodexAccountStatusItem
	var authIndex, accountID, provider, status, state, actionReason, errorText sql.NullString
	var actionStatus, executedAction, actionError, accountType sql.NullString
	var statusCode, priority, fiveHourResetAt, weeklyResetAt, monthlyResetAt, resetCredits, checkedAt sql.NullInt64
	var usedPercent, fiveHourUsedPercent, weeklyUsedPercent, monthlyUsedPercent sql.NullFloat64
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
		&priority,
		&accountType,
		&fiveHourUsedPercent,
		&fiveHourResetAt,
		&weeklyUsedPercent,
		&weeklyResetAt,
		&monthlyUsedPercent,
		&monthlyResetAt,
		&resetCredits,
		&checkedAt,
	); err != nil {
		return model.CodexAccountStatusItem{}, err
	}
	item.AuthIndex = authIndex.String
	item.AccountID = accountID.String
	item.Provider = provider.String
	item.Disabled = disabled != 0
	item.Status = status.String
	item.State = state.String
	item.ActionReason = actionReason.String
	item.IsQuota = isQuota != 0
	item.Error = errorText.String
	item.ActionStatus = model.NormalizeCodexInspectionActionStatus(actionStatus.String, item.Action)
	item.ExecutedAction = executedAction.String
	item.ActionError = actionError.String
	if priority.Valid {
		value := int(priority.Int64)
		item.Priority = &value
	}
	item.AccountType = accountType.String
	if statusCode.Valid {
		value := int(statusCode.Int64)
		item.StatusCode = &value
	}
	if usedPercent.Valid {
		value := usedPercent.Float64
		item.UsedPercent = &value
	}
	if fiveHourUsedPercent.Valid {
		value := fiveHourUsedPercent.Float64
		item.FiveHourUsedPercent = &value
	}
	if fiveHourResetAt.Valid {
		item.FiveHourResetAtMS = fiveHourResetAt.Int64
	}
	if weeklyUsedPercent.Valid {
		value := weeklyUsedPercent.Float64
		item.WeeklyUsedPercent = &value
	}
	if weeklyResetAt.Valid {
		item.WeeklyResetAtMS = weeklyResetAt.Int64
	}
	if monthlyUsedPercent.Valid {
		value := monthlyUsedPercent.Float64
		item.MonthlyUsedPercent = &value
	}
	if monthlyResetAt.Valid {
		item.MonthlyResetAtMS = monthlyResetAt.Int64
	}
	if resetCredits.Valid {
		value := int(resetCredits.Int64)
		item.RateLimitResetCreditsAvailableCount = &value
	}
	if checkedAt.Valid {
		item.CheckedAtMS = checkedAt.Int64
	}
	return item, nil
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
