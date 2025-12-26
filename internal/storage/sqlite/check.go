package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
)

type checkRepo struct {
	tx *sql.Tx
}

func (r *checkRepo) Create(ctx context.Context, check *domain.Check) error {
	optionsJSON, err := json.Marshal(check.Options)
	if err != nil {
		return err
	}

	depsJSON, err := json.Marshal(check.Dependencies)
	if err != nil {
		return err
	}

	_, err = r.tx.ExecContext(ctx, `
		INSERT INTO checks (id, work_plan_id, state, kind, options_json, dependencies_json, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, check.ID, check.WorkPlanID, check.State, check.Kind, string(optionsJSON), string(depsJSON),
		check.CreatedAt, check.UpdatedAt, check.Version)
	return err
}

func (r *checkRepo) Get(ctx context.Context, workPlanID, checkID string) (*domain.Check, error) {
	row := r.tx.QueryRowContext(ctx, `
		SELECT id, work_plan_id, state, kind, options_json, dependencies_json, created_at, updated_at, version
		FROM checks WHERE work_plan_id = ? AND id = ?
	`, workPlanID, checkID)

	check, err := r.scanCheck(row)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Load results
	results, err := r.loadResults(ctx, workPlanID, checkID)
	if err != nil {
		return nil, err
	}
	check.Results = results

	return check, nil
}

func (r *checkRepo) scanCheck(row *sql.Row) (*domain.Check, error) {
	check := &domain.Check{}
	var optionsJSON, depsJSON string

	err := row.Scan(&check.ID, &check.WorkPlanID, &check.State, &check.Kind,
		&optionsJSON, &depsJSON, &check.CreatedAt, &check.UpdatedAt, &check.Version)
	if err != nil {
		return nil, err
	}

	if optionsJSON != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &check.Options); err != nil {
			return nil, err
		}
	}
	if check.Options == nil {
		check.Options = make(map[string]any)
	}

	if depsJSON != "" && depsJSON != "null" {
		if err := json.Unmarshal([]byte(depsJSON), &check.Dependencies); err != nil {
			return nil, err
		}
	}

	return check, nil
}

func (r *checkRepo) loadResults(ctx context.Context, workPlanID, checkID string) ([]domain.CheckResult, error) {
	rows, err := r.tx.QueryContext(ctx, `
		SELECT id, owner_type, owner_id, data_json, created_at, finalized_at, failure_message, failure_at
		FROM check_results WHERE work_plan_id = ? AND check_id = ?
		ORDER BY id
	`, workPlanID, checkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.CheckResult
	for rows.Next() {
		var result domain.CheckResult
		var dataJSON sql.NullString
		var finalizedAt sql.NullTime
		var failureMessage sql.NullString
		var failureAt sql.NullTime

		err := rows.Scan(&result.ID, &result.OwnerType, &result.OwnerID, &dataJSON,
			&result.CreatedAt, &finalizedAt, &failureMessage, &failureAt)
		if err != nil {
			return nil, err
		}

		if dataJSON.Valid && dataJSON.String != "" {
			if err := json.Unmarshal([]byte(dataJSON.String), &result.Data); err != nil {
				return nil, err
			}
		}
		if result.Data == nil {
			result.Data = make(map[string]any)
		}

		if finalizedAt.Valid {
			result.FinalizedAt = &finalizedAt.Time
		}

		if failureMessage.Valid && failureAt.Valid {
			result.Failure = &domain.Failure{
				Message:    failureMessage.String,
				OccurredAt: failureAt.Time,
			}
		}

		results = append(results, result)
	}

	return results, rows.Err()
}

func (r *checkRepo) Update(ctx context.Context, check *domain.Check) error {
	optionsJSON, err := json.Marshal(check.Options)
	if err != nil {
		return err
	}

	depsJSON, err := json.Marshal(check.Dependencies)
	if err != nil {
		return err
	}

	result, err := r.tx.ExecContext(ctx, `
		UPDATE checks
		SET state = ?, kind = ?, options_json = ?, dependencies_json = ?, updated_at = ?, version = version + 1
		WHERE work_plan_id = ? AND id = ? AND version = ?
	`, check.State, check.Kind, string(optionsJSON), string(depsJSON),
		check.UpdatedAt, check.WorkPlanID, check.ID, check.Version)
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

	check.Version++
	return nil
}

func (r *checkRepo) List(ctx context.Context, workPlanID string, opts storage.ListOptions) ([]*domain.Check, error) {
	// Build check filter conditions
	checkFilter := "c.work_plan_id = ?"
	args := []any{workPlanID}

	if len(opts.IDs) > 0 {
		placeholders := make([]string, len(opts.IDs))
		for i, id := range opts.IDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		checkFilter += " AND c.id IN (" + strings.Join(placeholders, ",") + ")"
	}

	if len(opts.CheckStates) > 0 {
		placeholders := make([]string, len(opts.CheckStates))
		for i, state := range opts.CheckStates {
			placeholders[i] = "?"
			args = append(args, state)
		}
		checkFilter += " AND c.state IN (" + strings.Join(placeholders, ",") + ")"
	}

	// Use a single JOIN query to fetch checks and results together
	query := `
		SELECT c.id, c.work_plan_id, c.state, c.kind, c.options_json, c.dependencies_json,
		       c.created_at, c.updated_at, c.version,
		       cr.id, cr.owner_type, cr.owner_id, cr.data_json, cr.created_at,
		       cr.finalized_at, cr.failure_message, cr.failure_at
		FROM checks c
		LEFT JOIN check_results cr ON c.work_plan_id = cr.work_plan_id AND c.id = cr.check_id
		WHERE ` + checkFilter + `
		ORDER BY c.created_at, c.id, cr.id`

	// Apply LIMIT/OFFSET to a subquery for checks, not the joined result
	if opts.Limit > 0 || opts.Offset > 0 {
		subquery := `SELECT id FROM checks WHERE work_plan_id = ?`
		subArgs := []any{workPlanID}

		if len(opts.IDs) > 0 {
			placeholders := make([]string, len(opts.IDs))
			for i, id := range opts.IDs {
				placeholders[i] = "?"
				subArgs = append(subArgs, id)
			}
			subquery += " AND id IN (" + strings.Join(placeholders, ",") + ")"
		}
		if len(opts.CheckStates) > 0 {
			placeholders := make([]string, len(opts.CheckStates))
			for i, state := range opts.CheckStates {
				placeholders[i] = "?"
				subArgs = append(subArgs, state)
			}
			subquery += " AND state IN (" + strings.Join(placeholders, ",") + ")"
		}

		subquery += " ORDER BY created_at"
		if opts.Limit > 0 {
			subquery += " LIMIT ?"
			subArgs = append(subArgs, opts.Limit)
		}
		if opts.Offset > 0 {
			subquery += " OFFSET ?"
			subArgs = append(subArgs, opts.Offset)
		}

		query = `
			SELECT c.id, c.work_plan_id, c.state, c.kind, c.options_json, c.dependencies_json,
			       c.created_at, c.updated_at, c.version,
			       cr.id, cr.owner_type, cr.owner_id, cr.data_json, cr.created_at,
			       cr.finalized_at, cr.failure_message, cr.failure_at
			FROM checks c
			LEFT JOIN check_results cr ON c.work_plan_id = cr.work_plan_id AND c.id = cr.check_id
			WHERE c.work_plan_id = ? AND c.id IN (` + subquery + `)
			ORDER BY c.created_at, c.id, cr.id`
		args = append([]any{workPlanID}, subArgs...)
	}

	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Group results by check ID
	checkMap := make(map[string]*domain.Check)
	var checkOrder []string

	for rows.Next() {
		var checkID, optionsJSON, depsJSON string
		var check domain.Check

		// Result columns (nullable due to LEFT JOIN)
		var resultID sql.NullInt64
		var resultOwnerType, resultOwnerID sql.NullString
		var resultDataJSON sql.NullString
		var resultCreatedAt sql.NullTime
		var resultFinalizedAt sql.NullTime
		var resultFailureMessage sql.NullString
		var resultFailureAt sql.NullTime

		err := rows.Scan(
			&checkID, &check.WorkPlanID, &check.State, &check.Kind,
			&optionsJSON, &depsJSON, &check.CreatedAt, &check.UpdatedAt, &check.Version,
			&resultID, &resultOwnerType, &resultOwnerID, &resultDataJSON, &resultCreatedAt,
			&resultFinalizedAt, &resultFailureMessage, &resultFailureAt,
		)
		if err != nil {
			return nil, err
		}

		// Get or create check entry
		existing, exists := checkMap[checkID]
		if !exists {
			check.ID = checkID
			if optionsJSON != "" {
				if err := json.Unmarshal([]byte(optionsJSON), &check.Options); err != nil {
					return nil, err
				}
			}
			if check.Options == nil {
				check.Options = make(map[string]any)
			}
			if depsJSON != "" && depsJSON != "null" {
				if err := json.Unmarshal([]byte(depsJSON), &check.Dependencies); err != nil {
					return nil, err
				}
			}
			existing = &check
			checkMap[checkID] = existing
			checkOrder = append(checkOrder, checkID)
		}

		// Add result if present (LEFT JOIN may yield NULL)
		if resultID.Valid {
			result := domain.CheckResult{
				ID:        resultID.Int64,
				OwnerType: resultOwnerType.String,
				OwnerID:   resultOwnerID.String,
				CreatedAt: resultCreatedAt.Time,
			}
			if resultDataJSON.Valid && resultDataJSON.String != "" {
				if err := json.Unmarshal([]byte(resultDataJSON.String), &result.Data); err != nil {
					return nil, err
				}
			}
			if result.Data == nil {
				result.Data = make(map[string]any)
			}
			if resultFinalizedAt.Valid {
				result.FinalizedAt = &resultFinalizedAt.Time
			}
			if resultFailureMessage.Valid && resultFailureAt.Valid {
				result.Failure = &domain.Failure{
					Message:    resultFailureMessage.String,
					OccurredAt: resultFailureAt.Time,
				}
			}
			existing.Results = append(existing.Results, result)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Return checks in original order
	checks := make([]*domain.Check, 0, len(checkOrder))
	for _, id := range checkOrder {
		checks = append(checks, checkMap[id])
	}

	return checks, nil
}

func (r *checkRepo) Delete(ctx context.Context, workPlanID, checkID string) error {
	result, err := r.tx.ExecContext(ctx, `DELETE FROM checks WHERE work_plan_id = ? AND id = ?`, workPlanID, checkID)
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

func (r *checkRepo) AddResult(ctx context.Context, workPlanID, checkID string, result *domain.CheckResult) error {
	dataJSON, err := json.Marshal(result.Data)
	if err != nil {
		return err
	}

	var failureMessage sql.NullString
	var failureAt sql.NullTime
	if result.Failure != nil {
		failureMessage = sql.NullString{String: result.Failure.Message, Valid: true}
		failureAt = sql.NullTime{Time: result.Failure.OccurredAt, Valid: true}
	}

	var finalizedAt sql.NullTime
	if result.FinalizedAt != nil {
		finalizedAt = sql.NullTime{Time: *result.FinalizedAt, Valid: true}
	}

	res, err := r.tx.ExecContext(ctx, `
		INSERT INTO check_results (work_plan_id, check_id, owner_type, owner_id, data_json, created_at, finalized_at, failure_message, failure_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, workPlanID, checkID, result.OwnerType, result.OwnerID, string(dataJSON),
		result.CreatedAt, finalizedAt, failureMessage, failureAt)
	if err != nil {
		return err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	result.ID = id

	return nil
}

func (r *checkRepo) UpdateResult(ctx context.Context, workPlanID, checkID string, resultID int64, result *domain.CheckResult) error {
	dataJSON, err := json.Marshal(result.Data)
	if err != nil {
		return err
	}

	var failureMessage sql.NullString
	var failureAt sql.NullTime
	if result.Failure != nil {
		failureMessage = sql.NullString{String: result.Failure.Message, Valid: true}
		failureAt = sql.NullTime{Time: result.Failure.OccurredAt, Valid: true}
	}

	var finalizedAt sql.NullTime
	if result.FinalizedAt != nil {
		finalizedAt = sql.NullTime{Time: *result.FinalizedAt, Valid: true}
	}

	_, err = r.tx.ExecContext(ctx, `
		UPDATE check_results
		SET data_json = ?, finalized_at = ?, failure_message = ?, failure_at = ?
		WHERE id = ? AND work_plan_id = ? AND check_id = ?
	`, string(dataJSON), finalizedAt, failureMessage, failureAt, resultID, workPlanID, checkID)
	return err
}
