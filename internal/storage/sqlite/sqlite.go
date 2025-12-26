package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/example/turboci-lite/internal/observability"
	"github.com/example/turboci-lite/internal/storage"
)

// SQLiteStorage implements the Storage interface using SQLite.
type SQLiteStorage struct {
	db      *sql.DB
	metrics *observability.Metrics
}

// New creates a new SQLite storage instance without metrics.
func New(path string) (*SQLiteStorage, error) {
	return NewWithMetrics(path, nil)
}

// NewWithMetrics creates a new SQLite storage instance with metrics instrumentation.
func NewWithMetrics(path string, metrics *observability.Metrics) (*SQLiteStorage, error) {
	isMemory := strings.Contains(path, ":memory:") || strings.Contains(path, "mode=memory")

	// Build connection string with SQLite pragmas.
	// Use & if path already has query params, otherwise use ?
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}

	// WAL mode doesn't work well with shared memory databases, so skip it for memory.
	// Use longer busy timeout for memory databases with concurrent access.
	var pragmas string
	if isMemory {
		pragmas = "_busy_timeout=10000&_foreign_keys=ON"
	} else {
		pragmas = "_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON"
	}
	dsn := path + separator + pragmas

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	// Set connection pool settings.
	// Allow multiple connections for concurrent read transactions.
	// WAL mode (enabled for file-based DBs) handles write serialization safely.
	// Without this, multiple goroutines trying to begin transactions will deadlock.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	return &SQLiteStorage{db: db, metrics: metrics}, nil
}

// Begin starts a new transaction.
func (s *SQLiteStorage) Begin(ctx context.Context) (storage.UnitOfWork, error) {
	start := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	if s.metrics != nil {
		s.metrics.DBTransactionBegin().Observe(time.Since(start))
		s.metrics.DBActiveTransactions().Inc()
	}

	return newUnitOfWork(tx, s.metrics), nil
}

// BeginImmediate starts a new immediate transaction.
// It emulates IMMEDIATE behavior by performing a write operation immediately.
func (s *SQLiteStorage) BeginImmediate(ctx context.Context) (storage.UnitOfWork, error) {
	lockStart := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	// Force RESERVED lock by writing.
	// This prevents the "upgrade deadlock" where multiple transactions read first
	// (acquiring SHARED locks) and then try to write (needing RESERVED lock).
	if _, err := tx.ExecContext(ctx, "UPDATE _lock SET id=id WHERE id=1"); err != nil {
		tx.Rollback()
		return nil, err
	}

	// Track lock wait time (includes BeginTx + UPDATE _lock)
	if s.metrics != nil {
		lockWait := time.Since(lockStart)
		s.metrics.DBLockWaitTime().Observe(lockWait)
		s.metrics.DBTransactionBegin().Observe(lockWait)
		s.metrics.DBActiveTransactions().Inc()
	}

	return newUnitOfWork(tx, s.metrics), nil
}

// Close closes the database connection.
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// Migrate runs database migrations.
func (s *SQLiteStorage) Migrate(ctx context.Context) error {
	return Migrate(ctx, s.db)
}

// unitOfWork implements the UnitOfWork interface.
type unitOfWork struct {
	tx              *sql.Tx
	metrics         *observability.Metrics
	workPlans       *workPlanRepo
	checks          *checkRepo
	stages          *stageRepo
	dependencies    *dependencyRepo
	stageRunners    *stageRunnerRepo
	stageExecutions *stageExecutionRepo
}

func newUnitOfWork(tx *sql.Tx, metrics *observability.Metrics) *unitOfWork {
	return &unitOfWork{
		tx:              tx,
		metrics:         metrics,
		workPlans:       &workPlanRepo{tx: tx},
		checks:          &checkRepo{tx: tx},
		stages:          &stageRepo{tx: tx},
		dependencies:    &dependencyRepo{tx: tx},
		stageRunners:    &stageRunnerRepo{tx: tx},
		stageExecutions: &stageExecutionRepo{tx: tx},
	}
}

func (u *unitOfWork) WorkPlans() storage.WorkPlanRepository {
	return u.workPlans
}

func (u *unitOfWork) Checks() storage.CheckRepository {
	return u.checks
}

func (u *unitOfWork) Stages() storage.StageRepository {
	return u.stages
}

func (u *unitOfWork) Dependencies() storage.DependencyRepository {
	return u.dependencies
}

func (u *unitOfWork) StageRunners() storage.StageRunnerRepository {
	return u.stageRunners
}

func (u *unitOfWork) StageExecutions() storage.StageExecutionRepository {
	return u.stageExecutions
}

func (u *unitOfWork) Commit() error {
	start := time.Now()
	err := u.tx.Commit()

	if u.metrics != nil {
		u.metrics.DBTransactionCommit().Observe(time.Since(start))
		u.metrics.DBActiveTransactions().Dec()
	}

	return err
}

func (u *unitOfWork) Rollback() error {
	err := u.tx.Rollback()

	// Decrement active transactions even on rollback
	if u.metrics != nil {
		u.metrics.DBActiveTransactions().Dec()
	}

	return err
}
