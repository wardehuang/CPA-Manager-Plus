package automation

import (
	"errors"
	"net/http"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	automationsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/automation"
)

// Handler 暴露自动化能力的只读状态。它不提供写入接口，因此 worker 行为不会被改变。
type Handler struct {
	App     *app.Context
	service *automationsvc.Service
}

func New(appCtx *app.Context) *Handler {
	return &Handler{App: appCtx, service: automationsvc.New(appCtx.Config)}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	if !h.authorizeRead(w, r) {
		return
	}
	response.JSON(w, http.StatusOK, h.service.Status())
}

// authorizeRead 与 managerconfig handler 保持一致：
// 配置了管理 key 时需要面板凭据；未配置 setup/management key 时允许读取。
func (h *Handler) authorizeRead(w http.ResponseWriter, r *http.Request) bool {
	ok, err := h.App.AdminAuthService.VerifyPanelHeader(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err)
		return false
	}
	if ok {
		return true
	}
	setup, setupOK, err := h.App.ManagerConfigService.ResolveSetup(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err)
		return false
	}
	if !setupOK || setup.ManagementKey == "" {
		return true
	}
	response.Error(w, http.StatusUnauthorized, errors.New("invalid admin key"))
	return false
}
