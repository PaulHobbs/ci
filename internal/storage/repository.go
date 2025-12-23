package storage

import (
	"context"
	"time"

	"github.com/example/turboci-lite/internal/domain"
)

// ListOptions provides filtering options for list operations.
type ListOptions struct {
	// IDs to filter by (empty = all)
	IDs []string

	// States to filter by (empty = all)
	CheckStates []domain.CheckState
	StageStates []domain.StageState

	// Pagination
	Limit  int
	Offset int
}

// WorkPlanRepository provides access to WorkPlan storage.
type WorkPlanRepository interface {
	// Create creates a new WorkPlan.
	Create(ctx context.Context, wp *domain.WorkPlan) error

	// Get retrieves a WorkPlan by ID.
	Get(ctx context.Context, id string) (*domain.WorkPlan, error)

	// Update updates an existing WorkPlan.
	Update(ctx context.Context, wp *domain.WorkPlan) error

	// Delete deletes a WorkPlan by ID.
	Delete(ctx context.Context, id string) error
}

// CheckRepository provides access to Check storage.
type CheckRepository interface {
	// Create creates a new Check.
	Create(ctx context.Context, check *domain.Check) error

	// Get retrieves a Check by WorkPlan ID and Check ID.
	Get(ctx context.Context, workPlanID, checkID string) (*domain.Check, error)

	// Update updates an existing Check.
	Update(ctx context.Context, check *domain.Check) error

	// List lists Checks in a WorkPlan with optional filtering.
	List(ctx context.Context, workPlanID string, opts ListOptions) ([]*domain.Check, error)

	// Delete deletes a Check.
	Delete(ctx context.Context, workPlanID, checkID string) error

	// AddResult adds a result to a Check.
	AddResult(ctx context.Context, workPlanID, checkID string, result *domain.CheckResult) error

	// UpdateResult updates an existing result.
	UpdateResult(ctx context.Context, workPlanID, checkID string, resultID int64, result *domain.CheckResult) error
}

// StageRepository provides access to Stage storage.
type StageRepository interface {
	// Create creates a new Stage.
	Create(ctx context.Context, stage *domain.Stage) error

	// Get retrieves a Stage by WorkPlan ID and Stage ID.
	Get(ctx context.Context, workPlanID, stageID string) (*domain.Stage, error)

	// Update updates an existing Stage.
	Update(ctx context.Context, stage *domain.Stage) error

	// List lists Stages in a WorkPlan with optional filtering.
	List(ctx context.Context, workPlanID string, opts ListOptions) ([]*domain.Stage, error)

	// Delete deletes a Stage.
	Delete(ctx context.Context, workPlanID, stageID string) error

	// AddAttempt adds an attempt to a Stage.
	AddAttempt(ctx context.Context, workPlanID, stageID string, attempt *domain.Attempt) error

	// UpdateAttempt updates an existing attempt.
	UpdateAttempt(ctx context.Context, workPlanID, stageID string, idx int, attempt *domain.Attempt) error
}

// DependencyRepository provides access to Dependency storage.
type DependencyRepository interface {
	// Create creates a new Dependency.
	Create(ctx context.Context, dep *domain.Dependency) error

	// CreateBatch creates multiple dependencies.
	CreateBatch(ctx context.Context, deps []*domain.Dependency) error

	// GetBySource retrieves dependencies where the given node is the source.
	GetBySource(ctx context.Context, workPlanID string, sourceType domain.NodeType, sourceID string) ([]*domain.Dependency, error)

	// GetByTarget retrieves dependencies where the given node is the target.
	GetByTarget(ctx context.Context, workPlanID string, targetType domain.NodeType, targetID string) ([]*domain.Dependency, error)

	// MarkResolved marks a dependency as resolved.
	MarkResolved(ctx context.Context, id int64, satisfied bool) error

	// GetUnresolvedByTarget gets unresolved dependencies pointing to a target.
	GetUnresolvedByTarget(ctx context.Context, workPlanID string, targetType domain.NodeType, targetID string) ([]*domain.Dependency, error)
}

// StageRunnerRepository provides access to runner registrations.
type StageRunnerRepository interface {
	// Register creates or updates a runner registration.
	Register(ctx context.Context, runner *domain.StageRunner) error

	// Get retrieves a runner by registration ID.
	Get(ctx context.Context, registrationID string) (*domain.StageRunner, error)

	// GetByType returns all runners for a given type.
	GetByType(ctx context.Context, runnerType string) ([]*domain.StageRunner, error)

	// GetAvailable returns available runners for a type with capacity and mode support.
	GetAvailable(ctx context.Context, runnerType string, mode domain.ExecutionMode) ([]*domain.StageRunner, error)

	// Unregister removes a runner registration.
	Unregister(ctx context.Context, registrationID string) error

	// UpdateHeartbeat refreshes the heartbeat and expiry.
	UpdateHeartbeat(ctx context.Context, registrationID string, newExpiry time.Time) error

	// IncrementLoad increases the current load counter.
	IncrementLoad(ctx context.Context, registrationID string) error

	// DecrementLoad decreases the current load counter.
	DecrementLoad(ctx context.Context, registrationID string) error

	// CleanupExpired removes expired registrations.
	CleanupExpired(ctx context.Context) (int, error)

	// List returns all registered runners.
	List(ctx context.Context) ([]*domain.StageRunner, error)
}

// StageExecutionRepository provides access to the execution queue.
type StageExecutionRepository interface {
	// Create adds a new execution to the queue.
	Create(ctx context.Context, exec *domain.StageExecution) error

	// Get retrieves an execution by ID.
	Get(ctx context.Context, executionID string) (*domain.StageExecution, error)

	// GetByStageAttempt retrieves execution for a specific attempt.
	GetByStageAttempt(ctx context.Context, workPlanID, stageID string, attemptIdx int) (*domain.StageExecution, error)

	// Update updates an execution.
	Update(ctx context.Context, exec *domain.StageExecution) error

	// GetPending returns pending executions for a runner type, ordered by creation time.
	GetPending(ctx context.Context, runnerType string, limit int) ([]*domain.StageExecution, error)

	// MarkDispatched transitions execution to dispatched state.
	MarkDispatched(ctx context.Context, executionID string, runnerID string) error

	// MarkRunning transitions execution to running state.
	MarkRunning(ctx context.Context, executionID string) error

	// MarkComplete transitions execution to complete state.
	MarkComplete(ctx context.Context, executionID string) error

	// MarkFailed transitions execution to failed state.
	MarkFailed(ctx context.Context, executionID string, errorMsg string) error

	// UpdateProgress updates progress information.
	UpdateProgress(ctx context.Context, executionID string, percent int, message string) error

	// GetTimedOut returns executions that have exceeded their deadline.
	GetTimedOut(ctx context.Context) ([]*domain.StageExecution, error)

	// GetStale returns dispatched executions with no progress update within duration.
	GetStale(ctx context.Context, staleDuration time.Duration) ([]*domain.StageExecution, error)
}

// UnitOfWork provides transactional access to all repositories.
type UnitOfWork interface {
	// Repository accessors
	WorkPlans() WorkPlanRepository
	Checks() CheckRepository
	Stages() StageRepository
	Dependencies() DependencyRepository
	StageRunners() StageRunnerRepository
	StageExecutions() StageExecutionRepository

	// Transaction control
	Commit() error
	Rollback() error
}

// Storage provides the main entry point for storage operations.
type Storage interface {
	// Begin starts a new transaction and returns a UnitOfWork.
	Begin(ctx context.Context) (UnitOfWork, error)

	// BeginImmediate starts a new immediate transaction and returns a UnitOfWork.
	// This is preferred for read-write operations to avoid upgrade deadlocks in SQLite.
	BeginImmediate(ctx context.Context) (UnitOfWork, error)

	// Close closes the storage connection.
	Close() error

	// Migrate runs database migrations.
	Migrate(ctx context.Context) error
}
