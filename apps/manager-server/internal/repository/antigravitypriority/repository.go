package antigravitypriority

import (
	"context"
	"database/sql"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	Delete(ctx context.Context, accountKey string, targetProvider string) error
	Get(ctx context.Context, accountKey string, targetProvider string) (model.AntigravityPriorityAdjustment, bool, error)
	ListDue(ctx context.Context, nowMS int64) ([]model.AntigravityPriorityAdjustment, error)
	Upsert(ctx context.Context, adjustment model.AntigravityPriorityAdjustment) error
}

type repository struct {
	db *sql.DB
}

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Delete(ctx context.Context, accountKey string, targetProvider string) error {
	targetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude)
	_, err := r.db.ExecContext(ctx, `delete from antigravity_priority_adjustments where account_key = ? and target_provider = ?`, accountKey, targetProvider)
	return err
}

func (r *repository) Get(ctx context.Context, accountKey string, targetProvider string) (model.AntigravityPriorityAdjustment, bool, error) {
	targetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude)
	row := r.db.QueryRowContext(
		ctx,
		`select account_key, target_provider, file_name, display_account, auth_index, account_id,
			original_priority, recover_at_ms, created_at_ms, updated_at_ms
		from antigravity_priority_adjustments where account_key = ? and target_provider = ?`,
		accountKey,
		targetProvider,
	)
	item, err := scanPriorityAdjustment(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.AntigravityPriorityAdjustment{}, false, nil
		}
		return model.AntigravityPriorityAdjustment{}, false, err
	}
	return item, true, nil
}

func (r *repository) ListDue(ctx context.Context, nowMS int64) ([]model.AntigravityPriorityAdjustment, error) {
	if nowMS <= 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(
		ctx,
		`select account_key, target_provider, file_name, display_account, auth_index, account_id,
			original_priority, recover_at_ms, created_at_ms, updated_at_ms
		from antigravity_priority_adjustments
		where recover_at_ms > 0 and recover_at_ms <= ?
		order by recover_at_ms asc, updated_at_ms asc`,
		nowMS,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AntigravityPriorityAdjustment, 0)
	for rows.Next() {
		item, err := scanPriorityAdjustment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *repository) Upsert(ctx context.Context, adjustment model.AntigravityPriorityAdjustment) error {
	now := time.Now().UnixMilli()
	if adjustment.CreatedAtMS <= 0 {
		adjustment.CreatedAtMS = now
	}
	if adjustment.UpdatedAtMS <= 0 {
		adjustment.UpdatedAtMS = now
	}
	adjustment.TargetProvider = model.NormalizeAntigravityTargetProvider(adjustment.TargetProvider, model.AntigravityTargetProviderClaude)
	_, err := r.db.ExecContext(
		ctx,
		`insert into antigravity_priority_adjustments (
			account_key, target_provider, file_name, display_account, auth_index, account_id,
			original_priority, recover_at_ms, created_at_ms, updated_at_ms
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(account_key, target_provider) do update set
			file_name = excluded.file_name,
			display_account = excluded.display_account,
			auth_index = excluded.auth_index,
			account_id = excluded.account_id,
			recover_at_ms = excluded.recover_at_ms,
			updated_at_ms = excluded.updated_at_ms`,
		adjustment.AccountKey,
		adjustment.TargetProvider,
		adjustment.FileName,
		adjustment.DisplayAccount,
		nullString(adjustment.AuthIndex),
		nullString(adjustment.AccountID),
		nullInt(adjustment.OriginalPriority),
		nullPositiveInt64(adjustment.RecoverAtMS),
		adjustment.CreatedAtMS,
		adjustment.UpdatedAtMS,
	)
	return err
}

func scanPriorityAdjustment(row interface{ Scan(dest ...any) error }) (model.AntigravityPriorityAdjustment, error) {
	var item model.AntigravityPriorityAdjustment
	var authIndex, accountID sql.NullString
	var originalPriority, recoverAt sql.NullInt64
	if err := row.Scan(
		&item.AccountKey,
		&item.TargetProvider,
		&item.FileName,
		&item.DisplayAccount,
		&authIndex,
		&accountID,
		&originalPriority,
		&recoverAt,
		&item.CreatedAtMS,
		&item.UpdatedAtMS,
	); err != nil {
		return model.AntigravityPriorityAdjustment{}, err
	}
	item.AuthIndex = authIndex.String
	item.AccountID = accountID.String
	if originalPriority.Valid {
		value := int(originalPriority.Int64)
		item.OriginalPriority = &value
	}
	if recoverAt.Valid {
		item.RecoverAtMS = recoverAt.Int64
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
