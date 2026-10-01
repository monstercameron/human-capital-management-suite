package governance

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/pilot"
)

func TestTodo_PILOT_001_Serving(t *testing.T) {
	observed := fixedNow.Add(-time.Hour).UnixMilli()
	evidence := make([]pilot.Evidence, 0, 7)
	for _, criterion := range []string{
		pilot.CriterionSafety,
		pilot.CriterionCorrectness,
		pilot.CriterionSLO,
		pilot.CriterionAdoption,
		pilot.CriterionValue,
		pilot.CriterionCost,
		pilot.CriterionSupport,
	} {
		evidence = append(evidence, pilot.Evidence{
			Criterion: criterion, Status: pilot.EvidencePass,
			Numerator: 9, Denominator: 10,
			Digest: "sha256:" + criterion, Outcome: criterion + " outcome measured",
			ObservedUnixMilli: observed, MaxAgeMillis: 24 * time.Hour.Milliseconds(),
		})
	}

	result := Compose(baseReq())
	decision, err := result.PilotRuntime().Review(pilot.ReviewInput{
		PilotRef: "pilot-serving-test", Evidence: evidence,
		NowUnixMilli: fixedNow.UnixMilli(), RiskTier: pilot.RiskMedium,
	})
	if err != nil {
		t.Fatalf("serving pilot review: %v", err)
	}
	if decision.Decision != pilot.DecisionGo {
		t.Fatalf("serving pilot review decision = %s, want %s", decision.Decision, pilot.DecisionGo)
	}
	if decision.Digest == "" || len(decision.Metrics) != len(evidence) {
		t.Fatalf("serving pilot review omitted evidence digest or metrics: %+v", decision)
	}
}
