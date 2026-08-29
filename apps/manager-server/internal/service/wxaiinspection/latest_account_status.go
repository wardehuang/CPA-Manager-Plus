package wxaiinspection

import (
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

// collapseWxaiLatestAccountStatusItems keeps one row per fileName+authIndex.
// Historical realtime-guard rows used a non-canonical account_key in the same
// scheduled run, which inflated 总账号 and hid later priority updates.
func collapseWxaiLatestAccountStatusItems(items []model.WxaiAccountStatusItem) []model.WxaiAccountStatusItem {
	if len(items) <= 1 {
		return items
	}

	type accountStatusGroup struct {
		latest       model.WxaiAccountStatusItem
		canonical    model.WxaiAccountStatusItem
		hasCanonical bool
		details      model.WxaiAccountStatusItem
		hasDetails   bool
	}

	groups := make(map[string]*accountStatusGroup, len(items))
	groupOrder := make([]string, 0, len(items))
	for _, item := range items {
		groupKey := wxaiAccountStatusCollapseKey(item)
		group, exists := groups[groupKey]
		if !exists {
			group = &accountStatusGroup{latest: item}
			groups[groupKey] = group
			groupOrder = append(groupOrder, groupKey)
		} else if wxaiAccountStatusItemIsNewer(item, group.latest) {
			group.latest = item
		}
		if wxaiAccountStatusItemHasCanonicalKey(item) &&
			(!group.hasCanonical || wxaiAccountStatusItemIsNewer(item, group.canonical)) {
			group.canonical = item
			group.hasCanonical = true
		}
		if item.Priority != nil && (!group.hasDetails || wxaiAccountStatusItemIsNewer(item, group.details)) {
			group.details = item
			group.hasDetails = true
		}
	}

	collapsed := make([]model.WxaiAccountStatusItem, 0, len(groupOrder))
	for _, groupKey := range groupOrder {
		group := groups[groupKey]
		selected := group.latest
		if group.hasCanonical {
			selected.AccountKey = group.canonical.AccountKey
			if strings.TrimSpace(selected.AccountID) == "" {
				selected.AccountID = group.canonical.AccountID
			}
		}
		if group.hasDetails {
			fillWxaiAccountStatusDetailsFromDonor(&selected, group.details)
		}
		collapsed = append(collapsed, selected)
	}
	return collapsed
}

func wxaiAccountStatusCollapseKey(item model.WxaiAccountStatusItem) string {
	return strings.TrimSpace(item.FileName) + "\x00" + strings.TrimSpace(item.AuthIndex)
}

func wxaiAccountStatusItemHasCanonicalKey(item model.WxaiAccountStatusItem) bool {
	fileName := strings.TrimSpace(item.FileName)
	return fileName != "" && strings.HasPrefix(item.AccountKey, fileName+"|")
}

func wxaiAccountStatusItemIsNewer(item model.WxaiAccountStatusItem, current model.WxaiAccountStatusItem) bool {
	if item.ResultCreatedAtMS != current.ResultCreatedAtMS {
		return item.ResultCreatedAtMS > current.ResultCreatedAtMS
	}
	return item.ID > current.ID
}

func fillWxaiAccountStatusDetailsFromDonor(selected *model.WxaiAccountStatusItem, donor model.WxaiAccountStatusItem) {
	if selected.Priority == nil {
		selected.Priority = donor.Priority
	}
	if selected.ScheduleGroup == nil {
		selected.ScheduleGroup = donor.ScheduleGroup
	}
	if strings.TrimSpace(selected.AccountType) == "" {
		selected.AccountType = donor.AccountType
	}
	if selected.WeeklyUsedPercent == nil {
		selected.WeeklyUsedPercent = donor.WeeklyUsedPercent
	}
	if selected.WeeklyResetAtMS == 0 {
		selected.WeeklyResetAtMS = donor.WeeklyResetAtMS
	}
	if selected.MonthlyUsedPercent == nil {
		selected.MonthlyUsedPercent = donor.MonthlyUsedPercent
	}
	if selected.MonthlyResetAtMS == 0 {
		selected.MonthlyResetAtMS = donor.MonthlyResetAtMS
	}
	if selected.MonthlyLimitCents == nil {
		selected.MonthlyLimitCents = donor.MonthlyLimitCents
	}
	if selected.MonthlyUsedCents == nil {
		selected.MonthlyUsedCents = donor.MonthlyUsedCents
	}
	if selected.CheckedAtMS == 0 {
		selected.CheckedAtMS = donor.CheckedAtMS
	}
}
