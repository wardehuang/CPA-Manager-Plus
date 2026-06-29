package antigravityaccountstatus

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/pricing"
)

const (
	antigravityAccountStatusFiveHourWindowSeconds = 5 * 60 * 60
	antigravityAccountStatusWeekWindowSeconds     = 7 * 24 * 60 * 60
	antigravityAccountStatusMonthWindowSeconds    = 30 * 24 * 60 * 60
	antigravityAccountStatusWindowToleranceMS     = 60 * 1000
)

type antigravityAccountStatusWindowCostSpec struct {
	targetProvider string
	windowType     string
	seconds        int64
	resetAtMS      int64
	usedPercent    *float64
}

func (s *Service) refreshAntigravityAccountWindowCosts(ctx context.Context, items []model.AntigravityAccountStatusItem, targetProvider string) {
	if len(items) == 0 {
		return
	}
	prices, err := s.store.LoadModelPrices(ctx)
	if err != nil {
		log.Printf("[antigravity-account-status] load model prices for account window cost failed: %v", err)
		return
	}
	targetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude)
	now := time.Now().UnixMilli()
	for _, item := range items {
		target := model.CodexAccountWindowCostTarget{
			AccountKey:     item.AccountKey,
			AuthIndex:      item.AuthIndex,
			AccountID:      item.AccountID,
			DisplayAccount: item.DisplayAccount,
			FileName:       item.FileName,
		}
		for _, spec := range antigravityAccountWindowCostSpecs(item, targetProvider, now) {
			s.refreshAntigravityAccountWindowCost(ctx, target, spec, prices, now)
		}
	}
}

func (s *Service) refreshAntigravityAccountWindowCost(ctx context.Context, target model.CodexAccountWindowCostTarget, spec antigravityAccountStatusWindowCostSpec, prices map[string]model.ModelPrice, now int64) {
	if target.AccountKey == "" || spec.resetAtMS <= 0 || spec.seconds <= 0 {
		return
	}
	windowStartAtMS := spec.resetAtMS - spec.seconds*1000
	windowEndAtMS := now
	if windowEndAtMS > spec.resetAtMS {
		windowEndAtMS = spec.resetAtMS
	}
	if windowEndAtMS <= windowStartAtMS {
		return
	}
	queryStartAtMS := windowStartAtMS - antigravityAccountStatusWindowToleranceMS
	if queryStartAtMS < 0 {
		queryStartAtMS = 0
	}
	aggregates, err := s.store.SumCodexAccountUsageByWindow(ctx, target, queryStartAtMS, windowEndAtMS)
	if err != nil {
		log.Printf("[antigravity-account-status] sum account window usage failed account_key=%q target_provider=%s window=%s error=%v", target.AccountKey, spec.targetProvider, spec.windowType, err)
		return
	}
	estimatedCost := 0.0
	inputTokens := int64(0)
	outputTokens := int64(0)
	cachedTokens := int64(0)
	for _, aggregate := range aggregates {
		if !antigravityUsageModelMatchesProvider(aggregate.Model, spec.targetProvider) {
			continue
		}
		inputTokens += aggregate.InputTokens
		outputTokens += aggregate.OutputTokens
		cachedTokens += aggregate.CachedTokens
		estimatedCost += pricing.CostForModelWithServiceTier(aggregate.Model, aggregate.ServiceTier, pricing.ModelTokens{
			InputTokens:         aggregate.InputTokens,
			OutputTokens:        aggregate.OutputTokens,
			CachedTokens:        aggregate.CachedTokens,
			CacheReadTokens:     aggregate.CacheReadTokens,
			CacheCreationTokens: aggregate.CacheCreationTokens,
		}, prices)
	}
	exhausted := spec.usedPercent != nil && *spec.usedPercent >= 100
	if err := s.store.UpsertAntigravityAccountWindowCost(ctx, model.AntigravityAccountWindowCost{
		AccountKey:       target.AccountKey,
		TargetProvider:   spec.targetProvider,
		WindowType:       spec.windowType,
		WindowStartAtMS:  windowStartAtMS,
		WindowResetAtMS:  spec.resetAtMS,
		EstimatedCost:    estimatedCost,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		CachedTokens:     cachedTokens,
		IsQuotaExhausted: exhausted,
		CalculatedAtMS:   now,
	}); err != nil {
		log.Printf("[antigravity-account-status] upsert account window cost failed account_key=%q target_provider=%s window=%s error=%v", target.AccountKey, spec.targetProvider, spec.windowType, err)
	}
}

func antigravityAccountWindowCostSpecs(item model.AntigravityAccountStatusItem, targetProvider string, nowMS int64) []antigravityAccountStatusWindowCostSpec {
	targetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude)
	specs := make([]antigravityAccountStatusWindowCostSpec, 0, len(item.QuotaWindows)+3)
	seen := map[string]struct{}{}
	add := func(windowType string, seconds int64, resetAtMS int64, usedPercent *float64) {
		if windowType == "" || seconds <= 0 || resetAtMS <= 0 {
			return
		}
		key := windowType + "\x00" + strconv.FormatInt(resetAtMS, 10)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		specs = append(specs, antigravityAccountStatusWindowCostSpec{targetProvider: targetProvider, windowType: windowType, seconds: seconds, resetAtMS: resetAtMS, usedPercent: usedPercent})
	}
	for _, window := range item.QuotaWindows {
		if antigravityQuotaWindowProvider(window, targetProvider) != targetProvider {
			continue
		}
		windowType, seconds := antigravityQuotaWindowType(window, targetProvider, item.CheckedAtMS, nowMS)
		add(windowType, seconds, window.ResetAtMS, window.UsedPercent)
	}
	if targetProvider == model.AntigravityTargetProviderClaude {
		add("weekly", antigravityAccountStatusWeekWindowSeconds, item.WeeklyResetAtMS, item.WeeklyUsedPercent)
	}
	if targetProvider == model.AntigravityTargetProviderGemini {
		add("monthly", antigravityAccountStatusMonthWindowSeconds, item.MonthlyResetAtMS, item.MonthlyUsedPercent)
	}
	if len(specs) == 0 && item.ResetAtMS > 0 {
		windowType := "weekly"
		seconds := int64(antigravityAccountStatusWeekWindowSeconds)
		if targetProvider == model.AntigravityTargetProviderGemini {
			windowType = "monthly"
			seconds = antigravityAccountStatusMonthWindowSeconds
		}
		add(windowType, seconds, item.ResetAtMS, item.UsedPercent)
	}
	return specs
}

func filterAntigravityWindowCosts(item model.AntigravityAccountStatusItem, costs []model.AntigravityAccountWindowCost, targetProvider string) []model.AntigravityAccountWindowCost {
	if len(costs) == 0 {
		return nil
	}
	valid := map[string]struct{}{}
	for _, spec := range antigravityAccountWindowCostSpecs(item, targetProvider, time.Now().UnixMilli()) {
		valid[spec.windowType+"\x00"+strconv.FormatInt(spec.resetAtMS, 10)] = struct{}{}
	}
	if len(valid) == 0 {
		return costs
	}
	out := make([]model.AntigravityAccountWindowCost, 0, len(costs))
	for _, cost := range costs {
		if _, ok := valid[cost.WindowType+"\x00"+strconv.FormatInt(cost.WindowResetAtMS, 10)]; ok {
			out = append(out, cost)
		}
	}
	return out
}

func antigravityQuotaWindowProvider(window model.AntigravityInspectionQuotaWindow, fallback string) string {
	text := strings.ToLower(strings.TrimSpace(window.ID + " " + window.LabelKey + " " + window.ResetLabel))
	if strings.Contains(text, "gemini") {
		return model.AntigravityTargetProviderGemini
	}
	if strings.Contains(text, "claude") || strings.Contains(text, "opus") || strings.Contains(text, "sonnet") || strings.Contains(text, "gpt-oss") {
		return model.AntigravityTargetProviderClaude
	}
	return model.NormalizeAntigravityTargetProvider(fallback, model.AntigravityTargetProviderClaude)
}

func antigravityUsageModelMatchesProvider(modelName string, targetProvider string) bool {
	text := strings.ToLower(strings.TrimSpace(modelName))
	switch model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude) {
	case model.AntigravityTargetProviderGemini:
		return strings.Contains(text, "gemini")
	case model.AntigravityTargetProviderClaude:
		return strings.Contains(text, "claude") || strings.Contains(text, "opus") || strings.Contains(text, "sonnet") || strings.Contains(text, "gpt-oss")
	}
	return true
}

func antigravityQuotaWindowType(window model.AntigravityInspectionQuotaWindow, targetProvider string, checkedAtMS int64, nowMS int64) (string, int64) {
	if window.LimitWindowSeconds != nil && *window.LimitWindowSeconds > 0 {
		seconds := int64(*window.LimitWindowSeconds)
		return antigravityWindowTypeBySeconds(seconds)
	}
	text := strings.ToLower(strings.TrimSpace(window.ID + " " + window.LabelKey + " " + window.ResetLabel))
	switch {
	case strings.Contains(text, "five") || strings.Contains(text, "5") || strings.Contains(text, "hour"):
		return "five_hour", antigravityAccountStatusFiveHourWindowSeconds
	case strings.Contains(text, "month") || strings.Contains(text, "monthly") || strings.Contains(text, "gemini"):
		return "monthly", antigravityAccountStatusMonthWindowSeconds
	case strings.Contains(text, "week") || strings.Contains(text, "weekly") || strings.Contains(text, "claude"):
		return "weekly", antigravityAccountStatusWeekWindowSeconds
	}
	if checkedAtMS <= 0 {
		checkedAtMS = nowMS
	}
	if checkedAtMS > 0 && window.ResetAtMS > checkedAtMS {
		return antigravityWindowTypeBySeconds((window.ResetAtMS - checkedAtMS) / 1000)
	}
	if model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude) == model.AntigravityTargetProviderGemini {
		return "monthly", antigravityAccountStatusMonthWindowSeconds
	}
	return "weekly", antigravityAccountStatusWeekWindowSeconds
}

func antigravityWindowTypeBySeconds(seconds int64) (string, int64) {
	if seconds <= 6*60*60 {
		return "five_hour", antigravityAccountStatusFiveHourWindowSeconds
	}
	if seconds <= 8*24*60*60 {
		return "weekly", antigravityAccountStatusWeekWindowSeconds
	}
	return "monthly", antigravityAccountStatusMonthWindowSeconds
}
