package worker

import (
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func resolveWxaiScheduledTrigger(
	now time.Time,
	lastScheduledRunTime time.Time,
	config model.ManagerWxaiInspectionConfig,
) (string, bool) {
	if config.Schedule.Mode != model.WxaiInspectionScheduleModeTimePoints {
		triggerKey := model.WxaiInspectionTriggerKey(now, config)
		return triggerKey, triggerKey != "" && model.WxaiInspectionScheduleDue(now, lastScheduledRunTime, config)
	}

	latestScheduledTime, exists := resolveLatestWxaiTimePoint(now, config.Schedule)
	if !exists {
		return "", false
	}

	location := model.ResolveWxaiInspectionLocation(config.Schedule.TimeZone)
	return latestScheduledTime.In(location).Format("2006-01-02 15:04"), true
}

func resolveLatestWxaiTimePoint(
	now time.Time,
	schedule model.ManagerWxaiInspectionScheduleConfig,
) (time.Time, bool) {
	location := model.ResolveWxaiInspectionLocation(schedule.TimeZone)
	localNow := now.In(location)
	var latestScheduledTime time.Time

	for _, configuredTimePoint := range schedule.TimePoints {
		normalizedTimePoint, valid := model.NormalizeWxaiInspectionTimePoint(configuredTimePoint)
		if !valid {
			continue
		}
		parsedTimePoint, err := time.Parse("15:04", normalizedTimePoint)
		if err != nil {
			continue
		}
		candidateTime := time.Date(
			localNow.Year(),
			localNow.Month(),
			localNow.Day(),
			parsedTimePoint.Hour(),
			parsedTimePoint.Minute(),
			0,
			0,
			location,
		)
		if candidateTime.After(localNow) {
			candidateTime = candidateTime.AddDate(0, 0, -1)
		}
		if latestScheduledTime.IsZero() || candidateTime.After(latestScheduledTime) {
			latestScheduledTime = candidateTime
		}
	}

	return latestScheduledTime, !latestScheduledTime.IsZero()
}
