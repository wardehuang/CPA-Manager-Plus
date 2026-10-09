package monitoring

import (
	"errors"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
)

func (h *Handler) rawEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}

	eventID := strings.TrimSpace(r.URL.Query().Get("id"))
	if eventID == "" {
		response.Error(w, http.StatusBadRequest, errors.New("event id is required"))
		return
	}

	result, found, err := h.App.MonitoringService.RawEvent(r.Context(), eventID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err)
		return
	}
	if !found {
		response.Error(w, http.StatusNotFound, errors.New("raw event not found"))
		return
	}
	response.JSON(w, http.StatusOK, result)
}
