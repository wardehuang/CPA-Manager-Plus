package wxaiinspection

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const wxaiConditionalReasonActiveRecent = "active_recent"

type wxaiConditionalAccountRef struct {
	AccountKey     string
	FileName       string
	DisplayAccount string
	AuthIndex      string
	AccountID      string
	Provider       string
}

type wxaiConditionalCandidateSet struct {
	accountsByKey map[string]account
	reasonsByKey  map[string][]string
}

func (service *Service) resolveConditionalAccounts(
	ctx context.Context,
	runID int64,
	accounts []account,
	nowMS int64,
	logger runLogger,
) ([]account, map[string][]string, error) {
	if runID <= 0 || len(accounts) == 0 || nowMS <= 0 {
		return nil, nil, nil
	}
	matcher := newWxaiConditionalAccountMatcher(accounts)
	candidates := &wxaiConditionalCandidateSet{
		accountsByKey: map[string]account{},
		reasonsByKey:  map[string][]string{},
	}

	activeAccounts, err := service.store.ConditionalAccountsBetween(ctx, nowMS-int64(10*time.Minute/time.Millisecond), nowMS)
	if err != nil {
		return nil, nil, err
	}
	for _, activeAccount := range activeAccounts {
		if activeAccount.Calls <= 0 || normalizeWxaiProvider(activeAccount.AuthProviderSnapshot) != "xai" {
			continue
		}
		matchedAccount, matched := matcher.match(wxaiConditionalRefFromUsage(activeAccount))
		if !matched {
			logger.warning(ctx, "wXAi 条件巡检账号未匹配", map[string]any{
				"reason":          wxaiConditionalReasonActiveRecent,
				"fileName":        activeAccount.FileName,
				"accountSnapshot": activeAccount.AccountSnapshot,
				"authIndex":       activeAccount.AuthIndex,
				"provider":        activeAccount.AuthProviderSnapshot,
			})
			continue
		}
		_, realtimeCooldownActive, err := service.resolveWxaiRealtimeDegradationCooldown(ctx, matchedAccount, time.UnixMilli(nowMS))
		if err != nil {
			return nil, nil, err
		}
		if realtimeCooldownActive {
			continue
		}
		candidates.add(matchedAccount, wxaiConditionalReasonActiveRecent)
	}

	return candidates.list(), candidates.reasonsByKey, nil
}

func (candidates *wxaiConditionalCandidateSet) add(currentAccount account, reason string) {
	if isWxaiInspectionExcluded(currentAccount) {
		return
	}
	accountKey := strings.TrimSpace(currentAccount.Key)
	if accountKey == "" || reason == "" {
		return
	}
	candidates.accountsByKey[accountKey] = currentAccount
	for _, existingReason := range candidates.reasonsByKey[accountKey] {
		if existingReason == reason {
			return
		}
	}
	candidates.reasonsByKey[accountKey] = append(candidates.reasonsByKey[accountKey], reason)
	sort.Strings(candidates.reasonsByKey[accountKey])
}

func (candidates *wxaiConditionalCandidateSet) list() []account {
	accounts := make([]account, 0, len(candidates.accountsByKey))
	for _, currentAccount := range candidates.accountsByKey {
		accounts = append(accounts, currentAccount)
	}
	sort.Slice(accounts, func(leftIndex int, rightIndex int) bool {
		if accounts[leftIndex].FileName == accounts[rightIndex].FileName {
			return accounts[leftIndex].DisplayAccount < accounts[rightIndex].DisplayAccount
		}
		return accounts[leftIndex].FileName < accounts[rightIndex].FileName
	})
	return accounts
}

type wxaiConditionalAccountMatcher struct {
	byKey               map[string]account
	byFileAuth          map[string]account
	byProviderAccountID map[string]account
	uniqueByFile        map[string]account
	uniqueByAuthIndex   map[string]account
}

func newWxaiConditionalAccountMatcher(accounts []account) wxaiConditionalAccountMatcher {
	matcher := wxaiConditionalAccountMatcher{
		byKey:               map[string]account{},
		byFileAuth:          map[string]account{},
		byProviderAccountID: map[string]account{},
		uniqueByFile:        map[string]account{},
		uniqueByAuthIndex:   map[string]account{},
	}
	fileCounts := map[string]int{}
	authIndexCounts := map[string]int{}
	for _, currentAccount := range accounts {
		if fileName := normalizeWxaiConditionalKey(currentAccount.FileName); fileName != "" {
			fileCounts[fileName]++
		}
		if authIndex := normalizeWxaiConditionalKey(currentAccount.AuthIndex); authIndex != "" {
			authIndexCounts[authIndex]++
		}
	}
	for _, currentAccount := range accounts {
		if accountKey := strings.TrimSpace(currentAccount.Key); accountKey != "" {
			matcher.byKey[accountKey] = currentAccount
		}
		fileName := normalizeWxaiConditionalKey(currentAccount.FileName)
		authIndex := normalizeWxaiConditionalKey(currentAccount.AuthIndex)
		accountID := strings.TrimSpace(currentAccount.AccountID)
		if fileName != "" && authIndex != "" {
			matcher.byFileAuth[fileName+"\x00"+authIndex] = currentAccount
		}
		if accountID != "" {
			matcher.byProviderAccountID["xai\x00"+accountID] = currentAccount
		}
		if fileName != "" && fileCounts[fileName] == 1 {
			matcher.uniqueByFile[fileName] = currentAccount
		}
		if authIndex != "" && authIndexCounts[authIndex] == 1 {
			matcher.uniqueByAuthIndex[authIndex] = currentAccount
		}
	}
	return matcher
}

func (matcher wxaiConditionalAccountMatcher) match(reference wxaiConditionalAccountRef) (account, bool) {
	if accountKey := strings.TrimSpace(reference.AccountKey); accountKey != "" {
		if currentAccount, exists := matcher.byKey[accountKey]; exists {
			return currentAccount, true
		}
	}
	fileName := normalizeWxaiConditionalKey(reference.FileName)
	authIndex := normalizeWxaiConditionalKey(reference.AuthIndex)
	provider := normalizeWxaiProvider(reference.Provider)
	accountID := strings.TrimSpace(reference.AccountID)
	if fileName != "" && authIndex != "" {
		if currentAccount, exists := matcher.byFileAuth[fileName+"\x00"+authIndex]; exists {
			return currentAccount, true
		}
	}
	if provider == "xai" && accountID != "" {
		if currentAccount, exists := matcher.byProviderAccountID[provider+"\x00"+accountID]; exists {
			return currentAccount, true
		}
	}
	if fileName != "" {
		if currentAccount, exists := matcher.uniqueByFile[fileName]; exists {
			return currentAccount, true
		}
	}
	if authIndex != "" {
		if currentAccount, exists := matcher.uniqueByAuthIndex[authIndex]; exists {
			return currentAccount, true
		}
	}
	return account{}, false
}

func wxaiConditionalRefFromUsage(item store.ConditionalAccountStat) wxaiConditionalAccountRef {
	return wxaiConditionalAccountRef{
		FileName:       item.FileName,
		DisplayAccount: firstNonEmpty(item.AccountSnapshot, item.AuthLabelSnapshot),
		AuthIndex:      item.AuthIndex,
		Provider:       item.AuthProviderSnapshot,
	}
}

func normalizeWxaiConditionalKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
