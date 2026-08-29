package wxaiinspection

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const (
	wxaiFirstRealtimeDegradationCooldown  = 24 * time.Hour
	wxaiSecondRealtimeDegradationCooldown = 48 * time.Hour
	wxaiTerminalRealtimeDegradationCount  = 3
)

type RealtimeHealthyRequest struct {
	AccountKey string `json:"accountKey"`
	FileName   string `json:"fileName"`
	AuthIndex  string `json:"authIndex"`
	AccountID  string `json:"accountId"`
}

type realtimeDegradationStage struct {
	Priority         int
	DegradationCount int
	CooldownUntilMS  int64
	ActionReason     string
	ExecutedAction   string
	LogMessage       string
}

func (service *Service) applyRealtimeDegradationState(
	ctx context.Context,
	setup store.Setup,
	currentAccount account,
	request RealtimeDegradationRequest,
) (realtimeDegradationStage, error) {
	now := time.Now()
	existingState, stateExists, err := service.store.GetWxaiRealtimeDegradationState(ctx, request.AccountKey)
	if err != nil {
		return realtimeDegradationStage{}, err
	}
	if stateExists && existingState.DegradationCount >= wxaiTerminalRealtimeDegradationCount {
		return terminalRealtimeDegradationStage(), nil
	}
	if stateExists && existingState.CooldownUntilMS > now.UnixMilli() {
		return activeRealtimeDegradationStage(existingState), nil
	}
	existingAdjustment, adjustmentExists, err := service.store.GetWxaiPriorityAdjustment(ctx, request.AccountKey)
	if err != nil {
		return realtimeDegradationStage{}, err
	}
	if stateExists && adjustmentExists && existingAdjustment.AdjustedPriority == wxaiPositionDegradedPriorityValue {
		return awaitingRealtimeDegradationInspectionStage(existingState), nil
	}

	degradationCount := 1
	if stateExists {
		degradationCount = existingState.DegradationCount + 1
	}
	state := model.WxaiRealtimeDegradationState{
		AccountKey:       request.AccountKey,
		FileName:         request.FileName,
		DisplayAccount:   request.DisplayAccount,
		AuthIndex:        request.AuthIndex,
		AccountID:        request.AccountID,
		DegradationCount: degradationCount,
		CreatedAtMS:      existingState.CreatedAtMS,
		UpdatedAtMS:      now.UnixMilli(),
	}

	if degradationCount >= wxaiTerminalRealtimeDegradationCount {
		state.DegradationCount = wxaiTerminalRealtimeDegradationCount
		if err := service.store.UpsertWxaiRealtimeDegradationState(ctx, state); err != nil {
			return realtimeDegradationStage{}, err
		}
		if currentAccount.Priority == nil || *currentAccount.Priority != wxaiBotFlaggedPriorityValue {
			authFilesClient := cpaauthfiles.New(service.client, cpaauthfiles.DefaultTimeout)
			if err := authFilesClient.PatchPriority(
				ctx,
				setup.CPAUpstreamURL,
				setup.ManagementKey,
				currentAccount.FileName,
				wxaiBotFlaggedPriorityValue,
			); err != nil {
				rollbackErr := service.rollbackRealtimeDegradationState(ctx, existingState, stateExists, request.AccountKey)
				if rollbackErr != nil {
					return realtimeDegradationStage{}, fmt.Errorf("设置第三次降智账号 priority=-6: %w; 回滚降智状态: %v", err, rollbackErr)
				}
				return realtimeDegradationStage{}, fmt.Errorf("设置第三次降智账号 priority=-6: %w", err)
			}
		}
		if err := service.store.DeleteWxaiPriorityAdjustment(ctx, request.AccountKey); err != nil {
			return realtimeDegradationStage{}, fmt.Errorf("删除第三次降智账号 priority adjustment: %w", err)
		}
		return terminalRealtimeDegradationStage(), nil
	}

	cooldown := wxaiFirstRealtimeDegradationCooldown
	if degradationCount == 2 {
		cooldown = wxaiSecondRealtimeDegradationCooldown
	}
	state.CooldownUntilMS = now.Add(cooldown).UnixMilli()
	if err := service.store.UpsertWxaiRealtimeDegradationState(ctx, state); err != nil {
		return realtimeDegradationStage{}, err
	}

	storedPriority := request.OriginalPriority
	if adjustmentExists && existingAdjustment.AdjustedPriority == wxaiPositionDegradedPriorityValue && existingAdjustment.OriginalPriority != nil {
		storedPriority = existingAdjustment.OriginalPriority
	}
	if storedPriority != nil {
		storedPriority = intPointer(wxaiNormalizedPriorityValue)
	}
	if err := service.store.UpsertWxaiPriorityAdjustment(ctx, model.WxaiPriorityAdjustment{
		AccountKey:       request.AccountKey,
		FileName:         request.FileName,
		DisplayAccount:   request.DisplayAccount,
		AuthIndex:        request.AuthIndex,
		AccountID:        request.AccountID,
		OriginalPriority: storedPriority,
		AdjustedPriority: wxaiPositionDegradedPriorityValue,
		RecoverAtMS:      state.CooldownUntilMS,
	}); err != nil {
		rollbackErr := service.rollbackRealtimeDegradationState(ctx, existingState, stateExists, request.AccountKey)
		if rollbackErr != nil {
			return realtimeDegradationStage{}, fmt.Errorf("保存实时降智 priority adjustment: %w; 回滚降智状态: %v", err, rollbackErr)
		}
		return realtimeDegradationStage{}, err
	}
	authFilesClient := cpaauthfiles.New(service.client, cpaauthfiles.DefaultTimeout)
	if err := authFilesClient.PatchPriority(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
		currentAccount.FileName,
		wxaiPositionDegradedPriorityValue,
	); err != nil {
		return realtimeDegradationStage{}, fmt.Errorf("确认实时降智账号 priority=-8: %w", err)
	}
	return activeRealtimeDegradationStage(state), nil
}

func (service *Service) rollbackRealtimeDegradationState(
	ctx context.Context,
	existingState model.WxaiRealtimeDegradationState,
	stateExists bool,
	accountKey string,
) error {
	if stateExists {
		return service.store.UpsertWxaiRealtimeDegradationState(ctx, existingState)
	}
	return service.store.DeleteWxaiRealtimeDegradationState(ctx, accountKey)
}

func activeRealtimeDegradationStage(state model.WxaiRealtimeDegradationState) realtimeDegradationStage {
	hours := 24
	if state.DegradationCount == 2 {
		hours = 48
	}
	return realtimeDegradationStage{
		Priority:         wxaiPositionDegradedPriorityValue,
		DegradationCount: state.DegradationCount,
		CooldownUntilMS:  state.CooldownUntilMS,
		ActionReason:     fmt.Sprintf("第 %d 次位置降智，priority=-8，冷却 %d 小时", state.DegradationCount, hours),
		ExecutedAction:   fmt.Sprintf("priority_-8_cooldown_%dh", hours),
		LogMessage:       fmt.Sprintf("实时守护发现第 %d 次位置降智，xAI 账号 priority 已设为 -8，冷却 %d 小时", state.DegradationCount, hours),
	}
}

func awaitingRealtimeDegradationInspectionStage(state model.WxaiRealtimeDegradationState) realtimeDegradationStage {
	return realtimeDegradationStage{
		Priority:         wxaiPositionDegradedPriorityValue,
		DegradationCount: state.DegradationCount,
		CooldownUntilMS:  state.CooldownUntilMS,
		ActionReason:     fmt.Sprintf("第 %d 次位置降智冷却已到期，但账号尚未由服务器巡检恢复，不递增次数", state.DegradationCount),
		ExecutedAction:   "awaiting_inspection_restore",
		LogMessage:       "实时守护收到位置降智通知，但账号尚未由服务器巡检恢复，连续降智次数保持不变",
	}
}

func terminalRealtimeDegradationStage() realtimeDegradationStage {
	return realtimeDegradationStage{
		Priority:         wxaiBotFlaggedPriorityValue,
		DegradationCount: wxaiTerminalRealtimeDegradationCount,
		ActionReason:     "第 3 次位置降智，priority 已设为 -6，永久退出巡检",
		ExecutedAction:   "priority_-6",
		LogMessage:       "实时守护发现第 3 次位置降智，xAI 账号 priority 已设为 -6，永久退出巡检",
	}
}

func (service *Service) RecordRealtimeHealthy(ctx context.Context, request RealtimeHealthyRequest) (bool, error) {
	if request.AccountKey == "" {
		return false, errors.New("account key is required")
	}
	service.realtimeDegradationMu.Lock()
	defer service.realtimeDegradationMu.Unlock()

	state, exists, err := service.store.GetWxaiRealtimeDegradationState(ctx, request.AccountKey)
	if err != nil {
		return false, err
	}
	if exists && (state.DegradationCount >= wxaiTerminalRealtimeDegradationCount || state.CooldownUntilMS > time.Now().UnixMilli()) {
		return false, nil
	}

	_, setup, err := service.resolveRuntime(ctx)
	if err != nil {
		return false, err
	}
	accounts, err := service.fetchAccounts(ctx, setup)
	if err != nil {
		return false, err
	}
	matchedAccount, matched := newWxaiConditionalAccountMatcher(accounts).match(wxaiConditionalAccountRef{
		AccountKey: request.AccountKey,
		FileName:   request.FileName,
		AuthIndex:  request.AuthIndex,
		AccountID:  request.AccountID,
		Provider:   "xai",
	})
	if !matched {
		return false, fmt.Errorf("实时健康账号未匹配: fileName=%s authIndex=%s accountID=%s", request.FileName, request.AuthIndex, request.AccountID)
	}
	if matchedAccount.Priority == nil || *matchedAccount.Priority != wxaiNormalizedPriorityValue {
		return false, nil
	}
	if !exists {
		return true, nil
	}
	if err := service.store.DeleteWxaiRealtimeDegradationState(ctx, state.AccountKey); err != nil {
		return false, err
	}
	runID, err := service.latestReusableWxaiRunID(ctx)
	if err != nil || runID <= 0 {
		return true, err
	}
	_, err = service.store.InsertWxaiInspectionLog(ctx, model.WxaiInspectionLog{
		RunID:   runID,
		Level:   "info",
		Message: "实时守护确认 xAI 账号恢复正常，连续降智次数已清零",
		Detail: map[string]any{
			"accountKey":               request.AccountKey,
			"fileName":                 request.FileName,
			"authIndex":                request.AuthIndex,
			"previousDegradationCount": state.DegradationCount,
		},
	})
	return true, err
}
