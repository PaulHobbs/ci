package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/example/turboci-lite/internal/domain"
)

type workPlanRepo struct {
	tx *sql.Tx
}

func (r *workPlanRepo) Create(ctx context.Context, wp *domain.WorkPlan) error {
	metadataJSON, err := json.Marshal(wp.Metadata)
	if err != nil {
		return err
	}

	_, err = r.tx.ExecContext(ctx, `
		INSERT INTO work_plans (id, metadata_json, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?)
	`, wp.ID, string(metadataJSON), wp.CreatedAt, wp.UpdatedAt, wp.Version)
	return err
}

func (r *workPlanRepo) Get(ctx context.Context, id string) (*domain.WorkPlan, error) {
	row := r.tx.QueryRowContext(ctx, `
		SELECT id, metadata_json, created_at, updated_at, version
		FROM work_plans WHERE id = ?
	`, id)

	wp := &domain.WorkPlan{}
	var metadataJSON string

	err := row.Scan(&wp.ID, &metadataJSON, &wp.CreatedAt, &wp.UpdatedAt, &wp.Version)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &wp.Metadata); err != nil {
			return nil, err
		}
	}
	if wp.Metadata == nil {
		wp.Metadata = make(map[string]string)
	}

	return wp, nil
}

func (r *workPlanRepo) Update(ctx context.Context, wp *domain.WorkPlan) error {
	metadataJSON, err := json.Marshal(wp.Metadata)
	if err != nil {
		return err
	}

	result, err := r.tx.ExecContext(ctx, `
		UPDATE work_plans
		SET metadata_json = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?
	`, string(metadataJSON), wp.UpdatedAt, wp.ID, wp.Version)
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

	wp.Version++
	return nil
}

func (r *workPlanRepo) Delete(ctx context.Context, id string) error {
	result, err := r.tx.ExecContext(ctx, `DELETE FROM work_plans WHERE id = ?`, id)
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
