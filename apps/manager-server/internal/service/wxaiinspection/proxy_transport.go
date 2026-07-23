package wxaiinspection

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/proxy"
)

type wxaiProxySummary struct {
	configured bool
	mode       string
	host       string
}

func buildWxaiProxyTransport(rawProxyURL string) (*http.Transport, wxaiProxySummary, error) {
	trimmedProxyURL := strings.TrimSpace(rawProxyURL)
	if trimmedProxyURL == "" || strings.EqualFold(trimmedProxyURL, "direct") || strings.EqualFold(trimmedProxyURL, "none") {
		return newWxaiDirectTransport(), wxaiProxySummary{mode: "direct"}, nil
	}

	if legacySetting, matched := parseLegacyWxaiSOCKS5Proxy(trimmedProxyURL); matched {
		transport, err := buildWxaiSOCKS5Transport(legacySetting.address, legacySetting.authentication)
		if err != nil {
			return nil, wxaiProxySummary{}, err
		}
		return transport, wxaiProxySummary{
			configured: true,
			mode:       strings.ToLower(trimmedProxyURL[:strings.Index(trimmedProxyURL, "://")]),
			host:       legacySetting.address,
		}, nil
	}

	parsedProxyURL, err := url.Parse(trimmedProxyURL)
	if err != nil || parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
		return nil, wxaiProxySummary{}, fmt.Errorf("proxy URL 缺少合法 scheme 或 host")
	}

	proxyMode := strings.ToLower(parsedProxyURL.Scheme)
	proxySummary := wxaiProxySummary{configured: true, mode: proxyMode, host: parsedProxyURL.Host}
	switch proxyMode {
	case "socks5", "socks5h":
		proxyAuthentication := wxaiSOCKS5Authentication(parsedProxyURL.User)
		transport, buildErr := buildWxaiSOCKS5Transport(parsedProxyURL.Host, proxyAuthentication)
		if buildErr != nil {
			return nil, wxaiProxySummary{}, buildErr
		}
		return transport, proxySummary, nil
	case "http", "https":
		transport := newWxaiDirectTransport()
		transport.Proxy = http.ProxyURL(parsedProxyURL)
		return transport, proxySummary, nil
	default:
		return nil, wxaiProxySummary{}, fmt.Errorf("不支持代理协议 %q", parsedProxyURL.Scheme)
	}
}

type wxaiLegacySOCKS5Setting struct {
	address        string
	authentication *proxy.Auth
}

func parseLegacyWxaiSOCKS5Proxy(rawProxyURL string) (wxaiLegacySOCKS5Setting, bool) {
	schemeSeparatorIndex := strings.Index(rawProxyURL, "://")
	if schemeSeparatorIndex <= 0 {
		return wxaiLegacySOCKS5Setting{}, false
	}
	scheme := strings.ToLower(rawProxyURL[:schemeSeparatorIndex])
	if scheme != "socks5" && scheme != "socks5h" {
		return wxaiLegacySOCKS5Setting{}, false
	}
	authority := rawProxyURL[schemeSeparatorIndex+3:]
	if strings.Contains(authority, "@") || strings.ContainsAny(authority, "/?#") {
		return wxaiLegacySOCKS5Setting{}, false
	}
	segments := strings.Split(authority, ":")
	if len(segments) != 4 {
		return wxaiLegacySOCKS5Setting{}, false
	}
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return wxaiLegacySOCKS5Setting{}, false
		}
	}
	return wxaiLegacySOCKS5Setting{
		address: net.JoinHostPort(segments[2], segments[3]),
		authentication: &proxy.Auth{
			User:     segments[0],
			Password: segments[1],
		},
	}, true
}

func wxaiSOCKS5Authentication(userInformation *url.Userinfo) *proxy.Auth {
	if userInformation == nil {
		return nil
	}
	password, _ := userInformation.Password()
	return &proxy.Auth{
		User:     userInformation.Username(),
		Password: password,
	}
}

func buildWxaiSOCKS5Transport(address string, authentication *proxy.Auth) (*http.Transport, error) {
	dialer, err := proxy.SOCKS5("tcp", address, authentication, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("创建 SOCKS5 dialer: %w", err)
	}

	transport := newWxaiDirectTransport()
	if contextDialer, supportsContext := dialer.(proxy.ContextDialer); supportsContext {
		transport.DialContext = contextDialer.DialContext
		return transport, nil
	}
	transport.DialContext = func(_ context.Context, network string, destination string) (net.Conn, error) {
		return dialer.Dial(network, destination)
	}
	return transport, nil
}

func newWxaiDirectTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return transport
}
