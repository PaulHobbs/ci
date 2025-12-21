package domain

import "time"

// WorkPlan is a container for an entire workflow execution.
type WorkPlan struct {
	ID        string
	Metadata  map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int64
}

// NewWorkPlan creates a new WorkPlan with the given ID.
func NewWorkPlan(id string) *WorkPlan {
	now := time.Now().UTC()
	return &WorkPlan{
		ID:        id,
		Metadata:  make(map[string]string),
		CreatedAt: now,
		UpdatedAt: now,
		Version:   1,
	}
}
