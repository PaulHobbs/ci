package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/turboci-lite/cmd/culprit-finder/internal/session"
	"github.com/example/turboci-lite/cmd/culprit-finder/internal/ui"
	"github.com/example/turboci-lite/culprit/domain"
	"github.com/example/turboci-lite/culprit/runner"
	"github.com/example/turboci-lite/internal/storage/sqlite"
	"github.com/example/turboci-lite/pkg/id"
	"github.com/spf13/cobra"
)

var (
	continueOnError bool
	showProgress    bool
	parallelism     int
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the active culprit search",
	Long: `Run the active culprit search session.

This command executes the culprit finding algorithm using the configuration
from 'culprit-finder start'. It will create git worktrees, run tests in parallel,
and decode the results to identify culprits.

The search can be interrupted with Ctrl+C and resumed later using the same command.

EXAMPLES:
  # Run the search
  culprit-finder run

  # Run with progress updates
  culprit-finder run --progress

  # Continue even if some tests fail to run
  culprit-finder run --continue-on-error

PROGRESS:
  The search runs tests in parallel. You can check progress with:
    culprit-finder status

  Progress is saved continuously, so you can stop and resume at any time.`,
	RunE: runRun,
}

func init() {
	runCmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "continue even if some tests fail to execute")
	runCmd.Flags().BoolVar(&showProgress, "progress", true, "show progress updates")
	runCmd.Flags().IntVar(&parallelism, "parallelism", 0, "number of parallel tests (0 = auto)")
}

func runRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Handle Ctrl+C gracefully
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		ui.PrintWarning("\nInterrupted! Cleaning up...")
		ui.PrintInfo("Progress has been saved. Run 'culprit-finder run' again to resume.")
		cancel()
	}()

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

	// Check if already completed
	if sess.Status == "completed" {
		ui.PrintWarning("Session already completed!")
		return runStatus(cmd, args)
	}

	ui.PrintHeader("Running Culprit Finder")
	ui.PrintInfo(fmt.Sprintf("Session: %s", sess.ID))
	ui.PrintInfo(fmt.Sprintf("Commits: %d (%s..%s)", len(sess.Commits), sess.GoodRef, sess.BadRef))
	ui.PrintInfo(fmt.Sprintf("Test command: %s", sess.TestCommand))
	ui.PrintInfo("")

	// Setup storage
	ui.PrintStep("Initializing database")
	storage, err := sqlite.New(sess.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		return fmt.Errorf("failed to migrate storage: %w", err)
	}
	ui.PrintSuccess("Database ready")

	// Setup materializer
	ui.PrintStep("Configuring git worktree manager")
	materializer := runner.NewGitMaterializer(repoPath)
	if sess.WorktreeDir != "" {
		materializer.WithWorktreeBaseDir(sess.WorktreeDir)
	}
	// Ensure cleanup happens
	defer func() {
		if err := materializer.CleanupAll(ctx); err != nil {
			ui.PrintWarning(fmt.Sprintf("Warning: failed to cleanup worktrees: %v", err))
		}
	}()
	ui.PrintSuccess("Git worktree manager ready")

	// Setup test runner
	ui.PrintStep("Configuring test runner")
	testRunner := runner.NewCommandTestRunner()
	ui.PrintSuccess("Test runner ready")

	// Create culprit finder
	finder := runner.NewRunner(storage, materializer, testRunner, id.Generate)

	// Start or resume search
	ui.PrintStep("Initializing search")

	var searchSession *domain.SearchSession

	// Check if we have a work plan ID (resuming)
	if sess.WorkPlanID != "" {
		ui.PrintInfo("Resuming previous session...")
		searchSession, err = finder.GetSession(ctx, sess.WorkPlanID)
		if err != nil {
			// Work plan might not exist in memory, start fresh
			ui.PrintInfo("Previous session not found in memory, starting new search...")
			searchSession, err = startNewSearch(ctx, finder, sess)
			if err != nil {
				return err
			}
		}
	} else {
		searchSession, err = startNewSearch(ctx, finder, sess)
		if err != nil {
			return err
		}
	}

	// Update session
	sess.WorkPlanID = searchSession.ID
	sess.Status = "running"
	if err := sess.Save(repoPath); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	ui.PrintSuccess("Search initialized")
	ui.PrintInfo("")
	ui.PrintHeader("Executing Tests")

	// Run the search with progress tracking
	startTime := time.Now()

	// Run in background with progress updates
	done := make(chan error, 1)
	go func() {
		done <- finder.RunLoop(ctx, searchSession.ID)
	}()

	// Show progress
	if showProgress {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case err := <-done:
				ticker.Stop()
				if err != nil {
					if ctx.Err() != nil {
						// Interrupted
						return nil
					}
					sess.Status = "failed"
					sess.Save(repoPath)
					return fmt.Errorf("search failed: %w", err)
				}
				// Search complete
				goto searchComplete

			case <-ticker.C:
				// Update progress
				result, _ := finder.GetResults(ctx, searchSession.ID)
				if result != nil {
					completed := len(result.GroupOutcomes)
					ui.PrintProgress(completed, sess.TotalTests, "Tests:")
					sess.CompletedTests = completed
					sess.Save(repoPath)
				}
			}
		}
	} else {
		// Just wait
		err := <-done
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			sess.Status = "failed"
			sess.Save(repoPath)
			return fmt.Errorf("search failed: %w", err)
		}
	}

searchComplete:
	elapsed := time.Since(startTime)

	// Get final results
	ui.PrintInfo("")
	ui.PrintStep("Decoding results")
	result, err := finder.GetResults(ctx, searchSession.ID)
	if err != nil {
		return fmt.Errorf("failed to get results: %w", err)
	}

	// Update session with results
	sess.Result = result
	sess.Status = "completed"
	sess.CompletedTests = sess.TotalTests
	if err := sess.Save(repoPath); err != nil {
		ui.PrintWarning(fmt.Sprintf("Warning: failed to save results: %v", err))
	}

	// Display results
	ui.PrintSuccess("Search complete!")
	ui.PrintInfo("")
	ui.PrintHeader("Results")

	ui.PrintInfo(fmt.Sprintf("Time elapsed: %s", ui.FormatDuration(elapsed)))
	ui.PrintInfo(fmt.Sprintf("Tests run: %d", sess.CompletedTests))
	ui.PrintInfo(fmt.Sprintf("Overall confidence: %.1f%%", result.Confidence*100))
	ui.PrintInfo("")

	if len(result.IdentifiedCulprits) > 0 {
		ui.PrintSuccess(fmt.Sprintf("Found %d culprit(s):", len(result.IdentifiedCulprits)))
		ui.PrintInfo("")

		for i, culprit := range result.IdentifiedCulprits {
			ui.PrintCulprit(culprit, i+1)
		}

		ui.PrintInfo("")
		ui.PrintInfo("Review these commits for the introduced failure.")
	} else {
		ui.PrintWarning("No culprits identified!")
		ui.PrintInfo("")
		ui.PrintInfo("Possible reasons:")
		ui.PrintInfo("  - The test might be flaky (try increasing --repetitions)")
		ui.PrintInfo("  - The failure might not be in this commit range")
		ui.PrintInfo("  - The test command might not be working correctly")
	}

	if len(result.InnocentCommits) > 0 {
		ui.PrintInfo("")
		ui.PrintSuccess(fmt.Sprintf("%d commits cleared (not culprits)", len(result.InnocentCommits)))
	}

	if len(result.AmbiguousCommits) > 0 {
		ui.PrintInfo("")
		ui.PrintWarning(fmt.Sprintf("%d commits ambiguous (could not determine)", len(result.AmbiguousCommits)))
		if result.Confidence < 0.7 {
			ui.PrintInfo("Consider re-running with more repetitions for better accuracy")
		}
	}

	ui.PrintInfo("")
	ui.PrintInfo("Use 'culprit-finder reset' to clean up and start a new search")

	return nil
}

func startNewSearch(ctx context.Context, finder *runner.CulpritSearchRunner, sess *session.Session) (*domain.SearchSession, error) {
	req := &runner.SearchRequest{
		CommitRange: domain.CommitRange{
			Repository: sess.Repository,
			BaseRef:    sess.GoodRef,
			Commits:    sess.Commits,
		},
		TestCommand: sess.TestCommand,
		TestTimeout: sess.TestTimeout,
		Config:      sess.Config,
	}

	searchSession, err := finder.StartSearch(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to start search: %w", err)
	}

	return searchSession, nil
}
