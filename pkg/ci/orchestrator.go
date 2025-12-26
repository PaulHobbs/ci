package ci

import (
	"context"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// Orchestrator defines the interface for workflow orchestration.
// This allows the CI package to work with either a local service or a remote gRPC client.
type Orchestrator interface {
	// CreateWorkPlan creates a new empty WorkPlan.
	CreateWorkPlan(ctx context.Context, req *service.CreateWorkPlanRequest) (*domain.WorkPlan, error)

	// WriteNodes atomically writes or updates multiple nodes within a WorkPlan.
	WriteNodes(ctx context.Context, req *service.WriteNodesRequest) (*service.WriteNodesResponse, error)

	// QueryNodes queries nodes in a WorkPlan.
	QueryNodes(ctx context.Context, req *service.QueryNodesRequest) (*service.QueryNodesResponse, error)
}

// LocalOrchestrator wraps a service.OrchestratorService for local (embedded) use.
type LocalOrchestrator struct {
	svc *service.OrchestratorService
}

// NewLocalOrchestrator creates an Orchestrator that uses a local service.
func NewLocalOrchestrator(svc *service.OrchestratorService) Orchestrator {
	return &LocalOrchestrator{svc: svc}
}

// Service returns the underlying OrchestratorService for direct access when needed.
func (o *LocalOrchestrator) Service() *service.OrchestratorService {
	return o.svc
}

func (o *LocalOrchestrator) CreateWorkPlan(ctx context.Context, req *service.CreateWorkPlanRequest) (*domain.WorkPlan, error) {
	return o.svc.CreateWorkPlan(ctx, req)
}

func (o *LocalOrchestrator) WriteNodes(ctx context.Context, req *service.WriteNodesRequest) (*service.WriteNodesResponse, error) {
	return o.svc.WriteNodes(ctx, req)
}

func (o *LocalOrchestrator) QueryNodes(ctx context.Context, req *service.QueryNodesRequest) (*service.QueryNodesResponse, error) {
	return o.svc.QueryNodes(ctx, req)
}
