package storage

import (
	"context"

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

// UnitOfWork provides transactional access to all repositories.
type UnitOfWork interface {
	// Repository accessors
	WorkPlans() WorkPlanRepository
	Checks() CheckRepository
	Stages() StageRepository
	Dependencies() DependencyRepository

	// Transaction control
	Commit() error
	Rollback() error
}

// Storage provides the main entry point for storage operations.
type Storage interface {
	// Begin starts a new transaction and returns a UnitOfWork.
	Begin(ctx context.Context) (UnitOfWork, error)

	// Close closes the storage connection.
	Close() error

	// Migrate runs database migrations.
	Migrate(ctx context.Context) error
}
