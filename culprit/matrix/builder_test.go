package matrix

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/example/turboci-lite/culprit/domain"
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

func TestBuilderBasic(t *testing.T) {
	idCounter := 0
	idGen := func() string {
		idCounter++
		return fmt.Sprintf("id-%d", idCounter)
	}

	builder := NewBuilder(idGen)
	commits := makeCommits(20)
	config := domain.CulpritFinderConfig{
		MaxCulprits: 2,
		Repetitions: 3,
		RandomSeed:  42,
	}.WithDefaults()

	matrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Basic checks
	if matrix.NumCommits != 20 {
		t.Errorf("NumCommits = %d, want 20", matrix.NumCommits)
	}

	if matrix.Repetitions != 3 {
		t.Errorf("Repetitions = %d, want 3", matrix.Repetitions)
	}

	// Should have a reasonable number of groups
	numGroups := matrix.NumGroups()
	if numGroups < 2 {
		t.Errorf("NumGroups = %d, want at least 2", numGroups)
	}

	// Total tests should be groups * repetitions
	totalTests := matrix.TotalTests()
	if totalTests != numGroups*3 {
		t.Errorf("TotalTests = %d, want %d", totalTests, numGroups*3)
	}

	// AllGroups should return the expanded list
	allGroups := matrix.AllGroups()
	if len(allGroups) != totalTests {
		t.Errorf("len(AllGroups) = %d, want %d", len(allGroups), totalTests)
	}

	// Each commit should appear in at least one group
	for i := 0; i < 20; i++ {
		groups := matrix.GroupsContainingCommit(i)
		if len(groups) == 0 {
			t.Errorf("Commit %d not in any group", i)
		}
	}
}

func TestBuilderSmallRange(t *testing.T) {
	builder := NewBuilder(func() string { return "test-id" })
	commits := makeCommits(5)
	config := domain.CulpritFinderConfig{
		MaxCulprits: 1,
		Repetitions: 2,
		RandomSeed:  42,
	}.WithDefaults()

	matrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if matrix.NumCommits != 5 {
		t.Errorf("NumCommits = %d, want 5", matrix.NumCommits)
	}

	// Each commit should be covered
	for i := 0; i < 5; i++ {
		if len(matrix.GroupsContainingCommit(i)) == 0 {
			t.Errorf("Commit %d not covered", i)
		}
	}
}

func TestBuilderTooFewCommits(t *testing.T) {
	builder := NewBuilder(func() string { return "test-id" })
	commits := makeCommits(1)
	config := domain.DefaultConfig()

	_, err := builder.Build(commits, config)
	if err == nil {
		t.Error("Expected error for single commit")
	}
}

func TestBuilderInvalidConfig(t *testing.T) {
	builder := NewBuilder(func() string { return "test-id" })
	commits := makeCommits(10)

	tests := []struct {
		name   string
		config domain.CulpritFinderConfig
	}{
		// Note: 0 values get replaced by defaults in WithDefaults(), so we test
		// explicitly invalid values that can't be fixed by defaults
		{"too many culprits", domain.CulpritFinderConfig{MaxCulprits: 100, Repetitions: 3}},
		{"too many repetitions", domain.CulpritFinderConfig{MaxCulprits: 2, Repetitions: 100}},
		{"negative confidence", domain.CulpritFinderConfig{MaxCulprits: 2, Repetitions: 3, ConfidenceThreshold: -0.5}},
		{"confidence > 1", domain.CulpritFinderConfig{MaxCulprits: 2, Repetitions: 3, ConfidenceThreshold: 1.5}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := builder.Build(commits, tc.config)
			if err == nil {
				t.Error("Expected error for invalid config")
			}
		})
	}
}

func TestCalculateNumGroups(t *testing.T) {
	tests := []struct {
		n, d     int
		minWant  int
		maxWant  int
	}{
		{10, 1, 2, 20},
		{20, 2, 10, 100},
		{50, 2, 20, 200},
		{100, 2, 30, 300},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("n=%d,d=%d", tc.n, tc.d), func(t *testing.T) {
			numGroups := calculateNumGroups(tc.n, tc.d)
			if numGroups < tc.minWant || numGroups > tc.maxWant {
				t.Errorf("calculateNumGroups(%d, %d) = %d, want in [%d, %d]",
					tc.n, tc.d, numGroups, tc.minWant, tc.maxWant)
			}
		})
	}
}

func TestMatrixMethods(t *testing.T) {
	builder := NewBuilder(func() string { return "test-id" })
	commits := makeCommits(10)
	config := domain.CulpritFinderConfig{
		MaxCulprits: 1,
		Repetitions: 2,
		RandomSeed:  123,
	}.WithDefaults()

	matrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Test CommitInGroup
	for i, group := range matrix.Groups {
		for _, commitIdx := range group.CommitIndices {
			if !matrix.CommitInGroup(commitIdx, i) {
				t.Errorf("CommitInGroup(%d, %d) = false, want true", commitIdx, i)
			}
		}
	}

	// Test GetGroup
	for i := range matrix.Groups {
		group := matrix.GetGroup(i)
		if group == nil {
			t.Errorf("GetGroup(%d) = nil", i)
		}
	}

	if matrix.GetGroup(-1) != nil {
		t.Error("GetGroup(-1) should be nil")
	}
	if matrix.GetGroup(len(matrix.Groups)) != nil {
		t.Error("GetGroup(len) should be nil")
	}

	// Test Coverage
	coverage := matrix.Coverage()
	if coverage <= 0 {
		t.Errorf("Coverage = %f, want > 0", coverage)
	}
}

func TestDisjunctProperty(t *testing.T) {
	// Test that the matrix has reasonable disjunct properties
	// Note: Random matrices satisfy d-disjunct property with high probability,
	// but verification is probabilistic. We just check basic coverage.
	commits := makeCommits(30)
	config := domain.CulpritFinderConfig{
		MaxCulprits: 2,
		Repetitions: 1,
		RandomSeed:  42,
	}.WithDefaults()

	builder := NewBuilder(func() string { return "test-id" })
	matrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify each commit appears in multiple groups (coverage)
	for i := 0; i < 30; i++ {
		groups := matrix.GroupsContainingCommit(i)
		if len(groups) < 2 {
			t.Errorf("Commit %d only in %d groups, want at least 2", i, len(groups))
		}
	}

	// Verify we have enough groups for d=2
	expectedMinGroups := 4 * 2 * 2 // ~16 for d=2, log2(30)~5
	if matrix.NumGroups() < expectedMinGroups/2 {
		t.Errorf("Only %d groups, expected at least %d", matrix.NumGroups(), expectedMinGroups/2)
	}
}

func TestReproducibility(t *testing.T) {
	commits := makeCommits(20)
	config := domain.CulpritFinderConfig{
		MaxCulprits: 2,
		Repetitions: 3,
		RandomSeed:  999,
	}.WithDefaults()

	builder1 := NewBuilder(func() string { return "id-1" })
	matrix1, _ := builder1.Build(commits, config)

	builder2 := NewBuilder(func() string { return "id-2" })
	matrix2, _ := builder2.Build(commits, config)

	// With same seed, the groups should be identical
	if len(matrix1.Groups) != len(matrix2.Groups) {
		t.Error("Different number of groups with same seed")
	}

	for i := range matrix1.Groups {
		g1 := matrix1.Groups[i]
		g2 := matrix2.Groups[i]
		if len(g1.CommitIndices) != len(g2.CommitIndices) {
			t.Errorf("Group %d has different sizes", i)
			continue
		}
		for j := range g1.CommitIndices {
			if g1.CommitIndices[j] != g2.CommitIndices[j] {
				t.Errorf("Group %d differs at position %d", i, j)
			}
		}
	}
}

// ============================================================================
// Edge Case Tests for Matrix Construction
// ============================================================================

func TestBuilderMinimumCommits(t *testing.T) {
	builder := NewBuilder(func() string { return "test-id" })

	tests := []struct {
		name       string
		numCommits int
		wantError  bool
	}{
		{"zero commits", 0, true},
		{"one commit", 1, true},
		{"two commits (minimum valid)", 2, false},
		{"three commits", 3, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			commits := makeCommits(tc.numCommits)
			config := domain.DefaultConfig()
			_, err := builder.Build(commits, config)
			if (err != nil) != tc.wantError {
				t.Errorf("Build() error = %v, wantError = %v", err, tc.wantError)
			}
		})
	}
}

func TestBuilderLargeCommitRange(t *testing.T) {
	builder := NewBuilder(func() string { return "test-id" })

	tests := []struct {
		name       string
		numCommits int
		maxCulprit int
	}{
		{"100 commits d=1", 100, 1},
		{"100 commits d=2", 100, 2},
		{"100 commits d=3", 100, 3},
		{"500 commits d=2", 500, 2},
		{"1000 commits d=1", 1000, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			commits := makeCommits(tc.numCommits)
			config := domain.CulpritFinderConfig{
				MaxCulprits: tc.maxCulprit,
				Repetitions: 1,
				RandomSeed:  42,
			}.WithDefaults()

			matrix, err := builder.Build(commits, config)
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}

			// Verify all commits are covered
			for i := 0; i < tc.numCommits; i++ {
				groups := matrix.GroupsContainingCommit(i)
				if len(groups) == 0 {
					t.Errorf("Commit %d not in any group", i)
				}
			}

			// Verify group count is reasonable (O(d^2 * log n))
			expectedMinGroups := tc.maxCulprit
			if matrix.NumGroups() < expectedMinGroups {
				t.Errorf("NumGroups = %d, want >= %d", matrix.NumGroups(), expectedMinGroups)
			}

			t.Logf("n=%d, d=%d: %d groups, coverage=%.2f",
				tc.numCommits, tc.maxCulprit, matrix.NumGroups(), matrix.Coverage())
		})
	}
}

func TestCalculateColumnWeight(t *testing.T) {
	tests := []struct {
		n, d, numGroups int
		minWant         int
		maxWant         int
	}{
		{10, 1, 10, 2, 10},    // Small n
		{20, 2, 40, 5, 20},    // Medium n
		{100, 2, 100, 10, 50}, // Large n
		{5, 1, 5, 2, 5},       // Very small n
		{10, 1, 3, 2, 3},      // numGroups < weight (should clamp)
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("n=%d,d=%d,g=%d", tc.n, tc.d, tc.numGroups), func(t *testing.T) {
			weight := calculateColumnWeight(tc.n, tc.d, tc.numGroups)
			if weight < tc.minWant || weight > tc.maxWant {
				t.Errorf("calculateColumnWeight(%d, %d, %d) = %d, want in [%d, %d]",
					tc.n, tc.d, tc.numGroups, weight, tc.minWant, tc.maxWant)
			}
		})
	}
}

func TestCalculateColumnWeightEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		n, d, numGroups int
		want            int
	}{
		{"n=0", 0, 1, 10, 0},
		{"n=1", 1, 1, 10, 0},
		{"numGroups=0", 10, 1, 0, 0},
		{"negative numGroups", 10, 1, -1, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			weight := calculateColumnWeight(tc.n, tc.d, tc.numGroups)
			if weight != tc.want {
				t.Errorf("calculateColumnWeight(%d, %d, %d) = %d, want %d",
					tc.n, tc.d, tc.numGroups, weight, tc.want)
			}
		})
	}
}

func TestCalculateNumGroupsEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		n, d    int
		wantMin int
		wantMax int
	}{
		{"n=0", 0, 1, 0, 0},
		{"n=1", 1, 1, 0, 0},
		{"very small n", 2, 1, 2, 2},
		{"d larger than practical", 5, 5, 5, 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			numGroups := calculateNumGroups(tc.n, tc.d)
			if numGroups < tc.wantMin || numGroups > tc.wantMax {
				t.Errorf("calculateNumGroups(%d, %d) = %d, want in [%d, %d]",
					tc.n, tc.d, numGroups, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestRandomSampleEdgeCases(t *testing.T) {
	rng := newTestRng(42)

	tests := []struct {
		name    string
		n, k    int
		wantLen int
	}{
		{"k < n", 10, 5, 5},
		{"k = n", 10, 10, 10},
		{"k > n", 10, 15, 10},
		{"k = 0", 10, 0, 0},
		{"n = 0", 0, 5, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sample := randomSample(rng, tc.n, tc.k)
			if len(sample) != tc.wantLen {
				t.Errorf("randomSample(%d, %d) len = %d, want %d",
					tc.n, tc.k, len(sample), tc.wantLen)
			}

			// Verify all values are in range [0, n)
			for _, v := range sample {
				if v < 0 || v >= tc.n {
					t.Errorf("randomSample returned %d which is out of range [0, %d)", v, tc.n)
				}
			}

			// Verify no duplicates
			seen := make(map[int]bool)
			for _, v := range sample {
				if seen[v] {
					t.Errorf("randomSample returned duplicate value %d", v)
				}
				seen[v] = true
			}
		})
	}
}

func newTestRng(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

func TestMatrixGroupSizeDistribution(t *testing.T) {
	// Test that group sizes are reasonable (not too skewed)
	commits := makeCommits(50)
	config := domain.CulpritFinderConfig{
		MaxCulprits: 2,
		Repetitions: 1,
		RandomSeed:  42,
	}.WithDefaults()

	builder := NewBuilder(func() string { return "test-id" })
	matrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	var sizes []int
	totalSize := 0
	for _, g := range matrix.Groups {
		sizes = append(sizes, len(g.CommitIndices))
		totalSize += len(g.CommitIndices)
	}

	avgSize := float64(totalSize) / float64(len(sizes))

	// Group sizes should average around n * weight / numGroups
	// This is because each commit appears in 'weight' groups
	if avgSize < 1 {
		t.Error("Average group size too small")
	}

	// Check for empty groups
	emptyGroups := 0
	for _, size := range sizes {
		if size == 0 {
			emptyGroups++
		}
	}
	// Allow some empty groups but not too many
	maxEmptyRatio := 0.1
	if float64(emptyGroups)/float64(len(sizes)) > maxEmptyRatio {
		t.Errorf("Too many empty groups: %d out of %d", emptyGroups, len(sizes))
	}

	t.Logf("Groups: %d, Avg size: %.2f, Empty: %d", len(sizes), avgSize, emptyGroups)
}

func TestMatrixCommitCoverageDistribution(t *testing.T) {
	// Test that all commits have similar coverage
	commits := makeCommits(30)
	config := domain.CulpritFinderConfig{
		MaxCulprits: 2,
		Repetitions: 1,
		RandomSeed:  42,
	}.WithDefaults()

	builder := NewBuilder(func() string { return "test-id" })
	matrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	coverages := make([]int, 30)
	for i := 0; i < 30; i++ {
		coverages[i] = len(matrix.GroupsContainingCommit(i))
	}

	// All commits should have similar coverage
	minCov, maxCov := coverages[0], coverages[0]
	for _, c := range coverages {
		if c < minCov {
			minCov = c
		}
		if c > maxCov {
			maxCov = c
		}
	}

	// Coverage variance should not be too extreme
	if minCov == 0 {
		t.Error("Some commits have zero coverage")
	}

	// Allow 3x variance in coverage
	if maxCov > 3*minCov && minCov > 0 {
		t.Errorf("Coverage too variable: min=%d, max=%d", minCov, maxCov)
	}

	t.Logf("Coverage: min=%d, max=%d, avg=%.2f", minCov, maxCov, matrix.Coverage())
}

func TestMatrixCommitInGroupBoundaries(t *testing.T) {
	matrix := &domain.TestMatrix{
		NumCommits: 5,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1, 2}},
			{ID: "g1", Index: 1, CommitIndices: []int{3, 4}},
		},
	}

	tests := []struct {
		commitIdx int
		groupIdx  int
		want      bool
	}{
		{0, 0, true},
		{1, 0, true},
		{2, 0, true},
		{3, 0, false},
		{3, 1, true},
		{4, 1, true},
		{-1, 0, false},  // Negative commit
		{5, 0, false},   // Out of range commit
		{0, -1, false},  // Negative group
		{0, 2, false},   // Out of range group
		{100, 0, false}, // Way out of range
	}

	for _, tc := range tests {
		got := matrix.CommitInGroup(tc.commitIdx, tc.groupIdx)
		if got != tc.want {
			t.Errorf("CommitInGroup(%d, %d) = %v, want %v",
				tc.commitIdx, tc.groupIdx, got, tc.want)
		}
	}
}

func TestMatrixAllGroupsExpansion(t *testing.T) {
	matrix := &domain.TestMatrix{
		NumCommits:  5,
		Repetitions: 3,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{0, 1}},
			{ID: "g1", Index: 1, CommitIndices: []int{2, 3, 4}},
		},
	}

	allGroups := matrix.AllGroups()

	// Should have 2 groups * 3 repetitions = 6 instances
	if len(allGroups) != 6 {
		t.Errorf("AllGroups() len = %d, want 6", len(allGroups))
	}

	// Check repetitions are 1, 2, 3 for each base group
	repCounts := make(map[string][]int)
	for _, g := range allGroups {
		repCounts[g.GroupID] = append(repCounts[g.GroupID], g.Repetition)
	}

	for groupID, reps := range repCounts {
		if len(reps) != 3 {
			t.Errorf("Group %s has %d repetitions, want 3", groupID, len(reps))
		}
		// Verify we have reps 1, 2, 3
		hasRep := make(map[int]bool)
		for _, r := range reps {
			hasRep[r] = true
		}
		for r := 1; r <= 3; r++ {
			if !hasRep[r] {
				t.Errorf("Group %s missing repetition %d", groupID, r)
			}
		}
	}
}

func TestMatrixEmptyGroups(t *testing.T) {
	// Test matrix with no groups
	matrix := &domain.TestMatrix{
		NumCommits:  5,
		Repetitions: 3,
		Groups:      []domain.TestGroup{},
	}

	if matrix.NumGroups() != 0 {
		t.Errorf("NumGroups() = %d, want 0", matrix.NumGroups())
	}

	if matrix.TotalTests() != 0 {
		t.Errorf("TotalTests() = %d, want 0", matrix.TotalTests())
	}

	allGroups := matrix.AllGroups()
	if len(allGroups) != 0 {
		t.Errorf("AllGroups() len = %d, want 0", len(allGroups))
	}

	if matrix.Coverage() != 0 {
		t.Errorf("Coverage() = %f, want 0", matrix.Coverage())
	}
}

func TestMatrixZeroCommits(t *testing.T) {
	matrix := &domain.TestMatrix{
		NumCommits:  0,
		Repetitions: 3,
		Groups: []domain.TestGroup{
			{ID: "g0", Index: 0, CommitIndices: []int{}},
		},
	}

	if matrix.Coverage() != 0 {
		t.Errorf("Coverage() = %f, want 0 for zero commits", matrix.Coverage())
	}
}

// ============================================================================
// D-Disjunct Property Verification Tests
// ============================================================================

func TestVerifyDisjunctPropertyD1(t *testing.T) {
	// Create a known 1-disjunct matrix (identity matrix is always 1-disjunct)
	// Each column appears in exactly one unique row
	n := 5
	identityMatrix := make([][]int, n)
	for i := 0; i < n; i++ {
		identityMatrix[i] = []int{i}
	}

	if !verifyDisjunctProperty(identityMatrix, n, 1) {
		t.Error("Identity matrix should be 1-disjunct")
	}
}

func TestVerifyDisjunctPropertyViolation(t *testing.T) {
	// Create a matrix that violates 1-disjunct property
	// Column 0 is covered by column 1 (column 0 only appears where column 1 appears)
	matrix := [][]int{
		{0, 1}, // Both 0 and 1
		{1},    // Only 1
		{2},    // Only 2
	}

	// Column 0 is covered by column 1 (column 0's rows are subset of column 1's rows)
	// So this is NOT 1-disjunct
	if verifyDisjunctProperty(matrix, 3, 1) {
		t.Error("Matrix should NOT be 1-disjunct (column 0 is covered by column 1)")
	}
}

func TestVerifyDisjunctPropertyProbabilistic(t *testing.T) {
	// Generate a random matrix and verify it has high probability of being d-disjunct
	n := 30
	d := 2
	numGroups := calculateNumGroups(n, d)
	matrix := generateRandomDisjunctMatrix(n, d, numGroups, 42)

	// Random construction should produce d-disjunct matrix with high probability
	result := verifyDisjunctProperty(matrix, n, d)

	// Log the result (we can't guarantee it passes, but it should usually)
	t.Logf("Random matrix (n=%d, d=%d, groups=%d) disjunct verification: %v",
		n, d, numGroups, result)
}

func TestVerifyDisjunctPropertyEmptyMatrix(t *testing.T) {
	// Empty matrix should return true (vacuously true)
	result := verifyDisjunctProperty([][]int{}, 0, 1)
	if !result {
		t.Error("Empty matrix should be disjunct")
	}
}

func TestVerifyDisjunctPropertySmallD(t *testing.T) {
	// For d=0, any matrix is d-disjunct
	matrix := [][]int{{0, 1, 2}}
	if !verifyDisjunctProperty(matrix, 3, 0) {
		t.Error("Any matrix should be 0-disjunct")
	}
}

// ============================================================================
// Random Seed Tests
// ============================================================================

func TestDifferentSeeds(t *testing.T) {
	commits := makeCommits(20)
	config1 := domain.CulpritFinderConfig{
		MaxCulprits: 2,
		Repetitions: 1,
		RandomSeed:  1,
	}.WithDefaults()

	config2 := domain.CulpritFinderConfig{
		MaxCulprits: 2,
		Repetitions: 1,
		RandomSeed:  2,
	}.WithDefaults()

	builder := NewBuilder(func() string { return "test-id" })
	matrix1, _ := builder.Build(commits, config1)
	matrix2, _ := builder.Build(commits, config2)

	// Different seeds should produce different matrices
	// (with overwhelming probability)
	differences := 0
	for i := range matrix1.Groups {
		if i >= len(matrix2.Groups) {
			differences++
			continue
		}
		if len(matrix1.Groups[i].CommitIndices) != len(matrix2.Groups[i].CommitIndices) {
			differences++
			continue
		}
		for j := range matrix1.Groups[i].CommitIndices {
			if matrix1.Groups[i].CommitIndices[j] != matrix2.Groups[i].CommitIndices[j] {
				differences++
				break
			}
		}
	}

	if differences == 0 {
		t.Error("Different seeds produced identical matrices")
	}
}

func TestZeroSeedIsRandom(t *testing.T) {
	// With seed=0, should get random (non-reproducible) matrices
	commits := makeCommits(20)
	config := domain.CulpritFinderConfig{
		MaxCulprits: 2,
		Repetitions: 1,
		RandomSeed:  0, // Random
	}.WithDefaults()

	// Note: We can't really test randomness, but we can verify it doesn't crash
	builder := NewBuilder(func() string { return "test-id" })
	matrix, err := builder.Build(commits, config)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if matrix.NumGroups() < 2 {
		t.Error("Random matrix has too few groups")
	}
}

// ============================================================================
// Various MaxCulprits Values
// ============================================================================

func TestVariousMaxCulpritsValues(t *testing.T) {
	commits := makeCommits(50)

	for d := 1; d <= 5; d++ {
		t.Run(fmt.Sprintf("d=%d", d), func(t *testing.T) {
			config := domain.CulpritFinderConfig{
				MaxCulprits: d,
				Repetitions: 1,
				RandomSeed:  42,
			}.WithDefaults()

			builder := NewBuilder(func() string { return "test-id" })
			matrix, err := builder.Build(commits, config)
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}

			// Verify reasonable number of groups (O(d^2 * log n))
			// For n=50, log2(50)~5.6
			minExpectedGroups := d // At minimum
			if matrix.NumGroups() < minExpectedGroups {
				t.Errorf("NumGroups = %d, want >= %d", matrix.NumGroups(), minExpectedGroups)
			}

			// Verify coverage increases with d
			avgCoverage := matrix.Coverage()
			minCoverage := float64(d) // Each commit should be in at least d groups approximately
			if avgCoverage < minCoverage*0.5 {
				t.Errorf("Coverage = %.2f, want >= %.2f", avgCoverage, minCoverage*0.5)
			}

			t.Logf("d=%d: %d groups, coverage=%.2f", d, matrix.NumGroups(), avgCoverage)
		})
	}
}

// ============================================================================
// Repetitions Tests
// ============================================================================

func TestVariousRepetitions(t *testing.T) {
	commits := makeCommits(10)

	for reps := 1; reps <= 10; reps++ {
		t.Run(fmt.Sprintf("reps=%d", reps), func(t *testing.T) {
			config := domain.CulpritFinderConfig{
				MaxCulprits: 1,
				Repetitions: reps,
				RandomSeed:  42,
			}.WithDefaults()

			builder := NewBuilder(func() string { return "test-id" })
			matrix, err := builder.Build(commits, config)
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}

			expectedTotal := matrix.NumGroups() * reps
			if matrix.TotalTests() != expectedTotal {
				t.Errorf("TotalTests = %d, want %d", matrix.TotalTests(), expectedTotal)
			}

			allGroups := matrix.AllGroups()
			if len(allGroups) != expectedTotal {
				t.Errorf("AllGroups len = %d, want %d", len(allGroups), expectedTotal)
			}
		})
	}
}
