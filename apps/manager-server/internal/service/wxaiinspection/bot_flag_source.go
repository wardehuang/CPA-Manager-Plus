package wxaiinspection

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	wxaiBotFlagSourceClaim = "bot_flag_source"
	wxaiBFSClaim           = "bfs"
)

type wxaiBotFlagInspection struct {
	Flagged         bool
	Claim           string
	NormalizedValue string
}

func inspectWxaiBotFlags(accessToken string) (wxaiBotFlagInspection, error) {
	segments := strings.Split(accessToken, ".")
	if len(segments) < 2 {
		return wxaiBotFlagInspection{}, fmt.Errorf("access_token 不是 JWT")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segments[1], "="))
	if err != nil {
		return wxaiBotFlagInspection{}, fmt.Errorf("解码 JWT payload: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return wxaiBotFlagInspection{}, fmt.Errorf("解析 JWT payload: %w", err)
	}

	for _, claim := range []string{wxaiBotFlagSourceClaim, wxaiBFSClaim} {
		flagValue, exists := payload[claim]
		if !exists {
			continue
		}

		normalizedValue := normalizeWxaiBotFlagValue(flagValue)
		if flagValue == nil {
			continue
		}
		if stringValue, isString := flagValue.(string); isString && strings.TrimSpace(stringValue) == "" {
			continue
		}
		return wxaiBotFlagInspection{
			Flagged:         true,
			Claim:           claim,
			NormalizedValue: normalizedValue,
		}, nil
	}
	return wxaiBotFlagInspection{}, nil
}

func normalizeWxaiBotFlagValue(value any) string {
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encodedValue)
}
