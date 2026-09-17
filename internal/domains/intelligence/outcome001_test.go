package intelligence

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var outcome001At = time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)

func outcome001Input() OutcomeLinkInput {
	return OutcomeLinkInput{
		Tenant: "acme", DecisionRef: "decision:promo-41", OutcomeRef: "outcome:retention-q2",
		Definition: "retention-90d", DefinitionVersion: "v2026.01",
		Window:        OutcomeWindow{Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		PopulationRef: "population:promoted-2025", SourceAuthority: "authority:hris",
		Attribution: AttributionDescriptive,
		Watermark:   "watermark:analytics-881", Confidence: "moderate: n=212, response 78%",
		Censoring: "right-censored at window end", Limitations: "observational; no control group",
	}
}

// TestTodo_OUTCOME_001 is the PRIMARY contract: links record definition,
// window, attribution, confounders, censoring, watermarks, confidence
// and limitations; corrections supersede; descriptive evidence is never
// labelled causal; and the original decision is never rewritten.
func TestTodo_OUTCOME_001(t *testing.T) {
	got, err := RecordOutcomeLink(outcome001Input(), outcome001At)
	if err != nil {
		t.Fatalf("RecordOutcomeLink: %v", err)
	}
	if got.DecisionRef != "decision:promo-41" || got.Digest == "" {
		t.Fatalf("link must bind the decision and seal: %+v", got)
	}

	t.Run("descriptive evidence cannot claim causation", func(t *testing.T) {
		in := outcome001Input()
		in.Attribution = AttributionCausalTrial
		if _, err := RecordOutcomeLink(in, outcome001At); !errors.Is(err, ErrOutcomeRejected) {
			t.Fatalf("causal claim without basis must be OUTCOME_001_REJECTED, got %v", err)
		}
		var rej *OutcomeRejection
		if _, err := RecordOutcomeLink(in, outcome001At); !errors.As(err, &rej) || rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version")
		}
		causal := outcome001Input()
		causal.Attribution = AttributionQuasiExperimental
		causal.BasisRef = "study:did-2026-03"
		causal.Confounders = []string{"tenure", "seasonality"}
		causal.Limitations = "parallel-trends assumed"
		link, err := RecordOutcomeLink(causal, outcome001At)
		if err != nil {
			t.Fatalf("supported causal claim must link: %v", err)
		}
		if link.Attribution != AttributionQuasiExperimental {
			t.Fatalf("attribution must survive: %+v", link)
		}
	})

	t.Run("missing window population or authority never validates", func(t *testing.T) {
		for name, mutate := range map[string]func(*OutcomeLinkInput){
			"window":     func(in *OutcomeLinkInput) { in.Window.End = in.Window.Start },
			"population": func(in *OutcomeLinkInput) { in.PopulationRef = "" },
			"authority":  func(in *OutcomeLinkInput) { in.SourceAuthority = "" },
		} {
			in := outcome001Input()
			mutate(&in)
			if _, err := RecordOutcomeLink(in, outcome001At); !errors.Is(err, ErrOutcomeRejected) {
				t.Fatalf("missing %s must be OUTCOME_001_REJECTED", name)
			}
		}
	})

	t.Run("correction supersedes without rewriting", func(t *testing.T) {
		original, err := RecordOutcomeLink(outcome001Input(), outcome001At)
		if err != nil {
			t.Fatal(err)
		}
		correction := outcome001Input()
		correction.OutcomeRef = "outcome:retention-q2-rev1"
		correction.Supersedes = original.Digest
		corrected, err := RecordOutcomeLink(correction, outcome001At.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if corrected.Supersedes != original.Digest || corrected.Digest == original.Digest {
			t.Fatalf("correction must supersede with a new seal: %+v", corrected)
		}
		if original.Supersedes != "" {
			t.Fatalf("original must stay untouched: %+v", original)
		}
	})
}

func TestTodo_OUTCOME_001_Golden(t *testing.T) {
	got, err := RecordOutcomeLink(outcome001Input(), outcome001At)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "outcome_001_golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("golden file not checked in yet: %v", err)
	}
	if string(want) != string(raw)+"\n" {
		t.Fatalf("golden mismatch:\n got %s\nwant %s", raw, want)
	}
}

func TestTodo_OUTCOME_001_Mutation(t *testing.T) {
	a, err := RecordOutcomeLink(outcome001Input(), outcome001At)
	if err != nil {
		t.Fatal(err)
	}
	for field, mutate := range map[string]func(*OutcomeLinkInput){
		"definition": func(in *OutcomeLinkInput) { in.DefinitionVersion = "v2026.02" },
		"window":     func(in *OutcomeLinkInput) { in.Window.End = in.Window.End.AddDate(0, 0, 1) },
		"watermark":  func(in *OutcomeLinkInput) { in.Watermark = "watermark:other" },
	} {
		in := outcome001Input()
		mutate(&in)
		b, err := RecordOutcomeLink(in, outcome001At)
		if err != nil {
			t.Fatalf("mutation %s must link: %v", field, err)
		}
		if b.Digest == a.Digest {
			t.Fatalf("mutation %s must move the digest", field)
		}
	}
	if _, err := RecordOutcomeLink(outcome001Input(), time.Time{}); !errors.Is(err, ErrOutcomeRejected) {
		t.Fatalf("missing instant must be OUTCOME_001_REJECTED")
	}
}
