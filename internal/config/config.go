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
	maxFileSize, err := getEnvInt64("MAX_FILE_SIZE", 10*1024*1024)
	if err != nil {
		return nil, err
	}
	rateLimit, err := getEnvFloat("RATE_LIMIT", 20)
	if err != nil {
		return nil, err
	}
	useSSL, err := getEnvBool("S3_USE_SSL", false)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://gophprofile:gophprofile@localhost:5432/gophprofile?sslmode=disable"),
		PublicURL:   getEnv("PUBLIC_URL", "http://localhost:8080"),
		WebDir:      getEnv("WEB_DIR", "web"),
		MaxFileSize: maxFileSize,
		RateLimit:   rateLimit,
		S3: S3Config{
			Endpoint:       getEnv("S3_ENDPOINT", "localhost:9000"),
			AccessKey:      getEnv("S3_ACCESS_KEY", "minioadmin"),
			SecretKey:      getEnv("S3_SECRET_KEY", "minioadmin"),
			Bucket:         getEnv("S3_BUCKET", "avatars"),
			UseSSL:         useSSL,
			PublicEndpoint: getEnv("S3_PUBLIC_ENDPOINT", "http://localhost:9000"),
		},
		RabbitMQ: RabbitMQConfig{
			URL:      getEnv("BROKER_URL", "amqp://guest:guest@localhost:5672/"),
			Exchange: getEnv("RABBITMQ_EXCHANGE", "avatars.exchange"),
		},
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s=%q: %w", key, v, err)
	}
	return b, nil
}

func getEnvInt64(key string, fallback int64) (int64, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", key, v, err)
	}
	return n, nil
}

func getEnvFloat(key string, fallback float64) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", key, v, err)
	}
	return n, nil
}

func (c *Config) ShutdownTimeout() time.Duration {
	return 10 * time.Second
}
