package wxaipriorityadjustment

import (
	"context"
	"database/sql"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	Delete(ctx context.Context, accountKey string) error
	Get(ctx context.Context, accountKey string) (model.WxaiPriorityAdjustment, bool, error)
	ListDue(ctx context.Context, nowMS int64) ([]model.WxaiPriorityAdjustment, error)
	Upsert(ctx context.Context, adjustment model.WxaiPriorityAdjustment) error
}

type repository struct {
	db *sql.DB
}

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (repository *repository) Delete(ctx context.Context, accountKey string) error {
	_, err := repository.db.ExecContext(ctx, `delete from wxai_priority_adjustments where account_key = ?`, accountKey)
	return err
}

func (repository *repository) Get(ctx context.Context, accountKey string) (model.WxaiPriorityAdjustment, bool, error) {
	row := repository.db.QueryRowContext(ctx, wxaiPriorityAdjustmentSelectSQL()+` where account_key = ?`, accountKey)
	item, err := scanWxaiPriorityAdjustment(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.WxaiPriorityAdjustment{}, false, nil
		}
		return model.WxaiPriorityAdjustment{}, false, err
	}
	return item, true, nil
}

func (repository *repository) ListDue(ctx context.Context, nowMS int64) ([]model.WxaiPriorityAdjustment, error) {
	if nowMS <= 0 {
		return nil, nil
	}
	rows, err := repository.db.QueryContext(
		ctx,
		wxaiPriorityAdjustmentSelectSQL()+` where recover_at_ms > 0 and recover_at_ms <= ? order by recover_at_ms asc, updated_at_ms asc`,
		nowMS,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.WxaiPriorityAdjustment, 0)
	for rows.Next() {
		item, scanErr := scanWxaiPriorityAdjustment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repository *repository) Upsert(ctx context.Context, adjustment model.WxaiPriorityAdjustment) error {
	nowMS := time.Now().UnixMilli()
	if adjustment.CreatedAtMS <= 0 {
		adjustment.CreatedAtMS = nowMS
	}
	if adjustment.UpdatedAtMS <= 0 {
		adjustment.UpdatedAtMS = nowMS
	}
	_, err := repository.db.ExecContext(
		ctx,
		`insert into wxai_priority_adjustments (
			account_key, file_name, display_account, auth_index, account_id,
			original_priority, adjusted_priority, recover_at_ms, created_at_ms, updated_at_ms
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(account_key) do update set
			file_name = excluded.file_name,
			display_account = excluded.display_account,
			auth_index = excluded.auth_index,
			account_id = excluded.account_id,
			original_priority = case
				when excluded.original_priority is not null then excluded.original_priority
				else wxai_priority_adjustments.original_priority
			end,
			adjusted_priority = excluded.adjusted_priority,
			recover_at_ms = excluded.recover_at_ms,
			updated_at_ms = excluded.updated_at_ms`,
		adjustment.AccountKey,
		adjustment.FileName,
		adjustment.DisplayAccount,
		nullableString(adjustment.AuthIndex),
		nullableString(adjustment.AccountID),
		nullableInt(adjustment.OriginalPriority),
		adjustment.AdjustedPriority,
		nullablePositiveInt64(adjustment.RecoverAtMS),
		adjustment.CreatedAtMS,
		adjustment.UpdatedAtMS,
	)
	return err
}

func wxaiPriorityAdjustmentSelectSQL() string {
	return `select account_key, file_name, display_account, auth_index, account_id,
		original_priority, adjusted_priority, recover_at_ms, created_at_ms, updated_at_ms
	from wxai_priority_adjustments`
}

func scanWxaiPriorityAdjustment(row interface{ Scan(dest ...any) error }) (model.WxaiPriorityAdjustment, error) {
	var item model.WxaiPriorityAdjustment
	var authIndex sql.NullString
	var accountID sql.NullString
	var originalPriority sql.NullInt64
	var recoverAtMS sql.NullInt64
	if err := row.Scan(
		&item.AccountKey,
		&item.FileName,
		&item.DisplayAccount,
		&authIndex,
		&accountID,
		&originalPriority,
		&item.AdjustedPriority,
		&recoverAtMS,
		&item.CreatedAtMS,
		&item.UpdatedAtMS,
	); err != nil {
		return model.WxaiPriorityAdjustment{}, err
	}
	item.AuthIndex = authIndex.String
	item.AccountID = accountID.String
	if originalPriority.Valid {
		value := int(originalPriority.Int64)
		item.OriginalPriority = &value
	}
	if recoverAtMS.Valid {
		item.RecoverAtMS = recoverAtMS.Int64
	}
	return item, nil
}

func nullableString(value string) any {
	if value == "" {
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
