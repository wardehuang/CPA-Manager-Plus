package wxairealtimedegradation

import (
	"context"
	"database/sql"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	Delete(ctx context.Context, accountKey string) error
	Get(ctx context.Context, accountKey string) (model.WxaiRealtimeDegradationState, bool, error)
	Upsert(ctx context.Context, state model.WxaiRealtimeDegradationState) error
}

type repository struct {
	db *sql.DB
}

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (repository *repository) Delete(ctx context.Context, accountKey string) error {
	_, err := repository.db.ExecContext(ctx, `delete from wxai_realtime_degradation_states where account_key = ?`, accountKey)
	return err
}

func (repository *repository) Get(ctx context.Context, accountKey string) (model.WxaiRealtimeDegradationState, bool, error) {
	row := repository.db.QueryRowContext(ctx, `
select account_key, file_name, display_account, auth_index, account_id,
       degradation_count, cooldown_until_ms, created_at_ms, updated_at_ms
from wxai_realtime_degradation_states
where account_key = ?`, accountKey)
	state, err := scanWxaiRealtimeDegradationState(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.WxaiRealtimeDegradationState{}, false, nil
		}
		return model.WxaiRealtimeDegradationState{}, false, err
	}
	return state, true, nil
}

func (repository *repository) Upsert(ctx context.Context, state model.WxaiRealtimeDegradationState) error {
	nowMS := time.Now().UnixMilli()
	if state.CreatedAtMS <= 0 {
		state.CreatedAtMS = nowMS
	}
	if state.UpdatedAtMS <= 0 {
		state.UpdatedAtMS = nowMS
	}
	_, err := repository.db.ExecContext(ctx, `
insert into wxai_realtime_degradation_states (
    account_key, file_name, display_account, auth_index, account_id,
    degradation_count, cooldown_until_ms, created_at_ms, updated_at_ms
) values (?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(account_key) do update set
    file_name = excluded.file_name,
    display_account = excluded.display_account,
    auth_index = excluded.auth_index,
    account_id = excluded.account_id,
    degradation_count = excluded.degradation_count,
    cooldown_until_ms = excluded.cooldown_until_ms,
    updated_at_ms = excluded.updated_at_ms`,
		state.AccountKey,
		state.FileName,
		state.DisplayAccount,
		nullableString(state.AuthIndex),
		nullableString(state.AccountID),
		state.DegradationCount,
		state.CooldownUntilMS,
		state.CreatedAtMS,
		state.UpdatedAtMS,
	)
	return err
}

func scanWxaiRealtimeDegradationState(row interface{ Scan(dest ...any) error }) (model.WxaiRealtimeDegradationState, error) {
	var state model.WxaiRealtimeDegradationState
	var authIndex sql.NullString
	var accountID sql.NullString
	if err := row.Scan(
		&state.AccountKey,
		&state.FileName,
		&state.DisplayAccount,
		&authIndex,
		&accountID,
		&state.DegradationCount,
		&state.CooldownUntilMS,
		&state.CreatedAtMS,
		&state.UpdatedAtMS,
	); err != nil {
		return model.WxaiRealtimeDegradationState{}, err
	}
	state.AuthIndex = authIndex.String
	state.AccountID = accountID.String
	return state, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
