package codexaccountstatus

import (
	"context"
	"log"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/pricing"
)

const (
	codexAccountStatusFiveHourWindowSeconds  = 5 * 60 * 60
	codexAccountStatusWeekWindowSeconds      = 7 * 24 * 60 * 60
	codexAccountStatusMonthWindowSeconds     = 30 * 24 * 60 * 60
	codexAccountStatusWindowStartToleranceMS = 60 * 1000
)

type codexAccountStatusWindowCostSpec struct {
	windowType  string
	seconds     int64
	resetAtMS   int64
	usedPercent *float64
}

func (s *Service) refreshCodexAccountWindowCosts(ctx context.Context, items []model.CodexAccountStatusItem) {
	if len(items) == 0 {
		return
	}
	prices, err := s.store.LoadModelPrices(ctx)
	if err != nil {
		log.Printf("[codex-account-status] load model prices for account window cost failed: %v", err)
		return
	}
	now := time.Now().UnixMilli()
	for _, item := range items {
		target := model.CodexAccountWindowCostTarget{
			AccountKey:     item.AccountKey,
			AuthIndex:      item.AuthIndex,
			AccountID:      item.AccountID,
			DisplayAccount: item.DisplayAccount,
			FileName:       item.FileName,
		}
		windows := []codexAccountStatusWindowCostSpec{
			{windowType: "five_hour", seconds: codexAccountStatusFiveHourWindowSeconds, resetAtMS: item.FiveHourResetAtMS, usedPercent: item.FiveHourUsedPercent},
			{windowType: "weekly", seconds: codexAccountStatusWeekWindowSeconds, resetAtMS: item.WeeklyResetAtMS, usedPercent: item.WeeklyUsedPercent},
			{windowType: "monthly", seconds: codexAccountStatusMonthWindowSeconds, resetAtMS: item.MonthlyResetAtMS, usedPercent: item.MonthlyUsedPercent},
		}
		for _, spec := range windows {
			s.refreshCodexAccountWindowCost(ctx, target, spec, prices, now)
		}
	}
}

func (s *Service) refreshCodexAccountWindowCost(ctx context.Context, target model.CodexAccountWindowCostTarget, spec codexAccountStatusWindowCostSpec, prices map[string]model.ModelPrice, now int64) {
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
	queryStartAtMS := windowStartAtMS - codexAccountStatusWindowStartToleranceMS
	if queryStartAtMS < 0 {
		queryStartAtMS = 0
	}
	aggregates, err := s.store.SumCodexAccountUsageByWindow(ctx, target, queryStartAtMS, windowEndAtMS)
	if err != nil {
		log.Printf("[codex-account-status] sum account window usage failed account_key=%q window=%s error=%v", target.AccountKey, spec.windowType, err)
		return
	}
	estimatedCost := 0.0
	for _, aggregate := range aggregates {
		estimatedCost += pricing.CostForModelWithServiceTier(aggregate.Model, aggregate.ServiceTier, pricing.ModelTokens{
			InputTokens:         aggregate.InputTokens,
			OutputTokens:        aggregate.OutputTokens,
			CachedTokens:        aggregate.CachedTokens,
			CacheReadTokens:     aggregate.CacheReadTokens,
			CacheCreationTokens: aggregate.CacheCreationTokens,
		}, prices)
	}
	exhausted := spec.usedPercent != nil && *spec.usedPercent >= 100
	if err := s.store.UpsertCodexAccountWindowCost(ctx, model.CodexAccountWindowCost{
		AccountKey:       target.AccountKey,
		WindowType:       spec.windowType,
		WindowStartAtMS:  windowStartAtMS,
		WindowResetAtMS:  spec.resetAtMS,
		EstimatedCost:    estimatedCost,
		IsQuotaExhausted: exhausted,
		CalculatedAtMS:   now,
	}); err != nil {
		log.Printf("[codex-account-status] upsert account window cost failed account_key=%q window=%s error=%v", target.AccountKey, spec.windowType, err)
	}
}
