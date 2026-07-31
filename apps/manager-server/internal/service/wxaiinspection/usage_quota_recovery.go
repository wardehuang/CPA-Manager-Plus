package wxaiinspection

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type wxaiUsageQuotaRecoveryResolution struct {
	Recovery               wxaiQuotaRecovery
	CreditsAttempted       bool
	CreditsError           string
	CreditsRecoverAtMS     int64
	OriginalRecoverySource string
}

func (service *Service) resolveWxaiUsageQuotaRecovery(
	ctx context.Context,
	setup store.Setup,
	settings model.ManagerWxaiInspectionConfig,
	currentAccount account,
	runID int64,
	request UsageQuotaExhaustedRequest,
	inspectionTime time.Time,
	logger runLogger,
) wxaiUsageQuotaRecoveryResolution {
	usageRecovery := quotaRecoveryFromUsageEvent(request)
	resolution := wxaiUsageQuotaRecoveryResolution{
		Recovery:               usageRecovery,
		OriginalRecoverySource: usageRecovery.source,
	}
	if normalizeWxaiAccountType(currentAccount.AccountType) != wxaiAccountTypeSuper ||
		usageRecovery.recoverAtMS > inspectionTime.UnixMilli() {
		return resolution
	}

	resolution.CreditsAttempted = true
	requestContext := withWxaiInspectionRequestMetadata(ctx, runID, currentAccount)
	authFile, err := cpaauthfiles.New(service.client).DownloadJSON(
		requestContext,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
		currentAccount.FileName,
	)
	if err != nil {
		resolution.CreditsError = "download auth file: " + err.Error()
		service.logWxaiSuperQuotaRecoveryResolution(requestContext, currentAccount, resolution, logger)
		return resolution
	}
	accessToken := strings.TrimSpace(firstString(authFile, "access_token"))
	if accessToken == "" {
		resolution.CreditsError = "access-token-missing"
		service.logWxaiSuperQuotaRecoveryResolution(requestContext, currentAccount, resolution, logger)
		return resolution
	}
	billingUserID := resolveWxaiBillingUserID(authFile, currentAccount.AccountID)

	creditsSnapshot, creditsOutcome := service.probeWxaiCreditsBilling(
		requestContext,
		newWxaiDirectHTTPClient(),
		settings.Timeout,
		currentAccount.AuthIndex,
		accessToken,
		billingUserID,
		logger,
	)
	if !creditsOutcome.Alive {
		resolution.CreditsError = describeWxaiProbeOutcome(creditsOutcome)
		service.logWxaiSuperQuotaRecoveryResolution(requestContext, currentAccount, resolution, logger)
		return resolution
	}

	creditsRecovery := wxaiCreditsRecovery(creditsSnapshot, inspectionTime)
	if creditsRecovery.recoverAtMS <= 0 {
		resolution.CreditsError = "billing credits 未返回有效 currentPeriod.end 或 billingPeriodEnd"
		service.logWxaiSuperQuotaRecoveryResolution(requestContext, currentAccount, resolution, logger)
		return resolution
	}
	resolution.Recovery = creditsRecovery
	resolution.CreditsRecoverAtMS = creditsRecovery.recoverAtMS
	service.logWxaiSuperQuotaRecoveryResolution(requestContext, currentAccount, resolution, logger)
	return resolution
}

func (service *Service) logWxaiSuperQuotaRecoveryResolution(
	ctx context.Context,
	currentAccount account,
	resolution wxaiUsageQuotaRecoveryResolution,
	logger runLogger,
) {
	logger.info(context.WithoutCancel(ctx), "wXAi SUPER 业务 429 冷却时间已解析", map[string]any{
		"fileName":               currentAccount.FileName,
		"displayAccount":         currentAccount.DisplayAccount,
		"eventRecoverySource":    resolution.OriginalRecoverySource,
		"creditsAttempted":       resolution.CreditsAttempted,
		"creditsRecoverAtMs":     resolution.CreditsRecoverAtMS,
		"creditsError":           resolution.CreditsError,
		"selectedRecoverySource": resolution.Recovery.source,
	})
}

func describeWxaiProbeOutcome(outcome wxaiProbeOutcome) string {
	if outcome.StatusCode > 0 {
		return fmt.Sprintf("HTTP %d: %s", outcome.StatusCode, outcome.Detail)
	}
	return firstNonEmpty(outcome.Detail, outcome.ErrorKind, "billing credits request failed")
}
