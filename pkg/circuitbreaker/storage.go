package circuitbreaker

import (
	"context"
	"io"
)

// Storage is the subset of S3 operations protected by the breaker.
type Storage interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, string, error)
	Delete(ctx context.Context, key string) error
	DeleteMany(ctx context.Context, keys []string) error
	Ping(ctx context.Context) error
	EnsureBucket(ctx context.Context) error
}

// StorageBreaker wraps ObjectStorage calls with a circuit breaker.
type StorageBreaker struct {
	inner Storage
	cb    *Breaker
}

// WrapStorage returns a storage wrapper protected by breaker name "s3".
func WrapStorage(inner Storage, cb *Breaker) *StorageBreaker {
	if cb == nil {
		cb = New(Settings{Name: "s3"})
	}
	return &StorageBreaker{inner: inner, cb: cb}
}

func (s *StorageBreaker) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	return s.cb.Execute(func() error {
		return s.inner.Upload(ctx, key, reader, size, contentType)
	})
}

func (s *StorageBreaker) Download(ctx context.Context, key string) (io.ReadCloser, string, error) {
	type result struct {
		rc   io.ReadCloser
		ct   string
	}
	r, err := ExecuteValue(s.cb, func() (result, error) {
		rc, ct, err := s.inner.Download(ctx, key)
		return result{rc: rc, ct: ct}, err
	})
	return r.rc, r.ct, err
}

func (s *StorageBreaker) Delete(ctx context.Context, key string) error {
	return s.cb.Execute(func() error {
		return s.inner.Delete(ctx, key)
	})
}

func (s *StorageBreaker) DeleteMany(ctx context.Context, keys []string) error {
	return s.cb.Execute(func() error {
		return s.inner.DeleteMany(ctx, keys)
	})
}

func (s *StorageBreaker) Ping(ctx context.Context) error {
	if s.cb.IsOpen() {
		return ErrOpen
	}
	return s.cb.Execute(func() error {
		return s.inner.Ping(ctx)
	})
}

func (s *StorageBreaker) EnsureBucket(ctx context.Context) error {
	return s.cb.Execute(func() error {
		return s.inner.EnsureBucket(ctx)
	})
}

// Breaker exposes the underlying breaker for health checks.
func (s *StorageBreaker) Breaker() *Breaker { return s.cb }
