package wxaiinspection

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type wxaiServerInspectionSelection struct {
	inspectionAccounts          []account
	disabledAccounts            []account
	botFlaggedAccounts          []account
	quotaCooldownAccounts       []account
	cooldownUntilByAccountKeyMS map[string]int64
}

func isWxaiServerAccountDisabled(currentAccount account) bool {
	return currentAccount.Priority != nil && *currentAccount.Priority == wxaiDisabledPriorityValue
}

func isWxaiBotFlaggedAccount(currentAccount account) bool {
	return isWxaiBotFlaggedPriority(currentAccount.Priority)
}

func isWxaiInspectionExcluded(currentAccount account) bool {
	return isWxaiServerAccountDisabled(currentAccount) || isWxaiBotFlaggedAccount(currentAccount)
}

func (service *Service) resolveWxaiServerInspectionSelection(
	ctx context.Context,
	accounts []account,
	now time.Time,
) (wxaiServerInspectionSelection, error) {
	selection := wxaiServerInspectionSelection{
		inspectionAccounts:          make([]account, 0, len(accounts)),
		disabledAccounts:            make([]account, 0),
		botFlaggedAccounts:          make([]account, 0),
		quotaCooldownAccounts:       make([]account, 0),
		cooldownUntilByAccountKeyMS: make(map[string]int64),
	}

	for _, currentAccount := range accounts {
		if isWxaiServerAccountDisabled(currentAccount) {
			selection.disabledAccounts = append(selection.disabledAccounts, currentAccount)
			continue
		}
		if isWxaiBotFlaggedAccount(currentAccount) {
			selection.botFlaggedAccounts = append(selection.botFlaggedAccounts, currentAccount)
			continue
		}
		cooldownUntilMS, cooldownActive, err := service.resolveWxaiQuotaCooldown(ctx, currentAccount, now)
		if err != nil {
			return wxaiServerInspectionSelection{}, err
		}
		if !cooldownActive {
			selection.inspectionAccounts = append(selection.inspectionAccounts, currentAccount)
			continue
		}
		selection.quotaCooldownAccounts = append(selection.quotaCooldownAccounts, currentAccount)
		selection.cooldownUntilByAccountKeyMS[currentAccount.Key] = cooldownUntilMS
	}
	return selection, nil
}

func (service *Service) resolveWxaiQuotaCooldown(
	ctx context.Context,
	currentAccount account,
	now time.Time,
) (int64, bool, error) {
	adjustment, exists, err := service.store.GetWxaiPriorityAdjustment(ctx, currentAccount.Key)
	if err != nil {
		return 0, false, err
	}
	if !exists || adjustment.AdjustedPriority != wxaiQuotaPriorityValue || adjustment.RecoverAtMS <= now.UnixMilli() {
		return 0, false, nil
	}
	return adjustment.RecoverAtMS, true, nil
}

func (service *Service) filterWxaiQuotaCooldownAccounts(
	ctx context.Context,
	accounts []account,
	now time.Time,
) ([]account, map[string]int64, error) {
	eligibleAccounts := make([]account, 0, len(accounts))
	cooldownUntilByAccountKeyMS := make(map[string]int64)
	for _, currentAccount := range accounts {
		cooldownUntilMS, cooldownActive, err := service.resolveWxaiQuotaCooldown(ctx, currentAccount, now)
		if err != nil {
			return nil, nil, err
		}
		if cooldownActive {
			cooldownUntilByAccountKeyMS[currentAccount.Key] = cooldownUntilMS
			continue
		}
		eligibleAccounts = append(eligibleAccounts, currentAccount)
	}
	return eligibleAccounts, cooldownUntilByAccountKeyMS, nil
}

func (service *Service) loadLatestWxaiAccountStatusItems(ctx context.Context) ([]model.WxaiAccountStatusItem, error) {
	latestRun, exists, err := service.store.GetLatestWxaiInspectionRun(ctx)
	if err != nil || !exists {
		return nil, err
	}
	return service.store.ListWxaiAccountStatusItems(ctx, latestRun.ID)
}

func (service *Service) preserveWxaiServerInspectionAccounts(
	ctx context.Context,
	runID int64,
	accounts []account,
	previousItems []model.WxaiAccountStatusItem,
	logger runLogger,
) []model.WxaiInspectionResult {
	previousItemsByAccountKey := make(map[string]model.WxaiAccountStatusItem, len(previousItems))
	for _, previousItem := range previousItems {
		previousItemsByAccountKey[previousItem.AccountKey] = previousItem
	}

	persistContext := context.WithoutCancel(ctx)
	results := make([]model.WxaiInspectionResult, 0, len(accounts))
	for _, currentAccount := range accounts {
		previousItem, hasPreviousItem := previousItemsByAccountKey[currentAccount.Key]
		result, detail := buildPreservedWxaiAccountState(runID, currentAccount, previousItem, hasPreviousItem)

		storedResult, err := service.store.InsertWxaiInspectionResult(persistContext, result)
		if err != nil {
			logger.error(persistContext, "写入未测活 wXAi 账号状态失败", map[string]any{
				"fileName": currentAccount.FileName,
				"priority": currentAccount.Priority,
				"error":    err.Error(),
			})
			continue
		}
		result.ID = storedResult.ID
		if err := service.store.UpsertWxaiAccountStatusDetail(persistContext, detail); err != nil {
			logger.error(persistContext, "写入未测活 wXAi 账号详情失败", map[string]any{
				"fileName": currentAccount.FileName,
				"priority": currentAccount.Priority,
				"error":    err.Error(),
			})
			continue
		}
		results = append(results, result)
	}

	sort.Slice(results, func(leftIndex int, rightIndex int) bool {
		if results[leftIndex].FileName == results[rightIndex].FileName {
			return results[leftIndex].DisplayAccount < results[rightIndex].DisplayAccount
		}
		return results[leftIndex].FileName < results[rightIndex].FileName
	})
	return results
}

func (service *Service) preserveWxaiQuotaCooldownAccounts(
	ctx context.Context,
	runID int64,
	accounts []account,
	previousItems []model.WxaiAccountStatusItem,
	cooldownUntilByAccountKeyMS map[string]int64,
	logger runLogger,
) []model.WxaiInspectionResult {
	previousItemsByAccountKey := make(map[string]model.WxaiAccountStatusItem, len(previousItems))
	for _, previousItem := range previousItems {
		previousItemsByAccountKey[previousItem.AccountKey] = previousItem
	}

	persistContext := context.WithoutCancel(ctx)
	results := make([]model.WxaiInspectionResult, 0, len(accounts))
	for _, currentAccount := range accounts {
		previousItem, hasPreviousItem := previousItemsByAccountKey[currentAccount.Key]
		cooldownUntilMS := cooldownUntilByAccountKeyMS[currentAccount.Key]
		result, detail := buildPreservedWxaiAccountState(runID, currentAccount, previousItem, hasPreviousItem)
		result.ID = 0
		result.Disabled = false
		result.ActionStatus = model.WxaiInspectionActionStatusSkipped
		result.IsQuota = true
		result.ErrorKind = "quota_exhausted"
		result.ActionReason = fmt.Sprintf(
			"额度耗尽冷却中，跳过巡检，冷却至 %s",
			time.UnixMilli(cooldownUntilMS).UTC().Format(time.RFC3339),
		)
		result.CreatedAtMS = time.Now().UnixMilli()

		storedResult, err := service.store.InsertWxaiInspectionResult(persistContext, result)
		if err != nil {
			logger.error(persistContext, "写入额度冷却账号状态失败", map[string]any{
				"fileName": currentAccount.FileName,
				"priority": currentAccount.Priority,
				"error":    err.Error(),
			})
			continue
		}
		result.ID = storedResult.ID

		if err := service.store.UpsertWxaiAccountStatusDetail(persistContext, detail); err != nil {
			logger.error(persistContext, "写入额度冷却账号详情失败", map[string]any{
				"fileName": currentAccount.FileName,
				"priority": currentAccount.Priority,
				"error":    err.Error(),
			})
			continue
		}
		results = append(results, result)
	}

	sort.Slice(results, func(leftIndex int, rightIndex int) bool {
		if results[leftIndex].FileName == results[rightIndex].FileName {
			return results[leftIndex].DisplayAccount < results[rightIndex].DisplayAccount
		}
		return results[leftIndex].FileName < results[rightIndex].FileName
	})
	return results
}

func buildPreservedWxaiAccountState(
	runID int64,
	currentAccount account,
	previousItem model.WxaiAccountStatusItem,
	hasPreviousItem bool,
) (model.WxaiInspectionResult, model.WxaiAccountStatusDetail) {
	nowMS := time.Now().UnixMilli()
	result := model.WxaiInspectionResult{}
	if hasPreviousItem {
		result = previousItem.WxaiInspectionResult
	}
	previousErrorDetail := result.ErrorDetail

	result.ID = 0
	result.RunID = runID
	result.AccountKey = currentAccount.Key
	result.FileName = currentAccount.FileName
	result.DisplayAccount = currentAccount.DisplayAccount
	result.AuthIndex = currentAccount.AuthIndex
	result.AccountID = currentAccount.AccountID
	result.Provider = "xai"
	result.Disabled = isWxaiServerAccountDisabled(currentAccount)
	result.Status = currentAccount.Status
	result.State = currentAccount.State
	result.Action = "keep"
	result.ActionStatus = model.WxaiInspectionActionStatusSkipped
	result.ExecutedAction = ""
	result.ActionError = ""
	result.StatusCode = nil
	result.IsQuota = false
	result.Error = ""
	result.ErrorKind = ""
	result.ErrorDetail = ""
	result.PlanType = firstNonEmpty(normalizeWxaiAccountType(currentAccount.AccountType), normalizeWxaiAccountType(result.PlanType))
	if result.Disabled {
		result.ActionReason = "账号已停用，未调用测活请求，保持停用状态"
	} else if isWxaiBotFlaggedAccount(currentAccount) {
		result.ErrorKind = "account_abnormal"
		result.ErrorDetail = previousErrorDetail
		result.ActionReason = "账号命中 bot_flag_source，priority 为 -6，永久跳过巡检"
	} else {
		result.ActionReason = "账号未参与本轮网络探测，保持当前状态"
	}
	result.CreatedAtMS = nowMS

	detail := model.WxaiAccountStatusDetail{
		RunID:       runID,
		AccountKey:  currentAccount.Key,
		Priority:    currentAccount.Priority,
		AccountType: firstNonEmpty(result.PlanType, normalizeWxaiAccountType(currentAccount.AccountType)),
		CheckedAtMS: 0,
	}
	if hasPreviousItem {
		detail.WeeklyUsedPercent = previousItem.WeeklyUsedPercent
		detail.WeeklyResetAtMS = previousItem.WeeklyResetAtMS
		detail.MonthlyUsedPercent = previousItem.MonthlyUsedPercent
		detail.MonthlyResetAtMS = previousItem.MonthlyResetAtMS
		detail.MonthlyLimitCents = previousItem.MonthlyLimitCents
		detail.MonthlyUsedCents = previousItem.MonthlyUsedCents
		detail.CheckedAtMS = previousItem.CheckedAtMS
	}
	return result, detail
}
