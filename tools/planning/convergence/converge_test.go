package convergence

import (
	"strings"
	"testing"
)

func TestGapIdentityIgnoresWordingAndSeparatesKeyParts(t *testing.T) {
	a := GapIdentity("owner", ContractTest, "")
	if a != GapIdentity("owner", ContractTest, "") || !strings.HasPrefix(a, "GAP-") || len(a) != len("GAP-")+16 {
		t.Fatalf("identity %q is not stable and well formed", a)
	}
	distinct := []string{
		a,
		GapIdentity("owner", ContractTest, "s"),
		GapIdentity("owner", ContractEvidence, ""),
		GapIdentity("owner2", ContractTest, ""),
		GapIdentity("ownerTEST", "", ""),
	}
	seen := map[string]bool{}
	for _, id := range distinct {
		if seen[id] {
			t.Fatalf("identity collision in %v", distinct)
		}
		seen[id] = true
	}
}

func TestProposeIsAPureFunctionOfTheGapKey(t *testing.T) {
	owner := SelectedOwner{Owner: "hcmnext.people.promote_worker/v1", Phase: " P1A "}
	p := Propose(owner, ContractHandler, "")
	if p != Propose(owner, ContractHandler, "") {
		t.Fatal("re-proposing the same gap minted a different todo")
	}
	if p.Oracle != "TestConvergence_HcmnextPeoplePromoteWorkerV1_Handler" || p.Phase != "P1A" || !strings.HasPrefix(p.ID, "CONV-") || len(p.ID) != len("CONV-")+8 {
		t.Fatalf("proposal = %+v", p)
	}
	if p.GapIdentity != GapIdentity(owner.Owner, ContractHandler, "") {
		t.Fatalf("proposal binds identity %s", p.GapIdentity)
	}
	withSubject := Propose(owner, ContractHandler, "slot:provider")
	if withSubject.ID == p.ID || !strings.HasPrefix(withSubject.Oracle, p.Oracle+"_") || !strings.Contains(withSubject.Title, "slot:provider") {
		t.Fatalf("subject did not qualify the proposal: %+v", withSubject)
	}
}

func TestCamelProducesIdentifierFragments(t *testing.T) {
	for in, want := range map[string]string{
		"hcmnext.people.promote_worker/v1": "HcmnextPeoplePromoteWorkerV1",
		"SELECT-002":                       "Select002",
		"DOWNSTREAM_CONSUMER":              "DownstreamConsumer",
		"é-x":                              "X",
		"":                                 "",
	} {
		if got := camel(in); got != want {
			t.Errorf("camel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWellFormedOwnerRefusesSeparators(t *testing.T) {
	for owner, want := range map[string]bool{"SELECT-001": true, "": true, "a b": false, "a\x00b": false, "a\tb": false} {
		if got := wellFormedOwner(owner); got != want {
			t.Errorf("wellFormedOwner(%q) = %v, want %v", owner, got, want)
		}
	}
}

func TestDedupSortsAndCollapsesUnknownsAndFindings(t *testing.T) {
	u := dedupUnknowns([]Unknown{{Code: "B", Ref: "1"}, {Code: "A", Ref: "2"}, {Code: "B", Ref: "1"}, {Code: "A", Ref: "1", Detail: "z"}, {Code: "A", Ref: "1", Detail: "a"}})
	if len(u) != 4 || u[0].Detail != "a" || u[2].Ref != "2" || u[3].Code != "B" {
		t.Fatalf("unknowns = %+v", u)
	}
	f := dedupFindings([]Finding{{Code: "B"}, {Code: "A", Ref: "2"}, {Code: "B"}, {Code: "A", Ref: "1", Detail: "z"}, {Code: "A", Ref: "1", Detail: "a"}})
	if len(f) != 4 || f[0].Detail != "a" || f[2].Ref != "2" || f[3].Code != "B" {
		t.Fatalf("findings = %+v", f)
	}
}

func TestMarshalReportIsCanonicalAndNewlineTerminated(t *testing.T) {
	r := Converge(fixtureSnapshot())
	a, err := MarshalReport(r)
	if err != nil {
		t.Fatal(err)
	}
	if a[len(a)-1] != '\n' || !strings.Contains(string(a), `"digest": "`+r.Digest+`"`) {
		t.Fatal("report bytes are not the canonical indented form")
	}
	if strings.Contains(string(a), `"owner": ""`+",\n      \"code\"") {
		t.Fatal("observation owner leaked into observation JSON")
	}
}

func TestConvergeKeepsTheFirstDuplicateSelectedOwnerAndTodo(t *testing.T) {
	snap := fixtureSnapshot()
	snap.Selected = append(snap.Selected, SelectedOwner{Owner: fxAlpha, Phase: "P9"})
	snap.Todos = append(snap.Todos, TodoRef{ID: "ALPHA-HANDLER-1", Done: true})
	r := Converge(snap)
	for _, p := range r.Proposals {
		if p.Owner == fxAlpha && p.Phase != "P1A" {
			t.Errorf("duplicate selected owner overrode the phase: %+v", p)
		}
	}
	if gapByKey(t, r, fxAlpha, ContractHandler, "").Resolution.Kind != ResolutionExistingTodo {
		t.Error("duplicate todo row overrode the first declaration")
	}
}
