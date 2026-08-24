package wxaiinspection

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	wxaiSSOAccountStateURL       = "https://grok.com/"
	wxaiSSOAccountStateTimeout   = 20 * time.Second
	wxaiSSOAccountStateBodyMax   = 4 * 1024 * 1024
	wxaiSSOAccountStateUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
)

var (
	wxaiSSOBotFlagSourcePattern  = regexp.MustCompile(`botFlagSource"\s*:\s*(null|-?\d+)`)
	wxaiSSOBotFlagDetailsPattern = regexp.MustCompile(`botFlagDetails"\s*:\s*(?:null|"([^"]*)")`)
)

type wxaiSSOBotFlagInspection struct {
	Found          bool
	Flagged        bool
	BotFlagSource  *int
	BotFlagDetails string
	Policy         string
	Risk           *float64
	Event          string
	Denied         bool
	StatusCode     int
	FinalURL       string
}

func inspectWxaiSSOBotFlags(ctx context.Context, ssoCookie string, authProxyURL string) (wxaiSSOBotFlagInspection, error) {
	trimmedSSOCookie := strings.TrimSpace(ssoCookie)
	if trimmedSSOCookie == "" {
		return wxaiSSOBotFlagInspection{}, fmt.Errorf("sso 为空")
	}

	httpClient, _, err := newWxaiAuthProxyHTTPClient(authProxyURL, wxaiSSOAccountStateTimeout)
	if err != nil {
		return wxaiSSOBotFlagInspection{}, err
	}
	cookieJar, err := cookiejar.New(nil)
	if err != nil {
		return wxaiSSOBotFlagInspection{}, fmt.Errorf("创建 SSO cookie jar: %w", err)
	}
	httpClient.Jar = cookieJar
	setWxaiSSOCookies(cookieJar, trimmedSSOCookie)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, wxaiSSOAccountStateURL, nil)
	if err != nil {
		return wxaiSSOBotFlagInspection{}, fmt.Errorf("创建 grok.com SSO 请求: %w", err)
	}
	request.Header.Set("User-Agent", wxaiSSOAccountStateUserAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml")

	response, err := httpClient.Do(request)
	if err != nil {
		return wxaiSSOBotFlagInspection{}, fmt.Errorf("请求 grok.com SSO 状态: %w", err)
	}
	defer response.Body.Close()

	inspection := wxaiSSOBotFlagInspection{
		StatusCode: response.StatusCode,
		FinalURL:   response.Request.URL.String(),
	}
	if response.StatusCode != http.StatusOK {
		return inspection, fmt.Errorf("grok.com HTTP %d", response.StatusCode)
	}

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, wxaiSSOAccountStateBodyMax+1))
	if err != nil {
		return inspection, fmt.Errorf("读取 grok.com SSO 状态响应: %w", err)
	}
	if len(responseBody) > wxaiSSOAccountStateBodyMax {
		return inspection, fmt.Errorf("grok.com SSO 状态响应超过 %d bytes", wxaiSSOAccountStateBodyMax)
	}
	inspection = parseWxaiSSOBotFlagState(string(responseBody))
	inspection.StatusCode = response.StatusCode
	inspection.FinalURL = response.Request.URL.String()
	if !inspection.Found {
		return inspection, fmt.Errorf("grok.com 未发现 botFlag 字段")
	}
	return inspection, nil
}

func setWxaiSSOCookies(cookieJar http.CookieJar, ssoCookie string) {
	for _, domain := range []string{"x.ai", "accounts.x.ai", "auth.x.ai", "grok.com"} {
		domainURL := &url.URL{Scheme: "https", Host: domain}
		cookieJar.SetCookies(domainURL, []*http.Cookie{
			{Name: "sso", Value: ssoCookie, Path: "/"},
			{Name: "sso-rw", Value: ssoCookie, Path: "/"},
		})
	}
}

func parseWxaiSSOBotFlagState(pageHTML string) wxaiSSOBotFlagInspection {
	normalizedHTML := strings.ReplaceAll(pageHTML, `\"`, `"`)
	sourceMatch := wxaiSSOBotFlagSourcePattern.FindStringSubmatch(normalizedHTML)
	detailsMatch := wxaiSSOBotFlagDetailsPattern.FindStringSubmatch(normalizedHTML)

	inspection := wxaiSSOBotFlagInspection{
		Found: sourceMatch != nil || detailsMatch != nil,
	}
	if sourceMatch != nil && sourceMatch[1] != "null" {
		if source, err := strconv.Atoi(sourceMatch[1]); err == nil {
			inspection.BotFlagSource = &source
		}
	}
	if detailsMatch != nil && len(detailsMatch) > 1 {
		inspection.BotFlagDetails = detailsMatch[1]
	}

	detailFields := parseWxaiSSOBotFlagDetails(inspection.BotFlagDetails)
	inspection.Policy = strings.ToLower(detailFields["policy"])
	inspection.Event = detailFields["event"]
	if rawRisk := detailFields["risk"]; rawRisk != "" {
		if risk, err := strconv.ParseFloat(rawRisk, 64); err == nil {
			inspection.Risk = &risk
		}
	}
	inspection.Denied = inspection.Policy == "deny" && inspection.Event == "$registration"
	inspection.Flagged = inspection.Denied ||
		(inspection.BotFlagSource != nil && (*inspection.BotFlagSource == 1 || *inspection.BotFlagSource == 2))
	return inspection
}

func parseWxaiSSOBotFlagDetails(details string) map[string]string {
	fields := make(map[string]string)
	for _, item := range strings.Split(details, ",") {
		key, value, found := strings.Cut(item, "=")
		if !found || strings.TrimSpace(key) == "" {
			continue
		}
		fields[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return fields
}

func formatWxaiSSOInspectionError(inspection wxaiSSOBotFlagInspection, inspectionError error) string {
	value := map[string]any{
		"error":      inspectionError.Error(),
		"finalURL":   inspection.FinalURL,
		"statusCode": inspection.StatusCode,
	}
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return inspectionError.Error()
	}
	return string(encodedValue)
}

func (inspection wxaiSSOBotFlagInspection) normalizedValue() string {
	value := map[string]any{
		"botFlagDetails": inspection.BotFlagDetails,
		"denied":         inspection.Denied,
		"event":          inspection.Event,
		"finalURL":       inspection.FinalURL,
		"policy":         inspection.Policy,
		"statusCode":     inspection.StatusCode,
	}
	if inspection.BotFlagSource != nil {
		value["botFlagSource"] = *inspection.BotFlagSource
	}
	if inspection.Risk != nil {
		value["risk"] = *inspection.Risk
	}
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("botFlagSource=%v; botFlagDetails=%s", inspection.BotFlagSource, inspection.BotFlagDetails)
	}
	return string(encodedValue)
}
