package wxaiinspection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/managerconfig"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const maxStoredBodyText = 2048

var (
	ErrRunAlreadyActive               = errors.New("wxai inspection is already running")
	ErrNotConfigured                  = errors.New("usage service is not configured")
	ErrRunNotFound                    = errors.New("wxai inspection run not found")
	ErrManualRefreshAccountNotFound   = errors.New("wxai inspection account not found")
	ErrManualRefreshRequiresServerRun = errors.New("至少有过一次服务器巡检")
	ErrWxaiAutoActionUnsupported      = errors.New("wXAi autoActionMode no longer supports enable, disable, or delete actions")
)

type Service struct {
	store                *store.Store
	managerConfigService *managerconfig.Service
	client               *http.Client
	authFileMutations    *cpaauthfiles.MutationCoordinator

	mutex                   sync.Mutex
	running                 bool
	realtimeDegradationMu   sync.Mutex
	wxaiSwitcherSnapshotMu  sync.Mutex
	wxaiSwitcherExitIPCache wxaiSwitcherExitIPSnapshot
}

type RunRequest struct {
	TriggerType string `json:"triggerType,omitempty"`
	TriggerKey  string `json:"triggerKey,omitempty"`
}

type ManualRefreshRequest struct {
	AccountKey string `json:"accountKey,omitempty"`
	FileName   string `json:"fileName,omitempty"`
	AuthIndex  string `json:"authIndex,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type RunDetail struct {
	Run     model.WxaiInspectionRun      `json:"run"`
	Results []model.WxaiInspectionResult `json:"results"`
	Logs    []model.WxaiInspectionLog    `json:"logs"`
}

type SettingsResponse struct {
	Settings model.ManagerWxaiInspectionConfig `json:"settings"`
	Exists   bool                              `json:"exists"`
}

type RealtimeDegradationRequest struct {
	AccountKey       string  `json:"accountKey"`
	FileName         string  `json:"fileName"`
	DisplayAccount   string  `json:"displayAccount"`
	AuthIndex        string  `json:"authIndex"`
	AccountID        string  `json:"accountId"`
	OriginalPriority *int    `json:"originalPriority,omitempty"`
	Reason           string  `json:"reason"`
	QualityLevel     string  `json:"qualityLevel"`
	TokensPerSecond  float64 `json:"tokensPerSecond"`
	RequestID        string  `json:"requestId"`
	ProxyURL         string  `json:"proxyUrl"`
}

type LatestCompletedScheduledRunResponse struct {
	Found bool                     `json:"found"`
	Run   *model.WxaiInspectionRun `json:"run"`
}

type account struct {
	Key            string
	RuntimeID      string
	FileName       string
	DisplayAccount string
	AuthIndex      string
	AccountID      string
	Priority       *int
	ScheduleGroup  *int
	AccountType    string
	Status         string
	State          string
}

func New(st *store.Store, managerConfigService *managerconfig.Service) *Service {
	return NewWithOptions(st, managerConfigService, ServiceOptions{})
}

type ServiceOptions struct {
	AuthFileMutationCoordinator *cpaauthfiles.MutationCoordinator
}

func NewWithOptions(st *store.Store, managerConfigService *managerconfig.Service, options ServiceOptions) *Service {
	coordinator := options.AuthFileMutationCoordinator
	if coordinator == nil {
		coordinator = cpaauthfiles.NewMutationCoordinator()
	}
	return &Service{
		store:                st,
		managerConfigService: managerConfigService,
		client:               &http.Client{Timeout: 60 * time.Second},
		authFileMutations:    coordinator,
	}
}

func (service *Service) IsRunning() bool {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	return service.running
}

func (service *Service) ResolveConfig(ctx context.Context) (model.ManagerWxaiInspectionConfig, bool, error) {
	settings, _, err := service.resolveRuntime(ctx)
	if errors.Is(err, ErrNotConfigured) {
		return model.DefaultWxaiInspectionConfig(), false, nil
	}
	return settings, err == nil, err
}

func (service *Service) Run(ctx context.Context, request RunRequest) (RunDetail, error) {
	if !service.acquireRun() {
		return RunDetail{}, ErrRunAlreadyActive
	}
	defer service.releaseRun()

	settings, setup, err := service.resolveRuntime(ctx)
	if err != nil {
		return RunDetail{}, err
	}
	if err := validateWxaiPriorityOnlyMode(settings); err != nil {
		return RunDetail{}, err
	}
	triggerType := strings.TrimSpace(request.TriggerType)
	if triggerType == "" {
		triggerType = model.WxaiInspectionTriggerManual
	}
	triggerKey := strings.TrimSpace(request.TriggerKey)
	if triggerKey == "" {
		triggerKey = triggerType
	}
	previousStatusItems, err := service.loadLatestWxaiAccountStatusItems(ctx)
	if err != nil {
		return RunDetail{}, err
	}
	run, err := service.store.CreateWxaiInspectionRun(ctx, model.WxaiInspectionRun{
		TriggerType: triggerType,
		TriggerKey:  triggerKey,
		Status:      model.WxaiInspectionStatusRunning,
		Settings:    model.SanitizeWxaiInspectionConfig(settings),
	})
	if err != nil {
		return RunDetail{}, err
	}
	logger := runLogger{service: service, runID: run.ID}
	logger.info(ctx, "wXAi 巡检开始", map[string]any{"triggerType": triggerType, "triggerKey": triggerKey})
	scheduleGroupCount, err := service.fetchScheduleGroupCount(ctx, setup)
	if err != nil {
		logger.error(ctx, "读取 xAI 调度组配置失败", map[string]any{"error": err.Error()})
		return service.failRun(ctx, run, err)
	}

	accounts, err := service.fetchAccounts(ctx, setup)
	if err != nil {
		logger.error(ctx, "读取 wXAi 授权文件失败", map[string]any{"error": err.Error()})
		return service.failRun(ctx, run, err)
	}
	selection, err := service.resolveWxaiServerInspectionSelection(
		ctx,
		accounts,
		time.Now(),
	)
	if err != nil {
		logger.error(ctx, "筛选 wXAi 服务端巡检账号失败", map[string]any{"error": err.Error()})
		return service.failRun(ctx, run, err)
	}
	run.TotalFiles = len(accounts)
	run.ProbeSetCount = len(selection.inspectionAccounts)
	run.SampledCount = 0
	run.DisabledCount = len(selection.disabledAccounts)
	run.EnabledCount = len(accounts) - len(selection.disabledAccounts)
	_ = service.store.UpdateWxaiInspectionRun(ctx, run)
	logger.info(ctx, "wXAi 服务端巡检候选已筛选", map[string]any{
		"totalCount":                len(accounts),
		"inspectionCount":           len(selection.inspectionAccounts),
		"disabledSkipCount":         len(selection.disabledAccounts),
		"botFlaggedSkipCount":       len(selection.botFlaggedAccounts),
		"realtimeCooldownSkipCount": len(selection.realtimeCooldownAccounts),
		"quotaCooldownSkipCount":    len(selection.quotaCooldownAccounts),
	})

	results := service.inspectAccounts(ctx, setup, settings, run.ID, selection.inspectionAccounts, logger)
	preservedResults := service.preserveWxaiServerInspectionAccounts(
		ctx,
		run.ID,
		selection.disabledAccounts,
		previousStatusItems,
		logger,
	)
	results = append(results, preservedResults...)
	botFlaggedResults := service.preserveWxaiServerInspectionAccounts(
		ctx,
		run.ID,
		selection.botFlaggedAccounts,
		previousStatusItems,
		logger,
	)
	results = append(results, botFlaggedResults...)
	realtimeCooldownResults := service.preserveWxaiRealtimeDegradationCooldownAccounts(
		ctx,
		run.ID,
		selection.realtimeCooldownAccounts,
		previousStatusItems,
		selection.realtimeCooldownUntilByKey,
		logger,
	)
	results = append(results, realtimeCooldownResults...)
	cooldownResults := service.preserveWxaiQuotaCooldownAccounts(
		ctx,
		run.ID,
		selection.quotaCooldownAccounts,
		previousStatusItems,
		selection.cooldownUntilByAccountKeyMS,
		logger,
	)
	results = append(results, cooldownResults...)
	run.QuotaExhaustedCount = countWxaiQuotaResults(results)
	run.AbnormalCount = countWxaiAbnormalResults(results)
	run.KeepCount = len(results) - run.QuotaExhaustedCount - run.AbnormalCount
	assignments, err := service.assignScheduleGroups(ctx, setup, scheduleGroupCount, accounts)
	if err != nil {
		logger.error(ctx, "分配 xAI 调度组失败", map[string]any{"error": err.Error()})
		return service.failRun(ctx, run, err)
	}
	if err := service.persistScheduleGroupAssignments(context.WithoutCancel(ctx), run.ID, assignments); err != nil {
		logger.error(ctx, "持久化 xAI 调度组状态失败", map[string]any{"error": err.Error()})
		return service.failRun(ctx, run, err)
	}
	if err := service.resetScheduleGroupCounters(context.WithoutCancel(ctx), setup); err != nil {
		logger.error(ctx, "重置 xAI 调度组调用次数失败", map[string]any{"error": err.Error()})
		return service.failRun(ctx, run, err)
	}
	run.Status = model.WxaiInspectionStatusCompleted
	run.FinishedAtMS = time.Now().UnixMilli()
	logger.info(ctx, "wXAi 巡检完成", map[string]any{
		"total": len(results), "keep": run.KeepCount, "abnormal": countWxaiAbnormalResults(results), "quotaExhausted": countWxaiQuotaResults(results),
	})
	if err := service.store.UpdateWxaiInspectionRun(ctx, run); err != nil {
		return RunDetail{}, err
	}
	// 巡检结束：把健康账号邮箱自动同步到 Grok2Api Console（trigger=keepalive）。
	healthyEmails := collectHealthyAccountEmails(results)
	if isWxaiGrok2apiSyncConfigured(settings) && len(healthyEmails) > 0 {
		go service.syncGrok2apiAfterRun(context.WithoutCancel(ctx), run.ID, healthyEmails)
	}
	return service.GetRun(ctx, run.ID)
}

// TriggerGrok2apiSync WebUI 手动触发：用最近一次已完成巡检的健康账号执行同步（trigger=manual）。
func (service *Service) TriggerGrok2apiSync(ctx context.Context) (Grok2apiSyncResponse, error) {
	run, found, err := service.store.GetLatestWxaiInspectionRun(ctx)
	if err != nil {
		return Grok2apiSyncResponse{}, err
	}
	if !found || run.Status != model.WxaiInspectionStatusCompleted {
		return Grok2apiSyncResponse{}, ErrManualRefreshRequiresServerRun
	}
	results, err := service.store.ListWxaiInspectionResults(ctx, run.ID)
	if err != nil {
		return Grok2apiSyncResponse{}, err
	}
	return service.SyncHealthyAccountsToGrok2api(ctx, Grok2apiSyncTriggerManual, collectHealthyAccountEmails(results))
}

func (service *Service) Latest(ctx context.Context) (model.WxaiAccountStatusResponse, error) {
	run, ok, err := service.store.GetLatestWxaiInspectionRun(ctx)
	if err != nil {
		return model.WxaiAccountStatusResponse{}, err
	}
	if !ok {
		return model.WxaiAccountStatusResponse{Items: []model.WxaiAccountStatusItem{}}, nil
	}
	items, err := service.store.ListWxaiAccountStatusItems(ctx, run.ID)
	if err != nil {
		return model.WxaiAccountStatusResponse{}, err
	}
	items = collapseWxaiLatestAccountStatusItems(items)
	service.attachWxaiExitIPs(ctx, items)
	windowCostsByAccount, err := service.listWxaiAccountWindowCostsByAccount(ctx, run.ID)
	if err != nil {
		return model.WxaiAccountStatusResponse{}, err
	}
	for itemIndex := range items {
		items[itemIndex].WindowCosts = windowCostsByAccount[items[itemIndex].AccountKey]
		adjustment, exists, adjustmentErr := service.store.GetWxaiPriorityAdjustment(ctx, items[itemIndex].AccountKey)
		if adjustmentErr != nil {
			return model.WxaiAccountStatusResponse{}, adjustmentErr
		}
		if exists {
			items[itemIndex].OriginalPriority = adjustment.OriginalPriority
			items[itemIndex].RecoverAtMS = adjustment.RecoverAtMS
		}
	}
	return model.WxaiAccountStatusResponse{Run: run, Items: items}, nil
}

func (service *Service) ListRuns(ctx context.Context, limit int) ([]model.WxaiInspectionRun, error) {
	return service.store.ListWxaiInspectionRuns(ctx, limit)
}

func (service *Service) LatestCompletedScheduledRun(ctx context.Context) (LatestCompletedScheduledRunResponse, error) {
	run, found, err := service.store.GetLatestCompletedWxaiInspectionRunByTriggerType(ctx, model.WxaiInspectionTriggerScheduled)
	if err != nil {
		return LatestCompletedScheduledRunResponse{}, err
	}
	if !found {
		return LatestCompletedScheduledRunResponse{Found: false}, nil
	}
	return LatestCompletedScheduledRunResponse{Found: true, Run: &run}, nil
}

func (service *Service) RecordRealtimeDegradation(ctx context.Context, request RealtimeDegradationRequest) error {
	service.realtimeDegradationMu.Lock()
	defer service.realtimeDegradationMu.Unlock()

	if strings.TrimSpace(request.AccountKey) == "" {
		return errors.New("account key is required")
	}
	if strings.TrimSpace(request.FileName) == "" {
		return errors.New("file name is required")
	}
	if strings.TrimSpace(request.DisplayAccount) == "" {
		return errors.New("display account is required")
	}

	_, setup, err := service.resolveRuntime(ctx)
	if err != nil {
		return err
	}
	accounts, err := service.fetchAccounts(ctx, setup)
	if err != nil {
		return err
	}
	matchedAccount, matched := newWxaiConditionalAccountMatcher(accounts).match(wxaiConditionalAccountRef{
		AccountKey: request.AccountKey,
		FileName:   request.FileName,
		AuthIndex:  request.AuthIndex,
		AccountID:  request.AccountID,
		Provider:   "xai",
	})
	if !matched {
		return fmt.Errorf("实时守护账号未匹配: fileName=%s authIndex=%s accountID=%s", request.FileName, request.AuthIndex, request.AccountID)
	}
	request.AccountKey = matchedAccount.Key
	request.FileName = matchedAccount.FileName
	request.DisplayAccount = matchedAccount.DisplayAccount
	request.AuthIndex = matchedAccount.AuthIndex
	request.AccountID = matchedAccount.AccountID

	stage, err := service.applyRealtimeDegradationState(ctx, setup, matchedAccount, request)
	if err != nil {
		return err
	}

	runID, err := service.latestReusableWxaiRunID(ctx)
	if err != nil || runID <= 0 {
		return err
	}
	statusDetail := model.WxaiAccountStatusDetail{
		RunID:       runID,
		AccountKey:  request.AccountKey,
		Priority:    intPointer(stage.Priority),
		CheckedAtMS: time.Now().UnixMilli(),
	}
	latestItems, err := service.store.ListWxaiAccountStatusItems(ctx, runID)
	if err != nil {
		return err
	}
	for _, item := range latestItems {
		if item.AccountKey != request.AccountKey {
			continue
		}
		statusDetail.AccountType = item.AccountType
		statusDetail.ScheduleGroup = item.ScheduleGroup
		statusDetail.WeeklyUsedPercent = item.WeeklyUsedPercent
		statusDetail.WeeklyResetAtMS = item.WeeklyResetAtMS
		statusDetail.MonthlyUsedPercent = item.MonthlyUsedPercent
		statusDetail.MonthlyResetAtMS = item.MonthlyResetAtMS
		statusDetail.MonthlyLimitCents = item.MonthlyLimitCents
		statusDetail.MonthlyUsedCents = item.MonthlyUsedCents
		break
	}
	if err := service.store.UpsertWxaiAccountStatusDetail(ctx, statusDetail); err != nil {
		return err
	}
	result := model.WxaiInspectionResult{
		RunID:          runID,
		AccountKey:     request.AccountKey,
		FileName:       request.FileName,
		DisplayAccount: request.DisplayAccount,
		AuthIndex:      request.AuthIndex,
		AccountID:      request.AccountID,
		Provider:       "xai",
		Status:         "abnormal",
		State:          "account_abnormal",
		Action:         "keep",
		ActionReason:   stage.ActionReason,
		ActionStatus:   model.WxaiInspectionActionStatusSuccess,
		ExecutedAction: stage.ExecutedAction,
		ErrorKind:      "position_degradation",
		ErrorDetail:    fmt.Sprintf("来源=realtime_guard；次数=%d；冷却至=%d；原因=%s；等级=%s；TPS=%.2f；请求=%s；代理=%s", stage.DegradationCount, stage.CooldownUntilMS, request.Reason, request.QualityLevel, request.TokensPerSecond, request.RequestID, request.ProxyURL),
	}
	if _, err := service.store.InsertWxaiInspectionResult(ctx, result); err != nil {
		return err
	}
	_, err = service.store.InsertWxaiInspectionLog(ctx, model.WxaiInspectionLog{
		RunID:   runID,
		Level:   "warn",
		Message: stage.LogMessage,
		Detail: map[string]any{
			"accountKey": request.AccountKey, "fileName": request.FileName, "authIndex": request.AuthIndex,
			"priority": stage.Priority, "reason": "position_degradation", "realtimeGuard": request.Reason,
			"degradationCount": stage.DegradationCount, "cooldownUntilMs": stage.CooldownUntilMS,
			"qualityLevel": request.QualityLevel, "tokensPerSecond": request.TokensPerSecond,
			"requestID": request.RequestID, "proxyURL": request.ProxyURL,
		},
	})
	return err
}

func (service *Service) GetRun(ctx context.Context, runID int64) (RunDetail, error) {
	run, ok, err := service.store.GetWxaiInspectionRun(ctx, runID)
	if err != nil {
		return RunDetail{}, err
	}
	if !ok {
		return RunDetail{}, ErrRunNotFound
	}
	results, err := service.store.ListWxaiInspectionResults(ctx, runID)
	if err != nil {
		return RunDetail{}, err
	}
	windowCostsByAccount, err := service.listWxaiAccountWindowCostsByAccount(ctx, runID)
	if err != nil {
		return RunDetail{}, err
	}
	for resultIndex := range results {
		results[resultIndex].WindowCosts = windowCostsByAccount[results[resultIndex].AccountKey]
	}
	logs, err := service.store.ListWxaiInspectionLogs(ctx, runID)
	if err != nil {
		return RunDetail{}, err
	}
	return RunDetail{Run: run, Results: results, Logs: logs}, nil
}

func (service *Service) GetSettings(ctx context.Context) (SettingsResponse, error) {
	settings, _, err := service.resolveRuntime(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	_, exists, err := service.store.GetWxaiInspectionSettings(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	return SettingsResponse{Settings: settings, Exists: exists}, nil
}

func (service *Service) SaveSettings(ctx context.Context, settings model.ManagerWxaiInspectionConfig) (SettingsResponse, error) {
	if err := model.ValidateWxaiInspectionConfig(settings); err != nil {
		return SettingsResponse{}, err
	}
	if err := validateWxaiPriorityOnlyMode(settings); err != nil {
		return SettingsResponse{}, err
	}
	if _, _, err := service.resolveRuntime(ctx); err != nil {
		return SettingsResponse{}, err
	}
	// 密码留空=保持已存值：repo 层归一化以默认配置为 fallback，感知不到已存密码，
	// 因此在 service 层先回填。
	if strings.TrimSpace(settings.Grok2apiAdminPassword) == "" {
		if stored, exists, loadErr := service.store.GetWxaiInspectionSettings(ctx); loadErr == nil && exists && stored.Grok2apiAdminPassword != "" {
			settings.Grok2apiAdminPassword = stored.Grok2apiAdminPassword
		}
	}
	saved, err := service.store.SaveWxaiInspectionSettings(ctx, settings)
	if err != nil {
		return SettingsResponse{}, err
	}
	return SettingsResponse{Settings: saved, Exists: true}, nil
}

func (service *Service) resolveRuntime(ctx context.Context) (model.ManagerWxaiInspectionConfig, store.Setup, error) {
	managerConfig, _, ok, err := service.managerConfigService.ResolveManagerConfigWithSource(ctx)
	if err != nil {
		return model.ManagerWxaiInspectionConfig{}, store.Setup{}, err
	}
	if !ok || strings.TrimSpace(managerConfig.CPAConnection.CPABaseURL) == "" || strings.TrimSpace(managerConfig.CPAConnection.ManagementKey) == "" {
		return model.ManagerWxaiInspectionConfig{}, store.Setup{}, ErrNotConfigured
	}
	settings := model.DefaultWxaiInspectionConfig()
	if stored, exists, loadErr := service.store.GetWxaiInspectionSettings(ctx); loadErr != nil {
		return model.ManagerWxaiInspectionConfig{}, store.Setup{}, loadErr
	} else if exists {
		settings = model.NormalizeWxaiInspectionConfig(stored, settings)
	}
	return settings, managerconfig.SetupFromManagerConfig(managerConfig), nil
}

func (service *Service) fetchAccounts(ctx context.Context, setup store.Setup) ([]account, error) {
	files, err := cpaauthfiles.New(service.client).Fetch(ctx, setup.CPAUpstreamURL, setup.ManagementKey)
	if err != nil {
		return nil, err
	}
	accounts := make([]account, 0, len(files))
	seenKeys := make(map[string]int, len(files))
	for index, file := range files {
		provider := normalizeWxaiProvider(firstNonEmpty(file.Provider, firstString(file.Raw, "provider", "type", "auth_type", "authType", "typo")))
		if provider != "xai" {
			continue
		}
		fileName := firstNonEmpty(file.Name, firstString(file.Raw, "name", "fileName", "file_name"))
		if fileName == "" {
			fileName = fmt.Sprintf("wxai-%d", index)
		}
		displayAccount := firstString(file.Raw, "label", "account", "email", "displayAccount", "display_account")
		if displayAccount == "" {
			displayAccount = fileName
		}
		authIndex := firstNonEmpty(file.AuthIndex, firstString(file.Raw, "auth_index", "authIndex", "index", "id"))
		accountID := firstNonEmpty(file.AccountID, firstString(file.Raw, "account_id", "accountId", "sub", "subject", "user_id", "userId"))
		baseKey := strings.Join([]string{fileName, displayAccount, authIndex, accountID}, "|")
		duplicateIndex := seenKeys[baseKey]
		seenKeys[baseKey] = duplicateIndex + 1
		accountKey := baseKey
		if duplicateIndex > 0 {
			accountKey = fmt.Sprintf("%s|duplicate-%d", baseKey, duplicateIndex)
		}
		accounts = append(accounts, account{
			Key:            accountKey,
			RuntimeID:      file.ID,
			FileName:       fileName,
			DisplayAccount: displayAccount,
			AuthIndex:      authIndex,
			AccountID:      accountID,
			Priority:       readNestedInt(file.Raw, "priority"),
			ScheduleGroup:  readNestedInt(file.Raw, "schedule_group"),
			Status:         firstString(file.Raw, "status"),
			State:          firstString(file.Raw, "state"),
		})
	}
	if err := service.attachWxaiAccountProfiles(ctx, accounts); err != nil {
		return nil, fmt.Errorf("读取 wXAi 账号类型: %w", err)
	}
	return accounts, nil
}

func (service *Service) inspectAccounts(
	ctx context.Context,
	setup store.Setup,
	settings model.ManagerWxaiInspectionConfig,
	runID int64,
	accounts []account,
	logger runLogger,
) []model.WxaiInspectionResult {
	workers := settings.Workers
	if workers <= 0 {
		workers = 1
	}
	if workers > len(accounts) {
		workers = len(accounts)
	}
	type inspectedResult struct {
		account account
		result  model.WxaiInspectionResult
	}
	jobs := make(chan account)
	results := make(chan inspectedResult, len(accounts))
	var waitGroup sync.WaitGroup
	workerStartStagger := model.WxaiWorkerStartStagger(settings)
	accountTakeStagger := model.WxaiAccountTakeStagger(settings)
	accountTakeGate := &wxaiProbeAccountTakeGate{interval: accountTakeStagger}

	go func() {
		defer close(jobs)
		for _, currentAccount := range accounts {
			select {
			case <-ctx.Done():
				return
			case jobs <- currentAccount:
			}
		}
	}()

	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		if workerIndex > 0 && workerStartStagger > 0 {
			staggerTimer := time.NewTimer(workerStartStagger)
			select {
			case <-ctx.Done():
				if !staggerTimer.Stop() {
					select {
					case <-staggerTimer.C:
					default:
					}
				}
				logger.info(context.WithoutCancel(ctx), "wXAi 探测 worker 错峰启动已中止", map[string]any{
					"startedWorkers": workerIndex,
					"plannedWorkers": workers,
				})
				goto waitForProbeWorkers
			case <-staggerTimer.C:
			}
		}
		waitGroup.Add(1)
		go func(startedWorkerIndex int) {
			defer waitGroup.Done()
			logger.info(context.WithoutCancel(ctx), "wXAi 探测 worker 已启动", map[string]any{
				"workerIndex":          startedWorkerIndex + 1,
				"plannedWorkers":       workers,
				"workerStaggerMs":      int(workerStartStagger / time.Millisecond),
				"accountTakeStaggerMs": int(accountTakeStagger / time.Millisecond),
			})
			for currentAccount := range jobs {
				if err := accountTakeGate.wait(ctx); err != nil {
					return
				}
				result, effectivePriority := service.inspectSingleAccount(
					ctx,
					setup,
					settings,
					runID,
					currentAccount,
					logger,
				)
				currentAccount.Priority = effectivePriority
				results <- inspectedResult{account: currentAccount, result: result}
			}
		}(workerIndex)
	}

waitForProbeWorkers:
	waitGroup.Wait()
	close(results)

	output := make([]model.WxaiInspectionResult, 0, len(accounts))
	for inspected := range results {
		storedResult, err := service.store.InsertWxaiInspectionResult(context.WithoutCancel(ctx), inspected.result)
		if err != nil {
			logger.error(context.WithoutCancel(ctx), "写入 wXAi 巡检结果失败", map[string]any{"fileName": inspected.account.FileName, "error": err.Error()})
			continue
		}
		inspected.result.ID = storedResult.ID
		persistContext := context.WithoutCancel(ctx)
		statusDetail, detailErr := service.writeAccountStatusDetail(
			persistContext,
			runID,
			inspected.account,
			inspected.result,
		)
		if detailErr != nil {
			logger.error(persistContext, "写入 wXAi 账号状态详情失败", map[string]any{
				"fileName": inspected.account.FileName,
				"error":    detailErr.Error(),
			})
		} else {
			service.captureWxaiAccountWindowCosts(
				persistContext,
				inspected.account,
				inspected.result,
				statusDetail,
				logger,
			)
		}
		output = append(output, inspected.result)
	}
	sort.Slice(output, func(leftIndex, rightIndex int) bool {
		return output[leftIndex].FileName < output[rightIndex].FileName
	})
	return output
}

// wxaiProbeAccountTakeGate 保证任意 worker 开始探测下一账号时，与上一账号开始时刻至少间隔 interval。
type wxaiProbeAccountTakeGate struct {
	mutex      sync.Mutex
	nextStart  time.Time
	interval   time.Duration
	hasStarted bool
}

func (gate *wxaiProbeAccountTakeGate) wait(ctx context.Context) error {
	if gate == nil {
		return nil
	}
	interval := gate.interval
	if interval <= 0 {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		gate.mutex.Lock()
		now := time.Now()
		if !gate.hasStarted || !now.Before(gate.nextStart) {
			gate.hasStarted = true
			gate.nextStart = now.Add(interval)
			gate.mutex.Unlock()
			return nil
		}
		waitDuration := gate.nextStart.Sub(now)
		gate.mutex.Unlock()
		timer := time.NewTimer(waitDuration)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (service *Service) writeAccountStatusDetail(
	ctx context.Context,
	runID int64,
	currentAccount account,
	result model.WxaiInspectionResult,
) (model.WxaiAccountStatusDetail, error) {
	detail := model.WxaiAccountStatusDetail{
		RunID:             runID,
		AccountKey:        result.AccountKey,
		Priority:          currentAccount.Priority,
		ScheduleGroup:     currentAccount.ScheduleGroup,
		AccountType:       firstNonEmpty(result.PlanType, currentAccount.AccountType),
		MonthlyLimitCents: result.MonthlyLimitCents,
		MonthlyUsedCents:  result.MonthlyUsedCents,
		CheckedAtMS:       time.Now().UnixMilli(),
	}
	for _, window := range result.QuotaWindows {
		switch window.ID {
		case "weekly":
			detail.WeeklyUsedPercent = window.UsedPercent
			detail.WeeklyResetAtMS = window.ResetAtMS
		case "monthly":
			detail.MonthlyUsedPercent = window.UsedPercent
			detail.MonthlyResetAtMS = window.ResetAtMS
		}
	}
	if err := service.store.UpsertWxaiAccountStatusDetail(ctx, detail); err != nil {
		return model.WxaiAccountStatusDetail{}, err
	}
	return detail, nil
}

func (service *Service) failRun(ctx context.Context, run model.WxaiInspectionRun, cause error) (RunDetail, error) {
	run.Status = model.WxaiInspectionStatusFailed
	run.Error = cause.Error()
	run.FinishedAtMS = time.Now().UnixMilli()
	_ = service.store.UpdateWxaiInspectionRun(context.WithoutCancel(ctx), run)
	return RunDetail{}, cause
}

func (service *Service) acquireRun() bool {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if service.running {
		return false
	}
	service.running = true
	return true
}

func (service *Service) releaseRun() {
	service.mutex.Lock()
	service.running = false
	service.mutex.Unlock()
}

func normalizeWxaiProvider(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "xai", "x-ai", "grok":
		return "xai"
	default:
		return normalized
	}
}

func readNestedInt(values map[string]any, key string) *int {
	if value, exists := values[key]; exists {
		if parsed, ok := readInteger(value); ok {
			return &parsed
		}
	}
	for _, nestedKey := range []string{"attributes", "metadata"} {
		nested, ok := mapValue(values, nestedKey)
		if !ok {
			continue
		}
		if value, exists := nested[key]; exists {
			if parsed, ok := readInteger(value); ok {
				return &parsed
			}
		}
	}
	return nil
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, exists := values[key]
		if !exists || value == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func readBool(values map[string]any, key string) bool {
	value, exists := values[key]
	if !exists {
		return false
	}
	switch typedValue := value.(type) {
	case bool:
		return typedValue
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typedValue))
		return parsed || strings.TrimSpace(typedValue) == "1"
	case float64:
		return typedValue != 0
	default:
		return false
	}
}

func asMap(value any) (map[string]any, bool) {
	typedValue, ok := value.(map[string]any)
	return typedValue, ok
}

func mapValue(values map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		value, exists := values[key]
		if !exists || value == nil {
			continue
		}
		if typedValue, ok := asMap(value); ok {
			return typedValue, true
		}
	}
	return nil, false
}

func readInteger(value any) (int, bool) {
	switch typedValue := value.(type) {
	case float64:
		return int(typedValue), true
	case int:
		return typedValue, true
	case int64:
		return int(typedValue), true
	case json.Number:
		parsed, err := typedValue.Int64()
		return int(parsed), err == nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typedValue))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func matchAccount(accounts []account, request ManualRefreshRequest) (account, bool) {
	accountKey := strings.TrimSpace(request.AccountKey)
	fileName := strings.ToLower(strings.TrimSpace(request.FileName))
	authIndex := strings.ToLower(strings.TrimSpace(request.AuthIndex))
	for _, currentAccount := range accounts {
		if accountKey != "" && currentAccount.Key == accountKey {
			return currentAccount, true
		}
		if fileName != "" && strings.ToLower(currentAccount.FileName) == fileName {
			if authIndex == "" || strings.ToLower(currentAccount.AuthIndex) == authIndex {
				return currentAccount, true
			}
		}
	}
	return account{}, false
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

type runLogger struct {
	service *Service
	runID   int64
	prefix  string
}

func (logger runLogger) error(ctx context.Context, message string, detail any) {
	logger.log(ctx, "error", message, detail)
}

func (logger runLogger) warning(ctx context.Context, message string, detail any) {
	logger.log(ctx, "warning", message, detail)
}

func (logger runLogger) info(ctx context.Context, message string, detail any) {
	logger.log(ctx, "info", message, detail)
}

func (logger runLogger) success(ctx context.Context, message string, detail any) {
	logger.log(ctx, "success", message, detail)
}

func (logger runLogger) log(ctx context.Context, level string, message string, detail any) {
	if logger.service == nil || logger.runID <= 0 {
		return
	}
	_, _ = logger.service.store.InsertWxaiInspectionLog(context.WithoutCancel(ctx), model.WxaiInspectionLog{
		RunID: logger.runID, Level: level, Message: logger.prefix + message, Detail: detail,
	})
}
