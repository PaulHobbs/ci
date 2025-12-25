package progress

import (
	"strings"
)

// ExtractDisplayName converts a node ID to a human-readable display name
func ExtractDisplayName(id string, kind string, runnerType string) string {
	parts := strings.Split(id, ":")
	if len(parts) == 0 {
		return id
	}

	nodeType := parts[0]

	switch nodeType {
	case "stage":
		return formatStageName(parts, runnerType)
	case "check":
		return formatCheckName(parts, kind)
	default:
		return id
	}
}

// formatStageName formats stage IDs like "stage:build:ci" -> "Build: ci"
func formatStageName(parts []string, runnerType string) string {
	if len(parts) < 2 {
		return strings.Join(parts, ":")
	}

	// Extract the stage type (e.g., "trigger", "build", "test")
	stageType := parts[1]

	// Clean up runner type names
	stageType = cleanRunnerType(stageType)

	// Title case the type
	name := titleCase(stageType)

	// If there are more parts, add them as details
	if len(parts) > 2 {
		details := strings.Join(parts[2:], "/")
		// Clean up the details
		details = strings.ReplaceAll(details, "_", " ")
		return name + ": " + details
	}

	return name
}

// formatCheckName formats check IDs like "check:build:pkg:./internal" -> "Build Pkg: ./internal"
func formatCheckName(parts []string, kind string) string {
	if len(parts) < 2 {
		return strings.Join(parts, ":")
	}

	// Use kind if available, otherwise use parts
	if kind != "" && !strings.HasPrefix(kind, "check:") {
		name := titleCase(kind)
		if len(parts) > 2 {
			details := strings.Join(parts[2:], "/")
			return name + ": " + details
		}
		return name
	}

	// Extract the check type
	checkType := parts[1]
	name := titleCase(checkType)

	// If there are more parts, add them as details
	if len(parts) > 2 {
		details := strings.Join(parts[2:], "/")
		return name + ": " + details
	}

	return name
}

// cleanRunnerType cleans up runner type names
func cleanRunnerType(runnerType string) string {
	// Map common runner types to cleaner names
	switch runnerType {
	case "trigger_builds":
		return "trigger"
	case "go_builder":
		return "build"
	case "go_tester":
		return "test"
	case "result_collector":
		return "collector"
	case "conditional_tester":
		return "conditional"
	default:
		return runnerType
	}
}

// titleCase converts snake_case or kebab-case to Title Case
func titleCase(s string) string {
	// Replace underscores and hyphens with spaces
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")

	// Title case each word
	words := strings.Fields(s)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}

	return strings.Join(words, " ")
}

// StatusSymbol returns a symbol representing the node status
func StatusSymbol(status NodeStatus) string {
	switch status {
	case NodeStatusRunning:
		return "⟳"
	case NodeStatusPending:
		return "○"
	case NodeStatusComplete:
		return "✓"
	case NodeStatusFailed:
		return "✗"
	default:
		return "?"
	}
}
