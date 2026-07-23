package model

type WxaiPriorityAdjustment struct {
	AccountKey       string `json:"accountKey"`
	FileName         string `json:"fileName"`
	DisplayAccount   string `json:"displayAccount"`
	AuthIndex        string `json:"authIndex,omitempty"`
	AccountID        string `json:"accountId,omitempty"`
	OriginalPriority *int   `json:"originalPriority,omitempty"`
	AdjustedPriority int    `json:"adjustedPriority"`
	RecoverAtMS      int64  `json:"recoverAtMs,omitempty"`
	CreatedAtMS      int64  `json:"createdAtMs"`
	UpdatedAtMS      int64  `json:"updatedAtMs"`
}
