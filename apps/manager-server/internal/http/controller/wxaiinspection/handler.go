package wxaiinspection

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	wxaiinspectionsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/wxaiinspection"
)

type Handler struct {
	App *app.Context
}

func (handler *Handler) Handle(responseWriter http.ResponseWriter, request *http.Request) {
	if !middleware.AuthorizePanel(responseWriter, request, handler.App.AdminAuthService) {
		return
	}
	path := strings.TrimSpace(strings.TrimRight(request.URL.Path, "/"))
	switch {
	case path == "/v0/management/wxai-inspection/latest":
		if request.Method != http.MethodGet {
			response.MethodNotAllowed(responseWriter)
			return
		}
		result, err := handler.App.WxaiInspectionService.Latest(request.Context())
		if err != nil {
			response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, result)
	case path == "/v0/management/wxai-inspection/scheduled/latest-completed":
		if request.Method != http.MethodGet {
			response.MethodNotAllowed(responseWriter)
			return
		}
		result, err := handler.App.WxaiInspectionService.LatestCompletedScheduledRun(request.Context())
		if err != nil {
			response.Error(responseWriter, http.StatusInternalServerError, err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, result)
	case path == "/v0/management/wxai-inspection/realtime-degradation":
		if request.Method != http.MethodPost {
			response.MethodNotAllowed(responseWriter)
			return
		}
		var payload wxaiinspectionsvc.RealtimeDegradationRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			response.Error(responseWriter, http.StatusBadRequest, err)
			return
		}
		if err := handler.App.WxaiInspectionService.RecordRealtimeDegradation(request.Context(), payload); err != nil {
			response.Error(responseWriter, http.StatusBadRequest, err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, map[string]any{"recorded": true})
	case path == "/v0/management/wxai-inspection/settings":
		handler.handleSettings(responseWriter, request)
	case path == "/v0/management/wxai-inspection/manual-refresh":
		if request.Method != http.MethodPost {
			response.MethodNotAllowed(responseWriter)
			return
		}
		var payload wxaiinspectionsvc.ManualRefreshRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			response.Error(responseWriter, http.StatusBadRequest, err)
			return
		}
		result, err := handler.App.WxaiInspectionService.RunManualRefresh(context.WithoutCancel(request.Context()), payload)
		if err != nil {
			response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, result)
	case path == "/v0/management/wxai-inspection/tool-call-check":
		if request.Method != http.MethodPost {
			response.MethodNotAllowed(responseWriter)
			return
		}
		var payload wxaiinspectionsvc.ToolCallCheckRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			response.Error(responseWriter, http.StatusBadRequest, err)
			return
		}
		result, err := handler.App.WxaiInspectionService.RunToolCallCheck(request.Context(), payload)
		if err != nil {
			response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, result)
	case path == "/v0/management/wxai-inspection/grok2api-sync":
		if request.Method != http.MethodPost {
			response.MethodNotAllowed(responseWriter)
			return
		}
		result, err := handler.App.WxaiInspectionService.TriggerGrok2apiSync(context.WithoutCancel(request.Context()))
		if err != nil {
			response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, result)
	case path == "/v0/management/wxai-inspection/grok2api-test":
		if request.Method != http.MethodPost {
			response.MethodNotAllowed(responseWriter)
			return
		}
		var payload wxaiinspectionsvc.Grok2apiTestRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			response.Error(responseWriter, http.StatusBadRequest, err)
			return
		}
		if err := handler.App.WxaiInspectionService.TestGrok2apiConnection(request.Context(), payload); err != nil {
			response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, map[string]any{"ok": true})
	case path == "/v0/management/wxai-inspection/run":
		if request.Method != http.MethodPost {
			response.MethodNotAllowed(responseWriter)
			return
		}
		var payload wxaiinspectionsvc.RunRequest
		_ = json.NewDecoder(request.Body).Decode(&payload)
		result, err := handler.App.WxaiInspectionService.Run(context.WithoutCancel(request.Context()), payload)
		if err != nil {
			response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, result)
	case path == "/v0/management/wxai-inspection/runs":
		if request.Method != http.MethodGet {
			response.MethodNotAllowed(responseWriter)
			return
		}
		limit := 20
		if rawLimit := strings.TrimSpace(request.URL.Query().Get("limit")); rawLimit != "" {
			if parsedLimit, err := strconv.Atoi(rawLimit); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}
		runs, err := handler.App.WxaiInspectionService.ListRuns(request.Context(), limit)
		if err != nil {
			response.Error(responseWriter, http.StatusInternalServerError, err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, map[string]any{"items": runs})
	default:
		if !strings.HasPrefix(path, "/v0/management/wxai-inspection/runs/") {
			response.MethodNotAllowed(responseWriter)
			return
		}
		runPath := strings.TrimPrefix(path, "/v0/management/wxai-inspection/runs/")
		actionPath := strings.HasSuffix(runPath, "/actions")
		if actionPath {
			runPath = strings.TrimSuffix(runPath, "/actions")
		}
		runID, err := strconv.ParseInt(runPath, 10, 64)
		if err != nil || runID <= 0 {
			response.Error(responseWriter, http.StatusBadRequest, errors.New("run id is required"))
			return
		}
		if actionPath {
			if request.Method != http.MethodPost {
				response.MethodNotAllowed(responseWriter)
				return
			}
			var payload wxaiinspectionsvc.ExecuteActionsRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				response.Error(responseWriter, http.StatusBadRequest, err)
				return
			}
			result, err := handler.App.WxaiInspectionService.ExecuteManualActions(request.Context(), runID, payload)
			if err != nil {
				response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
				return
			}
			response.JSON(responseWriter, http.StatusOK, result)
			return
		}
		if request.Method != http.MethodGet {
			response.MethodNotAllowed(responseWriter)
			return
		}
		result, err := handler.App.WxaiInspectionService.GetRun(request.Context(), runID)
		if err != nil {
			response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, result)
	}
}

func (handler *Handler) handleSettings(responseWriter http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		result, err := handler.App.WxaiInspectionService.GetSettings(request.Context())
		if err != nil {
			response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, result)
		return
	}
	if request.Method == http.MethodPut {
		var payload struct {
			Settings model.ManagerWxaiInspectionConfig `json:"settings"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			response.Error(responseWriter, http.StatusBadRequest, err)
			return
		}
		result, err := handler.App.WxaiInspectionService.SaveSettings(request.Context(), payload.Settings)
		if err != nil {
			response.Error(responseWriter, wxaiInspectionErrorStatus(err), err)
			return
		}
		response.JSON(responseWriter, http.StatusOK, result)
		return
	}
	response.MethodNotAllowed(responseWriter)
}

func wxaiInspectionErrorStatus(err error) int {
	switch {
	case errors.Is(err, wxaiinspectionsvc.ErrRunNotFound),
		errors.Is(err, wxaiinspectionsvc.ErrManualRefreshAccountNotFound),
		errors.Is(err, wxaiinspectionsvc.ErrWxaiToolCallCheckAccountNotFound):
		return http.StatusNotFound
	case errors.Is(err, wxaiinspectionsvc.ErrRunAlreadyActive), errors.Is(err, wxaiinspectionsvc.ErrRunNotCompleted):
		return http.StatusConflict
	case errors.Is(err, wxaiinspectionsvc.ErrActionIDsRequired),
		errors.Is(err, wxaiinspectionsvc.ErrNoActionableResults),
		errors.Is(err, wxaiinspectionsvc.ErrManualRefreshRequiresServerRun):
		return http.StatusBadRequest
	case errors.Is(err, wxaiinspectionsvc.ErrNotConfigured),
		errors.Is(err, wxaiinspectionsvc.ErrGrok2apiNotConfigured):
		return http.StatusPreconditionFailed
	case errors.Is(err, wxaiinspectionsvc.ErrGrok2apiInvalidCredentials),
		errors.Is(err, wxaiinspectionsvc.ErrGrok2apiUnauthorized),
		errors.Is(err, wxaiinspectionsvc.ErrGrok2apiLoginRateLimited),
		errors.Is(err, wxaiinspectionsvc.ErrGrok2apiNoHealthyAccounts):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
