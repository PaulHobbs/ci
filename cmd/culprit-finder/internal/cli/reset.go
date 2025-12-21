package cli

import (
	"fmt"
	"os"

	"github.com/example/turboci-lite/cmd/culprit-finder/internal/session"
	"github.com/example/turboci-lite/cmd/culprit-finder/internal/ui"
	"github.com/spf13/cobra"
)

var (
	force bool
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset and clean up the current session",
	Long: `Reset the current culprit finder session and clean up all data.

This command removes the session state, database, and any temporary files.
Use this to start a fresh search or clean up after completion.

WARNING: This cannot be undone! All progress will be lost.

EXAMPLES:
  # Reset with confirmation prompt
  culprit-finder reset

  # Force reset without confirmation
  culprit-finder reset --force`,
	RunE: runReset,
}

func init() {
	resetCmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
}

func runReset(cmd *cobra.Command, args []string) error {
	// Find repository root
	repoPath, err := findGitRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Check if session exists
	if !session.Exists(repoPath) {
		ui.PrintInfo("No active session found")
		return nil
	}

	// Load session for info
	sess, err := session.Load(repoPath)
	if err != nil {
		ui.PrintWarning("Warning: could not load session details")
	} else {
		ui.PrintInfo(fmt.Sprintf("Session: %s", sess.ID))
		ui.PrintInfo(fmt.Sprintf("Status: %s", sess.Status))
		ui.PrintInfo(fmt.Sprintf("Commits: %d", len(sess.Commits)))
		ui.PrintInfo("")
	}

	// Confirm unless forced
	if !force {
		if !ui.Confirm("Are you sure you want to reset? All progress will be lost.") {
			ui.PrintInfo("Reset cancelled")
			return nil
		}
	}

	ui.PrintStep("Removing session data")

	// Remove session directory (includes database and session file)
	sessionDir := session.GetSessionDir(repoPath)
	if err := os.RemoveAll(sessionDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove session directory: %w", err)
		}
	}

	ui.PrintSuccess("Session reset complete")
	ui.PrintInfo("")
	ui.PrintInfo("You can now start a new search with 'culprit-finder start'")

	return nil
}
