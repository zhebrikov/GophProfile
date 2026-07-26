package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/practicum/gophprofile/internal/domain"
)

var (
	ErrNotFound = errors.New("avatar not found")
)

type AvatarRepository struct {
	pool *pgxpool.Pool
}

func NewAvatarRepository(pool *pgxpool.Pool) *AvatarRepository {
	return &AvatarRepository{pool: pool}
}

func (r *AvatarRepository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *AvatarRepository) Create(ctx context.Context, avatar *domain.Avatar) error {
	if avatar.ID == "" {
		avatar.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	avatar.CreatedAt = now
	avatar.UpdatedAt = now

	const q = `
		INSERT INTO avatars (
			id, user_id, file_name, mime_type, size_bytes, s3_key,
			thumbnail_s3_keys, width, height, upload_status, processing_status,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`

	var thumbs any
	if len(avatar.ThumbnailS3Keys) > 0 {
		thumbs = avatar.ThumbnailS3Keys
	}

	_, err := r.pool.Exec(ctx, q,
		avatar.ID, avatar.UserID, avatar.FileName, avatar.MimeType, avatar.SizeBytes,
		avatar.S3Key, thumbs, avatar.Width, avatar.Height,
		avatar.UploadStatus, avatar.ProcessingStatus,
		avatar.CreatedAt, avatar.UpdatedAt,
	)
	return err
}

func (r *AvatarRepository) GetByID(ctx context.Context, id string) (*domain.Avatar, error) {
	const q = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
			thumbnail_s3_keys, width, height, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars WHERE id = $1 AND deleted_at IS NULL`

	return r.scanOne(ctx, q, id)
}

func (r *AvatarRepository) GetByIDIncludingDeleted(ctx context.Context, id string) (*domain.Avatar, error) {
	const q = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
			thumbnail_s3_keys, width, height, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars WHERE id = $1`

	return r.scanOne(ctx, q, id)
}

func (r *AvatarRepository) GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error) {
	const q = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
			thumbnail_s3_keys, width, height, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`

	return r.scanOne(ctx, q, userID)
}

func (r *AvatarRepository) ListByUserID(ctx context.Context, userID string) ([]*domain.Avatar, error) {
	const q = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
			thumbnail_s3_keys, width, height, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*domain.Avatar
	for rows.Next() {
		a, err := scanAvatar(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (r *AvatarRepository) SoftDelete(ctx context.Context, id string) error {
	now := time.Now().UTC()
	const q = `UPDATE avatars SET deleted_at = $1, updated_at = $1 WHERE id = $2 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, now, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *AvatarRepository) UpdateUploadStatus(ctx context.Context, id, status string) error {
	const q = `UPDATE avatars SET upload_status = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.pool.Exec(ctx, q, status, id)
	return err
}

func (r *AvatarRepository) UpdateProcessingStatus(ctx context.Context, id, status string) error {
	const q = `UPDATE avatars SET processing_status = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.pool.Exec(ctx, q, status, id)
	return err
}

func (r *AvatarRepository) UpdateThumbnails(ctx context.Context, id string, thumbs []domain.ThumbnailInfo, width, height int) error {
	data, err := json.Marshal(thumbs)
	if err != nil {
		return err
	}
	const q = `
		UPDATE avatars
		SET thumbnail_s3_keys = $1, width = $2, height = $3,
			processing_status = $4, updated_at = NOW()
		WHERE id = $5`
	_, err = r.pool.Exec(ctx, q, data, width, height, domain.ProcessingStatusCompleted, id)
	return err
}

func (r *AvatarRepository) MarkMessageProcessed(ctx context.Context, messageID string) (bool, error) {
	const q = `INSERT INTO processed_messages (message_id) VALUES ($1) ON CONFLICT DO NOTHING`
	tag, err := r.pool.Exec(ctx, q, messageID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *AvatarRepository) IsMessageProcessed(ctx context.Context, messageID string) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM processed_messages WHERE message_id = $1)`
	var exists bool
	err := r.pool.QueryRow(ctx, q, messageID).Scan(&exists)
	return exists, err
}

func (r *AvatarRepository) scanOne(ctx context.Context, q string, args ...any) (*domain.Avatar, error) {
	row := r.pool.QueryRow(ctx, q, args...)
	a, err := scanAvatar(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAvatar(row scannable) (*domain.Avatar, error) {
	var a domain.Avatar
	var thumbs []byte
	err := row.Scan(
		&a.ID, &a.UserID, &a.FileName, &a.MimeType, &a.SizeBytes, &a.S3Key,
		&thumbs, &a.Width, &a.Height, &a.UploadStatus, &a.ProcessingStatus,
		&a.CreatedAt, &a.UpdatedAt, &a.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	if thumbs != nil {
		a.ThumbnailS3Keys = thumbs
	}
	return &a, nil
}
