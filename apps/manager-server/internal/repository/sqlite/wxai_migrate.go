package sqlite

import (
	"database/sql"
	"fmt"
)

func migrateWxaiInspection(db *sql.DB) error {
	statements := []string{
		`create table if not exists wxai_inspection_runs (
			id integer primary key autoincrement,
			trigger_type text not null,
			trigger_key text,
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
			quota_exhausted_count integer not null default 0,
			abnormal_count integer not null default 0,
			error text,
			settings_json text not null,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
		`create index if not exists idx_wxai_inspection_runs_started_at on wxai_inspection_runs(started_at_ms)`,
		`create index if not exists idx_wxai_inspection_runs_status on wxai_inspection_runs(status)`,
		`create table if not exists wxai_inspection_results (
			id integer primary key autoincrement,
			run_id integer not null,
			account_key text not null,
			file_name text not null,
			display_account text not null,
			auth_index text,
			account_id text,
			provider text not null,
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
			monthly_limit_cents real,
			monthly_used_cents real,
			error_kind text,
			error_detail text,
			created_at_ms integer not null,
			foreign key(run_id) references wxai_inspection_runs(id) on delete cascade,
			unique(run_id, account_key)
		)`,
		`create index if not exists idx_wxai_inspection_results_run on wxai_inspection_results(run_id)`,
		`create table if not exists wxai_inspection_http_responses (
			id integer primary key autoincrement,
			run_id integer not null,
			account_key text not null,
			file_name text not null,
			request_stage text not null,
			request_method text not null,
			request_url text not null,
			response_status_code integer not null,
			final_url text,
			response_headers_json text not null default '{}',
			response_body blob not null,
			body_truncated integer not null default 0,
			sensitive_fields_redacted integer not null default 0,
			created_at_ms integer not null,
			foreign key(run_id) references wxai_inspection_runs(id) on delete cascade
		)`,
		`create index if not exists idx_wxai_inspection_http_responses_run on wxai_inspection_http_responses(run_id, created_at_ms)`,
		`create index if not exists idx_wxai_inspection_http_responses_account on wxai_inspection_http_responses(account_key, created_at_ms)`,
		`create table if not exists wxai_inspection_logs (
			id integer primary key autoincrement,
			run_id integer not null,
			level text not null,
			message text not null,
			detail_json text,
			created_at_ms integer not null,
			foreign key(run_id) references wxai_inspection_runs(id) on delete cascade
		)`,
		`create index if not exists idx_wxai_inspection_logs_run on wxai_inspection_logs(run_id, created_at_ms)`,
		`drop index if exists idx_wxai_inspection_logs_realtime_request_id`,
		`create index if not exists idx_wxai_inspection_logs_realtime_attempt
			on wxai_inspection_logs(
				json_extract(detail_json, '$.requestID'),
				json_extract(detail_json, '$.authIndex')
			)
			where json_extract(detail_json, '$.reason') = 'position_degradation'`,
		`create table if not exists wxai_account_status_details (
			run_id integer not null,
			account_key text not null,
			priority integer,
			schedule_group integer,
			account_type text,
			weekly_used_percent real,
			weekly_reset_at_ms integer,
			monthly_used_percent real,
			monthly_reset_at_ms integer,
			monthly_limit_cents real,
			monthly_used_cents real,
			checked_at_ms integer,
			created_at_ms integer not null,
			updated_at_ms integer not null,
			foreign key(run_id) references wxai_inspection_runs(id) on delete cascade,
			unique(run_id, account_key)
		)`,
		`create index if not exists idx_wxai_account_status_details_run on wxai_account_status_details(run_id)`,
		`create index if not exists idx_wxai_account_status_details_reset on wxai_account_status_details(account_key, weekly_reset_at_ms, monthly_reset_at_ms)`,
		`create table if not exists wxai_account_profiles (
			account_key text primary key,
			account_type text not null,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
		`insert or ignore into wxai_account_profiles (account_key, account_type, created_at_ms, updated_at_ms)
			select details.account_key, upper(details.account_type), details.created_at_ms, details.updated_at_ms
			from wxai_account_status_details details
			inner join (
				select account_key, max(updated_at_ms) as latest_updated_at_ms
				from wxai_account_status_details
				where upper(account_type) in ('FREE', 'SUPER')
				group by account_key
			) latest
			on latest.account_key = details.account_key
			and latest.latest_updated_at_ms = details.updated_at_ms
			where upper(details.account_type) in ('FREE', 'SUPER')`,
		`create index if not exists idx_wxai_account_profiles_type on wxai_account_profiles(account_type)`,
		`create table if not exists wxai_account_window_costs (
			account_key text not null,
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
			unique(account_key, window_type, window_reset_at_ms)
		)`,
		`create index if not exists idx_wxai_account_window_costs_account on wxai_account_window_costs(account_key, window_type)`,
		`create index if not exists idx_wxai_account_window_costs_reset on wxai_account_window_costs(window_type, window_reset_at_ms)`,
		`create table if not exists wxai_inspection_settings (
			id integer primary key check (id = 1),
			settings_json text not null,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
		`create table if not exists wxai_priority_adjustments (
			account_key text primary key,
			file_name text not null,
			display_account text not null,
			auth_index text,
			account_id text,
			original_priority integer,
			adjusted_priority integer not null,
			recover_at_ms integer,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
		`create index if not exists idx_wxai_priority_adjustments_recover_at on wxai_priority_adjustments(recover_at_ms)`,
		`create table if not exists wxai_realtime_degradation_states (
			account_key text primary key,
			file_name text not null,
			display_account text not null,
			auth_index text,
			account_id text,
			degradation_count integer not null,
			cooldown_until_ms integer not null default 0,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
		`create index if not exists idx_wxai_realtime_degradation_cooldown on wxai_realtime_degradation_states(cooldown_until_ms)`,
		`create table if not exists wxai_account_priority_intervals (
			account_key text primary key,
			started_at_ms integer,
			ended_at_ms integer,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureWxaiInspectionRunCountColumns(db); err != nil {
		return err
	}
	if err := ensureWxaiInspectionResultActionColumns(db); err != nil {
		return err
	}
	if err := ensureWxaiInspectionHTTPResponseColumns(db); err != nil {
		return err
	}
	return ensureWxaiAccountStatusDetailColumns(db)
}

func ensureWxaiAccountStatusDetailColumns(db *sql.DB) error {
	return ensureWxaiInspectionColumns(db, "wxai_account_status_details", []struct {
		name       string
		definition string
	}{
		{name: "schedule_group", definition: "integer"},
	})
}

func ensureWxaiInspectionRunCountColumns(db *sql.DB) error {
	return ensureWxaiInspectionColumns(db, "wxai_inspection_runs", []struct {
		name       string
		definition string
	}{
		{name: "delete_count", definition: "integer not null default 0"},
		{name: "enable_count", definition: "integer not null default 0"},
		{name: "quota_exhausted_count", definition: "integer not null default 0"},
		{name: "abnormal_count", definition: "integer not null default 0"},
	})
}

func ensureWxaiInspectionResultActionColumns(db *sql.DB) error {
	return ensureWxaiInspectionColumns(db, "wxai_inspection_results", []struct {
		name       string
		definition string
	}{
		{name: "executed_action", definition: "text"},
		{name: "action_error", definition: "text"},
	})
}

func ensureWxaiInspectionHTTPResponseColumns(db *sql.DB) error {
	return ensureWxaiInspectionColumns(db, "wxai_inspection_http_responses", []struct {
		name       string
		definition string
	}{
		{name: "response_headers_json", definition: "text not null default '{}'"},
	})
}

func ensureWxaiInspectionColumns(
	db *sql.DB,
	tableName string,
	columns []struct {
		name       string
		definition string
	},
) error {
	rows, err := db.Query(fmt.Sprintf(`pragma table_info(%s)`, tableName))
	if err != nil {
		return err
	}
	defer rows.Close()

	existingColumns := map[string]struct{}{}
	for rows.Next() {
		var columnID int
		var columnName string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&columnID, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		existingColumns[columnName] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, column := range columns {
		if _, exists := existingColumns[column.name]; exists {
			continue
		}
		statement := fmt.Sprintf(`alter table %s add column %s %s`, tableName, column.name, column.definition)
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
