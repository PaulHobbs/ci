package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/example/turboci-lite/internal/domain"
)

type stageRunnerRepo struct {
	tx *sql.Tx
}

func (r *stageRunnerRepo) Register(ctx context.Context, runner *domain.StageRunner) error {
	modesJSON, err := json.Marshal(runner.SupportedModes)
	if err != nil {
		return err
	}

	metadataJSON, err := json.Marshal(runner.Metadata)
	if err != nil {
		return err
	}

	// Use INSERT OR REPLACE to update existing registration
	_, err = r.tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO stage_runners (
			registration_id, id, runner_type, address, supported_modes_json,
			max_concurrent, current_load, metadata_json, registered_at, last_heartbeat, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, runner.RegistrationID, runner.ID, runner.RunnerType, runner.Address,
		string(modesJSON), runner.MaxConcurrent, runner.CurrentLoad,
		string(metadataJSON), runner.RegisteredAt, runner.LastHeartbeat, runner.ExpiresAt)
	return err
}

func (r *stageRunnerRepo) Get(ctx context.Context, registrationID string) (*domain.StageRunner, error) {
	row := r.tx.QueryRowContext(ctx, `
		SELECT registration_id, id, runner_type, address, supported_modes_json,
			max_concurrent, current_load, metadata_json, registered_at, last_heartbeat, expires_at
		FROM stage_runners WHERE registration_id = ?
	`, registrationID)

	runner, err := r.scanRunner(row)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	return runner, err
}

func (r *stageRunnerRepo) scanRunner(row *sql.Row) (*domain.StageRunner, error) {
	runner := &domain.StageRunner{}
	var modesJSON, metadataJSON string

	err := row.Scan(&runner.RegistrationID, &runner.ID, &runner.RunnerType, &runner.Address,
		&modesJSON, &runner.MaxConcurrent, &runner.CurrentLoad,
		&metadataJSON, &runner.RegisteredAt, &runner.LastHeartbeat, &runner.ExpiresAt)
	if err != nil {
		return nil, err
	}

	if modesJSON != "" {
		if err := json.Unmarshal([]byte(modesJSON), &runner.SupportedModes); err != nil {
			return nil, err
		}
	}

	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &runner.Metadata); err != nil {
			return nil, err
		}
	}
	if runner.Metadata == nil {
		runner.Metadata = make(map[string]string)
	}

	return runner, nil
}

func (r *stageRunnerRepo) scanRunnerFromRows(rows *sql.Rows) (*domain.StageRunner, error) {
	runner := &domain.StageRunner{}
	var modesJSON, metadataJSON string

	err := rows.Scan(&runner.RegistrationID, &runner.ID, &runner.RunnerType, &runner.Address,
		&modesJSON, &runner.MaxConcurrent, &runner.CurrentLoad,
		&metadataJSON, &runner.RegisteredAt, &runner.LastHeartbeat, &runner.ExpiresAt)
	if err != nil {
		return nil, err
	}

	if modesJSON != "" {
		if err := json.Unmarshal([]byte(modesJSON), &runner.SupportedModes); err != nil {
			return nil, err
		}
	}

	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &runner.Metadata); err != nil {
			return nil, err
		}
	}
	if runner.Metadata == nil {
		runner.Metadata = make(map[string]string)
	}

	return runner, nil
}

func (r *stageRunnerRepo) GetByType(ctx context.Context, runnerType string) ([]*domain.StageRunner, error) {
	rows, err := r.tx.QueryContext(ctx, `
		SELECT registration_id, id, runner_type, address, supported_modes_json,
			max_concurrent, current_load, metadata_json, registered_at, last_heartbeat, expires_at
		FROM stage_runners WHERE runner_type = ? AND expires_at > datetime('now')
		ORDER BY current_load ASC
	`, runnerType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runners []*domain.StageRunner
	for rows.Next() {
		runner, err := r.scanRunnerFromRows(rows)
		if err != nil {
			return nil, err
		}
		runners = append(runners, runner)
	}

	return runners, rows.Err()
}

func (r *stageRunnerRepo) GetAvailable(ctx context.Context, runnerType string, mode domain.ExecutionMode) ([]*domain.StageRunner, error) {
	runners, err := r.GetByType(ctx, runnerType)
	if err != nil {
		return nil, err
	}

	// Filter by capacity and mode support
	var available []*domain.StageRunner
	for _, runner := range runners {
		if runner.HasCapacity() && runner.SupportsMode(mode) {
			available = append(available, runner)
		}
	}

	return available, nil
}

func (r *stageRunnerRepo) Unregister(ctx context.Context, registrationID string) error {
	result, err := r.tx.ExecContext(ctx, `DELETE FROM stage_runners WHERE registration_id = ?`, registrationID)
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

func (r *stageRunnerRepo) UpdateHeartbeat(ctx context.Context, registrationID string, newExpiry time.Time) error {
	now := time.Now().UTC()
	result, err := r.tx.ExecContext(ctx, `
		UPDATE stage_runners SET last_heartbeat = ?, expires_at = ? WHERE registration_id = ?
	`, now, newExpiry, registrationID)
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

func (r *stageRunnerRepo) IncrementLoad(ctx context.Context, registrationID string) error {
	_, err := r.tx.ExecContext(ctx, `
		UPDATE stage_runners SET current_load = current_load + 1 WHERE registration_id = ?
	`, registrationID)
	return err
}

func (r *stageRunnerRepo) DecrementLoad(ctx context.Context, registrationID string) error {
	_, err := r.tx.ExecContext(ctx, `
		UPDATE stage_runners SET current_load = CASE WHEN current_load > 0 THEN current_load - 1 ELSE 0 END WHERE registration_id = ?
	`, registrationID)
	return err
}

func (r *stageRunnerRepo) CleanupExpired(ctx context.Context) (int, error) {
	result, err := r.tx.ExecContext(ctx, `DELETE FROM stage_runners WHERE expires_at < datetime('now')`)
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return int(rows), nil
}

func (r *stageRunnerRepo) List(ctx context.Context) ([]*domain.StageRunner, error) {
	rows, err := r.tx.QueryContext(ctx, `
		SELECT registration_id, id, runner_type, address, supported_modes_json,
			max_concurrent, current_load, metadata_json, registered_at, last_heartbeat, expires_at
		FROM stage_runners
		ORDER BY runner_type, current_load ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runners []*domain.StageRunner
	for rows.Next() {
		runner, err := r.scanRunnerFromRows(rows)
		if err != nil {
			return nil, err
		}
		runners = append(runners, runner)
	}

	return runners, rows.Err()
}
