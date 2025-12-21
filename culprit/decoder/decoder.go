package decoder

import (
	"fmt"
	"sort"
	"time"

	"github.com/example/turboci-lite/culprit/domain"
)

// Decoder analyzes test results to identify culprit commits.
type Decoder interface {
	// Decode analyzes the test results and identifies likely culprits.
	Decode(
		commits []domain.Commit,
		matrix *domain.TestMatrix,
		results []domain.TestGroupResult,
	) (*domain.DecodingResult, error)
}

// LikelihoodDecoder implements the Decoder interface using
// majority voting and log-likelihood scoring.
type LikelihoodDecoder struct {
	config domain.CulpritFinderConfig
	scorer *LikelihoodScorer
}

// NewDecoder creates a new LikelihoodDecoder with the given configuration.
func NewDecoder(config domain.CulpritFinderConfig) *LikelihoodDecoder {
	config = config.WithDefaults()
	return &LikelihoodDecoder{
		config: config,
		scorer: NewLikelihoodScorer(config.FalsePositiveRate, config.FalseNegativeRate),
	}
}

// Decode analyzes test results and identifies culprit commits.
func (d *LikelihoodDecoder) Decode(
	commits []domain.Commit,
	matrix *domain.TestMatrix,
	results []domain.TestGroupResult,
) (*domain.DecodingResult, error) {

	if len(results) == 0 {
		return nil, fmt.Errorf("%w: no test results to decode", domain.ErrNoResults)
	}

	// Step 1: Aggregate results by majority voting
	groupOutcomes := AggregateByMajority(results, matrix.NumGroups())

	// Step 2: Compute log-likelihood scores for each commit
	scores := d.scorer.ComputeScores(matrix, groupOutcomes)

	// Step 3: Classify commits based on scores
	var culprits []domain.CulpritCandidate
	var innocent []string
	var ambiguous []string

	for i, commit := range commits {
		score := scores[i]
		confidence := ScoreToConfidence(score)

		if confidence >= d.config.ConfidenceThreshold {
			// High confidence culprit
			evidence := d.scorer.ComputeEvidenceForCommit(i, matrix, groupOutcomes)
			culprits = append(culprits, domain.CulpritCandidate{
				Commit:     commit,
				Score:      score,
				Confidence: confidence,
				Evidence:   evidence,
			})
		} else if confidence <= 1-d.config.ConfidenceThreshold {
			// High confidence innocent
			innocent = append(innocent, commit.SHA)
		} else {
			// Ambiguous
			ambiguous = append(ambiguous, commit.SHA)
		}
	}

	// Sort culprits by confidence (highest first)
	sort.Slice(culprits, func(i, j int) bool {
		return culprits[i].Confidence > culprits[j].Confidence
	})

	// Compute overall confidence
	overallConfidence := computeOverallConfidence(culprits, groupOutcomes)

	return &domain.DecodingResult{
		IdentifiedCulprits: culprits,
		Confidence:         overallConfidence,
		InnocentCommits:    innocent,
		AmbiguousCommits:   ambiguous,
		GroupOutcomes:      groupOutcomes,
		DecodedAt:          time.Now().UTC(),
	}, nil
}

// computeOverallConfidence estimates the overall confidence in the result.
func computeOverallConfidence(
	culprits []domain.CulpritCandidate,
	outcomes []domain.AggregatedGroupOutcome,
) float64 {

	if len(culprits) == 0 {
		// No culprits identified - could be:
		// 1. No culprit exists (all tests pass)
		// 2. Too much noise to identify
		passes, fails := CountOutcomes(outcomes)
		if fails == 0 {
			// All tests pass - high confidence there's no culprit
			return 0.95
		}
		// Some tests fail but no clear culprit - low confidence
		ratio := float64(passes) / float64(passes+fails)
		return ratio * 0.5
	}

	// Average confidence of identified culprits
	totalConf := 0.0
	for _, c := range culprits {
		totalConf += c.Confidence
	}
	return totalConf / float64(len(culprits))
}

// DecodeWithDeterministicElimination provides an alternative decoding
// that uses deterministic elimination for the d-disjunct property.
//
// This is the classic group testing decode:
// - A commit is INNOCENT if it appears in ANY passing group
// - A commit is a CULPRIT if it appears ONLY in failing groups
//
// This is exact when there are no flakes, but can be affected by noise.
func DecodeWithDeterministicElimination(
	commits []domain.Commit,
	matrix *domain.TestMatrix,
	outcomes []domain.AggregatedGroupOutcome,
) *domain.DecodingResult {

	n := len(commits)
	if n == 0 {
		return &domain.DecodingResult{
			DecodedAt: time.Now().UTC(),
		}
	}

	// Track which commits appear in passing groups (definitely innocent)
	innocentByElimination := make([]bool, n)

	// Track which commits appear only in failing groups (potential culprits)
	onlyInFailing := make([]bool, n)
	for i := range onlyInFailing {
		onlyInFailing[i] = true // Start assuming all are only in failing
	}

	for _, outcome := range outcomes {
		group := matrix.GetGroup(outcome.GroupIndex)
		if group == nil {
			continue
		}

		if outcome.Outcome == domain.OutcomePass {
			// All commits in a passing group are innocent
			for _, commitIdx := range group.CommitIndices {
				if commitIdx < n {
					innocentByElimination[commitIdx] = true
					onlyInFailing[commitIdx] = false
				}
			}
		}
	}

	// Also check that "only in failing" commits actually appear in some failing group
	appearsInFailing := make([]bool, n)
	for _, outcome := range outcomes {
		if outcome.Outcome != domain.OutcomeFail {
			continue
		}
		group := matrix.GetGroup(outcome.GroupIndex)
		if group == nil {
			continue
		}
		for _, commitIdx := range group.CommitIndices {
			if commitIdx < n {
				appearsInFailing[commitIdx] = true
			}
		}
	}

	var culprits []domain.CulpritCandidate
	var innocent []string
	var ambiguous []string

	for i, commit := range commits {
		if innocentByElimination[i] {
			innocent = append(innocent, commit.SHA)
		} else if onlyInFailing[i] && appearsInFailing[i] {
			culprits = append(culprits, domain.CulpritCandidate{
				Commit:     commit,
				Score:      1.0, // Deterministic identification
				Confidence: 0.99,
			})
		} else {
			ambiguous = append(ambiguous, commit.SHA)
		}
	}

	return &domain.DecodingResult{
		IdentifiedCulprits: culprits,
		Confidence:         0.99, // Deterministic
		InnocentCommits:    innocent,
		AmbiguousCommits:   ambiguous,
		GroupOutcomes:      outcomes,
		DecodedAt:          time.Now().UTC(),
	}
}
