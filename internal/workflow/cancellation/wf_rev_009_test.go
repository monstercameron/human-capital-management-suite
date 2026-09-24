package cancellation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
)

func TestTodo_WF_REV_009(t *testing.T) {
	firstID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	secondID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	records := []cancellation.Record{
		{Outcome: cancellation.Outcome{
			DecisionID: firstID,
			Decision:   workflow.CompensationRequired,
			Evidence: workflow.CancellationOutcome{Digest: "sha256:first", Effects: []workflow.EffectDisposition{
				{ID: "write_profile#1", Disposition: "REVERTED"},
				{ID: "publish_notice#1", Disposition: "COMPENSATE:notice.retract@1"},
				{ID: "notify_payroll#1", Disposition: "IRREVERSIBLE"},
				{ID: "publish_notice#1", Disposition: "CORRECT:notice.correct@1"},
			}},
			Reasons: []cancellation.Reason{{Code: cancellation.ReasonEffectCompensation, Ref: "publish_notice#1"},
				{Code: cancellation.ReasonEffectIrreversible, Ref: "notify_payroll#1"}},
		}},
		{Outcome: cancellation.Outcome{
			DecisionID: secondID,
			Decision:   workflow.RepairRequired,
			Evidence: workflow.CancellationOutcome{Digest: "sha256:second", Effects: []workflow.EffectDisposition{
				{ID: "publish_notice#1", Disposition: "REMAINING:provider unavailable"},
			}},
			Reasons: []cancellation.Reason{{Code: cancellation.ReasonEffectCompensation, Ref: "publish_notice#1"}},
		}},
	}
	got := cancellation.ProjectSettlement(records)
	if got.BusinessState != "NOT_ACHIEVED" || got.ConsistencyState != "DEGRADED" {
		t.Fatalf("lifecycle summary = (%s, %s)", got.BusinessState, got.ConsistencyState)
	}
	if len(got.Effects) != 3 {
		t.Fatalf("effects = %d, want 3: %+v", len(got.Effects), got.Effects)
	}
	want := map[string]cancellation.EffectSettlement{
		"write_profile#1":  {EffectID: "write_profile#1", State: cancellation.SettlementReversed, DecisionID: firstID, Evidence: "sha256:first"},
		"publish_notice#1": {EffectID: "publish_notice#1", State: cancellation.SettlementUnresolved, Reason: cancellation.ReasonEffectCompensation, DecisionID: secondID, Evidence: "sha256:second"},
		"notify_payroll#1": {EffectID: "notify_payroll#1", State: cancellation.SettlementKept, Reason: cancellation.ReasonEffectIrreversible, DecisionID: firstID, Evidence: "sha256:first"},
	}
	for _, effect := range got.Effects {
		if effect != want[effect.EffectID] {
			t.Errorf("effect %s = %+v, want %+v", effect.EffectID, effect, want[effect.EffectID])
		}
	}

	// A proposed forward correction is not proof that it completed.
	proposed := cancellation.ProjectSettlement([]cancellation.Record{{Outcome: cancellation.Outcome{
		DecisionID: firstID,
		Evidence: workflow.CancellationOutcome{Digest: "sha256:proposal", Effects: []workflow.EffectDisposition{
			{ID: "write_profile#1", Disposition: "CORRECT:profile.correct@1"},
		}},
	}}})
	if len(proposed.Effects) != 1 || proposed.Effects[0].State != cancellation.SettlementUnresolved {
		t.Fatalf("proposed correction projection = %+v", proposed)
	}
}

func TestTodo_WF_REV_009_Golden(t *testing.T) {
	records := []cancellation.Record{{Outcome: cancellation.Outcome{
		DecisionID: uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		Evidence: workflow.CancellationOutcome{Digest: "sha256:golden", Effects: []workflow.EffectDisposition{
			{ID: "a#1", Disposition: "REVERTED"},
			{ID: "b#1", Disposition: "IRREVERSIBLE"},
			{ID: "c#1", Disposition: "CORRECT:correct@1"},
		}},
		Reasons: []cancellation.Reason{{Code: cancellation.ReasonEffectIrreversible, Ref: "b#1"}},
	}}}
	got, err := json.MarshalIndent(cancellation.ProjectSettlement(records), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "wf_rev_009_settlement.golden")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("settlement differs from golden %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
