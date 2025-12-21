package domain

// Commit represents a single commit to test in culprit finding.
type Commit struct {
	// SHA is the unique identifier for the commit (git SHA or CL ID).
	SHA string

	// Index is the position in the commit range (0 = oldest).
	Index int

	// Subject is the commit message subject line.
	Subject string

	// Author is the commit author.
	Author string
}

// CommitRange represents a range of commits to search for culprits.
type CommitRange struct {
	// Repository is the repository identifier.
	Repository string

	// BaseRef is the reference to the known-good state (exclusive).
	// All commits in the range will be cherry-picked onto this base.
	BaseRef string

	// Commits is the ordered list of commits to search (oldest to newest).
	Commits []Commit
}

// Len returns the number of commits in the range.
func (r *CommitRange) Len() int {
	return len(r.Commits)
}

// GetCommit returns the commit at the given index.
func (r *CommitRange) GetCommit(index int) *Commit {
	if index < 0 || index >= len(r.Commits) {
		return nil
	}
	return &r.Commits[index]
}

// GetCommitBySHA returns the commit with the given SHA.
func (r *CommitRange) GetCommitBySHA(sha string) *Commit {
	for i := range r.Commits {
		if r.Commits[i].SHA == sha {
			return &r.Commits[i]
		}
	}
	return nil
}

// SelectCommits returns a subset of commits by their indices.
func (r *CommitRange) SelectCommits(indices []int) []Commit {
	result := make([]Commit, 0, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(r.Commits) {
			result = append(result, r.Commits[idx])
		}
	}
	return result
}
