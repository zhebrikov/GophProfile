package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/practicum/gophprofile/internal/domain"
)

func RequireUserID(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID := strings.TrimSpace(c.Request().Header.Get("X-User-ID"))
		if userID == "" {
			return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
				Error:   "Missing X-User-ID",
				Details: "X-User-ID header is required",
			})
		}
		if len(userID) > 255 {
			return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
				Error:   "Invalid X-User-ID",
				Details: "User ID must be at most 255 characters",
			})
		}
		c.Set("user_id", userID)
		return next(c)
	}
}

// RateLimiter rejects requests that exceed the configured per-key rate.
type RateLimiter struct {
	store LimiterStore
}

// NewRateLimiter creates a rate limiter with an in-memory TTL store.
// Inactive keys are evicted after defaultLimiterTTL.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return NewRateLimiterWithStore(NewMemoryLimiterStore(
		rps,
		burst,
		defaultLimiterTTL,
		defaultLimiterCleanupInterval,
	))
}

// NewRateLimiterWithStore creates a rate limiter backed by the given store.
// Use this to swap the in-memory implementation for Redis or another backend.
func NewRateLimiterWithStore(store LimiterStore) *RateLimiter {
	return &RateLimiter{store: store}
}

func (rl *RateLimiter) Middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !rl.store.Allow(c.RealIP()) {
			return c.JSON(http.StatusTooManyRequests, domain.ErrorResponse{
				Error: "Rate limit exceeded",
			})
		}
		return next(c)
	}
}
