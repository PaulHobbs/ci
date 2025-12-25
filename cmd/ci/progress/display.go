package progress

import (
	"fmt"
	"sort"
	"strings"
)

// RenderTreeDAG renders a tree-style DAG with box-drawing characters
func RenderTreeDAG(graph *ProgressGraph) string {
	if graph == nil || len(graph.NodesByID) == 0 {
		return "(no active nodes)"
	}

	// Filter to non-final nodes only
	filtered := graph.FilterNonFinal()
	if len(filtered.NodesByID) == 0 {
		return "(all nodes complete)"
	}

	// Find root nodes (nodes with no dependencies in filtered graph)
	roots := findRoots(filtered)
	if len(roots) == 0 {
		// No roots - just show all nodes
		for id := range filtered.NodesByID {
			roots = append(roots, id)
		}
	}
	sort.Strings(roots)

	// Build parent->children map for rendering
	children := make(map[string][]string)
	for parentID, childList := range filtered.Dependencies {
		// Only include if both parent and children are in filtered graph
		if _, exists := filtered.NodesByID[parentID]; !exists {
			continue
		}
		var validChildren []string
		for _, childID := range childList {
			if _, exists := filtered.NodesByID[childID]; exists {
				validChildren = append(validChildren, childID)
			}
		}
		if len(validChildren) > 0 {
			sort.Strings(validChildren)
			children[parentID] = validChildren
		}
	}

	var buf strings.Builder

	// Render each root
	for i, rootID := range roots {
		isLast := (i == len(roots)-1)
		renderNode(rootID, &buf, filtered, children, "", isLast, 0)
	}

	return buf.String()
}

// renderNode recursively renders a node and its children
func renderNode(nodeID string, buf *strings.Builder, graph *ProgressGraph,
	children map[string][]string, prefix string, isLast bool, depth int) {

	node, exists := graph.NodesByID[nodeID]
	if !exists {
		return
	}

	// Draw connection character (skip for root level)
	if depth > 0 {
		if isLast {
			buf.WriteString(prefix + "└── ")
		} else {
			buf.WriteString(prefix + "├── ")
		}
	}

	// Draw node status symbol and name
	symbol := StatusSymbol(node.Status)
	line := fmt.Sprintf("%s %s", symbol, node.DisplayName)

	// Add state for context
	if node.State != "" {
		// Clean up state name
		state := strings.TrimPrefix(node.State, "STAGE_STATE_")
		state = strings.TrimPrefix(state, "CHECK_STATE_")
		state = titleCase(strings.ToLower(state))
		line += fmt.Sprintf(" (%s)", state)
	}

	// Add attempt info if running
	if node.Status == NodeStatusRunning && node.AttemptIndex > 0 {
		line += fmt.Sprintf(" [attempt %d]", node.AttemptIndex)
	}

	// Add error if failed
	if node.Error != "" {
		errMsg := truncate(node.Error, 50)
		line += fmt.Sprintf(" ✗ %s", errMsg)
	}

	buf.WriteString(line)
	buf.WriteString("\n")

	// Render children
	childList := children[nodeID]
	for i, childID := range childList {
		isLastChild := (i == len(childList)-1)

		// Update prefix for children
		var newPrefix string
		if depth > 0 {
			if isLast {
				newPrefix = prefix + "    "
			} else {
				newPrefix = prefix + "│   "
			}
		} else {
			// For root level nodes
			if isLast {
				newPrefix = "    "
			} else {
				newPrefix = "│   "
			}
		}

		renderNode(childID, buf, graph, children, newPrefix, isLastChild, depth+1)
	}
}

// findRoots finds nodes with no dependencies
func findRoots(graph *ProgressGraph) []string {
	var roots []string
	for nodeID := range graph.NodesByID {
		if len(graph.ReverseDeps[nodeID]) == 0 {
			roots = append(roots, nodeID)
		}
	}
	return roots
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// RenderStateSummary renders a one-line state summary
func RenderStateSummary(graph *ProgressGraph) string {
	if graph == nil || len(graph.NodesByID) == 0 {
		return ""
	}

	counts := make(map[NodeStatus]int)
	for _, node := range graph.NodesByID {
		counts[node.Status]++
	}

	total := len(graph.NodesByID)
	complete := counts[NodeStatusComplete]
	running := counts[NodeStatusRunning]
	pending := counts[NodeStatusPending]
	failed := counts[NodeStatusFailed]

	return fmt.Sprintf("%d/%d complete | %d running | %d pending | %d failed",
		complete, total, running, pending, failed)
}
