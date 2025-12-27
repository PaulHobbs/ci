package web

import (
	"time"

	"github.com/example/turboci-lite/internal/domain"
)

// TimelineResponse is the response for GET /api/workplans/:id/timeline
type TimelineResponse struct {
	WorkPlanID string         `json:"workPlanId"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	Nodes      []TimelineNode `json:"nodes"`
}

// TimelineNode represents a node (check or stage) in the timeline
type TimelineNode struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // "check" or "stage"
	State        string         `json:"state"`
	StartTime    *time.Time     `json:"startTime,omitempty"`    // When execution actually started (not creation)
	EndTime      *time.Time     `json:"endTime,omitempty"`      // When execution completed
	CreatedAt    time.Time      `json:"createdAt"`              // When node was created (for reference)
	Dependencies []string       `json:"dependencies,omitempty"`

	// Timing instrumentation (in milliseconds)
	QueueDurationMs     *int64 `json:"queueDurationMs,omitempty"`     // Time from creation to execution start
	ExecutionDurationMs *int64 `json:"executionDurationMs,omitempty"` // Time from execution start to end
	TotalDurationMs     *int64 `json:"totalDurationMs,omitempty"`     // Total time from creation to completion

	// Check-specific fields
	Kind    string         `json:"kind,omitempty"`
	Options map[string]any `json:"options,omitempty"`

	// Stage-specific fields
	RunnerType    string          `json:"runnerType,omitempty"`
	ExecutionMode string          `json:"executionMode,omitempty"`
	Args          map[string]any  `json:"args,omitempty"`
	Attempts      []AttemptInfo   `json:"attempts,omitempty"`
}

// AttemptInfo represents a stage execution attempt
type AttemptInfo struct {
	Idx        int            `json:"idx"`
	State      string         `json:"state"`
	StartTime  time.Time      `json:"startTime"`
	EndTime    *time.Time     `json:"endTime,omitempty"`
	DurationMs *int64         `json:"durationMs,omitempty"` // Actual execution duration
	Progress   []ProgressInfo `json:"progress,omitempty"`
	Failure    *FailureInfo   `json:"failure,omitempty"`
}

// ProgressInfo represents a progress update
type ProgressInfo struct {
	Message   string         `json:"message"`
	Timestamp time.Time      `json:"timestamp"`
	Details   map[string]any `json:"details,omitempty"`
}

// FailureInfo represents a failure
type FailureInfo struct {
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurredAt"`
}

// WorkPlanSummary is a summary of a work plan for listing
type WorkPlanSummary struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	NodeCount NodeCount         `json:"nodeCount"`
}

// NodeCount contains counts of nodes by type and state
type NodeCount struct {
	Checks int `json:"checks"`
	Stages int `json:"stages"`
}

// ListWorkPlansResponse is the response for GET /api/workplans
type ListWorkPlansResponse struct {
	WorkPlans []WorkPlanSummary `json:"workPlans"`
}

// convertCheck converts a domain.Check to a TimelineNode
func convertCheck(check *domain.Check) TimelineNode {
	node := TimelineNode{
		ID:        check.ID,
		Type:      "check",
		Kind:      check.Kind,
		State:     check.State.String(),
		Options:   check.Options,
		CreatedAt: check.CreatedAt,
	}

	// For checks, StartTime is when it transitioned to WAITING state
	// (when its fulfilling stage started executing)
	// We approximate this as when results started arriving, or use CreatedAt as fallback
	if len(check.Results) > 0 {
		// Use first result's creation time as start
		node.StartTime = &check.Results[0].CreatedAt
	} else if check.State == domain.CheckStateWaiting || check.State == domain.CheckStateFinal {
		// No results yet but state advanced - use UpdatedAt as approximation
		node.StartTime = &check.UpdatedAt
	}
	// If still PLANNED/PLANNING, leave StartTime nil (not started yet)

	// End time is UpdatedAt if the check is in a final state
	if check.State == domain.CheckStateFinal {
		node.EndTime = &check.UpdatedAt

		// Calculate durations
		if node.StartTime != nil {
			execMs := check.UpdatedAt.Sub(*node.StartTime).Milliseconds()
			node.ExecutionDurationMs = &execMs
		}
		totalMs := check.UpdatedAt.Sub(check.CreatedAt).Milliseconds()
		node.TotalDurationMs = &totalMs

		if node.StartTime != nil {
			queueMs := node.StartTime.Sub(check.CreatedAt).Milliseconds()
			node.QueueDurationMs = &queueMs
		}
	}

	// Convert dependencies
	if check.Dependencies != nil {
		for _, dep := range check.Dependencies.Dependencies {
			node.Dependencies = append(node.Dependencies, string(dep.TargetType)+":"+dep.TargetID)
		}
	}

	return node
}

// convertStage converts a domain.Stage to a TimelineNode
func convertStage(stage *domain.Stage) TimelineNode {
	node := TimelineNode{
		ID:            stage.ID,
		Type:          "stage",
		State:         stage.State.String(),
		RunnerType:    stage.RunnerType,
		ExecutionMode: stage.ExecutionMode.String(),
		Args:          stage.Args,
		CreatedAt:     stage.CreatedAt,
	}

	// StartTime is when execution actually started (first attempt's CreatedAt)
	// NOT when the stage was created in the database
	if len(stage.Attempts) > 0 {
		node.StartTime = &stage.Attempts[0].CreatedAt
	}
	// If no attempts yet, leave StartTime nil (hasn't started executing)

	// End time is when the last attempt completed (if in final state)
	if stage.State == domain.StageStateFinal {
		node.EndTime = &stage.UpdatedAt

		// Calculate durations
		if node.StartTime != nil {
			execMs := stage.UpdatedAt.Sub(*node.StartTime).Milliseconds()
			node.ExecutionDurationMs = &execMs

			queueMs := node.StartTime.Sub(stage.CreatedAt).Milliseconds()
			node.QueueDurationMs = &queueMs
		}
		totalMs := stage.UpdatedAt.Sub(stage.CreatedAt).Milliseconds()
		node.TotalDurationMs = &totalMs
	}

	// Convert attempts
	for _, attempt := range stage.Attempts {
		attemptInfo := AttemptInfo{
			Idx:       attempt.Idx,
			State:     attempt.State.String(),
			StartTime: attempt.CreatedAt,
		}

		if attempt.State.IsFinal() {
			attemptInfo.EndTime = &attempt.UpdatedAt

			// Add duration to attempt info
			durationMs := attempt.UpdatedAt.Sub(attempt.CreatedAt).Milliseconds()
			attemptInfo.DurationMs = &durationMs
		}

		for _, prog := range attempt.Progress {
			attemptInfo.Progress = append(attemptInfo.Progress, ProgressInfo{
				Message:   prog.Message,
				Timestamp: prog.Timestamp,
				Details:   prog.Details,
			})
		}

		if attempt.Failure != nil {
			attemptInfo.Failure = &FailureInfo{
				Message:    attempt.Failure.Message,
				OccurredAt: attempt.Failure.OccurredAt,
			}
		}

		node.Attempts = append(node.Attempts, attemptInfo)
	}

	// Convert dependencies
	if stage.Dependencies != nil {
		for _, dep := range stage.Dependencies.Dependencies {
			node.Dependencies = append(node.Dependencies, string(dep.TargetType)+":"+dep.TargetID)
		}
	}

	return node
}
