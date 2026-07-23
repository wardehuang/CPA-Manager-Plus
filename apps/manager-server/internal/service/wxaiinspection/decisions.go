package wxaiinspection

import (
	"fmt"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func validateWxaiPriorityOnlyMode(settings model.ManagerWxaiInspectionConfig) error {
	mode := strings.ToLower(strings.TrimSpace(settings.AutoActionMode))
	if mode != "" && mode != model.WxaiInspectionAutoActionNone {
		return fmt.Errorf("%w: %s", ErrWxaiAutoActionUnsupported, mode)
	}
	return nil
}

func applyWxaiPriorityError(result *model.WxaiInspectionResult, errorKind string, priorityErr error) {
	result.ErrorKind = errorKind
	result.Error = priorityErr.Error()
	result.ErrorDetail = truncate(priorityErr.Error(), maxStoredBodyText)
}

func countWxaiAbnormalResults(results []model.WxaiInspectionResult) int {
	count := 0
	for _, result := range results {
		if result.Error != "" || result.ErrorKind == "account_abnormal" || result.ErrorKind == "request_error" || result.ErrorKind == "missing_auth_index" {
			count++
		}
	}
	return count
}

func countWxaiQuotaResults(results []model.WxaiInspectionResult) int {
	count := 0
	for _, result := range results {
		if result.IsQuota {
			count++
		}
	}
	return count
}
