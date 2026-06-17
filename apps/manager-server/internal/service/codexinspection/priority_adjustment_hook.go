package codexinspection

import (
	"context"
	"fmt"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const priorityPausedValue = -1
const priorityDefaultRestoreValue = 1

func (s *Service) applyCodexPriorityAdjustment(
	ctx context.Context,
	setup store.Setup,
	item account,
	payload map[string]any,
	planType string,
	logger runLogger,
) (string, *int) {
	usage := codexPriorityUsageFromPayload(payload, planType)
	if !usage.hasAnyPercent() {
		return "", nil
	}
	if exhausted, recoverAt := usage.hasExhaustedWindow(); exhausted {
		return s.pauseCodexPriority(ctx, setup, item, recoverAt, logger)
	}
	if usage.allKnownWindowsAvailable() {
		return s.restoreCodexPriority(ctx, setup, item, logger)
	}
	return "", nil
}

type codexPriorityUsage struct {
	FiveHourUsedPercent *float64
	FiveHourResetAtMS   int64
	WeeklyUsedPercent   *float64
	WeeklyResetAtMS     int64
	MonthlyUsedPercent  *float64
	MonthlyResetAtMS    int64
}

func codexPriorityUsageFromPayload(payload map[string]any, planType string) codexPriorityUsage {
	usage := codexPriorityUsage{}
	if payload == nil {
		return usage
	}
	rateLimit := readMap(payload, "rate_limit", "rateLimit")
	primary := parseAccountStatusWindow(readMap(rateLimit, "primary_window", "primaryWindow"))
	secondary := parseAccountStatusWindow(readMap(rateLimit, "secondary_window", "secondaryWindow"))
	fiveHour, weekly, monthly := classifyAccountStatusWindows(primary, secondary, planType)
	if fiveHour != nil {
		usage.FiveHourUsedPercent = fiveHour.UsedPercent
		usage.FiveHourResetAtMS = fiveHour.ResetAtMS
	}
	if weekly != nil {
		usage.WeeklyUsedPercent = weekly.UsedPercent
		usage.WeeklyResetAtMS = weekly.ResetAtMS
	}
	if monthly != nil {
		usage.MonthlyUsedPercent = monthly.UsedPercent
		usage.MonthlyResetAtMS = monthly.ResetAtMS
	}
	return usage
}

func (u codexPriorityUsage) hasAnyPercent() bool {
	return u.FiveHourUsedPercent != nil || u.WeeklyUsedPercent != nil || u.MonthlyUsedPercent != nil
}

func (u codexPriorityUsage) hasExhaustedWindow() (bool, int64) {
	exhausted := false
	recoverAt := int64(0)
	if codexPriorityPercentExhausted(u.FiveHourUsedPercent) {
		exhausted = true
		recoverAt = maxCodexPriorityRecoverAt(recoverAt, u.FiveHourResetAtMS)
	}
	if codexPriorityPercentExhausted(u.WeeklyUsedPercent) {
		exhausted = true
		recoverAt = maxCodexPriorityRecoverAt(recoverAt, u.WeeklyResetAtMS)
	}
	if codexPriorityPercentExhausted(u.MonthlyUsedPercent) {
		exhausted = true
		recoverAt = maxCodexPriorityRecoverAt(recoverAt, u.MonthlyResetAtMS)
	}
	return exhausted, recoverAt
}

func (u codexPriorityUsage) allKnownWindowsAvailable() bool {
	return u.hasAnyPercent() &&
		codexPriorityPercentAvailable(u.FiveHourUsedPercent) &&
		codexPriorityPercentAvailable(u.WeeklyUsedPercent) &&
		codexPriorityPercentAvailable(u.MonthlyUsedPercent)
}

func codexPriorityPercentExhausted(percent *float64) bool {
	return percent != nil && *percent >= 100
}

func codexPriorityPercentAvailable(percent *float64) bool {
	return percent == nil || *percent < 100
}

func maxCodexPriorityRecoverAt(current int64, next int64) int64 {
	if next > current {
		return next
	}
	return current
}

func ptrInt(value int) *int {
	return &value
}

func (s *Service) pauseCodexPriority(ctx context.Context, setup store.Setup, item account, recoverAt int64, logger runLogger) (string, *int) {
	if item.Priority != nil && *item.Priority == priorityPausedValue {
		return "", nil
	}
	if err := s.updateCodexAuthFilePriority(ctx, setup, item.FileName, priorityPausedValue); err != nil {
		logger.warning(ctx, "调整账号优先级失败", map[string]any{
			"fileName":       item.FileName,
			"displayAccount": item.DisplayAccount,
			"priority":       priorityPausedValue,
			"error":          err.Error(),
		})
		return "", nil
	}
	if err := s.store.UpsertCodexPriorityAdjustment(ctx, model.CodexPriorityAdjustment{
		AccountKey:       item.Key,
		FileName:         item.FileName,
		DisplayAccount:   item.DisplayAccount,
		AuthIndex:        item.AuthIndex,
		AccountID:        item.AccountID,
		OriginalPriority: item.Priority,
		RecoverAtMS:      recoverAt,
	}); err != nil {
		logger.warning(ctx, "记录账号优先级调整失败", map[string]any{
			"fileName":       item.FileName,
			"displayAccount": item.DisplayAccount,
			"error":          err.Error(),
		})
	}
	logger.info(ctx, "账号优先级已下降", map[string]any{
		"fileName":       item.FileName,
		"displayAccount": item.DisplayAccount,
		"recoverAtMs":    recoverAt,
	})
	return "优先级下降", ptrInt(priorityPausedValue)
}

func (s *Service) restoreCodexPriority(ctx context.Context, setup store.Setup, item account, logger runLogger) (string, *int) {
	if item.Priority == nil || *item.Priority != priorityPausedValue {
		return "", nil
	}
	adjustment, ok, err := s.store.GetCodexPriorityAdjustment(ctx, item.Key)
	if err != nil {
		logger.warning(ctx, "读取账号优先级调整记录失败", map[string]any{
			"fileName":       item.FileName,
			"displayAccount": item.DisplayAccount,
			"error":          err.Error(),
		})
		return "", nil
	}
	targetPriority := priorityDefaultRestoreValue
	if ok && adjustment.OriginalPriority != nil {
		targetPriority = *adjustment.OriginalPriority
	}
	if err := s.updateCodexAuthFilePriority(ctx, setup, item.FileName, targetPriority); err != nil {
		logger.warning(ctx, "恢复账号优先级失败", map[string]any{
			"fileName":       item.FileName,
			"displayAccount": item.DisplayAccount,
			"priority":       targetPriority,
			"error":          err.Error(),
		})
		return "", nil
	}
	if ok {
		if err := s.store.DeleteCodexPriorityAdjustment(ctx, item.Key); err != nil {
			logger.warning(ctx, "删除账号优先级调整记录失败", map[string]any{
				"fileName":       item.FileName,
				"displayAccount": item.DisplayAccount,
				"error":          err.Error(),
			})
		}
	}
	logger.info(ctx, "账号优先级已恢复", map[string]any{
		"fileName":       item.FileName,
		"displayAccount": item.DisplayAccount,
		"priority":       targetPriority,
	})
	return "优先级恢复", ptrInt(targetPriority)
}

func (s *Service) updateCodexAuthFilePriority(ctx context.Context, setup store.Setup, fileName string, priority int) error {
	payload := map[string]any{"name": fileName, "priority": priority}
	primaryErr, primaryStatus := s.patchAuthFile(ctx, setup, "/auth-files/fields", payload)
	if primaryErr == nil {
		return nil
	}
	if shouldFallbackManagement(primaryStatus) {
		managementErr, _ := s.patchAuthFile(ctx, setup, "/v0/management/auth-files/fields", payload)
		if managementErr == nil {
			return nil
		}
		return combineActionEndpointErrors(
			actionEndpointError{Endpoint: "/auth-files/fields", Err: primaryErr},
			actionEndpointError{Endpoint: "/v0/management/auth-files/fields", Err: managementErr},
		)
	}
	return primaryErr
}

func appendPriorityAdjustmentReason(reason string, label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return reason
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Sprintf("【%s】", label)
	}
	return fmt.Sprintf("%s 【%s】", reason, label)
}
