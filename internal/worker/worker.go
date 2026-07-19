package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/practicum/gophprofile/internal/domain"
	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/practicum/gophprofile/pkg/imageutil"
)

type AvatarRepository interface {
	GetByIDIncludingDeleted(ctx context.Context, id string) (*domain.Avatar, error)
	UpdateProcessingStatus(ctx context.Context, id, status string) error
	UpdateThumbnails(ctx context.Context, id string, thumbs []domain.ThumbnailInfo, width, height int) error
	MarkMessageProcessed(ctx context.Context, messageID string) (bool, error)
	IsMessageProcessed(ctx context.Context, messageID string) (bool, error)
}

type ObjectStorage interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, string, error)
	DeleteMany(ctx context.Context, keys []string) error
}

type MessageBus interface {
	SetupQueues() error
	Consume(ctx context.Context, queue string, handler broker.MessageHandler) error
}

type Worker struct {
	repo    AvatarRepository
	storage ObjectStorage
	broker  MessageBus
}

func New(repo AvatarRepository, store ObjectStorage, mq MessageBus) *Worker {
	return &Worker{repo: repo, storage: store, broker: mq}
}

func (w *Worker) Start(ctx context.Context) error {
	if err := w.broker.SetupQueues(); err != nil {
		return err
	}

	if err := w.broker.Consume(ctx, broker.QueueProcess, w.handleUploadMessage); err != nil {
		return err
	}
	if err := w.broker.Consume(ctx, broker.QueueDelete, w.handleDeleteMessage); err != nil {
		return err
	}

	log.Println("worker: consuming queues")
	<-ctx.Done()
	return ctx.Err()
}

func (w *Worker) handleUploadMessage(ctx context.Context, body []byte) error {
	var event domain.AvatarUploadEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}
	return broker.RetryWithBackoff(ctx, 5, time.Second, func() error {
		return w.HandleUploadEvent(ctx, event)
	})
}

func (w *Worker) handleDeleteMessage(ctx context.Context, body []byte) error {
	var event domain.AvatarDeleteEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}
	return broker.RetryWithBackoff(ctx, 5, time.Second, func() error {
		return w.HandleDeleteEvent(ctx, event)
	})
}

func (w *Worker) HandleUploadEvent(ctx context.Context, event domain.AvatarUploadEvent) error {
	if event.MessageID != "" {
		done, err := w.repo.IsMessageProcessed(ctx, event.MessageID)
		if err != nil {
			return err
		}
		if done {
			log.Printf("worker: skip duplicate message %s", event.MessageID)
			return nil
		}
	}

	avatar, err := w.repo.GetByIDIncludingDeleted(ctx, event.AvatarID)
	if err != nil {
		return err
	}
	if avatar.DeletedAt != nil {
		return nil
	}
	if avatar.ProcessingStatus == domain.ProcessingStatusCompleted {
		if event.MessageID != "" {
			_, _ = w.repo.MarkMessageProcessed(ctx, event.MessageID)
		}
		return nil
	}

	if err := w.repo.UpdateProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusProcessing); err != nil {
		return err
	}

	rc, _, err := w.storage.Download(ctx, event.S3Key)
	if err != nil {
		_ = w.repo.UpdateProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusFailed)
		return err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		_ = w.repo.UpdateProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusFailed)
		return err
	}

	img, _, err := imageutil.Decode(data)
	if err != nil {
		_ = w.repo.UpdateProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusFailed)
		// Permanent failure: ack message so it does not poison the queue.
		return nil
	}

	thumbs := make([]domain.ThumbnailInfo, 0, 2)
	for _, size := range []struct {
		name string
		px   int
	}{
		{domain.Size100, 100},
		{domain.Size300, 300},
	} {
		thumbImg := imageutil.Resize(img, size.px, size.px)
		thumbData, err := imageutil.EncodeJPEG(thumbImg, 85)
		if err != nil {
			_ = w.repo.UpdateProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusFailed)
			return err
		}
		key := fmt.Sprintf("thumbnails/%s/%s.jpg", event.AvatarID, size.name)
		if err := w.storage.Upload(ctx, key, bytes.NewReader(thumbData), int64(len(thumbData)), "image/jpeg"); err != nil {
			_ = w.repo.UpdateProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusFailed)
			return err
		}
		thumbs = append(thumbs, domain.ThumbnailInfo{Size: size.name, Key: key})
	}

	bounds := img.Bounds()
	if err := w.repo.UpdateThumbnails(ctx, event.AvatarID, thumbs, bounds.Dx(), bounds.Dy()); err != nil {
		return err
	}
	if event.MessageID != "" {
		_, err = w.repo.MarkMessageProcessed(ctx, event.MessageID)
	}
	return err
}

func (w *Worker) HandleDeleteEvent(ctx context.Context, event domain.AvatarDeleteEvent) error {
	msgID := ""
	if event.MessageID != "" {
		msgID = "delete:" + event.MessageID
		done, err := w.repo.IsMessageProcessed(ctx, msgID)
		if err != nil {
			return err
		}
		if done {
			log.Printf("worker: skip duplicate delete %s", event.MessageID)
			return nil
		}
	}
	if err := w.storage.DeleteMany(ctx, event.S3Keys); err != nil {
		return err
	}
	if msgID != "" {
		_, err := w.repo.MarkMessageProcessed(ctx, msgID)
		return err
	}
	return nil
}
