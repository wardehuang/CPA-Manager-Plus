package wxaiinspection

import (
	"context"
	"sort"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func (service *Service) preserveWxaiRealtimeDegradationCooldownAccounts(
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
		result.IsQuota = false
		result.ErrorKind = "position_degradation"
		result.ActionReason = "位置降智冷却中，跳过服务器巡检，冷却至 " + time.UnixMilli(cooldownUntilMS).UTC().Format(time.RFC3339)
		result.CreatedAtMS = time.Now().UnixMilli()

		storedResult, err := service.store.InsertWxaiInspectionResult(persistContext, result)
		if err != nil {
			logger.error(persistContext, "写入实时降智冷却账号状态失败", map[string]any{
				"fileName": currentAccount.FileName,
				"priority": currentAccount.Priority,
				"error":    err.Error(),
			})
			continue
		}
		result.ID = storedResult.ID
		if err := service.store.UpsertWxaiAccountStatusDetail(persistContext, detail); err != nil {
			logger.error(persistContext, "写入实时降智冷却账号详情失败", map[string]any{
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
