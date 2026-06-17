package codexinspection

import (
	"context"
	"log"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/pricing"
)

const codexInspectionWindowStartToleranceMS = 60 * 1000

type accountWindowCostSpec struct {
	windowType string
	seconds    int
	window     *accountStatusWindow
}

func (s *Service) captureCodexAccountWindowCosts(ctx context.Context, item account, payload map[string]any, planType string, logger runLogger) {
	if item.Key == "" || payload == nil {
		return
	}

	rateLimit := readMap(payload, "rate_limit", "rateLimit")
	primary := parseAccountStatusWindow(readMap(rateLimit, "primary_window", "primaryWindow"))
	secondary := parseAccountStatusWindow(readMap(rateLimit, "secondary_window", "secondaryWindow"))
	fiveHour, weekly, monthly := classifyAccountStatusWindows(primary, secondary, planType)
	windows := []accountWindowCostSpec{
		{windowType: "five_hour", seconds: codexFiveHourWindow, window: fiveHour},
		{windowType: "weekly", seconds: codexWeekWindow, window: weekly},
		{windowType: "monthly", seconds: codexMonthWindow, window: monthly},
	}

	prices, err := s.store.LoadModelPrices(ctx)
	if err != nil {
		logger.warning(ctx, "计算账号窗口预计花费失败", map[string]any{
			"accountKey": item.Key,
			"error":      err.Error(),
		})
		log.Printf("[codex-inspection] load model prices for account window cost failed account_key=%q error=%v", item.Key, err)
		return
	}

	now := time.Now().UnixMilli()
	target := model.CodexAccountWindowCostTarget{
		AccountKey:     item.Key,
		AuthIndex:      item.AuthIndex,
		AccountID:      item.AccountID,
		DisplayAccount: item.DisplayAccount,
		FileName:       item.FileName,
	}
	for _, spec := range windows {
		if spec.window == nil || spec.window.ResetAtMS <= 0 || spec.seconds <= 0 {
			continue
		}
		windowStartAtMS := spec.window.ResetAtMS - int64(spec.seconds)*1000
		windowEndAtMS := now
		if windowEndAtMS > spec.window.ResetAtMS {
			windowEndAtMS = spec.window.ResetAtMS
		}
		if windowEndAtMS <= windowStartAtMS {
			continue
		}

		queryStartAtMS := windowStartAtMS - codexInspectionWindowStartToleranceMS
		if queryStartAtMS < 0 {
			queryStartAtMS = 0
		}
		aggregates, err := s.store.SumCodexAccountUsageByWindow(ctx, target, queryStartAtMS, windowEndAtMS)
		if err != nil {
			logger.warning(ctx, "计算账号窗口预计花费失败", map[string]any{
				"accountKey": item.Key,
				"windowType": spec.windowType,
				"error":      err.Error(),
			})
			log.Printf("[codex-inspection] sum account window usage failed account_key=%q window=%s error=%v", item.Key, spec.windowType, err)
			continue
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

		exhausted := spec.window.UsedPercent != nil && *spec.window.UsedPercent >= 100
		if err := s.store.UpsertCodexAccountWindowCost(ctx, model.CodexAccountWindowCost{
			AccountKey:       item.Key,
			WindowType:       spec.windowType,
			WindowStartAtMS:  windowStartAtMS,
			WindowResetAtMS:  spec.window.ResetAtMS,
			EstimatedCost:    estimatedCost,
			IsQuotaExhausted: exhausted,
			CalculatedAtMS:   now,
		}); err != nil {
			logger.warning(ctx, "保存账号窗口预计花费失败", map[string]any{
				"accountKey": item.Key,
				"windowType": spec.windowType,
				"error":      err.Error(),
			})
			log.Printf("[codex-inspection] upsert account window cost failed account_key=%q window=%s error=%v", item.Key, spec.windowType, err)
		}
	}
}
