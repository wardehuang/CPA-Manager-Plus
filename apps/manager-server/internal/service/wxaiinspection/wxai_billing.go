package wxaiinspection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

const (
	wxaiBillingURL           = "https://cli-chat-proxy.grok.com/v1/billing"
	wxaiBillingCreditsURL    = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	wxaiBillingUserAgent     = "grok-shell/0.2.99 (linux; x86_64)"
	wxaiBillingClientVersion = "0.2.99"
)

type wxaiBillingSnapshot struct {
	QuotaWindows      []model.WxaiInspectionQuotaWindow
	MonthlyLimitCents *float64
	MonthlyUsedCents  *float64
}

type wxaiMonthlyBillingResponse struct {
	Config struct {
		MonthlyLimit     json.RawMessage `json:"monthlyLimit"`
		Used             json.RawMessage `json:"used"`
		BillingPeriodEnd string          `json:"billingPeriodEnd"`
	} `json:"config"`
}

type wxaiCreditsBillingResponse struct {
	Config struct {
		CreditUsagePercent *float64 `json:"creditUsagePercent"`
		BillingPeriodEnd   string   `json:"billingPeriodEnd"`
		CurrentPeriod      struct {
			Type  string `json:"type"`
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"currentPeriod"`
	} `json:"config"`
}

func (service *Service) refreshWxaiBillingMetadata(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	accessToken string,
	accountType string,
) (wxaiBillingSnapshot, string, error) {
	normalizedAccountType := normalizeWxaiAccountType(accountType)
	switch normalizedAccountType {
	case wxaiAccountTypeFree:
		return wxaiBillingSnapshot{}, normalizedAccountType, nil
	case wxaiAccountTypeSuper:
		snapshot, err := service.fetchWxaiCreditsBilling(ctx, client, timeoutMilliseconds, accessToken)
		return snapshot, normalizedAccountType, err
	}

	snapshot, err := service.fetchWxaiMonthlyBilling(ctx, client, timeoutMilliseconds, accessToken)
	if err != nil {
		return wxaiBillingSnapshot{}, "", err
	}
	resolvedAccountType := resolveWxaiAccountType(snapshot.MonthlyLimitCents)
	if resolvedAccountType == wxaiAccountTypeFree {
		return snapshot, resolvedAccountType, nil
	}

	creditsSnapshot, creditsErr := service.fetchWxaiCreditsBilling(ctx, client, timeoutMilliseconds, accessToken)
	mergeWxaiBillingSnapshot(&snapshot, creditsSnapshot)
	return snapshot, resolvedAccountType, creditsErr
}

func (service *Service) fetchWxaiMonthlyBilling(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	accessToken string,
) (wxaiBillingSnapshot, error) {
	response, err := service.performWxaiRequest(
		ctx,
		client,
		timeoutMilliseconds,
		http.MethodGet,
		wxaiBillingURL,
		nil,
		wxaiBillingHeaders(accessToken),
	)
	if err != nil {
		return wxaiBillingSnapshot{}, fmt.Errorf("billing request: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return wxaiBillingSnapshot{}, fmt.Errorf("billing returned HTTP %d", response.StatusCode)
	}
	snapshot := wxaiBillingSnapshot{QuotaWindows: make([]model.WxaiInspectionQuotaWindow, 0, 1)}
	if err := parseWxaiMonthlyBilling(response.Body, &snapshot); err != nil {
		return wxaiBillingSnapshot{}, fmt.Errorf("parse billing response: %w", err)
	}
	return snapshot, nil
}

func (service *Service) fetchWxaiCreditsBilling(
	ctx context.Context,
	client *http.Client,
	timeoutMilliseconds int,
	accessToken string,
) (wxaiBillingSnapshot, error) {
	response, err := service.performWxaiRequest(
		ctx,
		client,
		timeoutMilliseconds,
		http.MethodGet,
		wxaiBillingCreditsURL,
		nil,
		wxaiBillingHeaders(accessToken),
	)
	if err != nil {
		return wxaiBillingSnapshot{}, fmt.Errorf("billing credits request: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return wxaiBillingSnapshot{}, fmt.Errorf("billing credits returned HTTP %d", response.StatusCode)
	}
	snapshot := wxaiBillingSnapshot{QuotaWindows: make([]model.WxaiInspectionQuotaWindow, 0, 1)}
	if err := parseWxaiCreditsBilling(response.Body, &snapshot); err != nil {
		return wxaiBillingSnapshot{}, fmt.Errorf("parse billing credits response: %w", err)
	}
	return snapshot, nil
}

func parseWxaiMonthlyBilling(body []byte, snapshot *wxaiBillingSnapshot) error {
	var response wxaiMonthlyBillingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}
	monthlyLimit, hasMonthlyLimit, err := parseWxaiNestedNumber(response.Config.MonthlyLimit)
	if err != nil {
		return fmt.Errorf("monthlyLimit: %w", err)
	}
	monthlyUsed, hasMonthlyUsed, err := parseWxaiNestedNumber(response.Config.Used)
	if err != nil {
		return fmt.Errorf("used: %w", err)
	}
	if hasMonthlyLimit {
		snapshot.MonthlyLimitCents = floatPointer(monthlyLimit)
	}
	if hasMonthlyUsed {
		snapshot.MonthlyUsedCents = floatPointer(monthlyUsed)
	}

	resetAtMS := parseWxaiBillingTime(response.Config.BillingPeriodEnd)
	if !hasMonthlyLimit || monthlyLimit <= 0 || !hasMonthlyUsed || resetAtMS <= 0 {
		return nil
	}
	usedPercent := monthlyUsed / monthlyLimit * 100
	snapshot.QuotaWindows = append(snapshot.QuotaWindows, model.WxaiInspectionQuotaWindow{
		ID:          model.WxaiAccountWindowTypeMonthly,
		LabelKey:    "月限额",
		UsedPercent: floatPointer(usedPercent),
		ResetAtMS:   resetAtMS,
	})
	return nil
}

func parseWxaiCreditsBilling(body []byte, snapshot *wxaiBillingSnapshot) error {
	var response wxaiCreditsBillingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}
	periodType := strings.TrimSpace(response.Config.CurrentPeriod.Type)
	resetAtMS := parseWxaiBillingTime(firstNonEmpty(
		response.Config.CurrentPeriod.End,
		response.Config.BillingPeriodEnd,
	))
	if periodType == "" || resetAtMS <= 0 {
		return nil
	}

	usedPercent := 0.0
	if response.Config.CreditUsagePercent != nil {
		usedPercent = *response.Config.CreditUsagePercent
	}
	windowID := model.WxaiAccountWindowTypeWeekly
	windowLabel := "周限额"
	if periodType == "USAGE_PERIOD_TYPE_MONTHLY" {
		windowID = model.WxaiAccountWindowTypeMonthly
		windowLabel = "月限额"
	}
	upsertWxaiQuotaWindow(&snapshot.QuotaWindows, model.WxaiInspectionQuotaWindow{
		ID:          windowID,
		LabelKey:    windowLabel,
		UsedPercent: floatPointer(usedPercent),
		ResetAtMS:   resetAtMS,
	})
	return nil
}

func parseWxaiNestedNumber(raw json.RawMessage) (float64, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false, nil
	}
	var directValue float64
	if err := json.Unmarshal(raw, &directValue); err == nil {
		return directValue, true, nil
	}
	var wrappedValue struct {
		Value *float64 `json:"val"`
	}
	if err := json.Unmarshal(raw, &wrappedValue); err != nil {
		return 0, false, err
	}
	if wrappedValue.Value == nil {
		return 0, false, nil
	}
	return *wrappedValue.Value, true, nil
}

func parseWxaiBillingTime(value string) int64 {
	parsedTime, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsedTime.UnixMilli()
}

func upsertWxaiQuotaWindow(
	windows *[]model.WxaiInspectionQuotaWindow,
	candidate model.WxaiInspectionQuotaWindow,
) {
	for index := range *windows {
		if (*windows)[index].ID == candidate.ID {
			(*windows)[index] = candidate
			return
		}
	}
	*windows = append(*windows, candidate)
}

func mergeWxaiBillingSnapshot(target *wxaiBillingSnapshot, source wxaiBillingSnapshot) {
	for _, quotaWindow := range source.QuotaWindows {
		upsertWxaiQuotaWindow(&target.QuotaWindows, quotaWindow)
	}
	if source.MonthlyLimitCents != nil {
		target.MonthlyLimitCents = source.MonthlyLimitCents
	}
	if source.MonthlyUsedCents != nil {
		target.MonthlyUsedCents = source.MonthlyUsedCents
	}
}

func resolveWxaiAccountType(monthlyLimitCents *float64) string {
	if monthlyLimitCents != nil && *monthlyLimitCents > 0 {
		return wxaiAccountTypeSuper
	}
	return wxaiAccountTypeFree
}

func wxaiBillingHeaders(accessToken string) map[string]string {
	return map[string]string{
		"Authorization":            "Bearer " + accessToken,
		"Accept":                   "application/json",
		"X-XAI-Token-Auth":         "xai-grok-cli",
		"x-grok-client-version":    wxaiBillingClientVersion,
		"x-grok-client-identifier": "grok-shell",
		"x-grok-client-surface":    "tui",
		"x-grok-client-name":       "grok-shell",
		"User-Agent":               wxaiBillingUserAgent,
	}
}

func floatPointer(value float64) *float64 {
	return &value
}
