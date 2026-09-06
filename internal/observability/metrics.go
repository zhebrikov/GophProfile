package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"method", "path", "status"})

	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gophprofile_http_request_duration_seconds",
		Help:    "HTTP request latency",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	AvatarsUploaded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gophprofile_avatars_uploaded_total",
		Help: "Total avatars uploaded successfully",
	})

	AvatarsDeleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gophprofile_avatars_deleted_total",
		Help: "Total avatars deleted",
	})

	AvatarsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_avatars_processed_total",
		Help: "Total avatars processed by worker",
	}, []string{"status"})

	AvatarProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "gophprofile_avatar_processing_duration_seconds",
		Help:    "Avatar thumbnail processing duration",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	AvatarDeletesProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_avatar_deletes_processed_total",
		Help: "Total delete events processed by worker",
	}, []string{"status"})

	HealthChecks = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gophprofile_health_component_up",
		Help: "Component health (1=up, 0=down)",
	}, []string{"component"})

	CircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gophprofile_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=half-open, 2=open)",
	}, []string{"name"})

	CircuitBreakerTrips = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_circuit_breaker_trips_total",
		Help: "Total times a circuit breaker transitioned to open",
	}, []string{"name"})
)

// MetricsHandler returns the Prometheus scrape handler.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// EchoMetricsMiddleware records RED metrics for HTTP handlers.
func EchoMetricsMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Path()
			if path == "" {
				path = c.Request().URL.Path
			}
			if path == "/metrics" || path == "/health" || path == "/live" || path == "/ready" {
				return next(c)
			}

			start := time.Now()
			err := next(c)
			status := c.Response().Status
			if status == 0 {
				status = http.StatusOK
			}
			if err != nil {
				if he, ok := err.(*echo.HTTPError); ok {
					status = he.Code
				} else if status < 400 {
					status = http.StatusInternalServerError
				}
			}

			labels := []string{c.Request().Method, path}
			HTTPDuration.WithLabelValues(labels...).Observe(time.Since(start).Seconds())
			HTTPRequests.WithLabelValues(c.Request().Method, path, strconv.Itoa(status)).Inc()
			return err
		}
	}
}
