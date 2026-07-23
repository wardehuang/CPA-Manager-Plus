package cpaauthfiles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
)

const (
	authFileDownloadPath     = "/v0/management/auth-files/download"
	maxAuthFileDownloadBytes = 4 * 1024 * 1024
)

func (client *Client) DownloadJSON(
	ctx context.Context,
	baseURL string,
	managementKey string,
	fileName string,
) (map[string]any, error) {
	trimmedFileName := strings.TrimSpace(fileName)
	if trimmedFileName == "" {
		return nil, errors.New("CPA auth file name is required")
	}

	endpoint := cpa.NormalizeBaseURL(baseURL) + authFileDownloadPath + "?name=" + url.QueryEscape(trimmedFileName)
	requestContext, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", authFileDownloadPath, err)
	}
	request.Header.Set("Authorization", "Bearer "+managementKey)

	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", authFileDownloadPath, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf(
			"GET %s: HTTP %d %s",
			authFileDownloadPath,
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	limitedBody := &io.LimitedReader{R: response.Body, N: maxAuthFileDownloadBytes + 1}
	decoder := json.NewDecoder(limitedBody)
	decoder.UseNumber()
	var authFile map[string]any
	if err := decoder.Decode(&authFile); err != nil {
		return nil, fmt.Errorf("decode CPA auth file %q: %w", trimmedFileName, err)
	}
	if limitedBody.N == 0 {
		return nil, responseTooLargeError("auth file download", maxAuthFileDownloadBytes)
	}
	return authFile, nil
}
