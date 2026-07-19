package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	appmw "github.com/practicum/gophprofile/internal/middleware"
	"github.com/stretchr/testify/require"
)

func TestRequireUserID_Missing(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := appmw.RequireUserID(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	require.NoError(t, h(c))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRequireUserID_OK(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-User-ID", "user-1")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := appmw.RequireUserID(func(c echo.Context) error {
		require.Equal(t, "user-1", c.Get("user_id"))
		return c.String(http.StatusOK, "ok")
	})
	require.NoError(t, h(c))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireUserID_TooLong(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-User-ID", string(make([]byte, 300)))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := appmw.RequireUserID(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	require.NoError(t, h(c))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRateLimiter(t *testing.T) {
	e := echo.New()
	rl := appmw.NewRateLimiter(1, 1)
	h := rl.Middleware(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	rec1 := httptest.NewRecorder()
	require.NoError(t, h(e.NewContext(req1, rec1)))
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	rec2 := httptest.NewRecorder()
	require.NoError(t, h(e.NewContext(req2, rec2)))
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
}
