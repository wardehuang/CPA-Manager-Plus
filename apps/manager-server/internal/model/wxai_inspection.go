package model

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	WxaiInspectionScheduleModeInterval   = "interval"
	WxaiInspectionScheduleModeTimePoints = "time_points"

	WxaiInspectionAutoActionNone    = "none"
	WxaiInspectionAutoActionEnable  = "enable"
	WxaiInspectionAutoActionDisable = "disable"
	WxaiInspectionAutoActionDelete  = "delete"

	WxaiInspectionStatusRunning   = "running"
	WxaiInspectionStatusCompleted = "completed"
	WxaiInspectionStatusFailed    = "failed"

	WxaiInspectionTriggerManual    = "manual"
	WxaiInspectionTriggerScheduled = "scheduled"

	WxaiInspectionActionStatusNone        = "none"
	WxaiInspectionActionStatusPending     = "pending"
	WxaiInspectionActionStatusSuccess     = "success"
	WxaiInspectionActionStatusFailed      = "failed"
	WxaiInspectionActionStatusSkipped     = "skipped"
	WxaiInspectionActionStatusNeedsReview = "needs_review"
)

type ManagerWxaiInspectionConfig struct {
	Enabled       *bool                               `json:"enabled,omitempty"`
	Schedule      ManagerWxaiInspectionScheduleConfig `json:"schedule"`
	TargetType    string                              `json:"targetType,omitempty"`
	Workers       int                                 `json:"workers,omitempty"`
	DeleteWorkers int                                 `json:"deleteWorkers,omitempty"`
	Timeout       int                                 `json:"timeout,omitempty"`
	Retries       int                                 `json:"retries,omitempty"`
	// WorkerStartStaggerMs: worker 错峰启动间隔（毫秒）。nil=默认；0=不交错（同时启动）。
	WorkerStartStaggerMs *int `json:"workerStartStaggerMs,omitempty"`
	// AccountTakeStaggerMs: 全局取账号间隔（毫秒）。nil=默认；0=不限流。
	AccountTakeStaggerMs *int    `json:"accountTakeStaggerMs,omitempty"`
	UserAgent            string  `json:"userAgent,omitempty"`
	UsedPercentThreshold float64 `json:"usedPercentThreshold,omitempty"`
	SampleSize           int     `json:"sampleSize,omitempty"`
	AutoActionMode       string  `json:"autoActionMode,omitempty"`
}

type ManagerWxaiInspectionScheduleConfig struct {
	Mode            string   `json:"mode,omitempty"`
	TimePoints      []string `json:"timePoints,omitempty"`
	IntervalMinutes int      `json:"intervalMinutes,omitempty"`
	TimeZone        string   `json:"timeZone,omitempty"`
}

type WxaiInspectionRun struct {
	ID                  int64                       `json:"id"`
	TriggerType         string                      `json:"triggerType"`
	TriggerKey          string                      `json:"triggerKey,omitempty"`
	Status              string                      `json:"status"`
	StartedAtMS         int64                       `json:"startedAtMs"`
	FinishedAtMS        int64                       `json:"finishedAtMs,omitempty"`
	TotalFiles          int                         `json:"totalFiles"`
	ProbeSetCount       int                         `json:"probeSetCount"`
	SampledCount        int                         `json:"sampledCount"`
	DisabledCount       int                         `json:"disabledCount"`
	EnabledCount        int                         `json:"enabledCount"`
	DeleteCount         int                         `json:"deleteCount"`
	DisableCount        int                         `json:"disableCount"`
	EnableCount         int                         `json:"enableCount"`
	ReauthCount         int                         `json:"reauthCount"`
	KeepCount           int                         `json:"keepCount"`
	QuotaExhaustedCount int                         `json:"quotaExhaustedCount"`
	AbnormalCount       int                         `json:"abnormalCount"`
	Error               string                      `json:"error,omitempty"`
	Settings            ManagerWxaiInspectionConfig `json:"settings"`
	SettingsJSON        string                      `json:"-"`
	CreatedAtMS         int64                       `json:"createdAtMs"`
	UpdatedAtMS         int64                       `json:"updatedAtMs"`
}

type WxaiInspectionQuotaWindow struct {
	ID          string   `json:"id"`
	LabelKey    string   `json:"labelKey"`
	UsedPercent *float64 `json:"usedPercent,omitempty"`
	ResetAtMS   int64    `json:"resetAtMs,omitempty"`
	ResetLabel  string   `json:"resetLabel,omitempty"`
}

type WxaiInspectionResult struct {
	ID                int64                       `json:"id"`
	RunID             int64                       `json:"runId"`
	AccountKey        string                      `json:"accountKey"`
	FileName          string                      `json:"fileName"`
	DisplayAccount    string                      `json:"displayAccount"`
	AuthIndex         string                      `json:"authIndex,omitempty"`
	AccountID         string                      `json:"accountId,omitempty"`
	Provider          string                      `json:"provider"`
	Disabled          bool                        `json:"disabled"`
	Status            string                      `json:"status,omitempty"`
	State             string                      `json:"state,omitempty"`
	Action            string                      `json:"action"`
	ActionReason      string                      `json:"actionReason"`
	ActionStatus      string                      `json:"actionStatus,omitempty"`
	ExecutedAction    string                      `json:"executedAction,omitempty"`
	ActionError       string                      `json:"actionError,omitempty"`
	StatusCode        *int                        `json:"statusCode,omitempty"`
	UsedPercent       *float64                    `json:"usedPercent,omitempty"`
	IsQuota           bool                        `json:"isQuota"`
	Error             string                      `json:"error,omitempty"`
	PlanType          string                      `json:"planType,omitempty"`
	QuotaWindows      []WxaiInspectionQuotaWindow `json:"quotaWindows,omitempty"`
	QuotaWindowsJSON  string                      `json:"-"`
	MonthlyLimitCents *float64                    `json:"monthlyLimitCents,omitempty"`
	MonthlyUsedCents  *float64                    `json:"monthlyUsedCents,omitempty"`
	ErrorKind         string                      `json:"errorKind,omitempty"`
	ErrorDetail       string                      `json:"errorDetail,omitempty"`
	WindowCosts       []WxaiAccountWindowCost     `json:"windowCosts,omitempty"`
	CreatedAtMS       int64                       `json:"createdAtMs"`
}

type WxaiInspectionLog struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"runId"`
	Level       string `json:"level"`
	Message     string `json:"message"`
	DetailJSON  string `json:"-"`
	Detail      any    `json:"detail,omitempty"`
	CreatedAtMS int64  `json:"createdAtMs"`
}

type WxaiAccountStatusDetail struct {
	RunID              int64    `json:"runId"`
	AccountKey         string   `json:"accountKey"`
	Priority           *int     `json:"priority,omitempty"`
	AccountType        string   `json:"accountType,omitempty"`
	WeeklyUsedPercent  *float64 `json:"weeklyUsedPercent,omitempty"`
	WeeklyResetAtMS    int64    `json:"weeklyResetAtMs,omitempty"`
	MonthlyUsedPercent *float64 `json:"monthlyUsedPercent,omitempty"`
	MonthlyResetAtMS   int64    `json:"monthlyResetAtMs,omitempty"`
	MonthlyLimitCents  *float64 `json:"monthlyLimitCents,omitempty"`
	MonthlyUsedCents   *float64 `json:"monthlyUsedCents,omitempty"`
	CheckedAtMS        int64    `json:"checkedAtMs,omitempty"`
	CreatedAtMS        int64    `json:"createdAtMs"`
	UpdatedAtMS        int64    `json:"updatedAtMs"`
}

type WxaiAccountStatusItem struct {
	WxaiInspectionResult
	ResultCreatedAtMS  int64    `json:"resultCreatedAtMs"`
	Priority           *int     `json:"priority,omitempty"`
	OriginalPriority   *int     `json:"originalPriority,omitempty"`
	RecoverAtMS        int64    `json:"recoverAtMs,omitempty"`
	AccountType        string   `json:"accountType,omitempty"`
	WeeklyUsedPercent  *float64 `json:"weeklyUsedPercent,omitempty"`
	WeeklyResetAtMS    int64    `json:"weeklyResetAtMs,omitempty"`
	MonthlyUsedPercent *float64 `json:"monthlyUsedPercent,omitempty"`
	MonthlyResetAtMS   int64    `json:"monthlyResetAtMs,omitempty"`
	CheckedAtMS        int64    `json:"checkedAtMs,omitempty"`
}

type WxaiAccountStatusResponse struct {
	Run   WxaiInspectionRun       `json:"run"`
	Items []WxaiAccountStatusItem `json:"items"`
}

const (
	DefaultWxaiWorkerStartStaggerMs = 10000
	DefaultWxaiAccountTakeStaggerMs = 10000
)

func DefaultWxaiInspectionConfig() ManagerWxaiInspectionConfig {
	return ManagerWxaiInspectionConfig{
		Enabled: wxaiBoolPointer(false),
		Schedule: ManagerWxaiInspectionScheduleConfig{
			Mode:            WxaiInspectionScheduleModeInterval,
			IntervalMinutes: 60,
		},
		TargetType:           "xai",
		Workers:              4,
		DeleteWorkers:        4,
		Timeout:              25000,
		Retries:              1,
		WorkerStartStaggerMs: wxaiIntPointer(DefaultWxaiWorkerStartStaggerMs),
		AccountTakeStaggerMs: wxaiIntPointer(DefaultWxaiAccountTakeStaggerMs),
		UserAgent:            "grok-shell/0.2.99 (linux; x86_64)",
		UsedPercentThreshold: 100,
		SampleSize:           0,
		AutoActionMode:       WxaiInspectionAutoActionNone,
	}
}

func NormalizeWxaiInspectionConfig(input ManagerWxaiInspectionConfig, fallback ManagerWxaiInspectionConfig) ManagerWxaiInspectionConfig {
	base := fallback
	if strings.TrimSpace(base.TargetType) == "" {
		base = DefaultWxaiInspectionConfig()
	}
	next := base
	if input.Enabled != nil {
		next.Enabled = wxaiBoolPointer(*input.Enabled)
	}
	next.Schedule = NormalizeWxaiInspectionSchedule(input.Schedule, base.Schedule)
	next.TargetType = "xai"
	next.Workers = wxaiPositiveOr(input.Workers, base.Workers)
	next.DeleteWorkers = wxaiPositiveOr(input.DeleteWorkers, wxaiPositiveOr(input.Workers, base.DeleteWorkers))
	next.Timeout = wxaiPositiveOr(input.Timeout, base.Timeout)
	next.Retries = 1
	next.WorkerStartStaggerMs = wxaiNonNegativeMsPointer(
		input.WorkerStartStaggerMs,
		base.WorkerStartStaggerMs,
		DefaultWxaiWorkerStartStaggerMs,
	)
	next.AccountTakeStaggerMs = wxaiNonNegativeMsPointer(
		input.AccountTakeStaggerMs,
		base.AccountTakeStaggerMs,
		DefaultWxaiAccountTakeStaggerMs,
	)
	next.UserAgent = wxaiValueOr(input.UserAgent, base.UserAgent)
	next.UsedPercentThreshold = normalizeWxaiPercent(input.UsedPercentThreshold, base.UsedPercentThreshold)
	if input.SampleSize >= 0 {
		next.SampleSize = input.SampleSize
	}
	next.AutoActionMode = NormalizeWxaiInspectionAutoActionMode(input.AutoActionMode, base.AutoActionMode)
	return next
}

func NormalizeWxaiInspectionSchedule(input ManagerWxaiInspectionScheduleConfig, fallback ManagerWxaiInspectionScheduleConfig) ManagerWxaiInspectionScheduleConfig {
	base := fallback
	if strings.TrimSpace(base.Mode) == "" {
		base = DefaultWxaiInspectionConfig().Schedule
	}
	next := base
	if timePoints := NormalizeWxaiInspectionTimePoints(input.TimePoints); len(timePoints) > 0 {
		next.TimePoints = timePoints
	}
	if input.IntervalMinutes > 0 {
		next.IntervalMinutes = input.IntervalMinutes
	}
	next.TimeZone = NormalizeWxaiInspectionTimeZone(input.TimeZone, fallback.TimeZone)
	switch strings.ToLower(strings.TrimSpace(input.Mode)) {
	case WxaiInspectionScheduleModeTimePoints:
		next.Mode = WxaiInspectionScheduleModeTimePoints
	case WxaiInspectionScheduleModeInterval:
		next.Mode = WxaiInspectionScheduleModeInterval
	case "":
		if len(NormalizeWxaiInspectionTimePoints(input.TimePoints)) > 0 {
			next.Mode = WxaiInspectionScheduleModeTimePoints
		} else if input.IntervalMinutes > 0 {
			next.Mode = WxaiInspectionScheduleModeInterval
		}
	}
	if next.Mode == WxaiInspectionScheduleModeTimePoints && len(next.TimePoints) == 0 {
		next.Mode = WxaiInspectionScheduleModeInterval
	}
	if next.Mode == WxaiInspectionScheduleModeInterval && next.IntervalMinutes <= 0 {
		next.IntervalMinutes = 60
	}
	return next
}

func NormalizeWxaiInspectionTimePoints(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized, ok := NormalizeWxaiInspectionTimePoint(value)
		if !ok {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result
}

func NormalizeWxaiInspectionTimePoint(value string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return "", false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return "", false
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", hour, minute), true
}

func NormalizeWxaiInspectionTimeZone(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return strings.TrimSpace(fallback)
	}
	if _, err := time.LoadLocation(trimmed); err != nil {
		return strings.TrimSpace(fallback)
	}
	return trimmed
}

func ValidateWxaiInspectionConfig(input ManagerWxaiInspectionConfig) error {
	return ValidateWxaiInspectionTimeZone(input.Schedule.TimeZone)
}

func ValidateWxaiInspectionTimeZone(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if _, err := time.LoadLocation(trimmed); err != nil {
		return fmt.Errorf("invalid time zone %q: %w", trimmed, err)
	}
	return nil
}

func ResolveWxaiInspectionLocation(timeZone string) *time.Location {
	trimmed := strings.TrimSpace(timeZone)
	if trimmed == "" {
		return time.Local
	}
	location, err := time.LoadLocation(trimmed)
	if err != nil {
		return time.Local
	}
	return location
}

func NormalizeWxaiInspectionAutoActionMode(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case WxaiInspectionAutoActionEnable:
		return WxaiInspectionAutoActionEnable
	case WxaiInspectionAutoActionDisable:
		return WxaiInspectionAutoActionDisable
	case WxaiInspectionAutoActionDelete:
		return WxaiInspectionAutoActionDelete
	case WxaiInspectionAutoActionNone:
		return WxaiInspectionAutoActionNone
	default:
		switch fallback {
		case WxaiInspectionAutoActionEnable, WxaiInspectionAutoActionDisable, WxaiInspectionAutoActionDelete:
			return fallback
		default:
			return WxaiInspectionAutoActionNone
		}
	}
}

func NormalizeWxaiInspectionActionStatus(value string, action string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case WxaiInspectionActionStatusPending,
		WxaiInspectionActionStatusSuccess,
		WxaiInspectionActionStatusFailed,
		WxaiInspectionActionStatusSkipped,
		WxaiInspectionActionStatusNeedsReview:
		return strings.ToLower(strings.TrimSpace(value))
	case WxaiInspectionActionStatusNone:
		return WxaiInspectionActionStatusNone
	}
	if strings.TrimSpace(action) == "" || strings.TrimSpace(action) == "keep" {
		return WxaiInspectionActionStatusNone
	}
	return WxaiInspectionActionStatusPending
}

func WxaiInspectionTriggerKey(now time.Time, config ManagerWxaiInspectionConfig) string {
	switch config.Schedule.Mode {
	case WxaiInspectionScheduleModeTimePoints:
		return now.In(ResolveWxaiInspectionLocation(config.Schedule.TimeZone)).Format("2006-01-02 15:04")
	case WxaiInspectionScheduleModeInterval:
		if config.Schedule.IntervalMinutes <= 0 {
			return now.Format("2006-01-02T15:04")
		}
		bucket := now.Unix() / int64(config.Schedule.IntervalMinutes*60)
		return fmt.Sprintf("interval:%d:%d", config.Schedule.IntervalMinutes, bucket)
	default:
		return ""
	}
}

func WxaiInspectionScheduleDue(now time.Time, lastRun time.Time, config ManagerWxaiInspectionConfig) bool {
	if config.Enabled == nil || !*config.Enabled {
		return false
	}
	switch config.Schedule.Mode {
	case WxaiInspectionScheduleModeTimePoints:
		currentTimePoint := now.In(ResolveWxaiInspectionLocation(config.Schedule.TimeZone)).Format("15:04")
		for _, timePoint := range config.Schedule.TimePoints {
			if timePoint == currentTimePoint {
				return true
			}
		}
		return false
	case WxaiInspectionScheduleModeInterval:
		if config.Schedule.IntervalMinutes <= 0 {
			return false
		}
		if lastRun.IsZero() {
			return true
		}
		return now.Sub(lastRun) >= time.Duration(config.Schedule.IntervalMinutes)*time.Minute
	default:
		return false
	}
}

func wxaiBoolPointer(value bool) *bool {
	return &value
}

func wxaiIntPointer(value int) *int {
	return &value
}

func wxaiPositiveOr(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// wxaiNonNegativeMsPointer 归一化错峰毫秒：input 非 nil 且 >=0 时采用 input；
// 否则采用 fallback；再否则采用 defaultMs。0 表示关闭错峰。
func wxaiNonNegativeMsPointer(input *int, fallback *int, defaultMs int) *int {
	if input != nil && *input >= 0 {
		return wxaiIntPointer(*input)
	}
	if fallback != nil && *fallback >= 0 {
		return wxaiIntPointer(*fallback)
	}
	if defaultMs < 0 {
		defaultMs = 0
	}
	return wxaiIntPointer(defaultMs)
}

// WxaiWorkerStartStagger 返回 worker 错峰启动间隔；nil/负按默认；0 表示不交错。
func WxaiWorkerStartStagger(settings ManagerWxaiInspectionConfig) time.Duration {
	return wxaiStaggerDuration(settings.WorkerStartStaggerMs, DefaultWxaiWorkerStartStaggerMs)
}

// WxaiAccountTakeStagger 返回全局取账号间隔；nil/负按默认；0 表示不限流。
func WxaiAccountTakeStagger(settings ManagerWxaiInspectionConfig) time.Duration {
	return wxaiStaggerDuration(settings.AccountTakeStaggerMs, DefaultWxaiAccountTakeStaggerMs)
}

func wxaiStaggerDuration(milliseconds *int, defaultMs int) time.Duration {
	if milliseconds == nil {
		if defaultMs < 0 {
			defaultMs = 0
		}
		return time.Duration(defaultMs) * time.Millisecond
	}
	if *milliseconds < 0 {
		if defaultMs < 0 {
			defaultMs = 0
		}
		return time.Duration(defaultMs) * time.Millisecond
	}
	return time.Duration(*milliseconds) * time.Millisecond
}

func wxaiValueOr(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func normalizeWxaiPercent(value float64, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	if value > 0 && value <= 1 {
		value *= 100
	}
	if value < 0 || value > 100 {
		return fallback
	}
	return value
}

func MarshalWxaiInspectionSettings(settings ManagerWxaiInspectionConfig) string {
	data, err := json.Marshal(settings)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func UnmarshalWxaiInspectionSettings(raw string) ManagerWxaiInspectionConfig {
	fallback := DefaultWxaiInspectionConfig()
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	var parsed ManagerWxaiInspectionConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return fallback
	}
	return NormalizeWxaiInspectionConfig(parsed, fallback)
}

func MarshalWxaiInspectionQuotaWindows(windows []WxaiInspectionQuotaWindow) string {
	data, err := json.Marshal(windows)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func UnmarshalWxaiInspectionQuotaWindows(raw string) []WxaiInspectionQuotaWindow {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var windows []WxaiInspectionQuotaWindow
	if err := json.Unmarshal([]byte(raw), &windows); err != nil {
		return nil
	}
	return windows
}
