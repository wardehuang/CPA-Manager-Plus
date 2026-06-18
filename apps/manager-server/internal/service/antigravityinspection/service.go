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
	antigravityProbeURL = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	maxStoredBodyText   = 2048
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
	files, err := s.fetchAuthFiles(ctx, setup)
	if err != nil {
		return s.failRun(ctx, run, err)
	}
	accounts := s.accountsFromFiles(files, settings.TargetProvider)
	run.TotalFiles = len(files)
	run.ProbeSetCount = len(accounts)
	run.DisabledCount, run.EnabledCount = countDisabled(accounts)
	sampled := pickSample(accounts, settings.SampleSize)
	run.SampledCount = len(sampled)
	_ = s.store.UpdateAntigravityInspectionRun(ctx, run)
	results := s.inspectAccounts(ctx, setup, settings, run.ID, sampled, logger)
	for _, result := range results {
		summarizeResult(&run, result)
	}
	run.Status = model.AntigravityInspectionStatusCompleted
	run.FinishedAtMS = time.Now().UnixMilli()
	if err := s.store.UpdateAntigravityInspectionRun(ctx, run); err != nil {
		return RunDetail{}, err
	}
	return s.GetRun(ctx, run.ID)
}

func (s *Service) RunConditional(ctx context.Context, req ConditionalRunRequest) (RunDetail, error) {
	return s.Run(ctx, RunRequest{TriggerType: model.AntigravityInspectionTriggerScheduled, TriggerKey: "conditional", TargetProvider: req.TargetProvider})
}

func (s *Service) RunManualRefresh(ctx context.Context, req ManualRefreshRequest) (RunDetail, error) {
	provider := model.NormalizeAntigravityTargetProvider(req.TargetProvider, model.AntigravityTargetProviderClaude)
	settings, setup, err := s.resolveRuntime(ctx, provider)
	if err != nil {
		return RunDetail{}, err
	}
	files, err := s.fetchAuthFiles(ctx, setup)
	if err != nil {
		return RunDetail{}, err
	}
	accounts := s.accountsFromFiles(files, provider)
	selected := make([]account, 0, 1)
	for _, item := range accounts {
		if req.AccountKey != "" && item.Key == req.AccountKey || req.AuthIndex != "" && item.AuthIndex == req.AuthIndex || req.FileName != "" && item.FileName == req.FileName {
			selected = append(selected, item)
			break
		}
	}
	if len(selected) == 0 {
		return RunDetail{}, ErrManualRefreshAccountNotFound
	}
	return s.Run(ctx, RunRequest{TriggerType: model.AntigravityInspectionTriggerManual, TriggerKey: "manual-refresh", TargetProvider: settings.TargetProvider})
}

func (s *Service) ListRuns(ctx context.Context, limit int) ([]model.AntigravityInspectionRun, error) {
	return s.store.ListAntigravityInspectionRuns(ctx, limit)
}

func (s *Service) GetRun(ctx context.Context, id int64) (RunDetail, error) {
	run, ok, err := s.store.GetAntigravityInspectionRun(ctx, id)
	if err != nil {
		return RunDetail{}, err
	}
	if !ok {
		return RunDetail{}, ErrRunNotFound
	}
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
		key := strings.Join([]string{fileName, authIndex, accountID, targetProvider}, "|")
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
			ProjectID:      readString(file, "project_id", "projectId"),
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
	jobs := make(chan account)
	results := make(chan model.AntigravityInspectionResult, len(accounts))
	var wg sync.WaitGroup
	for i := 0; i < workers && i < len(accounts); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				result := s.inspectSingleAccount(ctx, setup, settings, runID, item)
				if _, err := s.store.InsertAntigravityInspectionResult(ctx, result); err != nil {
					logger.error(ctx, "写入 Agy 巡检账号结果失败", map[string]any{"fileName": item.FileName, "error": err.Error()})
				}
				_ = s.store.UpsertAntigravityAccountStatusDetail(ctx, model.AntigravityAccountStatusDetail{
					RunID:          runID,
					AccountKey:     result.AccountKey,
					TargetProvider: settings.TargetProvider,
					Priority:       item.Priority,
					UsedPercent:    result.UsedPercent,
					CheckedAtMS:    time.Now().UnixMilli(),
				})
				results <- result
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
	for result := range results {
		out = append(out, result)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FileName < out[j].FileName })
	return out
}

func (s *Service) inspectSingleAccount(ctx context.Context, setup store.Setup, settings model.ManagerAntigravityInspectionConfig, runID int64, item account) model.AntigravityInspectionResult {
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
	res, err := s.requestAntigravityProbe(ctx, setup, settings, item)
	if err != nil {
		base.Error = err.Error()
		base.ActionReason = "Antigravity 探测失败"
		base.Action = "reauth"
		base.ActionStatus = model.AntigravityInspectionActionStatusNeedsReview
		return base
	}
	if res.HasStatusCode {
		base.StatusCode = &res.StatusCode
	}
	if res.StatusCode == http.StatusUnauthorized {
		base.Action = "reauth"
		base.ActionReason = "Antigravity 探测返回 401，需要重新授权"
		base.ActionStatus = model.AntigravityInspectionActionStatusNeedsReview
		base.ErrorKind = "unauthorized"
		base.ErrorDetail = truncate(res.BodyText, maxStoredBodyText)
	} else if res.StatusCode >= 400 {
		base.Action = "reauth"
		base.ActionReason = fmt.Sprintf("Antigravity 探测返回 HTTP %d", res.StatusCode)
		base.ActionStatus = model.AntigravityInspectionActionStatusNeedsReview
		base.ErrorDetail = truncate(res.BodyText, maxStoredBodyText)
	}
	return base
}

func (s *Service) requestAntigravityProbe(ctx context.Context, setup store.Setup, settings model.ManagerAntigravityInspectionConfig, item account) (apiCallResponse, error) {
	headers := map[string]string{"Authorization": "Bearer $TOKEN$", "Content-Type": "application/json", "User-Agent": settings.UserAgent}
	data := "{}"
	if strings.TrimSpace(item.ProjectID) != "" {
		payload, _ := json.Marshal(map[string]string{"project": strings.TrimSpace(item.ProjectID)})
		data = string(payload)
	}
	payload := map[string]any{"authIndex": item.AuthIndex, "method": http.MethodPost, "url": antigravityProbeURL, "header": headers, "data": data}
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

func readPriority(file authFile, targetProvider string) *int {
	key := "priority_claude"
	if model.NormalizeAntigravityTargetProvider(targetProvider, model.AntigravityTargetProviderClaude) == model.AntigravityTargetProviderGemini {
		key = "priority_gemini"
	}
	value, ok := file[key]
	if !ok || value == nil {
		return nil
	}
	parsed := int(readFloat(value, 0))
	return &parsed
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

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

type runLogger struct {
	service *Service
	runID   int64
}

func (l runLogger) error(ctx context.Context, message string, detail any) {
	_, _ = l.service.store.InsertAntigravityInspectionLog(ctx, model.AntigravityInspectionLog{RunID: l.runID, Level: "error", Message: message, Detail: detail})
}
