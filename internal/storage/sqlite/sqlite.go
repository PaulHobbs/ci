package sqlite

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"

	"github.com/example/turboci-lite/internal/storage"
)

// SQLiteStorage implements the Storage interface using SQLite.
type SQLiteStorage struct {
	db *sql.DB
}

// New creates a new SQLite storage instance.
func New(path string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, err
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite works best with single connection for writes
	db.SetMaxIdleConns(1)

	return &SQLiteStorage{db: db}, nil
}

// Begin starts a new transaction.
func (s *SQLiteStorage) Begin(ctx context.Context) (storage.UnitOfWork, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return newUnitOfWork(tx), nil
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
	tx           *sql.Tx
	workPlans    *workPlanRepo
	checks       *checkRepo
	stages       *stageRepo
	dependencies *dependencyRepo
}

func newUnitOfWork(tx *sql.Tx) *unitOfWork {
	return &unitOfWork{
		tx:           tx,
		workPlans:    &workPlanRepo{tx: tx},
		checks:       &checkRepo{tx: tx},
		stages:       &stageRepo{tx: tx},
		dependencies: &dependencyRepo{tx: tx},
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

func (u *unitOfWork) Commit() error {
	return u.tx.Commit()
}

func (u *unitOfWork) Rollback() error {
	return u.tx.Rollback()
}
