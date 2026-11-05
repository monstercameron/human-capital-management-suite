// TestTodo_WF_CTRL_001 proves the checked-in control-ladder decision
// record states all six normative positions and that the kernel facts it
// cites hold: no new step type, retired types retired, and the worked
// read-only agent example intact.
package modeling

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/triage"
)

const wfCtrl001Record = "testdata/wf_ctrl_001_control_ladder.md"

func wfCtrl001Body(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(wfCtrl001Record))
	if err != nil {
		t.Fatalf("read decision record: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("decision record is empty")
	}
	return string(body)
}

func TestTodo_WF_CTRL_001(t *testing.T) {
	body := wfCtrl001Body(t)

	sections := []string{
		"## 1. The ladder",
		"## 2. Where each sits in the kernel",
		"## 3. The signed scope exchange",
		"## 4. Determinism",
		"## 5. Explicit routes, no implicit default",
		"## 6. What a model-backed node may never do",
	}
	for _, section := range sections {
		if !strings.Contains(body, section) {
			t.Fatalf("record is missing section %q", section)
		}
	}
	// (1) the ladder, cheapest and most certain first.
	for _, rung := range []string{"**Rule.**", "**Decision model.**", "**Agent.**", "**Person.**", "not one rung lower"} {
		if !strings.Contains(body, rung) {
			t.Fatalf("record does not state ladder rung %q", rung)
		}
	}
	// (2) kernel placement with no new step type.
	for _, placement := range []string{"evaluator kind on `DECISION`", "agent-eligible", "`CAPABILITY`", "stay retired", "WF-STEP-012"} {
		if !strings.Contains(body, placement) {
			t.Fatalf("record does not state kernel placement %q", placement)
		}
	}
	// (3) the signed scope exchange contents.
	for _, field := range []string{"**principal**", "**data scope**", "**tools**", "**budget**", "**effect class**", "**who signed**", "published agent version's own"} {
		if !strings.Contains(body, field) {
			t.Fatalf("scope exchange does not contain %q", field)
		}
	}
	// (4) determinism: the record is the output.
	for _, field := range []string{"answer, confidence, model identity, prompt", "digest and cost", "read the record", "new attempt"} {
		if !strings.Contains(body, field) {
			t.Fatalf("determinism position does not state %q", field)
		}
	}
	// (5) explicit routes, no implicit default.
	for _, route := range []string{"abstained or below threshold", "**unavailable**", "**refused**", "**budget exhausted**", "no\nimplicit default route", "rule, a person, or a stated end"} {
		if !strings.Contains(body, route) {
			t.Fatalf("record does not require route %q", route)
		}
	}
	// (6) prohibitions.
	for _, ban := range []string{"a rule or a person has not gated", "Widen the run's access", "outside the run's access policy"} {
		if !strings.Contains(body, ban) {
			t.Fatalf("record does not prohibit %q", ban)
		}
	}

	// The kernel facts the record cites hold. Retired step types stay
	// retired: none of them validates.
	for _, retired := range []workflow.StepType{"AGENT", "RULE", "CHECKPOINT", "DOCUMENT"} {
		if retired.Valid() {
			t.Fatalf("retired step type %s validates", retired)
		}
		if _, ok := workflow.ConformanceFor(retired); ok {
			t.Fatalf("retired step type %s has conformance", retired)
		}
	}
	decision, ok := workflow.ConformanceFor(workflow.StepDecision)
	if !ok || !decision.AuthorRoutes || decision.MayCarryEffect {
		t.Fatalf("DECISION cannot host author-routed evaluators without effects: %+v", decision)
	}
	capability, ok := workflow.ConformanceFor(workflow.StepCapability)
	if !ok || !capability.RequiresCapability || !capability.MayCarryEffect {
		t.Fatalf("CAPABILITY cannot host bound agent execution: %+v", capability)
	}
	// The extension points exist: an evaluator reference on DECISION and
	// agent eligibility on the compiled capability binding.
	var compiledCapability workflow.CompiledCapability
	compiledCapability.AgentEligible = true
	if !compiledCapability.AgentEligible {
		t.Fatal("compiled capabilities carry no agent eligibility")
	}
	var compiledDecision workflow.CompiledDecision
	compiledDecision.EvaluatorRef = "evaluator/model/v1"
	if compiledDecision.EvaluatorRef == "" {
		t.Fatal("compiled decisions carry no evaluator reference")
	}
	// The worked read-only example holds: triage keeps its manual path
	// and its no-employment-decision attestation.
	if err := triage.ValidateServingContract(); err != nil {
		t.Fatalf("triage serving contract: %v", err)
	}
}

// TestTodo_WF_CTRL_001_Golden pins the decision record bytes: the record
// changes only deliberately, with this seal re-pinned by its author.
func TestTodo_WF_CTRL_001_Golden(t *testing.T) {
	body, err := os.ReadFile(filepath.Clean(wfCtrl001Record))
	if err != nil {
		t.Fatalf("read decision record: %v", err)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != "70b60cd207b235f0099463184aa613597b9f99735b3bf892f7678950ad31d1d3" {
		t.Fatalf("decision record changed: sha256:%s", got)
	}
}
