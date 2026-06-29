package antigravityinspection

import (
	"context"
	"fmt"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func (s *Service) runManualRefresh(ctx context.Context, req ManualRefreshRequest) (RunDetail, error) {
	provider := model.NormalizeAntigravityTargetProvider(req.TargetProvider, model.AntigravityTargetProviderClaude)
	settings, setup, err := s.resolveRuntime(ctx, provider)
	if err != nil {
		return RunDetail{}, err
	}
	runs, err := s.store.ListAntigravityInspectionRuns(ctx, 1)
	if err != nil {
		return RunDetail{}, err
	}
	if len(runs) == 0 || runs[0].ID <= 0 {
		return RunDetail{}, ErrRunNotFound
	}
	run := runs[0]
	if strings.TrimSpace(run.TargetProvider) != "" {
		settings.TargetProvider = model.NormalizeAntigravityTargetProvider(run.TargetProvider, settings.TargetProvider)
	}

	persistCtx := context.WithoutCancel(ctx)
	logger := runLogger{service: s, runID: run.ID, prefix: "【Agy 手动刷新】 "}
	quietLogger := runLogger{}

	files, err := s.fetchAuthFiles(ctx, setup)
	if err != nil {
		logger.error(persistCtx, "加载 Agy 授权文件列表失败", map[string]any{"error": err.Error()})
		return s.getRunWithCause(persistCtx, run.ID, err)
	}

	accounts := s.accountsFromFiles(files, settings.TargetProvider)
	selected, ok := matchManualRefreshAccount(accounts, req)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrManualRefreshAccountNotFound, firstNonEmpty(req.AccountKey, req.FileName, req.AuthIndex))
		logger.warning(persistCtx, "Agy 账号未匹配", map[string]any{"accountKey": req.AccountKey, "fileName": req.FileName, "authIndex": req.AuthIndex})
		return s.getRunWithCause(persistCtx, run.ID, err)
	}

	results := s.inspectAccounts(ctx, setup, settings, run.ID, []account{selected}, quietLogger)
	if err := ctx.Err(); err != nil {
		logger.warning(persistCtx, "Agy 账号刷新已取消", map[string]any{"error": err.Error()})
		return s.getRunWithCause(persistCtx, run.ID, err)
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "手动刷新"
	}
	for _, result := range results {
		logConditionalResult(persistCtx, logger, result, []string{reason})
	}
	return s.GetRun(persistCtx, run.ID)
}

func matchManualRefreshAccount(accounts []account, req ManualRefreshRequest) (account, bool) {
	accountKey := strings.TrimSpace(req.AccountKey)
	fileName := normalizeConditionalKey(req.FileName)
	authIndex := normalizeConditionalKey(req.AuthIndex)
	for _, item := range accounts {
		if accountKey != "" && item.Key == accountKey {
			return item, true
		}
		if fileName != "" && normalizeConditionalKey(item.FileName) == fileName {
			if authIndex == "" || normalizeConditionalKey(item.AuthIndex) == authIndex {
				return item, true
			}
		}
	}
	return account{}, false
}
