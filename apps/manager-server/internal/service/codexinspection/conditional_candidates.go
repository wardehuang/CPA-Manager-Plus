package codexinspection

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const (
	conditionalReasonActiveRecent    = "active_recent"
	conditionalReasonQuotaResetDue   = "quota_reset_due"
	conditionalReasonUnauthorized401 = "unauthorized_401"
)

type conditionalAccountRef struct {
	AccountKey     string
	FileName       string
	DisplayAccount string
	AuthIndex      string
	AccountID      string
	Provider       string
}

type conditionalCandidateSet struct {
	accountsByKey map[string]account
	reasonsByKey  map[string][]string
}

func (s *Service) resolveConditionalAccounts(ctx context.Context, runID int64, accounts []account, nowMS int64, logger runLogger) ([]account, map[string][]string, error) {
	if runID <= 0 || len(accounts) == 0 || nowMS <= 0 {
		return nil, nil, nil
	}
	matcher := newConditionalAccountMatcher(accounts)
	candidates := &conditionalCandidateSet{
		accountsByKey: map[string]account{},
		reasonsByKey:  map[string][]string{},
	}

	active, err := s.store.ConditionalAccountsBetween(ctx, nowMS-int64(10*time.Minute/time.Millisecond), nowMS)
	if err != nil {
		return nil, nil, err
	}
	for _, item := range active {
		if item.Calls <= 0 {
			continue
		}
		matched, ok := matcher.match(conditionalRefFromUsage(item))
		if !ok {
			logger.warning(ctx, "条件巡检账号未匹配", map[string]any{
				"reason":          conditionalReasonActiveRecent,
				"fileName":        item.FileName,
				"accountSnapshot": item.AccountSnapshot,
				"authIndex":       item.AuthIndex,
				"provider":        item.AuthProviderSnapshot,
			})
			continue
		}
		candidates.add(matched, conditionalReasonActiveRecent)
		if item.UnauthorizedCalls > 0 {
			candidates.add(matched, conditionalReasonUnauthorized401)
		}
	}

	items, err := s.store.ListCodexAccountStatusItems(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	for _, item := range items {
		matched, ok := matcher.match(conditionalRefFromStatusItem(item))
		if !ok {
			continue
		}
		if isStatusItemResetDue(item, nowMS) {
			candidates.add(matched, conditionalReasonQuotaResetDue)
		}
		if item.StatusCode != nil && *item.StatusCode == 401 {
			candidates.add(matched, conditionalReasonUnauthorized401)
		}
	}

	dueAdjustments, err := s.store.ListDueCodexPriorityAdjustments(ctx, nowMS)
	if err != nil {
		return nil, nil, err
	}
	for _, item := range dueAdjustments {
		matched, ok := matcher.match(conditionalRefFromPriorityAdjustment(item))
		if !ok {
			logger.warning(ctx, "条件巡检账号未匹配", map[string]any{
				"reason":         conditionalReasonQuotaResetDue,
				"accountKey":     item.AccountKey,
				"fileName":       item.FileName,
				"displayAccount": item.DisplayAccount,
				"authIndex":      item.AuthIndex,
			})
			continue
		}
		if matched.Priority == nil || *matched.Priority != priorityPausedValue {
			deleteKey := firstNonEmpty(item.AccountKey, matched.Key)
			if err := s.store.DeleteCodexPriorityAdjustment(ctx, deleteKey); err != nil {
				logger.warning(ctx, "清理过期优先级调整记录失败", map[string]any{
					"accountKey":     deleteKey,
					"fileName":       matched.FileName,
					"displayAccount": matched.DisplayAccount,
					"authIndex":      matched.AuthIndex,
					"priority":       matched.Priority,
					"error":          err.Error(),
				})
			} else {
				logger.info(ctx, "清理过期优先级调整记录", map[string]any{
					"accountKey":     deleteKey,
					"fileName":       matched.FileName,
					"displayAccount": matched.DisplayAccount,
					"authIndex":      matched.AuthIndex,
					"priority":       matched.Priority,
				})
			}
			continue
		}
		candidates.add(matched, conditionalReasonQuotaResetDue)
	}

	return candidates.list(), candidates.reasonsByKey, nil
}

func (c *conditionalCandidateSet) add(item account, reason string) {
	key := strings.TrimSpace(item.Key)
	if key == "" || reason == "" {
		return
	}
	c.accountsByKey[key] = item
	for _, existing := range c.reasonsByKey[key] {
		if existing == reason {
			return
		}
	}
	c.reasonsByKey[key] = append(c.reasonsByKey[key], reason)
	sort.Strings(c.reasonsByKey[key])
}

func (c *conditionalCandidateSet) list() []account {
	items := make([]account, 0, len(c.accountsByKey))
	for _, item := range c.accountsByKey {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].FileName == items[j].FileName {
			return items[i].DisplayAccount < items[j].DisplayAccount
		}
		return items[i].FileName < items[j].FileName
	})
	return items
}

type conditionalAccountMatcher struct {
	byKey                 map[string]account
	byFileAuth            map[string]account
	byProviderAccountID   map[string]account
	byProviderAuthDisplay map[string]account
	uniqueByFile          map[string]account
	uniqueByAuthIndex     map[string]account
}

func newConditionalAccountMatcher(accounts []account) conditionalAccountMatcher {
	matcher := conditionalAccountMatcher{
		byKey:                 map[string]account{},
		byFileAuth:            map[string]account{},
		byProviderAccountID:   map[string]account{},
		byProviderAuthDisplay: map[string]account{},
		uniqueByFile:          map[string]account{},
		uniqueByAuthIndex:     map[string]account{},
	}
	fileCounts := map[string]int{}
	authIndexCounts := map[string]int{}
	for _, item := range accounts {
		if file := normalizeConditionalKey(item.FileName); file != "" {
			fileCounts[file]++
		}
		if authIndex := normalizeConditionalKey(item.AuthIndex); authIndex != "" {
			authIndexCounts[authIndex]++
		}
	}
	for _, item := range accounts {
		if key := strings.TrimSpace(item.Key); key != "" {
			matcher.byKey[key] = item
		}
		file := normalizeConditionalKey(item.FileName)
		authIndex := normalizeConditionalKey(item.AuthIndex)
		provider := normalizeConditionalKey(item.Provider)
		display := normalizeConditionalKey(item.DisplayAccount)
		accountID := strings.TrimSpace(item.AccountID)
		if file != "" && authIndex != "" {
			matcher.byFileAuth[file+"\x00"+authIndex] = item
		}
		if provider != "" && accountID != "" {
			matcher.byProviderAccountID[provider+"\x00"+accountID] = item
		}
		if provider != "" && authIndex != "" && display != "" {
			matcher.byProviderAuthDisplay[provider+"\x00"+authIndex+"\x00"+display] = item
		}
		if file != "" && fileCounts[file] == 1 {
			matcher.uniqueByFile[file] = item
		}
		if authIndex != "" && authIndexCounts[authIndex] == 1 {
			matcher.uniqueByAuthIndex[authIndex] = item
		}
	}
	return matcher
}

func (m conditionalAccountMatcher) match(ref conditionalAccountRef) (account, bool) {
	if key := strings.TrimSpace(ref.AccountKey); key != "" {
		if item, ok := m.byKey[key]; ok {
			return item, true
		}
	}
	file := normalizeConditionalKey(ref.FileName)
	authIndex := normalizeConditionalKey(ref.AuthIndex)
	provider := normalizeConditionalKey(ref.Provider)
	display := normalizeConditionalKey(ref.DisplayAccount)
	accountID := strings.TrimSpace(ref.AccountID)
	if file != "" && authIndex != "" {
		if item, ok := m.byFileAuth[file+"\x00"+authIndex]; ok {
			return item, true
		}
	}
	if provider != "" && accountID != "" {
		if item, ok := m.byProviderAccountID[provider+"\x00"+accountID]; ok {
			return item, true
		}
	}
	if provider != "" && authIndex != "" && display != "" {
		if item, ok := m.byProviderAuthDisplay[provider+"\x00"+authIndex+"\x00"+display]; ok {
			return item, true
		}
	}
	if file != "" {
		if item, ok := m.uniqueByFile[file]; ok {
			return item, true
		}
	}
	if authIndex != "" {
		if item, ok := m.uniqueByAuthIndex[authIndex]; ok {
			return item, true
		}
	}
	return account{}, false
}

func conditionalRefFromUsage(item store.ConditionalAccountStat) conditionalAccountRef {
	return conditionalAccountRef{
		FileName:       item.FileName,
		DisplayAccount: firstNonEmpty(item.AccountSnapshot, item.AuthLabelSnapshot),
		AuthIndex:      item.AuthIndex,
		Provider:       item.AuthProviderSnapshot,
	}
}

func conditionalRefFromStatusItem(item model.CodexAccountStatusItem) conditionalAccountRef {
	return conditionalAccountRef{
		AccountKey:     item.AccountKey,
		FileName:       item.FileName,
		DisplayAccount: item.DisplayAccount,
		AuthIndex:      item.AuthIndex,
		AccountID:      item.AccountID,
		Provider:       item.Provider,
	}
}

func conditionalRefFromPriorityAdjustment(item model.CodexPriorityAdjustment) conditionalAccountRef {
	return conditionalAccountRef{
		AccountKey:     item.AccountKey,
		FileName:       item.FileName,
		DisplayAccount: item.DisplayAccount,
		AuthIndex:      item.AuthIndex,
		AccountID:      item.AccountID,
	}
}

func isStatusItemResetDue(item model.CodexAccountStatusItem, nowMS int64) bool {
	return isResetAtDue(item.FiveHourResetAtMS, nowMS) ||
		isResetAtDue(item.WeeklyResetAtMS, nowMS) ||
		isResetAtDue(item.MonthlyResetAtMS, nowMS)
}

func isResetAtDue(resetAtMS int64, nowMS int64) bool {
	return resetAtMS > 0 && resetAtMS <= nowMS
}

func normalizeConditionalKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
