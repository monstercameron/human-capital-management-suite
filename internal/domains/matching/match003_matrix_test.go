package matching

import (
	"context"
	"errors"
	"testing"
)

func mustMatch(t *testing.T, request MatchRequest, facts []CandidateFacts) MatchResult {
	t.Helper()
	port, err := NewInMemoryCandidateFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Match(context.Background(), port, request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// TestTodo_MATCH_003_Property: every hard failure excludes with its
// typed reason while soft failures stay eligible.
func TestTodo_MATCH_003_Property(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	expensive := candidateFact(t, "006", "new-york", "150.00", true)
	relocated := candidateFact(t, "007", "boston", "90.00", true)
	unqualified := candidateFact(t, "008", "new-york", "90.00", false)
	unavailable := candidateFact(t, "009", "new-york", "90.00", true)
	unavailable.Availability = nil
	result := mustMatch(t, request, []CandidateFacts{expensive, relocated, unqualified, unavailable})
	byRef := make(map[string]CandidateMatch, len(result.Matches))
	for _, match := range result.Matches {
		byRef[match.CandidateRef.Id] = match
	}
	if !byRef["00000000-0000-4000-8000-000000000006"].Eligible {
		t.Fatal("soft cost failure excluded the candidate")
	}
	for id, reason := range map[string]SatisfactionReason{
		"00000000-0000-4000-8000-000000000007": ReasonLocationMismatch,
		"00000000-0000-4000-8000-000000000008": ReasonQualificationMissing,
		"00000000-0000-4000-8000-000000000009": ReasonAvailabilityGap,
	} {
		match := byRef[id]
		if match.Eligible {
			t.Fatalf("candidate %s stayed eligible", id)
		}
		found := false
		for _, satisfaction := range match.Satisfactions {
			if satisfaction.Status == SatisfactionUnsatisfied && satisfaction.Mode == ConstraintHard && satisfaction.Reason == reason {
				found = true
			}
		}
		if !found {
			t.Fatalf("candidate %s missing hard reason %v", id, reason)
		}
	}
}

// TestTodo_MATCH_003_Security: cross-tenant facts are refused, never
// ranked.
func TestTodo_MATCH_003_Security(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	foreign := candidateFact(t, "006", "new-york", "90.00", true)
	foreign.CandidateRef.Tenant = "tenant-b"
	port, err := NewInMemoryCandidateFacts([]CandidateFacts{foreign})
	if err != nil {
		// Refusal at the boundary is the secure outcome.
		if !errors.Is(err, ErrInvalidCandidate) && !errors.Is(err, ErrCrossTenantReference) && !errors.Is(err, ErrDuplicateCandidate) {
			t.Fatalf("error = %v", err)
		}
		return
	}
	if _, err := Match(context.Background(), port, request); err == nil {
		t.Fatal("cross-tenant candidate facts ranked")
	} else if !errors.Is(err, ErrInvalidCandidate) && !errors.Is(err, ErrCrossTenantReference) {
		t.Fatalf("error = %v", err)
	}
}

// TestTodo_MATCH_003_Mutation: mode edges resolve on the documented
// side.
func TestTodo_MATCH_003_Mutation(t *testing.T) {
	// The same location failure as SOFT stays eligible and keeps rank
	// by score instead of gating it.
	soft := validMatchRequest(t)
	soft.Constraints[0].Mode = ConstraintSoft
	soft.Constraints[0].Weight = 3
	softRequest, err := NewMatchRequest(soft)
	if err != nil {
		t.Fatal(err)
	}
	result := mustMatch(t, softRequest, []CandidateFacts{
		candidateFact(t, "006", "boston", "90.00", true),
	})
	if !result.Matches[0].Eligible {
		t.Fatal("soft location failure excluded the candidate")
	}
	// Cost exactly at the ceiling satisfies, even as HARD.
	hard := validMatchRequest(t)
	hard.Constraints[2].Mode = ConstraintHard
	hardRequest, err := NewMatchRequest(hard)
	if err != nil {
		t.Fatal(err)
	}
	atCeiling := mustMatch(t, hardRequest, []CandidateFacts{
		candidateFact(t, "006", "new-york", "100.00", true),
	})
	if !atCeiling.Matches[0].Eligible {
		t.Fatal("at-ceiling hard cost excluded the candidate")
	}
}
