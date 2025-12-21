package endpoint

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// Endpoint is a function that takes a request and returns a response.
type Endpoint func(ctx context.Context, request any) (response any, err error)

// Endpoints holds all endpoint handlers.
type Endpoints struct {
	CreateWorkPlan Endpoint
	GetWorkPlan    Endpoint
	WriteNodes     Endpoint
	QueryNodes     Endpoint
}

// MakeEndpoints creates all endpoints from the service.
func MakeEndpoints(svc *service.OrchestratorService) Endpoints {
	return Endpoints{
		CreateWorkPlan: makeCreateWorkPlanEndpoint(svc),
		GetWorkPlan:    makeGetWorkPlanEndpoint(svc),
		WriteNodes:     makeWriteNodesEndpoint(svc),
		QueryNodes:     makeQueryNodesEndpoint(svc),
	}
}

func makeCreateWorkPlanEndpoint(svc *service.OrchestratorService) Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(*service.CreateWorkPlanRequest)
		return svc.CreateWorkPlan(ctx, req)
	}
}

func makeGetWorkPlanEndpoint(svc *service.OrchestratorService) Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		id := request.(string)
		if id == "" {
			return nil, status.Error(codes.InvalidArgument, "work plan ID is required")
		}
		return svc.GetWorkPlan(ctx, id)
	}
}

func makeWriteNodesEndpoint(svc *service.OrchestratorService) Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(*service.WriteNodesRequest)
		if err := validateWriteNodesRequest(req); err != nil {
			return nil, err
		}
		return svc.WriteNodes(ctx, req)
	}
}

func makeQueryNodesEndpoint(svc *service.OrchestratorService) Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(*service.QueryNodesRequest)
		if err := validateQueryNodesRequest(req); err != nil {
			return nil, err
		}
		return svc.QueryNodes(ctx, req)
	}
}

// MapErrorToStatus maps domain errors to gRPC status codes.
func MapErrorToStatus(err error) error {
	if err == nil {
		return nil
	}

	// Already a gRPC status error
	if _, ok := status.FromError(err); ok {
		return err
	}

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrInvalidState):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, domain.ErrConcurrentModify):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, domain.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, domain.ErrInvalidDependency):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrCyclicDependency):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrAttemptClaimed):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, "internal error")
	}
}
