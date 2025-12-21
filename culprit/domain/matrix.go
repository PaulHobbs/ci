package domain

// TestMatrix represents the group testing design matrix.
// Rows are test groups, columns are commits.
// A 1 in position (i,j) means commit j is included in test group i.
type TestMatrix struct {
	// ID is the unique identifier for this matrix.
	ID string

	// NumCommits is the number of commits (columns).
	NumCommits int

	// Groups contains all test groups (rows) without repetition expansion.
	// For the full list including repetitions, use AllGroups().
	Groups []TestGroup

	// Repetitions is the number of times each group should be tested.
	Repetitions int

	// Config is the configuration used to build this matrix.
	Config CulpritFinderConfig
}

// TestGroup represents a single test group (row in the matrix).
type TestGroup struct {
	// ID is the unique identifier for this group.
	ID string

	// Index is the row index in the base matrix (before repetition expansion).
	Index int

	// CommitIndices contains the column indices of commits in this group.
	CommitIndices []int
}

// TestGroupInstance represents a specific instance of a test group
// including which repetition it is.
type TestGroupInstance struct {
	// GroupID is the base group ID.
	GroupID string

	// GroupIndex is the base group index.
	GroupIndex int

	// Repetition is which repetition this is (1-based).
	Repetition int

	// CommitIndices contains the column indices of commits in this group.
	CommitIndices []int
}

// InstanceID returns a unique ID for this group instance.
func (i TestGroupInstance) InstanceID() string {
	return i.GroupID + "-r" + string(rune('0'+i.Repetition))
}

// NumGroups returns the number of base test groups (before repetition).
func (m *TestMatrix) NumGroups() int {
	return len(m.Groups)
}

// TotalTests returns the total number of test executions (groups × repetitions).
func (m *TestMatrix) TotalTests() int {
	return len(m.Groups) * m.Repetitions
}

// AllGroups returns all group instances including repetitions.
func (m *TestMatrix) AllGroups() []TestGroupInstance {
	instances := make([]TestGroupInstance, 0, m.TotalTests())
	for _, g := range m.Groups {
		for r := 1; r <= m.Repetitions; r++ {
			instances = append(instances, TestGroupInstance{
				GroupID:       g.ID,
				GroupIndex:    g.Index,
				Repetition:    r,
				CommitIndices: g.CommitIndices,
			})
		}
	}
	return instances
}

// CommitInGroup returns true if the commit at the given index is in the group.
func (m *TestMatrix) CommitInGroup(commitIdx, groupIdx int) bool {
	if groupIdx < 0 || groupIdx >= len(m.Groups) {
		return false
	}
	for _, idx := range m.Groups[groupIdx].CommitIndices {
		if idx == commitIdx {
			return true
		}
	}
	return false
}

// GroupsContainingCommit returns the indices of all groups containing the commit.
func (m *TestMatrix) GroupsContainingCommit(commitIdx int) []int {
	var groups []int
	for i, g := range m.Groups {
		for _, idx := range g.CommitIndices {
			if idx == commitIdx {
				groups = append(groups, i)
				break
			}
		}
	}
	return groups
}

// GetGroup returns the group at the given index.
func (m *TestMatrix) GetGroup(index int) *TestGroup {
	if index < 0 || index >= len(m.Groups) {
		return nil
	}
	return &m.Groups[index]
}

// Coverage returns the average number of groups each commit appears in.
func (m *TestMatrix) Coverage() float64 {
	if m.NumCommits == 0 {
		return 0
	}
	total := 0
	for _, g := range m.Groups {
		total += len(g.CommitIndices)
	}
	return float64(total) / float64(m.NumCommits)
}
