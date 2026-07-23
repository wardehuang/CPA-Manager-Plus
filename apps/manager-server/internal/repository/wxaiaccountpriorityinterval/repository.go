package wxaiaccountpriorityinterval

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	Get(ctx context.Context, accountKey string) (model.WxaiAccountPriorityInterval, bool, error)
	MarkAbnormal(ctx context.Context, accountKey string, endedAtMS int64) error
	MarkRecovered(ctx context.Context, accountKey string, startedAtMS int64) error
}

type repository struct {
	db *sql.DB
}

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (repository *repository) Get(
	ctx context.Context,
	accountKey string,
) (model.WxaiAccountPriorityInterval, bool, error) {
	var interval model.WxaiAccountPriorityInterval
	var startedAtMS sql.NullInt64
	var endedAtMS sql.NullInt64
	err := repository.db.QueryRowContext(
		ctx,
		`select account_key, started_at_ms, ended_at_ms, created_at_ms, updated_at_ms
		from wxai_account_priority_intervals where account_key = ?`,
		accountKey,
	).Scan(
		&interval.AccountKey,
		&startedAtMS,
		&endedAtMS,
		&interval.CreatedAtMS,
		&interval.UpdatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WxaiAccountPriorityInterval{}, false, nil
	}
	if err != nil {
		return model.WxaiAccountPriorityInterval{}, false, err
	}
	if startedAtMS.Valid {
		interval.StartedAtMS = &startedAtMS.Int64
	}
	if endedAtMS.Valid {
		interval.EndedAtMS = &endedAtMS.Int64
	}
	return interval, true, nil
}

func (repository *repository) MarkAbnormal(ctx context.Context, accountKey string, endedAtMS int64) error {
	nowMS := time.Now().UnixMilli()
	_, err := repository.db.ExecContext(
		ctx,
		`insert into wxai_account_priority_intervals (
			account_key, started_at_ms, ended_at_ms, created_at_ms, updated_at_ms
		) values (?, null, ?, ?, ?)
		on conflict(account_key) do update set
			ended_at_ms = excluded.ended_at_ms,
			updated_at_ms = excluded.updated_at_ms`,
		accountKey,
		endedAtMS,
		nowMS,
		nowMS,
	)
	return err
}

func (repository *repository) MarkRecovered(ctx context.Context, accountKey string, startedAtMS int64) error {
	nowMS := time.Now().UnixMilli()
	_, err := repository.db.ExecContext(
		ctx,
		`insert into wxai_account_priority_intervals (
			account_key, started_at_ms, ended_at_ms, created_at_ms, updated_at_ms
		) values (?, ?, null, ?, ?)
		on conflict(account_key) do update set
			started_at_ms = excluded.started_at_ms,
			ended_at_ms = null,
			updated_at_ms = excluded.updated_at_ms`,
		accountKey,
		startedAtMS,
		nowMS,
		nowMS,
	)
	return err
}
