package config_test

import (
	"testing"

	"github.com/practicum/gophprofile/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
	t.Setenv("HTTP_ADDR", "")
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTPAddr)
	require.Equal(t, int64(10*1024*1024), cfg.MaxFileSize)
	require.Equal(t, "avatars", cfg.S3.Bucket)
	require.Equal(t, "avatars.exchange", cfg.RabbitMQ.Exchange)
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	cfg, err := config.Load()
	require.Nil(t, cfg)
	require.ErrorContains(t, err, "DATABASE_URL is required")
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("S3_BUCKET", "mybucket")
	t.Setenv("MAX_FILE_SIZE", "1024")
	t.Setenv("S3_USE_SSL", "true")
	t.Setenv("RATE_LIMIT", "5")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, ":9090", cfg.HTTPAddr)
	require.Equal(t, "mybucket", cfg.S3.Bucket)
	require.Equal(t, int64(1024), cfg.MaxFileSize)
	require.True(t, cfg.S3.UseSSL)
	require.Equal(t, 5.0, cfg.RateLimit)
	require.Positive(t, cfg.ShutdownTimeout())
}

func TestLoadInvalidBool(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("S3_USE_SSL", "yes")
	cfg, err := config.Load()
	require.Nil(t, cfg)
	require.ErrorContains(t, err, "invalid S3_USE_SSL")
}

func TestLoadInvalidInt64(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("MAX_FILE_SIZE", "10mb")
	cfg, err := config.Load()
	require.Nil(t, cfg)
	require.ErrorContains(t, err, "invalid MAX_FILE_SIZE")
}

func TestLoadInvalidFloat(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("RATE_LIMIT", "fast")
	cfg, err := config.Load()
	require.Nil(t, cfg)
	require.ErrorContains(t, err, "invalid RATE_LIMIT")
}
