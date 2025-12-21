package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "culprit-finder",
	Short: "Find culprit commits using flake-aware non-adaptive group testing",
	Long: `culprit-finder is a tool for identifying which commit(s) introduced a test failure.

It uses non-adaptive group testing with d-disjunct matrices to efficiently search
through commit ranges, with built-in robustness to flaky tests through repetitions
and probabilistic decoding.

Similar to "git bisect", but:
  - Can find multiple simultaneous culprits
  - Handles flaky tests automatically
  - Uses fewer test runs (logarithmic in commit count)
  - Runs tests in parallel for speed

WORKFLOW:
  1. culprit-finder start <good-ref> <bad-ref> -- <test-command>
  2. culprit-finder run
  3. culprit-finder status  (check progress)
  4. Review results when complete
  5. culprit-finder reset   (cleanup)

EXAMPLES:
  # Find culprit between v1.0 and HEAD
  culprit-finder start v1.0 HEAD -- make test

  # With custom configuration for flaky tests
  culprit-finder start v1.0 HEAD --repetitions 5 --flake-rate 0.15 -- npm test

  # Continue a paused search
  culprit-finder run

  # Check current progress
  culprit-finder status

  # Reset and start over
  culprit-finder reset`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(resetCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(listCmd)
}
