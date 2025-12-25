// Package common provides shared utilities for CI runners.
//
// This file provides fluent builders for proto types, mirroring the pkg/ci API
// but producing protobuf types for use with gRPC clients.
package common

import (
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

// CheckBuilder provides a fluent API for constructing pb.CheckWrite objects.
type CheckBuilder struct {
	write *pb.CheckWrite
}

// NewCheck creates a new CheckBuilder with the given ID.
func NewCheck(id string) *CheckBuilder {
	return &CheckBuilder{
		write: &pb.CheckWrite{
			Id: id,
		},
	}
}

// Kind sets the check kind (e.g., "build", "test", "deploy").
func (b *CheckBuilder) Kind(kind string) *CheckBuilder {
	b.write.Kind = kind
	return b
}

// State sets the check state.
func (b *CheckBuilder) State(state pb.CheckState) *CheckBuilder {
	b.write.State = state
	return b
}

// Planning sets the state to PLANNING.
func (b *CheckBuilder) Planning() *CheckBuilder {
	return b.State(pb.CheckState_CHECK_STATE_PLANNING)
}

// Options sets all check options from a map.
func (b *CheckBuilder) Options(opts map[string]any) *CheckBuilder {
	if s, err := structpb.NewStruct(opts); err == nil {
		b.write.Options = s
	}
	return b
}

// DependsOn sets AND dependencies on the given check IDs.
func (b *CheckBuilder) DependsOn(checkIDs ...string) *CheckBuilder {
	if len(checkIDs) == 0 {
		return b
	}
	deps := make([]*pb.Dependency, len(checkIDs))
	for i, id := range checkIDs {
		deps[i] = &pb.Dependency{
			TargetType: pb.NodeType_NODE_TYPE_CHECK,
			TargetId:   id,
		}
	}
	b.write.Dependencies = &pb.DependencyGroup{
		Predicate:    pb.PredicateType_PREDICATE_TYPE_AND,
		Dependencies: deps,
	}
	return b
}

// Dependencies sets the full dependency group.
func (b *CheckBuilder) Dependencies(deps *pb.DependencyGroup) *CheckBuilder {
	b.write.Dependencies = deps
	return b
}

// Build returns the constructed CheckWrite.
func (b *CheckBuilder) Build() *pb.CheckWrite {
	return b.write
}

// StageBuilder provides a fluent API for constructing pb.StageWrite objects.
type StageBuilder struct {
	write *pb.StageWrite
}

// NewStage creates a new StageBuilder with the given ID.
func NewStage(id string) *StageBuilder {
	return &StageBuilder{
		write: &pb.StageWrite{
			Id:    id,
			State: pb.StageState_STAGE_STATE_PLANNED,
		},
	}
}

// RunnerType sets the runner type that handles this stage.
func (b *StageBuilder) RunnerType(rt string) *StageBuilder {
	b.write.RunnerType = rt
	return b
}

// Sync sets the execution mode to synchronous.
func (b *StageBuilder) Sync() *StageBuilder {
	b.write.ExecutionMode = pb.ExecutionMode_EXECUTION_MODE_SYNC
	return b
}

// Async sets the execution mode to asynchronous.
func (b *StageBuilder) Async() *StageBuilder {
	b.write.ExecutionMode = pb.ExecutionMode_EXECUTION_MODE_ASYNC
	return b
}

// Args sets all stage arguments from a map.
func (b *StageBuilder) Args(args map[string]any) *StageBuilder {
	if s, err := structpb.NewStruct(args); err == nil {
		b.write.Args = s
	}
	return b
}

// Assigns adds an assignment to a check with FINAL goal state.
func (b *StageBuilder) Assigns(checkID string) *StageBuilder {
	b.write.Assignments = append(b.write.Assignments, &pb.Assignment{
		TargetCheckId: checkID,
		GoalState:     pb.CheckState_CHECK_STATE_FINAL,
	})
	return b
}

// DependsOnStages sets AND dependencies on the given stage IDs.
func (b *StageBuilder) DependsOnStages(stageIDs ...string) *StageBuilder {
	if len(stageIDs) == 0 {
		return b
	}
	deps := make([]*pb.Dependency, len(stageIDs))
	for i, id := range stageIDs {
		deps[i] = &pb.Dependency{
			TargetType: pb.NodeType_NODE_TYPE_STAGE,
			TargetId:   id,
		}
	}
	b.write.Dependencies = &pb.DependencyGroup{
		Predicate:    pb.PredicateType_PREDICATE_TYPE_AND,
		Dependencies: deps,
	}
	return b
}

// DependsOnChecks sets AND dependencies on the given check IDs.
func (b *StageBuilder) DependsOnChecks(checkIDs ...string) *StageBuilder {
	if len(checkIDs) == 0 {
		return b
	}
	deps := make([]*pb.Dependency, len(checkIDs))
	for i, id := range checkIDs {
		deps[i] = &pb.Dependency{
			TargetType: pb.NodeType_NODE_TYPE_CHECK,
			TargetId:   id,
		}
	}
	if b.write.Dependencies == nil {
		b.write.Dependencies = &pb.DependencyGroup{Predicate: pb.PredicateType_PREDICATE_TYPE_AND}
	}
	b.write.Dependencies.Dependencies = append(b.write.Dependencies.Dependencies, deps...)
	return b
}

// Dependencies sets the full dependency group.
func (b *StageBuilder) Dependencies(deps *pb.DependencyGroup) *StageBuilder {
	b.write.Dependencies = deps
	return b
}

// Build returns the constructed StageWrite.
func (b *StageBuilder) Build() *pb.StageWrite {
	return b.write
}

// DepsAND creates an AND dependency group from check dependencies.
func DepsAND(checkIDs ...string) *pb.DependencyGroup {
	if len(checkIDs) == 0 {
		return nil
	}
	deps := make([]*pb.Dependency, len(checkIDs))
	for i, id := range checkIDs {
		deps[i] = &pb.Dependency{
			TargetType: pb.NodeType_NODE_TYPE_CHECK,
			TargetId:   id,
		}
	}
	return &pb.DependencyGroup{
		Predicate:    pb.PredicateType_PREDICATE_TYPE_AND,
		Dependencies: deps,
	}
}

// StageDepsAND creates an AND dependency group from stage dependencies.
func StageDepsAND(stageIDs ...string) *pb.DependencyGroup {
	if len(stageIDs) == 0 {
		return nil
	}
	deps := make([]*pb.Dependency, len(stageIDs))
	for i, id := range stageIDs {
		deps[i] = &pb.Dependency{
			TargetType: pb.NodeType_NODE_TYPE_STAGE,
			TargetId:   id,
		}
	}
	return &pb.DependencyGroup{
		Predicate:    pb.PredicateType_PREDICATE_TYPE_AND,
		Dependencies: deps,
	}
}

// ResponseBuilder provides a fluent API for constructing pb.RunResponse objects.
type ResponseBuilder struct {
	resp *pb.RunResponse
}

// NewResponse creates a new ResponseBuilder.
func NewResponse() *ResponseBuilder {
	return &ResponseBuilder{
		resp: &pb.RunResponse{
			StageState: pb.StageState_STAGE_STATE_FINAL,
		},
	}
}

// StageState sets the stage state.
func (b *ResponseBuilder) StageState(state pb.StageState) *ResponseBuilder {
	b.resp.StageState = state
	return b
}

// CheckUpdate adds a check update to the response.
func (b *ResponseBuilder) CheckUpdate(checkID string, state pb.CheckState, data map[string]any, finalize bool) *ResponseBuilder {
	update := &pb.CheckUpdate{
		CheckId:  checkID,
		State:    state,
		Finalize: finalize,
	}
	if data != nil {
		if s, err := structpb.NewStruct(data); err == nil {
			update.ResultData = s
		}
	}
	b.resp.CheckUpdates = append(b.resp.CheckUpdates, update)
	return b
}

// FinalizeCheck adds a finalized check update (FINAL state).
func (b *ResponseBuilder) FinalizeCheck(checkID string, data map[string]any) *ResponseBuilder {
	return b.CheckUpdate(checkID, pb.CheckState_CHECK_STATE_FINAL, data, true)
}

// FailCheck adds a failed check update.
func (b *ResponseBuilder) FailCheck(checkID string, message string, data map[string]any) *ResponseBuilder {
	update := &pb.CheckUpdate{
		CheckId:  checkID,
		State:    pb.CheckState_CHECK_STATE_FINAL,
		Finalize: true,
		Failure:  &pb.Failure{Message: message},
	}
	if data != nil {
		if s, err := structpb.NewStruct(data); err == nil {
			update.ResultData = s
		}
	}
	b.resp.CheckUpdates = append(b.resp.CheckUpdates, update)
	return b
}

// Failure sets the overall failure for the response.
func (b *ResponseBuilder) Failure(message string) *ResponseBuilder {
	b.resp.Failure = &pb.Failure{Message: message}
	return b
}

// Build returns the constructed RunResponse.
func (b *ResponseBuilder) Build() *pb.RunResponse {
	return b.resp
}
