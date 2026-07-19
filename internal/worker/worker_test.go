package worker_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"testing"
	"time"

	"github.com/practicum/gophprofile/internal/domain"
	"github.com/practicum/gophprofile/internal/worker"
	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/stretchr/testify/require"
)

type fakeRepo struct {
	avatar    *domain.Avatar
	processed map[string]bool
	thumbs    []domain.ThumbnailInfo
	statusLog []string
}

func (r *fakeRepo) GetByIDIncludingDeleted(context.Context, string) (*domain.Avatar, error) {
	cp := *r.avatar
	return &cp, nil
}
func (r *fakeRepo) UpdateProcessingStatus(_ context.Context, _ string, status string) error {
	r.statusLog = append(r.statusLog, status)
	r.avatar.ProcessingStatus = status
	return nil
}
func (r *fakeRepo) UpdateThumbnails(_ context.Context, _ string, thumbs []domain.ThumbnailInfo, width, height int) error {
	r.thumbs = thumbs
	r.avatar.Width = width
	r.avatar.Height = height
	r.avatar.ProcessingStatus = domain.ProcessingStatusCompleted
	_ = r.avatar.SetThumbnails(thumbs)
	return nil
}
func (r *fakeRepo) MarkMessageProcessed(_ context.Context, messageID string) (bool, error) {
	if r.processed == nil {
		r.processed = map[string]bool{}
	}
	if r.processed[messageID] {
		return false, nil
	}
	r.processed[messageID] = true
	return true, nil
}
func (r *fakeRepo) IsMessageProcessed(_ context.Context, messageID string) (bool, error) {
	return r.processed[messageID], nil
}

type fakeStorage struct {
	objects map[string][]byte
}

func (s *fakeStorage) Upload(_ context.Context, key string, reader io.Reader, size int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if s.objects == nil {
		s.objects = map[string][]byte{}
	}
	s.objects[key] = data
	return nil
}
func (s *fakeStorage) Download(_ context.Context, key string) (io.ReadCloser, string, error) {
	return io.NopCloser(bytes.NewReader(s.objects[key])), "image/jpeg", nil
}
func (s *fakeStorage) DeleteMany(_ context.Context, keys []string) error {
	for _, k := range keys {
		delete(s.objects, k)
	}
	return nil
}

type noopBus struct{}

func (noopBus) SetupQueues() error { return nil }
func (noopBus) Consume(context.Context, string, broker.MessageHandler) error {
	return nil
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 120, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}

func TestHandleUploadEvent(t *testing.T) {
	data := jpegBytes(t)
	store := &fakeStorage{objects: map[string][]byte{"originals/u/a.jpg": data}}
	repo := &fakeRepo{
		avatar: &domain.Avatar{
			ID:               "avatar-1",
			UserID:           "u",
			S3Key:            "originals/u/a.jpg",
			ProcessingStatus: domain.ProcessingStatusPending,
		},
		processed: map[string]bool{},
	}
	w := worker.New(repo, store, noopBus{})

	err := w.HandleUploadEvent(context.Background(), domain.AvatarUploadEvent{
		MessageID: "msg-1",
		AvatarID:  "avatar-1",
		UserID:    "u",
		S3Key:     "originals/u/a.jpg",
	})
	require.NoError(t, err)
	require.Equal(t, domain.ProcessingStatusCompleted, repo.avatar.ProcessingStatus)
	require.Len(t, repo.thumbs, 2)
	require.Contains(t, store.objects, "thumbnails/avatar-1/100x100.jpg")
	require.Contains(t, store.objects, "thumbnails/avatar-1/300x300.jpg")

	// idempotent
	err = w.HandleUploadEvent(context.Background(), domain.AvatarUploadEvent{
		MessageID: "msg-1",
		AvatarID:  "avatar-1",
		S3Key:     "originals/u/a.jpg",
	})
	require.NoError(t, err)
}

func TestHandleUploadEvent_AlreadyCompleted(t *testing.T) {
	repo := &fakeRepo{
		avatar: &domain.Avatar{
			ID:               "a",
			ProcessingStatus: domain.ProcessingStatusCompleted,
		},
		processed: map[string]bool{},
	}
	w := worker.New(repo, &fakeStorage{}, noopBus{})
	err := w.HandleUploadEvent(context.Background(), domain.AvatarUploadEvent{
		MessageID: "m",
		AvatarID:  "a",
	})
	require.NoError(t, err)
	require.True(t, repo.processed["m"])
}

func TestHandleUploadEvent_Deleted(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{
		avatar: &domain.Avatar{
			ID:        "a",
			DeletedAt: &now,
		},
	}
	w := worker.New(repo, &fakeStorage{}, noopBus{})
	err := w.HandleUploadEvent(context.Background(), domain.AvatarUploadEvent{AvatarID: "a"})
	require.NoError(t, err)
}

func TestHandleDeleteEvent(t *testing.T) {
	store := &fakeStorage{objects: map[string][]byte{"a": {1}, "b": {2}}}
	repo := &fakeRepo{processed: map[string]bool{}}
	w := worker.New(repo, store, noopBus{})

	err := w.HandleDeleteEvent(context.Background(), domain.AvatarDeleteEvent{
		MessageID: "d1",
		AvatarID:  "a",
		S3Keys:    []string{"a", "b"},
	})
	require.NoError(t, err)
	require.Empty(t, store.objects)

	err = w.HandleDeleteEvent(context.Background(), domain.AvatarDeleteEvent{
		MessageID: "d1",
		S3Keys:    []string{"a"},
	})
	require.NoError(t, err)
}
