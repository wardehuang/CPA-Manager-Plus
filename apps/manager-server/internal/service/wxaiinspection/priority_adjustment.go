package wxaiinspection

import (
	"context"
	"fmt"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const (
	wxaiQuotaPriorityValue            = -1
	wxaiAbnormalPriorityValue         = -2
	wxaiLegacyPriorityValue           = -3
	wxaiUnauthorizedPriorityValue     = -4
	wxaiDisabledPriorityValue         = -5
	wxaiBotFlaggedPriorityValue       = -6
	wxaiSSOExpiredPriorityValue       = -7
	wxaiPositionDegradedPriorityValue = -8
	wxaiNormalizedPriorityValue       = 1
	wxaiPriorityRecheckInterval       = 30 * time.Second
)

func (service *Service) lowerWxaiPriority(
	ctx context.Context,
	setup store.Setup,
	currentAccount account,
	adjustedPriority int,
	recoverAtMS int64,
	logger runLogger,
) (*int, error) {
	if recoverAtMS <= 0 {
		recoverAtMS = time.Now().Add(wxaiPriorityRecheckInterval).UnixMilli()
	}
	originalPriority := currentAccount.Priority
	existingAdjustment, adjustmentExists, err := service.store.GetWxaiPriorityAdjustment(ctx, currentAccount.Key)
	if err != nil {
		return currentAccount.Priority, fmt.Errorf("读取 wXAi priority adjustment: %w", err)
	}
	if adjustmentExists && currentAccount.Priority != nil && *currentAccount.Priority == existingAdjustment.AdjustedPriority {
		originalPriority = existingAdjustment.OriginalPriority
	}
	if isWxaiNegativePriority(currentAccount.Priority) && originalPriority != nil {
		originalPriority = intPointer(wxaiNormalizedPriorityValue)
	}
	adjustment := model.WxaiPriorityAdjustment{
		AccountKey:       currentAccount.Key,
		FileName:         currentAccount.FileName,
		DisplayAccount:   currentAccount.DisplayAccount,
		AuthIndex:        currentAccount.AuthIndex,
		AccountID:        currentAccount.AccountID,
		OriginalPriority: originalPriority,
		AdjustedPriority: adjustedPriority,
		RecoverAtMS:      recoverAtMS,
	}
	if err := service.store.UpsertWxaiPriorityAdjustment(ctx, adjustment); err != nil {
		return currentAccount.Priority, fmt.Errorf("保存 wXAi priority adjustment: %w", err)
	}
	if currentAccount.Priority != nil && *currentAccount.Priority == adjustedPriority {
		return intPointer(adjustedPriority), nil
	}
	authFilesClient := cpaauthfiles.New(service.client, cpaauthfiles.DefaultTimeout)
	if err := authFilesClient.PatchPriority(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
		currentAccount.FileName,
		adjustedPriority,
	); err != nil {
		rollbackErr := service.rollbackWxaiPriorityAdjustment(ctx, currentAccount.Key, existingAdjustment, adjustmentExists)
		if rollbackErr != nil {
			return currentAccount.Priority, fmt.Errorf(
				"patch wXAi priority %d: %w; 回滚 priority adjustment: %v",
				adjustedPriority,
				err,
				rollbackErr,
			)
		}
		return currentAccount.Priority, fmt.Errorf("patch wXAi priority %d: %w", adjustedPriority, err)
	}
	if !isWxaiManagedPriority(currentAccount.Priority) {
		if err := service.store.MarkWxaiAccountPriorityAbnormal(ctx, currentAccount.Key, time.Now().UnixMilli()); err != nil {
			return intPointer(adjustedPriority), fmt.Errorf("记录 wXAi 正常转异常时间: %w", err)
		}
	}
	logger.info(context.WithoutCancel(ctx), "wXAi 账号 priority 已调整", map[string]any{
		"fileName":         currentAccount.FileName,
		"displayAccount":   currentAccount.DisplayAccount,
		"authIndex":        currentAccount.AuthIndex,
		"adjustedPriority": adjustedPriority,
		"recoverAtMs":      recoverAtMS,
	})
	return intPointer(adjustedPriority), nil
}

func (service *Service) rollbackWxaiPriorityAdjustment(
	ctx context.Context,
	accountKey string,
	existingAdjustment model.WxaiPriorityAdjustment,
	adjustmentExists bool,
) error {
	if adjustmentExists {
		return service.store.UpsertWxaiPriorityAdjustment(ctx, existingAdjustment)
	}
	return service.store.DeleteWxaiPriorityAdjustment(ctx, accountKey)
}

func (service *Service) restoreWxaiPriority(
	ctx context.Context,
	setup store.Setup,
	currentAccount account,
	logger runLogger,
) (*int, error) {
	adjustment, adjustmentExists, err := service.store.GetWxaiPriorityAdjustment(ctx, currentAccount.Key)
	if err != nil {
		return currentAccount.Priority, fmt.Errorf("读取 wXAi priority adjustment: %w", err)
	}
	if adjustmentExists && (currentAccount.Priority == nil || *currentAccount.Priority != adjustment.AdjustedPriority) {
		if err := service.store.DeleteWxaiPriorityAdjustment(ctx, currentAccount.Key); err != nil {
			return currentAccount.Priority, fmt.Errorf("删除 stale wXAi priority adjustment: %w", err)
		}
		logger.info(context.WithoutCancel(ctx), "已清理 stale wXAi priority adjustment", map[string]any{
			"fileName":       currentAccount.FileName,
			"displayAccount": currentAccount.DisplayAccount,
			"priority":       currentAccount.Priority,
		})
		return currentAccount.Priority, nil
	}
	if !adjustmentExists && !isWxaiManagedPriority(currentAccount.Priority) {
		return currentAccount.Priority, nil
	}

	targetPriority := resolveWxaiRestorePriority()
	authFilesClient := cpaauthfiles.New(service.client, cpaauthfiles.DefaultTimeout)
	if err := authFilesClient.PatchPriority(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
		currentAccount.FileName,
		targetPriority,
	); err != nil {
		return currentAccount.Priority, fmt.Errorf("恢复 wXAi priority %d: %w", targetPriority, err)
	}
	priorityWasAdjusted := adjustmentExists && currentAccount.Priority != nil && *currentAccount.Priority == adjustment.AdjustedPriority
	if isWxaiManagedPriority(currentAccount.Priority) || priorityWasAdjusted {
		if err := service.store.MarkWxaiAccountPriorityRecovered(ctx, currentAccount.Key, time.Now().UnixMilli()); err != nil {
			return intPointer(targetPriority), fmt.Errorf("记录 wXAi 异常恢复时间: %w", err)
		}
	}
	if adjustmentExists {
		if err := service.store.DeleteWxaiPriorityAdjustment(ctx, currentAccount.Key); err != nil {
			return intPointer(targetPriority), fmt.Errorf("删除 wXAi priority adjustment: %w", err)
		}
	}
	logger.info(context.WithoutCancel(ctx), "wXAi 账号 priority 已恢复", map[string]any{
		"fileName":       currentAccount.FileName,
		"displayAccount": currentAccount.DisplayAccount,
		"priority":       targetPriority,
	})
	return intPointer(targetPriority), nil
}

func (service *Service) setWxaiTerminalPriority(
	ctx context.Context,
	setup store.Setup,
	currentAccount account,
	priority int,
	logger runLogger,
) (*int, error) {
	if priority != wxaiBotFlaggedPriorityValue && priority != wxaiSSOExpiredPriorityValue {
		return currentAccount.Priority, fmt.Errorf("不支持的 wXAi 终止巡检 priority: %d", priority)
	}
	if currentAccount.Priority == nil || *currentAccount.Priority != priority {
		authFilesClient := cpaauthfiles.New(service.client, cpaauthfiles.DefaultTimeout)
		if err := authFilesClient.PatchPriority(
			ctx,
			setup.CPAUpstreamURL,
			setup.ManagementKey,
			currentAccount.FileName,
			priority,
		); err != nil {
			return currentAccount.Priority, fmt.Errorf("设置 wXAi 终止巡检 priority %d: %w", priority, err)
		}
	}
	if err := service.store.DeleteWxaiPriorityAdjustment(ctx, currentAccount.Key); err != nil {
		return intPointer(priority), fmt.Errorf("删除终止巡检账号 priority adjustment: %w", err)
	}
	if !isWxaiManagedPriority(currentAccount.Priority) &&
		!isWxaiBotFlaggedPriority(currentAccount.Priority) &&
		!isWxaiSSOExpiredPriority(currentAccount.Priority) {
		if err := service.store.MarkWxaiAccountPriorityAbnormal(ctx, currentAccount.Key, time.Now().UnixMilli()); err != nil {
			return intPointer(priority), fmt.Errorf("记录 wXAi 终止巡检账号异常时间: %w", err)
		}
	}
	logger.warning(context.WithoutCancel(ctx), "wXAi 终止巡检账号 priority 已设置", map[string]any{
		"fileName":       currentAccount.FileName,
		"displayAccount": currentAccount.DisplayAccount,
		"authIndex":      currentAccount.AuthIndex,
		"priority":       priority,
	})
	return intPointer(priority), nil
}

func resolveWxaiRestorePriority() int {
	return wxaiNormalizedPriorityValue
}

func isWxaiNegativePriority(priority *int) bool {
	return priority != nil && *priority < 0
}

func isWxaiManagedPriority(priority *int) bool {
	return priority != nil && (*priority == wxaiQuotaPriorityValue ||
		*priority == wxaiAbnormalPriorityValue ||
		*priority == wxaiLegacyPriorityValue ||
		*priority == wxaiUnauthorizedPriorityValue ||
		*priority == wxaiSSOExpiredPriorityValue)
}

func isWxaiBotFlaggedPriority(priority *int) bool {
	return priority != nil && *priority == wxaiBotFlaggedPriorityValue
}

func isWxaiSSOExpiredPriority(priority *int) bool {
	return priority != nil && *priority == wxaiSSOExpiredPriorityValue
}

func intPointer(value int) *int {
	return &value
}
