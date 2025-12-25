package progress

import (
	"context"
	"fmt"
	"strings"
	"time"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

// ResultsSummary contains final CI execution results
type ResultsSummary struct {
	OverallStatus string
	Duration      time.Duration
	StartTime     time.Time
	EndTime       time.Time

	TotalStages      int
	CompletedStages  int
	FailedStages     int
	IncompleteStages int

	TotalChecks      int
	CompletedChecks  int
	FailedChecks     int
	PendingChecks    int

	FailedNodes []*FailedNodeDetails
}

// FailedNodeDetails contains details about a failed node
type FailedNodeDetails struct {
	ID          string
	DisplayName string
	Type        string
	Error       string
	Duration    time.Duration
	StartTime   time.Time
	AttemptNum  int
}

// BuildResultsSummary builds a results summary from the final graph state
func BuildResultsSummary(ctx context.Context, orchestrator pb.TurboCIOrchestratorClient, workPlanID string, startTime time.Time) (*ResultsSummary, error) {
	graph, err := BuildGraph(ctx, orchestrator, workPlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	summary := &ResultsSummary{
		StartTime: startTime,
		EndTime:   time.Now(),
	}
	summary.Duration = summary.EndTime.Sub(summary.StartTime)

	// Count nodes by status
	stageCounts := make(map[NodeStatus]int)
	checkCounts := make(map[NodeStatus]int)

	for _, node := range graph.NodesByID {
		if node.Type == "stage" {
			summary.TotalStages++
			stageCounts[node.Status]++

			if node.Status == NodeStatusFailed {
				summary.FailedNodes = append(summary.FailedNodes, &FailedNodeDetails{
					ID:          node.ID,
					DisplayName: node.DisplayName,
					Type:        "stage",
					Error:       node.Error,
					AttemptNum:  node.AttemptIndex,
				})
			}
		} else if node.Type == "check" {
			summary.TotalChecks++
			checkCounts[node.Status]++

			if node.Status == NodeStatusFailed {
				summary.FailedNodes = append(summary.FailedNodes, &FailedNodeDetails{
					ID:          node.ID,
					DisplayName: node.DisplayName,
					Type:        "check",
					Error:       node.Error,
				})
			}
		}
	}

	summary.CompletedStages = stageCounts[NodeStatusComplete]
	summary.FailedStages = stageCounts[NodeStatusFailed]
	summary.IncompleteStages = stageCounts[NodeStatusPending] + stageCounts[NodeStatusRunning]

	summary.CompletedChecks = checkCounts[NodeStatusComplete]
	summary.FailedChecks = checkCounts[NodeStatusFailed]
	summary.PendingChecks = checkCounts[NodeStatusPending] + checkCounts[NodeStatusRunning]

	// Determine overall status
	if summary.FailedStages > 0 || summary.FailedChecks > 0 {
		summary.OverallStatus = "FAILED"
	} else if summary.IncompleteStages > 0 || summary.PendingChecks > 0 {
		summary.OverallStatus = "INCOMPLETE"
	} else {
		summary.OverallStatus = "PASSED"
	}

	return summary, nil
}

// RenderResultsSummary renders a detailed results summary
func RenderResultsSummary(summary *ResultsSummary) string {
	var buf strings.Builder

	// Header
	buf.WriteString("\n")
	buf.WriteString("═══════════════════════════════════════════════════════════\n")
	buf.WriteString("                    CI RESULTS SUMMARY\n")
	buf.WriteString("═══════════════════════════════════════════════════════════\n")
	buf.WriteString("\n")

	// Overall status
	statusSymbol := "✓"
	if summary.OverallStatus == "FAILED" {
		statusSymbol = "✗"
	} else if summary.OverallStatus == "INCOMPLETE" {
		statusSymbol = "○"
	}

	buf.WriteString(fmt.Sprintf("Overall Status: %s %s\n", statusSymbol, summary.OverallStatus))
	buf.WriteString(fmt.Sprintf("Duration: %s\n", formatDuration(summary.Duration)))
	buf.WriteString("\n")

	// Stage summary
	buf.WriteString("Stages:\n")
	buf.WriteString(fmt.Sprintf("  Total:      %d\n", summary.TotalStages))
	buf.WriteString(fmt.Sprintf("  Completed:  %d\n", summary.CompletedStages))
	if summary.FailedStages > 0 {
		buf.WriteString(fmt.Sprintf("  Failed:     %d\n", summary.FailedStages))
	}
	if summary.IncompleteStages > 0 {
		buf.WriteString(fmt.Sprintf("  Incomplete: %d\n", summary.IncompleteStages))
	}
	buf.WriteString("\n")

	// Check summary
	buf.WriteString("Checks:\n")
	buf.WriteString(fmt.Sprintf("  Total:      %d\n", summary.TotalChecks))
	buf.WriteString(fmt.Sprintf("  Completed:  %d\n", summary.CompletedChecks))
	if summary.FailedChecks > 0 {
		buf.WriteString(fmt.Sprintf("  Failed:     %d\n", summary.FailedChecks))
	}
	if summary.PendingChecks > 0 {
		buf.WriteString(fmt.Sprintf("  Pending:    %d\n", summary.PendingChecks))
	}
	buf.WriteString("\n")

	// Failures section
	if len(summary.FailedNodes) > 0 {
		buf.WriteString("═══════════════════════════════════════════════════════════\n")
		buf.WriteString("                    FAILURES & ERRORS\n")
		buf.WriteString("═══════════════════════════════════════════════════════════\n")
		buf.WriteString("\n")

		for _, failed := range summary.FailedNodes {
			buf.WriteString(fmt.Sprintf("%s: %s\n", strings.Title(failed.Type), failed.DisplayName))
			buf.WriteString(fmt.Sprintf("  ID:     %s\n", failed.ID))
			if failed.AttemptNum > 0 {
				buf.WriteString(fmt.Sprintf("  Attempt: %d\n", failed.AttemptNum))
			}
			if failed.Error != "" {
				buf.WriteString(fmt.Sprintf("  Error:   %s\n", failed.Error))
			}
			buf.WriteString("\n")
		}
	}

	buf.WriteString("═══════════════════════════════════════════════════════════\n")

	return buf.String()
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
