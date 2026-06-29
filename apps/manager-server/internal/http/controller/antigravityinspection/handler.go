package antigravityinspection

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
	antigravitysvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/antigravityinspection"
)

type Handler struct {
	App *app.Context
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if !middleware.AuthorizePanel(w, r, h.App.AdminAuthService) {
		return
	}
	path := strings.Trim(strings.TrimRight(r.URL.Path, "/"), " ")
	switch {
	case path == "/v0/management/antigravity-inspection/settings":
		provider := r.URL.Query().Get("provider")
		if r.Method == http.MethodGet {
			result, err := h.App.AntigravityInspectionService.GetSettings(r.Context(), provider)
			if err != nil {
				response.Error(w, antigravityInspectionErrorStatus(err), err)
				return
			}
			response.JSON(w, http.StatusOK, result)
			return
		}
		if r.Method == http.MethodPut {
			var payload struct {
				Settings map[string]any `json:"settings"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				response.Error(w, http.StatusBadRequest, err)
				return
			}
			data, _ := json.Marshal(payload.Settings)
			var settings model.ManagerAntigravityInspectionConfig
			if err := json.Unmarshal(data, &settings); err != nil {
				response.Error(w, http.StatusBadRequest, err)
				return
			}
			result, err := h.App.AntigravityInspectionService.SaveSettings(r.Context(), provider, settings)
			if err != nil {
				response.Error(w, antigravityInspectionErrorStatus(err), err)
				return
			}
			response.JSON(w, http.StatusOK, result)
			return
		}
		response.MethodNotAllowed(w)
	case path == "/v0/management/antigravity-inspection/manual-refresh":
		if r.Method != http.MethodPost {
			response.MethodNotAllowed(w)
			return
		}
		var req antigravitysvc.ManualRefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		result, err := h.App.AntigravityInspectionService.RunManualRefresh(context.WithoutCancel(r.Context()), req)
		if err != nil {
			response.Error(w, antigravityInspectionErrorStatus(err), err)
			return
		}
		response.JSON(w, http.StatusOK, result)
	case path == "/v0/management/antigravity-inspection/run":
		if r.Method != http.MethodPost {
			response.MethodNotAllowed(w)
			return
		}
		var req antigravitysvc.RunRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.TriggerType == "" {
			req.TriggerType = "manual"
		}
		if req.TriggerKey == "" {
			req.TriggerKey = "manual"
		}
		result, err := h.App.AntigravityInspectionService.Run(context.WithoutCancel(r.Context()), req)
		if err != nil {
			response.Error(w, antigravityInspectionErrorStatus(err), err)
			return
		}
		response.JSON(w, http.StatusOK, result)
	case path == "/v0/management/antigravity-inspection/runs":
		if r.Method != http.MethodGet {
			response.MethodNotAllowed(w)
			return
		}
		limit := 20
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		runs, err := h.App.AntigravityInspectionService.ListRuns(r.Context(), limit)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err)
			return
		}
		response.JSON(w, http.StatusOK, map[string]any{"items": runs})
	default:
		if !strings.HasPrefix(path, "/v0/management/antigravity-inspection/runs/") {
			response.MethodNotAllowed(w)
			return
		}
		idRaw := strings.TrimPrefix(path, "/v0/management/antigravity-inspection/runs/")
		actionPath := false
		if strings.HasSuffix(idRaw, "/actions") {
			actionPath = true
			idRaw = strings.TrimSuffix(idRaw, "/actions")
		}
		id, err := strconv.ParseInt(idRaw, 10, 64)
		if err != nil || id <= 0 {
			if err == nil {
				err = errors.New("run id is required")
			}
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		if actionPath {
			if r.Method != http.MethodPost {
				response.MethodNotAllowed(w)
				return
			}
			var req antigravitysvc.ExecuteActionsRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				response.Error(w, http.StatusBadRequest, err)
				return
			}
			result, err := h.App.AntigravityInspectionService.ExecuteManualActions(r.Context(), id, req)
			if err != nil {
				response.Error(w, antigravityInspectionErrorStatus(err), err)
				return
			}
			response.JSON(w, http.StatusOK, result)
			return
		}
		if r.Method != http.MethodGet {
			response.MethodNotAllowed(w)
			return
		}
		detail, err := h.App.AntigravityInspectionService.GetRun(r.Context(), id)
		if err != nil {
			response.Error(w, antigravityInspectionErrorStatus(err), err)
			return
		}
		response.JSON(w, http.StatusOK, detail)
	}
}

func antigravityInspectionErrorStatus(err error) int {
	switch {
	case errors.Is(err, antigravitysvc.ErrRunNotFound), errors.Is(err, antigravitysvc.ErrManualRefreshAccountNotFound):
		return http.StatusNotFound
	case errors.Is(err, antigravitysvc.ErrRunAlreadyActive), errors.Is(err, antigravitysvc.ErrRunNotCompleted):
		return http.StatusConflict
	case errors.Is(err, antigravitysvc.ErrNotConfigured):
		return http.StatusPreconditionFailed
	case errors.Is(err, antigravitysvc.ErrActionIDsRequired), errors.Is(err, antigravitysvc.ErrNoActionableResults):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
