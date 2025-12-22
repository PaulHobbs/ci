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
			execution_mode INTEGER NOT NULL DEFAULT 1,
			runner_type TEXT,
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

		// Stage Runners table (registrations)
		`CREATE TABLE IF NOT EXISTS stage_runners (
			registration_id TEXT PRIMARY KEY,
			id TEXT NOT NULL,
			runner_type TEXT NOT NULL,
			address TEXT NOT NULL,
			supported_modes_json TEXT NOT NULL,
			max_concurrent INTEGER NOT NULL DEFAULT 0,
			current_load INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT,
			registered_at DATETIME NOT NULL,
			last_heartbeat DATETIME NOT NULL,
			expires_at DATETIME NOT NULL
		)`,

		// Stage Executions queue table
		`CREATE TABLE IF NOT EXISTS stage_executions (
			id TEXT PRIMARY KEY,
			work_plan_id TEXT NOT NULL,
			stage_id TEXT NOT NULL,
			attempt_idx INTEGER NOT NULL,
			runner_type TEXT NOT NULL,
			execution_mode INTEGER NOT NULL DEFAULT 1,
			state INTEGER NOT NULL DEFAULT 10,
			runner_id TEXT,
			dispatched_at DATETIME,
			started_at DATETIME,
			completed_at DATETIME,
			deadline DATETIME,
			last_progress_at DATETIME,
			progress_percent INTEGER DEFAULT 0,
			progress_message TEXT,
			error_message TEXT,
			retry_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY (work_plan_id) REFERENCES work_plans(id) ON DELETE CASCADE
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

		// Indexes for stage runners and executions
		`CREATE INDEX IF NOT EXISTS idx_runners_type ON stage_runners(runner_type)`,
		`CREATE INDEX IF NOT EXISTS idx_runners_expires ON stage_runners(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_executions_pending ON stage_executions(state, runner_type)`,
		`CREATE INDEX IF NOT EXISTS idx_executions_work_plan ON stage_executions(work_plan_id, stage_id)`,
		`CREATE INDEX IF NOT EXISTS idx_executions_runner ON stage_executions(runner_id)`,
	}

	for _, migration := range migrations {
		if _, err := db.ExecContext(ctx, migration); err != nil {
			return err
		}
	}

	return nil
}
