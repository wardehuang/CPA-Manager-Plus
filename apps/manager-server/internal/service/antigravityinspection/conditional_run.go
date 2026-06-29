package antigravityinspection

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func (s *Service) runConditional(ctx context.Context, req ConditionalRunRequest) (RunDetail, error) {
	if req.RunID <= 0 {
		return RunDetail{}, ErrRunNotFound
	}

	provider := model.NormalizeAntigravityTargetProvider(req.TargetProvider, model.AntigravityTargetProviderServer)
	settings, setup, err := s.resolveRuntime(ctx, provider)
	if err != nil {
		return RunDetail{}, err
	}
	if settings.Enabled == nil || !*settings.Enabled {
		return s.GetRun(ctx, req.RunID)
	}

	detail, err := s.GetRun(ctx, req.RunID)
	if err != nil {
		return RunDetail{}, err
	}
	if strings.TrimSpace(detail.Run.TargetProvider) != "" {
		settings.TargetProvider = model.NormalizeAntigravityTargetProvider(detail.Run.TargetProvider, settings.TargetProvider)
	}

	persistCtx := context.WithoutCancel(ctx)
	logger := runLogger{service: s, runID: req.RunID, prefix: "[Agy 条件巡检] "}
	quietLogger := runLogger{}
	files, err := s.fetchAuthFiles(ctx, setup)
	if err != nil {
		logger.error(persistCtx, "加载 Agy 授权文件列表失败", map[string]any{"error": err.Error()})
		return s.getRunWithCause(persistCtx, req.RunID, err)
	}

	accounts := s.accountsFromFiles(files, settings.TargetProvider)
	if len(accounts) == 0 {
		return s.GetRun(persistCtx, req.RunID)
	}

	nowMS := time.Now().UnixMilli()
	selected, reasonsByKey, err := s.resolveConditionalAccounts(ctx, req.RunID, accounts, nowMS, quietLogger)
	if err != nil {
		logger.error(persistCtx, "解析 Agy 条件巡检候选账号失败", map[string]any{"error": err.Error()})
		return s.getRunWithCause(persistCtx, req.RunID, err)
	}
	if len(selected) == 0 {
		return s.GetRun(persistCtx, req.RunID)
	}

	results := s.inspectAccounts(ctx, setup, settings, req.RunID, selected, quietLogger)
	if err := ctx.Err(); err != nil {
		logger.warning(persistCtx, "Agy 条件巡检已取消", map[string]any{"error": err.Error(), "candidateCount": len(selected), "finishedCount": len(results)})
		return s.getRunWithCause(persistCtx, req.RunID, err)
	}

	for _, result := range results {
		logConditionalResult(persistCtx, logger, result, reasonsByKey[result.AccountKey])
	}
	return s.GetRun(persistCtx, req.RunID)
}

func logConditionalResult(ctx context.Context, logger runLogger, result model.AntigravityInspectionResult, reasons []string) {
	level := "info"
	if result.Error != "" || result.Action == "delete" || result.Action == "reauth" || result.Action == "disable" {
		level = "warning"
	}
	logger.log(ctx, level, "Agy 账号刷新完成", map[string]any{
		"fileName":       result.FileName,
		"displayAccount": result.DisplayAccount,
		"authIndex":      result.AuthIndex,
		"accountId":      result.AccountID,
		"targetProvider": result.TargetProvider,
		"reasons":        reasons,
		"action":         result.Action,
		"actionReason":   result.ActionReason,
		"statusCode":     result.StatusCode,
		"usedPercent":    result.UsedPercent,
		"isQuota":        result.IsQuota,
		"error":          result.Error,
	})
}

func (s *Service) getRunWithCause(ctx context.Context, runID int64, cause error) (RunDetail, error) {
	detail, err := s.GetRun(ctx, runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("%w; load run: %v", cause, err)
	}
	return detail, cause
}
