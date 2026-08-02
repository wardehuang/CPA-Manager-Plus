package toolcallcheck

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func parseProxyURL(rawProxyURL string) (*url.URL, error) {
	trimmedProxyURL := strings.TrimSpace(rawProxyURL)
	if trimmedProxyURL == "" {
		return nil, errors.New("proxy URL is empty")
	}

	normalizedProxyURL := trimmedProxyURL
	if !strings.Contains(trimmedProxyURL, "://") {
		if legacyProxyURL, recognized, normalizeError := normalizeLegacySOCKS5Proxy(trimmedProxyURL, "socks5"); recognized {
			if normalizeError != nil {
				return nil, normalizeError
			}
			normalizedProxyURL = legacyProxyURL
		}
	}

	parsedProxyURL, parseError := url.Parse(normalizedProxyURL)
	if parseError != nil {
		return nil, fmt.Errorf("parse proxy URL: %w", parseError)
	}

	if isSOCKS5Scheme(parsedProxyURL.Scheme) && parsedProxyURL.User == nil {
		if legacyProxyURL, recognized, normalizeError := normalizeLegacySOCKS5Proxy(parsedProxyURL.Host, parsedProxyURL.Scheme); recognized {
			if normalizeError != nil {
				return nil, normalizeError
			}
			parsedProxyURL, parseError = url.Parse(legacyProxyURL)
			if parseError != nil {
				return nil, fmt.Errorf("parse normalized proxy URL: %w", parseError)
			}
		}
	}

	if parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
		return nil, errors.New("proxy URL must contain scheme and host")
	}
	return parsedProxyURL, nil
}

func normalizeLegacySOCKS5Proxy(rawProxyValue string, scheme string) (string, bool, error) {
	if _, _, splitHostPortError := net.SplitHostPort(strings.TrimSpace(rawProxyValue)); splitHostPortError == nil {
		return "", false, nil
	}
	proxyParts := strings.Split(rawProxyValue, ":")
	if len(proxyParts) != 4 {
		return "", false, nil
	}

	proxyUsername := proxyParts[0]
	proxyPassword := proxyParts[1]
	proxyHost := strings.TrimSpace(proxyParts[2])
	proxyPort := strings.TrimSpace(proxyParts[3])
	if strings.TrimSpace(proxyUsername) == "" || proxyHost == "" || proxyPort == "" {
		return "", true, errors.New("legacy SOCKS5 proxy must contain username, host, and port")
	}
	if strings.Contains(proxyHost, ":") {
		return "", true, errors.New("legacy SOCKS5 proxy host must not contain a colon")
	}
	if _, parseError := strconv.ParseUint(proxyPort, 10, 16); parseError != nil {
		return "", true, fmt.Errorf("legacy SOCKS5 proxy port is invalid: %w", parseError)
	}

	if scheme == "" {
		scheme = "socks5"
	}
	if !isSOCKS5Scheme(scheme) {
		return "", false, nil
	}

	normalizedProxyURL := &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(proxyHost, proxyPort),
		User:   url.UserPassword(proxyUsername, proxyPassword),
	}
	return normalizedProxyURL.String(), true, nil
}

func isSOCKS5Scheme(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "socks5", "socks5h":
		return true
	default:
		return false
	}
}
