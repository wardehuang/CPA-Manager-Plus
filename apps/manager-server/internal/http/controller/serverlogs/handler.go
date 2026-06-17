package serverlogs

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
)

const (
	defaultServerLogSize = 12
	maxServerLogSize     = 200
)

type Handler struct {
	App *app.Context
}

type File struct {
	Name string `json:"name"`
	Time string `json:"time"`
	Size int64  `json:"size"`
}

type listItem struct {
	File
	modTime time.Time
}

type ListResponse struct {
	Files      []File `json:"files"`
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
	Total      int    `json:"total"`
	TotalPages int    `json:"total_pages"`
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if !middleware.AuthorizePanel(w, r, h.App.AdminAuthService) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	switch path {
	case "/v0/management/server-logs":
		h.list(w, r)
	case "/v0/management/server-logs/download":
		h.download(w, r)
	default:
		response.MethodNotAllowed(w)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}

	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), defaultServerLogSize)
	if pageSize > maxServerLogSize {
		pageSize = maxServerLogSize
	}

	logDir := h.logDir()
	if strings.TrimSpace(logDir) == "" {
		response.Error(w, http.StatusInternalServerError, errors.New("server log directory is not configured"))
		return
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			response.JSON(w, http.StatusOK, ListResponse{
				Files:      []File{},
				Page:       page,
				PageSize:   pageSize,
				Total:      0,
				TotalPages: 1,
			})
			return
		}
		response.Error(w, http.StatusInternalServerError, errors.New("unable to read server log directory"))
		return
	}

	items := make([]listItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		modTime := info.ModTime()
		items = append(items, listItem{
			File: File{
				Name: entry.Name(),
				Time: modTime.Format(time.RFC3339),
				Size: info.Size(),
			},
			modTime: modTime,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.After(items[j].modTime)
	})

	total := len(items)
	totalPages := 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	files := make([]File, 0, end-start)
	for _, item := range items[start:end] {
		files = append(files, item.File)
	}

	response.JSON(w, http.StatusOK, ListResponse{
		Files:      files,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	})
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		response.Error(w, http.StatusBadRequest, errors.New("file name is required"))
		return
	}
	if !isSafeFileName(name) {
		response.Error(w, http.StatusBadRequest, errors.New("invalid file name"))
		return
	}

	logDir := h.logDir()
	if strings.TrimSpace(logDir) == "" {
		response.Error(w, http.StatusInternalServerError, errors.New("server log directory is not configured"))
		return
	}

	http.ServeFile(w, r, filepath.Join(logDir, name))
}

func (h *Handler) logDir() string {
	if h == nil || h.App == nil {
		return ""
	}
	return h.App.Config.ServerLogDir
}

func parsePositiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func isSafeFileName(name string) bool {
	return filepath.Base(name) == name && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}
