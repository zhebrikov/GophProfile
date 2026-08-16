package observability

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

type ctxKey string

const (
	requestIDKey ctxKey = "request_id"
	serviceKey   ctxKey = "service"
)

// SetupLogger configures a JSON slog logger as the default.
func SetupLogger(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler).With("service", service)
	slog.SetDefault(logger)
	return logger
}

// WithRequestID stores a request id in context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext returns the request id if present.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// LoggerFromContext returns a logger enriched with request_id and trace/span ids.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	logger := slog.Default()
	if rid := RequestIDFromContext(ctx); rid != "" {
		logger = logger.With("request_id", rid)
	}
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		sc := span.SpanContext()
		logger = logger.With(
			"trace_id", sc.TraceID().String(),
			"span_id", sc.SpanID().String(),
		)
	}
	return logger
}
