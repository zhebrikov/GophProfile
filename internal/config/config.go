package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr    string
	DatabaseURL string
	S3          S3Config
	RabbitMQ    RabbitMQConfig
	PublicURL   string
	WebDir      string
	MaxFileSize int64
	RateLimit   float64
}

type S3Config struct {
	Endpoint       string
	AccessKey      string
	SecretKey      string
	Bucket         string
	UseSSL         bool
	PublicEndpoint string
}

type RabbitMQConfig struct {
	URL      string
	Exchange string
}

func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://gophprofile:gophprofile@localhost:5432/gophprofile?sslmode=disable"),
		PublicURL:   getEnv("PUBLIC_URL", "http://localhost:8080"),
		WebDir:      getEnv("WEB_DIR", "web"),
		MaxFileSize: getEnvInt64("MAX_FILE_SIZE", 10*1024*1024),
		RateLimit:   getEnvFloat("RATE_LIMIT", 20),
		S3: S3Config{
			Endpoint:       getEnv("S3_ENDPOINT", "localhost:9000"),
			AccessKey:      getEnv("S3_ACCESS_KEY", "minioadmin"),
			SecretKey:      getEnv("S3_SECRET_KEY", "minioadmin"),
			Bucket:         getEnv("S3_BUCKET", "avatars"),
			UseSSL:         getEnvBool("S3_USE_SSL", false),
			PublicEndpoint: getEnv("S3_PUBLIC_ENDPOINT", "http://localhost:9000"),
		},
		RabbitMQ: RabbitMQConfig{
			URL:      getEnv("BROKER_URL", "amqp://guest:guest@localhost:5672/"),
			Exchange: getEnv("RABBITMQ_EXCHANGE", "avatars.exchange"),
		},
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func (c *Config) ShutdownTimeout() time.Duration {
	return 10 * time.Second
}
