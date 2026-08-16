package services

import (
	"context"
	"io"

	"github.com/practicum/gophprofile/internal/domain"
)

type AvatarRepository interface {
	Create(ctx context.Context, avatar *domain.Avatar) error
	GetByID(ctx context.Context, id string) (*domain.Avatar, error)
	GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error)
	ListByUserID(ctx context.Context, userID string) ([]*domain.Avatar, error)
	SoftDelete(ctx context.Context, id string) error
	UpdateUploadStatus(ctx context.Context, id, status string) error
	Ping(ctx context.Context) error
}

type ObjectStorage interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, string, error)
	Ping(ctx context.Context) error
}

type EventPublisher interface {
	Publish(ctx context.Context, routingKey string, payload any) error
	Ping(ctx context.Context) error
}
