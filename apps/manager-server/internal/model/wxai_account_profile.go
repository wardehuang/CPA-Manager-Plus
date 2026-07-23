package model

type WxaiAccountProfile struct {
	AccountKey  string `json:"accountKey"`
	AccountType string `json:"accountType"`
	CreatedAtMS int64  `json:"createdAtMs"`
	UpdatedAtMS int64  `json:"updatedAtMs"`
}
