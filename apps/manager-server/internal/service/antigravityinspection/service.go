package antigravityinspection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/managerconfig"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const (
	antigravityQuotaSummaryURL    = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
	antigravityAvailableModelsURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	defaultAntigravityUserAgent   = "antigravity/1.11.5 windows/amd64"
	legacyAntigravityUserAgent    = "cpa-manager-plus-antigravity-inspection"
	defaultAntigravityProjectID   = "bamboo-precept-lgxtn"
	maxStoredBodyText             = 2048
)

var (
	ErrRunAlreadyActive             = errors.New("antigravity inspection is already running")
	ErrNotConfigured                = errors.New("usage service is not configured")
	ErrRunNotFound                  = errors.New("antigravity inspection run not found")
	ErrRunNotCompleted              = errors.New("antigravity inspection run is not completed")
	ErrActionIDsRequired            = errors.New("antigravity inspection action result ids are required")
	ErrNoActionableResults          = errors.New("antigravity inspection has no actionable results")
	ErrManualRefreshAccountNotFound = errors.New("antigravity inspection account not found")
)

type Service struct {
	store                *store.Store
	managerConfigService *managerconfig.Service
	client               *http.Client

	mu      sync.Mutex
	running bool
}

type RunRequest struct {
	TriggerType    string `json:"triggerType,omitempty"`
	TriggerKey     string `json:"triggerKey,omitempty"`
	TargetProvider string `json:"targetProvider,omitempty"`
}

type ConditionalRunRequest struct {
	RunID          int64
	TargetProvider string
}

type ManualRefreshRequest struct {
	AccountKey     string `json:"accountKey,omitempty"`
	FileName       string `json:"fileName,omitempty"`
	AuthIndex      string `json:"authIndex,omitempty"`
	TargetProvider string `json:"targetProvider,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type RunDetail struct {
	Run     model.AntigravityInspectionRun      `json:"run"`
	Results []model.AntigravityInspectionResult `json:"results"`
	Logs    []model.AntigravityInspectionLog    `json:"logs"`
}

type ExecuteActionsRequest struct {
	ResultIDs []int64 `json:"resultIds"`
}

type ActionOutcome struct {
	ResultID       int64  `json:"resultId,omitempty"`
	AccountKey     string `json:"accountKey,omitempty"`
	FileName       string `json:"fileName"`
	DisplayAccount string `json:"displayAccount"`
	Action         string `json:"action"`
	Status         string `json:"status"`
	Success        bool   `json:"success"`
	Error          string `json:"error,omitempty"`
}

type ExecuteActionsResult struct {
	Outcomes []ActionOutcome `json:"outcomes"`
	Detail   RunDetail       `json:"detail"`
}

type SettingsResponse struct {
	Settings model.ManagerAntigravityInspectionConfig `json:"settings"`
	Exists   bool                                     `json:"exists"`
}

type authFile map[string]any

type account struct {
	Key            string
	FileName       string
	DisplayAccount string
	AuthIndex      string
	AccountID      string
	Priority       *int
	Provider       string
	Disabled       bool
	Status         string
	State          string
	ProjectID      string
	File           authFile
}

type apiCallResponse struct {
	StatusCode    int
	HasStatusCode bool
	BodyText      string
	Body          any
}

func New(st *store.Store, managerConfigService *managerconfig.Service) *Service {
	return &Service{store: st, managerConfigService: managerConfigService, client: &http.Client{Timeout: 60 * time.Second}}
}

func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Service) Run(ctx context.Context, req RunRequest) (RunDetail, error) {
	if !s.acquireRun() {
		return RunDetail{}, ErrRunAlreadyActive
	}
	defer s.releaseRun()
	settings, setup, err := s.resolveRuntime(ctx, req.TargetProvider)
	if err != nil {
		return RunDetail{}, err
	}
	triggerType := strings.TrimSpace(req.TriggerType)
	if triggerType == "" {
		triggerType = model.AntigravityInspectionTriggerManual
	}
	triggerKey := strings.TrimSpace(req.TriggerKey)
	if triggerKey == "" {
		triggerKey = triggerType
	}
	run, err := s.store.CreateAntigravityInspectionRun(ctx, model.AntigravityInspectionRun{
		TriggerType:    triggerType,
		TriggerKey:     triggerKey,
		TargetProvider: settings.TargetProvider,
		Status:         model.AntigravityInspectionStatusRunning,
		Settings:       settings,
	})
	if err != nil {
		return RunDetail{}, err
	}
	logger := runLogger{service: s, runID: run.ID}
	logger.info(ctx, "Agy 巡检开始", map[string]any{"targetProvider": settings.TargetProvider, "triggerType": triggerType, "triggerKey": triggerKey})
	files, err := s.fetchAuthFiles(ctx, setup)
	if err != nil {
		logger.error(ctx, "读取 Agy 授权文件失败", map[string]any{"error": err.Error()})
		return s.failRun(ctx, run, err)
	}
	accounts := s.accountsFromFiles(files, settings.TargetProvider)
	run.TotalFiles = len(accounts)
	run.ProbeSetCount = len(accounts)
	run.DisabledCount, run.EnabledCount = countDisabled(accounts)
	sampled := pickSample(accounts, settings.SampleSize)
	run.SampledCount = len(sampled)
	logger.info(ctx, "Agy 授权文件读取完成", map[string]any{"authFiles": len(files), "totalFiles": len(accounts), "matchedAccounts": len(accounts), "sampledAccounts": len(sampled)})
	if len(sampled) == 0 {
		logger.warning(ctx, "未找到可巡检的 Agy 账号", map[string]any{"targetProvider": settings.TargetProvider})
	}
	_ = s.store.UpdateAntigravityInspectionRun(ctx, run)
	results := s.inspectAccounts(ctx, setup, settings, run.ID, sampled, logger)
	for _, result := range results {
		summarizeResult(&run, result)
	}
	run.Status = model.AntigravityInspectionStatusCompleted
	run.FinishedAtMS = time.Now().UnixMilli()
	logger.success(ctx, "Agy 巡检完成", map[string]any{"total": len(results), "keep": run.KeepCount, "reauth": run.ReauthCount, "disable": run.DisableCount, "enable": run.EnableCount, "delete": run.DeleteCount})
	if err := s.store.UpdateAntigravityInspectionRun(ctx, run); err != nil {
		return RunDetail{}, err
	}
	return s.GetRun(ctx, run.ID)
}

func (s *Service) RunConditional(ctx context.Context, req ConditionalRunRequest) (RunDetail, error) {
	return s.runConditional(ctx, req)
}

func (s *Service) RunManualRefresh(ctx context.Context, req ManualRefreshRequest) (RunDetail, error) {
	return s.runManualRefresh(ctx, req)
}

func (s *Service) ListRuns(ctx context.Context, limit int) ([]model.AntigravityInspectionRun, error) {
	runs, err := s.store.ListAntigravityInspectionRuns(ctx, limit)
	if err != nil {
		return nil, err
	}
	for index := range runs {
		normalizeAntigravityRunCounts(&runs[index])
	}
	return runs, nil
}

func (s *Service) GetRun(ctx context.Context, id int64) (RunDetail, error) {
	run, ok, err := s.store.GetAntigravityInspectionRun(ctx, id)
	if err != nil {
		return RunDetail{}, err
	}
	if !ok {
		return RunDetail{}, ErrRunNotFound
	}
	normalizeAntigravityRunCounts(&run)
	results, err := s.store.ListAntigravityInspectionResults(ctx, id)
	if err != nil {
		return RunDetail{}, err
	}
	logs, err := s.store.ListAntigravityInspectionLogs(ctx, id)
	if err != nil {
		return RunDetail{}, err
	}
	return RunDetail{Run: run, Results: results, Logs: logs}, nil
}

func (s *Service) ExecuteManualActions(ctx context.Context, runID int64, req ExecuteActionsRequest) (ExecuteActionsResult, error) {
	if len(req.ResultIDs) == 0 {
		return ExecuteActionsResult{}, ErrActionIDsRequired
	}
	run, ok, err := s.store.GetAntigravityInspectionRun(ctx, runID)
	if err != nil {
		return ExecuteActionsResult{}, err
	}
	if !ok {
		return ExecuteActionsResult{}, ErrRunNotFound
	}
	if run.Status != model.AntigravityInspectionStatusCompleted {
		return ExecuteActionsResult{}, ErrRunNotCompleted
	}
	_, setup, err := s.resolveRuntime(ctx, run.TargetProvider)
	if err != nil {
		return ExecuteActionsResult{}, err
	}
	results, err := s.store.ListAntigravityInspectionResults(ctx, runID)
	if err != nil {
		return ExecuteActionsResult{}, err
	}
	selected := selectResults(results, req.ResultIDs)
	if len(selected) == 0 {
		return ExecuteActionsResult{}, ErrNoActionableResults
	}
	outcomes := make([]ActionOutcome, 0, len(selected))
	for _, item := range selected {
		outcome := ActionOutcome{ResultID: item.ID, AccountKey: item.AccountKey, FileName: item.FileName, DisplayAccount: item.DisplayAccount, Action: item.Action}
		if item.Action != "delete" && item.Action != "disable" && item.Action != "enable" {
			outcome.Status = model.AntigravityInspectionActionStatusSkipped
			outcome.Error = "unsupported action"
		} else if err := s.executeAction(ctx, setup, item); err != nil {
			outcome.Status = model.AntigravityInspectionActionStatusFailed
			outcome.Error = err.Error()
		} else {
			outcome.Status = model.AntigravityInspectionActionStatusSuccess
			outcome.Success = true
		}
		item.ActionStatus = outcome.Status
		item.ExecutedAction = item.Action
		item.ActionError = outcome.Error
		_, _ = s.store.InsertAntigravityInspectionResult(ctx, item)
		outcomes = append(outcomes, outcome)
	}
	detail, err := s.GetRun(ctx, runID)
	if err != nil {
		return ExecuteActionsResult{}, err
	}
	return ExecuteActionsResult{Outcomes: outcomes, Detail: detail}, nil
}

func (s *Service) GetSettings(ctx context.Context, targetProvider string) (SettingsResponse, error) {
	settings, _, err := s.resolveRuntime(ctx, targetProvider)
	if err != nil {
		return SettingsResponse{}, err
	}
	_, exists, err := s.store.GetAntigravityInspectionSettings(ctx, settings.TargetProvider)
	if err != nil {
		return SettingsResponse{}, err
	}
	return SettingsResponse{Settings: settings, Exists: exists}, nil
}

func (s *Service) SaveSettings(ctx context.Context, targetProvider string, settings model.ManagerAntigravityInspectionConfig) (SettingsResponse, error) {
	managerCfg, _, ok, err := s.managerConfigService.ResolveManagerConfigWithSource(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	if !ok || strings.TrimSpace(managerCfg.CPAConnection.CPABaseURL) == "" || strings.TrimSpace(managerCfg.CPAConnection.ManagementKey) == "" {
		return SettingsResponse{}, ErrNotConfigured
	}
	targetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderServer)
	settings.TargetProvider = targetProvider
	saved, err := s.store.SaveAntigravityInspectionSettings(ctx, targetProvider, settings)
	if err != nil {
		return SettingsResponse{}, err
	}
	return SettingsResponse{Settings: saved, Exists: true}, nil
}

func (s *Service) ResolveConfig(ctx context.Context) (model.ManagerAntigravityInspectionConfig, bool, error) {
	settings, _, err := s.resolveRuntime(ctx, model.AntigravityTargetProviderServer)
	if errors.Is(err, ErrNotConfigured) {
		return settings, false, nil
	}
	return settings, err == nil, err
}

func (s *Service) resolveRuntime(ctx context.Context, targetProvider string) (model.ManagerAntigravityInspectionConfig, store.Setup, error) {
	managerCfg, _, ok, err := s.managerConfigService.ResolveManagerConfigWithSource(ctx)
	if err != nil {
		return model.ManagerAntigravityInspectionConfig{}, store.Setup{}, err
	}
	if !ok || strings.TrimSpace(managerCfg.CPAConnection.CPABaseURL) == "" || strings.TrimSpace(managerCfg.CPAConnection.ManagementKey) == "" {
		return model.ManagerAntigravityInspectionConfig{}, store.Setup{}, ErrNotConfigured
	}
	settings := model.DefaultAntigravityInspectionConfig()
	settings.TargetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, settings.TargetProvider)
	if stored, ok, err := s.store.GetAntigravityInspectionSettings(ctx, settings.TargetProvider); err != nil {
		return model.ManagerAntigravityInspectionConfig{}, store.Setup{}, err
	} else if ok {
		settings = model.NormalizeAntigravityInspectionConfig(stored, settings)
		settings.TargetProvider = model.NormalizeAntigravityTargetProvider(targetProvider, settings.TargetProvider)
	}
	return settings, managerconfig.SetupFromManagerConfig(managerCfg), nil
}

func (s *Service) failRun(ctx context.Context, run model.AntigravityInspectionRun, cause error) (RunDetail, error) {
	run.Status = model.AntigravityInspectionStatusFailed
	run.Error = cause.Error()
	run.FinishedAtMS = time.Now().UnixMilli()
	_ = s.store.UpdateAntigravityInspectionRun(ctx, run)
	detail, err := s.GetRun(ctx, run.ID)
	if err != nil {
		return RunDetail{}, err
	}
	return detail, cause
}

func (s *Service) fetchAuthFiles(ctx context.Context, setup store.Setup) ([]authFile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cpa.NormalizeBaseURL(setup.CPAUpstreamURL)+"/v0/management/auth-files", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+setup.ManagementKey)
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("auth files request failed: %s %s", res.Status, truncate(string(body), maxStoredBodyText))
	}
	var payload struct {
		Files []authFile `json:"files"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Files, nil
}

func (s *Service) accountsFromFiles(files []authFile, targetProvider string) []account {
	accounts := make([]account, 0, len(files))
	seenKeys := make(map[string]int, len(files))
	for index, file := range files {
		provider := strings.ToLower(readString(file, "provider", "type", "auth_type"))
		if provider != "antigravity" {
			continue
		}
		fileName := readString(file, "name", "fileName", "file_name")
		if fileName == "" {
			fileName = fmt.Sprintf("antigravity-%d", index)
		}
		display := readString(file, "label", "account", "email", "displayAccount", "display_account")
		if display == "" {
			display = fileName
		}
		authIndex := readString(file, "auth_index", "authIndex", "index", "id")
		accountID := readString(file, "account_id", "accountId")
		priority := readPriority(file, targetProvider)
		key := strings.Join([]string{fileName, display, authIndex, accountID, targetProvider}, "|")
		if seen := seenKeys[key]; seen > 0 {
			key = fmt.Sprintf("%s|duplicate-%d", key, index)
		}
		seenKeys[key]++
		accounts = append(accounts, account{
			Key:            key,
			FileName:       fileName,
			DisplayAccount: display,
			AuthIndex:      authIndex,
			AccountID:      accountID,
			Priority:       priority,
			Provider:       "antigravity",
			Disabled:       readBool(file, "disabled"),
			Status:         readString(file, "status"),
			State:          readString(file, "state"),
			ProjectID:      readAntigravityProjectID(file),
			File:           file,
		})
	}
	return accounts
}

func (s *Service) inspectAccounts(ctx context.Context, setup store.Setup, settings model.ManagerAntigravityInspectionConfig, runID int64, accounts []account, logger runLogger) []model.AntigravityInspectionResult {
	workers := settings.Workers
	if workers <= 0 {
		workers = 1
	}
	type inspectedResult struct {
		item   account
		result model.AntigravityInspectionResult
	}
	jobs := make(chan account)
	results := make(chan inspectedResult, len(accounts))
	var wg sync.WaitGroup
	for i := 0; i < workers && i < len(accounts); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				result := s.inspectSingleAccount(ctx, setup, settings, runID, item, logger)
				results <- inspectedResult{item: item, result: result}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, item := range accounts {
			select {
			case <-ctx.Done():
				return
			case jobs <- item:
			}
		}
	}()
	wg.Wait()
	close(results)
	out := make([]model.AntigravityInspectionResult, 0, len(accounts))
	for inspected := range results {
		if _, err := s.store.InsertAntigravityInspectionResult(ctx, inspected.result); err != nil {
			logger.error(ctx, "写入 Agy 巡检账号结果失败", map[string]any{"fileName": inspected.item.FileName, "error": err.Error()})
		}
		s.writeAccountStatusDetails(ctx, runID, settings.TargetProvider, inspected.item, inspected.result)
		out = append(out, inspected.result)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FileName < out[j].FileName })
	return out
}

func (s *Service) writeAccountStatusDetails(ctx context.Context, runID int64, targetProvider string, item account, result model.AntigravityInspectionResult) {
	providers := []string{model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude)}
	if providers[0] == model.AntigravityTargetProviderServer {
		providers = []string{model.AntigravityTargetProviderServer, model.AntigravityTargetProviderClaude, model.AntigravityTargetProviderGemini}
	}
	checkedAt := time.Now().UnixMilli()
	for _, provider := range providers {
		detailPriority := readPriority(item.File, provider)
		providerQuotaWindows := model.FilterAntigravityQuotaWindows(result.QuotaWindows, provider)
		detailUsedPercent := model.MaxAntigravityQuotaUsedPercent(providerQuotaWindows)
		if result.IsQuota {
			quotaPriority := -1
			detailPriority = &quotaPriority
		}
		providerQuotaExhausted := result.IsQuota || (detailPriority != nil && *detailPriority == -1)
		if detailUsedPercent == nil && providerQuotaExhausted {
			usedPercent := float64(100)
			detailUsedPercent = &usedPercent
		}
		_ = s.store.UpsertAntigravityAccountStatusDetail(ctx, model.AntigravityAccountStatusDetail{
			RunID:          runID,
			AccountKey:     result.AccountKey,
			TargetProvider: provider,
			Priority:       detailPriority,
			UsedPercent:    detailUsedPercent,
			ResetAtMS:      model.FirstAntigravityQuotaResetAt(providerQuotaWindows),
			CheckedAtMS:    checkedAt,
		})
	}
}

func (s *Service) inspectSingleAccount(ctx context.Context, setup store.Setup, settings model.ManagerAntigravityInspectionConfig, runID int64, item account, logger runLogger) model.AntigravityInspectionResult {
	base := model.AntigravityInspectionResult{
		RunID:          runID,
		AccountKey:     item.Key,
		FileName:       item.FileName,
		DisplayAccount: item.DisplayAccount,
		AuthIndex:      item.AuthIndex,
		AccountID:      item.AccountID,
		Provider:       item.Provider,
		TargetProvider: settings.TargetProvider,
		Disabled:       item.Disabled,
		Status:         item.Status,
		State:          item.State,
		Action:         "keep",
		ActionReason:   "Antigravity 可用性探测通过",
		ActionStatus:   model.AntigravityInspectionActionStatusNone,
		CreatedAtMS:    time.Now().UnixMilli(),
	}
	if item.Disabled {
		base.ActionReason = "账号已停用，跳过动作"
	}
	if strings.TrimSpace(item.AuthIndex) == "" {
		base.Error = "缺少 auth_index"
		base.ErrorKind = "missing_auth_index"
		base.ErrorDetail = "缺少 auth_index"
		base.ActionReason = "缺少 auth_index，跳过探测"
		logger.warning(ctx, "Agy 账号缺少 auth_index，跳过探测", map[string]any{"fileName": item.FileName, "displayAccount": item.DisplayAccount})
		return base
	}
	var res apiCallResponse
	var err error
	for attempt := 0; attempt <= settings.Retries; attempt++ {
		res, err = s.requestAntigravityProbe(ctx, setup, settings, item)
		if err == nil {
			break
		}
	}
	if err != nil {
		base.Error = err.Error()
		base.ActionReason = "Antigravity 探测失败"
		base.Action = "reauth"
		base.ActionStatus = model.AntigravityInspectionActionStatusNeedsReview
		base.ErrorKind = "request_error"
		base.ErrorDetail = truncate(err.Error(), maxStoredBodyText)
		logger.warning(ctx, "Agy 账号探测失败", map[string]any{"fileName": item.FileName, "displayAccount": item.DisplayAccount, "error": err.Error()})
		return base
	}
	if res.HasStatusCode {
		base.StatusCode = &res.StatusCode
	}
	applyAntigravityQuotaSnapshot(&base, res)
	if isAntigravityQuotaResponse(res) {
		usedPercent := float64(100)
		base.UsedPercent = &usedPercent
		base.IsQuota = true
		base.Action = "disable"
		base.ActionReason = "Antigravity 额度耗尽"
		base.ActionStatus = model.AntigravityInspectionActionStatusPending
		base.ErrorKind = "quota_exhausted"
		base.ErrorDetail = truncate(res.BodyText, maxStoredBodyText)
		logger.warning(ctx, "Agy 账号额度耗尽", map[string]any{"fileName": item.FileName, "displayAccount": item.DisplayAccount, "statusCode": res.StatusCode})
	} else if res.StatusCode == http.StatusUnauthorized {
		base.Action = "reauth"
		base.ActionReason = "Antigravity 探测返回 401，需要重新授权"
		base.ActionStatus = model.AntigravityInspectionActionStatusNeedsReview
		base.ErrorKind = "unauthorized"
		base.ErrorDetail = truncate(res.BodyText, maxStoredBodyText)
		logger.warning(ctx, "Agy 账号授权失效", map[string]any{"fileName": item.FileName, "displayAccount": item.DisplayAccount, "statusCode": res.StatusCode})
	} else if res.StatusCode >= 400 {
		base.Action = "reauth"
		base.ActionReason = fmt.Sprintf("Antigravity 探测返回 HTTP %d", res.StatusCode)
		base.ActionStatus = model.AntigravityInspectionActionStatusNeedsReview
		base.ErrorDetail = truncate(res.BodyText, maxStoredBodyText)
		logger.warning(ctx, "Agy 账号探测返回错误状态", map[string]any{"fileName": item.FileName, "displayAccount": item.DisplayAccount, "statusCode": res.StatusCode})
	} else {
		logger.info(ctx, "Agy 账号探测完成", map[string]any{"fileName": item.FileName, "displayAccount": item.DisplayAccount, "statusCode": res.StatusCode})
	}
	return base
}

func (s *Service) requestAntigravityProbe(ctx context.Context, setup store.Setup, settings model.ManagerAntigravityInspectionConfig, item account) (apiCallResponse, error) {
	headers := map[string]string{"Authorization": "Bearer $TOKEN$", "Content-Type": "application/json", "User-Agent": normalizeAntigravityUserAgent(settings.UserAgent)}
	projectID := strings.TrimSpace(item.ProjectID)
	if projectID == "" {
		projectID = defaultAntigravityProjectID
	}
	dataPayload, _ := json.Marshal(map[string]string{"project": projectID})
	data := string(dataPayload)

	quotaSummaryResponse, quotaSummaryError := s.requestAntigravityEndpoint(
		ctx,
		setup,
		settings,
		item.AuthIndex,
		antigravityQuotaSummaryURL,
		headers,
		data,
	)
	quotaSummarySucceeded := quotaSummaryError == nil &&
		quotaSummaryResponse.StatusCode >= http.StatusOK &&
		quotaSummaryResponse.StatusCode < http.StatusMultipleChoices
	if quotaSummarySucceeded {
		if root, ok := quotaSummaryResponse.Body.(map[string]any); ok && len(buildAntigravityQuotaWindowsFromGroups(root)) > 0 {
			return quotaSummaryResponse, nil
		}
	}

	availableModelsResponse, availableModelsError := s.requestAntigravityEndpoint(
		ctx,
		setup,
		settings,
		item.AuthIndex,
		antigravityAvailableModelsURL,
		headers,
		data,
	)
	if availableModelsError != nil {
		if quotaSummaryError != nil {
			return apiCallResponse{}, fmt.Errorf("quota summary failed: %v; available models failed: %w", quotaSummaryError, availableModelsError)
		}
		return apiCallResponse{}, availableModelsError
	}
	return availableModelsResponse, nil
}

func (s *Service) requestAntigravityEndpoint(
	ctx context.Context,
	setup store.Setup,
	settings model.ManagerAntigravityInspectionConfig,
	authIndex string,
	targetURL string,
	headers map[string]string,
	data string,
) (apiCallResponse, error) {
	payload := map[string]any{"authIndex": authIndex, "method": http.MethodPost, "url": targetURL, "header": headers, "data": data}
	body, err := json.Marshal(payload)
	if err != nil {
		return apiCallResponse{}, err
	}
	requestCtx := ctx
	cancel := func() {}
	if settings.Timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(settings.Timeout)*time.Millisecond)
	}
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, cpa.NormalizeBaseURL(setup.CPAUpstreamURL)+"/v0/management/api-call", bytes.NewReader(body))
	if err != nil {
		return apiCallResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+setup.ManagementKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return apiCallResponse{}, err
	}
	defer res.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return apiCallResponse{}, fmt.Errorf("api-call failed: %s %s", res.Status, truncate(string(responseBody), maxStoredBodyText))
	}
	var raw map[string]any
	if err := json.Unmarshal(responseBody, &raw); err != nil {
		return apiCallResponse{}, err
	}
	statusRaw := firstValue(raw, "status_code", "statusCode")
	statusCode := int(readFloat(statusRaw, 0))
	bodyRaw := firstValue(raw, "body")
	bodyText, bodyValue := normalizeBody(bodyRaw)
	return apiCallResponse{StatusCode: statusCode, HasStatusCode: strings.TrimSpace(fmt.Sprint(statusRaw)) != "", BodyText: bodyText, Body: bodyValue}, nil
}

func (s *Service) executeAction(ctx context.Context, setup store.Setup, item model.AntigravityInspectionResult) error {
	switch item.Action {
	case "delete":
		return s.deleteAuthFile(ctx, setup, item.FileName)
	case "disable", "enable":
		payload := map[string]any{"name": item.FileName, "disabled": item.Action == "disable"}
		return s.patchAuthFile(ctx, setup, payload)
	default:
		return nil
	}
}

func (s *Service) deleteAuthFile(ctx context.Context, setup store.Setup, fileName string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, cpa.NormalizeBaseURL(setup.CPAUpstreamURL)+"/v0/management/auth-files?name="+url.QueryEscape(fileName), nil)
	if err != nil {
		return err
	}
	return s.doCPAAction(req, setup.ManagementKey)
}

func (s *Service) patchAuthFile(ctx context.Context, setup store.Setup, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, cpa.NormalizeBaseURL(setup.CPAUpstreamURL)+"/v0/management/auth-files/status", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.doCPAAction(req, setup.ManagementKey)
}

func (s *Service) doCPAAction(req *http.Request, managementKey string) error {
	req.Header.Set("Authorization", "Bearer "+managementKey)
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1024*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("%s %s", res.Status, truncate(string(body), maxStoredBodyText))
	}
	return nil
}

func (s *Service) acquireRun() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *Service) releaseRun() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
}

func countDisabled(accounts []account) (disabled int, enabled int) {
	for _, item := range accounts {
		if item.Disabled {
			disabled++
		} else {
			enabled++
		}
	}
	return disabled, enabled
}

func summarizeResult(run *model.AntigravityInspectionRun, result model.AntigravityInspectionResult) {
	switch result.Action {
	case "delete":
		run.DeleteCount++
	case "disable":
		run.DisableCount++
	case "enable":
		run.EnableCount++
	case "reauth":
		run.ReauthCount++
	default:
		run.KeepCount++
	}
}

func normalizeAntigravityRunCounts(run *model.AntigravityInspectionRun) {
	if run == nil {
		return
	}
	if run.ProbeSetCount > 0 && run.TotalFiles > run.ProbeSetCount {
		run.TotalFiles = run.ProbeSetCount
	}
}

func pickSample(accounts []account, size int) []account {
	if size <= 0 || size >= len(accounts) {
		return accounts
	}
	out := append([]account(nil), accounts...)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out[:size]
}

func selectResults(results []model.AntigravityInspectionResult, ids []int64) []model.AntigravityInspectionResult {
	selected := make([]model.AntigravityInspectionResult, 0, len(ids))
	wanted := map[int64]struct{}{}
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	for _, result := range results {
		if _, ok := wanted[result.ID]; ok {
			selected = append(selected, result)
		}
	}
	return selected
}

func readString(file authFile, keys ...string) string {
	for _, key := range keys {
		if value, ok := file[key]; ok && value != nil {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func readBool(file authFile, keys ...string) bool {
	for _, key := range keys {
		value, ok := file[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.TrimSpace(typed) == "1"
		case float64:
			return typed != 0
		}
	}
	return false
}

func readAntigravityProjectID(file authFile) string {
	if projectID := readString(file, "project_id", "projectId"); projectID != "" {
		return projectID
	}
	for _, key := range []string{"installed", "web"} {
		nested, ok := file[key]
		if !ok || nested == nil {
			continue
		}
		if values, ok := nested.(map[string]any); ok {
			if projectID := readString(authFile(values), "project_id", "projectId"); projectID != "" {
				return projectID
			}
		}
	}
	return ""
}

func normalizeAntigravityUserAgent(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == legacyAntigravityUserAgent {
		return defaultAntigravityUserAgent
	}
	return trimmed
}

func readPriority(file authFile, targetProvider string) *int {
	keys := []string{"priority_claude", "priority"}
	if model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude) == model.AntigravityTargetProviderGemini {
		keys = []string{"priority_gemini", "priority"}
	}
	for _, key := range keys {
		value, ok := readAuthFileValue(file, key)
		if !ok || value == nil {
			continue
		}
		parsed, ok := readInt(value)
		if ok {
			return &parsed
		}
	}
	return nil
}

func readAuthFileValue(file authFile, key string) (any, bool) {
	if value, ok := file[key]; ok && value != nil {
		return value, true
	}
	for _, nestedKey := range []string{"attributes", "metadata"} {
		nested, ok := file[nestedKey]
		if !ok || nested == nil {
			continue
		}
		switch typed := nested.(type) {
		case map[string]any:
			if value, ok := typed[key]; ok && value != nil {
				return value, true
			}
		case map[string]string:
			if value, ok := typed[key]; ok && strings.TrimSpace(value) != "" {
				return value, true
			}
		}
	}
	return nil, false
}

func applyAntigravityQuotaSnapshot(result *model.AntigravityInspectionResult, response apiCallResponse) {
	if result == nil {
		return
	}
	windows := buildAntigravityQuotaWindows(response.Body)
	if len(windows) == 0 {
		return
	}
	result.QuotaWindows = windows
	for _, window := range windows {
		if window.UsedPercent == nil {
			continue
		}
		if result.UsedPercent == nil || *window.UsedPercent > *result.UsedPercent {
			value := *window.UsedPercent
			result.UsedPercent = &value
		}
	}
}

func buildAntigravityQuotaWindows(body any) []model.AntigravityInspectionQuotaWindow {
	root, ok := body.(map[string]any)
	if !ok || root == nil {
		return nil
	}
	groupWindows := buildAntigravityQuotaWindowsFromGroups(root)
	models, ok := readMapValue(root, "models")
	if !ok || len(models) == 0 {
		return groupWindows
	}
	type quotaCandidate struct {
		id        string
		label     string
		used      *float64
		resetText string
		resetAt   int64
	}
	candidates := make([]quotaCandidate, 0, len(models))
	for modelID, raw := range models {
		modelMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		quotaInfo, ok := readMapValue(modelMap, "quotaInfo", "quota_info")
		if !ok {
			continue
		}
		remainingFraction, ok := readQuotaFraction(firstValue(quotaInfo, "remainingFraction", "remaining_fraction", "remaining"))
		if !ok {
			continue
		}
		usedPercent := (1 - remainingFraction) * 100
		if usedPercent < 0 {
			usedPercent = 0
		}
		if usedPercent > 100 {
			usedPercent = 100
		}
		resetText := readString(authFile(quotaInfo), "resetTime", "reset_time")
		candidates = append(candidates, quotaCandidate{
			id:        normalizeAntigravityQuotaWindowID(modelID),
			label:     readString(authFile(modelMap), "displayName", "display_name"),
			used:      &usedPercent,
			resetText: resetText,
			resetAt:   parseAntigravityResetTime(resetText),
		})
	}
	if len(candidates) == 0 {
		return groupWindows
	}
	windows := make([]model.AntigravityInspectionQuotaWindow, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.used == nil {
			continue
		}
		label := strings.TrimSpace(candidate.label)
		if label == "" {
			label = candidate.id
		}
		windows = append(windows, model.AntigravityInspectionQuotaWindow{
			ID:          candidate.id,
			LabelKey:    label,
			UsedPercent: candidate.used,
			ResetAtMS:   candidate.resetAt,
			ResetLabel:  candidate.resetText,
		})
	}
	return mergeAntigravityQuotaWindows(groupWindows, windows)
}

func readMapValue(raw map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		if typed, ok := value.(map[string]any); ok {
			return typed, true
		}
	}
	return nil, false
}

func readQuotaFraction(value any) (float64, bool) {
	parsed, ok := readFloatStrict(value)
	if !ok {
		return 0, false
	}
	if parsed > 1 {
		parsed = parsed / 100
	}
	if parsed < 0 {
		parsed = 0
	}
	if parsed > 1 {
		parsed = 1
	}
	return parsed, true
}

func normalizeAntigravityQuotaWindowID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "quota"
	}
	replacer := strings.NewReplacer("/", "-", "_", "-", ".", "-", " ", "-")
	return replacer.Replace(value)
}

func parseAntigravityResetTime(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UnixMilli()
		}
	}
	return 0
}

func readInt(value any) (int, bool) {
	parsed, ok := readFloatStrict(value)
	if !ok {
		return 0, false
	}
	return int(parsed), true
}

func readFloatStrict(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		var parsed float64
		if _, err := fmt.Sscan(trimmed, &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func firstValue(raw map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			return value
		}
	}
	return nil
}

func readFloat(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		var parsed float64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}

func normalizeBody(value any) (string, any) {
	switch typed := value.(type) {
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(typed), &parsed); err == nil {
			return typed, parsed
		}
		return typed, typed
	case nil:
		return "", nil
	default:
		data, _ := json.Marshal(typed)
		return string(data), typed
	}
}

func isAntigravityQuotaResponse(response apiCallResponse) bool {
	text := strings.ToLower(response.BodyText)
	if response.StatusCode == http.StatusPaymentRequired || response.StatusCode == http.StatusTooManyRequests {
		return strings.Contains(text, "quota") || strings.Contains(text, "credit") || strings.Contains(text, "limit") || strings.Contains(text, "resource_exhausted")
	}
	return strings.Contains(text, "quota exhausted") || strings.Contains(text, "quota_exhausted") || strings.Contains(text, "credit exhausted") || strings.Contains(text, "limit reached")
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

func (l runLogger) error(ctx context.Context, message string, detail any) {
	l.log(ctx, "error", message, detail)
}

func (l runLogger) warning(ctx context.Context, message string, detail any) {
	l.log(ctx, "warning", message, detail)
}

func (l runLogger) info(ctx context.Context, message string, detail any) {
	l.log(ctx, "info", message, detail)
}

func (l runLogger) success(ctx context.Context, message string, detail any) {
	l.log(ctx, "success", message, detail)
}

func (l runLogger) log(ctx context.Context, level string, message string, detail any) {
	if l.service == nil || l.service.store == nil || l.runID <= 0 {
		return
	}
	_, _ = l.service.store.InsertAntigravityInspectionLog(ctx, model.AntigravityInspectionLog{RunID: l.runID, Level: level, Message: l.prefix + message, Detail: detail})
}
