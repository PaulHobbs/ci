package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/example/turboci-lite/internal/domain"
)

type stageExecutionRepo struct {
	tx *sql.Tx
}

func (r *stageExecutionRepo) Create(ctx context.Context, exec *domain.StageExecution) error {
	_, err := r.tx.ExecContext(ctx, `
		INSERT INTO stage_executions (
			id, work_plan_id, stage_id, attempt_idx, runner_type, execution_mode,
			state, runner_id, dispatched_at, started_at, completed_at, deadline,
			last_progress_at, progress_percent, progress_message, error_message,
			retry_count, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, exec.ID, exec.WorkPlanID, exec.StageID, exec.AttemptIdx, exec.RunnerType, exec.ExecutionMode,
		exec.State, exec.RunnerID, exec.DispatchedAt, exec.StartedAt, exec.CompletedAt, exec.Deadline,
		exec.LastProgressAt, exec.ProgressPercent, exec.ProgressMessage, exec.ErrorMessage,
		exec.RetryCount, exec.CreatedAt, exec.UpdatedAt)
	return err
}

func (r *stageExecutionRepo) Get(ctx context.Context, executionID string) (*domain.StageExecution, error) {
	row := r.tx.QueryRowContext(ctx, `
		SELECT id, work_plan_id, stage_id, attempt_idx, runner_type, execution_mode,
			state, runner_id, dispatched_at, started_at, completed_at, deadline,
			last_progress_at, progress_percent, progress_message, error_message,
			retry_count, created_at, updated_at
		FROM stage_executions WHERE id = ?
	`, executionID)

	exec, err := r.scanExecution(row)
	if err == sql.ErrNoRows {
		return nil, domain.ErrExecutionNotFound
	}
	return exec, err
}

func (r *stageExecutionRepo) scanExecution(row *sql.Row) (*domain.StageExecution, error) {
	exec := &domain.StageExecution{}
	var runnerID sql.NullString
	var dispatchedAt, startedAt, completedAt, deadline, lastProgressAt sql.NullTime
	var progressMessage, errorMessage sql.NullString

	err := row.Scan(&exec.ID, &exec.WorkPlanID, &exec.StageID, &exec.AttemptIdx, &exec.RunnerType, &exec.ExecutionMode,
		&exec.State, &runnerID, &dispatchedAt, &startedAt, &completedAt, &deadline,
		&lastProgressAt, &exec.ProgressPercent, &progressMessage, &errorMessage,
		&exec.RetryCount, &exec.CreatedAt, &exec.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if runnerID.Valid {
		exec.RunnerID = runnerID.String
	}
	if dispatchedAt.Valid {
		exec.DispatchedAt = &dispatchedAt.Time
	}
	if startedAt.Valid {
		exec.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		exec.CompletedAt = &completedAt.Time
	}
	if deadline.Valid {
		exec.Deadline = &deadline.Time
	}
	if lastProgressAt.Valid {
		exec.LastProgressAt = &lastProgressAt.Time
	}
	if progressMessage.Valid {
		exec.ProgressMessage = progressMessage.String
	}
	if errorMessage.Valid {
		exec.ErrorMessage = errorMessage.String
	}

	return exec, nil
}

func (r *stageExecutionRepo) scanExecutionFromRows(rows *sql.Rows) (*domain.StageExecution, error) {
	exec := &domain.StageExecution{}
	var runnerID sql.NullString
	var dispatchedAt, startedAt, completedAt, deadline, lastProgressAt sql.NullTime
	var progressMessage, errorMessage sql.NullString

	err := rows.Scan(&exec.ID, &exec.WorkPlanID, &exec.StageID, &exec.AttemptIdx, &exec.RunnerType, &exec.ExecutionMode,
		&exec.State, &runnerID, &dispatchedAt, &startedAt, &completedAt, &deadline,
		&lastProgressAt, &exec.ProgressPercent, &progressMessage, &errorMessage,
		&exec.RetryCount, &exec.CreatedAt, &exec.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if runnerID.Valid {
		exec.RunnerID = runnerID.String
	}
	if dispatchedAt.Valid {
		exec.DispatchedAt = &dispatchedAt.Time
	}
	if startedAt.Valid {
		exec.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		exec.CompletedAt = &completedAt.Time
	}
	if deadline.Valid {
		exec.Deadline = &deadline.Time
	}
	if lastProgressAt.Valid {
		exec.LastProgressAt = &lastProgressAt.Time
	}
	if progressMessage.Valid {
		exec.ProgressMessage = progressMessage.String
	}
	if errorMessage.Valid {
		exec.ErrorMessage = errorMessage.String
	}

	return exec, nil
}

func (r *stageExecutionRepo) GetByStageAttempt(ctx context.Context, workPlanID, stageID string, attemptIdx int) (*domain.StageExecution, error) {
	row := r.tx.QueryRowContext(ctx, `
		SELECT id, work_plan_id, stage_id, attempt_idx, runner_type, execution_mode,
			state, runner_id, dispatched_at, started_at, completed_at, deadline,
			last_progress_at, progress_percent, progress_message, error_message,
			retry_count, created_at, updated_at
		FROM stage_executions WHERE work_plan_id = ? AND stage_id = ? AND attempt_idx = ?
	`, workPlanID, stageID, attemptIdx)

	exec, err := r.scanExecution(row)
	if err == sql.ErrNoRows {
		return nil, domain.ErrExecutionNotFound
	}
	return exec, err
}

func (r *stageExecutionRepo) Update(ctx context.Context, exec *domain.StageExecution) error {
	exec.UpdatedAt = time.Now().UTC()
	_, err := r.tx.ExecContext(ctx, `
		UPDATE stage_executions SET
			state = ?, runner_id = ?, dispatched_at = ?, started_at = ?, completed_at = ?, deadline = ?,
			last_progress_at = ?, progress_percent = ?, progress_message = ?, error_message = ?,
			retry_count = ?, updated_at = ?
		WHERE id = ?
	`, exec.State, exec.RunnerID, exec.DispatchedAt, exec.StartedAt, exec.CompletedAt, exec.Deadline,
		exec.LastProgressAt, exec.ProgressPercent, exec.ProgressMessage, exec.ErrorMessage,
		exec.RetryCount, exec.UpdatedAt, exec.ID)
	return err
}

func (r *stageExecutionRepo) GetPending(ctx context.Context, runnerType string, limit int) ([]*domain.StageExecution, error) {
	query := `
		SELECT id, work_plan_id, stage_id, attempt_idx, runner_type, execution_mode,
			state, runner_id, dispatched_at, started_at, completed_at, deadline,
			last_progress_at, progress_percent, progress_message, error_message,
			retry_count, created_at, updated_at
		FROM stage_executions
		WHERE state = ? AND runner_type = ?
		ORDER BY created_at ASC
	`
	args := []any{domain.ExecutionStatePending, runnerType}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []*domain.StageExecution
	for rows.Next() {
		exec, err := r.scanExecutionFromRows(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, exec)
	}

	return executions, rows.Err()
}

func (r *stageExecutionRepo) MarkDispatched(ctx context.Context, executionID string, runnerID string) error {
	now := time.Now().UTC()
	result, err := r.tx.ExecContext(ctx, `
		UPDATE stage_executions SET state = ?, runner_id = ?, dispatched_at = ?, updated_at = ?
		WHERE id = ? AND state = ?
	`, domain.ExecutionStateDispatched, runnerID, now, now, executionID, domain.ExecutionStatePending)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrInvalidState
	}

	return nil
}

func (r *stageExecutionRepo) MarkRunning(ctx context.Context, executionID string) error {
	now := time.Now().UTC()
	result, err := r.tx.ExecContext(ctx, `
		UPDATE stage_executions SET state = ?, started_at = ?, updated_at = ?
		WHERE id = ? AND state IN (?, ?)
	`, domain.ExecutionStateRunning, now, now, executionID, domain.ExecutionStatePending, domain.ExecutionStateDispatched)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrInvalidState
	}

	return nil
}

func (r *stageExecutionRepo) MarkComplete(ctx context.Context, executionID string) error {
	now := time.Now().UTC()
	_, err := r.tx.ExecContext(ctx, `
		UPDATE stage_executions SET state = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, domain.ExecutionStateComplete, now, now, executionID)
	return err
}

func (r *stageExecutionRepo) MarkFailed(ctx context.Context, executionID string, errorMsg string) error {
	now := time.Now().UTC()
	_, err := r.tx.ExecContext(ctx, `
		UPDATE stage_executions SET state = ?, error_message = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, domain.ExecutionStateFailed, errorMsg, now, now, executionID)
	return err
}

func (r *stageExecutionRepo) UpdateProgress(ctx context.Context, executionID string, percent int, message string) error {
	now := time.Now().UTC()
	_, err := r.tx.ExecContext(ctx, `
		UPDATE stage_executions SET progress_percent = ?, progress_message = ?, last_progress_at = ?, updated_at = ?
		WHERE id = ?
	`, percent, message, now, now, executionID)
	return err
}

func (r *stageExecutionRepo) GetTimedOut(ctx context.Context) ([]*domain.StageExecution, error) {
	rows, err := r.tx.QueryContext(ctx, `
		SELECT id, work_plan_id, stage_id, attempt_idx, runner_type, execution_mode,
			state, runner_id, dispatched_at, started_at, completed_at, deadline,
			last_progress_at, progress_percent, progress_message, error_message,
			retry_count, created_at, updated_at
		FROM stage_executions
		WHERE deadline IS NOT NULL AND deadline < datetime('now') AND state NOT IN (?, ?, ?)
	`, domain.ExecutionStateComplete, domain.ExecutionStateFailed, domain.ExecutionStateCancelled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []*domain.StageExecution
	for rows.Next() {
		exec, err := r.scanExecutionFromRows(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, exec)
	}

	return executions, rows.Err()
}

func (r *stageExecutionRepo) GetStale(ctx context.Context, staleDuration time.Duration) ([]*domain.StageExecution, error) {
	staleTime := time.Now().UTC().Add(-staleDuration)
	rows, err := r.tx.QueryContext(ctx, `
		SELECT id, work_plan_id, stage_id, attempt_idx, runner_type, execution_mode,
			state, runner_id, dispatched_at, started_at, completed_at, deadline,
			last_progress_at, progress_percent, progress_message, error_message,
			retry_count, created_at, updated_at
		FROM stage_executions
		WHERE state IN (?, ?) AND (
			(last_progress_at IS NOT NULL AND last_progress_at < ?) OR
			(last_progress_at IS NULL AND dispatched_at IS NOT NULL AND dispatched_at < ?)
		)
	`, domain.ExecutionStateDispatched, domain.ExecutionStateRunning, staleTime, staleTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []*domain.StageExecution
	for rows.Next() {
		exec, err := r.scanExecutionFromRows(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, exec)
	}

	return executions, rows.Err()
}
