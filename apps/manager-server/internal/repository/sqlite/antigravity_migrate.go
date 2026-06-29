package sqlite

import (
	"database/sql"
	"fmt"
)

func migrateAntigravityInspection(db *sql.DB) error {
	statements := []string{
		`create table if not exists antigravity_inspection_runs (
			id integer primary key autoincrement,
			trigger_type text not null,
			trigger_key text,
			target_provider text,
			status text not null,
			started_at_ms integer not null,
			finished_at_ms integer,
			total_files integer not null default 0,
			probe_set_count integer not null default 0,
			sampled_count integer not null default 0,
			disabled_count integer not null default 0,
			enabled_count integer not null default 0,
			delete_count integer not null default 0,
			disable_count integer not null default 0,
			enable_count integer not null default 0,
			reauth_count integer not null default 0,
			keep_count integer not null default 0,
			error text,
			settings_json text not null,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
		`create index if not exists idx_antigravity_inspection_runs_started_at on antigravity_inspection_runs(started_at_ms)`,
		`create index if not exists idx_antigravity_inspection_runs_status on antigravity_inspection_runs(status)`,
		`create index if not exists idx_antigravity_inspection_runs_provider on antigravity_inspection_runs(target_provider, started_at_ms)`,
		`create table if not exists antigravity_inspection_results (
			id integer primary key autoincrement,
			run_id integer not null,
			account_key text not null,
			file_name text not null,
			display_account text not null,
			auth_index text,
			account_id text,
			provider text,
			target_provider text not null,
			disabled integer not null default 0,
			status text,
			state text,
			action text not null,
			action_reason text,
			action_status text,
			executed_action text,
			action_error text,
			status_code integer,
			used_percent real,
			is_quota integer not null default 0,
			error text,
			plan_type text,
			quota_windows_json text,
			error_kind text,
			error_detail text,
			created_at_ms integer not null,
			foreign key(run_id) references antigravity_inspection_runs(id) on delete cascade,
			unique(run_id, account_key, target_provider)
		)`,
		`create index if not exists idx_antigravity_inspection_results_run on antigravity_inspection_results(run_id)`,
		`create table if not exists antigravity_inspection_logs (
			id integer primary key autoincrement,
			run_id integer not null,
			level text not null,
			message text not null,
			detail_json text,
			created_at_ms integer not null,
			foreign key(run_id) references antigravity_inspection_runs(id) on delete cascade
		)`,
		`create index if not exists idx_antigravity_inspection_logs_run on antigravity_inspection_logs(run_id, created_at_ms)`,
		`create table if not exists antigravity_account_status_details (
			run_id integer not null,
			account_key text not null,
			target_provider text not null,
			priority integer,
			account_type text,
			used_percent real,
			reset_at_ms integer,
			checked_at_ms integer,
			created_at_ms integer not null,
			updated_at_ms integer not null,
			foreign key(run_id) references antigravity_inspection_runs(id) on delete cascade,
			unique(run_id, account_key, target_provider)
		)`,
		`create index if not exists idx_antigravity_account_status_details_run on antigravity_account_status_details(run_id, target_provider)`,
		`create table if not exists antigravity_account_window_costs (
			account_key text not null,
			target_provider text not null,
			window_type text not null,
			window_start_at_ms integer not null,
			window_reset_at_ms integer not null,
			estimated_cost real not null default 0,
			input_tokens integer not null default 0,
			output_tokens integer not null default 0,
			cached_tokens integer not null default 0,
			is_quota_exhausted integer not null default 0,
			calculated_at_ms integer not null,
			created_at_ms integer not null,
			updated_at_ms integer not null,
			unique(account_key, target_provider, window_type, window_reset_at_ms)
		)`,
		`create index if not exists idx_antigravity_account_window_costs_account on antigravity_account_window_costs(account_key, target_provider, window_type)`,
		`create table if not exists antigravity_priority_adjustments (
			account_key text not null,
			target_provider text not null,
			file_name text not null,
			display_account text not null,
			auth_index text,
			account_id text,
			original_priority integer,
			recover_at_ms integer,
			created_at_ms integer not null,
			updated_at_ms integer not null,
			primary key(account_key, target_provider)
		)`,
		`create index if not exists idx_antigravity_priority_adjustments_recover_at on antigravity_priority_adjustments(recover_at_ms)`,
		`create table if not exists antigravity_inspection_settings (
			target_provider text primary key,
			settings_json text not null,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return ensureAntigravityAccountWindowCostColumns(db)
}

func ensureAntigravityAccountWindowCostColumns(db *sql.DB) error {
	rows, err := db.Query(`pragma table_info(antigravity_account_window_costs)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	columns := []struct {
		name       string
		definition string
	}{
		{name: "input_tokens", definition: "integer not null default 0"},
		{name: "output_tokens", definition: "integer not null default 0"},
		{name: "cached_tokens", definition: "integer not null default 0"},
	}
	for _, column := range columns {
		if _, ok := existing[column.name]; ok {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf(`alter table antigravity_account_window_costs add column %s %s`, column.name, column.definition)); err != nil {
			return err
		}
	}
	return nil
}
