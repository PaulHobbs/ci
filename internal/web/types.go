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
	StartTime    *time.Time     `json:"startTime,omitempty"`
	EndTime      *time.Time     `json:"endTime,omitempty"`
	Dependencies []string       `json:"dependencies,omitempty"`

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
	Idx       int            `json:"idx"`
	State     string         `json:"state"`
	StartTime time.Time      `json:"startTime"`
	EndTime   *time.Time     `json:"endTime,omitempty"`
	Progress  []ProgressInfo `json:"progress,omitempty"`
	Failure   *FailureInfo   `json:"failure,omitempty"`
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
		StartTime: &check.CreatedAt,
	}

	// End time is UpdatedAt if the check is in a final state
	if check.State == domain.CheckStateFinal {
		node.EndTime = &check.UpdatedAt
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
		StartTime:     &stage.CreatedAt,
	}

	// End time is UpdatedAt if the stage is in a final state
	if stage.State == domain.StageStateFinal {
		node.EndTime = &stage.UpdatedAt
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
