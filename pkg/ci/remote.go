package ci

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// RemoteOrchestrator implements Orchestrator using a gRPC client.
type RemoteOrchestrator struct {
	client pb.TurboCIOrchestratorClient
	conn   *grpc.ClientConn
}

// NewRemoteOrchestrator creates an Orchestrator that connects to a remote server.
func NewRemoteOrchestrator(addr string) (*RemoteOrchestrator, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &RemoteOrchestrator{
		client: pb.NewTurboCIOrchestratorClient(conn),
		conn:   conn,
	}, nil
}

// Close closes the underlying gRPC connection.
func (o *RemoteOrchestrator) Close() error {
	if o.conn != nil {
		return o.conn.Close()
	}
	return nil
}

// Client returns the underlying gRPC client for direct access when needed.
func (o *RemoteOrchestrator) Client() pb.TurboCIOrchestratorClient {
	return o.client
}

func (o *RemoteOrchestrator) CreateWorkPlan(ctx context.Context, req *service.CreateWorkPlanRequest) (*domain.WorkPlan, error) {
	pbReq := &pb.CreateWorkPlanRequest{}
	if req != nil {
		pbReq.Metadata = req.Metadata
	}

	resp, err := o.client.CreateWorkPlan(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	return workPlanFromProto(resp.WorkPlan), nil
}

func (o *RemoteOrchestrator) WriteNodes(ctx context.Context, req *service.WriteNodesRequest) (*service.WriteNodesResponse, error) {
	pbReq := &pb.WriteNodesRequest{
		WorkPlanId: req.WorkPlanID,
	}

	for _, cw := range req.Checks {
		pbReq.Checks = append(pbReq.Checks, checkWriteToProto(cw))
	}

	for _, sw := range req.Stages {
		pbReq.Stages = append(pbReq.Stages, stageWriteToProto(sw))
	}

	resp, err := o.client.WriteNodes(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	result := &service.WriteNodesResponse{}
	for _, c := range resp.Checks {
		result.Checks = append(result.Checks, checkFromProto(c))
	}
	for _, s := range resp.Stages {
		result.Stages = append(result.Stages, stageFromProto(s))
	}

	return result, nil
}

func (o *RemoteOrchestrator) QueryNodes(ctx context.Context, req *service.QueryNodesRequest) (*service.QueryNodesResponse, error) {
	pbReq := &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanID,
		CheckIds:      req.CheckIDs,
		StageIds:      req.StageIDs,
		IncludeChecks: req.IncludeChecks,
		IncludeStages: req.IncludeStages,
	}

	for _, s := range req.CheckStates {
		pbReq.CheckStates = append(pbReq.CheckStates, pb.CheckState(s))
	}
	for _, s := range req.StageStates {
		pbReq.StageStates = append(pbReq.StageStates, pb.StageState(s))
	}

	resp, err := o.client.QueryNodes(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	result := &service.QueryNodesResponse{}
	for _, c := range resp.Checks {
		result.Checks = append(result.Checks, checkFromProto(c))
	}
	for _, s := range resp.Stages {
		result.Stages = append(result.Stages, stageFromProto(s))
	}

	return result, nil
}

// Proto conversion functions

func workPlanFromProto(wp *pb.WorkPlan) *domain.WorkPlan {
	return &domain.WorkPlan{
		ID:        wp.Id,
		CreatedAt: wp.CreatedAt.AsTime(),
		UpdatedAt: wp.UpdatedAt.AsTime(),
		Version:   wp.Version,
	}
}

func checkFromProto(c *pb.Check) *domain.Check {
	check := &domain.Check{
		ID:         c.Id,
		WorkPlanID: c.WorkPlanId,
		State:      domain.CheckState(c.State),
		Kind:       c.Kind,
		CreatedAt:  c.CreatedAt.AsTime(),
		UpdatedAt:  c.UpdatedAt.AsTime(),
		Version:    c.Version,
	}

	if c.Options != nil {
		check.Options = c.Options.AsMap()
	}

	if c.Dependencies != nil {
		check.Dependencies = dependencyGroupFromProto(c.Dependencies)
	}

	for _, r := range c.Results {
		check.Results = append(check.Results, checkResultFromProto(r))
	}

	return check
}

func checkResultFromProto(r *pb.CheckResult) domain.CheckResult {
	result := domain.CheckResult{
		ID:        r.Id,
		OwnerType: r.OwnerType,
		OwnerID:   r.OwnerId,
		CreatedAt: r.CreatedAt.AsTime(),
	}

	if r.Data != nil {
		result.Data = r.Data.AsMap()
	}

	if r.FinalizedAt != nil {
		t := r.FinalizedAt.AsTime()
		result.FinalizedAt = &t
	}

	if r.Failure != nil {
		result.Failure = failureFromProto(r.Failure)
	}

	return result
}

func stageFromProto(s *pb.Stage) *domain.Stage {
	stage := &domain.Stage{
		ID:            s.Id,
		WorkPlanID:    s.WorkPlanId,
		State:         domain.StageState(s.State),
		ExecutionMode: domain.ExecutionMode(s.ExecutionMode),
		RunnerType:    s.RunnerType,
		CreatedAt:     s.CreatedAt.AsTime(),
		UpdatedAt:     s.UpdatedAt.AsTime(),
		Version:       s.Version,
	}

	if s.Args != nil {
		stage.Args = s.Args.AsMap()
	}

	if s.Dependencies != nil {
		stage.Dependencies = dependencyGroupFromProto(s.Dependencies)
	}

	for _, a := range s.Assignments {
		stage.Assignments = append(stage.Assignments, domain.Assignment{
			TargetCheckID: a.TargetCheckId,
			GoalState:     domain.CheckState(a.GoalState),
		})
	}

	for _, att := range s.Attempts {
		stage.Attempts = append(stage.Attempts, attemptFromProto(att))
	}

	return stage
}

func attemptFromProto(a *pb.Attempt) domain.Attempt {
	attempt := domain.Attempt{
		Idx:        int(a.Idx),
		State:      domain.AttemptState(a.State),
		ProcessUID: a.ProcessUid,
		CreatedAt:  a.CreatedAt.AsTime(),
		UpdatedAt:  a.UpdatedAt.AsTime(),
	}

	if a.Details != nil {
		attempt.Details = a.Details.AsMap()
	}

	for _, p := range a.Progress {
		pe := domain.ProgressEntry{
			Message:   p.Message,
			Timestamp: p.Timestamp.AsTime(),
		}
		if p.Details != nil {
			pe.Details = p.Details.AsMap()
		}
		attempt.Progress = append(attempt.Progress, pe)
	}

	if a.Failure != nil {
		attempt.Failure = failureFromProto(a.Failure)
	}

	return attempt
}

func dependencyGroupFromProto(dg *pb.DependencyGroup) *domain.DependencyGroup {
	if dg == nil {
		return nil
	}

	group := &domain.DependencyGroup{
		Predicate: predicateFromProto(dg.Predicate),
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

func predicateFromProto(p pb.PredicateType) domain.PredicateType {
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

// To proto conversion functions

func checkWriteToProto(cw *service.CheckWrite) *pb.CheckWrite {
	write := &pb.CheckWrite{
		Id:   cw.ID,
		Kind: cw.Kind,
	}

	if cw.State != nil {
		write.State = pb.CheckState(*cw.State)
	}

	if cw.Options != nil {
		if s, err := structpb.NewStruct(cw.Options); err == nil {
			write.Options = s
		}
	}

	if cw.Dependencies != nil {
		write.Dependencies = dependencyGroupToProto(cw.Dependencies)
	}

	if cw.Result != nil {
		write.Result = &pb.CheckResultWrite{
			OwnerType: cw.Result.OwnerType,
			OwnerId:   cw.Result.OwnerID,
			Finalize:  cw.Result.Finalize,
		}
		if cw.Result.Data != nil {
			if s, err := structpb.NewStruct(cw.Result.Data); err == nil {
				write.Result.Data = s
			}
		}
		if cw.Result.Failure != nil {
			write.Result.Failure = failureToProto(cw.Result.Failure)
		}
	}

	return write
}

func stageWriteToProto(sw *service.StageWrite) *pb.StageWrite {
	write := &pb.StageWrite{
		Id:         sw.ID,
		RunnerType: sw.RunnerType,
	}

	if sw.State != nil {
		write.State = pb.StageState(*sw.State)
	}

	if sw.ExecutionMode != nil {
		write.ExecutionMode = pb.ExecutionMode(*sw.ExecutionMode)
	}

	if sw.Args != nil {
		if s, err := structpb.NewStruct(sw.Args); err == nil {
			write.Args = s
		}
	}

	for _, a := range sw.Assignments {
		write.Assignments = append(write.Assignments, &pb.Assignment{
			TargetCheckId: a.TargetCheckID,
			GoalState:     pb.CheckState(a.GoalState),
		})
	}

	if sw.Dependencies != nil {
		write.Dependencies = dependencyGroupToProto(sw.Dependencies)
	}

	if sw.CurrentAttempt != nil {
		write.CurrentAttempt = attemptWriteToProto(sw.CurrentAttempt)
	}

	return write
}

func attemptWriteToProto(aw *service.AttemptWrite) *pb.AttemptWrite {
	write := &pb.AttemptWrite{
		ProcessUid: aw.ProcessUID,
	}

	if aw.State != nil {
		write.State = pb.AttemptState(*aw.State)
	}

	if aw.Details != nil {
		if s, err := structpb.NewStruct(aw.Details); err == nil {
			write.Details = s
		}
	}

	if aw.Progress != nil {
		write.Progress = progressEntryToProto(aw.Progress)
	}

	if aw.Failure != nil {
		write.Failure = failureToProto(aw.Failure)
	}

	return write
}

func dependencyGroupToProto(g *domain.DependencyGroup) *pb.DependencyGroup {
	if g == nil {
		return nil
	}

	dg := &pb.DependencyGroup{
		Predicate: predicateToProto(g.Predicate),
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
		Message: f.Message,
	}
}

func progressEntryToProto(p *domain.ProgressEntry) *pb.ProgressEntry {
	if p == nil {
		return nil
	}
	pe := &pb.ProgressEntry{
		Message: p.Message,
	}
	if p.Details != nil {
		if s, err := structpb.NewStruct(p.Details); err == nil {
			pe.Details = s
		}
	}
	return pe
}

func predicateToProto(p domain.PredicateType) pb.PredicateType {
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
