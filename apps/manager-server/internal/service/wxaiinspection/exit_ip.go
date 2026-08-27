package wxaiinspection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

const (
	wxaiExitIPUnavailable               = "未知"
	wxaiSwitcherManagementAPIPath       = "/v0/management/cpa-xai-ip-switcher/api"
	wxaiSwitcherExitIPPath              = "/auth-bindings/exit-ips"
	wxaiSwitcherSnapshotTTL             = 15 * time.Second
	wxaiSwitcherRequestTimeout          = 5 * time.Second
	wxaiSwitcherMaxResponseBytes  int64 = 8 * 1024 * 1024
)

type wxaiSwitcherExitIPBinding struct {
	AuthName   string `json:"authName"`
	AuthIndex  string `json:"authIndex"`
	SyncStatus string `json:"syncStatus"`
	ExitIP     string `json:"exitIp"`
}

type wxaiSwitcherExitIPResponse struct {
	Data struct {
		Items []wxaiSwitcherExitIPBinding `json:"items"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type wxaiSwitcherExitIPSnapshot struct {
	baseURL     string
	expiresAt   time.Time
	byAuthName  map[string]string
	byAuthIndex map[string]string
}

func (service *Service) attachWxaiExitIPs(ctx context.Context, items []model.WxaiAccountStatusItem) {
	for itemIndex := range items {
		items[itemIndex].ExitIP = wxaiExitIPUnavailable
	}
	if len(items) == 0 {
		return
	}

	_, setup, err := service.resolveRuntime(ctx)
	if err != nil {
		return
	}
	snapshot, err := service.loadWxaiSwitcherExitIPSnapshot(ctx, setup.CPAUpstreamURL, setup.ManagementKey)
	if err != nil {
		return
	}
	for itemIndex := range items {
		authName := normalizeWxaiSwitcherKey(items[itemIndex].FileName)
		if exitIP, found := snapshot.byAuthName[authName]; found {
			items[itemIndex].ExitIP = exitIP
			continue
		}
		authIndex := normalizeWxaiSwitcherKey(items[itemIndex].AuthIndex)
		if exitIP, found := snapshot.byAuthIndex[authIndex]; found {
			items[itemIndex].ExitIP = exitIP
		}
	}
}

func (service *Service) loadWxaiSwitcherExitIPSnapshot(
	ctx context.Context,
	baseURL string,
	managementKey string,
) (wxaiSwitcherExitIPSnapshot, error) {
	baseURL = cpa.NormalizeBaseURL(baseURL)
	now := time.Now()
	service.wxaiSwitcherSnapshotMu.Lock()
	cached := service.wxaiSwitcherExitIPCache
	if cached.baseURL == baseURL && cached.expiresAt.After(now) {
		service.wxaiSwitcherSnapshotMu.Unlock()
		return cached, nil
	}
	service.wxaiSwitcherSnapshotMu.Unlock()

	items, err := service.fetchWxaiSwitcherExitIPBindings(ctx, baseURL, managementKey)
	if err != nil {
		return wxaiSwitcherExitIPSnapshot{}, err
	}
	snapshot := buildWxaiSwitcherExitIPSnapshot(baseURL, items)
	service.wxaiSwitcherSnapshotMu.Lock()
	service.wxaiSwitcherExitIPCache = snapshot
	service.wxaiSwitcherSnapshotMu.Unlock()
	return snapshot, nil
}

func (service *Service) fetchWxaiSwitcherExitIPBindings(
	ctx context.Context,
	baseURL string,
	managementKey string,
) ([]wxaiSwitcherExitIPBinding, error) {
	payload, err := json.Marshal(struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}{
		Method: http.MethodGet,
		Path:   wxaiSwitcherExitIPPath,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal xai switcher exit IP request: %w", err)
	}

	requestContext, cancel := context.WithTimeout(ctx, wxaiSwitcherRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		baseURL+wxaiSwitcherManagementAPIPath,
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("create xai switcher exit IP request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+managementKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CPA-XAI-IP-Switcher-UI", "1")

	response, err := service.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request xai switcher exit IP bindings: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, wxaiSwitcherMaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read xai switcher exit IP bindings: %w", err)
	}
	if int64(len(responseBody)) > wxaiSwitcherMaxResponseBytes {
		return nil, fmt.Errorf("xai switcher exit IP response exceeds %d bytes", wxaiSwitcherMaxResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("xai switcher exit IP bindings: HTTP %d %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var decoded wxaiSwitcherExitIPResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode xai switcher exit IP bindings: %w", err)
	}
	if decoded.Error != nil {
		return nil, fmt.Errorf("xai switcher exit IP bindings: %s: %s", decoded.Error.Code, decoded.Error.Message)
	}
	return decoded.Data.Items, nil
}

func buildWxaiSwitcherExitIPSnapshot(baseURL string, items []wxaiSwitcherExitIPBinding) wxaiSwitcherExitIPSnapshot {
	byAuthName := make(map[string]string, len(items))
	byAuthIndex := make(map[string]string, len(items))
	authIndexDuplicates := make(map[string]struct{})
	for _, item := range items {
		if item.SyncStatus != "verified" {
			continue
		}
		exitIP := strings.TrimSpace(item.ExitIP)
		if exitIP == "" {
			continue
		}
		if authName := normalizeWxaiSwitcherKey(item.AuthName); authName != "" {
			byAuthName[authName] = exitIP
		}
		authIndex := normalizeWxaiSwitcherKey(item.AuthIndex)
		if authIndex == "" {
			continue
		}
		if _, ambiguous := authIndexDuplicates[authIndex]; ambiguous {
			continue
		}
		if _, exists := byAuthIndex[authIndex]; exists {
			delete(byAuthIndex, authIndex)
			authIndexDuplicates[authIndex] = struct{}{}
			continue
		}
		byAuthIndex[authIndex] = exitIP
	}
	return wxaiSwitcherExitIPSnapshot{
		baseURL:     baseURL,
		expiresAt:   time.Now().Add(wxaiSwitcherSnapshotTTL),
		byAuthName:  byAuthName,
		byAuthIndex: byAuthIndex,
	}
}

func normalizeWxaiSwitcherKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
