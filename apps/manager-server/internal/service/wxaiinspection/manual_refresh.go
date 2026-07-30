package wxaiinspection

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

// RunManualRefresh 独立于服务器/条件巡检全局锁，任意时刻可执行。
// 探测顺序：billing 元数据成功后，统一经 CPA proxy-url（含 socks5）调用 POST /v1/responses。
func (service *Service) RunManualRefresh(ctx context.Context, request ManualRefreshRequest) (RunDetail, error) {
	settings, setup, err := service.resolveRuntime(ctx)
	if err != nil {
		return RunDetail{}, err
	}
	if err := validateWxaiPriorityOnlyMode(settings); err != nil {
		return RunDetail{}, err
	}
	run, err := service.requireLatestManualRefreshRun(ctx)
	if err != nil {
		return RunDetail{}, err
	}
	accounts, err := service.fetchAccounts(ctx, setup)
	if err != nil {
		return RunDetail{}, err
	}
	selected, matched := matchAccount(accounts, request)
	if !matched {
		return RunDetail{}, fmt.Errorf(
			"%w: %s",
			ErrManualRefreshAccountNotFound,
			firstNonEmpty(request.AccountKey, request.FileName, request.AuthIndex),
		)
	}
	logger := runLogger{service: service, runID: run.ID, prefix: "【wXAi 手动刷新】 "}
	httpClientRuntime, err := service.resolveWxaiHTTPClient(ctx, setup)
	if err != nil {
		return RunDetail{}, err
	}
	logger.info(ctx, "wXAi 请求代理已配置", buildWxaiProxyLogDetail(httpClientRuntime.proxySummary, 1))
	logger.info(ctx, "wXAi 手动刷新开始", map[string]any{
		"accountKey":     selected.Key,
		"fileName":       selected.FileName,
		"authIndex":      selected.AuthIndex,
		"displayAccount": selected.DisplayAccount,
		"reason":         strings.TrimSpace(request.Reason),
		"runID":          run.ID,
	})

	result, effectivePriority := service.inspectManualRefreshAccount(
		ctx,
		setup,
		settings,
		httpClientRuntime.client,
		httpClientRuntime.clientVersion,
		run.ID,
		selected,
		logger,
	)
	selected.Priority = effectivePriority
	service.persistManualRefreshResult(ctx, run.ID, selected, result, logger)
	logger.info(context.WithoutCancel(ctx), "wXAi 手动刷新完成", map[string]any{
		"fileName":     selected.FileName,
		"actionReason": result.ActionReason,
		"errorKind":    result.ErrorKind,
		"statusCode":   result.StatusCode,
		"priority":     effectivePriority,
	})
	return service.GetRun(context.WithoutCancel(ctx), run.ID)
}

func (service *Service) requireLatestManualRefreshRun(ctx context.Context) (model.WxaiInspectionRun, error) {
	run, exists, err := service.store.GetLatestWxaiInspectionRun(ctx)
	if err != nil {
		return model.WxaiInspectionRun{}, err
	}
	if !exists || run.ID <= 0 {
		return model.WxaiInspectionRun{}, ErrManualRefreshRequiresServerRun
	}
	return run, nil
}

func (service *Service) inspectManualRefreshAccount(
	ctx context.Context,
	setup store.Setup,
	settings model.ManagerWxaiInspectionConfig,
	xaiClient *http.Client,
	xaiClientVersion string,
	runID int64,
	currentAccount account,
	logger runLogger,
) (model.WxaiInspectionResult, *int) {
	ctx = withWxaiInspectionRequestMetadata(ctx, runID, currentAccount)
	inspectionTime := time.Now()
	result := model.WxaiInspectionResult{
		RunID:          runID,
		AccountKey:     currentAccount.Key,
		FileName:       currentAccount.FileName,
		DisplayAccount: currentAccount.DisplayAccount,
		AuthIndex:      currentAccount.AuthIndex,
		AccountID:      currentAccount.AccountID,
		Provider:       "xai",
		Disabled:       isWxaiServerAccountDisabled(currentAccount),
		Status:         currentAccount.Status,
		State:          currentAccount.State,
		Action:         "keep",
		ActionReason:   "xAI 手动刷新成功",
		ActionStatus:   model.WxaiInspectionActionStatusNone,
		PlanType:       firstNonEmpty(normalizeWxaiAccountType(currentAccount.AccountType), wxaiAccountTypeUnknown),
		CreatedAtMS:    inspectionTime.UnixMilli(),
	}

	authFile, err := cpaauthfiles.New(service.client).DownloadJSON(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
		currentAccount.FileName,
	)
	if err != nil {
		return service.applyWxaiProbeFailure(
			ctx,
			setup,
			currentAccount,
			result,
			wxaiRequestFailure("download auth file: "+err.Error()),
			inspectionTime,
			logger,
		)
	}
	accessToken := strings.TrimSpace(firstString(authFile, "access_token"))
	if accessToken == "" {
		return service.applyWxaiProbeFailure(
			ctx,
			setup,
			currentAccount,
			result,
			wxaiAccountFailure(0, "access-token-missing"),
			inspectionTime,
			logger,
		)
	}

	botFlagInspection, err := inspectWxaiBotFlagSource(accessToken)
	if err != nil {
		return service.applyWxaiProbeFailure(
			ctx,
			setup,
			currentAccount,
			result,
			wxaiAccountFailure(0, "decode access_token JWT: "+err.Error()),
			inspectionTime,
			logger,
		)
	}
	if botFlagInspection.Flagged {
		return service.applyWxaiBotFlagFailure(
			ctx,
			setup,
			currentAccount,
			result,
			botFlagInspection.NormalizedValue,
			logger,
		)
	}

	billingUserID := resolveWxaiBillingUserID(authFile, currentAccount.AccountID)
	billingSnapshot := wxaiBillingSnapshot{}
	monthlyBillingProbed := false
	if wxaiAccountTypeNeedsResolution(result.PlanType) {
		result.PlanType = wxaiAccountTypeUnknown
		monthlySnapshot, monthlyOutcome := service.probeWxaiMonthlyBilling(
			ctx,
			setup,
			settings.Timeout,
			currentAccount.AuthIndex,
			billingUserID,
			logger,
		)
		monthlyBillingProbed = true
		if !monthlyOutcome.Alive {
			service.persistResolvedWxaiAccountType(ctx, currentAccount, wxaiAccountTypeUnknown, logger)
			return service.applyWxaiProbeFailure(
				ctx,
				setup,
				currentAccount,
				result,
				monthlyOutcome,
				inspectionTime,
				logger,
			)
		}
		mergeWxaiBillingSnapshot(&billingSnapshot, monthlySnapshot)
		result.PlanType = resolveWxaiAccountType(monthlySnapshot.MonthlyLimitCents)
		service.persistResolvedWxaiAccountType(ctx, currentAccount, result.PlanType, logger)
	}

	billingOutcome := wxaiProbeOutcome{}
	if normalizeWxaiAccountType(result.PlanType) == wxaiAccountTypeSuper {
		if monthlyBillingProbed {
			creditsSnapshot, creditsOutcome := service.probeWxaiCreditsBilling(
				ctx,
				setup,
				settings.Timeout,
				currentAccount.AuthIndex,
				billingUserID,
				logger,
			)
			mergeWxaiBillingSnapshot(&billingSnapshot, creditsSnapshot)
			billingOutcome = creditsOutcome
		} else {
			superSnapshot, superOutcome := service.probeWxaiSuperBilling(
				ctx,
				setup,
				settings.Timeout,
				currentAccount.AuthIndex,
				billingUserID,
				logger,
			)
			mergeWxaiBillingSnapshot(&billingSnapshot, superSnapshot)
			billingOutcome = superOutcome
		}
	} else {
		creditsSnapshot, creditsOutcome := service.probeWxaiCreditsBilling(
			ctx,
			setup,
			settings.Timeout,
			currentAccount.AuthIndex,
			billingUserID,
			logger,
		)
		mergeWxaiBillingSnapshot(&billingSnapshot, creditsSnapshot)
		billingOutcome = creditsOutcome
	}
	applyWxaiBillingSnapshot(&result, billingSnapshot)
	if !billingOutcome.Alive {
		return service.applyWxaiProbeFailure(
			ctx,
			setup,
			currentAccount,
			result,
			billingOutcome,
			inspectionTime,
			logger,
		)
	}

	healthOutcome := service.probeWxaiResponsesOnly(
		ctx,
		xaiClient,
		settings.Timeout,
		accessToken,
		xaiClientVersion,
	)
	if !healthOutcome.Alive {
		return service.applyWxaiProbeFailure(
			ctx,
			setup,
			currentAccount,
			result,
			healthOutcome,
			inspectionTime,
			logger,
		)
	}

	result.StatusCode = intPointer(healthOutcome.StatusCode)
	result.ErrorKind = ""
	result.ErrorDetail = ""
	result.ActionReason = "xAI 手动刷新 billing+responses 成功"
	effectivePriority, restoreErr := service.restoreWxaiPriority(ctx, setup, currentAccount, result.PlanType, logger)
	if restoreErr != nil {
		applyWxaiPriorityError(&result, "priority_restore_failed", restoreErr)
		result.ActionReason += "；priority 恢复失败"
	}
	return result, effectivePriority
}

func (service *Service) persistManualRefreshResult(
	ctx context.Context,
	runID int64,
	currentAccount account,
	result model.WxaiInspectionResult,
	logger runLogger,
) {
	persistContext := context.WithoutCancel(ctx)
	storedResult, err := service.store.InsertWxaiInspectionResult(persistContext, result)
	if err != nil {
		logger.error(persistContext, "写入 wXAi 手动刷新结果失败", map[string]any{
			"fileName": currentAccount.FileName,
			"error":    err.Error(),
		})
		return
	}
	result.ID = storedResult.ID
	statusDetail, detailErr := service.writeAccountStatusDetail(
		persistContext,
		runID,
		currentAccount,
		result,
	)
	if detailErr != nil {
		logger.error(persistContext, "写入 wXAi 账号状态详情失败", map[string]any{
			"fileName": currentAccount.FileName,
			"error":    detailErr.Error(),
		})
		return
	}
	service.captureWxaiAccountWindowCosts(
		persistContext,
		currentAccount,
		result,
		statusDetail,
		logger,
	)
}
