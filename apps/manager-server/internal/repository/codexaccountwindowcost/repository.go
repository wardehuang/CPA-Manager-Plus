package codexaccountwindowcost

import (
	"context"
	"database/sql"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	ListByRun(ctx context.Context, runID int64) ([]model.CodexAccountWindowCost, error)
	SumUsageByWindow(ctx context.Context, target model.CodexAccountWindowCostTarget, fromMS int64, toMS int64) ([]model.CodexAccountWindowUsageAggregate, error)
	Upsert(ctx context.Context, cost model.CodexAccountWindowCost) error
}

type repository struct {
	db *sql.DB
}

const compatCachedExpr = "max(max(cached_tokens, cache_tokens) - max(cache_read_tokens, 0) - max(cache_creation_tokens, 0), 0)"

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Upsert(ctx context.Context, cost model.CodexAccountWindowCost) error {
	now := time.Now().UnixMilli()
	if cost.CalculatedAtMS <= 0 {
		cost.CalculatedAtMS = now
	}
	if cost.CreatedAtMS <= 0 {
		cost.CreatedAtMS = now
	}
	if cost.UpdatedAtMS <= 0 {
		cost.UpdatedAtMS = now
	}
	exhausted := 0
	if cost.IsQuotaExhausted {
		exhausted = 1
	}
	_, err := r.db.ExecContext(
		ctx,
		`insert into codex_account_window_costs (
			account_key, window_type, window_start_at_ms, window_reset_at_ms,
			estimated_cost, is_quota_exhausted, calculated_at_ms, created_at_ms, updated_at_ms
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(account_key, window_type, window_reset_at_ms) do update set
			window_start_at_ms = excluded.window_start_at_ms,
			estimated_cost = excluded.estimated_cost,
			is_quota_exhausted = excluded.is_quota_exhausted,
			calculated_at_ms = excluded.calculated_at_ms,
			updated_at_ms = excluded.updated_at_ms`,
		cost.AccountKey,
		cost.WindowType,
		cost.WindowStartAtMS,
		cost.WindowResetAtMS,
		cost.EstimatedCost,
		exhausted,
		cost.CalculatedAtMS,
		cost.CreatedAtMS,
		cost.UpdatedAtMS,
	)
	return err
}

func (r *repository) ListByRun(ctx context.Context, runID int64) ([]model.CodexAccountWindowCost, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`select distinct
			c.account_key, c.window_type, c.window_start_at_ms, c.window_reset_at_ms,
			c.estimated_cost, c.is_quota_exhausted, c.calculated_at_ms, c.created_at_ms, c.updated_at_ms
		from codex_account_status_details d
		join codex_account_window_costs c on c.account_key = d.account_key and (
			(c.window_type = 'five_hour' and c.window_reset_at_ms = d.five_hour_reset_at_ms) or
			(c.window_type = 'weekly' and c.window_reset_at_ms = d.weekly_reset_at_ms) or
			(c.window_type = 'monthly' and c.window_reset_at_ms = d.monthly_reset_at_ms)
		)
		where d.run_id = ?
		order by c.account_key asc,
			case c.window_type when 'five_hour' then 1 when 'weekly' then 2 when 'monthly' then 3 else 9 end`,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CodexAccountWindowCost, 0)
	for rows.Next() {
		item, err := scanCost(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *repository) SumUsageByWindow(ctx context.Context, target model.CodexAccountWindowCostTarget, fromMS int64, toMS int64) ([]model.CodexAccountWindowUsageAggregate, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`select
			coalesce(nullif(resolved_model, ''), model) as billing_model,
			coalesce(service_tier, '') as service_tier,
			coalesce(sum(input_tokens), 0),
			coalesce(sum(output_tokens), 0),
			coalesce(sum(`+compatCachedExpr+`), 0),
			coalesce(sum(cache_read_tokens), 0),
			coalesce(sum(cache_creation_tokens), 0)
		from usage_events
		where timestamp_ms >= ? and timestamp_ms < ? and (
			(? <> '' and auth_index = ?) or
			(? <> '' and account_snapshot = ?) or
			(? <> '' and auth_label_snapshot = ?) or
			(? <> '' and auth_file_snapshot = ?)
		)
		group by billing_model, service_tier`,
		fromMS,
		toMS,
		target.AuthIndex, target.AuthIndex,
		target.DisplayAccount, target.DisplayAccount,
		target.DisplayAccount, target.DisplayAccount,
		target.FileName, target.FileName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CodexAccountWindowUsageAggregate, 0)
	for rows.Next() {
		var item model.CodexAccountWindowUsageAggregate
		if err := rows.Scan(
			&item.Model,
			&item.ServiceTier,
			&item.InputTokens,
			&item.OutputTokens,
			&item.CachedTokens,
			&item.CacheReadTokens,
			&item.CacheCreationTokens,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanCost(row interface{ Scan(dest ...any) error }) (model.CodexAccountWindowCost, error) {
	var item model.CodexAccountWindowCost
	var exhausted int
	if err := row.Scan(
		&item.AccountKey,
		&item.WindowType,
		&item.WindowStartAtMS,
		&item.WindowResetAtMS,
		&item.EstimatedCost,
		&exhausted,
		&item.CalculatedAtMS,
		&item.CreatedAtMS,
		&item.UpdatedAtMS,
	); err != nil {
		return model.CodexAccountWindowCost{}, err
	}
	item.IsQuotaExhausted = exhausted != 0
	return item, nil
}
