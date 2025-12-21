package cli

import (
	"fmt"

	"github.com/example/turboci-lite/cmd/culprit-finder/internal/session"
	"github.com/example/turboci-lite/cmd/culprit-finder/internal/ui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show current configuration",
	Long: `Display the configuration of the active session.

Shows all parameters that control the culprit finding algorithm.

EXAMPLES:
  # Show configuration
  culprit-finder config`,
	RunE: runConfig,
}

func runConfig(cmd *cobra.Command, args []string) error {
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

	ui.PrintHeader("Configuration")

	ui.PrintInfo("Search Parameters:")
	ui.PrintInfo(fmt.Sprintf("  Range: %s..%s", sess.GoodRef, sess.BadRef))
	ui.PrintInfo(fmt.Sprintf("  Commits: %d", len(sess.Commits)))
	ui.PrintInfo(fmt.Sprintf("  Test command: %s", sess.TestCommand))
	ui.PrintInfo(fmt.Sprintf("  Test timeout: %s", sess.TestTimeout))
	ui.PrintInfo("")

	ui.PrintInfo("Algorithm Configuration:")
	ui.PrintInfo(fmt.Sprintf("  Max culprits (d): %d", sess.Config.MaxCulprits))
	ui.PrintInfo(fmt.Sprintf("  Repetitions (r): %d", sess.Config.Repetitions))
	ui.PrintInfo(fmt.Sprintf("  Confidence threshold: %.2f", sess.Config.ConfidenceThreshold))
	ui.PrintInfo(fmt.Sprintf("  False positive rate: %.2f", sess.Config.FalsePositiveRate))
	ui.PrintInfo(fmt.Sprintf("  False negative rate: %.2f", sess.Config.FalseNegativeRate))
	if sess.Config.RandomSeed != 0 {
		ui.PrintInfo(fmt.Sprintf("  Random seed: %d", sess.Config.RandomSeed))
	}
	ui.PrintInfo("")

	ui.PrintInfo("Estimated Tests:")
	ui.PrintInfo(fmt.Sprintf("  Total: ~%d", sess.TotalTests))
	ui.PrintInfo(fmt.Sprintf("  Formula: 4 × d² × log₂(n) × r"))
	ui.PrintInfo(fmt.Sprintf("           = 4 × %d² × log₂(%d) × %d",
		sess.Config.MaxCulprits, len(sess.Commits), sess.Config.Repetitions))
	ui.PrintInfo("")

	ui.PrintInfo("Impact of Parameters:")
	ui.PrintInfo("  • max-culprits: Higher = can find more culprits, but requires more tests")
	ui.PrintInfo("  • repetitions: Higher = more robust to flakes, but takes longer")
	ui.PrintInfo("  • confidence: Higher = fewer false positives, but might miss some culprits")
	ui.PrintInfo("  • flake-rate: Should match your test's actual flakiness")

	return nil
}
