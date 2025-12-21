package cli

import (
	"fmt"

	"github.com/example/turboci-lite/cmd/culprit-finder/internal/session"
	"github.com/example/turboci-lite/cmd/culprit-finder/internal/ui"
	"github.com/spf13/cobra"
)

var (
	listLimit int
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List commits in the search range",
	Long: `Display the list of commits that will be searched for culprits.

Shows commit SHA, subject, and author for each commit in the range.

EXAMPLES:
  # List all commits
  culprit-finder list

  # List first 10 commits
  culprit-finder list --limit 10`,
	RunE: runList,
}

func init() {
	listCmd.Flags().IntVarP(&listLimit, "limit", "n", 0, "limit number of commits shown (0 = all)")
}

func runList(cmd *cobra.Command, args []string) error {
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

	ui.PrintHeader(fmt.Sprintf("Commits in Range (%s..%s)", sess.GoodRef, sess.BadRef))

	ui.PrintInfo(fmt.Sprintf("Total commits: %d", len(sess.Commits)))
	ui.PrintInfo("")

	// Determine limit
	limit := len(sess.Commits)
	if listLimit > 0 && listLimit < limit {
		limit = listLimit
	}

	// Print commits
	for i := 0; i < limit; i++ {
		commit := sess.Commits[i]

		// Check if this is a known culprit
		isCulprit := false
		if sess.Result != nil {
			for _, c := range sess.Result.IdentifiedCulprits {
				if c.Commit.SHA == commit.SHA {
					isCulprit = true
					break
				}
			}
		}

		ui.PrintCommit(commit, isCulprit)
	}

	if limit < len(sess.Commits) {
		ui.PrintInfo("")
		ui.PrintInfo(fmt.Sprintf("... and %d more commits (use --limit 0 to show all)", len(sess.Commits)-limit))
	}

	return nil
}
