package lineageconformance

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
)

func compileFixture(source closurewitness.EdgeState) Input {
	def := intent.Definition{Ref: intent.Ref{TypeID: "x.read", Version: 1}, SideEffect: intent.SideEffectReadOnly,
		ProposalBindingRule: "NOT_APPLICABLE", CompensationRule: "NOT_APPLICABLE", AllowedInitiators: []intent.Initiator{intent.InitiatorHuman}}
	return Input{
		AsOf: "2026-09-13",
		Witnesses: closurewitness.Report{Digest: "wr", Witnesses: []closurewitness.Witness{{
			Definition: "x.read/v1", Digest: "w", Classes: []closurewitness.ClassStatus{{Class: closurewitness.ClassSource, State: source}},
		}}},
		Definitions: []intent.Definition{def},
	}
}

func TestCompileProvesIntentOnlyFromBoundSource(t *testing.T) {
	rep, err := Compile(compileFixture(closurewitness.StateBound))
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Cases[0]
	if c.Links[0].Link != LinkIntent || c.Links[0].State != StateProven || c.Status != StatusUnknown || rep.Complete {
		t.Fatalf("bound source case = %+v", c)
	}
	rep, _ = Compile(compileFixture(closurewitness.StateAbsent))
	if rep.Cases[0].Links[0].State != StateUnknown {
		t.Fatalf("absent source proved INTENT: %+v", rep.Cases[0].Links[0])
	}
	if err := VerifyReport(rep); err != nil {
		t.Fatalf("honest report does not verify: %v", err)
	}
}

func TestVerifyReportRefusesEvidenceFreeProofAndCountDrift(t *testing.T) {
	rep, _ := Compile(compileFixture(closurewitness.StateBound))
	forged := rep
	forged.Cases = append([]CaseResult(nil), rep.Cases...)
	links := append([]LinkStatus(nil), forged.Cases[0].Links...)
	for i := range links {
		links[i].State = StateProven
	}
	forged.Cases[0].Links = links
	forged.Cases[0].Findings = nil
	forged.Cases[0].Status = StatusComplete
	if err := VerifyReport(SealDigest(forged)); !errors.Is(err, ErrFalseCompletion) || !strings.Contains(err.Error(), "no evidence") {
		t.Fatalf("evidence-free proof = %v", err)
	}
	drift := rep
	drift.Counts = map[Status]int{StatusComplete: 1}
	drift.ByPath = map[PathKind]map[Status]int{PathRoot: {StatusUnknown: 0}}
	if err := VerifyReport(SealDigest(drift)); !errors.Is(err, ErrFalseCompletion) {
		t.Fatalf("count drift = %v", err)
	}
}

func TestMarshalAndSummaryRenderEveryCase(t *testing.T) {
	rep, _ := Compile(compileFixture(closurewitness.StateBound))
	rep.Findings = []Finding{{Case: "x.orphan", Code: CodeProducerOrphan, Detail: "orphan"}}
	raw, err := MarshalReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(raw, &back); err != nil || back.Cases[0].Case.ID != "x.read/v1" || !strings.HasSuffix(string(raw), "}\n") {
		t.Fatalf("round trip = %v %+v", err, back)
	}
	s := Summary(rep)
	for _, want := range []string{"complete=false", "UNKNOWN=1", "ROOT", "x.read/v1 [WORKFLOW=UNKNOWN EVENT=UNKNOWN PROJECTION=UNKNOWN]", "finding PRODUCER_ORPHAN x.orphan"} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary lacks %q:\n%s", want, s)
		}
	}
}
