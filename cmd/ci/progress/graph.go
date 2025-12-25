package progress

import (
	"context"
	"fmt"
	"sort"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

// NodeStatus represents the computed display-friendly status of a node
type NodeStatus int

const (
	NodeStatusUnknown NodeStatus = iota
	NodeStatusPending
	NodeStatusRunning
	NodeStatusComplete
	NodeStatusFailed
)

// GraphNode represents a single node (stage or check) in the DAG
type GraphNode struct {
	ID           string
	Type         string // "stage" or "check"
	DisplayName  string
	State        string     // raw protobuf state string
	Status       NodeStatus // computed display status
	Kind         string     // check kind or stage runner type
	RunnerType   string     // for stages
	Error        string     // error message if failed
	AttemptIndex int        // current attempt index (0 if no attempts)
}

// ProgressGraph represents the visible DAG for rendering
type ProgressGraph struct {
	NodesByID    map[string]*GraphNode
	Dependencies map[string][]string // forward deps: nodeID -> children that depend on it
	ReverseDeps  map[string][]string // reverse deps: nodeID -> parents it depends on
	Levels       [][]string          // topological levels for layout
}

// BuildGraph queries the orchestrator and builds a progress graph
func BuildGraph(ctx context.Context, orchestrator pb.TurboCIOrchestratorClient, workPlanID string) (*ProgressGraph, error) {
	// Query all stages and checks
	resp, err := orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    workPlanID,
		IncludeStages: true,
		IncludeChecks: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query nodes: %w", err)
	}

	graph := &ProgressGraph{
		NodesByID:    make(map[string]*GraphNode),
		Dependencies: make(map[string][]string),
		ReverseDeps:  make(map[string][]string),
	}

	// Convert stages to graph nodes
	for _, stage := range resp.Stages {
		node := &GraphNode{
			ID:         stage.Id,
			Type:       "stage",
			State:      stage.State.String(),
			RunnerType: stage.RunnerType,
			Kind:       stage.RunnerType,
			Status:     computeStageStatus(stage),
		}

		// Extract error from latest attempt
		if len(stage.Attempts) > 0 {
			lastAttempt := stage.Attempts[len(stage.Attempts)-1]
			node.AttemptIndex = int(lastAttempt.Idx)
			if lastAttempt.Failure != nil {
				node.Error = lastAttempt.Failure.Message
			}
		}

		node.DisplayName = ExtractDisplayName(node.ID, node.Kind, node.RunnerType)
		graph.NodesByID[stage.Id] = node

		// Extract dependencies
		if stage.Dependencies != nil {
			for _, dep := range stage.Dependencies.Dependencies {
				targetID := dep.TargetId
				graph.Dependencies[targetID] = append(graph.Dependencies[targetID], stage.Id)
				graph.ReverseDeps[stage.Id] = append(graph.ReverseDeps[stage.Id], targetID)
			}
		}
	}

	// Convert checks to graph nodes
	for _, check := range resp.Checks {
		node := &GraphNode{
			ID:     check.Id,
			Type:   "check",
			State:  check.State.String(),
			Kind:   check.Id, // use ID as kind for now
			Status: computeCheckStatus(check),
		}

		// Extract kind from options if available
		if check.Options != nil {
			if kindField := check.Options.Fields["kind"]; kindField != nil {
				if kindVal := kindField.GetStringValue(); kindVal != "" {
					node.Kind = kindVal
				}
			}
		}

		// Check for failures in results
		if len(check.Results) > 0 {
			lastResult := check.Results[len(check.Results)-1]
			if lastResult.Failure != nil {
				node.Error = lastResult.Failure.Message
			}
		}

		node.DisplayName = ExtractDisplayName(node.ID, node.Kind, "")
		graph.NodesByID[check.Id] = node

		// Extract dependencies
		if check.Dependencies != nil {
			for _, dep := range check.Dependencies.Dependencies {
				targetID := dep.TargetId
				graph.Dependencies[targetID] = append(graph.Dependencies[targetID], check.Id)
				graph.ReverseDeps[check.Id] = append(graph.ReverseDeps[check.Id], targetID)
			}
		}
	}

	// Compute topological levels
	graph.Levels = computeTopologicalLevels(graph)

	return graph, nil
}

// computeStageStatus maps stage state to display status
func computeStageStatus(stage *pb.Stage) NodeStatus {
	switch stage.State {
	case pb.StageState_STAGE_STATE_ATTEMPTING:
		// Check if any attempt is running
		for _, attempt := range stage.Attempts {
			if attempt.State == pb.AttemptState_ATTEMPT_STATE_RUNNING ||
				attempt.State == pb.AttemptState_ATTEMPT_STATE_SCHEDULED {
				return NodeStatusRunning
			}
		}
		return NodeStatusRunning // ATTEMPTING means something is happening

	case pb.StageState_STAGE_STATE_PLANNED, pb.StageState_STAGE_STATE_AWAITING_GROUP:
		return NodeStatusPending

	case pb.StageState_STAGE_STATE_FINAL:
		// Check if any attempt failed
		for _, attempt := range stage.Attempts {
			if attempt.State == pb.AttemptState_ATTEMPT_STATE_INCOMPLETE ||
				attempt.Failure != nil {
				return NodeStatusFailed
			}
		}
		return NodeStatusComplete

	default:
		return NodeStatusUnknown
	}
}

// computeCheckStatus maps check state to display status
func computeCheckStatus(check *pb.Check) NodeStatus {
	switch check.State {
	case pb.CheckState_CHECK_STATE_WAITING:
		return NodeStatusRunning

	case pb.CheckState_CHECK_STATE_PLANNING, pb.CheckState_CHECK_STATE_PLANNED:
		return NodeStatusPending

	case pb.CheckState_CHECK_STATE_FINAL:
		// Check if any result has a failure
		for _, result := range check.Results {
			if result.Failure != nil {
				return NodeStatusFailed
			}
		}
		return NodeStatusComplete

	default:
		return NodeStatusUnknown
	}
}

// computeTopologicalLevels computes levels for DAG layout using BFS
func computeTopologicalLevels(graph *ProgressGraph) [][]string {
	if len(graph.NodesByID) == 0 {
		return nil
	}

	// Find root nodes (no dependencies)
	roots := []string{}
	for nodeID := range graph.NodesByID {
		if len(graph.ReverseDeps[nodeID]) == 0 {
			roots = append(roots, nodeID)
		}
	}

	if len(roots) == 0 {
		// No roots found - might be a cycle or all nodes have deps
		// Just pick the first node as root
		for nodeID := range graph.NodesByID {
			roots = append(roots, nodeID)
			break
		}
	}

	// Sort roots for consistent ordering
	sort.Strings(roots)

	// BFS to assign levels
	levels := [][]string{roots}
	visited := make(map[string]bool)
	for _, root := range roots {
		visited[root] = true
	}

	for level := 0; level < len(levels); level++ {
		var nextLevel []string
		for _, nodeID := range levels[level] {
			// Add all children to next level
			for _, childID := range graph.Dependencies[nodeID] {
				if !visited[childID] {
					// Check if all parents are visited
					allParentsVisited := true
					for _, parentID := range graph.ReverseDeps[childID] {
						if !visited[parentID] {
							allParentsVisited = false
							break
						}
					}

					if allParentsVisited {
						visited[childID] = true
						nextLevel = append(nextLevel, childID)
					}
				}
			}
		}

		if len(nextLevel) > 0 {
			sort.Strings(nextLevel)
			levels = append(levels, nextLevel)
		}
	}

	// Add any unvisited nodes (orphans or cycles) to final level
	var orphans []string
	for nodeID := range graph.NodesByID {
		if !visited[nodeID] {
			orphans = append(orphans, nodeID)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		levels = append(levels, orphans)
	}

	return levels
}

// FilterNonFinal returns a new graph with only non-final nodes
func (g *ProgressGraph) FilterNonFinal() *ProgressGraph {
	filtered := &ProgressGraph{
		NodesByID:    make(map[string]*GraphNode),
		Dependencies: make(map[string][]string),
		ReverseDeps:  make(map[string][]string),
	}

	// Copy non-final nodes
	for id, node := range g.NodesByID {
		if node.State != "STAGE_STATE_FINAL" && node.State != "CHECK_STATE_FINAL" {
			filtered.NodesByID[id] = node
		}
	}

	// Copy dependencies for non-final nodes
	for parentID, children := range g.Dependencies {
		if _, exists := filtered.NodesByID[parentID]; !exists {
			continue
		}
		for _, childID := range children {
			if _, exists := filtered.NodesByID[childID]; exists {
				filtered.Dependencies[parentID] = append(filtered.Dependencies[parentID], childID)
			}
		}
	}

	// Copy reverse dependencies for non-final nodes
	for childID, parents := range g.ReverseDeps {
		if _, exists := filtered.NodesByID[childID]; !exists {
			continue
		}
		for _, parentID := range parents {
			if _, exists := filtered.NodesByID[parentID]; exists {
				filtered.ReverseDeps[childID] = append(filtered.ReverseDeps[childID], parentID)
			}
		}
	}

	// Recompute levels
	filtered.Levels = computeTopologicalLevels(filtered)

	return filtered
}
