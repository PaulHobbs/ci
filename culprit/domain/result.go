package domain

import "time"

// TestOutcome represents the result of a single test execution.
type TestOutcome int

const (
	OutcomeUnknown TestOutcome = iota
	OutcomePass                // Test passed (no failure detected)
	OutcomeFail                // Test failed (failure detected)
	OutcomeInfra               // Infrastructure failure, not a real test result
)

func (o TestOutcome) String() string {
	switch o {
	case OutcomePass:
		return "PASS"
	case OutcomeFail:
		return "FAIL"
	case OutcomeInfra:
		return "INFRA"
	default:
		return "UNKNOWN"
	}
}

// TestGroupResult captures the outcome of testing a single group instance.
type TestGroupResult struct {
	// GroupID is the base test group ID.
	GroupID string

	// GroupIndex is the index of the base test group.
	GroupIndex int

	// Repetition is which repetition this result is for (1-based).
	Repetition int

	// Outcome is the test result.
	Outcome TestOutcome

	// Duration is how long the test took.
	Duration time.Duration

	// Logs contains any test output or error messages.
	Logs string

	// ExecutedAt is when the test was executed.
	ExecutedAt time.Time
}

// InstanceID returns a unique ID for this result's group instance.
func (r TestGroupResult) InstanceID() string {
	return r.GroupID + "-r" + string(rune('0'+r.Repetition))
}

// IsUsable returns true if this result can be used for decoding.
// Infrastructure failures are not usable.
func (r TestGroupResult) IsUsable() bool {
	return r.Outcome == OutcomePass || r.Outcome == OutcomeFail
}

// DecodingResult contains the analysis results from decoding test outcomes.
type DecodingResult struct {
	// IdentifiedCulprits are commits identified as likely culprits.
	IdentifiedCulprits []CulpritCandidate

	// Confidence is the overall confidence in the result (0-1).
	Confidence float64

	// InnocentCommits are commits definitively ruled out as culprits.
	InnocentCommits []string

	// AmbiguousCommits are commits that couldn't be classified.
	AmbiguousCommits []string

	// GroupOutcomes are the aggregated outcomes per base group after majority voting.
	GroupOutcomes []AggregatedGroupOutcome

	// DecodedAt is when the decoding was performed.
	DecodedAt time.Time
}

// CulpritCandidate represents a commit suspected to be a culprit.
type CulpritCandidate struct {
	// Commit is the suspected culprit commit.
	Commit Commit

	// Score is the log-likelihood ratio score.
	// Higher scores indicate stronger evidence of being a culprit.
	Score float64

	// Confidence is the probability of being a culprit given observations (0-1).
	Confidence float64

	// Evidence explains why this commit is suspected.
	Evidence []Evidence
}

// Evidence explains why a commit is suspected of being a culprit.
type Evidence struct {
	// GroupIndex is the index of the test group providing evidence.
	GroupIndex int

	// GroupID is the ID of the test group.
	GroupID string

	// Observation is the aggregated outcome of this group.
	Observation TestOutcome

	// Contribution is how much this observation contributes to the score.
	Contribution float64
}

// AggregatedGroupOutcome represents the majority-voted outcome for a base group.
type AggregatedGroupOutcome struct {
	// GroupIndex is the base group index.
	GroupIndex int

	// GroupID is the base group ID.
	GroupID string

	// Outcome is the aggregated outcome after majority voting.
	Outcome TestOutcome

	// PassCount is the number of passing repetitions.
	PassCount int

	// FailCount is the number of failing repetitions.
	FailCount int

	// InfraCount is the number of infrastructure failures.
	InfraCount int
}

// TotalUsable returns the total number of usable results (pass + fail).
func (a AggregatedGroupOutcome) TotalUsable() int {
	return a.PassCount + a.FailCount
}
