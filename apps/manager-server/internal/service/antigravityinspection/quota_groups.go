package antigravityinspection

import (
	"fmt"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

// buildAntigravityQuotaWindowsFromGroups supports the newer quota summary shape.
func buildAntigravityQuotaWindowsFromGroups(root map[string]any) []model.AntigravityInspectionQuotaWindow {
	rawGroups, ok := root["groups"].([]any)
	if !ok || len(rawGroups) == 0 {
		return nil
	}

	windows := make([]model.AntigravityInspectionQuotaWindow, 0)
	for groupIndex, rawGroup := range rawGroups {
		group, ok := rawGroup.(map[string]any)
		if !ok {
			continue
		}
		groupLabel := readString(authFile(group), "displayName", "display_name")
		if groupLabel == "" {
			groupLabel = fmt.Sprintf("quota-group-%d", groupIndex+1)
		}

		rawBuckets, ok := group["buckets"].([]any)
		if !ok {
			continue
		}
		for bucketIndex, rawBucket := range rawBuckets {
			bucket, ok := rawBucket.(map[string]any)
			if !ok {
				continue
			}
			remainingFraction, ok := readQuotaFraction(firstValue(bucket, "remainingFraction", "remaining_fraction"))
			if !ok {
				continue
			}

			usedPercent := (1 - remainingFraction) * 100
			if usedPercent < 0 {
				usedPercent = 0
			}
			if usedPercent > 100 {
				usedPercent = 100
			}

			bucketID := readString(authFile(bucket), "bucketId", "bucket_id")
			if bucketID == "" {
				bucketID = fmt.Sprintf("bucket-%d", bucketIndex+1)
			}
			bucketLabel := readString(authFile(bucket), "displayName", "display_name")
			if bucketLabel == "" {
				bucketLabel = bucketID
			}
			resetText := readString(authFile(bucket), "resetTime", "reset_time")
			limitWindowSeconds := parseAntigravityQuotaWindowSeconds(readString(authFile(bucket), "window"))
			windows = append(windows, model.AntigravityInspectionQuotaWindow{
				ID:                 normalizeAntigravityQuotaWindowID(groupLabel + "-" + bucketID),
				LabelKey:           strings.TrimSpace(groupLabel + " / " + bucketLabel),
				UsedPercent:        &usedPercent,
				ResetAtMS:          parseAntigravityResetTime(resetText),
				ResetLabel:         resetText,
				LimitWindowSeconds: limitWindowSeconds,
			})
		}
	}
	return windows
}

func parseAntigravityQuotaWindowSeconds(value string) *float64 {
	normalizedWindow := strings.ToLower(strings.TrimSpace(value))
	var windowDuration time.Duration
	switch {
	case strings.Contains(normalizedWindow, "five"), strings.Contains(normalizedWindow, "hour"):
		windowDuration = 5 * time.Hour
	case strings.Contains(normalizedWindow, "week"):
		windowDuration = 7 * 24 * time.Hour
	case strings.Contains(normalizedWindow, "month"):
		windowDuration = 30 * 24 * time.Hour
	default:
		return nil
	}
	seconds := windowDuration.Seconds()
	return &seconds
}

func mergeAntigravityQuotaWindows(
	preferredWindows []model.AntigravityInspectionQuotaWindow,
	fallbackWindows []model.AntigravityInspectionQuotaWindow,
) []model.AntigravityInspectionQuotaWindow {
	mergedWindows := make([]model.AntigravityInspectionQuotaWindow, 0, len(preferredWindows)+len(fallbackWindows))
	seenWindowIDs := make(map[string]struct{}, len(preferredWindows)+len(fallbackWindows))
	for _, windows := range [][]model.AntigravityInspectionQuotaWindow{preferredWindows, fallbackWindows} {
		for _, window := range windows {
			windowID := strings.ToLower(strings.TrimSpace(window.ID))
			if _, exists := seenWindowIDs[windowID]; exists {
				continue
			}
			seenWindowIDs[windowID] = struct{}{}
			mergedWindows = append(mergedWindows, window)
		}
	}
	return mergedWindows
}
