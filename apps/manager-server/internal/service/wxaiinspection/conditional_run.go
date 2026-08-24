package wxaiinspection

import (
	"context"
	"fmt"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type ConditionalRunRequest struct {
	RunID int64
}

func (service *Service) RunConditional(ctx context.Context, request ConditionalRunRequest) (RunDetail, error) {
	if request.RunID <= 0 {
		return RunDetail{}, ErrRunNotFound
	}
	if !service.acquireRun() {
		return RunDetail{}, ErrRunAlreadyActive
	}
	defer service.releaseRun()

	settings, setup, err := service.resolveRuntime(ctx)
	if err != nil {
		return RunDetail{}, err
	}
	if settings.Enabled == nil || !*settings.Enabled {
		return service.GetRun(ctx, request.RunID)
	}
	if err := validateWxaiPriorityOnlyMode(settings); err != nil {
		return RunDetail{}, err
	}

	detail, err := service.GetRun(ctx, request.RunID)
	if err != nil {
		return RunDetail{}, err
	}
	if detail.Run.Status == model.WxaiInspectionStatusRunning {
		return detail, ErrRunAlreadyActive
	}

	persistContext := context.WithoutCancel(ctx)
	logger := runLogger{service: service, runID: request.RunID, prefix: "【wXAi 条件巡检】 "}
	quietLogger := runLogger{}
	accounts, err := service.fetchAccounts(ctx, setup)
	if err != nil {
		logger.error(persistContext, "加载 wXAi 认证文件失败", map[string]any{"error": err.Error()})
		return service.getConditionalRunWithCause(persistContext, request.RunID, err)
	}
	if len(accounts) == 0 {
		return service.GetRun(persistContext, request.RunID)
	}

	selectedAccounts, reasonsByKey, err := service.resolveConditionalAccounts(ctx, request.RunID, accounts, time.Now().UnixMilli(), quietLogger)
	if err != nil {
		logger.error(persistContext, "解析 wXAi 条件候选失败", map[string]any{"error": err.Error()})
		return service.getConditionalRunWithCause(persistContext, request.RunID, err)
	}
	if len(selectedAccounts) == 0 {
		return service.GetRun(persistContext, request.RunID)
	}
	selectedAccounts, cooldownUntilByAccountKeyMS, err := service.filterWxaiQuotaCooldownAccounts(ctx, selectedAccounts, time.Now())
	if err != nil {
		logger.error(persistContext, "过滤 wXAi 条件巡检冷却账号失败", map[string]any{"error": err.Error()})
		return service.getConditionalRunWithCause(persistContext, request.RunID, err)
	}
	for accountKey, cooldownUntilMS := range cooldownUntilByAccountKeyMS {
		logger.info(persistContext, "账号处于额度冷却期，跳过条件巡检", map[string]any{
			"accountKey":      accountKey,
			"cooldownUntilMs": cooldownUntilMS,
		})
	}
	if len(selectedAccounts) == 0 {
		return service.GetRun(persistContext, request.RunID)
	}
	results := service.inspectAccounts(
		ctx,
		setup,
		settings,
		request.RunID,
		selectedAccounts,
		quietLogger,
	)
	if ctxErr := ctx.Err(); ctxErr != nil {
		logger.warning(persistContext, "条件巡检已取消", map[string]any{
			"error":          ctxErr.Error(),
			"candidateCount": len(selectedAccounts),
			"finishedCount":  len(results),
		})
		return service.getConditionalRunWithCause(persistContext, request.RunID, ctxErr)
	}
	for _, result := range results {
		logWxaiConditionalResult(persistContext, logger, result, reasonsByKey[result.AccountKey])
	}
	return service.GetRun(persistContext, request.RunID)
}

func logWxaiConditionalResult(ctx context.Context, logger runLogger, result model.WxaiInspectionResult, reasons []string) {
	level := "info"
	if result.Error != "" || result.ErrorKind == "account_abnormal" || result.ErrorKind == "request_error" {
		level = "warning"
	}
	logger.log(ctx, level, "账号刷新完成", map[string]any{
		"fileName":       result.FileName,
		"displayAccount": result.DisplayAccount,
		"authIndex":      result.AuthIndex,
		"accountId":      result.AccountID,
		"reasons":        reasons,
		"action":         result.Action,
		"actionReason":   result.ActionReason,
		"statusCode":     result.StatusCode,
		"usedPercent":    result.UsedPercent,
		"isQuota":        result.IsQuota,
		"error":          result.Error,
	})
}

func (service *Service) getConditionalRunWithCause(ctx context.Context, runID int64, cause error) (RunDetail, error) {
	detail, err := service.GetRun(ctx, runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("%w; load run: %v", cause, err)
	}
	return detail, cause
}
