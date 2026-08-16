package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/practicum/gophprofile/internal/domain"
	"github.com/practicum/gophprofile/internal/observability"
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/practicum/gophprofile/pkg/imageutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type AvatarService struct {
	repo      AvatarRepository
	storage   ObjectStorage
	broker    EventPublisher
	publicURL string
	maxSize   int64
}

func NewAvatarService(
	repo AvatarRepository,
	store ObjectStorage,
	mq EventPublisher,
	publicURL string,
	maxSize int64,
) *AvatarService {
	return &AvatarService{
		repo:      repo,
		storage:   store,
		broker:    mq,
		publicURL: strings.TrimRight(publicURL, "/"),
		maxSize:   maxSize,
	}
}

func (s *AvatarService) Upload(ctx context.Context, userID, fileName string, reader io.Reader) (*domain.UploadResponse, error) {
	ctx, span := otel.Tracer("gophprofile/services").Start(ctx, "AvatarService.Upload")
	defer span.End()
	span.SetAttributes(attribute.String("user.id", userID))

	if strings.TrimSpace(userID) == "" {
		return nil, ErrInvalidUserID
	}

	data, err := imageutil.ReadLimited(reader, s.maxSize)
	if err != nil {
		if err == imageutil.ErrTooLarge {
			return nil, ErrFileTooLarge
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	img, mime, err := imageutil.Decode(data)
	if err != nil {
		return nil, ErrInvalidFormat
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	avatarID := uuid.New().String()
	ext := imageutil.ExtensionForMIME(mime)
	s3Key := fmt.Sprintf("originals/%s/%s.%s", userID, avatarID, ext)
	span.SetAttributes(attribute.String("avatar.id", avatarID))

	avatar := &domain.Avatar{
		ID:               avatarID,
		UserID:           userID,
		FileName:         path.Base(fileName),
		MimeType:         mime,
		SizeBytes:        int64(len(data)),
		S3Key:            s3Key,
		Width:            width,
		Height:           height,
		UploadStatus:     domain.UploadStatusUploading,
		ProcessingStatus: domain.ProcessingStatusPending,
	}

	if err := s.repo.Create(ctx, avatar); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("create avatar record: %w", err)
	}

	if err := s.storage.Upload(ctx, s3Key, bytes.NewReader(data), int64(len(data)), mime); err != nil {
		_ = s.repo.UpdateUploadStatus(ctx, avatarID, domain.UploadStatusFailed)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("upload to s3: %w", err)
	}

	if err := s.repo.UpdateUploadStatus(ctx, avatarID, domain.UploadStatusUploaded); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	event := domain.AvatarUploadEvent{
		MessageID: uuid.New().String(),
		AvatarID:  avatarID,
		UserID:    userID,
		S3Key:     s3Key,
	}
	if err := s.broker.Publish(ctx, broker.RoutingKeyUploaded, event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("publish upload event: %w", err)
	}

	observability.AvatarsUploaded.Inc()
	observability.LoggerFromContext(ctx).Info("avatar uploaded",
		"avatar_id", avatarID,
		"user_id", userID,
		"size_bytes", len(data),
		"mime", mime,
	)

	return &domain.UploadResponse{
		ID:        avatar.ID,
		UserID:    avatar.UserID,
		URL:       s.avatarURL(avatar.ID),
		Status:    "processing",
		CreatedAt: avatar.CreatedAt,
	}, nil
}

func (s *AvatarService) GetImage(ctx context.Context, avatarID, size, format string) ([]byte, string, string, error) {
	ctx, span := otel.Tracer("gophprofile/services").Start(ctx, "AvatarService.GetImage")
	defer span.End()
	span.SetAttributes(
		attribute.String("avatar.id", avatarID),
		attribute.String("image.size", size),
		attribute.String("image.format", format),
	)

	avatar, err := s.repo.GetByID(ctx, avatarID)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return nil, "", "", err
	}
	return s.loadImage(ctx, avatar, size, format)
}

func (s *AvatarService) GetUserAvatar(ctx context.Context, userID, size, format string) ([]byte, string, string, error) {
	ctx, span := otel.Tracer("gophprofile/services").Start(ctx, "AvatarService.GetUserAvatar")
	defer span.End()
	span.SetAttributes(
		attribute.String("user.id", userID),
		attribute.String("image.size", size),
		attribute.String("image.format", format),
	)

	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			data, mime := DefaultPlaceholder()
			return data, mime, hashETag(data), nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, "", "", err
	}
	return s.loadImage(ctx, avatar, size, format)
}

func (s *AvatarService) loadImage(ctx context.Context, avatar *domain.Avatar, size, format string) ([]byte, string, string, error) {
	if size == "" {
		size = domain.SizeOriginal
	}

	key := avatar.S3Key
	if size != domain.SizeOriginal {
		thumbs, err := avatar.Thumbnails()
		if err != nil {
			return nil, "", "", err
		}
		found := false
		for _, t := range thumbs {
			if t.Size == size {
				key = t.Key
				found = true
				break
			}
		}
		if !found {
			if avatar.ProcessingStatus != domain.ProcessingStatusCompleted {
				key = avatar.S3Key
			} else {
				return nil, "", "", repository.ErrNotFound
			}
		}
	}

	rc, contentType, err := s.storage.Download(ctx, key)
	if err != nil {
		return nil, "", "", err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", "", err
	}

	etag := hashETag(data)

	if format != "" && format != "original" {
		img, _, err := imageutil.Decode(data)
		if err != nil {
			return nil, "", "", err
		}
		encoded, mime, err := imageutil.Encode(img, format)
		if err != nil {
			if errors.Is(err, imageutil.ErrUnsupportedFormat) {
				return nil, "", "", fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
			}
			return nil, "", "", err
		}
		return encoded, mime, hashETag(encoded), nil
	}

	return data, contentType, etag, nil
}

func (s *AvatarService) GetMetadata(ctx context.Context, avatarID string) (*domain.MetadataResponse, error) {
	ctx, span := otel.Tracer("gophprofile/services").Start(ctx, "AvatarService.GetMetadata")
	defer span.End()
	span.SetAttributes(attribute.String("avatar.id", avatarID))

	avatar, err := s.repo.GetByID(ctx, avatarID)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return nil, err
	}

	thumbs, err := avatar.Thumbnails()
	if err != nil {
		return nil, err
	}
	for i := range thumbs {
		thumbs[i].URL = fmt.Sprintf("%s/api/v1/avatars/%s?size=%s", s.publicURL, avatar.ID, thumbs[i].Size)
	}

	return &domain.MetadataResponse{
		ID:       avatar.ID,
		UserID:   avatar.UserID,
		FileName: avatar.FileName,
		MimeType: avatar.MimeType,
		Size:     avatar.SizeBytes,
		Dimensions: domain.Dimensions{
			Width:  avatar.Width,
			Height: avatar.Height,
		},
		Thumbnails: thumbs,
		Status:     avatar.ProcessingStatus,
		CreatedAt:  avatar.CreatedAt,
		UpdatedAt:  avatar.UpdatedAt,
	}, nil
}

func (s *AvatarService) ListByUser(ctx context.Context, userID string) ([]*domain.MetadataResponse, error) {
	ctx, span := otel.Tracer("gophprofile/services").Start(ctx, "AvatarService.ListByUser")
	defer span.End()
	span.SetAttributes(attribute.String("user.id", userID))

	avatars, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	result := make([]*domain.MetadataResponse, 0, len(avatars))
	for _, a := range avatars {
		meta, err := s.GetMetadata(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, meta)
	}
	return result, nil
}

func (s *AvatarService) Delete(ctx context.Context, avatarID, userID string) error {
	ctx, span := otel.Tracer("gophprofile/services").Start(ctx, "AvatarService.Delete")
	defer span.End()
	span.SetAttributes(
		attribute.String("avatar.id", avatarID),
		attribute.String("user.id", userID),
	)

	avatar, err := s.repo.GetByID(ctx, avatarID)
	if err != nil {
		return err
	}
	if avatar.UserID != userID {
		return ErrForbidden
	}
	if err := s.repo.SoftDelete(ctx, avatarID); err != nil {
		return err
	}
	if err := s.publishDelete(ctx, avatar); err != nil {
		return err
	}
	observability.AvatarsDeleted.Inc()
	observability.LoggerFromContext(ctx).Info("avatar deleted", "avatar_id", avatarID, "user_id", userID)
	return nil
}

func (s *AvatarService) DeleteUserAvatar(ctx context.Context, userID, requesterID string) error {
	ctx, span := otel.Tracer("gophprofile/services").Start(ctx, "AvatarService.DeleteUserAvatar")
	defer span.End()

	if userID != requesterID {
		return ErrForbidden
	}
	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, avatar.ID); err != nil {
		return err
	}
	if err := s.publishDelete(ctx, avatar); err != nil {
		return err
	}
	observability.AvatarsDeleted.Inc()
	observability.LoggerFromContext(ctx).Info("user avatar deleted", "user_id", userID, "avatar_id", avatar.ID)
	return nil
}

func (s *AvatarService) publishDelete(ctx context.Context, avatar *domain.Avatar) error {
	keys := []string{avatar.S3Key}
	if thumbs, err := avatar.Thumbnails(); err == nil {
		for _, t := range thumbs {
			keys = append(keys, t.Key)
		}
	}

	event := domain.AvatarDeleteEvent{
		MessageID: uuid.New().String(),
		AvatarID:  avatar.ID,
		S3Keys:    keys,
	}
	return s.broker.Publish(ctx, broker.RoutingKeyDelete, event)
}

func (s *AvatarService) avatarURL(id string) string {
	return fmt.Sprintf("%s/api/v1/avatars/%s", s.publicURL, id)
}

func (s *AvatarService) MaxFileSize() int64 {
	return s.maxSize
}

func hashETag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

var (
	ErrInvalidUserID     = errors.New("invalid user id")
	ErrInvalidFormat     = errors.New("invalid file format")
	ErrUnsupportedFormat = errors.New("unsupported output format")
	ErrFileTooLarge      = errors.New("file too large")
	ErrForbidden         = errors.New("forbidden")
)
