package wxaiinspection

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const wxaiBotFlagSourceClaim = "bot_flag_source"

type wxaiBotFlagSourceInspection struct {
	Flagged         bool
	NormalizedValue string
}

func inspectWxaiBotFlagSource(accessToken string) (wxaiBotFlagSourceInspection, error) {
	segments := strings.Split(accessToken, ".")
	if len(segments) < 2 {
		return wxaiBotFlagSourceInspection{}, fmt.Errorf("access_token 不是 JWT")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segments[1], "="))
	if err != nil {
		return wxaiBotFlagSourceInspection{}, fmt.Errorf("解码 JWT payload: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return wxaiBotFlagSourceInspection{}, fmt.Errorf("解析 JWT payload: %w", err)
	}
	botFlagSource, exists := payload[wxaiBotFlagSourceClaim]
	if !exists {
		return wxaiBotFlagSourceInspection{}, nil
	}

	normalizedValue := normalizeWxaiBotFlagSourceValue(botFlagSource)
	if botFlagSource == nil {
		return wxaiBotFlagSourceInspection{NormalizedValue: normalizedValue}, nil
	}
	if stringValue, isString := botFlagSource.(string); isString && strings.TrimSpace(stringValue) == "" {
		return wxaiBotFlagSourceInspection{NormalizedValue: normalizedValue}, nil
	}
	return wxaiBotFlagSourceInspection{
		Flagged:         true,
		NormalizedValue: normalizedValue,
	}, nil
}

func normalizeWxaiBotFlagSourceValue(value any) string {
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encodedValue)
}
