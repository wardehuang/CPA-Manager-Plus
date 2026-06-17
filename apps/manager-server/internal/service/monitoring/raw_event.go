package monitoring

import (
	"context"
	"encoding/json"
	"strings"
)

type RawEventResponse struct {
	Event       RawEventRecord `json:"event"`
	RawJSON     any            `json:"raw_json,omitempty"`
	RawJSONText string         `json:"raw_json_text,omitempty"`
}

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
}

func (s *Service) RawEvent(ctx context.Context, eventHash string) (RawEventResponse, bool, error) {
	eventHash = strings.TrimSpace(eventHash)
	if eventHash == "" {
		return RawEventResponse{}, false, nil
	}

	event, found, err := s.store.GetRawEventByHash(ctx, eventHash)
	if err != nil || !found {
		return RawEventResponse{}, found, err
	}

	rawJSONText := event.RawPayload
	var rawJSON any
	if strings.TrimSpace(rawJSONText) != "" {
		_ = json.Unmarshal([]byte(rawJSONText), &rawJSON)
	}

	return RawEventResponse{
		Event: RawEventRecord{
			ID:                    event.ID,
			RequestID:             event.RequestID,
			EventHash:             event.EventHash,
			TimestampMS:           event.TimestampMS,
			Timestamp:             event.Timestamp,
			Provider:              event.Provider,
			ExecutorType:          event.ExecutorType,
			Model:                 event.Model,
			Endpoint:              event.Endpoint,
			Method:                event.Method,
			Path:                  event.Path,
			AuthType:              event.AuthType,
			AuthIndex:             event.AuthIndex,
			Source:                event.Source,
			SourceHash:            event.SourceHash,
			APIKeyHash:            event.APIKeyHash,
			AccountSnapshot:       event.AccountSnapshot,
			AuthLabelSnapshot:     event.AuthLabelSnapshot,
			AuthFileSnapshot:      event.AuthFileSnapshot,
			AuthProviderSnapshot:  event.AuthProviderSnapshot,
			AuthProjectIDSnapshot: event.AuthProjectIDSnapshot,
			AuthSnapshotAtMS:      event.AuthSnapshotAtMS,
			RequestedModel:        event.RequestedModel,
			ResolvedModel:         event.ResolvedModel,
			ReasoningEffort:       event.ReasoningEffort,
			ServiceTier:           event.ServiceTier,
			InputTokens:           event.InputTokens,
			OutputTokens:          event.OutputTokens,
			ReasoningTokens:       event.ReasoningTokens,
			CachedTokens:          event.CachedTokens,
			CacheTokens:           event.CacheTokens,
			CacheReadTokens:       event.CacheReadTokens,
			CacheCreationTokens:   event.CacheCreationTokens,
			TotalTokens:           event.TotalTokens,
			LatencyMS:             event.LatencyMS,
			TTFTMS:                event.TTFTMS,
			Failed:                event.Failed,
			FailStatusCode:        event.FailStatusCode,
			FailSummary:           event.FailSummary,
			CreatedAtMS:           event.CreatedAtMS,
		},
		RawJSON:     rawJSON,
		RawJSONText: rawJSONText,
	}, true, nil
}
