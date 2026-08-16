package services

import (
	"context"

	"github.com/practicum/gophprofile/internal/domain"
	"github.com/practicum/gophprofile/internal/observability"
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
	logger := observability.LoggerFromContext(ctx)

	if err := s.repo.Ping(ctx); err != nil {
		logger.Error("health postgres unavailable", "error", err)
		components["postgres"] = "unavailable"
		status = "degraded"
		observability.HealthChecks.WithLabelValues("postgres").Set(0)
	} else {
		observability.HealthChecks.WithLabelValues("postgres").Set(1)
	}
	if err := s.storage.Ping(ctx); err != nil {
		logger.Error("health s3 unavailable", "error", err)
		components["s3"] = "unavailable"
		status = "degraded"
		observability.HealthChecks.WithLabelValues("s3").Set(0)
	} else {
		observability.HealthChecks.WithLabelValues("s3").Set(1)
	}
	if err := s.broker.Ping(ctx); err != nil {
		logger.Error("health broker unavailable", "error", err)
		components["broker"] = "unavailable"
		status = "degraded"
		observability.HealthChecks.WithLabelValues("broker").Set(0)
	} else {
		observability.HealthChecks.WithLabelValues("broker").Set(1)
	}

	return domain.HealthResponse{
		Status:     status,
		Components: components,
	}
}
