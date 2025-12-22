package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/example/turboci-lite/cmd/culprit-finder/internal/session"
	"github.com/example/turboci-lite/cmd/culprit-finder/internal/ui"
	"github.com/example/turboci-lite/culprit/domain"
	"github.com/spf13/cobra"
)

var (
	maxCulprits int
	repetitions int
	confidence  float64
	flakeRate   float64
	testTimeout time.Duration
	worktreeDir string
	interactive bool
	randomSeed  int64
)

var startCmd = &cobra.Command{
	Use:   "start <good-ref> <bad-ref> -- <test-command>",
	Short: "Start a new culprit search",
	Long: `Start a new culprit search between two git references.

The good-ref should point to a commit where tests pass, and bad-ref to where they fail.
The tool will search all commits between them to find which introduced the failure.

EXAMPLES:
  # Basic usage
  culprit-finder start v1.0 HEAD -- make test

  # With custom test timeout
  culprit-finder start --timeout 15m origin/main HEAD -- npm test

  # For flaky tests, increase repetitions
  culprit-finder start --repetitions 5 --flake-rate 0.15 main HEAD -- go test ./...

  # Expect multiple culprits
  culprit-finder start --max-culprits 3 v2.0 HEAD -- pytest

  # Interactive mode (prompts for configuration)
  culprit-finder start -i main HEAD -- cargo test

CONFIGURATION:
  --max-culprits: Maximum expected culprits (default: 2)
                  Higher values require more tests but can find more culprits

  --repetitions:  Number of times to repeat each test (default: 3)
                  Higher values improve flake robustness

  --confidence:   Minimum confidence to report a culprit (default: 0.8)
                  Range: 0.0-1.0

  --flake-rate:   Expected flake rate (default: 0.05)
                  Set higher if your tests are flakey

  --timeout:      Test command timeout (default: 10m)

NOTE: The test command should exit with code 0 on pass, non-zero on fail.`,
	Args: cobra.MinimumNArgs(2),
	RunE: runStart,
}

func init() {
	startCmd.Flags().IntVar(&maxCulprits, "max-culprits", 2, "maximum expected number of culprits")
	startCmd.Flags().IntVar(&repetitions, "repetitions", 3, "number of times to repeat each test")
	startCmd.Flags().Float64Var(&confidence, "confidence", 0.8, "minimum confidence threshold (0-1)")
	startCmd.Flags().Float64Var(&flakeRate, "flake-rate", 0.05, "expected test flake rate (0-1)")
	startCmd.Flags().DurationVar(&testTimeout, "timeout", 10*time.Minute, "test command timeout")
	startCmd.Flags().StringVar(&worktreeDir, "worktree-dir", "", "directory for git worktrees (default: temp dir)")
	startCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "interactive mode with prompts")
	startCmd.Flags().Int64Var(&randomSeed, "seed", 0, "random seed for reproducibility (0 = random)")
}

func runStart(cmd *cobra.Command, args []string) error {
	// Find repository root
	repoPath, err := findGitRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Check if session already exists
	if session.Exists(repoPath) {
		return fmt.Errorf("a culprit finder session is already active\n" +
			"Use 'culprit-finder status' to check it, or 'culprit-finder reset' to start over")
	}

	// Parse arguments
	goodRef := args[0]
	badRef := args[1]

	// Find the test command (everything after --)
	testCommand := ""
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			testCommand = strings.Join(os.Args[i+1:], " ")
			break
		}
	}

	if testCommand == "" {
		return fmt.Errorf("test command required after '--'\nExample: culprit-finder start main HEAD -- make test")
	}

	// Interactive mode
	if interactive {
		if err := runInteractive(&maxCulprits, &repetitions, &confidence, &flakeRate, &testTimeout); err != nil {
			return err
		}
	}

	ui.PrintHeader("Initializing Culprit Finder")

	// Validate refs exist
	ui.PrintStep("Validating git references")
	if err := validateRef(repoPath, goodRef); err != nil {
		return fmt.Errorf("invalid good-ref '%s': %w", goodRef, err)
	}
	if err := validateRef(repoPath, badRef); err != nil {
		return fmt.Errorf("invalid bad-ref '%s': %w", badRef, err)
	}
	ui.PrintSuccess(fmt.Sprintf("Refs validated: %s..%s", goodRef, badRef))

	// Get commit range
	ui.PrintStep("Fetching commit range")
	commits, err := getCommits(repoPath, goodRef, badRef)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	if len(commits) < 2 {
		return fmt.Errorf("need at least 2 commits in range, found %d\n"+
			"Make sure good-ref != bad-ref", len(commits))
	}

	ui.PrintSuccess(fmt.Sprintf("Found %d commits to search", len(commits)))

	// Verify test command works
	ui.PrintStep("Verifying test command")
	ui.PrintInfo(fmt.Sprintf("Test: %s", testCommand))
	ui.PrintInfo(fmt.Sprintf("Timeout: %s", testTimeout))

	// Create configuration
	config := domain.CulpritFinderConfig{
		MaxCulprits:         maxCulprits,
		Repetitions:         repetitions,
		ConfidenceThreshold: confidence,
		FalsePositiveRate:   flakeRate,
		FalseNegativeRate:   flakeRate,
		RandomSeed:          randomSeed,
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create session
	sess := session.New(repoPath, goodRef, badRef, testCommand, testTimeout, config, commits)
	if worktreeDir != "" {
		sess.WorktreeDir = worktreeDir
	}

	if err := sess.Save(repoPath); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	// Print summary
	ui.PrintHeader("Session Created")
	ui.PrintInfo(fmt.Sprintf("Session ID: %s", sess.ID))
	ui.PrintInfo(fmt.Sprintf("Commits: %d", len(commits)))
	ui.PrintInfo(fmt.Sprintf("Configuration:"))
	ui.PrintInfo(fmt.Sprintf("  Max culprits: %d", config.MaxCulprits))
	ui.PrintInfo(fmt.Sprintf("  Repetitions: %d", config.Repetitions))
	ui.PrintInfo(fmt.Sprintf("  Confidence threshold: %.2f", config.ConfidenceThreshold))
	ui.PrintInfo(fmt.Sprintf("  Flake rate: %.2f", config.FalsePositiveRate))

	estimatedTests := sess.TotalTests
	ui.PrintInfo(fmt.Sprintf("Estimated total tests: ~%d", estimatedTests))
	ui.PrintInfo("")
	ui.PrintSuccess("Session initialized successfully!")
	ui.PrintInfo("")
	ui.PrintInfo("Next steps:")
	ui.PrintInfo("  1. Run 'culprit-finder run' to start the search")
	ui.PrintInfo("  2. Use 'culprit-finder status' to check progress")
	ui.PrintInfo("  3. Results will be shown when complete")

	return nil
}

// findGitRoot finds the root of the git repository
func findGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// validateRef checks if a git ref exists
func validateRef(repoPath, ref string) error {
	cmd := exec.Command("git", "rev-parse", "--verify", ref)
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ref does not exist")
	}
	return nil
}

// getCommits retrieves the list of commits between goodRef and badRef
func getCommits(repoPath, goodRef, badRef string) ([]domain.Commit, error) {
	// Get commit list: goodRef..badRef (exclusive of goodRef)
	cmd := exec.Command("git", "log", "--pretty=format:%H|%s|%an", fmt.Sprintf("%s..%s", goodRef, badRef), "--reverse")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}

	commits := make([]domain.Commit, 0, len(lines))
	for i, line := range lines {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		commits = append(commits, domain.Commit{
			SHA:     parts[0],
			Index:   i,
			Subject: parts[1],
			Author:  parts[2],
		})
	}

	return commits, nil
}

// runInteractive prompts the user for configuration
func runInteractive(maxCulprits, repetitions *int, confidence, flakeRate *float64, timeout *time.Duration) error {
	ui.PrintHeader("Interactive Configuration")

	ui.PrintInfo("This wizard will help you configure the culprit finder.")
	ui.PrintInfo("")

	// Ask about test flakiness
	ui.PrintQuestion("How flaky are your tests?")
	ui.PrintInfo("  1. Very stable (< 1% flake rate)")
	ui.PrintInfo("  2. Mostly stable (~ 5% flake rate) [default]")
	ui.PrintInfo("  3. Somewhat flaky (~ 15% flake rate)")
	ui.PrintInfo("  4. Very flaky (> 25% flake rate)")

	var flakeChoice int
	fmt.Print("Choice [1-4]: ")
	fmt.Scanln(&flakeChoice)

	switch flakeChoice {
	case 1:
		*flakeRate = 0.01
		*repetitions = 1
	case 3:
		*flakeRate = 0.15
		*repetitions = 5
	case 4:
		*flakeRate = 0.25
		*repetitions = 7
	default:
		*flakeRate = 0.05
		*repetitions = 3
	}

	ui.PrintInfo("")

	// Ask about expected culprits
	ui.PrintQuestion("How many culprits do you expect?")
	ui.PrintInfo("  1. Single culprit (faster)")
	ui.PrintInfo("  2. Up to 2 culprits [default]")
	ui.PrintInfo("  3. Up to 3 culprits (slower)")

	var culpritChoice int
	fmt.Print("Choice [1-3]: ")
	fmt.Scanln(&culpritChoice)

	switch culpritChoice {
	case 1:
		*maxCulprits = 1
	case 3:
		*maxCulprits = 3
	default:
		*maxCulprits = 2
	}

	ui.PrintInfo("")
	ui.PrintSuccess(fmt.Sprintf("Configuration: %d reps, %.0f%% flake rate, max %d culprits",
		*repetitions, *flakeRate*100, *maxCulprits))

	return nil
}
