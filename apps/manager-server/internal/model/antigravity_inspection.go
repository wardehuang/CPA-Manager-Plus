package model

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const (
	AntigravityInspectionScheduleModeInterval   = "interval"
	AntigravityInspectionScheduleModeTimePoints = "time_points"

	AntigravityInspectionAutoActionNone    = "none"
	AntigravityInspectionAutoActionEnable  = "enable"
	AntigravityInspectionAutoActionDisable = "disable"
	AntigravityInspectionAutoActionDelete  = "delete"

	AntigravityInspectionStatusRunning   = "running"
	AntigravityInspectionStatusCompleted = "completed"
	AntigravityInspectionStatusFailed    = "failed"

	AntigravityInspectionTriggerManual    = "manual"
	AntigravityInspectionTriggerScheduled = "scheduled"

	AntigravityInspectionActionStatusNone        = "none"
	AntigravityInspectionActionStatusPending     = "pending"
	AntigravityInspectionActionStatusSuccess     = "success"
	AntigravityInspectionActionStatusFailed      = "failed"
	AntigravityInspectionActionStatusSkipped     = "skipped"
	AntigravityInspectionActionStatusNeedsReview = "needs_review"

	AntigravityTargetProviderClaude = "claude"
	AntigravityTargetProviderGemini = "gemini"
	AntigravityTargetProviderServer = "server"
)

type ManagerAntigravityInspectionConfig struct {
	Enabled              *bool                                      `json:"enabled,omitempty"`
	Schedule             ManagerAntigravityInspectionScheduleConfig `json:"schedule"`
	TargetType           string                                     `json:"targetType,omitempty"`
	TargetProvider       string                                     `json:"targetProvider,omitempty"`
	Workers              int                                        `json:"workers,omitempty"`
	DeleteWorkers        int                                        `json:"deleteWorkers,omitempty"`
	Timeout              int                                        `json:"timeout,omitempty"`
	Retries              int                                        `json:"retries,omitempty"`
	UserAgent            string                                     `json:"userAgent,omitempty"`
	UsedPercentThreshold float64                                    `json:"usedPercentThreshold,omitempty"`
	SampleSize           int                                        `json:"sampleSize,omitempty"`
	AutoActionMode       string                                     `json:"autoActionMode,omitempty"`
}

type ManagerAntigravityInspectionScheduleConfig struct {
	Mode            string   `json:"mode,omitempty"`
	TimePoints      []string `json:"timePoints,omitempty"`
	IntervalMinutes int      `json:"intervalMinutes,omitempty"`
	TimeZone        string   `json:"timeZone,omitempty"`
}

type AntigravityInspectionRun struct {
	ID             int64                              `json:"id"`
	TriggerType    string                             `json:"triggerType"`
	TriggerKey     string                             `json:"triggerKey,omitempty"`
	TargetProvider string                             `json:"targetProvider,omitempty"`
	Status         string                             `json:"status"`
	StartedAtMS    int64                              `json:"startedAtMs"`
	FinishedAtMS   int64                              `json:"finishedAtMs,omitempty"`
	TotalFiles     int                                `json:"totalFiles"`
	ProbeSetCount  int                                `json:"probeSetCount"`
	SampledCount   int                                `json:"sampledCount"`
	DisabledCount  int                                `json:"disabledCount"`
	EnabledCount   int                                `json:"enabledCount"`
	DeleteCount    int                                `json:"deleteCount"`
	DisableCount   int                                `json:"disableCount"`
	EnableCount    int                                `json:"enableCount"`
	ReauthCount    int                                `json:"reauthCount"`
	KeepCount      int                                `json:"keepCount"`
	Error          string                             `json:"error,omitempty"`
	Settings       ManagerAntigravityInspectionConfig `json:"settings"`
	SettingsJSON   string                             `json:"-"`
	CreatedAtMS    int64                              `json:"createdAtMs"`
	UpdatedAtMS    int64                              `json:"updatedAtMs"`
}

type AntigravityInspectionQuotaWindow struct {
	ID                 string         `json:"id"`
	LabelKey           string         `json:"labelKey"`
	LabelParams        map[string]any `json:"labelParams,omitempty"`
	UsedPercent        *float64       `json:"usedPercent,omitempty"`
	ResetLabel         string         `json:"resetLabel"`
	LimitWindowSeconds *float64       `json:"limitWindowSeconds,omitempty"`
}

type AntigravityInspectionResult struct {
	ID               int64                              `json:"id"`
	RunID            int64                              `json:"runId"`
	AccountKey       string                             `json:"accountKey"`
	FileName         string                             `json:"fileName"`
	DisplayAccount   string                             `json:"displayAccount"`
	AuthIndex        string                             `json:"authIndex,omitempty"`
	AccountID        string                             `json:"accountId,omitempty"`
	Provider         string                             `json:"provider"`
	TargetProvider   string                             `json:"targetProvider,omitempty"`
	Disabled         bool                               `json:"disabled"`
	Status           string                             `json:"status,omitempty"`
	State            string                             `json:"state,omitempty"`
	Action           string                             `json:"action"`
	ActionReason     string                             `json:"actionReason"`
	ActionStatus     string                             `json:"actionStatus,omitempty"`
	ExecutedAction   string                             `json:"executedAction,omitempty"`
	ActionError      string                             `json:"actionError,omitempty"`
	StatusCode       *int                               `json:"statusCode,omitempty"`
	UsedPercent      *float64                           `json:"usedPercent,omitempty"`
	IsQuota          bool                               `json:"isQuota"`
	Error            string                             `json:"error,omitempty"`
	PlanType         string                             `json:"planType,omitempty"`
	QuotaWindows     []AntigravityInspectionQuotaWindow `json:"quotaWindows,omitempty"`
	QuotaWindowsJSON string                             `json:"-"`
	ErrorKind        string                             `json:"errorKind,omitempty"`
	ErrorDetail      string                             `json:"errorDetail,omitempty"`
	CreatedAtMS      int64                              `json:"createdAtMs"`
}

type AntigravityInspectionLog struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"runId"`
	Level       string `json:"level"`
	Message     string `json:"message"`
	DetailJSON  string `json:"-"`
	Detail      any    `json:"detail,omitempty"`
	CreatedAtMS int64  `json:"createdAtMs"`
}

type AntigravityAccountStatusDetail struct {
	RunID          int64    `json:"runId"`
	AccountKey     string   `json:"accountKey"`
	TargetProvider string   `json:"targetProvider"`
	Priority       *int     `json:"priority,omitempty"`
	AccountType    string   `json:"accountType,omitempty"`
	UsedPercent    *float64 `json:"usedPercent,omitempty"`
	ResetAtMS      int64    `json:"resetAtMs,omitempty"`
	CheckedAtMS    int64    `json:"checkedAtMs,omitempty"`
	CreatedAtMS    int64    `json:"createdAtMs"`
	UpdatedAtMS    int64    `json:"updatedAtMs"`
}

type AntigravityAccountWindowCost struct {
	AccountKey       string  `json:"accountKey"`
	TargetProvider   string  `json:"targetProvider"`
	WindowType       string  `json:"windowType"`
	WindowStartAtMS  int64   `json:"windowStartAtMs"`
	WindowResetAtMS  int64   `json:"windowResetAtMs"`
	EstimatedCost    float64 `json:"estimatedCost"`
	IsQuotaExhausted bool    `json:"isQuotaExhausted"`
	CalculatedAtMS   int64   `json:"calculatedAtMs"`
	CreatedAtMS      int64   `json:"createdAtMs"`
	UpdatedAtMS      int64   `json:"updatedAtMs"`
}

type AntigravityPriorityAdjustment struct {
	AccountKey       string `json:"accountKey"`
	TargetProvider   string `json:"targetProvider"`
	FileName         string `json:"fileName"`
	DisplayAccount   string `json:"displayAccount"`
	AuthIndex        string `json:"authIndex,omitempty"`
	AccountID        string `json:"accountId,omitempty"`
	OriginalPriority *int   `json:"originalPriority,omitempty"`
	RecoverAtMS      int64  `json:"recoverAtMs,omitempty"`
	CreatedAtMS      int64  `json:"createdAtMs"`
	UpdatedAtMS      int64  `json:"updatedAtMs"`
}

type AntigravityAccountStatusItem struct {
	ID                int64                          `json:"id"`
	RunID             int64                          `json:"runId"`
	AccountKey        string                         `json:"accountKey"`
	FileName          string                         `json:"fileName"`
	DisplayAccount    string                         `json:"displayAccount"`
	AuthIndex         string                         `json:"authIndex,omitempty"`
	AccountID         string                         `json:"accountId,omitempty"`
	Provider          string                         `json:"provider"`
	TargetProvider    string                         `json:"targetProvider"`
	Disabled          bool                           `json:"disabled"`
	Status            string                         `json:"status,omitempty"`
	State             string                         `json:"state,omitempty"`
	Action            string                         `json:"action"`
	ActionReason      string                         `json:"actionReason"`
	ActionStatus      string                         `json:"actionStatus,omitempty"`
	ExecutedAction    string                         `json:"executedAction,omitempty"`
	ActionError       string                         `json:"actionError,omitempty"`
	StatusCode        *int                           `json:"statusCode,omitempty"`
	UsedPercent       *float64                       `json:"usedPercent,omitempty"`
	IsQuota           bool                           `json:"isQuota"`
	Error             string                         `json:"error,omitempty"`
	ResultCreatedAtMS int64                          `json:"resultCreatedAtMs"`
	Priority          *int                           `json:"priority,omitempty"`
	AccountType       string                         `json:"accountType,omitempty"`
	ResetAtMS         int64                          `json:"resetAtMs,omitempty"`
	CheckedAtMS       int64                          `json:"checkedAtMs,omitempty"`
	OriginalPriority  *int                           `json:"originalPriority,omitempty"`
	WindowCosts       []AntigravityAccountWindowCost `json:"windowCosts,omitempty"`
}

type AntigravityAccountStatusResponse struct {
	Run   AntigravityInspectionRun       `json:"run"`
	Items []AntigravityAccountStatusItem `json:"items"`
}

func DefaultAntigravityInspectionConfig() ManagerAntigravityInspectionConfig {
	return ManagerAntigravityInspectionConfig{
		Enabled: boolPtr(false),
		Schedule: ManagerAntigravityInspectionScheduleConfig{
			Mode:            AntigravityInspectionScheduleModeInterval,
			IntervalMinutes: 60,
		},
		TargetType:           "antigravity",
		TargetProvider:       AntigravityTargetProviderClaude,
		Workers:              4,
		DeleteWorkers:        4,
		Timeout:              15000,
		Retries:              0,
		UserAgent:            "cpa-manager-plus-antigravity-inspection",
		UsedPercentThreshold: 100,
		SampleSize:           0,
		AutoActionMode:       AntigravityInspectionAutoActionNone,
	}
}

func NormalizeAntigravityInspectionConfig(input ManagerAntigravityInspectionConfig, fallback ManagerAntigravityInspectionConfig) ManagerAntigravityInspectionConfig {
	base := fallback
	if base.TargetType == "" {
		base = DefaultAntigravityInspectionConfig()
	}
	next := base
	if input.Enabled != nil {
		next.Enabled = boolPtr(*input.Enabled)
	}
	next.Schedule = NormalizeAntigravityInspectionSchedule(input.Schedule, base.Schedule)
	next.TargetType = "antigravity"
	next.TargetProvider = NormalizeAntigravityTargetProvider(input.TargetProvider, base.TargetProvider)
	next.Workers = positiveOr(input.Workers, base.Workers)
	next.DeleteWorkers = positiveOr(input.DeleteWorkers, positiveOr(input.Workers, base.DeleteWorkers))
	next.Timeout = positiveOr(input.Timeout, base.Timeout)
	if input.Retries >= 0 {
		next.Retries = input.Retries
	}
	next.UserAgent = valueOr(input.UserAgent, base.UserAgent)
	next.UsedPercentThreshold = normalizePercent(input.UsedPercentThreshold, base.UsedPercentThreshold)
	if input.SampleSize >= 0 {
		next.SampleSize = input.SampleSize
	}
	next.AutoActionMode = NormalizeAntigravityInspectionAutoActionMode(input.AutoActionMode, base.AutoActionMode)
	return next
}

func NormalizeAntigravityInspectionSchedule(input ManagerAntigravityInspectionScheduleConfig, fallback ManagerAntigravityInspectionScheduleConfig) ManagerAntigravityInspectionScheduleConfig {
	base := fallback
	if base.Mode == "" {
		base = DefaultAntigravityInspectionConfig().Schedule
	}
	next := base
	timePoints := NormalizeAntigravityInspectionTimePoints(input.TimePoints)
	if len(timePoints) > 0 {
		next.TimePoints = timePoints
	}
	if input.IntervalMinutes > 0 {
		next.IntervalMinutes = input.IntervalMinutes
	}
	next.TimeZone = NormalizeCodexInspectionTimeZone(input.TimeZone, strings.TrimSpace(fallback.TimeZone))
	switch strings.ToLower(strings.TrimSpace(input.Mode)) {
	case AntigravityInspectionScheduleModeTimePoints:
		next.Mode = AntigravityInspectionScheduleModeTimePoints
	case AntigravityInspectionScheduleModeInterval:
		next.Mode = AntigravityInspectionScheduleModeInterval
	case "":
		if len(timePoints) > 0 {
			next.Mode = AntigravityInspectionScheduleModeTimePoints
		} else if input.IntervalMinutes > 0 {
			next.Mode = AntigravityInspectionScheduleModeInterval
		}
	}
	if next.Mode == AntigravityInspectionScheduleModeTimePoints && len(next.TimePoints) == 0 {
		next.Mode = AntigravityInspectionScheduleModeInterval
	}
	if next.Mode == AntigravityInspectionScheduleModeInterval && next.IntervalMinutes <= 0 {
		next.IntervalMinutes = 60
	}
	return next
}

func NormalizeAntigravityInspectionTimePoints(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		point := NormalizeCodexInspectionTimePoint(value)
		if point == "" {
			continue
		}
		if _, ok := seen[point]; ok {
			continue
		}
		seen[point] = struct{}{}
		out = append(out, point)
	}
	sort.Strings(out)
	return out
}

func NormalizeAntigravityTargetProvider(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AntigravityTargetProviderClaude:
		return AntigravityTargetProviderClaude
	case AntigravityTargetProviderGemini:
		return AntigravityTargetProviderGemini
	case AntigravityTargetProviderServer:
		return AntigravityTargetProviderServer
	}
	if strings.TrimSpace(fallback) != "" {
		return NormalizeAntigravityTargetProvider(fallback, AntigravityTargetProviderClaude)
	}
	return AntigravityTargetProviderClaude
}

func NormalizeAntigravityInspectionAutoActionMode(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AntigravityInspectionAutoActionEnable:
		return AntigravityInspectionAutoActionEnable
	case AntigravityInspectionAutoActionDisable:
		return AntigravityInspectionAutoActionDisable
	case AntigravityInspectionAutoActionDelete:
		return AntigravityInspectionAutoActionDelete
	case AntigravityInspectionAutoActionNone:
		return AntigravityInspectionAutoActionNone
	}
	if strings.TrimSpace(fallback) != "" {
		return NormalizeAntigravityInspectionAutoActionMode(fallback, AntigravityInspectionAutoActionNone)
	}
	return AntigravityInspectionAutoActionNone
}

func NormalizeAntigravityInspectionActionStatus(value string, action string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AntigravityInspectionActionStatusPending,
		AntigravityInspectionActionStatusSuccess,
		AntigravityInspectionActionStatusFailed,
		AntigravityInspectionActionStatusSkipped,
		AntigravityInspectionActionStatusNeedsReview:
		return strings.ToLower(strings.TrimSpace(value))
	case AntigravityInspectionActionStatusNone:
		return AntigravityInspectionActionStatusNone
	}
	if strings.TrimSpace(action) == "" || strings.TrimSpace(action) == "keep" {
		return AntigravityInspectionActionStatusNone
	}
	return AntigravityInspectionActionStatusPending
}

func MarshalAntigravityInspectionSettings(settings ManagerAntigravityInspectionConfig) string {
	data, err := json.Marshal(settings)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func UnmarshalAntigravityInspectionSettings(raw string) ManagerAntigravityInspectionConfig {
	settings := DefaultAntigravityInspectionConfig()
	if strings.TrimSpace(raw) == "" {
		return settings
	}
	var parsed ManagerAntigravityInspectionConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return settings
	}
	return NormalizeAntigravityInspectionConfig(parsed, settings)
}

func MarshalAntigravityInspectionQuotaWindows(windows []AntigravityInspectionQuotaWindow) string {
	data, err := json.Marshal(windows)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func UnmarshalAntigravityInspectionQuotaWindows(raw string) []AntigravityInspectionQuotaWindow {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var windows []AntigravityInspectionQuotaWindow
	if err := json.Unmarshal([]byte(raw), &windows); err != nil {
		return nil
	}
	return windows
}

func ResolveAntigravityInspectionLocation(schedule ManagerAntigravityInspectionScheduleConfig) *time.Location {
	if loc, err := time.LoadLocation(strings.TrimSpace(schedule.TimeZone)); err == nil {
		return loc
	}
	return time.Local
}
