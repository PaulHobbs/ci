package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
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

// GetByTargets retrieves dependencies for multiple targets in a single query.
func (r *dependencyRepo) GetByTargets(ctx context.Context, workPlanID string, targets []storage.TargetNode) (map[string][]*domain.Dependency, error) {
	if len(targets) == 0 {
		return make(map[string][]*domain.Dependency), nil
	}

	// Build query with OR clauses (SQLite doesn't support tuple IN)
	query := `SELECT id, work_plan_id, source_type, source_id, target_type, target_id,
		resolved, satisfied, resolved_at
		FROM dependencies WHERE work_plan_id = ? AND (`

	args := []any{workPlanID}
	orClauses := make([]string, len(targets))
	for i, target := range targets {
		orClauses[i] = "(target_type = ? AND target_id = ?)"
		args = append(args, target.Type, target.ID)
	}
	query += strings.Join(orClauses, " OR ") + ")"

	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Group by target
	result := make(map[string][]*domain.Dependency)
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

		key := string(dep.TargetType) + ":" + dep.TargetID
		result[key] = append(result[key], dep)
	}

	return result, rows.Err()
}

// MarkResolvedBatch marks multiple dependencies as resolved in a single operation.
func (r *dependencyRepo) MarkResolvedBatch(ctx context.Context, updates []storage.DependencyUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	// Group by satisfied value for efficient batching
	trueIDs := make([]int64, 0)
	falseIDs := make([]int64, 0)

	for _, update := range updates {
		if update.Satisfied {
			trueIDs = append(trueIDs, update.ID)
		} else {
			falseIDs = append(falseIDs, update.ID)
		}
	}

	now := time.Now().UTC()

	// Batch update satisfied=TRUE
	if len(trueIDs) > 0 {
		placeholders := make([]string, len(trueIDs))
		args := []any{true, now}
		for i, id := range trueIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query := `UPDATE dependencies SET resolved = TRUE, satisfied = ?, resolved_at = ?
			WHERE id IN (` + strings.Join(placeholders, ",") + `)`

		if _, err := r.tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	// Batch update satisfied=FALSE
	if len(falseIDs) > 0 {
		placeholders := make([]string, len(falseIDs))
		args := []any{false, now}
		for i, id := range falseIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query := `UPDATE dependencies SET resolved = TRUE, satisfied = ?, resolved_at = ?
			WHERE id IN (` + strings.Join(placeholders, ",") + `)`

		if _, err := r.tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	return nil
}
