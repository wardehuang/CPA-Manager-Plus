package toolcallcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ClassificationNormal            = "normal"
	ClassificationSuspectedDegraded = "suspected_degradation"
	ClassificationQuotaExhausted    = "quota_exhausted"
	ClassificationUnknown           = "unknown"

	QualityLevelHealthy        = "healthy"
	QualityLevelSoft           = "soft"
	QualityLevelHard           = "hard"
	QualityLevelQuotaExhausted = "quota_exhausted"
	QualityLevelUnknown        = "unknown"

	ExpectedAnswer              = "391"
	qualityProbeStreamReadBytes = 32 << 10
	qualityExpectedAnswer       = ExpectedAnswer
	qualitySoftTokensPerSecond  = 500.0
	qualityHardTokensPerSecond  = 1000.0
)

type streamingMetrics struct {
	modelAnswer         strings.Builder
	outputTokens        *int
	reasoningTokens     *int
	thinkingDelta       bool
	errorCode           string
	errorMessage        string
	firstGeneratedReady bool
	firstGeneratedAt    time.Time
	visibleCharacters   int
}

type streamingResponsesUsage struct {
	OutputTokens        int `json:"output_tokens"`
	OutputTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type streamingResponsesEvent struct {
	Type     string          `json:"type"`
	Delta    json.RawMessage `json:"delta"`
	Error    json.RawMessage `json:"error"`
	Code     string          `json:"code"`
	Message  string          `json:"message"`
	Response struct {
		Usage             *streamingResponsesUsage `json:"usage"`
		Error             json.RawMessage          `json:"error"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	} `json:"response"`
}

type streamingSSEInspector struct {
	pendingSSEData   []byte
	metrics          *streamingMetrics
	streamTerminated bool
}

type firstByteReader struct {
	source      io.Reader
	startedAt   time.Time
	ttfbMS      *int64
	wasMeasured *bool
}

func (reader *firstByteReader) Read(buffer []byte) (int, error) {
	readCount, readError := reader.source.Read(buffer)
	if readCount > 0 && !*reader.wasMeasured {
		*reader.ttfbMS = time.Since(reader.startedAt).Milliseconds()
		*reader.wasMeasured = true
	}
	return readCount, readError
}

func runStreamingResponse(
	httpClient *http.Client,
	httpRequest *http.Request,
	result *Result,
	startedAt time.Time,
) {
	result.ExpectedAnswer = qualityExpectedAnswer
	response, requestError := httpClient.Do(httpRequest)
	if requestError != nil {
		result.Error = requestError.Error()
		result.Classification = ClassificationUnknown
		result.QualityLevel = QualityLevelUnknown
		result.ClassificationReason = "request_error"
		return
	}
	defer response.Body.Close()

	result.StatusCode = response.StatusCode
	result.ResponseHeaders = sanitizeResponseHeaders(response.Header)

	var responseBody bytes.Buffer
	var ttfbMS int64
	wasTTFBMeasured := false
	streamReader := &firstByteReader{
		source:      response.Body,
		startedAt:   startedAt,
		ttfbMS:      &ttfbMS,
		wasMeasured: &wasTTFBMeasured,
	}
	metrics := streamingMetrics{}
	streamInspector := streamingSSEInspector{
		pendingSSEData: make([]byte, 0, qualityProbeStreamReadBytes),
		metrics:        &metrics,
	}
	streamBuffer := make([]byte, qualityProbeStreamReadBytes)
	streamBytes := 0

	for {
		readCount, readError := streamReader.Read(streamBuffer)
		if readCount > 0 {
			streamBytes += readCount
			if streamBytes > MaxResponseBodySize {
				metrics.errorCode = "quality_probe_response_too_large"
				result.Error = "quality probe response exceeds 4 MiB"
				break
			}
			streamChunk := streamBuffer[:readCount]
			streamInspector.Inspect(streamChunk)
			appendResponseBody(&responseBody, string(streamChunk), &result.ResponseBodyTruncated)
			streamInspector.MarkFirstGeneratedTokenConsumed()
			if streamInspector.streamTerminated {
				break
			}
		}
		if readError != nil {
			if errors.Is(readError, io.EOF) {
				streamInspector.Finish()
				streamInspector.MarkFirstGeneratedTokenConsumed()
				break
			}
			result.Error = readError.Error()
			break
		}
	}
	if metrics.errorMessage != "" && result.Error == "" {
		result.Error = metrics.errorMessage
	}
	if !streamInspector.streamTerminated && result.Error == "" && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		metrics.errorCode = "quality_probe_stream_incomplete"
		result.Error = "quality probe stream did not end with response.completed"
	}

	result.TTFBMS = ttfbMS
	result.ResponseBody = responseBody.String()
	result.ModelAnswer = metrics.modelAnswer.String()
	result.ThinkingDelta = metrics.thinkingDelta
	result.OutputTokens = metrics.outputTokens
	result.ReasoningTokens = metrics.reasoningTokens
	visibleTokens := calculateVisibleTokens(metrics.outputTokens, metrics.reasoningTokens, metrics.visibleCharacters)
	result.VisibleTokens = intPointer(visibleTokens)
	result.ErrorCode = metrics.errorCode

	currentTotalMS := time.Since(startedAt).Milliseconds()
	result.TotalMS = currentTotalMS
	result.DurationMS = currentTotalMS
	if !metrics.firstGeneratedAt.IsZero() {
		result.FirstTokenMS = metrics.firstGeneratedAt.Sub(startedAt).Milliseconds()
		result.GenerationMS = currentTotalMS - result.FirstTokenMS
		if result.GenerationMS < 1 {
			result.GenerationMS = 1
		}
	}

	outputTokensPerSecond := 0.0
	if result.GenerationMS > 0 && metrics.outputTokens != nil && *metrics.outputTokens > 0 {
		outputTokensPerSecond = float64(*metrics.outputTokens) * 1000 / float64(result.GenerationMS)
	}
	result.OutputTokensPerSecond = &outputTokensPerSecond
	answerMatched := strings.Contains(result.ModelAnswer, qualityExpectedAnswer)
	result.AnswerMatched = answerMatched
	result.Classification, result.QualityLevel, result.ClassificationReason = classifyStreamingResult(
		result.StatusCode,
		result.Error,
		result.ErrorCode,
		metrics.errorMessage,
		result.ResponseBody,
		outputTokensPerSecond,
	)
}

func appendResponseBody(responseBody *bytes.Buffer, content string, truncated *bool) {
	remainingBytes := MaxResponseBodySize - responseBody.Len()
	if remainingBytes <= 0 {
		*truncated = true
		return
	}
	if len(content) > remainingBytes {
		_, _ = responseBody.WriteString(content[:remainingBytes])
		*truncated = true
		return
	}
	_, _ = responseBody.WriteString(content)
}

func (inspector *streamingSSEInspector) Inspect(chunk []byte) {
	inspector.pendingSSEData = append(inspector.pendingSSEData, chunk...)
	for {
		lineEndIndex := bytes.IndexByte(inspector.pendingSSEData, '\n')
		if lineEndIndex < 0 {
			return
		}
		line := string(bytes.TrimSpace(inspector.pendingSSEData[:lineEndIndex]))
		inspector.pendingSSEData = inspector.pendingSSEData[lineEndIndex+1:]
		if processSSELine(line, inspector.metrics) {
			inspector.streamTerminated = true
		}
	}
}

func (inspector *streamingSSEInspector) Finish() {
	if len(inspector.pendingSSEData) == 0 {
		return
	}
	inspector.pendingSSEData = append(inspector.pendingSSEData, '\n')
	inspector.Inspect(nil)
}

func (inspector *streamingSSEInspector) MarkFirstGeneratedTokenConsumed() {
	metrics := inspector.metrics
	if !metrics.firstGeneratedReady || !metrics.firstGeneratedAt.IsZero() {
		return
	}
	metrics.firstGeneratedReady = false
	metrics.firstGeneratedAt = time.Now()
}

func processSSELine(line string, metrics *streamingMetrics) bool {
	normalizedLine := strings.TrimSuffix(line, "\n")
	normalizedLine = strings.TrimSuffix(normalizedLine, "\r")
	if normalizedLine == "" || strings.HasPrefix(normalizedLine, ":") || !strings.HasPrefix(normalizedLine, "data:") {
		return false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(normalizedLine, "data:"))
	if payload == "[DONE]" {
		return true
	}
	if payload == "" {
		return false
	}
	return processStreamingEvent(payload, metrics)
}

func processStreamingEvent(rawData string, metrics *streamingMetrics) bool {
	trimmedData := strings.TrimSpace(rawData)
	if trimmedData == "" || trimmedData == "[DONE]" {
		return false
	}

	var event streamingResponsesEvent
	if err := json.Unmarshal([]byte(trimmedData), &event); err != nil {
		return false
	}
	captureStreamingEventError(event, metrics)
	if event.Response.Usage != nil {
		metrics.outputTokens = intPointer(event.Response.Usage.OutputTokens)
		metrics.reasoningTokens = intPointer(event.Response.Usage.OutputTokensDetails.ReasoningTokens)
	}
	if isThinkingDeltaEvent(event) {
		metrics.thinkingDelta = true
	}
	deltaText := streamingEventDeltaText(event)
	if metrics.firstGeneratedAt.IsZero() && !metrics.firstGeneratedReady && containsGeneratedResponsesDelta(event, deltaText) {
		metrics.firstGeneratedReady = true
	}
	if event.Type == "response.output_text.delta" && deltaText != "" {
		metrics.modelAnswer.WriteString(deltaText)
		metrics.visibleCharacters += utf8.RuneCountInString(deltaText)
	}

	switch event.Type {
	case "response.completed":
		return true
	case "response.incomplete", "response.failed", "error":
		captureStreamingTerminalFailure(event, metrics)
		return true
	default:
		return false
	}
}

func streamingEventDeltaText(event streamingResponsesEvent) string {
	if len(event.Delta) == 0 || string(event.Delta) == "null" {
		return ""
	}

	var textDelta string
	if err := json.Unmarshal(event.Delta, &textDelta); err == nil {
		return textDelta
	}

	var structuredDelta struct {
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(event.Delta, &structuredDelta); err != nil {
		return ""
	}
	if structuredDelta.Text != "" {
		return structuredDelta.Text
	}
	if structuredDelta.Thinking != "" {
		return structuredDelta.Thinking
	}
	return structuredDelta.Content
}

func isThinkingDeltaEvent(event streamingResponsesEvent) bool {
	normalizedType := strings.ToLower(strings.TrimSpace(event.Type))
	if normalizedType == "response.reasoning_summary_text.delta" ||
		normalizedType == "response.reasoning_text.delta" ||
		strings.Contains(normalizedType, "thinking_delta") {
		return true
	}

	var structuredDelta struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(event.Delta, &structuredDelta); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(structuredDelta.Type), "thinking_delta")
}

func containsGeneratedResponsesDelta(event streamingResponsesEvent, deltaText string) bool {
	if deltaText == "" {
		return false
	}
	switch event.Type {
	case "response.output_text.delta",
		"response.reasoning_summary_text.delta",
		"response.reasoning_text.delta",
		"response.refusal.delta",
		"response.function_call_arguments.delta",
		"response.custom_tool_call_input.delta":
		return true
	default:
		return false
	}
}

func captureStreamingEventError(event streamingResponsesEvent, metrics *streamingMetrics) {
	if len(event.Error) > 0 {
		var errorValue any
		if err := json.Unmarshal(event.Error, &errorValue); err == nil {
			captureStreamingError(errorValue, metrics)
		}
	}
	if len(event.Response.Error) > 0 {
		var errorValue any
		if err := json.Unmarshal(event.Response.Error, &errorValue); err == nil {
			captureStreamingError(errorValue, metrics)
		}
	}
	if metrics.errorCode == "" && strings.TrimSpace(event.Code) != "" {
		metrics.errorCode = strings.TrimSpace(event.Code)
	}
	if metrics.errorMessage == "" && strings.TrimSpace(event.Message) != "" {
		metrics.errorMessage = strings.TrimSpace(event.Message)
	}
}

func captureStreamingTerminalFailure(event streamingResponsesEvent, metrics *streamingMetrics) {
	if metrics.errorCode == "" {
		metrics.errorCode = "quality_probe_" + strings.ReplaceAll(event.Type, ".", "_")
	}
	if metrics.errorMessage != "" {
		return
	}
	if reason := strings.TrimSpace(event.Response.IncompleteDetails.Reason); reason != "" {
		metrics.errorMessage = "quality probe " + event.Type + ": " + reason
		return
	}
	metrics.errorMessage = "quality probe terminated with " + event.Type
}

func captureStreamingError(value any, metrics *streamingMetrics) {
	switch typedValue := value.(type) {
	case string:
		if metrics.errorMessage == "" {
			metrics.errorMessage = strings.TrimSpace(typedValue)
		}
	case map[string]any:
		if metrics.errorCode == "" {
			for _, key := range []string{"code", "type"} {
				if code, ok := typedValue[key].(string); ok && strings.TrimSpace(code) != "" {
					metrics.errorCode = strings.TrimSpace(code)
					break
				}
			}
		}
		if metrics.errorMessage == "" {
			if message, ok := typedValue["message"].(string); ok {
				metrics.errorMessage = strings.TrimSpace(message)
			}
		}
	}
}

func calculateVisibleTokens(outputTokens *int, reasoningTokens *int, visibleCharacters int) int {
	visibleTokenCount := 0
	if outputTokens != nil {
		visibleTokenCount = *outputTokens
		if reasoningTokens != nil {
			visibleTokenCount -= *reasoningTokens
		}
	}
	if visibleTokenCount <= 0 && visibleCharacters > 0 {
		visibleTokenCount = (visibleCharacters + 3) / 4
	}
	return visibleTokenCount
}

func classifyStreamingResult(
	statusCode int,
	requestError string,
	errorCode string,
	errorMessage string,
	responseBody string,
	tokensPerSecond float64,
) (string, string, string) {
	if isFreeUsageExhaustedError(errorCode, errorMessage, requestError) ||
		(statusCode == http.StatusTooManyRequests && isFreeUsageExhaustedError(responseBody)) {
		return ClassificationQuotaExhausted, QualityLevelQuotaExhausted, "free_usage_exhausted"
	}
	if requestError != "" || statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return ClassificationUnknown, QualityLevelUnknown, "request_error"
	}
	if tokensPerSecond >= qualityHardTokensPerSecond {
		return ClassificationSuspectedDegraded, QualityLevelHard, "hard_tps"
	}
	if tokensPerSecond >= qualitySoftTokensPerSecond {
		return ClassificationSuspectedDegraded, QualityLevelSoft, "soft_tps"
	}
	return ClassificationNormal, QualityLevelHealthy, "within_threshold"
}

func intPointer(value int) *int {
	return &value
}

func isFreeUsageExhaustedError(values ...string) bool {
	combinedValue := strings.ToLower(strings.Join(values, " "))
	return strings.Contains(combinedValue, "free-usage-exhausted") ||
		strings.Contains(combinedValue, "free usage has been exhausted") ||
		strings.Contains(combinedValue, "used all the included free usage") ||
		strings.Contains(combinedValue, "included free usage has been exhausted")
}
