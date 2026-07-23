package wxaiinspection

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/pricing"
)

const (
	wxaiWeeklyWindowSeconds          = 7 * 24 * 60 * 60
	wxaiMonthlyWindowSeconds         = 30 * 24 * 60 * 60
	wxaiWindowStartToleranceMS int64 = 60 * 1000
)

type wxaiAccountWindowCostSpec struct {
	windowType    string
	windowSeconds int64
	resetAtMS     int64
	usedPercent   *float64
}

func (service *Service) RefreshAccountWindowCosts(
	ctx context.Context,
	authIndexes []string,
) error {
	targetAuthIndexes := make(map[string]struct{}, len(authIndexes))
	for _, authIndex := range authIndexes {
		normalizedAuthIndex := strings.TrimSpace(authIndex)
		if normalizedAuthIndex != "" {
			targetAuthIndexes[normalizedAuthIndex] = struct{}{}
		}
	}
	if len(targetAuthIndexes) == 0 {
		return nil
	}

	latestRun, exists, err := service.store.GetLatestWxaiInspectionRun(ctx)
	if err != nil || !exists {
		return err
	}
	items, err := service.store.ListWxaiAccountStatusItems(ctx, latestRun.ID)
	if err != nil {
		return err
	}

	refreshedAccountKeys := make(map[string]struct{}, len(targetAuthIndexes))
	for _, item := range items {
		authIndex := strings.TrimSpace(item.AuthIndex)
		if _, targeted := targetAuthIndexes[authIndex]; !targeted {
			continue
		}
		if _, refreshed := refreshedAccountKeys[item.AccountKey]; refreshed {
			continue
		}
		refreshedAccountKeys[item.AccountKey] = struct{}{}
		currentAccount := account{
			Key:            item.AccountKey,
			FileName:       item.FileName,
			DisplayAccount: item.DisplayAccount,
			AuthIndex:      authIndex,
			AccountID:      item.AccountID,
			Priority:       item.Priority,
			AccountType:    item.AccountType,
		}
		detail := model.WxaiAccountStatusDetail{
			RunID:              latestRun.ID,
			AccountKey:         item.AccountKey,
			Priority:           item.Priority,
			AccountType:        item.AccountType,
			WeeklyUsedPercent:  item.WeeklyUsedPercent,
			WeeklyResetAtMS:    item.WeeklyResetAtMS,
			MonthlyUsedPercent: item.MonthlyUsedPercent,
			MonthlyResetAtMS:   item.MonthlyResetAtMS,
			MonthlyLimitCents:  item.MonthlyLimitCents,
			MonthlyUsedCents:   item.MonthlyUsedCents,
			CheckedAtMS:        item.CheckedAtMS,
		}
		service.captureWxaiAccountWindowCosts(
			ctx,
			currentAccount,
			item.WxaiInspectionResult,
			detail,
			runLogger{},
		)
	}
	return nil
}

func (service *Service) captureWxaiAccountWindowCosts(
	ctx context.Context,
	currentAccount account,
	result model.WxaiInspectionResult,
	detail model.WxaiAccountStatusDetail,
	logger runLogger,
) {
	if result.Disabled || strings.TrimSpace(currentAccount.AuthIndex) == "" || strings.TrimSpace(result.AccountKey) == "" {
		return
	}

	prices, err := service.store.LoadModelPrices(ctx)
	if err != nil {
		logger.warning(ctx, "读取模型价格失败，跳过 wXAi 预计花费计算", map[string]any{
			"accountKey": result.AccountKey,
			"error":      err.Error(),
		})
		return
	}

	nowMS := time.Now().UnixMilli()
	target := model.WxaiAccountWindowCostTarget{
		AccountKey: result.AccountKey,
		AuthIndex:  currentAccount.AuthIndex,
	}
	windowSpecs := []wxaiAccountWindowCostSpec{
		{
			windowType:    model.WxaiAccountWindowTypeWeekly,
			windowSeconds: wxaiWeeklyWindowSeconds,
			resetAtMS:     detail.WeeklyResetAtMS,
			usedPercent:   detail.WeeklyUsedPercent,
		},
		{
			windowType:    model.WxaiAccountWindowTypeMonthly,
			windowSeconds: wxaiMonthlyWindowSeconds,
			resetAtMS:     detail.MonthlyResetAtMS,
			usedPercent:   detail.MonthlyUsedPercent,
		},
	}

	hasResetWindow := false
	for _, windowSpec := range windowSpecs {
		if windowSpec.resetAtMS <= nowMS {
			continue
		}
		hasResetWindow = true
		service.captureWxaiResetWindowCost(ctx, target, windowSpec, prices, nowMS, logger)
	}
	if hasResetWindow {
		return
	}
	service.captureWxaiPriorityCycleCost(ctx, target, currentAccount.Priority, prices, nowMS, logger)
}

func (service *Service) captureWxaiResetWindowCost(
	ctx context.Context,
	target model.WxaiAccountWindowCostTarget,
	windowSpec wxaiAccountWindowCostSpec,
	prices map[string]model.ModelPrice,
	nowMS int64,
	logger runLogger,
) {
	if windowSpec.windowSeconds <= 0 {
		return
	}
	windowStartAtMS := windowSpec.resetAtMS - windowSpec.windowSeconds*1000
	if windowStartAtMS < 0 {
		return
	}
	queryStartAtMS := windowStartAtMS - wxaiWindowStartToleranceMS
	if queryStartAtMS < 0 {
		queryStartAtMS = 0
	}

	cost, err := service.calculateWxaiWindowCost(ctx, target, queryStartAtMS, nowMS, prices)
	if err != nil {
		logger.warning(ctx, "聚合 wXAi 账号窗口用量失败", map[string]any{
			"accountKey": target.AccountKey,
			"windowType": windowSpec.windowType,
			"error":      err.Error(),
		})
		return
	}
	cost.AccountKey = target.AccountKey
	cost.WindowType = windowSpec.windowType
	cost.WindowStartAtMS = windowStartAtMS
	cost.WindowResetAtMS = windowSpec.resetAtMS
	cost.IsQuotaExhausted = windowSpec.usedPercent != nil && *windowSpec.usedPercent >= 100
	cost.CalculatedAtMS = nowMS
	service.storeWxaiWindowCost(ctx, cost, logger)
}

func (service *Service) captureWxaiPriorityCycleCost(
	ctx context.Context,
	target model.WxaiAccountWindowCostTarget,
	currentPriority *int,
	prices map[string]model.ModelPrice,
	nowMS int64,
	logger runLogger,
) {
	interval, exists, err := service.store.GetWxaiAccountPriorityInterval(ctx, target.AccountKey)
	if err != nil {
		logger.warning(ctx, "读取 wXAi priority 费用区间失败", map[string]any{
			"accountKey": target.AccountKey,
			"error":      err.Error(),
		})
		return
	}

	queryStartAtMS := int64(0)
	windowStartAtMS := int64(0)
	queryEndAtMS := nowMS
	windowEndAtMS := int64(math.MaxInt64)
	isCurrentlyManagedPriority := isWxaiManagedPriority(currentPriority)
	if exists && interval.EndedAtMS != nil && !isCurrentlyManagedPriority {
		queryStartAtMS = *interval.EndedAtMS
		windowStartAtMS = *interval.EndedAtMS
	} else {
		if exists && interval.StartedAtMS != nil {
			queryStartAtMS = *interval.StartedAtMS
			windowStartAtMS = *interval.StartedAtMS
		}
		if exists && interval.EndedAtMS != nil {
			queryEndAtMS = *interval.EndedAtMS
			windowEndAtMS = *interval.EndedAtMS
		}
	}
	if queryEndAtMS <= queryStartAtMS {
		return
	}

	cost, err := service.calculateWxaiWindowCost(ctx, target, queryStartAtMS, queryEndAtMS, prices)
	if err != nil {
		logger.warning(ctx, "聚合 wXAi priority 周期用量失败", map[string]any{
			"accountKey": target.AccountKey,
			"error":      err.Error(),
		})
		return
	}
	cost.AccountKey = target.AccountKey
	cost.WindowType = model.WxaiAccountWindowTypePriorityCycle
	cost.WindowStartAtMS = windowStartAtMS
	cost.WindowResetAtMS = windowEndAtMS
	cost.CalculatedAtMS = nowMS
	service.storeWxaiWindowCost(ctx, cost, logger)
}

func (service *Service) calculateWxaiWindowCost(
	ctx context.Context,
	target model.WxaiAccountWindowCostTarget,
	queryStartAtMS int64,
	queryEndAtMS int64,
	prices map[string]model.ModelPrice,
) (model.WxaiAccountWindowCost, error) {
	aggregates, err := service.store.SumWxaiAccountUsageByWindow(ctx, target, queryStartAtMS, queryEndAtMS)
	if err != nil {
		return model.WxaiAccountWindowCost{}, err
	}

	cost := model.WxaiAccountWindowCost{}
	for _, aggregate := range aggregates {
		cost.InputTokens += aggregate.InputTokens
		cost.OutputTokens += aggregate.OutputTokens
		cost.CachedTokens += aggregate.CachedTokens + aggregate.CacheReadTokens
		cost.EstimatedCost += pricing.CostForModelWithServiceTier(
			aggregate.Model,
			aggregate.ServiceTier,
			pricing.ModelTokens{
				InputTokens:         aggregate.InputTokens,
				OutputTokens:        aggregate.OutputTokens,
				CachedTokens:        aggregate.CachedTokens,
				CacheReadTokens:     aggregate.CacheReadTokens,
				CacheCreationTokens: aggregate.CacheCreationTokens,
			},
			prices,
		)
	}
	return cost, nil
}

func (service *Service) storeWxaiWindowCost(
	ctx context.Context,
	cost model.WxaiAccountWindowCost,
	logger runLogger,
) {
	if err := service.store.UpsertWxaiAccountWindowCost(ctx, cost); err != nil {
		logger.warning(ctx, "保存 wXAi 账号窗口预计花费失败", map[string]any{
			"accountKey": cost.AccountKey,
			"windowType": cost.WindowType,
			"error":      err.Error(),
		})
	}
}

func (service *Service) listWxaiAccountWindowCostsByAccount(
	ctx context.Context,
	runID int64,
) (map[string][]model.WxaiAccountWindowCost, error) {
	costs, err := service.store.ListWxaiAccountWindowCostsByRun(ctx, runID, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}

	costsByAccount := make(map[string][]model.WxaiAccountWindowCost)
	for _, cost := range costs {
		costsByAccount[cost.AccountKey] = append(costsByAccount[cost.AccountKey], cost)
	}
	return costsByAccount, nil
}
