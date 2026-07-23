package wxaiinspection

import (
	"context"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

const (
	wxaiAccountTypeFree  = "FREE"
	wxaiAccountTypeSuper = "SUPER"
)

func (service *Service) attachWxaiAccountProfiles(ctx context.Context, accounts []account) error {
	profiles, err := service.store.ListWxaiAccountProfiles(ctx)
	if err != nil {
		return err
	}
	accountTypeByKey := make(map[string]string, len(profiles))
	for _, profile := range profiles {
		accountTypeByKey[profile.AccountKey] = normalizeWxaiAccountType(profile.AccountType)
	}
	for accountIndex := range accounts {
		accounts[accountIndex].AccountType = accountTypeByKey[accounts[accountIndex].Key]
	}
	return nil
}

func (service *Service) persistWxaiAccountType(ctx context.Context, accountKey string, accountType string) error {
	normalizedAccountType := normalizeWxaiAccountType(accountType)
	if normalizedAccountType == "" {
		return nil
	}
	return service.store.UpsertWxaiAccountProfile(context.WithoutCancel(ctx), model.WxaiAccountProfile{
		AccountKey:  accountKey,
		AccountType: normalizedAccountType,
	})
}

func normalizeWxaiAccountType(accountType string) string {
	switch strings.ToUpper(strings.TrimSpace(accountType)) {
	case wxaiAccountTypeFree:
		return wxaiAccountTypeFree
	case wxaiAccountTypeSuper:
		return wxaiAccountTypeSuper
	default:
		return ""
	}
}
