package wxaiinspection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const (
	xaiScheduleGroupConfigPath        = "/schedule-groups/config"
	xaiScheduleGroupResetCountersPath = "/schedule-groups/counters/reset"
)

type scheduleGroupAssignment struct {
	AccountKey string
	Group      int
}

type scheduleGroupConfigResponse struct {
	Data struct {
		GroupCount int `json:"groupCount"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type scheduleGroupResetResponse struct {
	Data struct {
		Reset bool `json:"reset"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func scheduleGroupFileKey(file cpaauthfiles.File) string {
	return strings.Join([]string{
		strings.TrimSpace(file.ID),
		strings.TrimSpace(file.Name),
		strings.TrimSpace(file.AuthIndex),
		strings.TrimSpace(file.AccountID),
	}, "\x00")
}

func (service *Service) fetchScheduleGroupCount(ctx context.Context, setup store.Setup) (int, error) {
	var response scheduleGroupConfigResponse
	if err := service.doXaiSwitcherManagementRequest(ctx, setup, http.MethodGet, xaiScheduleGroupConfigPath, nil, &response); err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%s: %s", response.Error.Code, response.Error.Message)
	}
	if response.Data.GroupCount <= 0 {
		return 0, fmt.Errorf("xAI 调度组数量无效: %d", response.Data.GroupCount)
	}
	return response.Data.GroupCount, nil
}

func (service *Service) resetScheduleGroupCounters(ctx context.Context, setup store.Setup) error {
	var response scheduleGroupResetResponse
	if err := service.doXaiSwitcherManagementRequest(ctx, setup, http.MethodPost, xaiScheduleGroupResetCountersPath, map[string]any{}, &response); err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("%s: %s", response.Error.Code, response.Error.Message)
	}
	if !response.Data.Reset {
		return fmt.Errorf("xAI 调度组调用次数未重置")
	}
	return nil
}

func (service *Service) doXaiSwitcherManagementRequest(
	ctx context.Context,
	setup store.Setup,
	method string,
	path string,
	payload any,
	output any,
) error {
	proxyRequest := struct {
		Method string `json:"method"`
		Path   string `json:"path"`
		Body   any    `json:"body,omitempty"`
	}{
		Method: method,
		Path:   path,
		Body:   payload,
	}
	data, err := json.Marshal(proxyRequest)
	if err != nil {
		return fmt.Errorf("%s %s 编码请求: %w", method, path, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cpa.NormalizeBaseURL(setup.CPAUpstreamURL)+wxaiSwitcherManagementAPIPath, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	request.Header.Set("Authorization", "Bearer "+setup.ManagementKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CPA-XAI-IP-Switcher-UI", "1")
	response, err := service.client.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%s %s: HTTP %d %s", method, path, response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if output == nil {
		return fmt.Errorf("%s %s 缺少响应目标", method, path)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("%s %s 解码响应: %w", method, path, err)
	}
	return nil
}

func (service *Service) assignScheduleGroups(
	ctx context.Context,
	setup store.Setup,
	groupCount int,
) ([]scheduleGroupAssignment, error) {
	if service.authFileMutations == nil {
		return nil, cpaauthfiles.ErrMutationCoordinatorUnavailable
	}
	release, err := service.authFileMutations.AcquireAll(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	files, err := cpaauthfiles.New(service.client).Fetch(ctx, setup.CPAUpstreamURL, setup.ManagementKey)
	if err != nil {
		return nil, fmt.Errorf("重新读取 xAI 授权文件: %w", err)
	}
	xaiFiles := make([]cpaauthfiles.File, 0, len(files))
	accountKeys := make(map[string]string, len(files))
	seenKeys := make(map[string]int, len(files))
	for _, file := range files {
		if normalizeWxaiProvider(firstNonEmpty(file.Provider, firstString(file.Raw, "provider", "type", "auth_type", "authType", "typo"))) != "xai" {
			continue
		}
		xaiFiles = append(xaiFiles, file)
		fileName := firstNonEmpty(file.Name, firstString(file.Raw, "name", "fileName", "file_name"))
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
		accountKeys[scheduleGroupFileKey(file)] = accountKey
	}
	sort.SliceStable(xaiFiles, func(left, right int) bool {
		leftPrimary := firstNonEmpty(strings.TrimSpace(xaiFiles[left].ID), strings.TrimSpace(xaiFiles[left].Name))
		rightPrimary := firstNonEmpty(strings.TrimSpace(xaiFiles[right].ID), strings.TrimSpace(xaiFiles[right].Name))
		if leftPrimary != rightPrimary {
			return leftPrimary < rightPrimary
		}
		return scheduleGroupFileKey(xaiFiles[left]) < scheduleGroupFileKey(xaiFiles[right])
	})

	assignments := make([]scheduleGroupAssignment, 0, len(xaiFiles))
	authClient := cpaauthfiles.New(service.client)
	for index, file := range xaiFiles {
		fileName := firstNonEmpty(file.Name, firstString(file.Raw, "name", "fileName", "file_name"))
		if fileName == "" {
			return nil, fmt.Errorf("xAI auth %q 缺少文件名", strings.TrimSpace(file.ID))
		}
		group := (index % groupCount) + 1
		if err := authClient.PatchScheduleGroup(ctx, setup.CPAUpstreamURL, setup.ManagementKey, fileName, group); err != nil {
			return nil, fmt.Errorf("写入 xAI 调度组 file=%s group=%d: %w", fileName, group, err)
		}
		accountKey := accountKeys[scheduleGroupFileKey(file)]
		assignments = append(assignments, scheduleGroupAssignment{
			AccountKey: accountKey,
			Group:      group,
		})
	}
	return assignments, nil
}

func (service *Service) persistScheduleGroupAssignments(
	ctx context.Context,
	runID int64,
	assignments []scheduleGroupAssignment,
) error {
	groups := make(map[string]int, len(assignments))
	for _, assignment := range assignments {
		groups[assignment.AccountKey] = assignment.Group
	}
	if err := service.store.UpdateWxaiAccountScheduleGroups(ctx, runID, groups); err != nil {
		return fmt.Errorf("持久化 xAI 调度组状态: %w", err)
	}
	return nil
}
