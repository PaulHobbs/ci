package decoder

import (
	"github.com/example/turboci-lite/culprit/domain"
)

// AggregateByMajority aggregates test results by group using majority voting.
// For each base group, it counts pass/fail outcomes across repetitions
// and returns the majority outcome.
//
// If there's a tie, FAIL wins (conservative approach for culprit finding).
// Infrastructure failures are excluded from the vote.
func AggregateByMajority(results []domain.TestGroupResult, numGroups int) []domain.AggregatedGroupOutcome {
	// Initialize aggregated outcomes
	outcomes := make([]domain.AggregatedGroupOutcome, numGroups)
	for i := range outcomes {
		outcomes[i] = domain.AggregatedGroupOutcome{
			GroupIndex: i,
			GroupID:    "", // Will be set from results
			Outcome:    domain.OutcomeUnknown,
		}
	}

	// Count outcomes per group
	for _, r := range results {
		if r.GroupIndex < 0 || r.GroupIndex >= numGroups {
			continue
		}

		agg := &outcomes[r.GroupIndex]
		if agg.GroupID == "" {
			agg.GroupID = r.GroupID
		}

		switch r.Outcome {
		case domain.OutcomePass:
			agg.PassCount++
		case domain.OutcomeFail:
			agg.FailCount++
		case domain.OutcomeInfra:
			agg.InfraCount++
		}
	}

	// Determine majority outcome for each group
	for i := range outcomes {
		agg := &outcomes[i]
		if agg.PassCount == 0 && agg.FailCount == 0 {
			// No usable results
			if agg.InfraCount > 0 {
				agg.Outcome = domain.OutcomeInfra
			} else {
				agg.Outcome = domain.OutcomeUnknown
			}
		} else if agg.FailCount >= agg.PassCount {
			// Fail wins ties (conservative)
			agg.Outcome = domain.OutcomeFail
		} else {
			agg.Outcome = domain.OutcomePass
		}
	}

	return outcomes
}

// FilterUsableOutcomes returns only outcomes that can be used for decoding.
// This excludes groups with Unknown or Infra outcomes.
func FilterUsableOutcomes(outcomes []domain.AggregatedGroupOutcome) []domain.AggregatedGroupOutcome {
	usable := make([]domain.AggregatedGroupOutcome, 0, len(outcomes))
	for _, o := range outcomes {
		if o.Outcome == domain.OutcomePass || o.Outcome == domain.OutcomeFail {
			usable = append(usable, o)
		}
	}
	return usable
}

// CountOutcomes counts the number of pass and fail outcomes.
func CountOutcomes(outcomes []domain.AggregatedGroupOutcome) (passes, fails int) {
	for _, o := range outcomes {
		switch o.Outcome {
		case domain.OutcomePass:
			passes++
		case domain.OutcomeFail:
			fails++
		}
	}
	return
}
