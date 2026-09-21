package vpat

// REV-099-04: the VPAT reflects tool runs, never claims. A ran-and-passed
// check reports Supports, a ran-and-failed check reports Does Not Support,
// and a check that never ran reports Not Evaluated.
//
// RED: no VPAT generator existed; checkpoints had no machine-readable
// conformance tied to wcag runs.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

func TestTodo_REV_099_04(t *testing.T) {
	doc := "<html><head><style>@media (prefers-reduced-motion: reduce){*{animation:none}}</style></head><body><main><button aria-label=\"close\">x</button></main></body></html>"
	motion := wcag.CheckReducedMotion(doc)
	report := Generate([]qual.CriterionResult{motion})
	var row Row
	for _, candidate := range report.Rows {
		if candidate.Check == "Reduced motion" {
			row = candidate
		}
	}
	if motion.Pass && row.Conformance != Supports {
		t.Fatalf("ran-and-passed motion reports %q, want Supports", row.Conformance)
	}
	if !motion.Pass && row.Conformance != DoesNotSupport {
		t.Fatalf("ran-and-failed motion reports %q, want Does Not Support", row.Conformance)
	}
	if row.Evidence != motion.Detail {
		t.Fatalf("row evidence = %q, want the tool detail %q", row.Evidence, motion.Detail)
	}
	for _, candidate := range report.Rows {
		if candidate.Check != "Reduced motion" && candidate.Conformance != NotEvaluated {
			t.Fatalf("unrun check %q reports %q, want Not Evaluated", candidate.Check, candidate.Conformance)
		}
	}
	mixed := Generate([]qual.CriterionResult{
		{Name: "200% zoom and 400% reflow", Pass: true, Detail: "ok"},
		{Name: "Accessible authorization projection", Pass: false, Detail: "missing label"},
		{Name: "Focus order and accessible names", Pass: true, Detail: "ok"},
	})
	summary := mixed.Summary()
	if summary[Supports] != 2 || summary[DoesNotSupport] != 1 || summary[NotEvaluated] != 1 {
		t.Fatalf("summary = %+v, want 2 Supports, 1 Does Not Support, 1 Not Evaluated", summary)
	}
}

func TestTodo_REV_099_04_Conformance(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "vpat_report.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	report := Generate([]qual.CriterionResult{
		{Name: "200% zoom and 400% reflow", Pass: true},
		{Name: "Accessible authorization projection", Pass: false},
		{Name: "Focus order and accessible names", Pass: true},
	})
	if got, want := CanonicalReport(report), strings.TrimSpace(string(raw)); got != want {
		t.Fatalf("report drifted:\n got: %q\nwant: %q", got, want)
	}
}
