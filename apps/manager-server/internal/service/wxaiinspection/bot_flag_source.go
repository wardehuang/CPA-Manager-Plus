package wxaiinspection

import (
	"context"
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
	Priority        int
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

func (service *Service) inspectWxaiAuthBotFlags(
	ctx context.Context,
	authFile map[string]any,
	logger runLogger,
) (wxaiBotFlagInspection, error) {
	accessToken := strings.TrimSpace(firstString(authFile, "access_token"))
	jwtInspection, err := inspectWxaiBotFlags(accessToken)
	if err != nil || jwtInspection.Flagged {
		return jwtInspection, err
	}

	ssoCookie := strings.TrimSpace(firstString(authFile, "sso"))
	authProxyURL := resolveWxaiAuthProxyURL(authFile)
	if authProxyURL == "" {
		return wxaiBotFlagInspection{}, ErrWxaiAuthProxyURLMissing
	}
	ssoInspection, err := inspectWxaiSSOBotFlags(ctx, ssoCookie, authProxyURL)
	if err != nil {
		if isWxaiProxySetupError(err) {
			return wxaiBotFlagInspection{}, err
		}
		logger.warning(context.WithoutCancel(ctx), "wXAi SSO 已失效", map[string]any{
			"statusCode": ssoInspection.StatusCode,
			"finalURL":   ssoInspection.FinalURL,
			"error":      err.Error(),
		})
		return wxaiBotFlagInspection{
			Flagged:         true,
			Claim:           "sso_expired",
			NormalizedValue: formatWxaiSSOInspectionError(ssoInspection, err),
			Priority:        wxaiSSOExpiredPriorityValue,
		}, nil
	}
	if !ssoInspection.Flagged {
		logger.info(context.WithoutCancel(ctx), "wXAi SSO bot 标记检查完成", map[string]any{
			"statusCode":    ssoInspection.StatusCode,
			"botFlagSource": ssoInspection.BotFlagSource,
			"denied":        ssoInspection.Denied,
		})
		return wxaiBotFlagInspection{}, nil
	}
	return wxaiBotFlagInspection{
		Flagged:         true,
		Claim:           "sso_grok_account_state",
		NormalizedValue: ssoInspection.normalizedValue(),
	}, nil
}

func normalizeWxaiBotFlagValue(value any) string {
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encodedValue)
}
