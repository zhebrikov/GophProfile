package services_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"testing"
	"time"

	"github.com/practicum/gophprofile/internal/domain"
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/practicum/gophprofile/internal/services"
	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/stretchr/testify/require"
)

type memRepo struct {
	avatars  map[string]*domain.Avatar
	byUser   map[string][]string
	failPing bool
}

func newMemRepo() *memRepo {
	return &memRepo{avatars: map[string]*domain.Avatar{}, byUser: map[string][]string{}}
}

func (r *memRepo) Create(_ context.Context, a *domain.Avatar) error {
	cp := *a
	r.avatars[a.ID] = &cp
	r.byUser[a.UserID] = append(r.byUser[a.UserID], a.ID)
	return nil
}
func (r *memRepo) GetByID(_ context.Context, id string) (*domain.Avatar, error) {
	a, ok := r.avatars[id]
	if !ok || a.DeletedAt != nil {
		return nil, repository.ErrNotFound
	}
	cp := *a
	return &cp, nil
}
func (r *memRepo) GetLatestByUserID(_ context.Context, userID string) (*domain.Avatar, error) {
	ids := r.byUser[userID]
	for i := len(ids) - 1; i >= 0; i-- {
		a := r.avatars[ids[i]]
		if a != nil && a.DeletedAt == nil {
			cp := *a
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r *memRepo) ListByUserID(ctx context.Context, userID string) ([]*domain.Avatar, error) {
	var out []*domain.Avatar
	for _, id := range r.byUser[userID] {
		a := r.avatars[id]
		if a != nil && a.DeletedAt == nil {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (r *memRepo) SoftDelete(_ context.Context, id, userID string) (*domain.Avatar, error) {
	a, ok := r.avatars[id]
	if !ok || a.DeletedAt != nil {
		return nil, repository.ErrNotFound
	}
	if a.UserID != userID {
		return nil, repository.ErrForbidden
	}
	now := time.Now().UTC()
	a.DeletedAt = &now
	cp := *a
	return &cp, nil
}
func (r *memRepo) SoftDeleteLatestByUser(ctx context.Context, userID, requesterID string) (*domain.Avatar, error) {
	if userID != requesterID {
		return nil, repository.ErrForbidden
	}
	a, err := r.GetLatestByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return r.SoftDelete(ctx, a.ID, requesterID)
}
func (r *memRepo) UpdateUploadStatus(_ context.Context, id, status string) error {
	a, ok := r.avatars[id]
	if !ok {
		return repository.ErrNotFound
	}
	a.UploadStatus = status
	return nil
}
func (r *memRepo) Ping(context.Context) error {
	if r.failPing {
		return errors.New("db down")
	}
	return nil
}

type memStorage struct {
	objects  map[string][]byte
	types    map[string]string
	failPing bool
}

func newMemStorage() *memStorage {
	return &memStorage{objects: map[string][]byte{}, types: map[string]string{}}
}
func (s *memStorage) Upload(_ context.Context, key string, reader io.Reader, size int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.objects[key] = data
	s.types[key] = contentType
	return nil
}
func (s *memStorage) Download(_ context.Context, key string) (io.ReadCloser, string, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, "", errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), s.types[key], nil
}
func (s *memStorage) Ping(context.Context) error {
	if s.failPing {
		return errors.New("s3 down")
	}
	return nil
}

type memBroker struct {
	events   []any
	failPing bool
}

func (b *memBroker) Publish(_ context.Context, routingKey string, payload any) error {
	b.events = append(b.events, payload)
	return nil
}
func (b *memBroker) Ping(context.Context) error {
	if b.failPing {
		return errors.New("broker down")
	}
	return nil
}

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}

func TestUploadAndGetMetadata(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	mq := &memBroker{}
	svc := services.NewAvatarService(repo, store, mq, "http://localhost:8080", 10<<20)

	resp, err := svc.Upload(context.Background(), "user-1", "avatar.jpg", bytes.NewReader(makeJPEG(t, 40, 40)))
	require.NoError(t, err)
	require.Equal(t, "processing", resp.Status)
	require.NotEmpty(t, resp.ID)
	require.Len(t, mq.events, 1)
	require.Equal(t, broker.RoutingKeyUploaded, "avatar.uploaded")

	meta, err := svc.GetMetadata(context.Background(), resp.ID)
	require.NoError(t, err)
	require.Equal(t, "user-1", meta.UserID)
	require.Equal(t, "avatar.jpg", meta.FileName)

	data, mime, etag, err := svc.GetImage(context.Background(), resp.ID, "original", "")
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", mime)
	require.NotEmpty(t, data)
	require.NotEmpty(t, etag)

	list, err := svc.ListByUser(context.Background(), "user-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestUploadValidation(t *testing.T) {
	svc := services.NewAvatarService(newMemRepo(), newMemStorage(), &memBroker{}, "http://localhost", 100)

	_, err := svc.Upload(context.Background(), "", "a.jpg", bytes.NewReader(makeJPEG(t, 10, 10)))
	require.ErrorIs(t, err, services.ErrInvalidUserID)

	_, err = svc.Upload(context.Background(), "u1", "a.bin", bytes.NewReader([]byte("not-image")))
	require.ErrorIs(t, err, services.ErrInvalidFormat)

	big := bytes.Repeat([]byte("a"), 200)
	_, err = svc.Upload(context.Background(), "u1", "a.jpg", bytes.NewReader(big))
	require.ErrorIs(t, err, services.ErrFileTooLarge)
}

func TestDeleteAndPlaceholder(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	mq := &memBroker{}
	svc := services.NewAvatarService(repo, store, mq, "http://localhost:8080", 10<<20)

	resp, err := svc.Upload(context.Background(), "user-1", "a.jpg", bytes.NewReader(makeJPEG(t, 20, 20)))
	require.NoError(t, err)

	err = svc.Delete(context.Background(), resp.ID, "other")
	require.ErrorIs(t, err, repository.ErrForbidden)

	err = svc.Delete(context.Background(), resp.ID, "user-1")
	require.NoError(t, err)
	require.Len(t, mq.events, 2)

	data, mime, _, err := svc.GetUserAvatar(context.Background(), "missing", "", "")
	require.NoError(t, err)
	require.Equal(t, "image/png", mime)
	require.NotEmpty(t, data)
}

func TestDeleteUserAvatar(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	mq := &memBroker{}
	svc := services.NewAvatarService(repo, store, mq, "http://localhost:8080", 10<<20)

	_, err := svc.Upload(context.Background(), "user-1", "a.jpg", bytes.NewReader(makeJPEG(t, 20, 20)))
	require.NoError(t, err)

	err = svc.DeleteUserAvatar(context.Background(), "user-1", "user-1")
	require.NoError(t, err)
}

func TestGetImageWithThumbnailFallback(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	svc := services.NewAvatarService(repo, store, &memBroker{}, "http://localhost:8080", 10<<20)

	resp, err := svc.Upload(context.Background(), "user-1", "a.jpg", bytes.NewReader(makeJPEG(t, 50, 50)))
	require.NoError(t, err)

	// thumbnails not ready yet -> falls back to original
	data, _, _, err := svc.GetImage(context.Background(), resp.ID, "100x100", "")
	require.NoError(t, err)
	require.NotEmpty(t, data)

	data, mime, _, err := svc.GetUserAvatar(context.Background(), "user-1", "original", "png")
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.NotEmpty(t, mime)
}

func TestHealthService(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	mq := &memBroker{}
	hs := services.NewHealthService(repo, store, mq)
	resp := hs.Check(context.Background())
	require.Equal(t, "ok", resp.Status)

	repo.failPing = true
	store.failPing = true
	mq.failPing = true
	resp = hs.Check(context.Background())
	require.Equal(t, "degraded", resp.Status)
}

func TestDefaultPlaceholder(t *testing.T) {
	a, mime := services.DefaultPlaceholder()
	b, mime2 := services.DefaultPlaceholder()
	require.Equal(t, "image/png", mime)
	require.Equal(t, mime, mime2)
	require.Equal(t, a, b)
	require.NotEmpty(t, a)
}

func TestMaxFileSize(t *testing.T) {
	svc := services.NewAvatarService(newMemRepo(), newMemStorage(), &memBroker{}, "http://x", 123)
	require.Equal(t, int64(123), svc.MaxFileSize())
}
