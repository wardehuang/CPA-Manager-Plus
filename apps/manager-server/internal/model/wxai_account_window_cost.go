package model

const (
	WxaiAccountWindowTypeWeekly        = "weekly"
	WxaiAccountWindowTypeMonthly       = "monthly"
	WxaiAccountWindowTypePriorityCycle = "priority_cycle"
)

type WxaiAccountWindowCost struct {
	AccountKey       string  `json:"accountKey"`
	WindowType       string  `json:"windowType"`
	WindowStartAtMS  int64   `json:"windowStartAtMs"`
	WindowResetAtMS  int64   `json:"windowResetAtMs"`
	EstimatedCost    float64 `json:"estimatedCost"`
	InputTokens      int64   `json:"inputTokens"`
	OutputTokens     int64   `json:"outputTokens"`
	CachedTokens     int64   `json:"cachedTokens"`
	IsQuotaExhausted bool    `json:"isQuotaExhausted"`
	CalculatedAtMS   int64   `json:"calculatedAtMs"`
	CreatedAtMS      int64   `json:"createdAtMs"`
	UpdatedAtMS      int64   `json:"updatedAtMs"`
}

type WxaiAccountWindowCostTarget struct {
	AccountKey string
	AuthIndex  string
}

type WxaiAccountWindowUsageAggregate struct {
	Model               string
	ServiceTier         string
	InputTokens         int64
	OutputTokens        int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}
