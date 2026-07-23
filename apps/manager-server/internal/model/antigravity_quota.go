package model

import "strings"

// FilterAntigravityQuotaWindows keeps only quota windows owned by the requested provider.
// Server-level inspection retains every window because it represents the combined probe result.
func FilterAntigravityQuotaWindows(
	windows []AntigravityInspectionQuotaWindow,
	targetProvider string,
) []AntigravityInspectionQuotaWindow {
	normalizedProvider := NormalizeAntigravityTargetProvider(targetProvider, AntigravityTargetProviderClaude)
	if normalizedProvider == AntigravityTargetProviderServer {
		return append([]AntigravityInspectionQuotaWindow(nil), windows...)
	}

	filteredWindows := make([]AntigravityInspectionQuotaWindow, 0, len(windows))
	for _, window := range windows {
		if classifyAntigravityQuotaWindowProvider(window) == normalizedProvider {
			filteredWindows = append(filteredWindows, window)
		}
	}
	return filteredWindows
}

func MaxAntigravityQuotaUsedPercent(windows []AntigravityInspectionQuotaWindow) *float64 {
	var maximumUsedPercent *float64
	for _, window := range windows {
		if window.UsedPercent == nil {
			continue
		}
		if maximumUsedPercent == nil || *window.UsedPercent > *maximumUsedPercent {
			value := *window.UsedPercent
			maximumUsedPercent = &value
		}
	}
	return maximumUsedPercent
}

func FirstAntigravityQuotaResetAt(windows []AntigravityInspectionQuotaWindow) int64 {
	for _, window := range windows {
		if window.ResetAtMS > 0 {
			return window.ResetAtMS
		}
	}
	return 0
}

func classifyAntigravityQuotaWindowProvider(window AntigravityInspectionQuotaWindow) string {
	identity := strings.ToLower(strings.TrimSpace(window.ID + " " + window.LabelKey))
	if strings.Contains(identity, "claude") || strings.Contains(identity, "gpt-oss") {
		return AntigravityTargetProviderClaude
	}
	if strings.Contains(identity, "gemini") {
		return AntigravityTargetProviderGemini
	}
	return ""
}
