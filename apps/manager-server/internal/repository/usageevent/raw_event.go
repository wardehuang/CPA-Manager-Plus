package usageevent

import (
	"context"
	"database/sql"
)

type RawEventRecord struct {
	ID                    int64  `json:"id"`
	RequestID             string `json:"request_id"`
	EventHash             string `json:"event_hash"`
	TimestampMS           int64  `json:"timestamp_ms"`
	Timestamp             string `json:"timestamp"`
	Provider              string `json:"provider"`
	ExecutorType          string `json:"executor_type"`
	Model                 string `json:"model"`
	Endpoint              string `json:"endpoint"`
	Method                string `json:"method"`
	Path                  string `json:"path"`
	AuthType              string `json:"auth_type"`
	AuthIndex             string `json:"auth_index"`
	Source                string `json:"source"`
	SourceHash            string `json:"source_hash"`
	APIKeyHash            string `json:"api_key_hash"`
	AccountSnapshot       string `json:"account_snapshot"`
	AuthLabelSnapshot     string `json:"auth_label_snapshot"`
	AuthFileSnapshot      string `json:"auth_file_snapshot"`
	AuthProviderSnapshot  string `json:"auth_provider_snapshot"`
	AuthProjectIDSnapshot string `json:"auth_project_id_snapshot"`
	AuthSnapshotAtMS      int64  `json:"auth_snapshot_at_ms"`
	RequestedModel        string `json:"requested_model"`
	ResolvedModel         string `json:"resolved_model"`
	ReasoningEffort       string `json:"reasoning_effort"`
	ServiceTier           string `json:"service_tier"`
	InputTokens           int64  `json:"input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningTokens       int64  `json:"reasoning_tokens"`
	CachedTokens          int64  `json:"cached_tokens"`
	CacheTokens           int64  `json:"cache_tokens"`
	CacheReadTokens       int64  `json:"cache_read_tokens"`
	CacheCreationTokens   int64  `json:"cache_creation_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
	LatencyMS             *int64 `json:"latency_ms"`
	TTFTMS                *int64 `json:"ttft_ms"`
	Failed                bool   `json:"failed"`
	FailStatusCode        *int64 `json:"fail_status_code"`
	FailSummary           string `json:"fail_summary"`
	CreatedAtMS           int64  `json:"created_at_ms"`
	RawJSON               string `json:"-"`
}

func (r *repository) GetRawEventByHash(ctx context.Context, eventHash string) (RawEventRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `select
		id, coalesce(request_id, ''), event_hash, timestamp_ms, timestamp,
		coalesce(provider, ''), coalesce(executor_type, ''), model,
		coalesce(endpoint, ''), coalesce(method, ''), coalesce(path, ''),
		coalesce(auth_type, ''), coalesce(auth_index, ''), coalesce(source, ''),
		coalesce(source_hash, ''), coalesce(api_key_hash, ''),
		coalesce(account_snapshot, ''), coalesce(auth_label_snapshot, ''),
		coalesce(auth_file_snapshot, ''), coalesce(auth_provider_snapshot, ''),
		coalesce(auth_project_id_snapshot, ''), coalesce(auth_snapshot_at_ms, 0),
		coalesce(requested_model, ''), coalesce(resolved_model, ''),
		coalesce(reasoning_effort, ''), coalesce(service_tier, ''),
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_tokens,
		cache_read_tokens, cache_creation_tokens, total_tokens,
		latency_ms, ttft_ms, failed, fail_status_code,
		coalesce(fail_summary, ''), coalesce(raw_json, ''), created_at_ms
		from usage_events where event_hash = ? limit 1`, eventHash)

	var event RawEventRecord
	var latencyMS, ttftMS, failStatusCode sql.NullInt64
	var failed int64
	if err := row.Scan(
		&event.ID,
		&event.RequestID,
		&event.EventHash,
		&event.TimestampMS,
		&event.Timestamp,
		&event.Provider,
		&event.ExecutorType,
		&event.Model,
		&event.Endpoint,
		&event.Method,
		&event.Path,
		&event.AuthType,
		&event.AuthIndex,
		&event.Source,
		&event.SourceHash,
		&event.APIKeyHash,
		&event.AccountSnapshot,
		&event.AuthLabelSnapshot,
		&event.AuthFileSnapshot,
		&event.AuthProviderSnapshot,
		&event.AuthProjectIDSnapshot,
		&event.AuthSnapshotAtMS,
		&event.RequestedModel,
		&event.ResolvedModel,
		&event.ReasoningEffort,
		&event.ServiceTier,
		&event.InputTokens,
		&event.OutputTokens,
		&event.ReasoningTokens,
		&event.CachedTokens,
		&event.CacheTokens,
		&event.CacheReadTokens,
		&event.CacheCreationTokens,
		&event.TotalTokens,
		&latencyMS,
		&ttftMS,
		&failed,
		&failStatusCode,
		&event.FailSummary,
		&event.RawJSON,
		&event.CreatedAtMS,
	); err != nil {
		if err == sql.ErrNoRows {
			return RawEventRecord{}, false, nil
		}
		return RawEventRecord{}, false, err
	}

	event.Failed = failed != 0
	if latencyMS.Valid {
		value := latencyMS.Int64
		event.LatencyMS = &value
	}
	if ttftMS.Valid {
		value := ttftMS.Int64
		event.TTFTMS = &value
	}
	if failStatusCode.Valid {
		value := failStatusCode.Int64
		event.FailStatusCode = &value
	}
	return event, true, nil
}
