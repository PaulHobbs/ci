package matrix

import (
	"math"
	"math/rand"
	"sort"
)

// calculateNumGroups determines the number of test groups needed for a
// d-disjunct matrix with n items.
//
// For d-disjunct matrices, the theoretical minimum is O(d^2 * log n).
// We use a constant factor of 4 which provides good practical performance.
func calculateNumGroups(n, d int) int {
	if n <= 1 {
		return 0
	}

	// Base formula: 4 * d^2 * log2(n)
	// This provides high probability of correct identification
	logN := math.Log2(float64(n))
	numGroups := int(math.Ceil(4.0 * float64(d*d) * logN))

	// Ensure minimum number of groups
	minGroups := 2 * d
	if numGroups < minGroups {
		numGroups = minGroups
	}

	// For small n, don't exceed n (individual testing each commit)
	if numGroups > n {
		numGroups = n
	}

	return numGroups
}

// calculateColumnWeight determines how many groups each commit should appear in.
// This is the "weight" of each column in the matrix.
//
// For d-disjunct matrices, each column needs weight O(d * log n) to ensure
// no column is "covered" by any d other columns.
func calculateColumnWeight(n, d, numGroups int) int {
	if n <= 1 || numGroups <= 0 {
		return 0
	}

	// Target weight: 2 * d * log2(n)
	logN := math.Log2(float64(n))
	weight := int(math.Ceil(2.0 * float64(d) * logN))

	// Ensure minimum weight
	if weight < d+1 {
		weight = d + 1
	}

	// Don't exceed the number of groups
	if weight > numGroups {
		weight = numGroups
	}

	return weight
}

// generateRandomDisjunctMatrix generates a random matrix with the d-disjunct property.
// Returns a slice where each element is a row (test group) containing column indices.
//
// The algorithm:
// 1. For each column (commit), randomly select 'weight' rows to include it in
// 2. This creates a sparse random matrix
// 3. With high probability, this matrix is d-disjunct
//
// Parameters:
//   - n: number of columns (commits)
//   - d: maximum number of defectives (culprits)
//   - numGroups: number of rows (test groups)
//   - seed: random seed (0 for random)
func generateRandomDisjunctMatrix(n, d, numGroups int, seed int64) [][]int {
	// Initialize random source
	var rng *rand.Rand
	if seed == 0 {
		rng = rand.New(rand.NewSource(rand.Int63()))
	} else {
		rng = rand.New(rand.NewSource(seed))
	}

	// Calculate column weight
	weight := calculateColumnWeight(n, d, numGroups)

	// Initialize matrix as sets (for deduplication)
	rowSets := make([]map[int]struct{}, numGroups)
	for i := range rowSets {
		rowSets[i] = make(map[int]struct{})
	}

	// For each column, randomly assign it to 'weight' rows
	for col := 0; col < n; col++ {
		// Randomly select 'weight' distinct rows
		selectedRows := randomSample(rng, numGroups, weight)
		for _, row := range selectedRows {
			rowSets[row][col] = struct{}{}
		}
	}

	// Note: For small n, the random construction already provides good coverage.
	// Individual singleton tests are implicitly included in the random matrix
	// with high probability when column weight is high relative to n.

	// Convert sets to sorted slices
	result := make([][]int, numGroups)
	for i, rowSet := range rowSets {
		row := make([]int, 0, len(rowSet))
		for col := range rowSet {
			row = append(row, col)
		}
		sort.Ints(row)
		result[i] = row
	}

	return result
}

// randomSample returns k random distinct integers from [0, n).
func randomSample(rng *rand.Rand, n, k int) []int {
	if k >= n {
		// Return all indices
		result := make([]int, n)
		for i := 0; i < n; i++ {
			result[i] = i
		}
		return result
	}

	// Fisher-Yates-style sampling
	selected := make(map[int]struct{}, k)
	result := make([]int, 0, k)

	for len(result) < k {
		idx := rng.Intn(n)
		if _, exists := selected[idx]; !exists {
			selected[idx] = struct{}{}
			result = append(result, idx)
		}
	}

	return result
}

// verifyDisjunctProperty checks if a matrix has the d-disjunct property.
// This is primarily for testing purposes.
//
// A matrix is d-disjunct if for any column c and any set S of d other columns,
// there exists a row where c has a 1 but all columns in S have 0s.
//
// This is O(n^(d+1) * m) which is expensive for large n and d.
func verifyDisjunctProperty(matrix [][]int, n, d int) bool {
	if d <= 0 || n <= d {
		return true
	}

	// Convert to column-to-rows mapping for efficient lookup
	colToRows := make([]map[int]struct{}, n)
	for col := 0; col < n; col++ {
		colToRows[col] = make(map[int]struct{})
	}
	for rowIdx, row := range matrix {
		for _, col := range row {
			if col < n {
				colToRows[col][rowIdx] = struct{}{}
			}
		}
	}

	// For d=1, we can use a simpler check:
	// Each column must have at least one "unique" row
	// (a row where no other column appears)
	if d == 1 {
		return verifyDisjunctD1(matrix, n, colToRows)
	}

	// For d>1, full verification is expensive
	// We'll do a probabilistic check instead
	return verifyDisjunctProbabilistic(matrix, n, d, colToRows, 1000)
}

// verifyDisjunctD1 checks the 1-disjunct property.
func verifyDisjunctD1(matrix [][]int, n int, colToRows []map[int]struct{}) bool {
	// For each column, check if it has a row where it's the only member
	// (or at least, not covered by any single other column)
	for col := 0; col < n; col++ {
		hasUniqueRow := false
		for row := range colToRows[col] {
			// Check if any other column covers this row
			coveredByOther := false
			for otherCol := 0; otherCol < n; otherCol++ {
				if otherCol == col {
					continue
				}
				if _, exists := colToRows[otherCol][row]; exists {
					// Check if otherCol covers all rows of col
					covers := true
					for r := range colToRows[col] {
						if _, exists := colToRows[otherCol][r]; !exists {
							covers = false
							break
						}
					}
					if covers {
						coveredByOther = true
						break
					}
				}
			}
			if !coveredByOther {
				hasUniqueRow = true
				break
			}
		}
		if !hasUniqueRow {
			// Check if this column is NOT fully covered by any other single column
			fullyCovered := false
			for otherCol := 0; otherCol < n; otherCol++ {
				if otherCol == col {
					continue
				}
				covers := true
				for r := range colToRows[col] {
					if _, exists := colToRows[otherCol][r]; !exists {
						covers = false
						break
					}
				}
				if covers {
					fullyCovered = true
					break
				}
			}
			if fullyCovered {
				return false
			}
		}
	}
	return true
}

// verifyDisjunctProbabilistic does a probabilistic check of the d-disjunct property.
func verifyDisjunctProbabilistic(matrix [][]int, n, d int, colToRows []map[int]struct{}, numSamples int) bool {
	if n <= d {
		return true
	}

	rng := rand.New(rand.NewSource(42)) // Fixed seed for reproducibility

	for i := 0; i < numSamples; i++ {
		// Pick a random target column
		targetCol := rng.Intn(n)

		// Pick d random other columns
		otherCols := make([]int, 0, d)
		for len(otherCols) < d {
			col := rng.Intn(n)
			if col == targetCol {
				continue
			}
			duplicate := false
			for _, c := range otherCols {
				if c == col {
					duplicate = true
					break
				}
			}
			if !duplicate {
				otherCols = append(otherCols, col)
			}
		}

		// Check if target column is covered by the union of other columns
		covered := true
		for row := range colToRows[targetCol] {
			// Check if any of the other columns has this row
			inOther := false
			for _, otherCol := range otherCols {
				if _, exists := colToRows[otherCol][row]; exists {
					inOther = true
					break
				}
			}
			if !inOther {
				// Found a row where target has 1 but none of others have 1
				covered = false
				break
			}
		}

		if covered {
			// Target column is covered by the union of d other columns
			// This violates the d-disjunct property
			return false
		}
	}

	return true
}
