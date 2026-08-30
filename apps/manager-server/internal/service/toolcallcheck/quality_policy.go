package toolcallcheck

import (
	"net/http"
	"strings"
)

type StreamingQualityPolicy struct {
	SoftTokensPerSecond             float64 `json:"softTokensPerSecond"`
	HardTokensPerSecond             float64 `json:"hardTokensPerSecond"`
	TTFBSeconds                     float64 `json:"ttfbSeconds"`
	GenerationSeconds               float64 `json:"generationSeconds"`
	TokenThreshold                  int     `json:"tokenThreshold"`
	MinSummaryChars                 int     `json:"minSummaryChars"`
	MinEncryptedBytes               int     `json:"minEncryptedBytes"`
	EncryptedBytesPerReasoningToken int     `json:"encryptedBytesPerReasoningToken"`
	MinOutputTokens                 int     `json:"minOutputTokens"`
	BurstMinReasoningTokens         int     `json:"burstMinReasoningTokens"`
	BurstMaxVisibleTokens           int     `json:"burstMaxVisibleTokens"`
	BurstMaxWindowMS                int     `json:"burstMaxWindowMs"`
}

type QualityPolicy = StreamingQualityPolicy

type streamingThinkingEvidence struct {
	OutputTokens           int
	ReasoningTokens        int
	SummaryChars           int
	SummaryText            string
	EncryptedBytes         int
	ReasoningItemID        string
	ReasoningItemCompleted bool
	ReasoningMetadataError bool
	VisibleTokens          int
	VisibleFlushMS         int64
	EncryptedFloor         int
	IsRealThinking         bool
	Reason                 string
}

func evaluateStreamingThinking(evidence streamingThinkingEvidence, policy StreamingQualityPolicy) streamingThinkingEvidence {
	if evidence.OutputTokens < policy.MinOutputTokens {
		evidence.IsRealThinking = true
		evidence.Reason = "below_minimum_output_tokens"
		return evidence
	}
	evidence.VisibleTokens = evidence.OutputTokens - evidence.ReasoningTokens
	if evidence.VisibleTokens < 0 {
		evidence.VisibleTokens = 0
	}
	evidence.EncryptedFloor = policy.MinEncryptedBytes
	dynamicFloor := evidence.ReasoningTokens * policy.EncryptedBytesPerReasoningToken
	if dynamicFloor > evidence.EncryptedFloor {
		evidence.EncryptedFloor = dynamicFloor
	}
	hasSummaryEvidence := !evidence.ReasoningMetadataError &&
		evidence.SummaryChars >= policy.MinSummaryChars &&
		!isPlaceholderSummary(evidence.SummaryText)
	hasEncryptedEvidence := !evidence.ReasoningMetadataError &&
		evidence.ReasoningItemCompleted &&
		evidence.EncryptedBytes >= evidence.EncryptedFloor
	burstDump := evidence.ReasoningTokens >= policy.BurstMinReasoningTokens &&
		evidence.VisibleTokens > 0 &&
		evidence.VisibleTokens < policy.BurstMaxVisibleTokens &&
		evidence.VisibleFlushMS >= 0 &&
		evidence.VisibleFlushMS < int64(policy.BurstMaxWindowMS)
	evidence.IsRealThinking = (hasSummaryEvidence || hasEncryptedEvidence) && !burstDump
	switch {
	case burstDump:
		evidence.Reason = "burst_dump"
	case evidence.ReasoningMetadataError:
		evidence.Reason = "reasoning_metadata_invalid"
	case hasSummaryEvidence && hasEncryptedEvidence:
		evidence.Reason = "summary_and_encrypted_evidence"
	case hasSummaryEvidence:
		evidence.Reason = "summary_evidence"
	case hasEncryptedEvidence:
		evidence.Reason = "encrypted_evidence"
	case evidence.ReasoningTokens > 0:
		evidence.Reason = "reasoning_tokens_without_evidence"
	default:
		evidence.Reason = "missing_thinking_evidence"
	}
	return evidence
}

func classifyStreamingResult(
	statusCode int,
	requestError string,
	errorCode string,
	errorMessage string,
	responseBody string,
	tokensPerSecond float64,
	ttfbMS int64,
	generationMS int64,
	evidence streamingThinkingEvidence,
	policy StreamingQualityPolicy,
) (string, string, string) {
	if isFreeUsageExhaustedError(errorCode, errorMessage, requestError) ||
		(statusCode == http.StatusTooManyRequests && isFreeUsageExhaustedError(responseBody)) {
		return ClassificationQuotaExhausted, QualityLevelQuotaExhausted, "free_usage_exhausted"
	}
	if requestError != "" || statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return ClassificationUnknown, QualityLevelUnknown, "request_error"
	}
	if tokensPerSecond >= policy.HardTokensPerSecond {
		return ClassificationSuspectedDegraded, QualityLevelHard, "hard_tps"
	}
	if tokensPerSecond > policy.SoftTokensPerSecond &&
		tokensPerSecond < policy.HardTokensPerSecond &&
		!evidence.IsRealThinking {
		return ClassificationSuspectedDegraded, QualityLevelSoft, "soft_tps_missing_real_thinking"
	}
	if float64(ttfbMS) > policy.TTFBSeconds*1000 &&
		float64(generationMS) < policy.GenerationSeconds*1000 &&
		evidence.OutputTokens+evidence.ReasoningTokens > policy.TokenThreshold {
		return ClassificationSuspectedDegraded, QualityLevelSoft, "ttfb_downgrade"
	}
	return ClassificationNormal, QualityLevelHealthy, "within_threshold"
}

func isPlaceholderSummary(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "thinking", "thinking...", "thinking…":
		return true
	default:
		return false
	}
}
