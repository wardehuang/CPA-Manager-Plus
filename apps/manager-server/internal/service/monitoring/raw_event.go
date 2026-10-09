package monitoring

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/usageevent"
)

type RawEventResponse struct {
	Event       usageevent.RawEventRecord `json:"event"`
	RawJSON     any                       `json:"raw_json,omitempty"`
	RawJSONText string                    `json:"raw_json_text,omitempty"`
}

func (s *Service) RawEvent(ctx context.Context, eventHash string) (RawEventResponse, bool, error) {
	eventHash = strings.TrimSpace(eventHash)
	if eventHash == "" {
		return RawEventResponse{}, false, nil
	}

	event, found, err := s.store.UsageEvents.GetRawEventByHash(ctx, eventHash)
	if err != nil || !found {
		return RawEventResponse{}, found, err
	}

	rawJSONText := event.RawJSON
	var rawJSON any
	if strings.TrimSpace(rawJSONText) != "" {
		_ = json.Unmarshal([]byte(rawJSONText), &rawJSON)
	}

	return RawEventResponse{
		Event:       event,
		RawJSON:     rawJSON,
		RawJSONText: rawJSONText,
	}, true, nil
}
