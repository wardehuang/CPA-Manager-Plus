package usageevent

import (
	"context"
	"database/sql"
)

type RawEvent struct {
	ID                    int64
	RequestID             string
	EventHash             string
	TimestampMS           int64
	Timestamp             string
	Provider              string
	ExecutorType          string
	Model                 string
	Endpoint              string
	Method                string
	Path                  string
	AuthType              string
	AuthIndex             string
	Source                string
	SourceHash            string
	APIKeyHash            string
	AccountSnapshot       string
	AuthLabelSnapshot     string
	AuthFileSnapshot      string
	AuthProviderSnapshot  string
	AuthProjectIDSnapshot string
	AuthSnapshotAtMS      int64
	RequestedModel        string
	ResolvedModel         string
	ReasoningEffort       string
	ServiceTier           string
	InputTokens           int64
	OutputTokens          int64
	ReasoningTokens       int64
	CachedTokens          int64
	CacheTokens           int64
	CacheReadTokens       int64
	CacheCreationTokens   int64
	TotalTokens           int64
	LatencyMS             *int64
	TTFTMS                *int64
	GenerationMS          *int64
	Failed                bool
	FailStatusCode        *int64
	FailSummary           string
	RawJSON               string
	RawPayload            string
	CreatedAtMS           int64
}

func (r *repository) GetRawEventByHash(ctx context.Context, eventHash string) (RawEvent, bool, error) {
	row := r.db.QueryRowContext(ctx, `select
		e.id, e.request_id, e.event_hash, e.timestamp_ms, e.timestamp, e.provider, e.executor_type, e.model, e.endpoint, e.method, e.path,
		e.auth_type, e.auth_index, e.source, e.source_hash, e.api_key_hash,
		e.account_snapshot, e.auth_label_snapshot, e.auth_file_snapshot, e.auth_provider_snapshot, e.auth_project_id_snapshot, e.auth_snapshot_at_ms,
		e.requested_model, e.resolved_model, e.reasoning_effort, e.service_tier,
		e.input_tokens, e.output_tokens, e.reasoning_tokens, e.cached_tokens, e.cache_tokens, e.cache_read_tokens, e.cache_creation_tokens, e.total_tokens,
		e.latency_ms, e.ttft_ms, e.generation_ms, e.failed, e.fail_status_code, e.fail_summary, e.raw_json, r.raw_payload, e.created_at_ms
		from usage_events e join usage_raw r on r.event_hash = e.event_hash where e.event_hash = ?`, eventHash)

	var event RawEvent
	var requestID, provider, executorType, endpoint, method, path, authType, authIndex, source, sourceHash, apiKeyHash sql.NullString
	var accountSnapshot, authLabelSnapshot, authFileSnapshot, authProviderSnapshot, authProjectIDSnapshot sql.NullString
	var requestedModel, resolvedModel, reasoningEffort, serviceTier, failSummary, rawJSON, rawPayload sql.NullString
	var authSnapshotAtMS, latencyMS, ttftMS, generationMS, failStatusCode sql.NullInt64
	var failed int
	if err := row.Scan(
		&event.ID,
		&requestID,
		&event.EventHash,
		&event.TimestampMS,
		&event.Timestamp,
		&provider,
		&executorType,
		&event.Model,
		&endpoint,
		&method,
		&path,
		&authType,
		&authIndex,
		&source,
		&sourceHash,
		&apiKeyHash,
		&accountSnapshot,
		&authLabelSnapshot,
		&authFileSnapshot,
		&authProviderSnapshot,
		&authProjectIDSnapshot,
		&authSnapshotAtMS,
		&requestedModel,
		&resolvedModel,
		&reasoningEffort,
		&serviceTier,
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
		&generationMS,
		&failed,
		&failStatusCode,
		&failSummary,
		&rawJSON,
		&rawPayload,
		&event.CreatedAtMS,
	); err != nil {
		if err == sql.ErrNoRows {
			return RawEvent{}, false, nil
		}
		return RawEvent{}, false, err
	}

	event.RequestID = requestID.String
	event.Provider = provider.String
	event.ExecutorType = executorType.String
	event.Endpoint = endpoint.String
	event.Method = method.String
	event.Path = path.String
	event.AuthType = authType.String
	event.AuthIndex = authIndex.String
	event.Source = source.String
	event.SourceHash = sourceHash.String
	event.APIKeyHash = apiKeyHash.String
	event.AccountSnapshot = accountSnapshot.String
	event.AuthLabelSnapshot = authLabelSnapshot.String
	event.AuthFileSnapshot = authFileSnapshot.String
	event.AuthProviderSnapshot = authProviderSnapshot.String
	event.AuthProjectIDSnapshot = authProjectIDSnapshot.String
	event.AuthSnapshotAtMS = authSnapshotAtMS.Int64
	event.RequestedModel = requestedModel.String
	event.ResolvedModel = resolvedModel.String
	event.ReasoningEffort = reasoningEffort.String
	event.ServiceTier = serviceTier.String
	event.Failed = failed != 0
	event.FailSummary = failSummary.String
	event.RawJSON = rawJSON.String
	event.RawPayload = rawPayload.String
	if latencyMS.Valid {
		value := latencyMS.Int64
		event.LatencyMS = &value
	}
	if ttftMS.Valid {
		value := ttftMS.Int64
		event.TTFTMS = &value
	}
	if generationMS.Valid {
		value := generationMS.Int64
		event.GenerationMS = &value
	}
	if failStatusCode.Valid {
		value := failStatusCode.Int64
		event.FailStatusCode = &value
	}
	return event, true, nil
}
