package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/example/turboci-lite/culprit/domain"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// PrintHeader prints a section header
func PrintHeader(title string) {
	line := strings.Repeat("=", len(title)+4)
	fmt.Printf("\n%s%s%s\n", colorBold+colorBlue, line, colorReset)
	fmt.Printf("%s  %s  %s\n", colorBold+colorBlue, title, colorReset)
	fmt.Printf("%s%s%s\n\n", colorBold+colorBlue, line, colorReset)
}

// PrintStep prints a step in progress
func PrintStep(message string) {
	fmt.Printf("%s▶%s %s\n", colorCyan, colorReset, message)
}

// PrintSuccess prints a success message
func PrintSuccess(message string) {
	fmt.Printf("%s✓%s %s\n", colorGreen, colorReset, message)
}

// PrintError prints an error message
func PrintError(message string) {
	fmt.Printf("%s✗%s %s\n", colorRed, colorReset, message)
}

// PrintWarning prints a warning message
func PrintWarning(message string) {
	fmt.Printf("%s⚠%s %s\n", colorYellow, colorReset, message)
}

// PrintInfo prints an informational message
func PrintInfo(message string) {
	fmt.Printf("  %s\n", message)
}

// PrintQuestion prints a question prompt
func PrintQuestion(message string) {
	fmt.Printf("%s?%s %s\n", colorPurple, colorReset, message)
}

// PrintProgress prints a progress bar
func PrintProgress(current, total int, prefix string) {
	if total == 0 {
		return
	}

	percentage := float64(current) / float64(total) * 100
	barWidth := 40
	filled := int(float64(barWidth) * float64(current) / float64(total))

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	fmt.Printf("\r%s [%s%s%s] %d/%d (%.1f%%)",
		prefix, colorGreen, bar, colorReset, current, total, percentage)

	if current >= total {
		fmt.Println()
	}
}

// PrintSpinner shows a simple spinner animation
func PrintSpinner(message string, done <-chan bool) {
	spinChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			fmt.Printf("\r%s\n", strings.Repeat(" ", len(message)+5))
			return
		case <-ticker.C:
			fmt.Printf("\r%s%s%s %s", colorCyan, spinChars[i], colorReset, message)
			i = (i + 1) % len(spinChars)
		}
	}
}

// PrintCommit prints a formatted commit
func PrintCommit(c domain.Commit, highlight bool) {
	sha := c.SHA[:8]
	if highlight {
		fmt.Printf("  %s%s%s %s %s(%s)%s\n",
			colorYellow, sha, colorReset,
			c.Subject,
			colorGray, c.Author, colorReset)
	} else {
		fmt.Printf("  %s %s %s(%s)%s\n",
			sha, c.Subject,
			colorGray, c.Author, colorReset)
	}
}

// PrintCulprit prints a culprit with formatting
func PrintCulprit(c domain.CulpritCandidate, rank int) {
	confidenceColor := colorGreen
	if c.Confidence < 0.8 {
		confidenceColor = colorYellow
	}
	if c.Confidence < 0.6 {
		confidenceColor = colorRed
	}

	fmt.Printf("\n%s%d. CULPRIT%s\n", colorBold+colorRed, rank, colorReset)
	fmt.Printf("  Commit:     %s%s%s\n", colorYellow, c.Commit.SHA[:12], colorReset)
	fmt.Printf("  Subject:    %s\n", c.Commit.Subject)
	fmt.Printf("  Author:     %s\n", c.Commit.Author)
	fmt.Printf("  Confidence: %s%.1f%%%s\n", confidenceColor, c.Confidence*100, colorReset)
	fmt.Printf("  Score:      %.2f\n", c.Score)
}

// PrintSummary prints a session summary
func PrintSummary(totalTests, completedTests int, elapsed time.Duration, status string) {
	fmt.Printf("\n%sStatus:%s %s\n", colorBold, colorReset, status)
	if totalTests > 0 {
		PrintProgress(completedTests, totalTests, "Progress:")
	}
	if elapsed > 0 {
		fmt.Printf("Elapsed:  %s\n", formatDuration(elapsed))
	}
	if completedTests > 0 && completedTests < totalTests {
		remaining := estimateRemaining(completedTests, totalTests, elapsed)
		fmt.Printf("Est. time remaining: %s\n", formatDuration(remaining))
	}
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
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

// estimateRemaining estimates remaining time
func estimateRemaining(completed, total int, elapsed time.Duration) time.Duration {
	if completed == 0 {
		return 0
	}
	avgPerTest := elapsed / time.Duration(completed)
	remaining := total - completed
	return avgPerTest * time.Duration(remaining)
}

// PrintTable prints a simple table
func PrintTable(headers []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header
	for i, h := range headers {
		fmt.Printf("%s%-*s%s  ", colorBold, widths[i], h, colorReset)
	}
	fmt.Println()

	// Print separator
	for _, w := range widths {
		fmt.Print(strings.Repeat("-", w) + "  ")
	}
	fmt.Println()

	// Print rows
	for _, row := range rows {
		for i, cell := range row {
			fmt.Printf("%-*s  ", widths[i], cell)
		}
		fmt.Println()
	}
}

// Confirm prompts for yes/no confirmation
func Confirm(message string) bool {
	fmt.Printf("%s%s (y/n): %s", colorPurple, message, colorReset)
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// FormatDuration formats a duration for display (exported version)
func FormatDuration(d time.Duration) string {
	return formatDuration(d)
}
