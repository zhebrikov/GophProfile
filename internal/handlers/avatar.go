package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/practicum/gophprofile/internal/domain"
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/internal/services"
)

type AvatarHandler struct {
	avatars AvatarService
	health  HealthChecker
}

func NewAvatarHandler(avatars AvatarService, health HealthChecker) *AvatarHandler {
	return &AvatarHandler{avatars: avatars, health: health}
}

func (h *AvatarHandler) Upload(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "Missing file",
			Details: "multipart field 'file' is required",
		})
	}

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: "Cannot open file"})
	}
	defer func() { _ = src.Close() }()

	resp, err := h.avatars.Upload(c.Request().Context(), userID, file.Filename, src)
	if err != nil {
		return mapUploadError(c, err, h.avatars.MaxFileSize())
	}
	return c.JSON(http.StatusCreated, resp)
}

func (h *AvatarHandler) GetAvatar(c echo.Context) error {
	id := c.Param("avatar_id")
	size := c.QueryParam("size")
	format := c.QueryParam("format")

	data, contentType, etag, err := h.avatars.GetImage(c.Request().Context(), id, size, format)
	if err != nil {
		return mapGetImageError(c, err)
	}
	return writeImage(c, data, contentType, etag)
}

func (h *AvatarHandler) GetUserAvatar(c echo.Context) error {
	userID := c.Param("user_id")
	size := c.QueryParam("size")
	format := c.QueryParam("format")

	data, contentType, etag, err := h.avatars.GetUserAvatar(c.Request().Context(), userID, size, format)
	if err != nil {
		return mapGetImageError(c, err)
	}
	return writeImage(c, data, contentType, etag)
}

func (h *AvatarHandler) GetMetadata(c echo.Context) error {
	meta, err := h.avatars.GetMetadata(c.Request().Context(), c.Param("avatar_id"))
	if err != nil {
		return mapNotFound(c, err)
	}
	return c.JSON(http.StatusOK, meta)
}

func (h *AvatarHandler) ListUserAvatars(c echo.Context) error {
	list, err := h.avatars.ListByUser(c.Request().Context(), c.Param("user_id"))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, list)
}

func (h *AvatarHandler) DeleteAvatar(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	err := h.avatars.Delete(c.Request().Context(), c.Param("avatar_id"), userID)
	if err != nil {
		return mapDeleteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *AvatarHandler) DeleteUserAvatar(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	target := c.Param("user_id")
	err := h.avatars.DeleteUserAvatar(c.Request().Context(), target, userID)
	if err != nil {
		return mapDeleteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *AvatarHandler) Health(c echo.Context) error {
	resp := h.health.Check(c.Request().Context())
	code := http.StatusOK
	if resp.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	return c.JSON(code, resp)
}

func writeImage(c echo.Context, data []byte, contentType, etag string) error {
	if match := c.Request().Header.Get("If-None-Match"); match != "" && match == etag {
		return c.NoContent(http.StatusNotModified)
	}
	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Cache-Control", "max-age=86400")
	c.Response().Header().Set("ETag", etag)
	return c.Blob(http.StatusOK, contentType, data)
}

func mapUploadError(c echo.Context, err error, maxSize int64) error {
	switch {
	case errors.Is(err, services.ErrFileTooLarge):
		return c.JSON(http.StatusRequestEntityTooLarge, domain.ErrorResponse{
			Error:   "File too large",
			MaxSize: maxSize,
		})
	case errors.Is(err, services.ErrInvalidFormat):
		return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "Invalid file format",
			Details: "Supported formats: jpeg, png, webp",
		})
	case errors.Is(err, services.ErrInvalidUserID):
		return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "Invalid X-User-ID",
			Details: "X-User-ID header is required",
		})
	default:
		return internalError(c, err)
	}
}

func mapNotFound(c echo.Context, err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return c.JSON(http.StatusNotFound, domain.ErrorResponse{Error: "Avatar not found"})
	}
	return internalError(c, err)
}

func mapGetImageError(c echo.Context, err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return c.JSON(http.StatusNotFound, domain.ErrorResponse{Error: "Avatar not found"})
	}
	if errors.Is(err, services.ErrUnsupportedFormat) {
		return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Error:   "Unsupported output format",
			Details: "Supported formats: jpeg, png, webp",
		})
	}
	return internalError(c, err)
}

func mapDeleteError(c echo.Context, err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return c.JSON(http.StatusNotFound, domain.ErrorResponse{Error: "Avatar not found"})
	}
	if errors.Is(err, services.ErrForbidden) {
		return c.JSON(http.StatusForbidden, domain.ErrorResponse{
			Error:   "Forbidden",
			Details: "You can only delete your own avatars",
		})
	}
	return internalError(c, err)
}

func internalError(c echo.Context, err error) error {
	c.Logger().Error(err)
	return c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
		Error: http.StatusText(http.StatusInternalServerError),
	})
}

func UserIDFromContext(c echo.Context) string {
	if v, ok := c.Get("user_id").(string); ok {
		return v
	}
	return strings.TrimSpace(c.Request().Header.Get("X-User-ID"))
}
