package wxaiinspection

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

var ErrUsageQuotaAccountNotFound = errors.New("wxai usage quota account not found")

type UsageQuotaExhaustedRequest struct {
	FileName               string
	DisplayAccount         string
	AuthIndex              string
	Provider               string
	StatusCode             int
	Detail                 string
	ResponseBody           string
	HeaderQuotaRecoverAtMS int64
	EventHash              string
}

type UsageQuotaExhaustedResult struct {
	Applied            bool
	AccountKey         string
	FileName           string
	AccountType        string
	RunID              int64
	RecoverAtMS        int64
	RecoverySource     string
	CreditsAttempted   bool
	CreditsRecoverAtMS int64
	CreditsError       string
	SkippedReason      string
}

// ApplyUsageQuotaExhausted consumes a quota result already proven by the
// original xAI request. FREE accounts keep the direct persistence path. SUPER
// accounts query billing credits only when the original response has no usable
// recovery time.
func (service *Service) ApplyUsageQuotaExhausted(
	ctx context.Context,
	request UsageQuotaExhaustedRequest,
) (UsageQuotaExhaustedResult, error) {
	if !service.acquireRun() {
		return UsageQuotaExhaustedResult{}, ErrRunAlreadyActive
	}
	defer service.releaseRun()

	settings, setup, err := service.resolveRuntime(ctx)
	if err != nil {
		return UsageQuotaExhaustedResult{}, err
	}
	if settings.Enabled == nil || !*settings.Enabled {
		return UsageQuotaExhaustedResult{}, nil
	}
	if err := validateWxaiPriorityOnlyMode(settings); err != nil {
		return UsageQuotaExhaustedResult{}, err
	}

	accounts, err := service.fetchAccounts(ctx, setup)
	if err != nil {
		return UsageQuotaExhaustedResult{}, fmt.Errorf("加载 wXAi 认证文件: %w", err)
	}
	matchedAccount, matched := newWxaiConditionalAccountMatcher(accounts).match(wxaiConditionalAccountRef{
		FileName:       strings.TrimSpace(request.FileName),
		DisplayAccount: strings.TrimSpace(request.DisplayAccount),
		AuthIndex:      strings.TrimSpace(request.AuthIndex),
		Provider:       normalizeWxaiProvider(request.Provider),
	})
	if !matched {
		return UsageQuotaExhaustedResult{}, fmt.Errorf(
			"%w: fileName=%q authIndex=%q event=%q",
			ErrUsageQuotaAccountNotFound,
			request.FileName,
			request.AuthIndex,
			request.EventHash,
		)
	}
	if isWxaiBotFlaggedAccount(matchedAccount) {
		return UsageQuotaExhaustedResult{
			AccountKey:    matchedAccount.Key,
			FileName:      matchedAccount.FileName,
			AccountType:   normalizeWxaiAccountType(matchedAccount.AccountType),
			SkippedReason: "priority=-6 bot account",
		}, nil
	}

	inspectionTime := time.Now()
	runID, err := service.latestReusableWxaiRunID(ctx)
	if err != nil {
		return UsageQuotaExhaustedResult{}, err
	}
	logger := runLogger{service: service, runID: runID, prefix: "【wXAi 请求额度落盘】 "}

	if wxaiAccountTypeNeedsResolution(matchedAccount.AccountType) {
		matchedAccount.AccountType = wxaiAccountTypeFree
		if err := service.persistWxaiAccountType(ctx, matchedAccount.Key, wxaiAccountTypeFree); err != nil {
			logger.warning(context.WithoutCancel(ctx), "保存 wXAi 账号类型失败", map[string]any{
				"fileName": matchedAccount.FileName,
				"error":    err.Error(),
			})
		}
	}
	quotaRecoveryResolution := service.resolveWxaiUsageQuotaRecovery(
		ctx,
		setup,
		settings,
		matchedAccount,
		runID,
		request,
		inspectionTime,
		logger,
	)
	resolvedRecovery := resolveWxaiQuotaRecovery(quotaRecoveryResolution.Recovery, inspectionTime)

	result := model.WxaiInspectionResult{
		RunID:          runID,
		AccountKey:     matchedAccount.Key,
		FileName:       matchedAccount.FileName,
		DisplayAccount: matchedAccount.DisplayAccount,
		AuthIndex:      matchedAccount.AuthIndex,
		AccountID:      matchedAccount.AccountID,
		Provider:       "xai",
		Disabled:       isWxaiServerAccountDisabled(matchedAccount),
		Status:         matchedAccount.Status,
		State:          matchedAccount.State,
		Action:         "keep",
		ActionStatus:   model.WxaiInspectionActionStatusNone,
		PlanType:       normalizeWxaiAccountType(matchedAccount.AccountType),
		CreatedAtMS:    inspectionTime.UnixMilli(),
	}
	result, effectivePriority := service.applyWxaiProbeFailure(
		ctx,
		setup,
		matchedAccount,
		result,
		wxaiProbeOutcome{
			Quota:         true,
			StatusCode:    request.StatusCode,
			ErrorKind:     "quota_exhausted",
			Detail:        request.Detail,
			QuotaRecovery: quotaRecoveryResolution.Recovery,
		},
		inspectionTime,
		logger,
	)
	matchedAccount.Priority = effectivePriority

	if runID > 0 {
		persistContext := context.WithoutCancel(ctx)
		storedResult, persistErr := service.store.InsertWxaiInspectionResult(persistContext, result)
		if persistErr != nil {
			return UsageQuotaExhaustedResult{}, fmt.Errorf("写入 wXAi 请求额度结果: %w", persistErr)
		}
		result.ID = storedResult.ID
		if _, persistErr := service.writeAccountStatusDetail(persistContext, runID, matchedAccount, result); persistErr != nil {
			return UsageQuotaExhaustedResult{}, fmt.Errorf("写入 wXAi 请求额度状态详情: %w", persistErr)
		}
		logWxaiConditionalResult(persistContext, logger, result, []string{"request_quota_exhausted"})
	}

	usageResult := UsageQuotaExhaustedResult{
		AccountKey:         matchedAccount.Key,
		FileName:           matchedAccount.FileName,
		AccountType:        normalizeWxaiAccountType(matchedAccount.AccountType),
		RunID:              runID,
		RecoverAtMS:        resolvedRecovery.recoverAtMS,
		RecoverySource:     resolvedRecovery.source,
		CreditsAttempted:   quotaRecoveryResolution.CreditsAttempted,
		CreditsRecoverAtMS: quotaRecoveryResolution.CreditsRecoverAtMS,
		CreditsError:       quotaRecoveryResolution.CreditsError,
	}
	if result.Error != "" {
		return usageResult, errors.New(result.Error)
	}
	usageResult.Applied = true
	return usageResult, nil
}

func (service *Service) latestReusableWxaiRunID(ctx context.Context) (int64, error) {
	latestRun, exists, err := service.store.GetLatestWxaiInspectionRun(ctx)
	if err != nil {
		return 0, fmt.Errorf("加载最近 wXAi 巡检 run: %w", err)
	}
	if !exists || latestRun.ID <= 0 || latestRun.Status == model.WxaiInspectionStatusRunning {
		return 0, nil
	}
	return latestRun.ID, nil
}

func quotaRecoveryFromUsageEvent(request UsageQuotaExhaustedRequest) wxaiQuotaRecovery {
	responseHeader := make(http.Header)
	if request.HeaderQuotaRecoverAtMS > 0 {
		responseHeader.Set("X-RateLimit-Reset", strconv.FormatInt(request.HeaderQuotaRecoverAtMS, 10))
	}
	return extractWxaiQuotaRecovery(wxaiHTTPResponse{
		Header: responseHeader,
		Body:   []byte(strings.TrimSpace(request.ResponseBody)),
	})
}
