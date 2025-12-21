package cli

import (
	"fmt"
	"time"

	"github.com/example/turboci-lite/cmd/culprit-finder/internal/session"
	"github.com/example/turboci-lite/cmd/culprit-finder/internal/ui"
	"github.com/spf13/cobra"
)

var (
	verbose bool
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the status of the current session",
	Long: `Display the current status of the active culprit search session.

Shows progress, configuration, and results (if complete).

EXAMPLES:
  # Show status
  culprit-finder status

  # Show detailed status with commit list
  culprit-finder status --verbose`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show detailed information")
}

func runStatus(cmd *cobra.Command, args []string) error {
	// Find repository root
	repoPath, err := findGitRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Load session
	sess, err := session.Load(repoPath)
	if err != nil {
		return err
	}

	ui.PrintHeader("Culprit Finder Status")

	// Session info
	ui.PrintInfo(fmt.Sprintf("Session ID: %s", sess.ID))
	ui.PrintInfo(fmt.Sprintf("Created: %s", sess.CreatedAt.Format("2006-01-02 15:04:05")))
	ui.PrintInfo(fmt.Sprintf("Updated: %s", sess.UpdatedAt.Format("2006-01-02 15:04:05")))
	ui.PrintInfo("")

	// Repository info
	ui.PrintInfo(fmt.Sprintf("Repository: %s", sess.Repository))
	ui.PrintInfo(fmt.Sprintf("Range: %s..%s", sess.GoodRef, sess.BadRef))
	ui.PrintInfo(fmt.Sprintf("Commits: %d", len(sess.Commits)))
	ui.PrintInfo(fmt.Sprintf("Test command: %s", sess.TestCommand))
	ui.PrintInfo("")

	// Configuration
	ui.PrintInfo("Configuration:")
	ui.PrintInfo(fmt.Sprintf("  Max culprits: %d", sess.Config.MaxCulprits))
	ui.PrintInfo(fmt.Sprintf("  Repetitions: %d", sess.Config.Repetitions))
	ui.PrintInfo(fmt.Sprintf("  Confidence threshold: %.2f", sess.Config.ConfidenceThreshold))
	ui.PrintInfo(fmt.Sprintf("  Flake rate: %.2f", sess.Config.FalsePositiveRate))
	ui.PrintInfo("")

	// Progress
	elapsed := time.Since(sess.CreatedAt)
	ui.PrintSummary(sess.TotalTests, sess.CompletedTests, elapsed, sess.Status)

	// Results if completed
	if sess.Status == "completed" && sess.Result != nil {
		ui.PrintInfo("")
		ui.PrintHeader("Results")

		if len(sess.Result.IdentifiedCulprits) > 0 {
			ui.PrintSuccess(fmt.Sprintf("Found %d culprit(s):", len(sess.Result.IdentifiedCulprits)))
			ui.PrintInfo("")

			for i, culprit := range sess.Result.IdentifiedCulprits {
				ui.PrintCulprit(culprit, i+1)
			}
		} else {
			ui.PrintWarning("No culprits identified")
		}

		ui.PrintInfo("")
		ui.PrintInfo(fmt.Sprintf("Innocent commits: %d", len(sess.Result.InnocentCommits)))
		ui.PrintInfo(fmt.Sprintf("Ambiguous commits: %d", len(sess.Result.AmbiguousCommits)))
		ui.PrintInfo(fmt.Sprintf("Overall confidence: %.1f%%", sess.Result.Confidence*100))
	}

	// Verbose output
	if verbose && sess.Status != "completed" {
		ui.PrintInfo("")
		ui.PrintHeader("Commit List")
		for i, commit := range sess.Commits {
			if i >= 20 && !verbose {
				ui.PrintInfo(fmt.Sprintf("... and %d more commits", len(sess.Commits)-20))
				break
			}
			ui.PrintCommit(commit, false)
		}
	}

	// Next steps
	ui.PrintInfo("")
	ui.PrintHeader("Next Steps")
	switch sess.Status {
	case "initialized":
		ui.PrintInfo("Run 'culprit-finder run' to start the search")
	case "running":
		ui.PrintInfo("The search is in progress")
		ui.PrintInfo("Run 'culprit-finder run' to resume if interrupted")
	case "completed":
		ui.PrintInfo("Search complete! Review the results above")
		ui.PrintInfo("Run 'culprit-finder reset' to start a new search")
	case "failed":
		ui.PrintError("Search failed!")
		ui.PrintInfo("Check the error messages and try 'culprit-finder run' again")
		ui.PrintInfo("Or use 'culprit-finder reset' to start over")
	}

	return nil
}
