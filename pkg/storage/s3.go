package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type S3Storage struct {
	client         *minio.Client
	bucket         string
	publicEndpoint string
}

func NewS3Storage(endpoint, accessKey, secretKey, bucket string, useSSL bool, publicEndpoint string) (*S3Storage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	s := &S3Storage{
		client:         client,
		bucket:         bucket,
		publicEndpoint: strings.TrimRight(publicEndpoint, "/"),
	}
	return s, nil
}

func (s *S3Storage) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
	}
	return nil
}

func (s *S3Storage) Ping(ctx context.Context) error {
	_, err := s.client.BucketExists(ctx, s.bucket)
	return err
}

func (s *S3Storage) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	ctx, span := otel.Tracer("gophprofile/storage").Start(ctx, "S3.Upload")
	defer span.End()
	span.SetAttributes(attribute.String("s3.key", key), attribute.Int64("s3.size", size))

	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (s *S3Storage) Download(ctx context.Context, key string) (io.ReadCloser, string, error) {
	ctx, span := otel.Tracer("gophprofile/storage").Start(ctx, "S3.Download")
	defer span.End()
	span.SetAttributes(attribute.String("s3.key", key))

	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, "", err
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, "", err
	}
	return obj, info.ContentType, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	ctx, span := otel.Tracer("gophprofile/storage").Start(ctx, "S3.Delete")
	defer span.End()
	span.SetAttributes(attribute.String("s3.key", key))

	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (s *S3Storage) DeleteMany(ctx context.Context, keys []string) error {
	ctx, span := otel.Tracer("gophprofile/storage").Start(ctx, "S3.DeleteMany")
	defer span.End()
	span.SetAttributes(attribute.Int("s3.keys_count", len(keys)))

	for _, key := range keys {
		if key == "" {
			continue
		}
		if err := s.Delete(ctx, key); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	return nil
}

func (s *S3Storage) PublicURL(key string) string {
	return fmt.Sprintf("%s/%s/%s", s.publicEndpoint, s.bucket, key)
}

func (s *S3Storage) ObjectURL(key string) string {
	return fmt.Sprintf("/api/v1/files/%s", key)
}
