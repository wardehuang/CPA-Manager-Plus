package wxaiinspection

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

const (
	wxaiBillingURL           = "https://cli-chat-proxy.grok.com/v1/billing"
	wxaiBillingCreditsURL    = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	wxaiBillingUserAgent     = "grok-pager/0.2.101 grok-shell/0.2.101 (macos; aarch64)"
	wxaiBillingClientVersion = "0.2.101"
)

type wxaiBillingSnapshot struct {
	QuotaWindows      []model.WxaiInspectionQuotaWindow
	MonthlyLimitCents *float64
	MonthlyUsedCents  *float64
	RecoveryAtMS      int64
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
	snapshot.RecoveryAtMS = resetAtMS
	if response.Config.CreditUsagePercent == nil || periodType == "" || resetAtMS <= 0 {
		return nil
	}

	usedPercent := *response.Config.CreditUsagePercent
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
	if source.RecoveryAtMS > 0 {
		target.RecoveryAtMS = source.RecoveryAtMS
	}
}

func resolveWxaiAccountType(monthlyLimitCents *float64) string {
	if monthlyLimitCents != nil && *monthlyLimitCents > 0 {
		return wxaiAccountTypeSuper
	}
	return wxaiAccountTypeFree
}

func resolveWxaiBillingUserID(authFile map[string]any, accountID string) string {
	return firstNonEmpty(
		firstString(authFile, "sub", "subject", "user_id", "userId"),
		strings.TrimSpace(accountID),
	)
}

func wxaiBillingHeaders(accessToken string, userID string) map[string]string {
	headers := map[string]string{
		"Authorization":         "Bearer " + accessToken,
		"Accept":                "*/*",
		"X-XAI-Token-Auth":      "xai-grok-cli",
		"x-grok-client-version": wxaiBillingClientVersion,
		"User-Agent":            wxaiBillingUserAgent,
	}
	if normalizedUserID := strings.TrimSpace(userID); normalizedUserID != "" {
		headers["x-userid"] = normalizedUserID
	}
	return headers
}

func floatPointer(value float64) *float64 {
	return &value
}
