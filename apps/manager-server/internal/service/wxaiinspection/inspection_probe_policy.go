package wxaiinspection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func (service *Service) probeWxaiMonthlyBilling(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	authIndex string,
	accessToken string,
	userID string,
	logger runLogger,
) (wxaiBillingSnapshot, wxaiProbeOutcome) {
	response, err := service.performWxaiBillingDirectCall(
		ctx,
		client,
		timeoutMilliseconds,
		authIndex,
		wxaiBillingURL,
		accessToken,
		userID,
		logger,
	)
	if err != nil {
		return wxaiBillingSnapshot{}, wxaiRequestFailure("billing request: " + err.Error())
	}
	responseOutcome := classifyWxaiStandaloneResponse(response)
	if !responseOutcome.Alive {
		return wxaiBillingSnapshot{}, responseOutcome
	}

	snapshot := wxaiBillingSnapshot{QuotaWindows: make([]model.WxaiInspectionQuotaWindow, 0, 1)}
	if err := parseWxaiMonthlyBilling(response.Body, &snapshot); err != nil {
		return wxaiBillingSnapshot{}, wxaiRequestFailure("parse billing response: " + err.Error())
	}
	return snapshot, responseOutcome
}

func (service *Service) probeWxaiCreditsBilling(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	authIndex string,
	accessToken string,
	userID string,
	logger runLogger,
) (wxaiBillingSnapshot, wxaiProbeOutcome) {
	response, err := service.performWxaiBillingDirectCall(
		ctx,
		client,
		timeoutMilliseconds,
		authIndex,
		wxaiBillingCreditsURL,
		accessToken,
		userID,
		logger,
	)
	if err != nil {
		return wxaiBillingSnapshot{}, wxaiRequestFailure("billing credits request: " + err.Error())
	}
	responseOutcome := classifyWxaiStandaloneResponse(response)
	if !responseOutcome.Alive {
		return wxaiBillingSnapshot{}, responseOutcome
	}

	snapshot := wxaiBillingSnapshot{QuotaWindows: make([]model.WxaiInspectionQuotaWindow, 0, 1)}
	if err := parseWxaiCreditsBilling(response.Body, &snapshot); err != nil {
		return wxaiBillingSnapshot{}, wxaiRequestFailure("parse billing credits response: " + err.Error())
	}
	return snapshot, responseOutcome
}

type wxaiConcurrentBillingProbeResult struct {
	monthly  bool
	snapshot wxaiBillingSnapshot
	outcome  wxaiProbeOutcome
}

func (service *Service) probeWxaiSuperBilling(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	authIndex string,
	accessToken string,
	userID string,
	logger runLogger,
) (wxaiBillingSnapshot, wxaiProbeOutcome) {
	probeResults := make(chan wxaiConcurrentBillingProbeResult, 2)
	go func() {
		snapshot, outcome := service.probeWxaiMonthlyBilling(
			ctx,
			client,
			timeoutMilliseconds,
			authIndex,
			accessToken,
			userID,
			logger,
		)
		probeResults <- wxaiConcurrentBillingProbeResult{
			monthly:  true,
			snapshot: snapshot,
			outcome:  outcome,
		}
	}()
	go func() {
		snapshot, outcome := service.probeWxaiCreditsBilling(
			ctx,
			client,
			timeoutMilliseconds,
			authIndex,
			accessToken,
			userID,
			logger,
		)
		probeResults <- wxaiConcurrentBillingProbeResult{
			snapshot: snapshot,
			outcome:  outcome,
		}
	}()

	var monthlyProbe wxaiConcurrentBillingProbeResult
	var creditsProbe wxaiConcurrentBillingProbeResult
	for completedProbes := 0; completedProbes < 2; completedProbes++ {
		probeResult := <-probeResults
		if probeResult.monthly {
			monthlyProbe = probeResult
			continue
		}
		creditsProbe = probeResult
	}

	combinedSnapshot := wxaiBillingSnapshot{}
	mergeWxaiBillingSnapshot(&combinedSnapshot, monthlyProbe.snapshot)
	mergeWxaiBillingSnapshot(&combinedSnapshot, creditsProbe.snapshot)
	if !monthlyProbe.outcome.Alive {
		return combinedSnapshot, monthlyProbe.outcome
	}
	if !creditsProbe.outcome.Alive {
		return combinedSnapshot, creditsProbe.outcome
	}
	return combinedSnapshot, creditsProbe.outcome
}

func (service *Service) probeWxaiResponsesOnly(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	accessToken string,
	clientVersion string,
) wxaiProbeOutcome {
	requestBody, err := json.Marshal(wxaiResponsesRequest{
		Model:  wxaiProbeModel,
		Input:  wxaiProbeInput,
		Stream: false,
	})
	if err != nil {
		return wxaiRequestFailure("encode responses request: " + err.Error())
	}
	response, err := service.performWxaiRequest(
		ctx,
		client,
		timeoutMilliseconds,
		http.MethodPost,
		wxaiResponsesURL,
		requestBody,
		wxaiInspectionHeaders(accessToken, clientVersion),
	)
	if err != nil {
		return wxaiRequestFailure("responses request: " + err.Error())
	}
	return classifyWxaiStandaloneResponse(response)
}

func classifyWxaiStandaloneResponse(response wxaiHTTPResponse) wxaiProbeOutcome {
	outcome, definitive := classifyWxaiProbeResponse(response)
	if definitive {
		return outcome
	}
	return wxaiAccountFailure(
		response.StatusCode,
		truncate(strings.TrimSpace(string(response.Body)), wxaiProbeDetailLimit),
	)
}

func applyWxaiBillingSnapshot(result *model.WxaiInspectionResult, snapshot wxaiBillingSnapshot) {
	result.QuotaWindows = snapshot.QuotaWindows
	result.MonthlyLimitCents = snapshot.MonthlyLimitCents
	result.MonthlyUsedCents = snapshot.MonthlyUsedCents
	result.UsedPercent = primaryWxaiUsedPercent(snapshot.QuotaWindows)
}

func (service *Service) persistResolvedWxaiAccountType(
	ctx context.Context,
	currentAccount account,
	accountType string,
	logger runLogger,
) {
	if err := service.persistWxaiAccountType(ctx, currentAccount.Key, accountType); err != nil {
		logger.warning(context.WithoutCancel(ctx), "保存 wXAi 账号类型失败", map[string]any{
			"fileName":    currentAccount.FileName,
			"accountType": accountType,
			"error":       err.Error(),
		})
	}
}

func (service *Service) applyWxaiBotFlagFailure(
	ctx context.Context,
	setup store.Setup,
	currentAccount account,
	result model.WxaiInspectionResult,
	botFlagClaim string,
	botFlagValue string,
	logger runLogger,
) (model.WxaiInspectionResult, *int) {
	result.ErrorKind = "account_abnormal"
	result.ErrorDetail = truncate(fmt.Sprintf("%s=%s", botFlagClaim, botFlagValue), maxStoredBodyText)
	result.ActionReason = fmt.Sprintf("JWT %s 非空，priority 已设为 -6，账号不再参与巡检", botFlagClaim)
	effectivePriority, priorityErr := service.setWxaiBotFlaggedPriority(ctx, setup, currentAccount, logger)
	if priorityErr != nil {
		applyWxaiPriorityError(&result, "priority_adjustment_failed", priorityErr)
		result.ActionReason += "；priority 调整失败"
	}
	logger.warning(context.WithoutCancel(ctx), "wXAi 账号命中 JWT bot 标记", map[string]any{
		"fileName":       currentAccount.FileName,
		"displayAccount": currentAccount.DisplayAccount,
		"botFlagClaim":   botFlagClaim,
		"botFlagValue":   botFlagValue,
		"priority":       wxaiBotFlaggedPriorityValue,
	})
	return result, effectivePriority
}

func isWxaiQuotaRecoveryProbeRequired(currentAccount account, accountType string) bool {
	return normalizeWxaiAccountType(accountType) == wxaiAccountTypeFree &&
		currentAccount.Priority != nil &&
		*currentAccount.Priority == wxaiQuotaPriorityValue
}

func wxaiCreditsRecovery(snapshot wxaiBillingSnapshot, now time.Time) wxaiQuotaRecovery {
	if snapshot.RecoveryAtMS <= now.UnixMilli() {
		return wxaiQuotaRecovery{}
	}
	return wxaiQuotaRecovery{recoverAtMS: snapshot.RecoveryAtMS, source: "billing_credits"}
}
