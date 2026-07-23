package wxaiinspection

import (
	"context"
	"errors"
)

var (
	ErrRunNotCompleted     = errors.New("wxai inspection run is not completed")
	ErrActionIDsRequired   = errors.New("wxai inspection action result ids are required")
	ErrNoActionableResults = errors.New("wxai inspection has no actionable results")
)

type ExecuteActionsRequest struct {
	ResultIDs []int64 `json:"resultIds"`
}

type ActionOutcome struct {
	ResultID       int64  `json:"resultId,omitempty"`
	AccountKey     string `json:"accountKey,omitempty"`
	FileName       string `json:"fileName"`
	DisplayAccount string `json:"displayAccount"`
	Action         string `json:"action"`
	Status         string `json:"status"`
	Success        bool   `json:"success"`
	Error          string `json:"error,omitempty"`
}

type ExecuteActionsResult struct {
	Outcomes []ActionOutcome `json:"outcomes"`
	Detail   RunDetail       `json:"detail"`
}

func (service *Service) ExecuteManualActions(_ context.Context, _ int64, request ExecuteActionsRequest) (ExecuteActionsResult, error) {
	if len(request.ResultIDs) == 0 {
		return ExecuteActionsResult{}, ErrActionIDsRequired
	}
	return ExecuteActionsResult{}, ErrWxaiAutoActionUnsupported
}
