package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
)

type stageRepo struct {
	tx *sql.Tx
}

func (r *stageRepo) Create(ctx context.Context, stage *domain.Stage) error {
	argsJSON, err := json.Marshal(stage.Args)
	if err != nil {
		return err
	}

	assignmentsJSON, err := json.Marshal(stage.Assignments)
	if err != nil {
		return err
	}

	depsJSON, err := json.Marshal(stage.Dependencies)
	if err != nil {
		return err
	}

	_, err = r.tx.ExecContext(ctx, `
		INSERT INTO stages (id, work_plan_id, state, args_json, assignments_json, dependencies_json, execution_mode, runner_type, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, stage.ID, stage.WorkPlanID, stage.State, string(argsJSON), string(assignmentsJSON), string(depsJSON),
		stage.ExecutionMode, stage.RunnerType, stage.CreatedAt, stage.UpdatedAt, stage.Version)
	return err
}

func (r *stageRepo) Get(ctx context.Context, workPlanID, stageID string) (*domain.Stage, error) {
	row := r.tx.QueryRowContext(ctx, `
		SELECT id, work_plan_id, state, args_json, assignments_json, dependencies_json, execution_mode, runner_type, created_at, updated_at, version
		FROM stages WHERE work_plan_id = ? AND id = ?
	`, workPlanID, stageID)

	stage, err := r.scanStage(row)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Load attempts
	attempts, err := r.loadAttempts(ctx, workPlanID, stageID)
	if err != nil {
		return nil, err
	}
	stage.Attempts = attempts

	return stage, nil
}

func (r *stageRepo) scanStage(row *sql.Row) (*domain.Stage, error) {
	stage := &domain.Stage{}
	var argsJSON, assignmentsJSON, depsJSON string
	var runnerType sql.NullString

	err := row.Scan(&stage.ID, &stage.WorkPlanID, &stage.State,
		&argsJSON, &assignmentsJSON, &depsJSON, &stage.ExecutionMode, &runnerType,
		&stage.CreatedAt, &stage.UpdatedAt, &stage.Version)
	if err != nil {
		return nil, err
	}

	if runnerType.Valid {
		stage.RunnerType = runnerType.String
	}

	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &stage.Args); err != nil {
			return nil, err
		}
	}
	if stage.Args == nil {
		stage.Args = make(map[string]any)
	}

	if assignmentsJSON != "" && assignmentsJSON != "null" {
		if err := json.Unmarshal([]byte(assignmentsJSON), &stage.Assignments); err != nil {
			return nil, err
		}
	}

	if depsJSON != "" && depsJSON != "null" {
		if err := json.Unmarshal([]byte(depsJSON), &stage.Dependencies); err != nil {
			return nil, err
		}
	}

	return stage, nil
}

func (r *stageRepo) loadAttempts(ctx context.Context, workPlanID, stageID string) ([]domain.Attempt, error) {
	rows, err := r.tx.QueryContext(ctx, `
		SELECT idx, state, process_uid, details_json, progress_json, created_at, updated_at, failure_message, failure_at
		FROM stage_attempts WHERE work_plan_id = ? AND stage_id = ?
		ORDER BY idx
	`, workPlanID, stageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []domain.Attempt
	for rows.Next() {
		var attempt domain.Attempt
		var processUID sql.NullString
		var detailsJSON, progressJSON sql.NullString
		var failureMessage sql.NullString
		var failureAt sql.NullTime

		err := rows.Scan(&attempt.Idx, &attempt.State, &processUID,
			&detailsJSON, &progressJSON,
			&attempt.CreatedAt, &attempt.UpdatedAt, &failureMessage, &failureAt)
		if err != nil {
			return nil, err
		}

		if processUID.Valid {
			attempt.ProcessUID = processUID.String
		}

		if detailsJSON.Valid && detailsJSON.String != "" {
			if err := json.Unmarshal([]byte(detailsJSON.String), &attempt.Details); err != nil {
				return nil, err
			}
		}
		if attempt.Details == nil {
			attempt.Details = make(map[string]any)
		}

		if progressJSON.Valid && progressJSON.String != "" {
			if err := json.Unmarshal([]byte(progressJSON.String), &attempt.Progress); err != nil {
				return nil, err
			}
		}

		if failureMessage.Valid && failureAt.Valid {
			attempt.Failure = &domain.Failure{
				Message:    failureMessage.String,
				OccurredAt: failureAt.Time,
			}
		}

		attempts = append(attempts, attempt)
	}

	return attempts, rows.Err()
}

func (r *stageRepo) Update(ctx context.Context, stage *domain.Stage) error {
	argsJSON, err := json.Marshal(stage.Args)
	if err != nil {
		return err
	}

	assignmentsJSON, err := json.Marshal(stage.Assignments)
	if err != nil {
		return err
	}

	depsJSON, err := json.Marshal(stage.Dependencies)
	if err != nil {
		return err
	}

	result, err := r.tx.ExecContext(ctx, `
		UPDATE stages
		SET state = ?, args_json = ?, assignments_json = ?, dependencies_json = ?, execution_mode = ?, runner_type = ?, updated_at = ?, version = version + 1
		WHERE work_plan_id = ? AND id = ? AND version = ?
	`, stage.State, string(argsJSON), string(assignmentsJSON), string(depsJSON),
		stage.ExecutionMode, stage.RunnerType, stage.UpdatedAt, stage.WorkPlanID, stage.ID, stage.Version)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrConcurrentModify
	}

	stage.Version++
	return nil
}

func (r *stageRepo) List(ctx context.Context, workPlanID string, opts storage.ListOptions) ([]*domain.Stage, error) {
	query := `SELECT id, work_plan_id, state, args_json, assignments_json, dependencies_json, execution_mode, runner_type, created_at, updated_at, version
		FROM stages WHERE work_plan_id = ?`
	args := []any{workPlanID}

	if len(opts.IDs) > 0 {
		placeholders := make([]string, len(opts.IDs))
		for i, id := range opts.IDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query += " AND id IN (" + strings.Join(placeholders, ",") + ")"
	}

	if len(opts.StageStates) > 0 {
		placeholders := make([]string, len(opts.StageStates))
		for i, state := range opts.StageStates {
			placeholders[i] = "?"
			args = append(args, state)
		}
		query += " AND state IN (" + strings.Join(placeholders, ",") + ")"
	}

	query += " ORDER BY created_at"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stages []*domain.Stage
	for rows.Next() {
		stage := &domain.Stage{}
		var argsJSON, assignmentsJSON, depsJSON string
		var runnerType sql.NullString

		err := rows.Scan(&stage.ID, &stage.WorkPlanID, &stage.State,
			&argsJSON, &assignmentsJSON, &depsJSON, &stage.ExecutionMode, &runnerType,
			&stage.CreatedAt, &stage.UpdatedAt, &stage.Version)
		if err != nil {
			return nil, err
		}

		if runnerType.Valid {
			stage.RunnerType = runnerType.String
		}

		if argsJSON != "" {
			if err := json.Unmarshal([]byte(argsJSON), &stage.Args); err != nil {
				return nil, err
			}
		}
		if stage.Args == nil {
			stage.Args = make(map[string]any)
		}

		if assignmentsJSON != "" && assignmentsJSON != "null" {
			if err := json.Unmarshal([]byte(assignmentsJSON), &stage.Assignments); err != nil {
				return nil, err
			}
		}

		if depsJSON != "" && depsJSON != "null" {
			if err := json.Unmarshal([]byte(depsJSON), &stage.Dependencies); err != nil {
				return nil, err
			}
		}

		// Load attempts for each stage
		attempts, err := r.loadAttempts(ctx, workPlanID, stage.ID)
		if err != nil {
			return nil, err
		}
		stage.Attempts = attempts

		stages = append(stages, stage)
	}

	return stages, rows.Err()
}

func (r *stageRepo) Delete(ctx context.Context, workPlanID, stageID string) error {
	result, err := r.tx.ExecContext(ctx, `DELETE FROM stages WHERE work_plan_id = ? AND id = ?`, workPlanID, stageID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrNotFound
	}

	return nil
}

func (r *stageRepo) AddAttempt(ctx context.Context, workPlanID, stageID string, attempt *domain.Attempt) error {
	var processUID sql.NullString
	if attempt.ProcessUID != "" {
		processUID = sql.NullString{String: attempt.ProcessUID, Valid: true}
	}

	detailsJSON, err := json.Marshal(attempt.Details)
	if err != nil {
		return err
	}

	progressJSON, err := json.Marshal(attempt.Progress)
	if err != nil {
		return err
	}

	var failureMessage sql.NullString
	var failureAt sql.NullTime
	if attempt.Failure != nil {
		failureMessage = sql.NullString{String: attempt.Failure.Message, Valid: true}
		failureAt = sql.NullTime{Time: attempt.Failure.OccurredAt, Valid: true}
	}

	_, err = r.tx.ExecContext(ctx, `
		INSERT INTO stage_attempts (idx, work_plan_id, stage_id, state, process_uid, details_json, progress_json, created_at, updated_at, failure_message, failure_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, attempt.Idx, workPlanID, stageID, attempt.State, processUID,
		string(detailsJSON), string(progressJSON),
		attempt.CreatedAt, attempt.UpdatedAt, failureMessage, failureAt)
	return err
}

func (r *stageRepo) UpdateAttempt(ctx context.Context, workPlanID, stageID string, idx int, attempt *domain.Attempt) error {
	var processUID sql.NullString
	if attempt.ProcessUID != "" {
		processUID = sql.NullString{String: attempt.ProcessUID, Valid: true}
	}

	detailsJSON, err := json.Marshal(attempt.Details)
	if err != nil {
		return err
	}

	progressJSON, err := json.Marshal(attempt.Progress)
	if err != nil {
		return err
	}

	var failureMessage sql.NullString
	var failureAt sql.NullTime
	if attempt.Failure != nil {
		failureMessage = sql.NullString{String: attempt.Failure.Message, Valid: true}
		failureAt = sql.NullTime{Time: attempt.Failure.OccurredAt, Valid: true}
	}

	_, err = r.tx.ExecContext(ctx, `
		UPDATE stage_attempts
		SET state = ?, process_uid = ?, details_json = ?, progress_json = ?, updated_at = ?, failure_message = ?, failure_at = ?
		WHERE work_plan_id = ? AND stage_id = ? AND idx = ?
	`, attempt.State, processUID, string(detailsJSON), string(progressJSON),
		attempt.UpdatedAt, failureMessage, failureAt, workPlanID, stageID, idx)
	return err
}
