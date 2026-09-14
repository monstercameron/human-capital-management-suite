package convergence

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

// TestConcreteSelectionConvergenceSecondPassProducesNoNewGapIdentity is
// CLOSE-002's PRIMARY: over the fixture's duplicated, ownerless and inert
// inputs, an unchanged second pass is byte-identical, adopting every
// proposed todo adds no gap identity and proposes nothing twice, every
// selected-scope gap resolves, and one added contract is an exact bounded
// diff.
func TestConcreteSelectionConvergenceSecondPassProducesNoNewGapIdentity(t *testing.T) {
	snap := fixtureSnapshot()
	first := Converge(snap)
	second := Converge(snap)
	if !bytes.Equal(mustMarshal(t, first), mustMarshal(t, second)) {
		t.Fatal("an unchanged second pass is not byte-identical")
	}

	// Exact register shape: 13 observations collapse to 9 identities.
	if first.Totals.Observations != 13 || len(first.Gaps) != 9 || first.Totals.Deduplicated != 4 {
		t.Fatalf("observations=%d gaps=%d deduplicated=%d, want 13/9/4", first.Totals.Observations, len(first.Gaps), first.Totals.Deduplicated)
	}
	if g := gapByKey(t, first, fxAlpha, ContractTest, ""); len(g.Observations) != 3 || strings.Join(g.Compilers, ",") != "closurewitness,designclosure,intentcoverage" {
		t.Errorf("alpha TEST gap did not merge its three differently-worded observations: %+v", g)
	}
	if g := gapByKey(t, first, fxSelectX, ContractSelectionGate, ""); len(g.Observations) != 2 {
		t.Errorf("SELECT-X binding reason and slot reason did not merge: %+v", g)
	}

	// Every selected-scope gap resolves; proposals are atomic.
	want := map[string]ResolutionKind{
		GapIdentity(fxAlpha, ContractTest, ""):                                                   ResolutionProposedTodo,
		GapIdentity(fxAlpha, ContractHandler, ""):                                                ResolutionExistingTodo,
		GapIdentity(fxBeta, ContractWorkflowDesign, ""):                                          ResolutionProposedTodo,
		GapIdentity(fxBeta, ContractScenario, ""):                                                ResolutionRejected,
		GapIdentity(fxSelectX, ContractSelectionGate, ""):                                        ResolutionProposedTodo,
		GapIdentity(fxSelectX, ContractDownstreamConsumer, "selection:"+fxSelectX):               ResolutionProposedTodo,
		GapIdentity(fxGamma, ContractEngine, ""):                                                 ResolutionNotRequired,
		GapIdentity("", ContractEndpoint, "ENDPOINT|WIRE_METHOD_UNBOUND||svc.v1.Service/Method"): ResolutionUnresolved,
		GapIdentity("", ContractTodo, "todo_direct_dangling:NEXT-9"):                             ResolutionUnresolved,
	}
	for _, g := range first.Gaps {
		if got, ok := want[g.Identity]; !ok || got != g.Resolution.Kind {
			t.Errorf("gap %s/%s/%q resolved %s, want %s", g.Owner, g.Contract, g.Subject, g.Resolution.Kind, got)
		}
	}
	if first.Totals.SelectedUnresolved != 0 {
		t.Errorf("selected unresolved = %d, want 0", first.Totals.SelectedUnresolved)
	}
	if len(first.Proposals) != 4 {
		t.Fatalf("proposals = %d, want 4: %+v", len(first.Proposals), first.Proposals)
	}
	for _, p := range first.Proposals {
		if p.Owner == "" || p.Phase == "" || !strings.HasPrefix(p.Oracle, "Test") || p.ID == "" {
			t.Errorf("proposal lacks owner/phase/oracle/id: %+v", p)
		}
	}

	// Selected-scope unknowns are surfaced, not hidden.
	if codes := unknownCodes(first); codes[UnknownOwnerless] != 2 || codes[UnknownSlotUnfilled] != 1 || first.Result != ResultUnresolved {
		t.Errorf("unknowns %v result %s: ownerless gaps and the unfilled slot must keep the gate UNRESOLVED", codes, first.Result)
	}

	// The inert selected fact is detected; consumed facts are not.
	for _, f := range first.Facts {
		if inert := f.ID == "selection:"+fxSelectX; f.Inert != inert {
			t.Errorf("fact %s inert=%v, want %v (consumers %v)", f.ID, f.Inert, inert, f.Consumers)
		}
	}

	// Adopting every proposal is a fixed point with no new identity.
	fp := RunToFixedPoint(snap, 4)
	if !fp.Stable {
		t.Fatalf("fixed point unstable: %v", fp.Reasons)
	}
	for _, pass := range fp.Passes {
		if len(pass.Delta.Added) != 0 || len(pass.Delta.Removed) != 0 {
			t.Errorf("pass %d changed identities: %+v", pass.Index, pass.Delta)
		}
	}
	if len(fp.Final.Proposals) != 0 {
		t.Errorf("adopted snapshot still proposes %d todos", len(fp.Final.Proposals))
	}
	finalByID := map[string]Gap{}
	for _, g := range fp.Final.Gaps {
		finalByID[g.Identity] = g
	}
	for _, p := range first.Proposals {
		if g := finalByID[p.GapIdentity]; g.Resolution.Kind != ResolutionExistingTodo || g.Resolution.TodoID != p.ID {
			t.Errorf("adopted proposal %s did not become the gap's existing todo: %+v", p.ID, g.Resolution)
		}
	}
	if len(identities(fp.Final)) != len(identities(first)) {
		t.Errorf("identity count moved from %d to %d", len(identities(first)), len(identities(fp.Final)))
	}

	// One added contract is exactly one added identity and nothing else.
	grown := fixtureSnapshot()
	grown.Observations = append(grown.Observations, Observation{Compiler: CompilerClosureWitness, Code: "EDGE_ABSENT", Owner: fxBeta, Contract: ContractSlice, Detail: "no product slice names this definition"})
	delta := Diff(first, Converge(grown))
	if len(delta.Added) != 1 || delta.Added[0] != GapIdentity(fxBeta, ContractSlice, "") || len(delta.Removed) != 0 || len(delta.ResolutionChanged) != 0 {
		t.Errorf("adding one contract produced delta %+v, want exactly one added identity", delta)
	}
}

// TestTodo_CLOSE_002_Property proves identity is a function of owner,
// contract and subject only: random input permutations are byte-identical,
// random rewording changes no identity or resolution, and totals always
// reconcile.
func TestTodo_CLOSE_002_Property(t *testing.T) {
	base := Converge(fixtureSnapshot())
	baseBytes := mustMarshal(t, base)
	rng := rand.New(rand.NewSource(2026_09_13))
	for trial := 0; trial < 200; trial++ {
		snap := fixtureSnapshot()
		rng.Shuffle(len(snap.Observations), func(i, j int) {
			snap.Observations[i], snap.Observations[j] = snap.Observations[j], snap.Observations[i]
		})
		rng.Shuffle(len(snap.Facts), func(i, j int) { snap.Facts[i], snap.Facts[j] = snap.Facts[j], snap.Facts[i] })
		rng.Shuffle(len(snap.Consumers), func(i, j int) { snap.Consumers[i], snap.Consumers[j] = snap.Consumers[j], snap.Consumers[i] })
		rng.Shuffle(len(snap.Selected), func(i, j int) { snap.Selected[i], snap.Selected[j] = snap.Selected[j], snap.Selected[i] })
		rng.Shuffle(len(snap.Todos), func(i, j int) { snap.Todos[i], snap.Todos[j] = snap.Todos[j], snap.Todos[i] })
		permuted := Converge(snap)
		if !bytes.Equal(baseBytes, mustMarshal(t, permuted)) {
			t.Fatalf("trial %d: input order changed the report bytes", trial)
		}

		reworded := fixtureSnapshot()
		for i := range reworded.Observations {
			if rng.Intn(2) == 0 {
				reworded.Observations[i].Detail = fmt.Sprintf("reworded %d: %s", rng.Intn(1000), strings.ToUpper(reworded.Observations[i].Detail))
				reworded.Observations[i].Code += "_V2"
			}
		}
		got := Converge(reworded)
		if d := Diff(base, got); !d.Empty() {
			t.Fatalf("trial %d: rewording moved identities or resolutions: %+v", trial, d)
		}
		assertTotalsReconcile(t, got)
	}
}

func assertTotalsReconcile(t *testing.T, r Report) {
	t.Helper()
	scoped, resolved, grouped := 0, 0, 0
	for _, n := range r.Totals.ByScope {
		scoped += n
	}
	for _, n := range r.Totals.ByResolution {
		resolved += n
	}
	for _, g := range r.Gaps {
		if len(g.Observations) == 0 {
			t.Errorf("gap %s carries no observation", g.Identity)
		}
		grouped += len(g.Observations)
	}
	if scoped != len(r.Gaps) || resolved != len(r.Gaps) || r.Totals.Deduplicated != r.Totals.Observations-len(r.Gaps) || grouped > r.Totals.Observations {
		t.Errorf("totals do not reconcile: %+v over %d gaps (%d grouped observations)", r.Totals, len(r.Gaps), grouped)
	}
	if r.Totals.Unknowns != len(r.Unknowns) || r.Totals.Findings != len(r.Findings) || r.Totals.Proposals != len(r.Proposals) {
		t.Errorf("list totals disagree with lists: %+v", r.Totals)
	}
}

// TestTodo_CLOSE_002_Golden pins the canonical fixture report bytes.
func TestTodo_CLOSE_002_Golden(t *testing.T) {
	got := mustMarshal(t, Converge(fixtureSnapshot()))
	path := filepath.Join("testdata", "golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("report bytes drifted from %s\n--- got ---\n%s", path, got)
	}
}

// TestTodo_CLOSE_002_Fault proves defective inputs become named unknowns
// or findings and never a silent drop, a panic or a false RESOLVED.
func TestTodo_CLOSE_002_Fault(t *testing.T) {
	t.Run("unmapped compiler code surfaces", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Observations = append(snap.Observations, Observation{Compiler: CompilerWorkflowMaturity, Code: "BRAND_NEW_CODE", Owner: fxAlpha, Detail: "a code no adapter maps"})
		r := Converge(snap)
		if unknownCodes(r)[UnknownUnmappedCode] != 1 {
			t.Fatalf("unmapped code was dropped: %v", r.Unknowns)
		}
	})
	t.Run("selected owner without a phase cannot be proposed", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Selected[2].Phase = " "
		r := Converge(snap)
		if findingCodes(r)[FindingProposalIncomplete] != 2 || r.Totals.SelectedUnresolved != 2 {
			t.Fatalf("phaseless proposals accepted: findings %v unresolved %d", r.Findings, r.Totals.SelectedUnresolved)
		}
		for _, p := range r.Proposals {
			if p.Owner == fxSelectX {
				t.Errorf("phaseless owner still proposed %+v", p)
			}
		}
	})
	t.Run("claim naming an absent todo does not resolve", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Claims[0].TodoID = "GHOST-1"
		r := Converge(snap)
		if findingCodes(r)[FindingClaimTodoMissing] != 1 || gapByKey(t, r, fxAlpha, ContractHandler, "").Resolution.Kind != ResolutionProposedTodo {
			t.Fatalf("absent claimant resolved the gap: %+v", r.Findings)
		}
	})
	t.Run("invalid consumer layer and duplicate fact surface", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Consumers = append(snap.Consumers, ConsumerRef{Layer: "DASHBOARD", Ref: "ui", Tokens: []string{"digest-x"}})
		snap.Facts = append(snap.Facts, snap.Facts[0])
		r := Converge(snap)
		codes := unknownCodes(r)
		if codes[UnknownInvalidConsumer] != 1 || codes[UnknownDuplicateFact] != 1 {
			t.Fatalf("unknowns %v", codes)
		}
		for _, f := range r.Facts {
			if f.ID == "selection:"+fxSelectX && !f.Inert {
				t.Error("a non-downstream layer made the inert fact consumed")
			}
		}
	})
	t.Run("fact with no tokens is inert", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Facts[3].Tokens = []string{" "}
		r := Converge(snap)
		g := gapByKey(t, r, fxSelectY, ContractDownstreamConsumer, "selection:"+fxSelectY)
		if !strings.Contains(g.Observations[0].Detail, "no token") {
			t.Errorf("tokenless fact detail %q", g.Observations[0].Detail)
		}
	})
	t.Run("empty snapshot is resolved and deterministic", func(t *testing.T) {
		a, b := Converge(Snapshot{}), Converge(Snapshot{})
		if a.Result != ResultResolved || len(a.Gaps) != 0 || a.Digest == "" || a.Digest != b.Digest {
			t.Fatalf("empty snapshot = %+v", a)
		}
	})
	t.Run("live loader refuses an unusable as-of or root", func(t *testing.T) {
		if _, err := LoadSnapshot(".", "13/09/2026"); err == nil {
			t.Error("accepted a non-ISO as-of date")
		}
		if _, err := LoadSnapshot(t.TempDir(), "2026-09-13"); err == nil || !strings.Contains(err.Error(), "P1A manifest") {
			t.Errorf("empty root = %v, want a P1A manifest error", err)
		}
	})
}

// TestTodo_CLOSE_002_Security proves the gate cannot be talked into a
// resolution: forged owners, decider-less rejections, closed or competing
// claimants, colliding proposal ids, self-consumption and edited reports
// are all refused.
func TestTodo_CLOSE_002_Security(t *testing.T) {
	t.Run("owner carrying a separator cannot forge another key", func(t *testing.T) {
		snap := fixtureSnapshot()
		forged := Observation{Compiler: CompilerDesignClosure, Code: "MISSING_TEST", Owner: fxAlpha + "\x00TEST", Contract: ContractTest, Detail: "forged"}
		snap.Observations = append(snap.Observations, forged, Observation{Compiler: "x", Code: "y", Owner: "two words", Contract: ContractTest})
		r := Converge(snap)
		if unknownCodes(r)[UnknownMalformedOwner] != 2 || len(r.Gaps) != 9 {
			t.Fatalf("malformed owners entered the register: unknowns %v gaps %d", unknownCodes(r), len(r.Gaps))
		}
	})
	t.Run("rejection without a decider is not applied", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Rejections[0].DecidedBy = ""
		r := Converge(snap)
		if gapByKey(t, r, fxBeta, ContractScenario, "").Resolution.Kind == ResolutionRejected || findingCodes(r)[FindingInvalidRejection] != 1 {
			t.Fatalf("decider-less rejection applied: %+v", r.Findings)
		}
	})
	t.Run("rejection of an unselected gap is stale, not silent", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Rejections = append(snap.Rejections, Rejection{Owner: fxGamma, Contract: ContractEngine, Rationale: "r", DecidedBy: "d"})
		if findingCodes(Converge(snap))[FindingStaleRejection] != 1 {
			t.Fatal("stale rejection not reported")
		}
	})
	t.Run("closed todo cannot own open work", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Todos[0].Done = true
		r := Converge(snap)
		if gapByKey(t, r, fxAlpha, ContractHandler, "").Resolution.Kind == ResolutionExistingTodo || findingCodes(r)[FindingClaimTodoClosed] != 1 {
			t.Fatalf("ticked todo resolved an open gap: %+v", r.Findings)
		}
	})
	t.Run("competing claimants leave the gap unresolved", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Todos = append(snap.Todos, TodoRef{ID: "ALPHA-HANDLER-2", Phase: "P1A"})
		snap.Claims = append(snap.Claims, Claim{TodoID: "ALPHA-HANDLER-2", Owner: fxAlpha, Contract: ContractHandler})
		r := Converge(snap)
		if gapByKey(t, r, fxAlpha, ContractHandler, "").Resolution.Kind != ResolutionUnresolved || findingCodes(r)[FindingAmbiguousClaim] != 1 || r.Result != ResultUnresolved {
			t.Fatal("ambiguous ownership resolved silently")
		}
	})
	t.Run("proposal id colliding with an unrelated todo is refused", func(t *testing.T) {
		snap := fixtureSnapshot()
		p := Propose(snap.Selected[0], ContractTest, "")
		snap.Todos = append(snap.Todos, TodoRef{ID: p.ID, Phase: "P1A"})
		if findingCodes(Converge(snap))[FindingProposalCollision] != 1 {
			t.Fatal("proposal reused an existing unrelated todo id")
		}
	})
	t.Run("a fact's own artifact is not its consumer", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Consumers = append(snap.Consumers, ConsumerRef{Layer: LayerTest, Ref: "definitions/planning/gates/select-x.yaml", Tokens: []string{"definitions/planning/gates/select-x.yaml"}})
		for _, f := range Converge(snap).Facts {
			if f.ID == "selection:"+fxSelectX && !f.Inert {
				t.Fatal("self-reference counted as a downstream consumer")
			}
		}
	})
	t.Run("edited reports fail verification", func(t *testing.T) {
		snap := fixtureSnapshot()
		honest := Converge(snap)
		if v := Verify(snap, honest); len(v) != 0 {
			t.Fatalf("honest report fails verification: %v", v)
		}
		hidden := honest
		hidden.Unknowns = nil
		hidden.Totals.Unknowns = 0
		hidden.Result = ResultResolved
		hidden.Digest = digestOf(hidden)
		if v := Verify(snap, hidden); !containsPrefix(v, "report carries 0 unknowns") || !containsPrefix(v, "report result RESOLVED") {
			t.Errorf("hidden unknowns verified: %v", v)
		}
		promoted := honest
		promoted.Gaps = append([]Gap(nil), honest.Gaps...)
		dropped := promoted.Gaps[len(promoted.Gaps)-1].Identity
		promoted.Gaps = promoted.Gaps[:len(promoted.Gaps)-1]
		for i := range promoted.Gaps {
			if promoted.Gaps[i].Scope == ScopeUnknown {
				promoted.Gaps[i].Resolution = Resolution{Kind: ResolutionRejected, Rationale: "trust me", DecidedBy: "nobody"}
			}
		}
		promoted.Digest = digestOf(promoted)
		v := Verify(snap, promoted)
		if !containsPrefix(v, "gap "+dropped+" was dropped") || !containsSubstring(v, "the inputs give UNKNOWN/UNRESOLVED") {
			t.Errorf("dropped or promoted gap verified: %v", v)
		}
		tampered := honest
		tampered.Totals.Gaps = 1
		if !containsPrefix(Verify(snap, tampered), "report digest does not match its own content") {
			t.Error("self-digest mismatch not caught")
		}
	})
}

func containsPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func containsSubstring(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// TestTodo_CLOSE_002_Conformance runs the gate over the live repository:
// two independent loads are byte-identical, the fixed point holds, every
// selected intent and bound artifact is a fact, every closure-witness defect
// and every unready selection binding for the selection is carried into a
// selected gap, every unfilled slot is surfaced, and no selected gap is left
// without a resolution or a finding naming it.
func TestTodo_CLOSE_002_Conformance(t *testing.T) {
	root := repoRoot(t)
	const asOf = "2026-09-13"
	snap, err := LoadSnapshot(root, asOf)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	report := Converge(snap)
	again, err := LoadSnapshot(root, asOf)
	if err != nil {
		t.Fatalf("second LoadSnapshot: %v", err)
	}
	if !bytes.Equal(mustMarshal(t, report), mustMarshal(t, Converge(again))) {
		d := Diff(report, Converge(again))
		t.Fatalf("two unchanged live passes differ: %+v", d)
	}
	fp := RunToFixedPoint(snap, 4)
	if !fp.Stable {
		t.Fatalf("live fixed point unstable: %v", fp.Reasons)
	}
	assertTotalsReconcile(t, report)
	if v := Verify(snap, report); len(v) != 0 {
		t.Fatalf("live report fails verification: %v", v)
	}

	manifest, err := gateevidence.LoadP1AManifest(filepath.Join(root, filepath.FromSlash(gateevidence.P1AManifestPath)))
	if err != nil {
		t.Fatal(err)
	}
	facts := map[string]FactResult{}
	for _, f := range report.Facts {
		facts[f.ID] = f
	}
	selected := map[string]bool{}
	for _, it := range manifest.Intents {
		if it.Disposition == "INCLUDED" {
			selected[it.ID] = true
			if _, ok := facts["intent:"+it.ID]; !ok {
				t.Errorf("selected intent %s is not a fact", it.ID)
			}
		}
	}
	for _, b := range manifest.SelectionBindings {
		f, ok := facts["selection:"+b.TodoID]
		if !ok {
			t.Errorf("bound artifact %s is not a fact", b.TodoID)
			continue
		}
		if f.Inert {
			gapByKey(t, report, b.TodoID, ContractDownstreamConsumer, f.ID)
		}
	}
	if len(selected) != 8 || len(report.Facts) != 8+len(manifest.SelectionBindings) {
		t.Errorf("facts = %d over %d selected intents and %d bindings", len(report.Facts), len(selected), len(manifest.SelectionBindings))
	}

	ws, err := closurewitness.LoadSnapshot(root, asOf)
	if err != nil {
		t.Fatal(err)
	}
	witnesses, err := closurewitness.Compile(ws)
	if err != nil {
		t.Fatal(err)
	}
	carriedDefects := 0
	for _, w := range witnesses.Witnesses {
		if !selected[w.Definition] {
			continue
		}
		for _, d := range w.Defects {
			g := gapByKey(t, report, w.Definition, closureClassContract[d.Class], "")
			if g.Scope != ScopeSelected || !hasObservation(g, CompilerClosureWitness, d.Code, d.Detail) {
				t.Errorf("witness defect %s %s on %s is not carried into a selected gap", d.Code, d.Identity, w.Definition)
			}
			carriedDefects++
		}
	}
	if carriedDefects == 0 {
		t.Fatal("precondition: live witnesses carry no defect for the selection, so the carry check is vacuous")
	}

	named := map[string]bool{}
	for _, f := range report.Findings {
		named[f.Ref] = true
	}
	for _, g := range report.Gaps {
		if g.Scope == ScopeSelected && g.Resolution.Kind == ResolutionUnresolved && !named[g.Identity] {
			t.Errorf("selected gap %s is unresolved without a finding naming it", g.Identity)
		}
		if g.Scope == ScopeUnknown && !hasUnknownRef(report, g.Identity) {
			t.Errorf("unknown-scope gap %s is not surfaced as an unknown", g.Identity)
		}
	}
	slots := 0
	for _, u := range report.Unknowns {
		if u.Code == UnknownSlotUnfilled {
			slots++
		}
	}
	t.Logf("live convergence %s: observations=%d gaps=%d deduplicated=%d scope=%v resolution=%v facts=%d inert=%d proposals=%d unknowns=%d (unfilled slots %d) findings=%d passes=%d",
		report.Result, report.Totals.Observations, report.Totals.Gaps, report.Totals.Deduplicated, report.Totals.ByScope, report.Totals.ByResolution,
		report.Totals.Facts, report.Totals.InertFacts, report.Totals.Proposals, report.Totals.Unknowns, slots, report.Totals.Findings, len(fp.Passes))
}

func hasObservation(g Gap, compiler, code, detail string) bool {
	for _, o := range g.Observations {
		if o.Compiler == compiler && o.Code == code && o.Detail == detail {
			return true
		}
	}
	return false
}

func hasUnknownRef(r Report, ref string) bool {
	for _, u := range r.Unknowns {
		if u.Ref == ref {
			return true
		}
	}
	return false
}

// TestTodo_CLOSE_002_Mutation proves each input the gate reads is
// load-bearing: removing a consumer makes a fact inert with exactly one new
// identity, removing a claim flips exactly one resolution, renaming an
// owner moves identity, and an extra observation of an existing key moves
// the digest but no identity.
func TestTodo_CLOSE_002_Mutation(t *testing.T) {
	base := Converge(fixtureSnapshot())
	t.Run("dropping the only consumer makes the fact inert", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Consumers = snap.Consumers[:2] // drop the threat register consumer of SELECT-Y
		r := Converge(snap)
		d := Diff(base, r)
		if len(d.Added) != 1 || d.Added[0] != GapIdentity(fxSelectY, ContractDownstreamConsumer, "selection:"+fxSelectY) || r.Totals.InertFacts != 2 {
			t.Fatalf("delta %+v inert %d", d, r.Totals.InertFacts)
		}
	})
	t.Run("dropping a claim flips exactly one resolution", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Claims = nil
		d := Diff(base, Converge(snap))
		if len(d.Added) != 0 || len(d.ResolutionChanged) != 1 || d.ResolutionChanged[0] != GapIdentity(fxAlpha, ContractHandler, "") {
			t.Fatalf("delta %+v", d)
		}
	})
	t.Run("dropping the rejection reopens the gap as a proposal", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Rejections = nil
		if g := gapByKey(t, Converge(snap), fxBeta, ContractScenario, ""); g.Resolution.Kind != ResolutionProposedTodo {
			t.Fatalf("resolution %s", g.Resolution.Kind)
		}
	})
	t.Run("renaming an owner moves identity", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Observations[3].Owner = fxBeta
		d := Diff(base, Converge(snap))
		if len(d.Added) != 1 || len(d.Removed) != 1 {
			t.Fatalf("delta %+v", d)
		}
	})
	t.Run("deselecting an owner moves its gaps out of scope", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Selected = snap.Selected[1:]
		snap.KnownOwners = append(snap.KnownOwners, fxAlpha)
		r := Converge(snap)
		if g := gapByKey(t, r, fxAlpha, ContractTest, ""); g.Scope != ScopeOutOfSelection {
			t.Fatalf("scope %s", g.Scope)
		}
	})
	t.Run("an extra observation of an existing key adds no identity", func(t *testing.T) {
		snap := fixtureSnapshot()
		snap.Observations = append(snap.Observations, Observation{Compiler: CompilerWorkflowMaturity, Code: "DANGLING_TEST_EDGE", Owner: fxAlpha, Contract: ContractTest, Detail: "TEST MATRIX names a test that does not exist"})
		r := Converge(snap)
		if d := Diff(base, r); !d.Empty() || r.Digest == base.Digest {
			t.Fatalf("delta %+v, digest moved=%v", d, r.Digest != base.Digest)
		}
	})
}
