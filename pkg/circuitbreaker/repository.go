package circuitbreaker

import (
	"context"

	"github.com/practicum/gophprofile/internal/domain"
)

// Repository covers avatar persistence used by server and worker.
type Repository interface {
	Create(ctx context.Context, avatar *domain.Avatar) error
	GetByID(ctx context.Context, id string) (*domain.Avatar, error)
	GetByIDIncludingDeleted(ctx context.Context, id string) (*domain.Avatar, error)
	GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error)
	ListByUserID(ctx context.Context, userID string) ([]*domain.Avatar, error)
	SoftDelete(ctx context.Context, id string) error
	UpdateUploadStatus(ctx context.Context, id, status string) error
	UpdateProcessingStatus(ctx context.Context, id, status string) error
	UpdateThumbnails(ctx context.Context, id string, thumbs []domain.ThumbnailInfo, width, height int) error
	MarkMessageProcessed(ctx context.Context, messageID string) (bool, error)
	IsMessageProcessed(ctx context.Context, messageID string) (bool, error)
	Ping(ctx context.Context) error
}

// RepositoryBreaker wraps repository calls with a circuit breaker.
type RepositoryBreaker struct {
	inner Repository
	cb    *Breaker
}

// WrapRepository returns a repository wrapper protected by breaker name "postgres".
func WrapRepository(inner Repository, cb *Breaker) *RepositoryBreaker {
	if cb == nil {
		cb = New(Settings{Name: "postgres"})
	}
	return &RepositoryBreaker{inner: inner, cb: cb}
}

func (r *RepositoryBreaker) Create(ctx context.Context, avatar *domain.Avatar) error {
	return r.cb.Execute(func() error {
		return r.inner.Create(ctx, avatar)
	})
}

func (r *RepositoryBreaker) GetByID(ctx context.Context, id string) (*domain.Avatar, error) {
	return ExecuteValue(r.cb, func() (*domain.Avatar, error) {
		return r.inner.GetByID(ctx, id)
	})
}

func (r *RepositoryBreaker) GetByIDIncludingDeleted(ctx context.Context, id string) (*domain.Avatar, error) {
	return ExecuteValue(r.cb, func() (*domain.Avatar, error) {
		return r.inner.GetByIDIncludingDeleted(ctx, id)
	})
}

func (r *RepositoryBreaker) GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error) {
	return ExecuteValue(r.cb, func() (*domain.Avatar, error) {
		return r.inner.GetLatestByUserID(ctx, userID)
	})
}

func (r *RepositoryBreaker) ListByUserID(ctx context.Context, userID string) ([]*domain.Avatar, error) {
	return ExecuteValue(r.cb, func() ([]*domain.Avatar, error) {
		return r.inner.ListByUserID(ctx, userID)
	})
}

func (r *RepositoryBreaker) SoftDelete(ctx context.Context, id string) error {
	return r.cb.Execute(func() error {
		return r.inner.SoftDelete(ctx, id)
	})
}

func (r *RepositoryBreaker) UpdateUploadStatus(ctx context.Context, id, status string) error {
	return r.cb.Execute(func() error {
		return r.inner.UpdateUploadStatus(ctx, id, status)
	})
}

func (r *RepositoryBreaker) UpdateProcessingStatus(ctx context.Context, id, status string) error {
	return r.cb.Execute(func() error {
		return r.inner.UpdateProcessingStatus(ctx, id, status)
	})
}

func (r *RepositoryBreaker) UpdateThumbnails(ctx context.Context, id string, thumbs []domain.ThumbnailInfo, width, height int) error {
	return r.cb.Execute(func() error {
		return r.inner.UpdateThumbnails(ctx, id, thumbs, width, height)
	})
}

func (r *RepositoryBreaker) MarkMessageProcessed(ctx context.Context, messageID string) (bool, error) {
	return ExecuteValue(r.cb, func() (bool, error) {
		return r.inner.MarkMessageProcessed(ctx, messageID)
	})
}

func (r *RepositoryBreaker) IsMessageProcessed(ctx context.Context, messageID string) (bool, error) {
	return ExecuteValue(r.cb, func() (bool, error) {
		return r.inner.IsMessageProcessed(ctx, messageID)
	})
}

func (r *RepositoryBreaker) Ping(ctx context.Context) error {
	if r.cb.IsOpen() {
		return ErrOpen
	}
	return r.cb.Execute(func() error {
		return r.inner.Ping(ctx)
	})
}

// Breaker exposes the underlying breaker for health checks.
func (r *RepositoryBreaker) Breaker() *Breaker { return r.cb }
