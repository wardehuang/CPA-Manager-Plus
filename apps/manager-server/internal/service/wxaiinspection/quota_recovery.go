package wxaiinspection

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	wxaiQuotaRecoveryGracePeriod = time.Minute
	wxaiDefaultQuotaCooldown     = 24 * time.Hour
)

type wxaiQuotaRecovery struct {
	recoverAtMS int64
	source      string
}

type wxaiResolvedQuotaRecovery struct {
	upstreamRecoverAtMS int64
	recoverAtMS         int64
	source              string
}

func extractWxaiQuotaRecovery(response wxaiHTTPResponse) wxaiQuotaRecovery {
	now := time.Now()
	if recoverAtMS := parseWxaiRetryAfterHeader(response.Header.Get("Retry-After"), now); recoverAtMS > 0 {
		return wxaiQuotaRecovery{recoverAtMS: recoverAtMS, source: "header:Retry-After"}
	}
	for _, headerName := range []string{"X-RateLimit-Reset", "X-Rate-Limit-Reset"} {
		if recoverAtMS := parseWxaiAbsoluteRecoveryValue(response.Header.Get(headerName)); recoverAtMS > 0 {
			return wxaiQuotaRecovery{recoverAtMS: recoverAtMS, source: "header:" + headerName}
		}
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return wxaiQuotaRecovery{}
	}
	if recoverAtMS := findWxaiQuotaRecoveryTime(payload, now); recoverAtMS > 0 {
		return wxaiQuotaRecovery{recoverAtMS: recoverAtMS, source: "response_body"}
	}
	return wxaiQuotaRecovery{}
}

func findWxaiQuotaRecoveryTime(value any, now time.Time) int64 {
	switch typedValue := value.(type) {
	case map[string]any:
		for key, fieldValue := range typedValue {
			normalizedKey := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			switch normalizedKey {
			case "retryafter", "retryafterseconds":
				if recoverAtMS := parseWxaiRelativeRecoveryValue(fieldValue, now, time.Second); recoverAtMS > 0 {
					return recoverAtMS
				}
			case "retryafterms", "retryaftermilliseconds":
				if recoverAtMS := parseWxaiRelativeRecoveryValue(fieldValue, now, time.Millisecond); recoverAtMS > 0 {
					return recoverAtMS
				}
			case "resetat", "resetsat", "recoverytime", "recoverat":
				if recoverAtMS := parseWxaiAbsoluteRecoveryValue(fieldValue); recoverAtMS > 0 {
					return recoverAtMS
				}
			}
		}
		for _, fieldValue := range typedValue {
			if recoverAtMS := findWxaiQuotaRecoveryTime(fieldValue, now); recoverAtMS > 0 {
				return recoverAtMS
			}
		}
	case []any:
		for _, item := range typedValue {
			if recoverAtMS := findWxaiQuotaRecoveryTime(item, now); recoverAtMS > 0 {
				return recoverAtMS
			}
		}
	}
	return 0
}

func parseWxaiRetryAfterHeader(value string, now time.Time) int64 {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(trimmedValue, 64); err == nil && seconds > 0 {
		return now.Add(time.Duration(seconds * float64(time.Second))).UnixMilli()
	}
	if parsedTime, err := http.ParseTime(trimmedValue); err == nil {
		return parsedTime.UnixMilli()
	}
	return 0
}

func parseWxaiRelativeRecoveryValue(value any, now time.Time, unit time.Duration) int64 {
	numericValue, ok := wxaiNumericValue(value)
	if !ok || numericValue <= 0 {
		return 0
	}
	return now.Add(time.Duration(numericValue * float64(unit))).UnixMilli()
}

func parseWxaiAbsoluteRecoveryValue(value any) int64 {
	switch typedValue := value.(type) {
	case string:
		trimmedValue := strings.TrimSpace(typedValue)
		if trimmedValue == "" {
			return 0
		}
		if parsedTime, err := time.Parse(time.RFC3339Nano, trimmedValue); err == nil {
			return parsedTime.UnixMilli()
		}
		numericValue, err := strconv.ParseFloat(trimmedValue, 64)
		if err != nil {
			return 0
		}
		return normalizeWxaiUnixTimestamp(numericValue)
	default:
		numericValue, ok := wxaiNumericValue(value)
		if !ok {
			return 0
		}
		return normalizeWxaiUnixTimestamp(numericValue)
	}
}

func normalizeWxaiUnixTimestamp(value float64) int64 {
	if value <= 0 {
		return 0
	}
	if value < 100000000000 {
		return int64(value * 1000)
	}
	return int64(value)
}

func wxaiNumericValue(value any) (float64, bool) {
	switch typedValue := value.(type) {
	case float64:
		return typedValue, true
	case json.Number:
		parsedValue, err := typedValue.Float64()
		return parsedValue, err == nil
	case string:
		parsedValue, err := strconv.ParseFloat(strings.TrimSpace(typedValue), 64)
		return parsedValue, err == nil
	default:
		return 0, false
	}
}

func resolveWxaiQuotaRecovery(
	upstreamRecovery wxaiQuotaRecovery,
	now time.Time,
) wxaiResolvedQuotaRecovery {
	if upstreamRecovery.recoverAtMS > now.UnixMilli() {
		return wxaiResolvedQuotaRecovery{
			upstreamRecoverAtMS: upstreamRecovery.recoverAtMS,
			recoverAtMS:         time.UnixMilli(upstreamRecovery.recoverAtMS).Add(wxaiQuotaRecoveryGracePeriod).UnixMilli(),
			source:              upstreamRecovery.source,
		}
	}
	return wxaiResolvedQuotaRecovery{
		recoverAtMS: now.Add(wxaiDefaultQuotaCooldown + wxaiQuotaRecoveryGracePeriod).UnixMilli(),
		source:      "default_24h",
	}
}
