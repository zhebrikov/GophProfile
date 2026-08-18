package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/practicum/gophprofile/internal/config"
	"github.com/practicum/gophprofile/internal/observability"
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/internal/worker"
	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/practicum/gophprofile/pkg/storage"
)

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

	store, err := storage.NewS3Storage(
		cfg.S3.Endpoint, cfg.S3.AccessKey, cfg.S3.SecretKey,
		cfg.S3.Bucket, cfg.S3.UseSSL, cfg.S3.PublicEndpoint,
	)
	if err != nil {
		slog.Error("s3", "error", err)
		os.Exit(1)
	}
	if err := waitFor(ctx, "s3", func() error { return store.EnsureBucket(ctx) }); err != nil {
		slog.Error("s3", "error", err)
		os.Exit(1)
	}

	mq, err := waitForBroker(ctx, cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange)
	if err != nil {
		slog.Error("rabbitmq", "error", err)
		os.Exit(1)
	}
	defer func() { _ = mq.Close() }()

	repo := repository.NewAvatarRepository(pool)
	w := worker.New(repo, store, mq)

	metricsAddr := cfg.MetricsAddr
	if metricsAddr == "" {
		metricsAddr = ":9091"
	}
	metricsSrv := &http.Server{
		Addr:              metricsAddr,
		Handler:           observability.MetricsHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		slog.Info("worker metrics listening", "addr", metricsAddr)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server", "error", err)
		}
	}()

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
