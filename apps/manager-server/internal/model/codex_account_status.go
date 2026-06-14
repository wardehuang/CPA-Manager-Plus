package model

type CodexAccountStatusDetail struct {
	RunID                               int64    `json:"runId"`
	AccountKey                          string   `json:"accountKey"`
	AccountType                         string   `json:"accountType,omitempty"`
	FiveHourUsedPercent                 *float64 `json:"fiveHourUsedPercent,omitempty"`
	FiveHourResetAtMS                   int64    `json:"fiveHourResetAtMs,omitempty"`
	WeeklyUsedPercent                   *float64 `json:"weeklyUsedPercent,omitempty"`
	WeeklyResetAtMS                     int64    `json:"weeklyResetAtMs,omitempty"`
	MonthlyUsedPercent                  *float64 `json:"monthlyUsedPercent,omitempty"`
	MonthlyResetAtMS                    int64    `json:"monthlyResetAtMs,omitempty"`
	RateLimitResetCreditsAvailableCount *int     `json:"rateLimitResetCreditsAvailableCount,omitempty"`
	CheckedAtMS                         int64    `json:"checkedAtMs,omitempty"`
	CreatedAtMS                         int64    `json:"createdAtMs"`
	UpdatedAtMS                         int64    `json:"updatedAtMs"`
}

type CodexAccountStatusItem struct {
	ID                                  int64    `json:"id"`
	RunID                               int64    `json:"runId"`
	AccountKey                          string   `json:"accountKey"`
	FileName                            string   `json:"fileName"`
	DisplayAccount                      string   `json:"displayAccount"`
	AuthIndex                           string   `json:"authIndex,omitempty"`
	AccountID                           string   `json:"accountId,omitempty"`
	Provider                            string   `json:"provider"`
	Disabled                            bool     `json:"disabled"`
	Status                              string   `json:"status,omitempty"`
	State                               string   `json:"state,omitempty"`
	Action                              string   `json:"action"`
	ActionReason                        string   `json:"actionReason"`
	ActionStatus                        string   `json:"actionStatus,omitempty"`
	ExecutedAction                      string   `json:"executedAction,omitempty"`
	ActionError                         string   `json:"actionError,omitempty"`
	StatusCode                          *int     `json:"statusCode,omitempty"`
	UsedPercent                         *float64 `json:"usedPercent,omitempty"`
	IsQuota                             bool     `json:"isQuota"`
	Error                               string   `json:"error,omitempty"`
	ResultCreatedAtMS                   int64    `json:"resultCreatedAtMs"`
	AccountType                         string   `json:"accountType,omitempty"`
	FiveHourUsedPercent                 *float64 `json:"fiveHourUsedPercent,omitempty"`
	FiveHourResetAtMS                   int64    `json:"fiveHourResetAtMs,omitempty"`
	WeeklyUsedPercent                   *float64 `json:"weeklyUsedPercent,omitempty"`
	WeeklyResetAtMS                     int64    `json:"weeklyResetAtMs,omitempty"`
	MonthlyUsedPercent                  *float64 `json:"monthlyUsedPercent,omitempty"`
	MonthlyResetAtMS                    int64    `json:"monthlyResetAtMs,omitempty"`
	RateLimitResetCreditsAvailableCount *int     `json:"rateLimitResetCreditsAvailableCount,omitempty"`
	CheckedAtMS                         int64    `json:"checkedAtMs,omitempty"`
}

type CodexAccountStatusResponse struct {
	Run   CodexInspectionRun       `json:"run"`
	Items []CodexAccountStatusItem `json:"items"`
}
