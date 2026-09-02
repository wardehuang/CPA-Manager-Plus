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
)

type streamingMetrics struct {
	modelAnswer                strings.Builder
	summaryDelta               strings.Builder
	outputTokens               *int
	reasoningTokens            *int
	thinkingDelta              bool
	errorCode                  string
	errorMessage               string
	firstPayloadReady          bool
	firstPayloadAt             time.Time
	firstGeneratedReady        bool
	firstGeneratedAt           time.Time
	firstVisibleReady          bool
	firstVisibleAt             time.Time
	outputTextChars            int
	completedMessageCount      int
	completedMessageIDs        map[string]struct{}
	refusalDetected            bool
	summaryChars               int
	summaryText                string
	encryptedBytes             int
	reasoningItemID            string
	reasoningItemCompleted     bool
	reasoningMetadataError     bool
	completedFunctionCallCount int
	completedFunctionCallIDs   map[string]struct{}
	toolCallNames              []string
	toolCallNameSet            map[string]struct{}
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
	qualityPolicy StreamingQualityPolicy,
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
			streamInspector.MarkEventTimingsConsumed()
			if streamInspector.streamTerminated {
				break
			}
		}
		if readError != nil {
			if errors.Is(readError, io.EOF) {
				streamInspector.Finish()
				streamInspector.MarkEventTimingsConsumed()
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
	result.ErrorCode = metrics.errorCode

	currentTotalMS := time.Since(startedAt).Milliseconds()
	result.TotalMS = currentTotalMS
	result.DurationMS = currentTotalMS
	if !metrics.firstPayloadAt.IsZero() {
		firstPayloadMS := metrics.firstPayloadAt.Sub(startedAt).Milliseconds()
		result.GenerationMS = currentTotalMS - firstPayloadMS
		if result.GenerationMS < 1 {
			result.GenerationMS = 1
		}
	}
	if !metrics.firstGeneratedAt.IsZero() {
		result.FirstTokenMS = metrics.firstGeneratedAt.Sub(startedAt).Milliseconds()
	}
	visibleFlushMS := int64(-1)
	if !metrics.firstVisibleAt.IsZero() {
		visibleFlushMS = currentTotalMS - metrics.firstVisibleAt.Sub(startedAt).Milliseconds()
	}
	outputTokens := pointerIntValue(metrics.outputTokens)
	reasoningTokens := pointerIntValue(metrics.reasoningTokens)
	recordStreamingSummary(metrics.summaryDelta.String(), &metrics.summaryChars, &metrics.summaryText)
	evidence := evaluateStreamingThinking(streamingThinkingEvidence{
		OutputTokens:               outputTokens,
		ReasoningTokens:            reasoningTokens,
		SummaryChars:               metrics.summaryChars,
		SummaryText:                metrics.summaryText,
		EncryptedBytes:             metrics.encryptedBytes,
		ReasoningItemID:            metrics.reasoningItemID,
		ReasoningItemCompleted:     metrics.reasoningItemCompleted,
		ReasoningMetadataError:     metrics.reasoningMetadataError,
		VisibleFlushMS:             visibleFlushMS,
		CompletedFunctionCallCount: metrics.completedFunctionCallCount,
		OutputTextChars:            metrics.outputTextChars,
		CompletedMessageCount:      metrics.completedMessageCount,
		RefusalDetected:            metrics.refusalDetected,
	}, qualityPolicy)
	result.VisibleTokens = intPointer(evidence.VisibleTokens)
	result.SummaryChars = evidence.SummaryChars
	result.EncryptedBytes = evidence.EncryptedBytes
	result.EncryptedFloor = evidence.EncryptedFloor
	result.IsRealThinking = evidence.IsRealThinking
	result.RealThinkingReason = evidence.Reason
	result.VisibleFlushMS = evidence.VisibleFlushMS
	result.EvaluatedTokens = outputTokens + reasoningTokens
	result.ToolCallDetected = metrics.completedFunctionCallCount > 0
	result.ToolCallNames = metrics.toolCallNames
	result.CompletedFunctionCallCount = metrics.completedFunctionCallCount
	result.ToolCallOnly = evidence.ToolCallOnly
	result.OutputTextChars = evidence.OutputTextChars
	result.CompletedMessageCount = evidence.CompletedMessageCount
	result.RefusalDetected = evidence.RefusalDetected

	outputTokensPerSecond := 0.0
	if result.GenerationMS > 0 && result.EvaluatedTokens > 0 {
		outputTokensPerSecond = float64(result.EvaluatedTokens) * 1000 / float64(result.GenerationMS)
	}
	result.OutputTokensPerSecond = &outputTokensPerSecond
	if outputTokensPerSecond > qualityPolicy.SoftTokensPerSecond &&
		outputTokensPerSecond < qualityPolicy.HardTokensPerSecond &&
		!evidence.IsRealThinking &&
		!evidence.ToolCallOnly &&
		evidence.CompletedMessageCount > 0 {
		result.CompletedMutationEvidence = hasCompletedMutationEvidence(result.RequestBody)
	}
	answerMatched := strings.Contains(result.ModelAnswer, qualityExpectedAnswer)
	result.AnswerMatched = answerMatched
	result.Classification, result.QualityLevel, result.ClassificationReason = classifyStreamingResult(
		result.StatusCode,
		result.Error,
		result.ErrorCode,
		metrics.errorMessage,
		result.ResponseBody,
		outputTokensPerSecond,
		result.TTFBMS,
		result.GenerationMS,
		evidence,
		result.CompletedMutationEvidence,
		qualityPolicy,
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

func (inspector *streamingSSEInspector) MarkEventTimingsConsumed() {
	metrics := inspector.metrics
	now := time.Now()
	if metrics.firstPayloadReady && metrics.firstPayloadAt.IsZero() {
		metrics.firstPayloadReady = false
		metrics.firstPayloadAt = now
	}
	if metrics.firstGeneratedReady && metrics.firstGeneratedAt.IsZero() {
		metrics.firstGeneratedReady = false
		metrics.firstGeneratedAt = now
	}
	if metrics.firstVisibleReady && metrics.firstVisibleAt.IsZero() {
		metrics.firstVisibleReady = false
		metrics.firstVisibleAt = now
	}
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
	var message map[string]any
	if err := json.Unmarshal([]byte(trimmedData), &message); err != nil {
		return false
	}
	if metrics.firstPayloadAt.IsZero() && !metrics.firstPayloadReady {
		metrics.firstPayloadReady = true
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
		metrics.outputTextChars += utf8.RuneCountInString(deltaText)
		if metrics.firstVisibleAt.IsZero() && !metrics.firstVisibleReady {
			metrics.firstVisibleReady = true
		}
	}
	if event.Type == "response.refusal.delta" && deltaText != "" {
		metrics.refusalDetected = true
	}
	collectStreamingThinkingEvidence(message, metrics)

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

func collectStreamingThinkingEvidence(message map[string]any, metrics *streamingMetrics) {
	eventType := strings.ToLower(streamingStringField(message, "type"))
	switch eventType {
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		recordStreamingReasoningItemID(streamingStringField(message, "item_id"), metrics)
		metrics.summaryDelta.WriteString(streamingStringField(message, "delta"))
	case "response.reasoning_summary_text.done", "response.reasoning_text.done":
		recordStreamingReasoningItemID(streamingStringField(message, "item_id"), metrics)
		recordStreamingSummary(streamingStringField(message, "text"), &metrics.summaryChars, &metrics.summaryText)
	case "response.reasoning_summary_part.done":
		recordStreamingReasoningItemID(streamingStringField(message, "item_id"), metrics)
		if part, ok := message["part"].(map[string]any); ok && strings.EqualFold(streamingStringField(part, "type"), "summary_text") {
			recordStreamingSummary(streamingStringField(part, "text"), &metrics.summaryChars, &metrics.summaryText)
		}
	case "response.output_item.added":
		if item, ok := message["item"].(map[string]any); ok {
			collectStreamingOutputItem(item, metrics, false)
		}
	case "response.output_item.done":
		if item, ok := message["item"].(map[string]any); ok {
			collectStreamingOutputItem(item, metrics, true)
		}
	case "response.completed":
		if response, ok := message["response"].(map[string]any); ok {
			if output, ok := response["output"].([]any); ok {
				for _, rawItem := range output {
					if item, itemOK := rawItem.(map[string]any); itemOK {
						collectStreamingOutputItem(item, metrics, true)
					}
				}
			}
		}
	}
}

func collectStreamingOutputItem(item map[string]any, metrics *streamingMetrics, terminal bool) {
	switch strings.ToLower(streamingStringField(item, "type")) {
	case "reasoning":
		recordStreamingReasoningItemID(streamingStringField(item, "id"), metrics)
		if terminal || strings.EqualFold(streamingStringField(item, "status"), "completed") {
			metrics.reasoningItemCompleted = true
		}
		metrics.encryptedBytes = maxStreamingInt(metrics.encryptedBytes, len([]byte(strings.TrimSpace(streamingStringField(item, "encrypted_content")))))
		if summaries, ok := item["summary"].([]any); ok {
			for _, rawSummary := range summaries {
				summary, summaryOK := rawSummary.(map[string]any)
				if !summaryOK || !strings.EqualFold(streamingStringField(summary, "type"), "summary_text") {
					continue
				}
				recordStreamingSummary(streamingStringField(summary, "text"), &metrics.summaryChars, &metrics.summaryText)
			}
		}
	case "message":
		itemID := streamingStringField(item, "id")
		if terminal && strings.EqualFold(streamingStringField(item, "status"), "completed") && itemID != "" {
			if metrics.completedMessageIDs == nil {
				metrics.completedMessageIDs = make(map[string]struct{})
			}
			if _, exists := metrics.completedMessageIDs[itemID]; !exists {
				metrics.completedMessageIDs[itemID] = struct{}{}
				metrics.completedMessageCount++
			}
		}
		if content, ok := item["content"].([]any); ok {
			outputTextChars := 0
			for _, rawContent := range content {
				part, partOK := rawContent.(map[string]any)
				if !partOK {
					continue
				}
				switch strings.ToLower(streamingStringField(part, "type")) {
				case "output_text":
					outputTextChars += utf8.RuneCountInString(streamingStringField(part, "text"))
				case "refusal":
					metrics.refusalDetected = true
				}
			}
			metrics.outputTextChars = maxStreamingInt(metrics.outputTextChars, outputTextChars)
		}
	case "function_call":
		callID := streamingStringField(item, "call_id")
		name := streamingStringField(item, "name")
		if !terminal || !strings.EqualFold(streamingStringField(item, "status"), "completed") || callID == "" || name == "" || streamingStringField(item, "arguments") == "" {
			return
		}
		if metrics.completedFunctionCallIDs == nil {
			metrics.completedFunctionCallIDs = make(map[string]struct{})
		}
		if _, exists := metrics.completedFunctionCallIDs[callID]; exists {
			return
		}
		metrics.completedFunctionCallIDs[callID] = struct{}{}
		metrics.completedFunctionCallCount++
		if metrics.toolCallNameSet == nil {
			metrics.toolCallNameSet = make(map[string]struct{})
		}
		if _, exists := metrics.toolCallNameSet[name]; !exists {
			metrics.toolCallNameSet[name] = struct{}{}
			metrics.toolCallNames = append(metrics.toolCallNames, name)
		}
	}
}

func recordStreamingReasoningItemID(itemID string, metrics *streamingMetrics) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		metrics.reasoningMetadataError = true
		return
	}
	if metrics.reasoningItemID == "" {
		metrics.reasoningItemID = itemID
		return
	}
	if metrics.reasoningItemID != itemID {
		metrics.reasoningMetadataError = true
	}
}

func recordStreamingSummary(text string, summaryChars *int, summaryText *string) {
	text = strings.TrimSpace(text)
	characters := utf8.RuneCountInString(text)
	if characters > *summaryChars {
		*summaryChars = characters
		*summaryText = text
	}
}

func streamingStringField(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return strings.TrimSpace(text)
}

func pointerIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func maxStreamingInt(left, right int) int {
	if left > right {
		return left
	}
	return right
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
