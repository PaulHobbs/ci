package sqlite

import (
	"context"
	"database/sql"
)

// Migrate runs all database migrations.
func Migrate(ctx context.Context, db *sql.DB) error {
	migrations := []string{
		// Work Plans table
		`CREATE TABLE IF NOT EXISTS work_plans (
			id TEXT PRIMARY KEY,
			metadata_json TEXT,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			version INTEGER NOT NULL DEFAULT 1
		)`,

		// Checks table
		`CREATE TABLE IF NOT EXISTS checks (
			id TEXT NOT NULL,
			work_plan_id TEXT NOT NULL,
			state INTEGER NOT NULL DEFAULT 10,
			kind TEXT,
			options_json TEXT,
			dependencies_json TEXT,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (work_plan_id, id),
			FOREIGN KEY (work_plan_id) REFERENCES work_plans(id) ON DELETE CASCADE
		)`,

		// Check Results table
		`CREATE TABLE IF NOT EXISTS check_results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			work_plan_id TEXT NOT NULL,
			check_id TEXT NOT NULL,
			owner_type TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			data_json TEXT,
			created_at DATETIME NOT NULL,
			finalized_at DATETIME,
			failure_message TEXT,
			failure_at DATETIME,
			FOREIGN KEY (work_plan_id, check_id) REFERENCES checks(work_plan_id, id) ON DELETE CASCADE
		)`,

		// Stages table
		`CREATE TABLE IF NOT EXISTS stages (
			id TEXT NOT NULL,
			work_plan_id TEXT NOT NULL,
			state INTEGER NOT NULL DEFAULT 10,
			args_json TEXT,
			assignments_json TEXT,
			dependencies_json TEXT,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (work_plan_id, id),
			FOREIGN KEY (work_plan_id) REFERENCES work_plans(id) ON DELETE CASCADE
		)`,

		// Stage Attempts table
		`CREATE TABLE IF NOT EXISTS stage_attempts (
			idx INTEGER NOT NULL,
			work_plan_id TEXT NOT NULL,
			stage_id TEXT NOT NULL,
			state INTEGER NOT NULL DEFAULT 10,
			process_uid TEXT,
			details_json TEXT,
			progress_json TEXT,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			failure_message TEXT,
			failure_at DATETIME,
			PRIMARY KEY (work_plan_id, stage_id, idx),
			FOREIGN KEY (work_plan_id, stage_id) REFERENCES stages(work_plan_id, id) ON DELETE CASCADE
		)`,

		// Dependencies table
		`CREATE TABLE IF NOT EXISTS dependencies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			work_plan_id TEXT NOT NULL,
			source_type TEXT NOT NULL,
			source_id TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			resolved BOOLEAN DEFAULT FALSE,
			satisfied BOOLEAN,
			resolved_at DATETIME,
			UNIQUE(work_plan_id, source_type, source_id, target_type, target_id)
		)`,

		// Indexes for efficient queries
		`CREATE INDEX IF NOT EXISTS idx_checks_work_plan ON checks(work_plan_id)`,
		`CREATE INDEX IF NOT EXISTS idx_checks_state ON checks(work_plan_id, state)`,
		`CREATE INDEX IF NOT EXISTS idx_stages_work_plan ON stages(work_plan_id)`,
		`CREATE INDEX IF NOT EXISTS idx_stages_state ON stages(work_plan_id, state)`,
		`CREATE INDEX IF NOT EXISTS idx_deps_source ON dependencies(work_plan_id, source_type, source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_deps_target ON dependencies(work_plan_id, target_type, target_id)`,
		`CREATE INDEX IF NOT EXISTS idx_deps_unresolved ON dependencies(work_plan_id, resolved) WHERE resolved = FALSE`,
		`CREATE INDEX IF NOT EXISTS idx_check_results ON check_results(work_plan_id, check_id)`,
		`CREATE INDEX IF NOT EXISTS idx_stage_attempts ON stage_attempts(work_plan_id, stage_id)`,
	}

	for _, migration := range migrations {
		if _, err := db.ExecContext(ctx, migration); err != nil {
			return err
		}
	}

	return nil
}
