package codexaccountstatus

import (
	"errors"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	codexaccountstatussvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/codexaccountstatus"
)

type Handler struct {
	App *app.Context
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if !middleware.AuthorizePanel(w, r, h.App.AdminAuthService) {
		return
	}

	path := strings.Trim(strings.TrimRight(r.URL.Path, "/"), " ")
	if path != "/v0/management/codex-account-status/latest" {
		response.MethodNotAllowed(w)
		return
	}
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	result, err := h.App.CodexAccountStatusService.Latest(r.Context())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, codexaccountstatussvc.ErrNoCodexInspectionRun) {
			status = http.StatusNotFound
		}
		response.Error(w, status, err)
		return
	}
	response.JSON(w, http.StatusOK, result)
}
