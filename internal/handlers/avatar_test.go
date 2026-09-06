package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/practicum/gophprofile/internal/domain"
	"github.com/practicum/gophprofile/internal/handlers"
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/internal/services"
	"github.com/practicum/gophprofile/pkg/circuitbreaker"
	"github.com/stretchr/testify/require"
)

type mockAvatarService struct {
	uploadFn  func(ctx context.Context, userID, fileName string, reader io.Reader) (*domain.UploadResponse, error)
	getImgFn  func(ctx context.Context, avatarID, size, format string) ([]byte, string, string, error)
	getUserFn func(ctx context.Context, userID, size, format string) ([]byte, string, string, error)
	metaFn    func(ctx context.Context, avatarID string) (*domain.MetadataResponse, error)
	listFn    func(ctx context.Context, userID string) ([]*domain.MetadataResponse, error)
	delFn     func(ctx context.Context, avatarID, userID string) error
	delUserFn func(ctx context.Context, userID, requesterID string) error
	maxSize   int64
}

func (m *mockAvatarService) Upload(ctx context.Context, userID, fileName string, reader io.Reader) (*domain.UploadResponse, error) {
	return m.uploadFn(ctx, userID, fileName, reader)
}
func (m *mockAvatarService) GetImage(ctx context.Context, avatarID, size, format string) ([]byte, string, string, error) {
	return m.getImgFn(ctx, avatarID, size, format)
}
func (m *mockAvatarService) GetUserAvatar(ctx context.Context, userID, size, format string) ([]byte, string, string, error) {
	return m.getUserFn(ctx, userID, size, format)
}
func (m *mockAvatarService) GetMetadata(ctx context.Context, avatarID string) (*domain.MetadataResponse, error) {
	return m.metaFn(ctx, avatarID)
}
func (m *mockAvatarService) ListByUser(ctx context.Context, userID string) ([]*domain.MetadataResponse, error) {
	return m.listFn(ctx, userID)
}
func (m *mockAvatarService) Delete(ctx context.Context, avatarID, userID string) error {
	return m.delFn(ctx, avatarID, userID)
}
func (m *mockAvatarService) DeleteUserAvatar(ctx context.Context, userID, requesterID string) error {
	return m.delUserFn(ctx, userID, requesterID)
}
func (m *mockAvatarService) MaxFileSize() int64 { return m.maxSize }

type mockHealth struct {
	resp domain.HealthResponse
}

func (m *mockHealth) Check(context.Context) domain.HealthResponse { return m.resp }

func multipartBody(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

func TestUpload_Success(t *testing.T) {
	svc := &mockAvatarService{
		maxSize: 10 << 20,
		uploadFn: func(ctx context.Context, userID, fileName string, reader io.Reader) (*domain.UploadResponse, error) {
			require.Equal(t, "u1", userID)
			return &domain.UploadResponse{
				ID: "a1", UserID: userID, URL: "/api/v1/avatars/a1", Status: "processing", CreatedAt: time.Now().UTC(),
			}, nil
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	body, ct := multipartBody(t, "file", "a.jpg", []byte("img"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", body)
	req.Header.Set(echo.HeaderContentType, ct)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "u1")

	require.NoError(t, h.Upload(c))
	require.Equal(t, http.StatusCreated, rec.Code)
}

func TestUpload_TooLarge(t *testing.T) {
	svc := &mockAvatarService{
		maxSize: 1024,
		uploadFn: func(ctx context.Context, userID, fileName string, reader io.Reader) (*domain.UploadResponse, error) {
			return nil, services.ErrFileTooLarge
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	body, ct := multipartBody(t, "file", "a.jpg", []byte("img"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", body)
	req.Header.Set(echo.HeaderContentType, ct)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "u1")

	require.NoError(t, h.Upload(c))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestUpload_InvalidFormat(t *testing.T) {
	svc := &mockAvatarService{
		maxSize: 1024,
		uploadFn: func(ctx context.Context, userID, fileName string, reader io.Reader) (*domain.UploadResponse, error) {
			return nil, services.ErrInvalidFormat
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	body, ct := multipartBody(t, "file", "a.jpg", []byte("img"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", body)
	req.Header.Set(echo.HeaderContentType, ct)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "u1")

	require.NoError(t, h.Upload(c))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetAvatar_NotFound(t *testing.T) {
	svc := &mockAvatarService{
		getImgFn: func(ctx context.Context, avatarID, size, format string) ([]byte, string, string, error) {
			return nil, "", "", repository.ErrNotFound
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("avatar_id")
	c.SetParamValues("missing")

	require.NoError(t, h.GetAvatar(c))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetAvatar_UnsupportedFormat(t *testing.T) {
	svc := &mockAvatarService{
		getImgFn: func(ctx context.Context, avatarID, size, format string) ([]byte, string, string, error) {
			return nil, "", "", services.ErrUnsupportedFormat
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?format=gif", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("avatar_id")
	c.SetParamValues("a1")

	require.NoError(t, h.GetAvatar(c))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetAvatar_OK(t *testing.T) {
	svc := &mockAvatarService{
		getImgFn: func(ctx context.Context, avatarID, size, format string) ([]byte, string, string, error) {
			return []byte("img"), "image/jpeg", `"etag"`, nil
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("avatar_id")
	c.SetParamValues("a1")

	require.NoError(t, h.GetAvatar(c))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))
	require.Equal(t, `"etag"`, rec.Header().Get("ETag"))
}

func TestDelete_Forbidden(t *testing.T) {
	svc := &mockAvatarService{
		delFn: func(ctx context.Context, avatarID, userID string) error {
			return services.ErrForbidden
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "u1")
	c.SetParamNames("avatar_id")
	c.SetParamValues("a1")

	require.NoError(t, h.DeleteAvatar(c))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDelete_OK(t *testing.T) {
	svc := &mockAvatarService{
		delFn: func(ctx context.Context, avatarID, userID string) error { return nil },
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "u1")
	c.SetParamNames("avatar_id")
	c.SetParamValues("a1")

	require.NoError(t, h.DeleteAvatar(c))
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestGetMetadata(t *testing.T) {
	svc := &mockAvatarService{
		metaFn: func(ctx context.Context, avatarID string) (*domain.MetadataResponse, error) {
			return &domain.MetadataResponse{ID: avatarID, UserID: "u1", FileName: "a.jpg"}, nil
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("avatar_id")
	c.SetParamValues("a1")

	require.NoError(t, h.GetMetadata(c))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestListUserAvatars(t *testing.T) {
	svc := &mockAvatarService{
		listFn: func(ctx context.Context, userID string) ([]*domain.MetadataResponse, error) {
			return []*domain.MetadataResponse{{ID: "a1", UserID: userID}}, nil
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("user_id")
	c.SetParamValues("u1")

	require.NoError(t, h.ListUserAvatars(c))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestHealth(t *testing.T) {
	h := handlers.NewAvatarHandler(&mockAvatarService{}, &mockHealth{
		resp: domain.HealthResponse{Status: "ok", Components: map[string]string{"postgres": "ok"}},
	})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.Health(e.NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)

	h = handlers.NewAvatarHandler(&mockAvatarService{}, &mockHealth{
		resp: domain.HealthResponse{Status: "degraded"},
	})
	rec = httptest.NewRecorder()
	require.NoError(t, h.Health(e.NewContext(req, rec)))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestUpload_MissingFile(t *testing.T) {
	h := handlers.NewAvatarHandler(&mockAvatarService{maxSize: 10}, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "u1")
	require.NoError(t, h.Upload(c))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeleteUserAvatar_Error(t *testing.T) {
	svc := &mockAvatarService{
		delUserFn: func(ctx context.Context, userID, requesterID string) error {
			return errors.New("boom")
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "u1")
	c.SetParamNames("user_id")
	c.SetParamValues("u1")
	require.NoError(t, h.DeleteUserAvatar(c))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDeleteUserAvatar_CircuitOpen(t *testing.T) {
	svc := &mockAvatarService{
		delUserFn: func(ctx context.Context, userID, requesterID string) error {
			return circuitbreaker.ErrOpen
		},
	}
	h := handlers.NewAvatarHandler(svc, &mockHealth{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "u1")
	c.SetParamNames("user_id")
	c.SetParamValues("u1")
	require.NoError(t, h.DeleteUserAvatar(c))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
