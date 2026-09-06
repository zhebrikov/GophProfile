package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	"github.com/practicum/gophprofile/internal/config"
	"github.com/practicum/gophprofile/internal/handlers"
	appmw "github.com/practicum/gophprofile/internal/middleware"
	"github.com/practicum/gophprofile/internal/observability"
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/internal/services"
	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/practicum/gophprofile/pkg/circuitbreaker"
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
		serviceName = "gophprofile-server"
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
	if err := mq.SetupQueues(); err != nil {
		slog.Error("rabbitmq queues", "error", err)
		os.Exit(1)
	}

	repo := circuitbreaker.WrapRepository(
		repository.NewAvatarRepository(pool),
		circuitbreaker.New(circuitbreaker.Settings{Name: "postgres"}),
	)
	storeCB := circuitbreaker.WrapStorage(store, circuitbreaker.New(circuitbreaker.Settings{Name: "s3"}))
	mqCB := circuitbreaker.WrapPublisher(mq, circuitbreaker.New(circuitbreaker.Settings{Name: "broker"}))

	avatarSvc := services.NewAvatarService(repo, storeCB, mqCB, cfg.PublicURL, cfg.MaxFileSize)
	healthSvc := services.NewHealthService(repo, storeCB, mqCB)
	api := handlers.NewAvatarHandler(avatarSvc, healthSvc)

	web, err := handlers.NewWebHandler(avatarSvc, cfg.WebDir)
	if err != nil {
		slog.Error("web", "error", err)
		os.Exit(1)
	}

	e := echo.New()
	e.HideBanner = true
	e.Use(echomw.Recover())
	e.Use(observability.EchoTracingMiddleware(serviceName))
	e.Use(observability.RequestIDMiddleware)
	e.Use(observability.EchoMetricsMiddleware())
	e.Use(echomw.BodyLimit(fmt.Sprintf("%dM", cfg.MaxFileSize/(1024*1024)+1)))
	e.Use(echomw.CORSWithConfig(echomw.CORSConfig{
		AllowOrigins:  []string{"*"},
		AllowMethods:  []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowHeaders:  []string{echo.HeaderContentType, "X-User-ID", observability.RequestIDHeader, "traceparent", "tracestate"},
		ExposeHeaders: []string{observability.RequestIDHeader},
	}))

	limiter := appmw.NewRateLimiter(cfg.RateLimit, int(cfg.RateLimit*2))
	e.Use(limiter.Middleware)

	e.GET("/metrics", echo.WrapHandler(observability.MetricsHandler()))
	e.GET("/health", api.Health)
	e.GET("/live", api.Live)
	e.GET("/ready", api.Ready)

	v1 := e.Group("/api/v1")
	v1.POST("/avatars", api.Upload, appmw.RequireUserID)
	v1.GET("/avatars/:avatar_id", api.GetAvatar)
	v1.GET("/avatars/:avatar_id/metadata", api.GetMetadata)
	v1.DELETE("/avatars/:avatar_id", api.DeleteAvatar, appmw.RequireUserID)
	v1.GET("/users/:user_id/avatar", api.GetUserAvatar)
	v1.GET("/users/:user_id/avatars", api.ListUserAvatars)
	v1.DELETE("/users/:user_id/avatar", api.DeleteUserAvatar, appmw.RequireUserID)

	e.GET("/web/upload", web.UploadForm)
	e.POST("/web/upload", web.UploadSubmit)
	e.GET("/web/gallery/:user_id", web.Gallery)
	e.Static("/web/static", cfg.WebDir+"/static")
	e.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusFound, "/web/upload")
	})

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", cfg.HTTPAddr)
		errCh <- e.Start(cfg.HTTPAddr)
	}()

	var serverErr error
	select {
	case serverErr = <-errCh:
	case <-ctx.Done():
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
	defer shutdownCancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}

	if serverErr == nil {
		serverErr = <-errCh
	}
	if serverErr != nil && serverErr != http.ErrServerClosed {
		slog.Error("server", "error", serverErr)
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
