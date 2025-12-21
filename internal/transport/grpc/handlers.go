package grpc

import (
	"context"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/endpoint"
	"github.com/example/turboci-lite/internal/service"
)

// CreateWorkPlan implements the CreateWorkPlan RPC.
func (s *Server) CreateWorkPlan(ctx context.Context, req *pb.CreateWorkPlanRequest) (*pb.CreateWorkPlanResponse, error) {
	svcReq := &service.CreateWorkPlanRequest{
		Metadata: req.GetMetadata(),
	}

	resp, err := s.endpoints.CreateWorkPlan(ctx, svcReq)
	if err != nil {
		return nil, endpoint.MapErrorToStatus(err)
	}

	wp := resp.(*domain.WorkPlan)
	return &pb.CreateWorkPlanResponse{
		WorkPlan: workPlanToProto(wp),
	}, nil
}

// GetWorkPlan implements the GetWorkPlan RPC.
func (s *Server) GetWorkPlan(ctx context.Context, req *pb.GetWorkPlanRequest) (*pb.GetWorkPlanResponse, error) {
	resp, err := s.endpoints.GetWorkPlan(ctx, req.GetId())
	if err != nil {
		return nil, endpoint.MapErrorToStatus(err)
	}

	wp := resp.(*domain.WorkPlan)
	return &pb.GetWorkPlanResponse{
		WorkPlan: workPlanToProto(wp),
	}, nil
}

// WriteNodes implements the WriteNodes RPC.
func (s *Server) WriteNodes(ctx context.Context, req *pb.WriteNodesRequest) (*pb.WriteNodesResponse, error) {
	svcReq := &service.WriteNodesRequest{
		WorkPlanID: req.GetWorkPlanId(),
		Checks:     make([]*service.CheckWrite, 0, len(req.GetChecks())),
		Stages:     make([]*service.StageWrite, 0, len(req.GetStages())),
	}

	for _, cw := range req.GetChecks() {
		svcReq.Checks = append(svcReq.Checks, checkWriteFromProto(cw))
	}

	for _, sw := range req.GetStages() {
		svcReq.Stages = append(svcReq.Stages, stageWriteFromProto(sw))
	}

	resp, err := s.endpoints.WriteNodes(ctx, svcReq)
	if err != nil {
		return nil, endpoint.MapErrorToStatus(err)
	}

	result := resp.(*service.WriteNodesResponse)
	pbResp := &pb.WriteNodesResponse{
		Checks: make([]*pb.Check, 0, len(result.Checks)),
		Stages: make([]*pb.Stage, 0, len(result.Stages)),
	}

	for _, c := range result.Checks {
		pbResp.Checks = append(pbResp.Checks, checkToProto(c))
	}

	for _, s := range result.Stages {
		pbResp.Stages = append(pbResp.Stages, stageToProto(s))
	}

	return pbResp, nil
}

// QueryNodes implements the QueryNodes RPC.
func (s *Server) QueryNodes(ctx context.Context, req *pb.QueryNodesRequest) (*pb.QueryNodesResponse, error) {
	svcReq := &service.QueryNodesRequest{
		WorkPlanID:    req.GetWorkPlanId(),
		CheckIDs:      req.GetCheckIds(),
		StageIDs:      req.GetStageIds(),
		IncludeChecks: req.GetIncludeChecks(),
		IncludeStages: req.GetIncludeStages(),
	}

	for _, s := range req.GetCheckStates() {
		svcReq.CheckStates = append(svcReq.CheckStates, domain.CheckState(s))
	}

	for _, s := range req.GetStageStates() {
		svcReq.StageStates = append(svcReq.StageStates, domain.StageState(s))
	}

	resp, err := s.endpoints.QueryNodes(ctx, svcReq)
	if err != nil {
		return nil, endpoint.MapErrorToStatus(err)
	}

	result := resp.(*service.QueryNodesResponse)
	pbResp := &pb.QueryNodesResponse{
		Checks: make([]*pb.Check, 0, len(result.Checks)),
		Stages: make([]*pb.Stage, 0, len(result.Stages)),
	}

	for _, c := range result.Checks {
		pbResp.Checks = append(pbResp.Checks, checkToProto(c))
	}

	for _, s := range result.Stages {
		pbResp.Stages = append(pbResp.Stages, stageToProto(s))
	}

	return pbResp, nil
}

// Conversion functions

func workPlanToProto(wp *domain.WorkPlan) *pb.WorkPlan {
	return &pb.WorkPlan{
		Id:        wp.ID,
		CreatedAt: timestamppb.New(wp.CreatedAt),
		UpdatedAt: timestamppb.New(wp.UpdatedAt),
		Version:   wp.Version,
	}
}

func checkToProto(c *domain.Check) *pb.Check {
	check := &pb.Check{
		Id:         c.ID,
		WorkPlanId: c.WorkPlanID,
		State:      pb.CheckState(c.State),
		Kind:       c.Kind,
		CreatedAt:  timestamppb.New(c.CreatedAt),
		UpdatedAt:  timestamppb.New(c.UpdatedAt),
		Version:    c.Version,
	}

	if c.Options != nil {
		if s, err := structpb.NewStruct(c.Options); err == nil {
			check.Options = s
		}
	}

	if c.Dependencies != nil {
		check.Dependencies = dependencyGroupToProto(c.Dependencies)
	}

	for _, r := range c.Results {
		check.Results = append(check.Results, checkResultToProto(&r))
	}

	return check
}

func checkResultToProto(r *domain.CheckResult) *pb.CheckResult {
	result := &pb.CheckResult{
		Id:        r.ID,
		OwnerType: r.OwnerType,
		OwnerId:   r.OwnerID,
		CreatedAt: timestamppb.New(r.CreatedAt),
	}

	if r.Data != nil {
		if s, err := structpb.NewStruct(r.Data); err == nil {
			result.Data = s
		}
	}

	if r.FinalizedAt != nil {
		result.FinalizedAt = timestamppb.New(*r.FinalizedAt)
	}

	if r.Failure != nil {
		result.Failure = failureToProto(r.Failure)
	}

	return result
}

func stageToProto(s *domain.Stage) *pb.Stage {
	stage := &pb.Stage{
		Id:         s.ID,
		WorkPlanId: s.WorkPlanID,
		State:      pb.StageState(s.State),
		CreatedAt:  timestamppb.New(s.CreatedAt),
		UpdatedAt:  timestamppb.New(s.UpdatedAt),
		Version:    s.Version,
	}

	if s.Args != nil {
		if st, err := structpb.NewStruct(s.Args); err == nil {
			stage.Args = st
		}
	}

	if s.Dependencies != nil {
		stage.Dependencies = dependencyGroupToProto(s.Dependencies)
	}

	for _, a := range s.Assignments {
		stage.Assignments = append(stage.Assignments, &pb.Assignment{
			TargetCheckId: a.TargetCheckID,
			GoalState:     pb.CheckState(a.GoalState),
		})
	}

	for _, att := range s.Attempts {
		stage.Attempts = append(stage.Attempts, attemptToProto(&att))
	}

	return stage
}

func attemptToProto(a *domain.Attempt) *pb.Attempt {
	attempt := &pb.Attempt{
		Idx:        int32(a.Idx),
		State:      pb.AttemptState(a.State),
		ProcessUid: a.ProcessUID,
		CreatedAt:  timestamppb.New(a.CreatedAt),
		UpdatedAt:  timestamppb.New(a.UpdatedAt),
	}

	if a.Details != nil {
		if s, err := structpb.NewStruct(a.Details); err == nil {
			attempt.Details = s
		}
	}

	for _, p := range a.Progress {
		pe := &pb.ProgressEntry{
			Message:   p.Message,
			Timestamp: timestamppb.New(p.Timestamp),
		}
		if p.Details != nil {
			if s, err := structpb.NewStruct(p.Details); err == nil {
				pe.Details = s
			}
		}
		attempt.Progress = append(attempt.Progress, pe)
	}

	if a.Failure != nil {
		attempt.Failure = failureToProto(a.Failure)
	}

	return attempt
}

func dependencyGroupToProto(g *domain.DependencyGroup) *pb.DependencyGroup {
	if g == nil {
		return nil
	}

	dg := &pb.DependencyGroup{
		Predicate: pb.PredicateType(predicateTypeToProto(g.Predicate)),
	}

	for _, d := range g.Dependencies {
		dg.Dependencies = append(dg.Dependencies, &pb.Dependency{
			TargetType: nodeTypeToProto(d.TargetType),
			TargetId:   d.TargetID,
		})
	}

	return dg
}

func failureToProto(f *domain.Failure) *pb.Failure {
	if f == nil {
		return nil
	}
	return &pb.Failure{
		Message:    f.Message,
		OccurredAt: timestamppb.New(f.OccurredAt),
	}
}

func predicateTypeToProto(p domain.PredicateType) pb.PredicateType {
	switch p {
	case domain.PredicateAND:
		return pb.PredicateType_PREDICATE_TYPE_AND
	case domain.PredicateOR:
		return pb.PredicateType_PREDICATE_TYPE_OR
	default:
		return pb.PredicateType_PREDICATE_TYPE_UNKNOWN
	}
}

func nodeTypeToProto(n domain.NodeType) pb.NodeType {
	switch n {
	case domain.NodeTypeCheck:
		return pb.NodeType_NODE_TYPE_CHECK
	case domain.NodeTypeStage:
		return pb.NodeType_NODE_TYPE_STAGE
	default:
		return pb.NodeType_NODE_TYPE_UNKNOWN
	}
}

// From proto conversion functions

func checkWriteFromProto(cw *pb.CheckWrite) *service.CheckWrite {
	write := &service.CheckWrite{
		ID:   cw.GetId(),
		Kind: cw.GetKind(),
	}

	if cw.State != pb.CheckState_CHECK_STATE_UNKNOWN {
		state := domain.CheckState(cw.State)
		write.State = &state
	}

	if cw.Options != nil {
		write.Options = cw.Options.AsMap()
	}

	if cw.Dependencies != nil {
		write.Dependencies = dependencyGroupFromProto(cw.Dependencies)
	}

	if cw.Result != nil {
		write.Result = &service.CheckResultWrite{
			OwnerType: cw.Result.GetOwnerType(),
			OwnerID:   cw.Result.GetOwnerId(),
			Finalize:  cw.Result.GetFinalize(),
		}
		if cw.Result.Data != nil {
			write.Result.Data = cw.Result.Data.AsMap()
		}
		if cw.Result.Failure != nil {
			write.Result.Failure = failureFromProto(cw.Result.Failure)
		}
	}

	return write
}

func stageWriteFromProto(sw *pb.StageWrite) *service.StageWrite {
	write := &service.StageWrite{
		ID: sw.GetId(),
	}

	if sw.State != pb.StageState_STAGE_STATE_UNKNOWN {
		state := domain.StageState(sw.State)
		write.State = &state
	}

	if sw.Args != nil {
		write.Args = sw.Args.AsMap()
	}

	for _, a := range sw.Assignments {
		write.Assignments = append(write.Assignments, domain.Assignment{
			TargetCheckID: a.GetTargetCheckId(),
			GoalState:     domain.CheckState(a.GetGoalState()),
		})
	}

	if sw.Dependencies != nil {
		write.Dependencies = dependencyGroupFromProto(sw.Dependencies)
	}

	if sw.CurrentAttempt != nil {
		write.CurrentAttempt = attemptWriteFromProto(sw.CurrentAttempt)
	}

	return write
}

func attemptWriteFromProto(aw *pb.AttemptWrite) *service.AttemptWrite {
	write := &service.AttemptWrite{
		ProcessUID: aw.GetProcessUid(),
	}

	if aw.State != pb.AttemptState_ATTEMPT_STATE_UNKNOWN {
		state := domain.AttemptState(aw.State)
		write.State = &state
	}

	if aw.Details != nil {
		write.Details = aw.Details.AsMap()
	}

	if aw.Progress != nil {
		write.Progress = &domain.ProgressEntry{
			Message:   aw.Progress.GetMessage(),
			Timestamp: aw.Progress.GetTimestamp().AsTime(),
		}
		if aw.Progress.Details != nil {
			write.Progress.Details = aw.Progress.Details.AsMap()
		}
	}

	if aw.Failure != nil {
		write.Failure = failureFromProto(aw.Failure)
	}

	return write
}

func dependencyGroupFromProto(dg *pb.DependencyGroup) *domain.DependencyGroup {
	if dg == nil {
		return nil
	}

	group := &domain.DependencyGroup{
		Predicate: predicateTypeFromProto(dg.Predicate),
	}

	for _, d := range dg.Dependencies {
		group.Dependencies = append(group.Dependencies, domain.DependencyRef{
			TargetType: nodeTypeFromProto(d.TargetType),
			TargetID:   d.TargetId,
		})
	}

	return group
}

func failureFromProto(f *pb.Failure) *domain.Failure {
	if f == nil {
		return nil
	}
	return &domain.Failure{
		Message:    f.Message,
		OccurredAt: f.OccurredAt.AsTime(),
	}
}

func predicateTypeFromProto(p pb.PredicateType) domain.PredicateType {
	switch p {
	case pb.PredicateType_PREDICATE_TYPE_AND:
		return domain.PredicateAND
	case pb.PredicateType_PREDICATE_TYPE_OR:
		return domain.PredicateOR
	default:
		return domain.PredicateAND
	}
}

func nodeTypeFromProto(n pb.NodeType) domain.NodeType {
	switch n {
	case pb.NodeType_NODE_TYPE_CHECK:
		return domain.NodeTypeCheck
	case pb.NodeType_NODE_TYPE_STAGE:
		return domain.NodeTypeStage
	default:
		return domain.NodeTypeCheck
	}
}
