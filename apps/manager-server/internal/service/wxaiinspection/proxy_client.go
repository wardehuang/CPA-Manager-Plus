package wxaiinspection

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type wxaiHTTPClientRuntime struct {
	client        *http.Client
	clientVersion string
}

// resolveWxaiHTTPClient 条件巡检、手动刷新和服务器巡检共用直连 client + CPA xai-client-version。
func (service *Service) resolveWxaiHTTPClient(ctx context.Context, setup store.Setup) (wxaiHTTPClientRuntime, error) {
	clientVersion, err := cpa.FetchXAIClientVersion(
		ctx,
		setup.CPAUpstreamURL,
		setup.ManagementKey,
	)
	if err != nil {
		return wxaiHTTPClientRuntime{}, fmt.Errorf("读取 CPA xAI client version: %w", err)
	}
	return wxaiHTTPClientRuntime{
		client:        newWxaiDirectHTTPClient(),
		clientVersion: clientVersion,
	}, nil
}

func newWxaiDirectHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// 显式关闭代理：忽略 HTTP(S)_PROXY 环境变量。
	transport.Proxy = nil
	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: transport,
	}
}

func buildWxaiDirectClientLogDetail(accountCount int) map[string]any {
	return map[string]any{
		"proxyConfigured": false,
		"proxyMode":       "direct",
		"accountCount":    accountCount,
	}
}
