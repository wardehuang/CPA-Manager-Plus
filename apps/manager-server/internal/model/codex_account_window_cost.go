package model

type CodexAccountWindowCost struct {
	AccountKey       string  `json:"accountKey"`
	WindowType       string  `json:"windowType"`
	WindowStartAtMS  int64   `json:"windowStartAtMs"`
	WindowResetAtMS  int64   `json:"windowResetAtMs"`
	EstimatedCost    float64 `json:"estimatedCost"`
	InputTokens      int64   `json:"inputTokens"`
	OutputTokens     int64   `json:"outputTokens"`
	// CachedTokens is the display cache-hit total: compatible residual + cache_read.
	// Cost calculation still prices residual cached and cache_read as separate buckets.
	CachedTokens     int64   `json:"cachedTokens"`
	IsQuotaExhausted bool    `json:"isQuotaExhausted"`
	CalculatedAtMS   int64   `json:"calculatedAtMs"`
	CreatedAtMS      int64   `json:"createdAtMs"`
	UpdatedAtMS      int64   `json:"updatedAtMs"`
}

type CodexAccountWindowCostTarget struct {
	AccountKey     string
	AuthIndex      string
	AccountID      string
	DisplayAccount string
	FileName       string
}

type CodexAccountWindowUsageAggregate struct {
	Model               string
	ServiceTier         string
	InputTokens         int64
	OutputTokens        int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}
