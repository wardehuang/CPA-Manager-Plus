package model

type WxaiRealtimeDegradationState struct {
	AccountKey       string `json:"accountKey"`
	FileName         string `json:"fileName"`
	DisplayAccount   string `json:"displayAccount"`
	AuthIndex        string `json:"authIndex,omitempty"`
	AccountID        string `json:"accountId,omitempty"`
	DegradationCount int    `json:"degradationCount"`
	CooldownUntilMS  int64  `json:"cooldownUntilMs,omitempty"`
	CreatedAtMS      int64  `json:"createdAtMs"`
	UpdatedAtMS      int64  `json:"updatedAtMs"`
}
