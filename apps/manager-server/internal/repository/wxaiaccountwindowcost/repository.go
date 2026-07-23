package wxaiaccountwindowcost

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

const compatibleCachedTokensExpression = "max(max(cached_tokens, cache_tokens) - max(cache_read_tokens, 0) - max(cache_creation_tokens, 0), 0)"

type Repository interface {
	ListByRun(ctx context.Context, runID int64, nowMS int64) ([]model.WxaiAccountWindowCost, error)
	SumUsageByWindow(ctx context.Context, target model.WxaiAccountWindowCostTarget, fromMS int64, toMS int64) ([]model.WxaiAccountWindowUsageAggregate, error)
	Upsert(ctx context.Context, cost model.WxaiAccountWindowCost) error
}

type repository struct {
	db *sql.DB
}

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (repository *repository) ListByRun(ctx context.Context, runID int64, nowMS int64) ([]model.WxaiAccountWindowCost, error) {
	rows, err := repository.db.QueryContext(
		ctx,
		`select distinct
			cost.account_key, cost.window_type, cost.window_start_at_ms, cost.window_reset_at_ms,
			cost.estimated_cost, cost.input_tokens, cost.output_tokens, cost.cached_tokens,
			cost.is_quota_exhausted, cost.calculated_at_ms, cost.created_at_ms, cost.updated_at_ms
		from wxai_account_status_details detail
		join wxai_account_window_costs cost on cost.account_key = detail.account_key
		where detail.run_id = ? and (
			(cost.window_type = 'weekly' and cost.window_reset_at_ms = detail.weekly_reset_at_ms and cost.window_reset_at_ms > ?) or
			(cost.window_type = 'monthly' and cost.window_reset_at_ms = detail.monthly_reset_at_ms and cost.window_reset_at_ms > ?) or
			(cost.window_type = 'priority_cycle' and
				coalesce(detail.weekly_reset_at_ms, 0) <= ? and
				coalesce(detail.monthly_reset_at_ms, 0) <= ?)
		)
		order by cost.account_key asc,
			case cost.window_type when 'weekly' then 1 when 'monthly' then 2 else 9 end`,
		runID,
		nowMS,
		nowMS,
		nowMS,
		nowMS,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	costs := make([]model.WxaiAccountWindowCost, 0)
	for rows.Next() {
		cost, scanErr := scanCost(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		costs = append(costs, cost)
	}
	return costs, rows.Err()
}

func (repository *repository) SumUsageByWindow(
	ctx context.Context,
	target model.WxaiAccountWindowCostTarget,
	fromMS int64,
	toMS int64,
) ([]model.WxaiAccountWindowUsageAggregate, error) {
	authIndex := strings.TrimSpace(target.AuthIndex)
	if authIndex == "" || fromMS < 0 || toMS <= fromMS {
		return []model.WxaiAccountWindowUsageAggregate{}, nil
	}

	rows, err := repository.db.QueryContext(
		ctx,
		`select
			coalesce(nullif(resolved_model, ''), model) as billing_model,
			coalesce(service_tier, '') as service_tier,
			coalesce(sum(input_tokens), 0),
			coalesce(sum(output_tokens), 0),
			coalesce(sum(`+compatibleCachedTokensExpression+`), 0),
			coalesce(sum(cache_read_tokens), 0),
			coalesce(sum(cache_creation_tokens), 0)
		from usage_events
		where timestamp_ms >= ? and timestamp_ms < ?
			and provider = 'xai' and auth_index = ?
		group by billing_model, service_tier`,
		fromMS,
		toMS,
		authIndex,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	aggregates := make([]model.WxaiAccountWindowUsageAggregate, 0)
	for rows.Next() {
		var aggregate model.WxaiAccountWindowUsageAggregate
		if scanErr := rows.Scan(
			&aggregate.Model,
			&aggregate.ServiceTier,
			&aggregate.InputTokens,
			&aggregate.OutputTokens,
			&aggregate.CachedTokens,
			&aggregate.CacheReadTokens,
			&aggregate.CacheCreationTokens,
		); scanErr != nil {
			return nil, scanErr
		}
		aggregates = append(aggregates, aggregate)
	}
	return aggregates, rows.Err()
}

func (repository *repository) Upsert(ctx context.Context, cost model.WxaiAccountWindowCost) error {
	nowMS := time.Now().UnixMilli()
	if cost.CalculatedAtMS <= 0 {
		cost.CalculatedAtMS = nowMS
	}
	if cost.CreatedAtMS <= 0 {
		cost.CreatedAtMS = nowMS
	}
	if cost.UpdatedAtMS <= 0 {
		cost.UpdatedAtMS = nowMS
	}

	if cost.WindowType != model.WxaiAccountWindowTypePriorityCycle {
		return upsertWxaiAccountWindowCost(ctx, repository.db, cost)
	}

	transaction, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(
		ctx,
		`delete from wxai_account_window_costs where account_key = ? and window_type = ?`,
		cost.AccountKey,
		cost.WindowType,
	); err != nil {
		return err
	}
	if err := upsertWxaiAccountWindowCost(ctx, transaction, cost); err != nil {
		return err
	}
	return transaction.Commit()
}

type wxaiAccountWindowCostExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func upsertWxaiAccountWindowCost(
	ctx context.Context,
	executor wxaiAccountWindowCostExecutor,
	cost model.WxaiAccountWindowCost,
) error {
	quotaExhausted := 0
	if cost.IsQuotaExhausted {
		quotaExhausted = 1
	}
	_, err := executor.ExecContext(
		ctx,
		`insert into wxai_account_window_costs (
			account_key, window_type, window_start_at_ms, window_reset_at_ms,
			estimated_cost, input_tokens, output_tokens, cached_tokens,
			is_quota_exhausted, calculated_at_ms, created_at_ms, updated_at_ms
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(account_key, window_type, window_reset_at_ms) do update set
			window_start_at_ms = excluded.window_start_at_ms,
			estimated_cost = excluded.estimated_cost,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cached_tokens = excluded.cached_tokens,
			is_quota_exhausted = excluded.is_quota_exhausted,
			calculated_at_ms = excluded.calculated_at_ms,
			updated_at_ms = excluded.updated_at_ms`,
		cost.AccountKey,
		cost.WindowType,
		cost.WindowStartAtMS,
		cost.WindowResetAtMS,
		cost.EstimatedCost,
		cost.InputTokens,
		cost.OutputTokens,
		cost.CachedTokens,
		quotaExhausted,
		cost.CalculatedAtMS,
		cost.CreatedAtMS,
		cost.UpdatedAtMS,
	)
	return err
}

func scanCost(row interface{ Scan(dest ...any) error }) (model.WxaiAccountWindowCost, error) {
	var cost model.WxaiAccountWindowCost
	var quotaExhausted int
	if err := row.Scan(
		&cost.AccountKey,
		&cost.WindowType,
		&cost.WindowStartAtMS,
		&cost.WindowResetAtMS,
		&cost.EstimatedCost,
		&cost.InputTokens,
		&cost.OutputTokens,
		&cost.CachedTokens,
		&quotaExhausted,
		&cost.CalculatedAtMS,
		&cost.CreatedAtMS,
		&cost.UpdatedAtMS,
	); err != nil {
		return model.WxaiAccountWindowCost{}, err
	}
	cost.IsQuotaExhausted = quotaExhausted != 0
	return cost, nil
}
