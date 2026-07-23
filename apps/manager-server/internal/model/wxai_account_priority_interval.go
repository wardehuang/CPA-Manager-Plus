package model

type WxaiAccountPriorityInterval struct {
	AccountKey  string
	StartedAtMS *int64
	EndedAtMS   *int64
	CreatedAtMS int64
	UpdatedAtMS int64
}
