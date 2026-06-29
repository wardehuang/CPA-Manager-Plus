package antigravityaccountwindowcost

import (
	"context"
	"database/sql"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	ListByRun(ctx context.Context, runID int64, targetProvider string) ([]model.AntigravityAccountWindowCost, error)
	Upsert(ctx context.Context, cost model.AntigravityAccountWindowCost) error
}

type repository struct {
	db *sql.DB
}

func New(db *sql.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Upsert(ctx context.Context, cost model.AntigravityAccountWindowCost) error {
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
	cost.TargetProvider = model.NormalizeAntigravityTargetProvider(cost.TargetProvider, model.AntigravityTargetProviderClaude)
	exhausted := 0
	if cost.IsQuotaExhausted {
		exhausted = 1
	}
	_, err := r.db.ExecContext(
		ctx,
		`insert into antigravity_account_window_costs (
			account_key, target_provider, window_type, window_start_at_ms, window_reset_at_ms,
			estimated_cost, input_tokens, output_tokens, cached_tokens,
			is_quota_exhausted, calculated_at_ms, created_at_ms, updated_at_ms
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(account_key, target_provider, window_type, window_reset_at_ms) do update set
			window_start_at_ms = excluded.window_start_at_ms,
			estimated_cost = excluded.estimated_cost,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cached_tokens = excluded.cached_tokens,
			is_quota_exhausted = excluded.is_quota_exhausted,
			calculated_at_ms = excluded.calculated_at_ms,
			updated_at_ms = excluded.updated_at_ms`,
		cost.AccountKey,
		cost.TargetProvider,
		cost.WindowType,
		cost.WindowStartAtMS,
		cost.WindowResetAtMS,
		cost.EstimatedCost,
		cost.InputTokens,
		cost.OutputTokens,
		cost.CachedTokens,
		exhausted,
		cost.CalculatedAtMS,
		cost.CreatedAtMS,
		cost.UpdatedAtMS,
	)
	return err
}

func (r *repository) ListByRun(ctx context.Context, runID int64, targetProvider string) ([]model.AntigravityAccountWindowCost, error) {
	targetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude)
	rows, err := r.db.QueryContext(
		ctx,
		`select distinct
			c.account_key, c.target_provider, c.window_type, c.window_start_at_ms, c.window_reset_at_ms,
			c.estimated_cost, c.input_tokens, c.output_tokens, c.cached_tokens,
			c.is_quota_exhausted, c.calculated_at_ms, c.created_at_ms, c.updated_at_ms
		from antigravity_account_status_details d
		join antigravity_account_window_costs c on c.account_key = d.account_key
			and c.target_provider = d.target_provider
		where d.run_id = ? and d.target_provider = ?
		order by c.account_key asc, c.window_type asc`,
		runID,
		targetProvider,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AntigravityAccountWindowCost, 0)
	for rows.Next() {
		item, err := scanCost(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanCost(row interface{ Scan(dest ...any) error }) (model.AntigravityAccountWindowCost, error) {
	var item model.AntigravityAccountWindowCost
	var exhausted int
	if err := row.Scan(
		&item.AccountKey,
		&item.TargetProvider,
		&item.WindowType,
		&item.WindowStartAtMS,
		&item.WindowResetAtMS,
		&item.EstimatedCost,
		&item.InputTokens,
		&item.OutputTokens,
		&item.CachedTokens,
		&exhausted,
		&item.CalculatedAtMS,
		&item.CreatedAtMS,
		&item.UpdatedAtMS,
	); err != nil {
		return model.AntigravityAccountWindowCost{}, err
	}
	item.IsQuotaExhausted = exhausted != 0
	return item, nil
}
