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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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

func (r *AvatarRepository) startSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := otel.Tracer("gophprofile/repository").Start(ctx, name)
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	return ctx, span
}

func endSpan(span trace.Span, err error) {
	if err != nil && !errors.Is(err, ErrNotFound) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

func (r *AvatarRepository) Ping(ctx context.Context) error {
	ctx, span := r.startSpan(ctx, "AvatarRepository.Ping")
	err := r.pool.Ping(ctx)
	endSpan(span, err)
	return err
}

func (r *AvatarRepository) Create(ctx context.Context, avatar *domain.Avatar) error {
	ctx, span := r.startSpan(ctx, "AvatarRepository.Create",
		attribute.String("avatar.id", avatar.ID),
		attribute.String("user.id", avatar.UserID),
	)

	if avatar.ID == "" {
		avatar.ID = uuid.New().String()
		span.SetAttributes(attribute.String("avatar.id", avatar.ID))
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
	endSpan(span, err)
	return err
}

func (r *AvatarRepository) GetByID(ctx context.Context, id string) (*domain.Avatar, error) {
	ctx, span := r.startSpan(ctx, "AvatarRepository.GetByID", attribute.String("avatar.id", id))
	const q = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
			thumbnail_s3_keys, width, height, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars WHERE id = $1 AND deleted_at IS NULL`

	a, err := r.scanOne(ctx, q, id)
	endSpan(span, err)
	return a, err
}

func (r *AvatarRepository) GetByIDIncludingDeleted(ctx context.Context, id string) (*domain.Avatar, error) {
	ctx, span := r.startSpan(ctx, "AvatarRepository.GetByIDIncludingDeleted", attribute.String("avatar.id", id))
	const q = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
			thumbnail_s3_keys, width, height, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars WHERE id = $1`

	a, err := r.scanOne(ctx, q, id)
	endSpan(span, err)
	return a, err
}

func (r *AvatarRepository) GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error) {
	ctx, span := r.startSpan(ctx, "AvatarRepository.GetLatestByUserID", attribute.String("user.id", userID))
	const q = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
			thumbnail_s3_keys, width, height, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`

	a, err := r.scanOne(ctx, q, userID)
	endSpan(span, err)
	return a, err
}

func (r *AvatarRepository) ListByUserID(ctx context.Context, userID string) ([]*domain.Avatar, error) {
	ctx, span := r.startSpan(ctx, "AvatarRepository.ListByUserID", attribute.String("user.id", userID))
	const q = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
			thumbnail_s3_keys, width, height, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		endSpan(span, err)
		return nil, err
	}
	defer rows.Close()

	var result []*domain.Avatar
	for rows.Next() {
		a, err := scanAvatar(rows)
		if err != nil {
			endSpan(span, err)
			return nil, err
		}
		result = append(result, a)
	}
	err = rows.Err()
	endSpan(span, err)
	return result, err
}

func (r *AvatarRepository) SoftDelete(ctx context.Context, id string) error {
	ctx, span := r.startSpan(ctx, "AvatarRepository.SoftDelete", attribute.String("avatar.id", id))
	now := time.Now().UTC()
	const q = `UPDATE avatars SET deleted_at = $1, updated_at = $1 WHERE id = $2 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, now, id)
	if err != nil {
		endSpan(span, err)
		return err
	}
	if tag.RowsAffected() == 0 {
		err = ErrNotFound
		endSpan(span, err)
		return err
	}
	endSpan(span, nil)
	return nil
}

func (r *AvatarRepository) UpdateUploadStatus(ctx context.Context, id, status string) error {
	ctx, span := r.startSpan(ctx, "AvatarRepository.UpdateUploadStatus",
		attribute.String("avatar.id", id),
		attribute.String("upload.status", status),
	)
	const q = `UPDATE avatars SET upload_status = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.pool.Exec(ctx, q, status, id)
	endSpan(span, err)
	return err
}

func (r *AvatarRepository) UpdateProcessingStatus(ctx context.Context, id, status string) error {
	ctx, span := r.startSpan(ctx, "AvatarRepository.UpdateProcessingStatus",
		attribute.String("avatar.id", id),
		attribute.String("processing.status", status),
	)
	const q = `UPDATE avatars SET processing_status = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.pool.Exec(ctx, q, status, id)
	endSpan(span, err)
	return err
}

func (r *AvatarRepository) UpdateThumbnails(ctx context.Context, id string, thumbs []domain.ThumbnailInfo, width, height int) error {
	ctx, span := r.startSpan(ctx, "AvatarRepository.UpdateThumbnails", attribute.String("avatar.id", id))
	data, err := json.Marshal(thumbs)
	if err != nil {
		endSpan(span, err)
		return err
	}
	const q = `
		UPDATE avatars
		SET thumbnail_s3_keys = $1, width = $2, height = $3,
			processing_status = $4, updated_at = NOW()
		WHERE id = $5`
	_, err = r.pool.Exec(ctx, q, data, width, height, domain.ProcessingStatusCompleted, id)
	endSpan(span, err)
	return err
}

func (r *AvatarRepository) MarkMessageProcessed(ctx context.Context, messageID string) (bool, error) {
	ctx, span := r.startSpan(ctx, "AvatarRepository.MarkMessageProcessed",
		attribute.String("messaging.message_id", messageID),
	)
	const q = `INSERT INTO processed_messages (message_id) VALUES ($1) ON CONFLICT DO NOTHING`
	tag, err := r.pool.Exec(ctx, q, messageID)
	if err != nil {
		endSpan(span, err)
		return false, err
	}
	endSpan(span, nil)
	return tag.RowsAffected() > 0, nil
}

func (r *AvatarRepository) IsMessageProcessed(ctx context.Context, messageID string) (bool, error) {
	ctx, span := r.startSpan(ctx, "AvatarRepository.IsMessageProcessed",
		attribute.String("messaging.message_id", messageID),
	)
	const q = `SELECT EXISTS(SELECT 1 FROM processed_messages WHERE message_id = $1)`
	var exists bool
	err := r.pool.QueryRow(ctx, q, messageID).Scan(&exists)
	endSpan(span, err)
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
