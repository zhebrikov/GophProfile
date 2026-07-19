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
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/practicum/gophprofile/pkg/imageutil"
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
	if strings.TrimSpace(userID) == "" {
		return nil, ErrInvalidUserID
	}

	data, err := imageutil.ReadLimited(reader, s.maxSize)
	if err != nil {
		if err == imageutil.ErrTooLarge {
			return nil, ErrFileTooLarge
		}
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
		return nil, fmt.Errorf("create avatar record: %w", err)
	}

	if err := s.storage.Upload(ctx, s3Key, bytes.NewReader(data), int64(len(data)), mime); err != nil {
		_ = s.repo.UpdateUploadStatus(ctx, avatarID, domain.UploadStatusFailed)
		return nil, fmt.Errorf("upload to s3: %w", err)
	}

	if err := s.repo.UpdateUploadStatus(ctx, avatarID, domain.UploadStatusUploaded); err != nil {
		return nil, err
	}

	event := domain.AvatarUploadEvent{
		MessageID: uuid.New().String(),
		AvatarID:  avatarID,
		UserID:    userID,
		S3Key:     s3Key,
	}
	if err := s.broker.Publish(ctx, broker.RoutingKeyUploaded, event); err != nil {
		return nil, fmt.Errorf("publish upload event: %w", err)
	}

	return &domain.UploadResponse{
		ID:        avatar.ID,
		UserID:    avatar.UserID,
		URL:       s.avatarURL(avatar.ID),
		Status:    "processing",
		CreatedAt: avatar.CreatedAt,
	}, nil
}

func (s *AvatarService) GetImage(ctx context.Context, avatarID, size, format string) ([]byte, string, string, error) {
	avatar, err := s.repo.GetByID(ctx, avatarID)
	if err != nil {
		return nil, "", "", err
	}
	return s.loadImage(ctx, avatar, size, format)
}

func (s *AvatarService) GetUserAvatar(ctx context.Context, userID, size, format string) ([]byte, string, string, error) {
	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			data, mime := DefaultPlaceholder()
			return data, mime, hashETag(data), nil
		}
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
		if err == nil {
			encoded, mime, err := imageutil.Encode(img, format)
			if err == nil {
				return encoded, mime, hashETag(encoded), nil
			}
		}
	}

	return data, contentType, etag, nil
}

func (s *AvatarService) GetMetadata(ctx context.Context, avatarID string) (*domain.MetadataResponse, error) {
	avatar, err := s.repo.GetByID(ctx, avatarID)
	if err != nil {
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
	avatars, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
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
	avatar, err := s.repo.SoftDelete(ctx, avatarID, userID)
	if err != nil {
		return err
	}
	return s.publishDelete(ctx, avatar)
}

func (s *AvatarService) DeleteUserAvatar(ctx context.Context, userID, requesterID string) error {
	avatar, err := s.repo.SoftDeleteLatestByUser(ctx, userID, requesterID)
	if err != nil {
		return err
	}
	return s.publishDelete(ctx, avatar)
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
	ErrInvalidUserID = fmt.Errorf("invalid user id")
	ErrInvalidFormat = fmt.Errorf("invalid file format")
	ErrFileTooLarge  = fmt.Errorf("file too large")
)
