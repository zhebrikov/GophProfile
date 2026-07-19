package handlers

import (
	"context"
	"io"

	"github.com/practicum/gophprofile/internal/domain"
)

type AvatarService interface {
	Upload(ctx context.Context, userID, fileName string, reader io.Reader) (*domain.UploadResponse, error)
	GetImage(ctx context.Context, avatarID, size, format string) ([]byte, string, string, error)
	GetUserAvatar(ctx context.Context, userID, size, format string) ([]byte, string, string, error)
	GetMetadata(ctx context.Context, avatarID string) (*domain.MetadataResponse, error)
	ListByUser(ctx context.Context, userID string) ([]*domain.MetadataResponse, error)
	Delete(ctx context.Context, avatarID, userID string) error
	DeleteUserAvatar(ctx context.Context, userID, requesterID string) error
	MaxFileSize() int64
}

type HealthChecker interface {
	Check(ctx context.Context) domain.HealthResponse
}
