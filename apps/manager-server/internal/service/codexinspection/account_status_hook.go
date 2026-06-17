package codexinspection

import (
	"context"
	"log"
	"math"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type accountStatusWindow struct {
	UsedPercent        *float64
	LimitWindowSeconds *float64
	ResetAtMS          int64
}

func (s *Service) captureCodexAccountStatusDetail(ctx context.Context, runID int64, accountKey string, priority *int, payload map[string]any, planType string, logger runLogger) {
	if runID <= 0 || accountKey == "" || payload == nil {
		logger.warning(ctx, "跳过写入账号状态详情", map[string]any{
			"runId":      runID,
			"accountKey": accountKey,
			"payloadNil": payload == nil,
		})
		log.Printf("[codex-inspection] skip account status detail run_id=%d account_key=%q payload_nil=%t", runID, accountKey, payload == nil)
		return
	}
	rateLimit := readMap(payload, "rate_limit", "rateLimit")
	primary := parseAccountStatusWindow(readMap(rateLimit, "primary_window", "primaryWindow"))
	secondary := parseAccountStatusWindow(readMap(rateLimit, "secondary_window", "secondaryWindow"))
	fiveHour, weekly, monthly := classifyAccountStatusWindows(primary, secondary, planType)
	detail := model.CodexAccountStatusDetail{
		RunID:                               runID,
		AccountKey:                          accountKey,
		Priority:                            priority,
		AccountType:                         normalizeCodexPlanType(planType),
		RateLimitResetCreditsAvailableCount: readAccountStatusIntPtr(readMap(payload, "rate_limit_reset_credits", "rateLimitResetCredits"), "available_count", "availableCount"),
		CheckedAtMS:                         time.Now().UnixMilli(),
	}
	if fiveHour != nil {
		detail.FiveHourUsedPercent = fiveHour.UsedPercent
		detail.FiveHourResetAtMS = fiveHour.ResetAtMS
	}
	if weekly != nil {
		detail.WeeklyUsedPercent = weekly.UsedPercent
		detail.WeeklyResetAtMS = weekly.ResetAtMS
	}
	if monthly != nil {
		detail.MonthlyUsedPercent = monthly.UsedPercent
		detail.MonthlyResetAtMS = monthly.ResetAtMS
	}
	if err := s.upsertCodexAccountStatusDetailWithRetry(ctx, detail); err != nil {
		logger.warning(ctx, "写入账号状态详情失败", map[string]any{
			"runId":                 runID,
			"accountKey":            accountKey,
			"priority":              priority,
			"accountType":           detail.AccountType,
			"fiveHourUsedPercent":   detail.FiveHourUsedPercent,
			"weeklyUsedPercent":     detail.WeeklyUsedPercent,
			"monthlyUsedPercent":    detail.MonthlyUsedPercent,
			"fiveHourResetAtMs":     detail.FiveHourResetAtMS,
			"weeklyResetAtMs":       detail.WeeklyResetAtMS,
			"monthlyResetAtMs":      detail.MonthlyResetAtMS,
			"resetCreditsAvailable": detail.RateLimitResetCreditsAvailableCount,
			"error":                 err.Error(),
		})
		log.Printf("[codex-inspection] write account status detail failed run_id=%d account_key=%q error=%v", runID, accountKey, err)
		return
	}
}

func (s *Service) upsertCodexAccountStatusDetailWithRetry(ctx context.Context, detail model.CodexAccountStatusDetail) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.store.UpsertCodexAccountStatusDetail(ctx, detail); err != nil {
			lastErr = err
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
			}
			continue
		}
		return nil
	}
	return lastErr
}

func parseAccountStatusWindow(raw map[string]any) *accountStatusWindow {
	if raw == nil {
		return nil
	}
	window := &accountStatusWindow{}
	if value, ok := readNumberPtr(raw, "used_percent", "usedPercent"); ok {
		window.UsedPercent = value
	}
	if value, ok := readNumberPtr(raw, "limit_window_seconds", "limitWindowSeconds"); ok {
		window.LimitWindowSeconds = value
	}
	window.ResetAtMS = resolveAccountStatusResetAtMS(raw)
	return window
}

func classifyAccountStatusWindows(primary, secondary *accountStatusWindow, planType string) (*accountStatusWindow, *accountStatusWindow, *accountStatusWindow) {
	teamPlan := normalizeCodexPlanType(planType) == "team"
	windows := []*accountStatusWindow{primary, secondary}
	var fiveHour *accountStatusWindow
	var weekly *accountStatusWindow
	var monthly *accountStatusWindow
	var genericLong *accountStatusWindow
	for _, window := range windows {
		if window == nil || window.LimitWindowSeconds == nil {
			continue
		}
		seconds := int(math.Round(*window.LimitWindowSeconds))
		switch {
		case seconds == codexFiveHourWindow && fiveHour == nil:
			fiveHour = window
		case seconds == codexWeekWindow && weekly == nil:
			weekly = window
		case seconds == codexMonthWindow && monthly == nil:
			monthly = window
		case seconds > codexFiveHourWindow && genericLong == nil:
			genericLong = window
		}
	}
	if fiveHour == nil && primary != weekly && primary != monthly && primary != genericLong && !accountStatusHasExplicitWindowSeconds(primary) {
		fiveHour = primary
	}
	if teamPlan {
		if monthly == nil && secondary != fiveHour && !accountStatusHasExplicitWindowSeconds(secondary) {
			monthly = secondary
		}
	} else if weekly == nil && secondary != fiveHour && !accountStatusHasExplicitWindowSeconds(secondary) {
		weekly = secondary
	}
	return fiveHour, weekly, monthly
}

func accountStatusHasExplicitWindowSeconds(window *accountStatusWindow) bool {
	return window != nil && window.LimitWindowSeconds != nil
}

func resolveAccountStatusResetAtMS(raw map[string]any) int64 {
	if value, ok := readNumberPtr(raw, "reset_at", "resetAt"); ok && value != nil && *value > 0 {
		seconds := int64(math.Round(*value))
		if seconds > 1_000_000_000_000 {
			return seconds
		}
		return seconds * 1000
	}
	if value, ok := readNumberPtr(raw, "reset_after_seconds", "resetAfterSeconds"); ok && value != nil && *value > 0 {
		return time.Now().Add(time.Duration(*value * float64(time.Second))).UnixMilli()
	}
	return 0
}

func readAccountStatusIntPtr(record map[string]any, keys ...string) *int {
	value, ok := readNumberPtr(record, keys...)
	if !ok || value == nil {
		return nil
	}
	result := int(math.Round(*value))
	return &result
}
