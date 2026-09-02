package toolcallcheck

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	DefaultTimeout      = 60 * time.Second
	MaxResponseBodySize = 4 * 1024 * 1024
)

type ProxySource string

const (
	ProxySourceAuth   ProxySource = "auth"
	ProxySourceGlobal ProxySource = "global"
	ProxySourceDirect ProxySource = "direct"
)

type ProxySelection struct {
	URL    string
	Source ProxySource
}

type Request struct {
	CheckID       string
	Model         string
	Endpoint      string
	AccessToken   string
	Headers       map[string]string
	Body          any
	Proxy         ProxySelection
	Timeout       time.Duration
	Stream        bool
	QualityPolicy QualityPolicy
	CleanupPath   string
}

type Result struct {
	CheckID                     string              `json:"checkId"`
	Model                       string              `json:"model"`
	Endpoint                    string              `json:"endpoint"`
	ProxySource                 ProxySource         `json:"proxySource"`
	ProxyMode                   string              `json:"proxyMode"`
	ProxyURL                    string              `json:"proxyUrl,omitempty"`
	Stream                      bool                `json:"stream"`
	StartedAtMS                 int64               `json:"startedAtMs"`
	FinishedAtMS                int64               `json:"finishedAtMs"`
	DurationMS                  int64               `json:"durationMs"`
	TotalMS                     int64               `json:"totalMs"`
	TTFBMS                      int64               `json:"ttfbMs,omitempty"`
	FirstTokenMS                int64               `json:"firstTokenMs,omitempty"`
	GenerationMS                int64               `json:"generationMs,omitempty"`
	StatusCode                  int                 `json:"statusCode,omitempty"`
	ErrorCode                   string              `json:"errorCode,omitempty"`
	Classification              string              `json:"classification,omitempty"`
	QualityLevel                string              `json:"qualityLevel,omitempty"`
	ClassificationReason        string              `json:"classificationReason,omitempty"`
	OutputTokens                *int                `json:"outputTokens,omitempty"`
	ReasoningTokens             *int                `json:"reasoningTokens,omitempty"`
	ThinkingDelta               bool                `json:"thinkingDelta"`
	VisibleTokens               *int                `json:"visibleTokens,omitempty"`
	SummaryChars                int                 `json:"summaryChars"`
	EncryptedBytes              int                 `json:"encryptedBytes"`
	EncryptedFloor              int                 `json:"encryptedFloor"`
	IsRealThinking              bool                `json:"isRealThinking"`
	RealThinkingReason          string              `json:"realThinkingReason"`
	VisibleFlushMS              int64               `json:"visibleFlushMs"`
	EvaluatedTokens             int                 `json:"evaluatedTokens"`
	QualityPolicy               QualityPolicy       `json:"qualityPolicy"`
	ExpectedAnswer              string              `json:"expectedAnswer,omitempty"`
	AnswerMatched               bool                `json:"answerMatched"`
	OutputTokensPerSecond       *float64            `json:"outputTokensPerSecond,omitempty"`
	ModelAnswer                 string              `json:"modelAnswer,omitempty"`
	RequestBody                 json.RawMessage     `json:"requestBody,omitempty"`
	RequestHeaders              map[string]string   `json:"requestHeaders,omitempty"`
	ResponseHeaders             map[string][]string `json:"responseHeaders,omitempty"`
	ResponseBody                string              `json:"responseBody,omitempty"`
	ResponseBodyTruncated       bool                `json:"responseBodyTruncated"`
	ToolCallDetected            bool                `json:"toolCallDetected"`
	ToolCallNames               []string            `json:"toolCallNames,omitempty"`
	CompletedFunctionCallCount  int                 `json:"completedFunctionCallCount"`
	ToolCallOnly                bool                `json:"toolCallOnly"`
	OutputTextChars             int                 `json:"outputTextChars"`
	CompletedMessageCount       int                 `json:"completedMessageCount"`
	RefusalDetected             bool                `json:"refusalDetected"`
	SubstantiveVisibleResponse  bool                `json:"substantiveVisibleResponse"`
	ValidResponseEvidence       bool                `json:"validResponseEvidence"`
	ValidResponseEvidenceReason string              `json:"validResponseEvidenceReason"`
	Error                       string              `json:"error,omitempty"`
	CleanupPath                 string              `json:"cleanupPath,omitempty"`
	CleanupAttempted            bool                `json:"cleanupAttempted"`
	CleanupDeleted              bool                `json:"cleanupDeleted"`
	CleanupError                string              `json:"cleanupError,omitempty"`
}

func NewExecutionID() (string, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", fmt.Errorf("generate execution id: %w", err)
	}
	randomBytes[6] = (randomBytes[6] & 0x0f) | 0x40
	randomBytes[8] = (randomBytes[8] & 0x3f) | 0x80
	encodedID := hex.EncodeToString(randomBytes[:])
	return strings.Join([]string{
		encodedID[0:8],
		encodedID[8:12],
		encodedID[12:16],
		encodedID[16:20],
		encodedID[20:32],
	}, "-"), nil
}

func ResolveProxy(authProxyURL string, globalProxyURL string) ProxySelection {
	if trimmedAuthProxyURL := strings.TrimSpace(authProxyURL); trimmedAuthProxyURL != "" {
		return ProxySelection{URL: trimmedAuthProxyURL, Source: ProxySourceAuth}
	}
	if trimmedGlobalProxyURL := strings.TrimSpace(globalProxyURL); trimmedGlobalProxyURL != "" {
		return ProxySelection{URL: trimmedGlobalProxyURL, Source: ProxySourceGlobal}
	}
	return ProxySelection{Source: ProxySourceDirect}
}

func Run(ctx context.Context, request Request) (result Result, err error) {
	startedAt := time.Now()
	result = Result{
		CheckID:       strings.TrimSpace(request.CheckID),
		Model:         strings.TrimSpace(request.Model),
		Endpoint:      strings.TrimSpace(request.Endpoint),
		ProxySource:   request.Proxy.Source,
		ProxyURL:      RedactProxyURL(request.Proxy.URL),
		Stream:        request.Stream,
		QualityPolicy: request.QualityPolicy,
		StartedAtMS:   startedAt.UnixMilli(),
		CleanupPath:   strings.TrimSpace(request.CleanupPath),
	}
	if result.CheckID == "" {
		result.CheckID, err = NewExecutionID()
		if err != nil {
			return result, err
		}
	}
	defer func() {
		result.FinishedAtMS = time.Now().UnixMilli()
		result.DurationMS = time.Since(startedAt).Milliseconds()
		result.TotalMS = result.DurationMS
		if result.CleanupPath != "" {
			result.CleanupAttempted = true
			cleanupError := cleanupPath(result.CleanupPath)
			if cleanupError == nil {
				result.CleanupDeleted = true
				return
			}
			if errors.Is(cleanupError, os.ErrNotExist) {
				return
			}
			result.CleanupError = cleanupError.Error()
		}
	}()

	if result.Endpoint == "" {
		return result, errors.New("tool call check endpoint is required")
	}

	requestBody, marshalError := json.Marshal(request.Body)
	if marshalError != nil {
		return result, fmt.Errorf("marshal tool call check request: %w", marshalError)
	}
	result.RequestBody = bytes.Clone(requestBody)

	httpClient, proxyMode, clientError := NewHTTPClient(request.Proxy.URL, request.Timeout)
	if clientError != nil {
		return result, clientError
	}
	result.ProxyMode = proxyMode

	httpRequest, requestError := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		result.Endpoint,
		bytes.NewReader(requestBody),
	)
	if requestError != nil {
		return result, requestError
	}
	for headerName, headerValue := range request.Headers {
		httpRequest.Header.Set(headerName, headerValue)
	}
	if strings.TrimSpace(request.AccessToken) != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+strings.TrimSpace(request.AccessToken))
	}
	if httpRequest.Header.Get("Content-Type") == "" {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if request.Stream {
		httpRequest.Header.Set("Accept", "text/event-stream")
	} else if httpRequest.Header.Get("Accept") == "" {
		httpRequest.Header.Set("Accept", "application/json")
	}
	result.RequestHeaders = sanitizeRequestHeaders(httpRequest.Header)

	if request.Stream {
		runStreamingResponse(httpClient, httpRequest, &result, startedAt, request.QualityPolicy)
		return result, nil
	}

	response, doError := httpClient.Do(httpRequest)
	if doError != nil {
		result.Error = doError.Error()
		return result, nil
	}
	defer response.Body.Close()

	result.StatusCode = response.StatusCode
	result.ResponseHeaders = sanitizeResponseHeaders(response.Header)
	responseBody, readError := io.ReadAll(io.LimitReader(response.Body, MaxResponseBodySize+1))
	if readError != nil {
		result.Error = readError.Error()
		return result, nil
	}
	if len(responseBody) > MaxResponseBodySize {
		result.ResponseBodyTruncated = true
		responseBody = responseBody[:MaxResponseBodySize]
	}
	result.ResponseBody = string(responseBody)
	result.ToolCallDetected, result.ToolCallNames = detectToolCalls(responseBody)
	return result, nil
}

func NewHTTPClient(rawProxyURL string, timeout time.Duration) (*http.Client, string, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	trimmedProxyURL := strings.TrimSpace(rawProxyURL)
	if trimmedProxyURL == "" || strings.EqualFold(trimmedProxyURL, "direct") || strings.EqualFold(trimmedProxyURL, "none") {
		return &http.Client{
			Timeout:   timeout,
			Transport: newDirectTransport(),
		}, "direct", nil
	}

	parsedProxyURL, parseError := parseProxyURL(trimmedProxyURL)
	if parseError != nil {
		return nil, "", fmt.Errorf("parse proxy URL: %w", parseError)
	}

	switch strings.ToLower(parsedProxyURL.Scheme) {
	case "http", "https":
		transport := newDirectTransport()
		transport.Proxy = http.ProxyURL(parsedProxyURL)
		return &http.Client{Timeout: timeout, Transport: transport}, "proxy", nil
	case "socks5", "socks5h":
		var proxyAuthentication *proxy.Auth
		if parsedProxyURL.User != nil {
			password, _ := parsedProxyURL.User.Password()
			proxyAuthentication = &proxy.Auth{
				User:     parsedProxyURL.User.Username(),
				Password: password,
			}
		}
		socksDialer, dialerError := proxy.SOCKS5("tcp", parsedProxyURL.Host, proxyAuthentication, proxy.Direct)
		if dialerError != nil {
			return nil, "", fmt.Errorf("create SOCKS5 proxy dialer: %w", dialerError)
		}
		contextDialer, supportsContext := socksDialer.(proxy.ContextDialer)
		if !supportsContext {
			return nil, "", errors.New("SOCKS5 proxy dialer does not support context cancellation")
		}
		transport := newDirectTransport()
		transport.DialContext = contextDialer.DialContext
		return &http.Client{Timeout: timeout, Transport: transport}, "proxy", nil
	default:
		return nil, "", fmt.Errorf("unsupported proxy scheme: %s", parsedProxyURL.Scheme)
	}
}

func newDirectTransport() *http.Transport {
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok && defaultTransport != nil {
		transport := defaultTransport.Clone()
		transport.Proxy = nil
		return transport
	}
	return &http.Transport{}
}

func RedactProxyURL(rawProxyURL string) string {
	trimmedProxyURL := strings.TrimSpace(rawProxyURL)
	if trimmedProxyURL == "" {
		return ""
	}
	if strings.EqualFold(trimmedProxyURL, "direct") || strings.EqualFold(trimmedProxyURL, "none") {
		return strings.ToLower(trimmedProxyURL)
	}
	parsedProxyURL, parseError := parseProxyURL(trimmedProxyURL)
	if parseError != nil {
		return "<invalid proxy URL>"
	}
	redactedProxyURL := &url.URL{
		Scheme: parsedProxyURL.Scheme,
		Host:   parsedProxyURL.Host,
	}
	if parsedProxyURL.User != nil {
		redactedProxyURL.User = url.User("redacted")
	}
	return redactedProxyURL.String()
}

func sanitizeRequestHeaders(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for headerName, headerValues := range headers {
		if isSensitiveHeader(headerName) {
			result[headerName] = "[REDACTED]"
			continue
		}
		result[headerName] = strings.Join(headerValues, ", ")
	}
	return result
}

func sanitizeResponseHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for headerName, headerValues := range headers {
		if isSensitiveHeader(headerName) {
			result[headerName] = []string{"[REDACTED]"}
			continue
		}
		result[headerName] = append([]string(nil), headerValues...)
	}
	return result
}

func isSensitiveHeader(headerName string) bool {
	switch strings.ToLower(strings.TrimSpace(headerName)) {
	case "authorization", "proxy-authorization", "set-cookie", "set-cookie2":
		return true
	default:
		return false
	}
}

func cleanupPath(path string) error {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return err
	}
	return nil
}

func detectToolCalls(responseBody []byte) (bool, []string) {
	var payload any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return false, nil
	}
	seenNames := make(map[string]struct{})
	detected := false
	var visit func(any)
	visit = func(value any) {
		switch typedValue := value.(type) {
		case []any:
			for _, item := range typedValue {
				visit(item)
			}
		case map[string]any:
			typeName, _ := typedValue["type"].(string)
			normalizedType := strings.ToLower(strings.TrimSpace(typeName))
			if normalizedType == "function_call" || normalizedType == "tool_call" || normalizedType == "custom_tool_call" {
				detected = true
				if name, ok := typedValue["name"].(string); ok && strings.TrimSpace(name) != "" {
					seenNames[strings.TrimSpace(name)] = struct{}{}
				}
			}
			for key, nestedValue := range typedValue {
				if strings.EqualFold(key, "tool_calls") || strings.EqualFold(key, "function_call") {
					detected = true
				}
				visit(nestedValue)
			}
		}
	}
	visit(payload)
	toolCallNames := make([]string, 0, len(seenNames))
	for name := range seenNames {
		toolCallNames = append(toolCallNames, name)
	}
	return detected, toolCallNames
}
