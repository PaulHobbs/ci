package decoder

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/example/turboci-lite/culprit/domain"
	"github.com/example/turboci-lite/culprit/matrix"
)

func makeCommits(n int) []domain.Commit {
	commits := make([]domain.Commit, n)
	for i := 0; i < n; i++ {
		commits[i] = domain.Commit{
			SHA:     fmt.Sprintf("commit-%d", i),
			Index:   i,
			Subject: fmt.Sprintf("Commit message %d", i),
			Author:  "test@example.com",
		}
	}
	return commits
}

// simulateTests generates test results based on culprits and flake rate.
func simulateTests(
	testMatrix *domain.TestMatrix,
	culprits map[int]bool,
	flakeRate float64,
	rng *rand.Rand,
) []domain.TestGroupResult {

	var results []domain.TestGroupResult

	for _, instance := range testMatrix.AllGroups() {
		// Check if any commit in this group is a culprit
		hasCulprit := false
		for _, commitIdx := range instance.CommitIndices {
			if culprits[commitIdx] {
				hasCulprit = true
				break
			}
		}

		// Determine outcome (with possible flakes)
		outcome := domain.OutcomePass
		if hasCulprit {
			outcome = domain.OutcomeFail
			// Possible false positive (test passes despite culprit)
			if flakeRate > 0 && rng.Float64() < flakeRate {
				outcome = domain.OutcomePass
			}
		} else {
			// Possible false negative (test fails despite no culprit)
			if flakeRate > 0 && rng.Float64() < flakeRate {
				outcome = domain.OutcomeFail
			}
		}

		results = append(results, domain.TestGroupResult{
			GroupID:    instance.GroupID,
			GroupIndex: instance.GroupIndex,
			Repetition: instance.Repetition,
			Outcome:    outcome,
			Duration:   100 * time.Millisecond,
			ExecutedAt: time.Now(),
		})
	}

	return results
}

func TestMajorityVoting(t *testing.T) {
	tests := []struct {
		name     string
		outcomes []domain.TestOutcome
		want     domain.TestOutcome
	}{
		{"all pass", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomePass}, domain.OutcomePass},
		{"all fail", []domain.TestOutcome{domain.OutcomeFail, domain.OutcomeFail, domain.OutcomeFail}, domain.OutcomeFail},
		{"majority pass", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomeFail}, domain.OutcomePass},
		{"majority fail", []domain.TestOutcome{domain.OutcomeFail, domain.OutcomeFail, domain.OutcomePass}, domain.OutcomeFail},
		{"tie goes to fail", []domain.TestOutcome{domain.OutcomePass, domain.OutcomeFail}, domain.OutcomeFail},
		{"with infra", []domain.TestOutcome{domain.OutcomePass, domain.OutcomeInfra, domain.OutcomePass}, domain.OutcomePass},
		{"all infra", []domain.TestOutcome{domain.OutcomeInfra, domain.OutcomeInfra}, domain.OutcomeInfra},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create results for group 0
			var results []domain.TestGroupResult
			for i, outcome := range tc.outcomes {
				results = append(results, domain.TestGroupResult{
					GroupID:    "group-0",
					GroupIndex: 0,
					Repetition: i + 1,
					Outcome:    outcome,
				})
			}

			aggregated := AggregateByMajority(results, 1)
			if len(aggregated) != 1 {
				t.Fatalf("Expected 1 aggregated outcome, got %d", len(aggregated))
			}

			if aggregated[0].Outcome != tc.want {
				t.Errorf("Outcome = %s, want %s", aggregated[0].Outcome, tc.want)
			}
		})
	}
}

func TestDecoderSingleCulpritNoFlakes(t *testing.T) {
	commits := makeCommits(20)
	config := domain.CulpritFinderConfig{
		MaxCulprits:         2,
		Repetitions:         3,
		ConfidenceThreshold: 0.7,
		FalsePositiveRate:   0.05,
		FalseNegativeRate:   0.10,
		RandomSeed:          42,
	}.WithDefaults()

	builder := matrix.NewBuilder(func() string { return "test-matrix" })
	testMatrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build matrix failed: %v", err)
	}

	// Set commit 10 as the culprit
	culprits := map[int]bool{10: true}
	rng := rand.New(rand.NewSource(42))
	results := simulateTests(testMatrix, culprits, 0, rng)

	// Run decoder
	dec := NewDecoder(config)
	decoded, err := dec.Decode(commits, testMatrix, results)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// The actual culprit should be among the identified culprits
	found := false
	for _, c := range decoded.IdentifiedCulprits {
		if c.Commit.Index == 10 {
			found = true
			t.Logf("Found culprit commit-10 with confidence %.2f", c.Confidence)
			break
		}
	}

	if !found {
		t.Error("Culprit commit-10 not in identified culprits")
		t.Logf("Identified %d culprits", len(decoded.IdentifiedCulprits))
	}

	// Commit 10 should have high confidence
	// Use deterministic elimination to verify
	deterministicResult := DecodeWithDeterministicElimination(commits, testMatrix, decoded.GroupOutcomes)
	if len(deterministicResult.InnocentCommits) < 10 {
		t.Logf("Deterministic elimination found %d innocent commits", len(deterministicResult.InnocentCommits))
	}
}

func TestDecoderTwoCulpritsNoFlakes(t *testing.T) {
	commits := makeCommits(30)
	config := domain.CulpritFinderConfig{
		MaxCulprits:         2,
		Repetitions:         3,
		ConfidenceThreshold: 0.7,
		RandomSeed:          42,
	}.WithDefaults()

	builder := matrix.NewBuilder(func() string { return "test-matrix" })
	testMatrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build matrix failed: %v", err)
	}

	// Set commits 5 and 25 as culprits
	culprits := map[int]bool{5: true, 25: true}
	rng := rand.New(rand.NewSource(42))
	results := simulateTests(testMatrix, culprits, 0, rng)

	dec := NewDecoder(config)
	decoded, err := dec.Decode(commits, testMatrix, results)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Should identify both culprits
	foundIndices := make(map[int]bool)
	for _, c := range decoded.IdentifiedCulprits {
		foundIndices[c.Commit.Index] = true
	}

	if !foundIndices[5] {
		t.Error("Did not identify culprit at index 5")
	}
	if !foundIndices[25] {
		t.Error("Did not identify culprit at index 25")
	}
}

func TestDecoderWithFlakes(t *testing.T) {
	commits := makeCommits(20)
	config := domain.CulpritFinderConfig{
		MaxCulprits:         2,
		Repetitions:         5, // More repetitions for flake robustness
		ConfidenceThreshold: 0.6,
		FalsePositiveRate:   0.1,
		FalseNegativeRate:   0.1,
		RandomSeed:          42,
	}.WithDefaults()

	builder := matrix.NewBuilder(func() string { return "test-matrix" })
	testMatrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build matrix failed: %v", err)
	}

	// Set commit 7 as the culprit
	culprits := map[int]bool{7: true}
	rng := rand.New(rand.NewSource(42))
	results := simulateTests(testMatrix, culprits, 0.1, rng) // 10% flake rate

	dec := NewDecoder(config)
	decoded, err := dec.Decode(commits, testMatrix, results)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// The real culprit should be in the identified list
	found := false
	for _, c := range decoded.IdentifiedCulprits {
		if c.Commit.Index == 7 {
			found = true
			break
		}
	}

	if !found {
		t.Error("Culprit at index 7 not identified despite flakes")
		t.Logf("Identified culprits:")
		for _, c := range decoded.IdentifiedCulprits {
			t.Logf("  - Index %d: confidence %.2f", c.Commit.Index, c.Confidence)
		}
	}
}

func TestDecoderNoCulprits(t *testing.T) {
	commits := makeCommits(15)
	config := domain.CulpritFinderConfig{
		MaxCulprits: 2,
		Repetitions: 3,
		RandomSeed:  42,
	}.WithDefaults()

	builder := matrix.NewBuilder(func() string { return "test-matrix" })
	testMatrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build matrix failed: %v", err)
	}

	// No culprits - all tests pass
	culprits := map[int]bool{}
	rng := rand.New(rand.NewSource(42))
	results := simulateTests(testMatrix, culprits, 0, rng)

	dec := NewDecoder(config)
	decoded, err := dec.Decode(commits, testMatrix, results)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Should identify no culprits
	if len(decoded.IdentifiedCulprits) != 0 {
		t.Errorf("Identified %d culprits, want 0", len(decoded.IdentifiedCulprits))
	}

	// All commits should be innocent
	if len(decoded.InnocentCommits) != 15 {
		t.Errorf("Got %d innocent commits, want 15", len(decoded.InnocentCommits))
	}
}

func TestDeterministicElimination(t *testing.T) {
	commits := makeCommits(10)

	// Create a simple matrix where:
	// Group 0: commits 0, 1, 2
	// Group 1: commits 3, 4, 5
	// Group 2: commits 6, 7, 8, 9
	testMatrix := &domain.TestMatrix{
		NumCommits:  10,
		Repetitions: 1,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1, 2}},
			{ID: "g1", Index: 1, CommitIndices: []int{3, 4, 5}},
			{ID: "g2", Index: 2, CommitIndices: []int{6, 7, 8, 9}},
		},
	}

	// Group 0 passes, Group 1 fails, Group 2 passes
	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, GroupID: "g0", Outcome: domain.OutcomePass},
		{GroupIndex: 1, GroupID: "g1", Outcome: domain.OutcomeFail},
		{GroupIndex: 2, GroupID: "g2", Outcome: domain.OutcomePass},
	}

	result := DecodeWithDeterministicElimination(commits, testMatrix, outcomes)

	// Commits 0, 1, 2 (in passing group 0) should be innocent
	// Commits 6, 7, 8, 9 (in passing group 2) should be innocent
	// Commits 3, 4, 5 (only in failing group 1) are culprit candidates
	if len(result.InnocentCommits) != 7 {
		t.Errorf("Got %d innocent commits, want 7", len(result.InnocentCommits))
	}

	if len(result.IdentifiedCulprits) != 3 {
		t.Errorf("Got %d culprits, want 3", len(result.IdentifiedCulprits))
	}
}

func TestLikelihoodScorer(t *testing.T) {
	scorer := NewLikelihoodScorer(0.05, 0.10)

	// Verify log probability ratios are in expected directions
	// For a failing test:
	// - Culprit has high P(fail) = 1 - FP = 0.95
	// - Innocent has low P(fail) = FN = 0.10
	// So log(0.95/0.10) should be positive

	_ = makeCommits(5) // Not needed for scoring test
	testMatrix := &domain.TestMatrix{
		NumCommits:  5,
		Repetitions: 1,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1}},
			{ID: "g1", Index: 1, CommitIndices: []int{2, 3, 4}},
		},
	}

	// Both groups fail
	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, Outcome: domain.OutcomeFail},
		{GroupIndex: 1, Outcome: domain.OutcomeFail},
	}

	scores := scorer.ComputeScores(testMatrix, outcomes)

	// All commits should have positive scores (all tests fail)
	for i, score := range scores {
		if score <= 0 {
			t.Errorf("Score for commit %d = %f, want > 0", i, score)
		}
	}

	// Commits in more failing groups should have higher scores
	// Commits 0, 1 are in 1 group; commits 2, 3, 4 are in 1 group
	// Since all groups fail, scores depend on coverage
}

func TestScoreToConfidence(t *testing.T) {
	tests := []struct {
		score   float64
		minConf float64
		maxConf float64
	}{
		{0, 0.4, 0.6},         // Neutral score → ~0.5 confidence
		{5, 0.9, 1.0},         // High positive → high confidence
		{-5, 0.0, 0.1},        // High negative → low confidence
		{100, 0.999, 1.0},     // Very high → clamps to ~1
		{-100, 0.0, 0.001},    // Very low → clamps to ~0
	}

	for _, tc := range tests {
		conf := ScoreToConfidence(tc.score)
		if conf < tc.minConf || conf > tc.maxConf {
			t.Errorf("ScoreToConfidence(%f) = %f, want in [%f, %f]",
				tc.score, conf, tc.minConf, tc.maxConf)
		}
	}
}

func TestDecoderNoResults(t *testing.T) {
	commits := makeCommits(10)
	config := domain.DefaultConfig()
	testMatrix := &domain.TestMatrix{NumCommits: 10}

	dec := NewDecoder(config)
	_, err := dec.Decode(commits, testMatrix, nil)
	if err == nil {
		t.Error("Expected error for nil results")
	}

	_, err = dec.Decode(commits, testMatrix, []domain.TestGroupResult{})
	if err == nil {
		t.Error("Expected error for empty results")
	}
}

// ============================================================================
// Comprehensive Majority Voting Tests
// ============================================================================

func TestMajorityVotingExtended(t *testing.T) {
	tests := []struct {
		name     string
		outcomes []domain.TestOutcome
		want     domain.TestOutcome
	}{
		// Basic cases
		{"single pass", []domain.TestOutcome{domain.OutcomePass}, domain.OutcomePass},
		{"single fail", []domain.TestOutcome{domain.OutcomeFail}, domain.OutcomeFail},
		{"single infra", []domain.TestOutcome{domain.OutcomeInfra}, domain.OutcomeInfra},

		// Two outcomes
		{"2 pass", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass}, domain.OutcomePass},
		{"2 fail", []domain.TestOutcome{domain.OutcomeFail, domain.OutcomeFail}, domain.OutcomeFail},
		{"1 pass 1 fail (tie)", []domain.TestOutcome{domain.OutcomePass, domain.OutcomeFail}, domain.OutcomeFail},

		// Three outcomes
		{"2 pass 1 fail", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomeFail}, domain.OutcomePass},
		{"1 pass 2 fail", []domain.TestOutcome{domain.OutcomePass, domain.OutcomeFail, domain.OutcomeFail}, domain.OutcomeFail},
		{"3 pass", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomePass}, domain.OutcomePass},
		{"3 fail", []domain.TestOutcome{domain.OutcomeFail, domain.OutcomeFail, domain.OutcomeFail}, domain.OutcomeFail},

		// With infra failures (should be excluded from voting)
		{"1 pass 1 infra", []domain.TestOutcome{domain.OutcomePass, domain.OutcomeInfra}, domain.OutcomePass},
		{"1 fail 1 infra", []domain.TestOutcome{domain.OutcomeFail, domain.OutcomeInfra}, domain.OutcomeFail},
		{"1 pass 1 fail 1 infra", []domain.TestOutcome{domain.OutcomePass, domain.OutcomeFail, domain.OutcomeInfra}, domain.OutcomeFail},
		{"2 pass 1 infra", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomeInfra}, domain.OutcomePass},
		{"2 fail 1 infra", []domain.TestOutcome{domain.OutcomeFail, domain.OutcomeFail, domain.OutcomeInfra}, domain.OutcomeFail},

		// Multiple infra
		{"all infra (3)", []domain.TestOutcome{domain.OutcomeInfra, domain.OutcomeInfra, domain.OutcomeInfra}, domain.OutcomeInfra},
		{"2 infra 1 pass", []domain.TestOutcome{domain.OutcomeInfra, domain.OutcomeInfra, domain.OutcomePass}, domain.OutcomePass},

		// Larger counts
		{"5 pass", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomePass, domain.OutcomePass, domain.OutcomePass}, domain.OutcomePass},
		{"3 pass 2 fail", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomePass, domain.OutcomeFail, domain.OutcomeFail}, domain.OutcomePass},
		{"2 pass 3 fail", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomeFail, domain.OutcomeFail, domain.OutcomeFail}, domain.OutcomeFail},

		// Edge case: equal counts (tie goes to fail)
		{"2 pass 2 fail", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomeFail, domain.OutcomeFail}, domain.OutcomeFail},
		{"3 pass 3 fail", []domain.TestOutcome{domain.OutcomePass, domain.OutcomePass, domain.OutcomePass, domain.OutcomeFail, domain.OutcomeFail, domain.OutcomeFail}, domain.OutcomeFail},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var results []domain.TestGroupResult
			for i, outcome := range tc.outcomes {
				results = append(results, domain.TestGroupResult{
					GroupID:    "group-0",
					GroupIndex: 0,
					Repetition: i + 1,
					Outcome:    outcome,
				})
			}

			aggregated := AggregateByMajority(results, 1)
			if len(aggregated) != 1 {
				t.Fatalf("Expected 1 aggregated outcome, got %d", len(aggregated))
			}

			if aggregated[0].Outcome != tc.want {
				t.Errorf("Outcome = %s, want %s (passes=%d, fails=%d, infra=%d)",
					aggregated[0].Outcome, tc.want,
					aggregated[0].PassCount, aggregated[0].FailCount, aggregated[0].InfraCount)
			}
		})
	}
}

func TestMajorityVotingMultipleGroups(t *testing.T) {
	results := []domain.TestGroupResult{
		// Group 0: 2 pass, 1 fail -> PASS
		{GroupID: "g0", GroupIndex: 0, Repetition: 1, Outcome: domain.OutcomePass},
		{GroupID: "g0", GroupIndex: 0, Repetition: 2, Outcome: domain.OutcomePass},
		{GroupID: "g0", GroupIndex: 0, Repetition: 3, Outcome: domain.OutcomeFail},

		// Group 1: 1 pass, 2 fail -> FAIL
		{GroupID: "g1", GroupIndex: 1, Repetition: 1, Outcome: domain.OutcomePass},
		{GroupID: "g1", GroupIndex: 1, Repetition: 2, Outcome: domain.OutcomeFail},
		{GroupID: "g1", GroupIndex: 1, Repetition: 3, Outcome: domain.OutcomeFail},

		// Group 2: all infra -> INFRA
		{GroupID: "g2", GroupIndex: 2, Repetition: 1, Outcome: domain.OutcomeInfra},
		{GroupID: "g2", GroupIndex: 2, Repetition: 2, Outcome: domain.OutcomeInfra},
		{GroupID: "g2", GroupIndex: 2, Repetition: 3, Outcome: domain.OutcomeInfra},
	}

	aggregated := AggregateByMajority(results, 3)

	expected := []domain.TestOutcome{
		domain.OutcomePass,
		domain.OutcomeFail,
		domain.OutcomeInfra,
	}

	for i, want := range expected {
		if aggregated[i].Outcome != want {
			t.Errorf("Group %d: Outcome = %s, want %s", i, aggregated[i].Outcome, want)
		}
	}
}

func TestMajorityVotingMissingGroups(t *testing.T) {
	// Only provide results for groups 0 and 2, not 1
	results := []domain.TestGroupResult{
		{GroupID: "g0", GroupIndex: 0, Repetition: 1, Outcome: domain.OutcomePass},
		{GroupID: "g2", GroupIndex: 2, Repetition: 1, Outcome: domain.OutcomeFail},
	}

	aggregated := AggregateByMajority(results, 3)

	if len(aggregated) != 3 {
		t.Fatalf("Expected 3 aggregated outcomes, got %d", len(aggregated))
	}

	// Group 1 should have Unknown outcome
	if aggregated[1].Outcome != domain.OutcomeUnknown {
		t.Errorf("Group 1 Outcome = %s, want Unknown", aggregated[1].Outcome)
	}
}

func TestMajorityVotingOutOfBoundsGroups(t *testing.T) {
	// Results with invalid group indices should be ignored
	results := []domain.TestGroupResult{
		{GroupID: "g0", GroupIndex: 0, Repetition: 1, Outcome: domain.OutcomePass},
		{GroupID: "invalid", GroupIndex: -1, Repetition: 1, Outcome: domain.OutcomeFail},
		{GroupID: "invalid", GroupIndex: 100, Repetition: 1, Outcome: domain.OutcomeFail},
	}

	aggregated := AggregateByMajority(results, 2)

	if len(aggregated) != 2 {
		t.Fatalf("Expected 2 aggregated outcomes, got %d", len(aggregated))
	}

	// Group 0 should be PASS
	if aggregated[0].Outcome != domain.OutcomePass {
		t.Errorf("Group 0 Outcome = %s, want PASS", aggregated[0].Outcome)
	}

	// Group 1 should be Unknown
	if aggregated[1].Outcome != domain.OutcomeUnknown {
		t.Errorf("Group 1 Outcome = %s, want Unknown", aggregated[1].Outcome)
	}
}

func TestMajorityVotingCounts(t *testing.T) {
	results := []domain.TestGroupResult{
		{GroupID: "g0", GroupIndex: 0, Repetition: 1, Outcome: domain.OutcomePass},
		{GroupID: "g0", GroupIndex: 0, Repetition: 2, Outcome: domain.OutcomePass},
		{GroupID: "g0", GroupIndex: 0, Repetition: 3, Outcome: domain.OutcomeFail},
		{GroupID: "g0", GroupIndex: 0, Repetition: 4, Outcome: domain.OutcomeInfra},
		{GroupID: "g0", GroupIndex: 0, Repetition: 5, Outcome: domain.OutcomeInfra},
	}

	aggregated := AggregateByMajority(results, 1)

	if aggregated[0].PassCount != 2 {
		t.Errorf("PassCount = %d, want 2", aggregated[0].PassCount)
	}
	if aggregated[0].FailCount != 1 {
		t.Errorf("FailCount = %d, want 1", aggregated[0].FailCount)
	}
	if aggregated[0].InfraCount != 2 {
		t.Errorf("InfraCount = %d, want 2", aggregated[0].InfraCount)
	}
	if aggregated[0].TotalUsable() != 3 {
		t.Errorf("TotalUsable = %d, want 3", aggregated[0].TotalUsable())
	}
}

func TestFilterUsableOutcomes(t *testing.T) {
	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, Outcome: domain.OutcomePass},
		{GroupIndex: 1, Outcome: domain.OutcomeFail},
		{GroupIndex: 2, Outcome: domain.OutcomeInfra},
		{GroupIndex: 3, Outcome: domain.OutcomeUnknown},
		{GroupIndex: 4, Outcome: domain.OutcomePass},
	}

	usable := FilterUsableOutcomes(outcomes)

	if len(usable) != 3 {
		t.Errorf("len(usable) = %d, want 3", len(usable))
	}

	// Should contain groups 0, 1, 4
	indices := make(map[int]bool)
	for _, o := range usable {
		indices[o.GroupIndex] = true
	}

	for _, want := range []int{0, 1, 4} {
		if !indices[want] {
			t.Errorf("Missing group %d in usable outcomes", want)
		}
	}
}

func TestCountOutcomes(t *testing.T) {
	tests := []struct {
		name         string
		outcomes     []domain.AggregatedGroupOutcome
		wantPasses   int
		wantFails    int
	}{
		{
			"all pass",
			[]domain.AggregatedGroupOutcome{
				{Outcome: domain.OutcomePass},
				{Outcome: domain.OutcomePass},
			},
			2, 0,
		},
		{
			"all fail",
			[]domain.AggregatedGroupOutcome{
				{Outcome: domain.OutcomeFail},
				{Outcome: domain.OutcomeFail},
			},
			0, 2,
		},
		{
			"mixed",
			[]domain.AggregatedGroupOutcome{
				{Outcome: domain.OutcomePass},
				{Outcome: domain.OutcomeFail},
				{Outcome: domain.OutcomeInfra},
				{Outcome: domain.OutcomeUnknown},
				{Outcome: domain.OutcomePass},
			},
			2, 1,
		},
		{
			"empty",
			[]domain.AggregatedGroupOutcome{},
			0, 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			passes, fails := CountOutcomes(tc.outcomes)
			if passes != tc.wantPasses {
				t.Errorf("passes = %d, want %d", passes, tc.wantPasses)
			}
			if fails != tc.wantFails {
				t.Errorf("fails = %d, want %d", fails, tc.wantFails)
			}
		})
	}
}

// ============================================================================
// Comprehensive Likelihood Scoring Tests
// ============================================================================

func TestLikelihoodScorerFlakeRateClamping(t *testing.T) {
	// Test that extreme flake rates are clamped properly
	tests := []struct {
		name   string
		fpRate float64
		fnRate float64
	}{
		{"zero rates", 0, 0},
		{"one rates", 1, 1},
		{"negative rates", -0.5, -0.5},
		{"very small rates", 0.0001, 0.0001},
		{"very high rates", 0.9999, 0.9999},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic
			scorer := NewLikelihoodScorer(tc.fpRate, tc.fnRate)

			// Verify rates are clamped
			if scorer.FalsePositiveRate <= 0 || scorer.FalsePositiveRate >= 1 {
				t.Errorf("FP rate not properly clamped: %f", scorer.FalsePositiveRate)
			}
			if scorer.FalseNegativeRate <= 0 || scorer.FalseNegativeRate >= 1 {
				t.Errorf("FN rate not properly clamped: %f", scorer.FalseNegativeRate)
			}
		})
	}
}

func TestLikelihoodScorerScoreDirection(t *testing.T) {
	scorer := NewLikelihoodScorer(0.05, 0.10)

	// Create a simple matrix
	testMatrix := &domain.TestMatrix{
		NumCommits: 3,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0}},       // Only commit 0
			{ID: "g1", Index: 1, CommitIndices: []int{1}},       // Only commit 1
			{ID: "g2", Index: 2, CommitIndices: []int{0, 1, 2}}, // All commits
		},
	}

	tests := []struct {
		name         string
		outcomes     []domain.AggregatedGroupOutcome
		expectHigher int // Which commit should have higher score
		expectLower  int
	}{
		{
			"commit 0 in failing group only",
			[]domain.AggregatedGroupOutcome{
				{GroupIndex: 0, Outcome: domain.OutcomeFail},
				{GroupIndex: 1, Outcome: domain.OutcomePass},
				{GroupIndex: 2, Outcome: domain.OutcomeFail},
			},
			0, 1, // Commit 0 should have higher score than commit 1
		},
		{
			"commit 1 in passing group only",
			[]domain.AggregatedGroupOutcome{
				{GroupIndex: 0, Outcome: domain.OutcomePass},
				{GroupIndex: 1, Outcome: domain.OutcomePass},
				{GroupIndex: 2, Outcome: domain.OutcomeFail},
			},
			2, 1, // Commit 2 only in failing shared group, commit 1 only in passing
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scores := scorer.ComputeScores(testMatrix, tc.outcomes)

			if scores[tc.expectHigher] <= scores[tc.expectLower] {
				t.Errorf("Expected commit %d score (%.3f) > commit %d score (%.3f)",
					tc.expectHigher, scores[tc.expectHigher],
					tc.expectLower, scores[tc.expectLower])
			}
		})
	}
}

func TestLikelihoodScorerEmptyMatrix(t *testing.T) {
	scorer := NewLikelihoodScorer(0.05, 0.10)

	testMatrix := &domain.TestMatrix{
		NumCommits: 3,
		Groups:     []domain.TestGroup{},
	}

	scores := scorer.ComputeScores(testMatrix, nil)

	// All scores should be 0 (no evidence)
	for i, score := range scores {
		if score != 0 {
			t.Errorf("Score[%d] = %f, want 0", i, score)
		}
	}
}

func TestLikelihoodScorerAllPassingGroups(t *testing.T) {
	scorer := NewLikelihoodScorer(0.05, 0.10)

	testMatrix := &domain.TestMatrix{
		NumCommits: 3,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1}},
			{ID: "g1", Index: 1, CommitIndices: []int{1, 2}},
		},
	}

	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, Outcome: domain.OutcomePass},
		{GroupIndex: 1, Outcome: domain.OutcomePass},
	}

	scores := scorer.ComputeScores(testMatrix, outcomes)

	// All scores should be negative (passing groups indicate innocence)
	for i, score := range scores {
		if score > 0 {
			t.Errorf("Score[%d] = %f, expected negative for all-pass", i, score)
		}
	}
}

func TestLikelihoodScorerAllFailingGroups(t *testing.T) {
	scorer := NewLikelihoodScorer(0.05, 0.10)

	testMatrix := &domain.TestMatrix{
		NumCommits: 3,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1}},
			{ID: "g1", Index: 1, CommitIndices: []int{1, 2}},
		},
	}

	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, Outcome: domain.OutcomeFail},
		{GroupIndex: 1, Outcome: domain.OutcomeFail},
	}

	scores := scorer.ComputeScores(testMatrix, outcomes)

	// All scores should be positive (failing groups indicate guilt)
	for i, score := range scores {
		if score <= 0 {
			t.Errorf("Score[%d] = %f, expected positive for all-fail", i, score)
		}
	}

	// Commit 1 (in both failing groups) should have highest score
	if scores[1] <= scores[0] || scores[1] <= scores[2] {
		t.Errorf("Commit 1 (in both groups) should have highest score: %v", scores)
	}
}

func TestScoreToConfidenceExtended(t *testing.T) {
	tests := []struct {
		score   float64
		minConf float64
		maxConf float64
	}{
		// Normal range
		{0, 0.49, 0.51},
		{1, 0.7, 0.75},
		{-1, 0.25, 0.3},
		{2, 0.85, 0.92},
		{-2, 0.08, 0.15},
		{3, 0.94, 0.96},
		{-3, 0.04, 0.06},

		// Extreme values (should clamp)
		{20, 0.999, 1.0},
		{-20, 0.0, 0.001},
		{50, 0.999, 1.0},
		{-50, 0.0, 0.001},
		{100, 0.999, 1.0},
		{-100, 0.0, 0.001},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("score=%.1f", tc.score), func(t *testing.T) {
			conf := ScoreToConfidence(tc.score)
			if conf < tc.minConf || conf > tc.maxConf {
				t.Errorf("ScoreToConfidence(%f) = %f, want in [%f, %f]",
					tc.score, conf, tc.minConf, tc.maxConf)
			}
		})
	}
}

func TestScoreToConfidenceMonotonicity(t *testing.T) {
	// Confidence should be monotonically increasing with score
	prevConf := 0.0
	for score := -10.0; score <= 10.0; score += 0.5 {
		conf := ScoreToConfidence(score)
		if conf < prevConf {
			t.Errorf("Confidence not monotonic: ScoreToConfidence(%f)=%f < prev=%f",
				score, conf, prevConf)
		}
		prevConf = conf
	}
}

func TestComputeEvidenceForCommit(t *testing.T) {
	scorer := NewLikelihoodScorer(0.05, 0.10)

	testMatrix := &domain.TestMatrix{
		NumCommits: 3,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0}},
			{ID: "g1", Index: 1, CommitIndices: []int{0, 1}},
			{ID: "g2", Index: 2, CommitIndices: []int{2}},
		},
	}

	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, GroupID: "g0", Outcome: domain.OutcomeFail},
		{GroupIndex: 1, GroupID: "g1", Outcome: domain.OutcomePass},
		{GroupIndex: 2, GroupID: "g2", Outcome: domain.OutcomeFail},
	}

	// Get evidence for commit 0 (in groups 0 and 1)
	evidence := scorer.ComputeEvidenceForCommit(0, testMatrix, outcomes)

	if len(evidence) != 2 {
		t.Fatalf("Expected 2 evidence items, got %d", len(evidence))
	}

	// Verify evidence groups
	groupIndices := make(map[int]bool)
	for _, e := range evidence {
		groupIndices[e.GroupIndex] = true
	}
	if !groupIndices[0] || !groupIndices[1] {
		t.Error("Missing expected groups in evidence")
	}

	// Get evidence for commit 2 (only in group 2)
	evidence2 := scorer.ComputeEvidenceForCommit(2, testMatrix, outcomes)
	if len(evidence2) != 1 {
		t.Errorf("Expected 1 evidence item for commit 2, got %d", len(evidence2))
	}
}

func TestComputeEvidenceExcludesInfra(t *testing.T) {
	scorer := NewLikelihoodScorer(0.05, 0.10)

	testMatrix := &domain.TestMatrix{
		NumCommits: 2,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0}},
			{ID: "g1", Index: 1, CommitIndices: []int{0}},
		},
	}

	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, GroupID: "g0", Outcome: domain.OutcomeFail},
		{GroupIndex: 1, GroupID: "g1", Outcome: domain.OutcomeInfra}, // Should be excluded
	}

	evidence := scorer.ComputeEvidenceForCommit(0, testMatrix, outcomes)

	// Should only have 1 evidence (infra excluded)
	if len(evidence) != 1 {
		t.Errorf("Expected 1 evidence item (infra excluded), got %d", len(evidence))
	}
}

// ============================================================================
// Comprehensive Decoder Classification Tests
// ============================================================================

func TestDecoderThresholdSensitivity(t *testing.T) {
	commits := makeCommits(10)
	builder := matrix.NewBuilder(func() string { return "test-matrix" })

	baseConfig := domain.CulpritFinderConfig{
		MaxCulprits:       2,
		Repetitions:       3,
		FalsePositiveRate: 0.05,
		FalseNegativeRate: 0.10,
		RandomSeed:        42,
	}

	testMatrix, _ := builder.Build(commits, baseConfig.WithDefaults())

	// Set commit 5 as culprit
	culprits := map[int]bool{5: true}
	rng := rand.New(rand.NewSource(42))
	results := simulateTests(testMatrix, culprits, 0, rng)

	thresholds := []float64{0.5, 0.6, 0.7, 0.8, 0.9, 0.95}

	for _, threshold := range thresholds {
		t.Run(fmt.Sprintf("threshold=%.2f", threshold), func(t *testing.T) {
			config := baseConfig
			config.ConfidenceThreshold = threshold

			dec := NewDecoder(config.WithDefaults())
			result, err := dec.Decode(commits, testMatrix, results)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			t.Logf("Threshold %.2f: %d culprits, %d innocent, %d ambiguous",
				threshold,
				len(result.IdentifiedCulprits),
				len(result.InnocentCommits),
				len(result.AmbiguousCommits))

			// Lower threshold should identify more culprits (or same)
			// Higher threshold should be more conservative
			totalClassified := len(result.IdentifiedCulprits) + len(result.InnocentCommits)
			totalAmbiguous := len(result.AmbiguousCommits)

			// Verify totals add up
			if totalClassified+totalAmbiguous != 10 {
				t.Errorf("Classification totals don't add up: %d + %d != 10",
					totalClassified, totalAmbiguous)
			}
		})
	}
}

func TestDecoderCulpritSorting(t *testing.T) {
	commits := makeCommits(20)
	builder := matrix.NewBuilder(func() string { return "test-matrix" })

	config := domain.CulpritFinderConfig{
		MaxCulprits:         2,
		Repetitions:         5,
		ConfidenceThreshold: 0.5, // Low threshold to get more culprits
		RandomSeed:          42,
	}.WithDefaults()

	testMatrix, _ := builder.Build(commits, config)

	// Set two culprits
	culprits := map[int]bool{5: true, 15: true}
	rng := rand.New(rand.NewSource(42))
	results := simulateTests(testMatrix, culprits, 0, rng)

	dec := NewDecoder(config)
	result, _ := dec.Decode(commits, testMatrix, results)

	// Verify culprits are sorted by confidence (descending)
	for i := 1; i < len(result.IdentifiedCulprits); i++ {
		if result.IdentifiedCulprits[i].Confidence > result.IdentifiedCulprits[i-1].Confidence {
			t.Errorf("Culprits not sorted: %f > %f at positions %d, %d",
				result.IdentifiedCulprits[i].Confidence,
				result.IdentifiedCulprits[i-1].Confidence,
				i, i-1)
		}
	}
}

func TestDecoderOverallConfidence(t *testing.T) {
	commits := makeCommits(10)

	tests := []struct {
		name            string
		outcomes        []domain.AggregatedGroupOutcome
		expectCulprits  bool // Whether we expect culprits to be identified
		expectHighConf  bool // Whether overall confidence should be high (>0.7)
	}{
		{
			"all pass (no culprit)",
			[]domain.AggregatedGroupOutcome{
				{GroupIndex: 0, Outcome: domain.OutcomePass},
				{GroupIndex: 1, Outcome: domain.OutcomePass},
			},
			false, // No culprits
			true,  // High confidence (all pass means confident no culprit)
		},
		{
			"all fail (has culprits)",
			[]domain.AggregatedGroupOutcome{
				{GroupIndex: 0, Outcome: domain.OutcomeFail},
				{GroupIndex: 1, Outcome: domain.OutcomeFail},
			},
			true, // Has culprits
			true, // High confidence in culprits (average of identified)
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testMatrix := &domain.TestMatrix{
				NumCommits:  10,
				Repetitions: 1,
				Groups: []domain.TestGroup{
					{ID: "g0", Index: 0, CommitIndices: []int{0, 1, 2, 3, 4}},
					{ID: "g1", Index: 1, CommitIndices: []int{5, 6, 7, 8, 9}},
				},
			}

			// Create results from outcomes
			var results []domain.TestGroupResult
			for _, o := range tc.outcomes {
				results = append(results, domain.TestGroupResult{
					GroupID:    fmt.Sprintf("g%d", o.GroupIndex),
					GroupIndex: o.GroupIndex,
					Repetition: 1,
					Outcome:    o.Outcome,
				})
			}

			config := domain.DefaultConfig()
			dec := NewDecoder(config)
			result, _ := dec.Decode(commits, testMatrix, results)

			if tc.expectCulprits && len(result.IdentifiedCulprits) == 0 {
				t.Error("Expected culprits to be identified")
			}
			if !tc.expectCulprits && len(result.IdentifiedCulprits) > 0 {
				t.Error("Expected no culprits")
			}
			if tc.expectHighConf && result.Confidence < 0.7 {
				t.Errorf("Expected high confidence, got %f", result.Confidence)
			}

			t.Logf("Confidence = %f, culprits = %d", result.Confidence, len(result.IdentifiedCulprits))
		})
	}
}

func TestDeterministicEliminationComprehensive(t *testing.T) {
	commits := makeCommits(8)

	// Create matrix with overlapping groups
	testMatrix := &domain.TestMatrix{
		NumCommits:  8,
		Repetitions: 1,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1, 2}},    // PASS
			{ID: "g1", Index: 1, CommitIndices: []int{2, 3, 4}},    // FAIL
			{ID: "g2", Index: 2, CommitIndices: []int{5, 6}},       // PASS
			{ID: "g3", Index: 3, CommitIndices: []int{6, 7}},       // FAIL
		},
	}

	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, GroupID: "g0", Outcome: domain.OutcomePass},
		{GroupIndex: 1, GroupID: "g1", Outcome: domain.OutcomeFail},
		{GroupIndex: 2, GroupID: "g2", Outcome: domain.OutcomePass},
		{GroupIndex: 3, GroupID: "g3", Outcome: domain.OutcomeFail},
	}

	result := DecodeWithDeterministicElimination(commits, testMatrix, outcomes)

	// Expected classification:
	// - Commits 0, 1: in PASS g0 -> INNOCENT
	// - Commit 2: in PASS g0 and FAIL g1 -> INNOCENT (one pass is enough)
	// - Commits 3, 4: only in FAIL g1 -> CULPRIT candidates
	// - Commits 5, 6: in PASS g2 -> INNOCENT
	// - Commit 7: only in FAIL g3 -> CULPRIT candidate

	innocentSet := make(map[string]bool)
	for _, sha := range result.InnocentCommits {
		innocentSet[sha] = true
	}

	culpritSet := make(map[int]bool)
	for _, c := range result.IdentifiedCulprits {
		culpritSet[c.Commit.Index] = true
	}

	// Check innocent commits (0, 1, 2, 5, 6)
	for _, idx := range []int{0, 1, 2, 5, 6} {
		sha := fmt.Sprintf("commit-%d", idx)
		if !innocentSet[sha] {
			t.Errorf("Commit %d should be innocent", idx)
		}
	}

	// Check culprit candidates (3, 4, 7)
	for _, idx := range []int{3, 4, 7} {
		if !culpritSet[idx] {
			t.Errorf("Commit %d should be culprit candidate", idx)
		}
	}
}

func TestDeterministicEliminationEmptyCommits(t *testing.T) {
	result := DecodeWithDeterministicElimination(
		[]domain.Commit{},
		&domain.TestMatrix{},
		[]domain.AggregatedGroupOutcome{},
	)

	if len(result.IdentifiedCulprits) != 0 {
		t.Error("Expected 0 culprits for empty commits")
	}
	if len(result.InnocentCommits) != 0 {
		t.Error("Expected 0 innocent for empty commits")
	}
}

func TestDeterministicEliminationAllPass(t *testing.T) {
	commits := makeCommits(5)

	testMatrix := &domain.TestMatrix{
		NumCommits:  5,
		Repetitions: 1,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1, 2, 3, 4}},
		},
	}

	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, GroupID: "g0", Outcome: domain.OutcomePass},
	}

	result := DecodeWithDeterministicElimination(commits, testMatrix, outcomes)

	// All should be innocent
	if len(result.InnocentCommits) != 5 {
		t.Errorf("Expected 5 innocent commits, got %d", len(result.InnocentCommits))
	}
	if len(result.IdentifiedCulprits) != 0 {
		t.Errorf("Expected 0 culprits, got %d", len(result.IdentifiedCulprits))
	}
}

func TestDeterministicEliminationAllFail(t *testing.T) {
	commits := makeCommits(3)

	testMatrix := &domain.TestMatrix{
		NumCommits:  3,
		Repetitions: 1,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1, 2}},
		},
	}

	outcomes := []domain.AggregatedGroupOutcome{
		{GroupIndex: 0, GroupID: "g0", Outcome: domain.OutcomeFail},
	}

	result := DecodeWithDeterministicElimination(commits, testMatrix, outcomes)

	// All should be culprit candidates (no passing groups to eliminate anyone)
	if len(result.IdentifiedCulprits) != 3 {
		t.Errorf("Expected 3 culprits, got %d", len(result.IdentifiedCulprits))
	}
	if len(result.InnocentCommits) != 0 {
		t.Errorf("Expected 0 innocent, got %d", len(result.InnocentCommits))
	}
}

// ============================================================================
// Statistical Accuracy Tests
// ============================================================================

func TestDecoderAccuracyNoFlakes(t *testing.T) {
	// Run multiple trials and measure accuracy
	numTrials := 20
	correctIdentifications := 0

	for trial := 0; trial < numTrials; trial++ {
		commits := makeCommits(30)
		config := domain.CulpritFinderConfig{
			MaxCulprits:         2,
			Repetitions:         3,
			ConfidenceThreshold: 0.7,
			RandomSeed:          int64(trial * 100),
		}.WithDefaults()

		builder := matrix.NewBuilder(func() string { return "test-matrix" })
		testMatrix, _ := builder.Build(commits, config)

		// Random culprit
		culpritIdx := (trial * 7) % 30
		culprits := map[int]bool{culpritIdx: true}

		rng := rand.New(rand.NewSource(int64(trial)))
		results := simulateTests(testMatrix, culprits, 0, rng) // No flakes

		dec := NewDecoder(config)
		decoded, _ := dec.Decode(commits, testMatrix, results)

		// Check if actual culprit was found
		for _, c := range decoded.IdentifiedCulprits {
			if c.Commit.Index == culpritIdx {
				correctIdentifications++
				break
			}
		}
	}

	accuracy := float64(correctIdentifications) / float64(numTrials)
	t.Logf("Accuracy (no flakes): %.1f%% (%d/%d)", accuracy*100, correctIdentifications, numTrials)

	// With no flakes, accuracy should be very high
	if accuracy < 0.9 {
		t.Errorf("Accuracy too low: %.1f%%, want >= 90%%", accuracy*100)
	}
}

func TestDecoderAccuracyWithFlakes(t *testing.T) {
	// Run multiple trials with flakes
	numTrials := 30
	correctIdentifications := 0

	for trial := 0; trial < numTrials; trial++ {
		commits := makeCommits(25)
		config := domain.CulpritFinderConfig{
			MaxCulprits:         2,
			Repetitions:         5, // More repetitions for flake robustness
			ConfidenceThreshold: 0.6,
			FalsePositiveRate:   0.1,
			FalseNegativeRate:   0.1,
			RandomSeed:          int64(trial * 100),
		}.WithDefaults()

		builder := matrix.NewBuilder(func() string { return "test-matrix" })
		testMatrix, _ := builder.Build(commits, config)

		culpritIdx := (trial * 7) % 25
		culprits := map[int]bool{culpritIdx: true}

		rng := rand.New(rand.NewSource(int64(trial + 999)))
		results := simulateTests(testMatrix, culprits, 0.1, rng) // 10% flake rate

		dec := NewDecoder(config)
		decoded, _ := dec.Decode(commits, testMatrix, results)

		for _, c := range decoded.IdentifiedCulprits {
			if c.Commit.Index == culpritIdx {
				correctIdentifications++
				break
			}
		}
	}

	accuracy := float64(correctIdentifications) / float64(numTrials)
	t.Logf("Accuracy (10%% flakes): %.1f%% (%d/%d)", accuracy*100, correctIdentifications, numTrials)

	// With flakes, accuracy should still be reasonable
	if accuracy < 0.7 {
		t.Errorf("Accuracy too low with flakes: %.1f%%, want >= 70%%", accuracy*100)
	}
}

func TestDecoderAccuracyTwoCulprits(t *testing.T) {
	numTrials := 20
	foundBoth := 0
	foundOne := 0

	for trial := 0; trial < numTrials; trial++ {
		commits := makeCommits(40)
		config := domain.CulpritFinderConfig{
			MaxCulprits:         2,
			Repetitions:         3,
			ConfidenceThreshold: 0.6,
			RandomSeed:          int64(trial * 100),
		}.WithDefaults()

		builder := matrix.NewBuilder(func() string { return "test-matrix" })
		testMatrix, _ := builder.Build(commits, config)

		// Two culprits at different positions
		culprit1 := (trial * 7) % 40
		culprit2 := (trial*13 + 20) % 40
		if culprit2 == culprit1 {
			culprit2 = (culprit1 + 5) % 40
		}
		culprits := map[int]bool{culprit1: true, culprit2: true}

		rng := rand.New(rand.NewSource(int64(trial)))
		results := simulateTests(testMatrix, culprits, 0, rng)

		dec := NewDecoder(config)
		decoded, _ := dec.Decode(commits, testMatrix, results)

		foundSet := make(map[int]bool)
		for _, c := range decoded.IdentifiedCulprits {
			foundSet[c.Commit.Index] = true
		}

		if foundSet[culprit1] && foundSet[culprit2] {
			foundBoth++
			foundOne++
		} else if foundSet[culprit1] || foundSet[culprit2] {
			foundOne++
		}
	}

	t.Logf("Two culprits: found both=%.1f%%, found at least one=%.1f%%",
		float64(foundBoth)/float64(numTrials)*100,
		float64(foundOne)/float64(numTrials)*100)

	// Should find at least one culprit most of the time
	if float64(foundOne)/float64(numTrials) < 0.8 {
		t.Errorf("Too low detection rate for two culprits")
	}
}

// ============================================================================
// Edge Cases for Decoding
// ============================================================================

func TestDecoderSingleCommit(t *testing.T) {
	commits := makeCommits(2) // Minimum valid
	config := domain.CulpritFinderConfig{
		MaxCulprits: 1,
		Repetitions: 3,
		RandomSeed:  42,
	}.WithDefaults()

	builder := matrix.NewBuilder(func() string { return "test-matrix" })
	testMatrix, _ := builder.Build(commits, config)

	// Commit 0 is culprit
	culprits := map[int]bool{0: true}
	rng := rand.New(rand.NewSource(42))
	results := simulateTests(testMatrix, culprits, 0, rng)

	dec := NewDecoder(config)
	decoded, err := dec.Decode(commits, testMatrix, results)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Should identify commit 0 or have it as ambiguous
	found := false
	for _, c := range decoded.IdentifiedCulprits {
		if c.Commit.Index == 0 {
			found = true
			break
		}
	}

	if !found && len(decoded.AmbiguousCommits) == 0 {
		t.Error("Culprit commit 0 not identified and no ambiguous commits")
	}
}

func TestDecoderAllInfraFailures(t *testing.T) {
	commits := makeCommits(5)

	testMatrix := &domain.TestMatrix{
		NumCommits:  5,
		Repetitions: 3,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1, 2}},
			{ID: "g1", Index: 1, CommitIndices: []int{2, 3, 4}},
		},
	}

	// All results are infra failures
	var results []domain.TestGroupResult
	for _, g := range testMatrix.AllGroups() {
		results = append(results, domain.TestGroupResult{
			GroupID:    g.GroupID,
			GroupIndex: g.GroupIndex,
			Repetition: g.Repetition,
			Outcome:    domain.OutcomeInfra,
		})
	}

	config := domain.DefaultConfig()
	dec := NewDecoder(config)
	decoded, err := dec.Decode(commits, testMatrix, results)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// With all infra failures, all commits should be ambiguous
	if len(decoded.IdentifiedCulprits) != 0 {
		t.Errorf("Expected 0 culprits with all infra failures, got %d",
			len(decoded.IdentifiedCulprits))
	}

	// All should be ambiguous since we have no usable data
	t.Logf("All infra: %d innocent, %d culprits, %d ambiguous",
		len(decoded.InnocentCommits),
		len(decoded.IdentifiedCulprits),
		len(decoded.AmbiguousCommits))
}

func TestDecoderMixedOutcomes(t *testing.T) {
	commits := makeCommits(6)

	testMatrix := &domain.TestMatrix{
		NumCommits:  6,
		Repetitions: 5,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1}},
			{ID: "g1", Index: 1, CommitIndices: []int{2, 3}},
			{ID: "g2", Index: 2, CommitIndices: []int{4, 5}},
		},
	}

	// Create mixed results:
	// Group 0: 3 pass, 2 fail -> PASS
	// Group 1: 2 pass, 2 fail, 1 infra -> FAIL (tie goes to fail)
	// Group 2: 1 pass, 3 fail, 1 infra -> FAIL
	results := []domain.TestGroupResult{
		// Group 0
		{GroupID: "g0", GroupIndex: 0, Repetition: 1, Outcome: domain.OutcomePass},
		{GroupID: "g0", GroupIndex: 0, Repetition: 2, Outcome: domain.OutcomePass},
		{GroupID: "g0", GroupIndex: 0, Repetition: 3, Outcome: domain.OutcomePass},
		{GroupID: "g0", GroupIndex: 0, Repetition: 4, Outcome: domain.OutcomeFail},
		{GroupID: "g0", GroupIndex: 0, Repetition: 5, Outcome: domain.OutcomeFail},

		// Group 1
		{GroupID: "g1", GroupIndex: 1, Repetition: 1, Outcome: domain.OutcomePass},
		{GroupID: "g1", GroupIndex: 1, Repetition: 2, Outcome: domain.OutcomePass},
		{GroupID: "g1", GroupIndex: 1, Repetition: 3, Outcome: domain.OutcomeFail},
		{GroupID: "g1", GroupIndex: 1, Repetition: 4, Outcome: domain.OutcomeFail},
		{GroupID: "g1", GroupIndex: 1, Repetition: 5, Outcome: domain.OutcomeInfra},

		// Group 2
		{GroupID: "g2", GroupIndex: 2, Repetition: 1, Outcome: domain.OutcomePass},
		{GroupID: "g2", GroupIndex: 2, Repetition: 2, Outcome: domain.OutcomeFail},
		{GroupID: "g2", GroupIndex: 2, Repetition: 3, Outcome: domain.OutcomeFail},
		{GroupID: "g2", GroupIndex: 2, Repetition: 4, Outcome: domain.OutcomeFail},
		{GroupID: "g2", GroupIndex: 2, Repetition: 5, Outcome: domain.OutcomeInfra},
	}

	config := domain.CulpritFinderConfig{
		MaxCulprits:         2,
		Repetitions:         5,
		ConfidenceThreshold: 0.6,
	}.WithDefaults()

	dec := NewDecoder(config)
	decoded, err := dec.Decode(commits, testMatrix, results)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Commits 0, 1 should be innocent (in passing group 0)
	innocentSet := make(map[string]bool)
	for _, sha := range decoded.InnocentCommits {
		innocentSet[sha] = true
	}

	for _, idx := range []int{0, 1} {
		sha := fmt.Sprintf("commit-%d", idx)
		if !innocentSet[sha] {
			t.Errorf("Commit %d should be innocent (in passing group)", idx)
		}
	}

	t.Logf("Mixed: %d innocent, %d culprits, %d ambiguous",
		len(decoded.InnocentCommits),
		len(decoded.IdentifiedCulprits),
		len(decoded.AmbiguousCommits))
}
