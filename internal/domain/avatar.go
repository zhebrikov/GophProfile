package domain

import (
	"encoding/json"
	"time"
)

const (
	UploadStatusUploading = "uploading"
	UploadStatusUploaded  = "uploaded"
	UploadStatusFailed    = "failed"

	ProcessingStatusPending    = "pending"
	ProcessingStatusProcessing = "processing"
	ProcessingStatusCompleted  = "completed"
	ProcessingStatusFailed     = "failed"

	SizeOriginal = "original"
	Size100      = "100x100"
	Size300      = "300x300"
)

type Dimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ThumbnailInfo struct {
	Size string `json:"size"`
	Key  string `json:"key"`
	URL  string `json:"url,omitempty"`
}

type Avatar struct {
	ID               string          `json:"id"`
	UserID           string          `json:"user_id"`
	FileName         string          `json:"file_name"`
	MimeType         string          `json:"mime_type"`
	SizeBytes        int64           `json:"size_bytes"`
	S3Key            string          `json:"s3_key"`
	ThumbnailS3Keys  json.RawMessage `json:"thumbnail_s3_keys,omitempty"`
	Width            int             `json:"width"`
	Height           int             `json:"height"`
	UploadStatus     string          `json:"upload_status"`
	ProcessingStatus string          `json:"processing_status"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	DeletedAt        *time.Time      `json:"deleted_at,omitempty"`
}

type UploadResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type MetadataResponse struct {
	ID         string          `json:"id"`
	UserID     string          `json:"user_id"`
	FileName   string          `json:"file_name"`
	MimeType   string          `json:"mime_type"`
	Size       int64           `json:"size"`
	Dimensions Dimensions      `json:"dimensions"`
	Thumbnails []ThumbnailInfo `json:"thumbnails"`
	Status     string          `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
	MaxSize int64  `json:"max_size,omitempty"`
}

type HealthResponse struct {
	Status     string            `json:"status"`
	Components map[string]string `json:"components"`
}

type AvatarUploadEvent struct {
	MessageID string `json:"message_id"`
	AvatarID  string `json:"avatar_id"`
	UserID    string `json:"user_id"`
	S3Key     string `json:"s3_key"`
}

type ProcessingOp struct {
	Type string `json:"type"`
	Size string `json:"size,omitempty"`
}

type AvatarProcessEvent struct {
	MessageID  string         `json:"message_id"`
	AvatarID   string         `json:"avatar_id"`
	Operations []ProcessingOp `json:"operations"`
}

type AvatarDeleteEvent struct {
	MessageID string   `json:"message_id"`
	AvatarID  string   `json:"avatar_id"`
	S3Keys    []string `json:"s3_keys"`
}

func (a *Avatar) Thumbnails() ([]ThumbnailInfo, error) {
	if len(a.ThumbnailS3Keys) == 0 || string(a.ThumbnailS3Keys) == "null" {
		return nil, nil
	}
	var thumbs []ThumbnailInfo
	if err := json.Unmarshal(a.ThumbnailS3Keys, &thumbs); err != nil {
		return nil, err
	}
	return thumbs, nil
}

func (a *Avatar) SetThumbnails(thumbs []ThumbnailInfo) error {
	data, err := json.Marshal(thumbs)
	if err != nil {
		return err
	}
	a.ThumbnailS3Keys = data
	return nil
}
