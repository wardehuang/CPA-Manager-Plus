package wxaiinspection

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type wxaiHTTPClientRuntime struct {
	client        *http.Client
	clientVersion string
	proxySummary  wxaiProxySummary
}

func (service *Service) resolveWxaiHTTPClient(ctx context.Context, setup store.Setup) (wxaiHTTPClientRuntime, error) {
	managementConfig, err := cpa.FetchManagementConfig(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
	)
	if err != nil {
		return wxaiHTTPClientRuntime{}, fmt.Errorf("读取 CPA proxy-url: %w", err)
	}
	clientVersion, err := cpa.FetchXAIClientVersion(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
	)
	if err != nil {
		return wxaiHTTPClientRuntime{}, fmt.Errorf("读取 CPA xAI client version: %w", err)
	}

	proxyURL := strings.TrimSpace(managementConfig.ProxyURL)
	transport, proxySummary, err := buildWxaiProxyTransport(proxyURL)
	if err != nil {
		return wxaiHTTPClientRuntime{}, fmt.Errorf("CPA proxy-url 无效: %w", err)
	}
	return wxaiHTTPClientRuntime{
		client: &http.Client{
			Timeout:   60 * time.Second,
			Transport: transport,
		},
		clientVersion: clientVersion,
		proxySummary:  proxySummary,
	}, nil
}

func buildWxaiProxyLogDetail(proxySummary wxaiProxySummary, accountCount int) map[string]any {
	detail := map[string]any{
		"proxyConfigured": proxySummary.configured,
		"proxyMode":       proxySummary.mode,
		"accountCount":    accountCount,
	}
	if proxySummary.configured {
		detail["proxyHost"] = proxySummary.host
	}
	return detail
}
