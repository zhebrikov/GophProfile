package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/practicum/gophprofile/internal/config"
	"github.com/practicum/gophprofile/internal/observability"
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/internal/worker"
	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/practicum/gophprofile/pkg/circuitbreaker"
	"github.com/practicum/gophprofile/pkg/storage"
)

type workerDeps struct {
	repo  circuitbreaker.Repository
	store circuitbreaker.Storage
	mq    interface{ Ping(context.Context) error }
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}

	serviceName := cfg.ServiceName
	if serviceName == "" || serviceName == "gophprofile" {
		serviceName = "gophprofile-worker"
	}
	observability.SetupLogger(serviceName)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	shutdownTracing, err := observability.SetupTracing(ctx, serviceName, cfg.OTLPEndpoint)
	if err != nil {
		slog.Error("tracing setup failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = shutdownTracing(shutdownCtx)
	}()

	var (
		ready atomic.Bool
		deps  atomic.Pointer[workerDeps]
	)

	metricsAddr := cfg.MetricsAddr
	if metricsAddr == "" {
		metricsAddr = ":9091"
	}
	metricsSrv := &http.Server{
		Addr:              metricsAddr,
		Handler:           newWorkerProbeHandler(&ready, &deps),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		slog.Info("worker probes/metrics listening", "addr", metricsAddr)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server", "error", err)
		}
	}()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := waitFor(ctx, "postgres", func() error { return pool.Ping(ctx) }); err != nil {
		slog.Error("postgres", "error", err)
		os.Exit(1)
	}

	s3Store, err := storage.NewS3Storage(
		cfg.S3.Endpoint, cfg.S3.AccessKey, cfg.S3.SecretKey,
		cfg.S3.Bucket, cfg.S3.UseSSL, cfg.S3.PublicEndpoint,
	)
	if err != nil {
		slog.Error("s3", "error", err)
		os.Exit(1)
	}
	if err := waitFor(ctx, "s3", func() error { return s3Store.EnsureBucket(ctx) }); err != nil {
		slog.Error("s3", "error", err)
		os.Exit(1)
	}

	mq, err := waitForBroker(ctx, cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange)
	if err != nil {
		slog.Error("rabbitmq", "error", err)
		os.Exit(1)
	}
	defer func() { _ = mq.Close() }()

	repo := circuitbreaker.WrapRepository(
		repository.NewAvatarRepository(pool),
		circuitbreaker.New(circuitbreaker.Settings{Name: "postgres"}),
	)
	storeCB := circuitbreaker.WrapStorage(s3Store, circuitbreaker.New(circuitbreaker.Settings{Name: "s3"}))
	deps.Store(&workerDeps{repo: repo, store: storeCB, mq: mq})
	ready.Store(true)

	w := worker.New(repo, storeCB, mq)

	slog.Info("worker starting")
	err = w.Start(ctx)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = metricsSrv.Shutdown(shutdownCtx)

	if err != nil && err != context.Canceled {
		slog.Error("worker", "error", err)
		os.Exit(1)
	}
}

func newWorkerProbeHandler(ready *atomic.Bool, deps *atomic.Pointer[workerDeps]) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", observability.MetricsHandler())
	mux.HandleFunc("/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		d := deps.Load()
		if !ready.Load() || d == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "starting",
				"components": map[string]string{
					"postgres": "starting",
					"s3":       "starting",
					"broker":   "starting",
				},
			})
			return
		}

		ctx := r.Context()
		components := map[string]string{
			"postgres": "ok",
			"s3":       "ok",
			"broker":   "ok",
		}
		status := "ok"
		code := http.StatusOK

		if err := d.repo.Ping(ctx); err != nil {
			components["postgres"] = "unavailable"
			status = "degraded"
			code = http.StatusServiceUnavailable
		}
		if err := d.store.Ping(ctx); err != nil {
			components["s3"] = "unavailable"
			status = "degraded"
			code = http.StatusServiceUnavailable
		}
		if err := d.mq.Ping(ctx); err != nil {
			components["broker"] = "unavailable"
			status = "degraded"
			code = http.StatusServiceUnavailable
		}

		writeJSON(w, code, map[string]any{
			"status":     status,
			"components": components,
		})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func waitFor(ctx context.Context, name string, fn func() error) error {
	var last error
	for i := 0; i < 30; i++ {
		if err := fn(); err == nil {
			return nil
		} else {
			last = err
		}
		slog.Info("waiting for dependency", "name", name, "error", last)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return last
}

func waitForBroker(ctx context.Context, url, exchange string) (*broker.RabbitMQ, error) {
	var last error
	for i := 0; i < 30; i++ {
		mq, err := broker.NewRabbitMQ(url, exchange)
		if err == nil {
			return mq, nil
		}
		last = err
		slog.Info("waiting for rabbitmq", "error", last)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, last
}
