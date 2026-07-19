package middleware

import (
	"net/http"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/practicum/gophprofile/internal/domain"
	"golang.org/x/time/rate"
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

type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit
	b        int
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		r:        rate.Limit(rps),
		b:        burst,
	}
}

func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	lim, ok := rl.limiters[key]
	if !ok {
		lim = rate.NewLimiter(rl.r, rl.b)
		rl.limiters[key] = lim
	}
	return lim
}

func (rl *RateLimiter) Middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		key := c.RealIP()
		if !rl.getLimiter(key).Allow() {
			return c.JSON(http.StatusTooManyRequests, domain.ErrorResponse{
				Error: "Rate limit exceeded",
			})
		}
		return next(c)
	}
}
