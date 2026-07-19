package services

import (
	"context"

	"github.com/practicum/gophprofile/internal/domain"
)

type HealthService struct {
	repo    AvatarRepository
	storage ObjectStorage
	broker  EventPublisher
}

func NewHealthService(repo AvatarRepository, store ObjectStorage, mq EventPublisher) *HealthService {
	return &HealthService{repo: repo, storage: store, broker: mq}
}

func (s *HealthService) Check(ctx context.Context) domain.HealthResponse {
	components := map[string]string{
		"postgres": "ok",
		"s3":       "ok",
		"broker":   "ok",
	}
	status := "ok"

	if err := s.repo.Ping(ctx); err != nil {
		components["postgres"] = "error: " + err.Error()
		status = "degraded"
	}
	if err := s.storage.Ping(ctx); err != nil {
		components["s3"] = "error: " + err.Error()
		status = "degraded"
	}
	if err := s.broker.Ping(ctx); err != nil {
		components["broker"] = "error: " + err.Error()
		status = "degraded"
	}

	return domain.HealthResponse{
		Status:     status,
		Components: components,
	}
}
