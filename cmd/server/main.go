package main

import (
	"context"
	"fmt"
	"log"
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
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/internal/services"
	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/practicum/gophprofile/pkg/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	if err := waitFor(ctx, "postgres", func() error { return pool.Ping(ctx) }); err != nil {
		log.Fatalf("postgres: %v", err)
	}

	migrationSQL, err := os.ReadFile("migrations/001_init.sql")
	if err != nil {
		log.Fatalf("read migrations: %v", err)
	}
	if err := repository.Migrate(ctx, pool, string(migrationSQL)); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	store, err := storage.NewS3Storage(
		cfg.S3.Endpoint, cfg.S3.AccessKey, cfg.S3.SecretKey,
		cfg.S3.Bucket, cfg.S3.UseSSL, cfg.S3.PublicEndpoint,
	)
	if err != nil {
		log.Fatalf("s3: %v", err)
	}
	if err := waitFor(ctx, "s3", func() error { return store.EnsureBucket(ctx) }); err != nil {
		log.Fatalf("s3: %v", err)
	}

	mq, err := waitForBroker(ctx, cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange)
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	defer func() { _ = mq.Close() }()
	if err := mq.SetupQueues(); err != nil {
		log.Fatalf("rabbitmq queues: %v", err)
	}

	repo := repository.NewAvatarRepository(pool)
	avatarSvc := services.NewAvatarService(repo, store, mq, cfg.PublicURL, cfg.MaxFileSize)
	healthSvc := services.NewHealthService(repo, store, mq)
	api := handlers.NewAvatarHandler(avatarSvc, healthSvc)

	web, err := handlers.NewWebHandler(avatarSvc, cfg.WebDir)
	if err != nil {
		log.Fatalf("web: %v", err)
	}

	e := echo.New()
	e.HideBanner = true
	e.Use(echomw.Recover())
	e.Use(echomw.Logger())
	e.Use(echomw.BodyLimit(fmt.Sprintf("%dM", cfg.MaxFileSize/(1024*1024)+1)))
	e.Use(echomw.CORSWithConfig(echomw.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderContentType, "X-User-ID"},
	}))

	limiter := appmw.NewRateLimiter(cfg.RateLimit, int(cfg.RateLimit*2))
	e.Use(limiter.Middleware)

	e.GET("/health", api.Health)

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

	go func() {
		log.Printf("server listening on %s", cfg.HTTPAddr)
		if err := e.Start(cfg.HTTPAddr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
	defer shutdownCancel()
	_ = e.Shutdown(shutdownCtx)
}

func waitFor(ctx context.Context, name string, fn func() error) error {
	var last error
	for i := 0; i < 30; i++ {
		if err := fn(); err == nil {
			return nil
		} else {
			last = err
		}
		log.Printf("waiting for %s: %v", name, last)
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
		log.Printf("waiting for rabbitmq: %v", last)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, last
}
