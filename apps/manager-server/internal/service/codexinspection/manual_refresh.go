package codexinspection

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrManualRefreshAccountNotFound = errors.New("codex account not found")

type ManualRefreshRequest struct {
	AccountKey string `json:"accountKey"`
	FileName   string `json:"fileName"`
	AuthIndex  string `json:"authIndex"`
	Reason     string `json:"reason"`
}

func (s *Service) RunManualRefresh(ctx context.Context, req ManualRefreshRequest) (RunDetail, error) {
	settings, setup, err := s.resolveRuntime(ctx)
	if err != nil {
		return RunDetail{}, err
	}
	runs, err := s.store.ListCodexInspectionRuns(ctx, 1)
	if err != nil {
		return RunDetail{}, err
	}
	if len(runs) == 0 || runs[0].ID <= 0 {
		return RunDetail{}, ErrRunNotFound
	}
	run := runs[0]
	if strings.TrimSpace(run.Settings.TargetType) != "" {
		settings.TargetType = run.Settings.TargetType
	}

	persistCtx := context.WithoutCancel(ctx)
	logger := runLogger{service: s, runID: run.ID, prefix: "【手动刷新】 "}
	// Keep runID so account-status hooks still attach to the parent run; omit
	// service so per-account probe chatter stays quiet.
	quietLogger := runLogger{runID: run.ID}

	files, err := s.fetchAuthFiles(ctx, setup)
	if err != nil {
		logger.error(persistCtx, "加载认证文件列表失败", map[string]any{"error": err.Error()})
		return s.getRunWithCause(persistCtx, run.ID, err)
	}

	accounts := make([]account, 0, len(files))
	for _, file := range files {
		next := toAccount(file)
		if next.Provider == settings.TargetType {
			accounts = append(accounts, next)
		}
	}

	selected, ok := matchManualRefreshAccount(accounts, req)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrManualRefreshAccountNotFound, firstNonEmpty(req.AccountKey, req.FileName, req.AuthIndex))
		logger.warning(persistCtx, "账号未匹配", map[string]any{
			"accountKey": req.AccountKey,
			"fileName":   req.FileName,
			"authIndex":  req.AuthIndex,
		})
		return s.getRunWithCause(persistCtx, run.ID, err)
	}

	results := s.inspectAccounts(ctx, setup, settings, []account{selected}, quietLogger)
	if err := ctx.Err(); err != nil {
		_ = s.persistInspectionResults(persistCtx, run.ID, results, logger)
		logger.warning(persistCtx, "账号刷新已取消", map[string]any{"error": run.Error})
		return s.getRunWithCause(persistCtx, run.ID, err)
	}
	if failures := s.persistInspectionResults(persistCtx, run.ID, results, logger); failures > 0 {
		logger.warning(persistCtx, "部分手动刷新结果写入失败", map[string]any{
			"failureCount": failures,
			"resultCount":  len(results),
		})
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
