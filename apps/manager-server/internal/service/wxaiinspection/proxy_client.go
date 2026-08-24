package wxaiinspection

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/toolcallcheck"
)

var ErrWxaiAuthProxyURLMissing = errors.New("auth proxy_url 未配置")

func resolveWxaiAuthProxyURL(authFile map[string]any) string {
	return strings.TrimSpace(firstString(authFile, "proxy_url", "proxyUrl", "proxy-url"))
}

func resolveWxaiAuthHTTPClient(authFile map[string]any) (*http.Client, string, error) {
	return newWxaiAuthProxyHTTPClient(resolveWxaiAuthProxyURL(authFile), 60*time.Second)
}

func newWxaiAuthProxyHTTPClient(proxyURL string, timeout time.Duration) (*http.Client, string, error) {
	trimmedProxyURL := strings.TrimSpace(proxyURL)
	if trimmedProxyURL == "" {
		return nil, "", ErrWxaiAuthProxyURLMissing
	}
	client, proxyMode, err := toolcallcheck.NewHTTPClient(trimmedProxyURL, timeout)
	if err != nil {
		return nil, "", fmt.Errorf("create auth proxy_url HTTP client: %w", err)
	}
	if proxyMode != "proxy" {
		return nil, "", fmt.Errorf("auth proxy_url 必须配置代理地址")
	}
	return client, toolcallcheck.RedactProxyURL(trimmedProxyURL), nil
}

func isWxaiProxySetupError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrWxaiAuthProxyURLMissing) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "auth proxy_url") ||
		strings.Contains(message, "create auth proxy_url HTTP client") ||
		strings.Contains(message, "create auth proxy SSO client")
}

func buildWxaiAuthProxyClientLogDetail(redactedProxyURL string) map[string]any {
	return map[string]any{
		"proxyConfigured": true,
		"proxyMode":       "auth_proxy_url",
		"proxyURL":        redactedProxyURL,
	}
}
