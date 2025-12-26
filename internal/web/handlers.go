package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage"
)

// Handlers contains HTTP handlers for the web API
type Handlers struct {
	orchestrator *service.OrchestratorService
	storage      storage.Storage
}

// NewHandlers creates new API handlers
func NewHandlers(orchestrator *service.OrchestratorService, storage storage.Storage) *Handlers {
	return &Handlers{
		orchestrator: orchestrator,
		storage:      storage,
	}
}

// ListWorkPlans handles GET /api/workplans
func (h *Handlers) ListWorkPlans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	uow, err := h.storage.Begin(ctx)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer uow.Rollback()

	workPlans, err := uow.WorkPlans().List(ctx)
	if err != nil {
		http.Error(w, "Failed to list work plans: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := ListWorkPlansResponse{
		WorkPlans: make([]WorkPlanSummary, 0, len(workPlans)),
	}

	for _, wp := range workPlans {
		// Get node counts for each work plan
		checks, _ := uow.Checks().List(ctx, wp.ID, storage.ListOptions{})
		stages, _ := uow.Stages().List(ctx, wp.ID, storage.ListOptions{})

		summary := WorkPlanSummary{
			ID:        wp.ID,
			CreatedAt: wp.CreatedAt,
			UpdatedAt: wp.UpdatedAt,
			Metadata:  wp.Metadata,
			NodeCount: NodeCount{
				Checks: len(checks),
				Stages: len(stages),
			},
		}
		response.WorkPlans = append(response.WorkPlans, summary)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetTimeline handles GET /api/workplans/:id/timeline
func (h *Handlers) GetTimeline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract work plan ID from path
	// Path format: /api/workplans/{id}/timeline
	path := strings.TrimPrefix(r.URL.Path, "/api/workplans/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "timeline" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	workPlanID := parts[0]

	// Get work plan
	workPlan, err := h.orchestrator.GetWorkPlan(ctx, workPlanID)
	if err != nil {
		if err == domain.ErrNotFound {
			http.Error(w, "Work plan not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get work plan: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Query all nodes
	queryResp, err := h.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		IncludeChecks: true,
		IncludeStages: true,
	})
	if err != nil {
		http.Error(w, "Failed to query nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build timeline response
	response := TimelineResponse{
		WorkPlanID: workPlan.ID,
		CreatedAt:  workPlan.CreatedAt,
		UpdatedAt:  workPlan.UpdatedAt,
		Nodes:      make([]TimelineNode, 0, len(queryResp.Checks)+len(queryResp.Stages)),
	}

	for _, check := range queryResp.Checks {
		response.Nodes = append(response.Nodes, convertCheck(check))
	}

	for _, stage := range queryResp.Stages {
		response.Nodes = append(response.Nodes, convertStage(stage))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetWorkPlan handles GET /api/workplans/:id
func (h *Handlers) GetWorkPlan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract work plan ID from path
	workPlanID := strings.TrimPrefix(r.URL.Path, "/api/workplans/")
	if workPlanID == "" {
		http.Error(w, "Work plan ID required", http.StatusBadRequest)
		return
	}

	workPlan, err := h.orchestrator.GetWorkPlan(ctx, workPlanID)
	if err != nil {
		if err == domain.ErrNotFound {
			http.Error(w, "Work plan not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get work plan: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(workPlan)
}
