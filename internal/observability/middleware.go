package observability

import (
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const RequestIDHeader = "X-Request-ID"

// RequestIDMiddleware assigns/propagates a request id and logs request completion.
func RequestIDMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		reqID := c.Request().Header.Get(RequestIDHeader)
		if reqID == "" {
			reqID = uuid.New().String()
		}
		c.Response().Header().Set(RequestIDHeader, reqID)

		ctx := WithRequestID(c.Request().Context(), reqID)
		c.SetRequest(c.Request().WithContext(ctx))

		start := time.Now()
		err := next(c)

		status := c.Response().Status
		if status == 0 {
			status = 200
		}
		LoggerFromContext(c.Request().Context()).Info("http_request",
			"method", c.Request().Method,
			"path", c.Path(),
			"uri", c.Request().RequestURI,
			"status", status,
			"latency_ms", time.Since(start).Milliseconds(),
			"remote_ip", c.RealIP(),
		)
		return err
	}
}
