package decoder

import (
	"math"

	"github.com/example/turboci-lite/culprit/domain"
)

// LikelihoodScorer computes log-likelihood ratio scores for each commit
// based on observed test outcomes.
//
// The score for commit c is:
//
//	score(c) = sum over groups g containing c of:
//	           log(P(observation | c is culprit) / P(observation | c is innocent))
//
// Higher scores indicate stronger evidence that a commit is a culprit.
type LikelihoodScorer struct {
	// FalsePositiveRate is the probability of a pass when there should be a fail.
	// (A culprit is present but the test passes anyway due to flakiness)
	FalsePositiveRate float64

	// FalseNegativeRate is the probability of a fail when there should be a pass.
	// (No culprit but the test fails anyway due to flakiness)
	FalseNegativeRate float64
}

// NewLikelihoodScorer creates a new scorer with the given flake rates.
func NewLikelihoodScorer(fpRate, fnRate float64) *LikelihoodScorer {
	// Clamp rates to avoid log(0) issues
	if fpRate <= 0 {
		fpRate = 0.001
	}
	if fpRate >= 1 {
		fpRate = 0.999
	}
	if fnRate <= 0 {
		fnRate = 0.001
	}
	if fnRate >= 1 {
		fnRate = 0.999
	}

	return &LikelihoodScorer{
		FalsePositiveRate: fpRate,
		FalseNegativeRate: fnRate,
	}
}

// ComputeScores calculates log-likelihood ratio scores for each commit.
//
// For each group g that contains commit c:
//   - If g FAILED:
//     P(fail | c culprit) = 1 - FalsePositiveRate
//     P(fail | c innocent) = FalseNegativeRate (if no other culprit in g) or ~1 (if other culprit)
//     Since we don't know other culprits, we use a prior-weighted estimate
//   - If g PASSED:
//     P(pass | c culprit) = FalsePositiveRate
//     P(pass | c innocent) = 1 - FalseNegativeRate
//
// For a first-pass approximation, we assume commits are independent and
// use a simplified model where we don't condition on other commits.
func (s *LikelihoodScorer) ComputeScores(
	matrix *domain.TestMatrix,
	outcomes []domain.AggregatedGroupOutcome,
) []float64 {

	n := matrix.NumCommits
	scores := make([]float64, n)

	// Pre-compute log probability ratios
	// log(P(fail|culprit) / P(fail|innocent))
	logFailRatioCulprit := math.Log((1 - s.FalsePositiveRate) / s.FalseNegativeRate)

	// log(P(pass|culprit) / P(pass|innocent))
	logPassRatioCulprit := math.Log(s.FalsePositiveRate / (1 - s.FalseNegativeRate))

	// For each commit, sum contributions from groups containing it
	for commitIdx := 0; commitIdx < n; commitIdx++ {
		score := 0.0

		for _, outcome := range outcomes {
			groupIdx := outcome.GroupIndex
			if groupIdx < 0 || groupIdx >= matrix.NumGroups() {
				continue
			}

			// Check if this commit is in this group
			if !matrix.CommitInGroup(commitIdx, groupIdx) {
				continue
			}

			switch outcome.Outcome {
			case domain.OutcomeFail:
				score += logFailRatioCulprit
			case domain.OutcomePass:
				score += logPassRatioCulprit
			}
		}

		scores[commitIdx] = score
	}

	return scores
}

// ScoreToConfidence converts a log-likelihood score to a confidence value (0-1).
// Uses the sigmoid function: confidence = 1 / (1 + exp(-score))
func ScoreToConfidence(score float64) float64 {
	// Clamp extreme values to avoid overflow
	if score > 20 {
		return 0.9999999
	}
	if score < -20 {
		return 0.0000001
	}
	return 1.0 / (1.0 + math.Exp(-score))
}

// ComputeEvidenceForCommit computes detailed evidence for a specific commit.
func (s *LikelihoodScorer) ComputeEvidenceForCommit(
	commitIdx int,
	matrix *domain.TestMatrix,
	outcomes []domain.AggregatedGroupOutcome,
) []domain.Evidence {

	var evidence []domain.Evidence

	logFailRatioCulprit := math.Log((1 - s.FalsePositiveRate) / s.FalseNegativeRate)
	logPassRatioCulprit := math.Log(s.FalsePositiveRate / (1 - s.FalseNegativeRate))

	for _, outcome := range outcomes {
		groupIdx := outcome.GroupIndex
		if !matrix.CommitInGroup(commitIdx, groupIdx) {
			continue
		}

		var contribution float64
		switch outcome.Outcome {
		case domain.OutcomeFail:
			contribution = logFailRatioCulprit
		case domain.OutcomePass:
			contribution = logPassRatioCulprit
		default:
			continue
		}

		evidence = append(evidence, domain.Evidence{
			GroupIndex:   groupIdx,
			GroupID:      outcome.GroupID,
			Observation:  outcome.Outcome,
			Contribution: contribution,
		})
	}

	return evidence
}
