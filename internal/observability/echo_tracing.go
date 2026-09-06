package observability

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// EchoTracingMiddleware extracts/injects W3C Trace Context and creates a server span.
func EchoTracingMiddleware(serviceName string) echo.MiddlewareFunc {
	tracer := otel.Tracer(serviceName)
	propagator := otel.GetTextMapPropagator()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			if req.URL.Path == "/metrics" {
				return next(c)
			}

			ctx := propagator.Extract(req.Context(), propagation.HeaderCarrier(req.Header))
			spanName := fmt.Sprintf("%s %s", req.Method, c.Path())
			if c.Path() == "" {
				spanName = fmt.Sprintf("%s %s", req.Method, req.URL.Path)
			}

			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPMethod(req.Method),
					semconv.HTTPTarget(req.URL.RequestURI()),
					semconv.HTTPRoute(c.Path()),
					semconv.HTTPScheme(c.Scheme()),
					attribute.String("server.address", req.Host),
					semconv.UserAgentOriginal(req.UserAgent()),
				),
			)
			defer span.End()

			c.SetRequest(req.WithContext(ctx))
			propagator.Inject(ctx, propagation.HeaderCarrier(c.Response().Header()))

			err := next(c)

			status := c.Response().Status
			if status == 0 {
				status = http.StatusOK
			}
			if err != nil {
				span.RecordError(err)
				if he, ok := err.(*echo.HTTPError); ok {
					status = he.Code
				} else if status < http.StatusBadRequest {
					status = http.StatusInternalServerError
				}
				if status >= 500 {
					span.SetStatus(codes.Error, err.Error())
				}
			}
			span.SetAttributes(semconv.HTTPStatusCode(status))
			if status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(status))
			}
			return err
		}
	}
}
