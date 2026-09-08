package handlers

import (
	"errors"
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/labstack/echo/v4"
	"github.com/practicum/gophprofile/internal/observability"
	"github.com/practicum/gophprofile/internal/services"
	"github.com/practicum/gophprofile/internal/circuitbreaker"
)

type WebHandler struct {
	avatars AvatarService
	webDir  string
	tmpl    *template.Template
}

func NewWebHandler(avatars AvatarService, webDir string) (*WebHandler, error) {
	tmpl, err := template.ParseGlob(filepath.Join(webDir, "*.html"))
	if err != nil {
		return nil, err
	}
	return &WebHandler{avatars: avatars, webDir: webDir, tmpl: tmpl}, nil
}

func (h *WebHandler) UploadForm(c echo.Context) error {
	return h.tmpl.ExecuteTemplate(c.Response(), "upload.html", map[string]any{
		"Title": "Upload Avatar",
	})
}

func (h *WebHandler) UploadSubmit(c echo.Context) error {
	userID := c.FormValue("user_id")
	if userID == "" {
		userID = c.Request().Header.Get("X-User-ID")
	}
	file, err := c.FormFile("file")
	if err != nil {
		return h.renderUploadError(c, "File is required")
	}
	src, err := file.Open()
	if err != nil {
		return h.renderUploadError(c, "Cannot open file")
	}
	defer func() { _ = src.Close() }()

	resp, err := h.avatars.Upload(c.Request().Context(), userID, file.Filename, src)
	if err != nil {
		return h.mapWebUploadError(c, err)
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "upload.html", map[string]any{
		"Title":   "Upload Avatar",
		"Success": true,
		"Avatar":  resp,
		"UserID":  userID,
	})
}

func (h *WebHandler) Gallery(c echo.Context) error {
	userID := c.Param("user_id")
	list, err := h.avatars.ListByUser(c.Request().Context(), userID)
	if err != nil {
		return internalError(c, err)
	}
	return h.tmpl.ExecuteTemplate(c.Response(), "gallery.html", map[string]any{
		"Title":   "Gallery",
		"UserID":  userID,
		"Avatars": list,
	})
}

func (h *WebHandler) mapWebUploadError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, services.ErrFileTooLarge):
		return h.renderUploadError(c, "File too large")
	case errors.Is(err, services.ErrInvalidFormat):
		return h.renderUploadError(c, "Invalid file format")
	case errors.Is(err, services.ErrInvalidUserID):
		return h.renderUploadError(c, "Invalid user ID")
	case errors.Is(err, circuitbreaker.ErrOpen):
		observability.LoggerFromContext(c.Request().Context()).Warn("web upload circuit open", "error", err)
		c.Response().WriteHeader(http.StatusServiceUnavailable)
		return h.tmpl.ExecuteTemplate(c.Response(), "upload.html", map[string]any{
			"Title": "Upload Avatar",
			"Error": http.StatusText(http.StatusServiceUnavailable),
		})
	default:
		observability.LoggerFromContext(c.Request().Context()).Error("web upload failed", "error", err)
		c.Response().WriteHeader(http.StatusInternalServerError)
		return h.tmpl.ExecuteTemplate(c.Response(), "upload.html", map[string]any{
			"Title": "Upload Avatar",
			"Error": http.StatusText(http.StatusInternalServerError),
		})
	}
}

func (h *WebHandler) renderUploadError(c echo.Context, msg string) error {
	c.Response().WriteHeader(http.StatusBadRequest)
	return h.tmpl.ExecuteTemplate(c.Response(), "upload.html", map[string]any{
		"Title": "Upload Avatar",
		"Error": msg,
	})
}
