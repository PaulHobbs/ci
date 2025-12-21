package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/example/turboci-lite/internal/domain"
)

type dependencyRepo struct {
	tx *sql.Tx
}

func (r *dependencyRepo) Create(ctx context.Context, dep *domain.Dependency) error {
	result, err := r.tx.ExecContext(ctx, `
		INSERT INTO dependencies (work_plan_id, source_type, source_id, target_type, target_id, resolved, satisfied, resolved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, dep.WorkPlanID, dep.SourceType, dep.SourceID, dep.TargetType, dep.TargetID,
		dep.Resolved, dep.Satisfied, dep.ResolvedAt)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	dep.ID = id
	return nil
}

func (r *dependencyRepo) CreateBatch(ctx context.Context, deps []*domain.Dependency) error {
	for _, dep := range deps {
		if err := r.Create(ctx, dep); err != nil {
			return err
		}
	}
	return nil
}

func (r *dependencyRepo) GetBySource(ctx context.Context, workPlanID string, sourceType domain.NodeType, sourceID string) ([]*domain.Dependency, error) {
	rows, err := r.tx.QueryContext(ctx, `
		SELECT id, work_plan_id, source_type, source_id, target_type, target_id, resolved, satisfied, resolved_at
		FROM dependencies
		WHERE work_plan_id = ? AND source_type = ? AND source_id = ?
	`, workPlanID, sourceType, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanDependencies(rows)
}

func (r *dependencyRepo) GetByTarget(ctx context.Context, workPlanID string, targetType domain.NodeType, targetID string) ([]*domain.Dependency, error) {
	rows, err := r.tx.QueryContext(ctx, `
		SELECT id, work_plan_id, source_type, source_id, target_type, target_id, resolved, satisfied, resolved_at
		FROM dependencies
		WHERE work_plan_id = ? AND target_type = ? AND target_id = ?
	`, workPlanID, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanDependencies(rows)
}

func (r *dependencyRepo) GetUnresolvedByTarget(ctx context.Context, workPlanID string, targetType domain.NodeType, targetID string) ([]*domain.Dependency, error) {
	rows, err := r.tx.QueryContext(ctx, `
		SELECT id, work_plan_id, source_type, source_id, target_type, target_id, resolved, satisfied, resolved_at
		FROM dependencies
		WHERE work_plan_id = ? AND target_type = ? AND target_id = ? AND resolved = FALSE
	`, workPlanID, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanDependencies(rows)
}

func (r *dependencyRepo) scanDependencies(rows *sql.Rows) ([]*domain.Dependency, error) {
	var deps []*domain.Dependency
	for rows.Next() {
		dep := &domain.Dependency{}
		var satisfied sql.NullBool
		var resolvedAt sql.NullTime

		err := rows.Scan(&dep.ID, &dep.WorkPlanID, &dep.SourceType, &dep.SourceID,
			&dep.TargetType, &dep.TargetID, &dep.Resolved, &satisfied, &resolvedAt)
		if err != nil {
			return nil, err
		}

		if satisfied.Valid {
			dep.Satisfied = &satisfied.Bool
		}
		if resolvedAt.Valid {
			dep.ResolvedAt = &resolvedAt.Time
		}

		deps = append(deps, dep)
	}

	return deps, rows.Err()
}

func (r *dependencyRepo) MarkResolved(ctx context.Context, id int64, satisfied bool) error {
	now := time.Now().UTC()
	_, err := r.tx.ExecContext(ctx, `
		UPDATE dependencies
		SET resolved = TRUE, satisfied = ?, resolved_at = ?
		WHERE id = ?
	`, satisfied, now, id)
	return err
}
