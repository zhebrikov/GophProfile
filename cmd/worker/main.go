package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/practicum/gophprofile/internal/config"
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/internal/worker"
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

	repo := repository.NewAvatarRepository(pool)
	w := worker.New(repo, store, mq)

	log.Println("worker starting")
	if err := w.Start(ctx); err != nil && err != context.Canceled {
		log.Fatalf("worker: %v", err)
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
