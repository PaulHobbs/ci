package service

import (
	"context"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
	"github.com/example/turboci-lite/pkg/id"
)

// RunnerService manages stage runner registrations.
type RunnerService struct {
	storage storage.Storage
}

// NewRunnerService creates a new runner service.
func NewRunnerService(store storage.Storage) *RunnerService {
	return &RunnerService{storage: store}
}

// RegisterRunnerRequest is the request for RegisterRunner.
type RegisterRunnerRequest struct {
	RunnerID       string
	RunnerType     string
	Address        string
	SupportedModes []domain.ExecutionMode
	MaxConcurrent  int
	TTLSeconds     int64
	Metadata       map[string]string
}

// RegisterRunnerResponse is the response from RegisterRunner.
type RegisterRunnerResponse struct {
	RegistrationID string
	ExpiresAt      time.Time
}

// RegisterRunner registers a stage runner with the orchestrator.
func (s *RunnerService) RegisterRunner(ctx context.Context, req *RegisterRunnerRequest) (*RegisterRunnerResponse, error) {
	if req.RunnerID == "" {
		return nil, domain.ErrInvalidArgument
	}
	if req.RunnerType == "" {
		return nil, domain.ErrInvalidArgument
	}
	if req.Address == "" {
		return nil, domain.ErrInvalidArgument
	}
	if len(req.SupportedModes) == 0 {
		req.SupportedModes = []domain.ExecutionMode{domain.ExecutionModeSync}
	}
	if req.TTLSeconds <= 0 {
		req.TTLSeconds = 300 // Default 5 minutes
	}

	ttl := time.Duration(req.TTLSeconds) * time.Second

	runner := &domain.StageRunner{
		ID:             req.RunnerID,
		RegistrationID: id.Generate(),
		RunnerType:     req.RunnerType,
		Address:        req.Address,
		SupportedModes: req.SupportedModes,
		MaxConcurrent:  req.MaxConcurrent,
		CurrentLoad:    0,
		Metadata:       req.Metadata,
		RegisteredAt:   time.Now().UTC(),
		LastHeartbeat:  time.Now().UTC(),
		ExpiresAt:      time.Now().UTC().Add(ttl),
	}

	if runner.Metadata == nil {
		runner.Metadata = make(map[string]string)
	}

	uow, err := s.storage.BeginImmediate(ctx)
	if err != nil {
		return nil, err
	}
	defer uow.Rollback()

	if err := uow.StageRunners().Register(ctx, runner); err != nil {
		return nil, err
	}

	if err := uow.Commit(); err != nil {
		return nil, err
	}

	return &RegisterRunnerResponse{
		RegistrationID: runner.RegistrationID,
		ExpiresAt:      runner.ExpiresAt,
	}, nil
}

// UnregisterRunner removes a runner registration.
func (s *RunnerService) UnregisterRunner(ctx context.Context, registrationID string) error {
	if registrationID == "" {
		return domain.ErrInvalidArgument
	}

	uow, err := s.storage.BeginImmediate(ctx)
	if err != nil {
		return err
	}
	defer uow.Rollback()

	if err := uow.StageRunners().Unregister(ctx, registrationID); err != nil {
		return err
	}

	return uow.Commit()
}

// ListRunnersRequest is the request for ListRunners.
type ListRunnersRequest struct {
	RunnerType string // Optional filter
}

// ListRunners returns all registered runners.
func (s *RunnerService) ListRunners(ctx context.Context, req *ListRunnersRequest) ([]*domain.StageRunner, error) {
	uow, err := s.storage.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer uow.Rollback()

	var runners []*domain.StageRunner
	if req != nil && req.RunnerType != "" {
		runners, err = uow.StageRunners().GetByType(ctx, req.RunnerType)
	} else {
		runners, err = uow.StageRunners().List(ctx)
	}
	if err != nil {
		return nil, err
	}

	return runners, nil
}

// Heartbeat refreshes a runner's registration.
func (s *RunnerService) Heartbeat(ctx context.Context, registrationID string, ttl time.Duration) error {
	if registrationID == "" {
		return domain.ErrInvalidArgument
	}

	uow, err := s.storage.BeginImmediate(ctx)
	if err != nil {
		return err
	}
	defer uow.Rollback()

	newExpiry := time.Now().UTC().Add(ttl)
	if err := uow.StageRunners().UpdateHeartbeat(ctx, registrationID, newExpiry); err != nil {
		return err
	}

	return uow.Commit()
}

// SelectRunner selects an available runner for a given type and mode using the provided UnitOfWork.
// Returns the runner with the lowest current load.
func (s *RunnerService) SelectRunner(ctx context.Context, uow storage.UnitOfWork, runnerType string, mode domain.ExecutionMode) (*domain.StageRunner, error) {
	runners, err := uow.StageRunners().GetAvailable(ctx, runnerType, mode)
	if err != nil {
		return nil, err
	}

	if len(runners) == 0 {
		return nil, domain.ErrRunnerNotFound
	}

	// Runners are already sorted by load (lowest first)
	return runners[0], nil
}

// CleanupExpired removes expired runner registrations.
func (s *RunnerService) CleanupExpired(ctx context.Context) (int, error) {
	uow, err := s.storage.BeginImmediate(ctx)
	if err != nil {
		return 0, err
	}
	defer uow.Rollback()

	count, err := uow.StageRunners().CleanupExpired(ctx)
	if err != nil {
		return 0, err
	}

	if err := uow.Commit(); err != nil {
		return 0, err
	}

	return count, nil
}
