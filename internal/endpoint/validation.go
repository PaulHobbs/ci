package endpoint

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/example/turboci-lite/internal/service"
)

func validateWriteNodesRequest(req *service.WriteNodesRequest) error {
	if req.WorkPlanID == "" {
		return status.Error(codes.InvalidArgument, "work_plan_id is required")
	}

	for i, cw := range req.Checks {
		if err := validateCheckWrite(cw); err != nil {
			return status.Errorf(codes.InvalidArgument, "check[%d]: %v", i, err)
		}
	}

	for i, sw := range req.Stages {
		if err := validateStageWrite(sw); err != nil {
			return status.Errorf(codes.InvalidArgument, "stage[%d]: %v", i, err)
		}
	}

	return nil
}

func validateCheckWrite(cw *service.CheckWrite) error {
	if cw.ID == "" {
		return status.Error(codes.InvalidArgument, "id is required")
	}
	return nil
}

func validateStageWrite(sw *service.StageWrite) error {
	if sw.ID == "" {
		return status.Error(codes.InvalidArgument, "id is required")
	}
	return nil
}

func validateQueryNodesRequest(req *service.QueryNodesRequest) error {
	if req.WorkPlanID == "" {
		return status.Error(codes.InvalidArgument, "work_plan_id is required")
	}
	return nil
}
