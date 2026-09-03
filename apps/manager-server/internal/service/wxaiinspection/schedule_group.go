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

func scheduleGroupAccountSortKey(currentAccount account) string {
	return strings.Join([]string{
		strings.TrimSpace(currentAccount.RuntimeID),
		strings.TrimSpace(currentAccount.FileName),
		strings.TrimSpace(currentAccount.AuthIndex),
		strings.TrimSpace(currentAccount.AccountID),
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
	accounts []account,
) ([]scheduleGroupAssignment, error) {
	if service.authFileMutations == nil {
		return nil, cpaauthfiles.ErrMutationCoordinatorUnavailable
	}
	release, err := service.authFileMutations.AcquireAll(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	xaiAccounts := append([]account(nil), accounts...)
	sort.SliceStable(xaiAccounts, func(left, right int) bool {
		leftPrimary := firstNonEmpty(strings.TrimSpace(xaiAccounts[left].RuntimeID), strings.TrimSpace(xaiAccounts[left].FileName))
		rightPrimary := firstNonEmpty(strings.TrimSpace(xaiAccounts[right].RuntimeID), strings.TrimSpace(xaiAccounts[right].FileName))
		if leftPrimary != rightPrimary {
			return leftPrimary < rightPrimary
		}
		return scheduleGroupAccountSortKey(xaiAccounts[left]) < scheduleGroupAccountSortKey(xaiAccounts[right])
	})

	assignments := make([]scheduleGroupAssignment, 0, len(xaiAccounts))
	authClient := cpaauthfiles.New(service.client)
	for index, currentAccount := range xaiAccounts {
		fileName := strings.TrimSpace(currentAccount.FileName)
		if fileName == "" {
			return nil, fmt.Errorf("xAI auth %q 缺少文件名", strings.TrimSpace(currentAccount.RuntimeID))
		}
		group := (index % groupCount) + 1
		if err := authClient.PatchScheduleGroup(ctx, setup.CPAUpstreamURL, setup.ManagementKey, fileName, group); err != nil {
			return nil, fmt.Errorf("写入 xAI 调度组 file=%s group=%d: %w", fileName, group, err)
		}
		assignments = append(assignments, scheduleGroupAssignment{
			AccountKey: currentAccount.Key,
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
