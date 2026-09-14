package lineageconformance_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
	lc "github.com/monstercameron/human-capital-management-suite/tools/planning/lineageconformance"
)

// TestEveryAcceptedIntentFamilyHasCompleteAuthorizedRebuildableLineage is
// DATA-022's PRIMARY. Against the live registries it generates every ROOT,
// CHILD and TRIGGER case from the closure witnesses, proves the live report
// never claims completion it cannot back, and reports per-family counts.
// Against generated fixtures it proves the shared assertions accept a
// complete, authorized, redaction-safe, rebuildable lineage for every one
// of those same cases, including the Promotion chain built on DATA-015.
func TestEveryAcceptedIntentFamilyHasCompleteAuthorizedRebuildableLineage(t *testing.T) {
	in, err := lc.LoadInput(repoRoot(), asOf)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := lc.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.VerifyReport(rep); err != nil {
		t.Fatalf("live report does not verify: %v", err)
	}
	cases, _ := lc.GenerateCases(lc.CaseInput{Witnesses: in.Witnesses, Definitions: in.Definitions, Bindings: in.Bindings})
	if len(rep.Cases) != len(cases) || rep.ByPath[lc.PathRoot] == nil {
		t.Fatalf("report holds %d cases, generation holds %d", len(rep.Cases), len(cases))
	}
	roots := 0
	for _, c := range rep.Cases {
		if c.Case.Path == lc.PathRoot {
			roots++
		}
		for _, l := range c.Links {
			if l.State == lc.StateProven {
				continue
			}
			if !slices.ContainsFunc(c.Findings, func(f lc.Finding) bool { return f.Link == l.Link }) {
				t.Fatalf("%s link %s is %s with no finding naming it", c.Case.ID, l.Link, l.State)
			}
		}
		if c.Status == lc.StatusComplete && slices.ContainsFunc(c.Links, func(l lc.LinkStatus) bool { return l.State != lc.StateProven }) {
			t.Fatalf("%s is COMPLETE over an unproven link", c.Case.ID)
		}
	}
	if roots != len(in.Witnesses.Witnesses) {
		t.Fatalf("%d ROOT cases for %d witnesses", roots, len(in.Witnesses.Witnesses))
	}
	if rep.Complete != (rep.Counts[lc.StatusComplete] == len(rep.Cases)) {
		t.Fatalf("aggregate complete=%t disagrees with counts %v", rep.Complete, rep.Counts)
	}
	promotion := caseResult(t, rep, lc.PromotionDefinition)
	if promotion.Status == lc.StatusUnknown || promotion.Status == lc.StatusDefective {
		t.Fatalf("Promotion lineage = %s, want the DATA-015/LEDGER-013 links proven", promotion.Status)
	}
	t.Logf("live per-family lineage: %v by path %v\n%s", rep.Counts, rep.ByPath, lc.Summary(rep))

	// Every generated case accepts a complete, authorized lineage, and a
	// reader without clearance sees the same verdict over a redacted view.
	var results []lc.CaseResult
	for _, c := range cases {
		g := completeGraph(t, cases, c, tenantA)
		res := lc.Evaluate(c, g, authorized(tenantA))
		if res.Status != lc.StatusComplete {
			t.Fatalf("complete fixture for %s = %s: %+v", c.ID, res.Status, res.Findings)
		}
		view := lc.View(g, uncleared(tenantA))
		if got := lc.Evaluate(c, view, uncleared(tenantA)); got.Status != lc.StatusComplete {
			t.Fatalf("redacted view of %s = %s: %+v", c.ID, got.Status, got.Findings)
		}
		for _, rec := range view.Records {
			if rec.Classification != "" && (rec.Payload != "" || rec.Reason == "") {
				t.Fatalf("view discloses or leaves unexplained %s", rec.ID)
			}
		}
		results = append(results, res)
	}
	fixtureReport := lc.Assemble(asOf, in.Witnesses.Digest, results, nil)
	if !fixtureReport.Complete || lc.VerifyReport(fixtureReport) != nil {
		t.Fatalf("complete fixtures do not aggregate to a verified complete report: %v", lc.VerifyReport(fixtureReport))
	}

	promotionCase := caseByID(t, cases, lc.PromotionDefinition)
	if res := lc.Evaluate(promotionCase, promotionGraph(t, tenantA), authorized(tenantA)); res.Status != lc.StatusComplete {
		t.Fatalf("Promotion chain on DATA-015 = %s: %+v", res.Status, res.Findings)
	}
}

func caseResult(t *testing.T, rep lc.Report, id string) lc.CaseResult {
	t.Helper()
	for _, c := range rep.Cases {
		if c.Case.ID == id {
			return c
		}
	}
	t.Fatalf("report has no case %s", id)
	return lc.CaseResult{}
}

// TestTodo_DATA_022_Property drops every non-empty random subset of links
// from every generated case: the verdict is never COMPLETE, every dropped
// link is named, and redaction never changes a record's seal.
func TestTodo_DATA_022_Property(t *testing.T) {
	cases, _ := lc.GenerateCases(catalogInput())
	rng := rand.New(rand.NewSource(22))
	for _, c := range cases {
		base := completeGraph(t, cases, c, tenantA)
		for trial := 0; trial < 12; trial++ {
			g := base
			var dropped []lc.Link
			for _, l := range c.Required {
				if rng.Intn(3) == 0 {
					dropped = append(dropped, l)
					g = dropLink(g, c.ID, l)
				}
			}
			if len(dropped) == 0 {
				dropped = append(dropped, c.Required[rng.Intn(len(c.Required))])
				g = dropLink(g, c.ID, dropped[0])
			}
			res := lc.Evaluate(c, g, authorized(tenantA))
			if res.Status == lc.StatusComplete {
				t.Fatalf("%s without %v reports COMPLETE", c.ID, dropped)
			}
			for _, l := range dropped {
				if !hasFinding(res, lc.CodeLinkMissing, l) || linkState(res, l) != lc.StateUnknown {
					t.Fatalf("%s dropped %s but it is not named UNKNOWN: %+v", c.ID, l, res.Findings)
				}
			}
			if lc.VerifyReport(lc.Assemble(asOf, "", []lc.CaseResult{res}, nil)) != nil {
				t.Fatalf("honest partial report for %s does not verify", c.ID)
			}
		}
		view := lc.View(base, uncleared(tenantA))
		if lc.GraphDigest(view) != lc.GraphDigest(base) {
			t.Fatalf("redaction changed the seals of %s", c.ID)
		}
	}
}

// goldenDefinitions is a hand-built two-definition catalog so the golden
// bytes depend on this package alone: a mutating change request with a
// composite child and a schedule trigger, and a read-only explanation.
func goldenDefinitions() ([]intent.Definition, []intent.Binding) {
	change := intent.Definition{
		Ref: intent.Ref{TypeID: "fixture.people.change_thing", Version: 1}, DisplayName: "ChangeThing",
		Family: intent.FamilyChangeRequest, SideEffect: intent.SideEffectInternalMutation,
		ProposalBindingRule: "exact_proposal_digest/v1", CompensationRule: "repair_plan/v1",
		AllowedInitiators: []intent.Initiator{intent.InitiatorHuman, intent.InitiatorSchedule},
	}
	explain := intent.Definition{
		Ref: intent.Ref{TypeID: "fixture.people.explain_thing", Version: 1}, DisplayName: "ExplainThing",
		Family: intent.FamilyAnalyticalRequest, SideEffect: intent.SideEffectReadOnly,
		ProposalBindingRule: "NOT_APPLICABLE", CompensationRule: "NOT_APPLICABLE",
		AllowedInitiators: []intent.Initiator{intent.InitiatorHuman},
	}
	binding := intent.Binding{Definition: change.Ref, ChildDefinitions: []intent.Ref{explain.Ref}}
	return []intent.Definition{change, explain}, []intent.Binding{binding}
}

func goldenInput() lc.Input {
	defs, bindings := goldenDefinitions()
	return lc.Input{
		AsOf: asOf, Witnesses: witnessReport(defs), Definitions: defs, Bindings: bindings,
		Producers: []lc.Producer{{
			ID: "FIX-001/store", Todo: "FIX-001", Package: "internal/fixture", Case: "fixture.people.change_thing/v1",
			Links: []lc.Link{lc.LinkProposal, lc.LinkEvent}, Tests: []string{"TestFixtureLineage"},
		}},
		Todos:      []closurewitness.TodoRow{{ID: "FIX-001", Done: true, PrimaryTest: "TestFixtureLineage"}},
		TestExists: map[string]bool{"TestFixtureLineage": true},
	}
}

// goldenReportSHA pins the exact canonical report bytes for goldenInput.
const goldenReportSHA = "7d7d838eb2a91869e423bc0b96421a9a2753c42bfd5795099b24e59fa2ac44ba"

func TestTodo_DATA_022_Golden(t *testing.T) {
	rep, err := lc.Compile(goldenInput())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := lc.MarshalReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(raw)
	if got := hex.EncodeToString(h[:]); got != goldenReportSHA {
		t.Fatalf("golden report bytes changed: sha256 %s\n%s", got, raw)
	}
	again, _ := lc.Compile(goldenInput())
	if again.Digest != rep.Digest {
		t.Fatal("two compilations over one input differ")
	}
	wantIDs := []string{
		"fixture.people.change_thing/v1",
		"fixture.people.change_thing/v1>fixture.people.explain_thing/v1",
		"fixture.people.change_thing/v1@SCHEDULE",
		"fixture.people.explain_thing/v1",
	}
	var gotIDs []string
	for _, c := range rep.Cases {
		gotIDs = append(gotIDs, c.Case.ID)
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("golden cases = %v, want %v", gotIDs, wantIDs)
	}
}

// TestTodo_DATA_022_Fault feeds every generation and producer fault: each
// becomes UNKNOWN or PARTIAL naming the gap, never COMPLETE, and hard input
// faults are refused with an error.
func TestTodo_DATA_022_Fault(t *testing.T) {
	in := goldenInput()

	// A witness with no compiled definition keeps every link required.
	orphanDef := in
	orphanDef.Witnesses.Witnesses = append(slices.Clone(in.Witnesses.Witnesses), witnessFor("fixture.ghost/v1", closurewitness.StateBound))
	rep, err := lc.Compile(orphanDef)
	if err != nil {
		t.Fatal(err)
	}
	ghost := caseResult(t, rep, "fixture.ghost/v1")
	if ghost.Status != lc.StatusUnknown || len(ghost.Case.Required) != len(lc.ChainLinks()) ||
		!slices.ContainsFunc(ghost.Findings, func(f lc.Finding) bool { return f.Code == lc.CodeDefinitionAbsent }) {
		t.Fatalf("ghost witness = %+v", ghost)
	}

	// A composite child with no witness is generated and named.
	unaccepted := in
	unaccepted.Witnesses.Witnesses = in.Witnesses.Witnesses[:1]
	rep, _ = lc.Compile(unaccepted)
	child := caseResult(t, rep, "fixture.people.change_thing/v1>fixture.people.explain_thing/v1")
	if child.Status == lc.StatusComplete || !slices.ContainsFunc(child.Findings, func(f lc.Finding) bool { return f.Code == lc.CodeChildNotAccepted }) {
		t.Fatalf("unaccepted child = %+v", child)
	}

	// Producer faults leave their links UNKNOWN with the reason.
	faults := map[string]func(*lc.Input){
		"open todo":       func(i *lc.Input) { i.Todos[0].Done = false },
		"missing test":    func(i *lc.Input) { i.TestExists = map[string]bool{} },
		"uncited test":    func(i *lc.Input) { i.Todos[0].PrimaryTest = "TestOther" },
		"unknown todo":    func(i *lc.Input) { i.Todos = nil },
		"invalid link":    func(i *lc.Input) { i.Producers[0].Links = []lc.Link{"TELEPATHY"} },
		"retired todo":    func(i *lc.Input) { i.Todos[0].Retired = true },
		"no tests listed": func(i *lc.Input) { i.Producers[0].Tests = nil },
	}
	for name, fault := range faults {
		faulty := goldenInput()
		fault(&faulty)
		rep, err := lc.Compile(faulty)
		if err != nil {
			t.Fatal(err)
		}
		root := caseResult(t, rep, "fixture.people.change_thing/v1")
		if root.Status != lc.StatusUnknown || !slices.ContainsFunc(root.Findings, func(f lc.Finding) bool { return f.Code == lc.CodeProducerInvalid }) {
			t.Fatalf("%s: root = %s %+v", name, root.Status, root.Findings)
		}
		if err := lc.ValidateProducer(faulty.Producers[0], faulty.Todos, faulty.TestExists); !errors.Is(err, lc.ErrProducerInvalid) {
			t.Fatalf("%s: ValidateProducer = %v", name, err)
		}
	}

	// A producer for a case nothing generates is an orphan, not a proof.
	orphan := goldenInput()
	orphan.Producers[0].Case = "fixture.nowhere/v1"
	rep, _ = lc.Compile(orphan)
	if rep.Complete || !slices.ContainsFunc(rep.Findings, func(f lc.Finding) bool { return f.Code == lc.CodeProducerOrphan }) {
		t.Fatalf("orphan producer findings = %+v", rep.Findings)
	}

	// Hard faults: no witnesses, an unreadable tree, a tampered trace.
	if _, err := lc.Compile(lc.Input{}); err == nil {
		t.Fatal("compile without witnesses succeeded")
	}
	if _, err := lc.LoadInput(t.TempDir(), asOf); err == nil {
		t.Fatal("LoadInput over an empty tree succeeded")
	}
	trace := promotionTrace(t, tenantA)
	trace.Nodes[3].Digest = "sha256:edited"
	if _, err := lc.FromTrace(lc.PromotionDefinition, tenantA, trace); !errors.Is(err, lineage.ErrBrokenChain) {
		t.Fatalf("tampered trace = %v, want lineage.ErrBrokenChain", err)
	}
	if _, err := lc.FromTrace(lc.PromotionDefinition, tenantB, promotionTrace(t, tenantA)); err == nil {
		t.Fatal("trace of tenant-a accepted for tenant-b")
	}
}

// TestTodo_DATA_022_Security plants every authorization defect in a
// complete Promotion lineage: cross-tenant hops, cross-tenant child
// causation, over-disclosure to an uncleared reader, unexplained redaction
// and a foreign reader. Each is DEFECTIVE; View never leaks.
func TestTodo_DATA_022_Security(t *testing.T) {
	cases, _ := lc.GenerateCases(catalogInput())
	promo := caseByID(t, cases, lc.PromotionDefinition)
	g := promotionGraph(t, tenantA)
	workflow := lc.PromotionDefinition + "#WORKFLOW"
	if res := lc.Evaluate(promo, lc.View(g, uncleared(tenantA)), uncleared(tenantA)); res.Status != lc.StatusComplete {
		t.Fatalf("redacted base = %s %+v", res.Status, res.Findings)
	}

	crossTenant := withRecord(g, lc.PromotionDefinition+"#EVENT", true, func(r *lc.Record) { r.Tenant = tenantB })
	if res := lc.Evaluate(promo, crossTenant, authorized(tenantA)); res.Status != lc.StatusDefective || !hasFinding(res, lc.CodeCrossTenant, lc.LinkEvent) {
		t.Fatalf("cross-tenant hop = %s %+v", res.Status, res.Findings)
	}

	// The restricted approval payload is back on the record a reader without
	// clearance was handed.
	leaked := withRecord(g, workflow, true, func(r *lc.Record) { r.Payload, r.PayloadDigest, r.Redacted = "approver=hrbp-7 ssn=992", "", false })
	res := lc.Evaluate(promo, leaked, uncleared(tenantA))
	if res.Status != lc.StatusDefective || !hasFinding(res, lc.CodeOverDisclosed, lc.LinkWorkflow) {
		t.Fatalf("over-disclosure = %s %+v", res.Status, res.Findings)
	}
	if res := lc.Evaluate(promo, leaked, authorized(tenantA)); hasFinding(res, lc.CodeOverDisclosed, lc.LinkWorkflow) {
		t.Fatal("a cleared reader seeing the approval is reported as over-disclosure")
	}
	for _, rec := range lc.View(leaked, uncleared(tenantA)).Records {
		if strings.Contains(rec.Payload, "ssn") {
			t.Fatal("View leaked the restricted payload")
		}
	}

	unexplained := withRecord(g, workflow, true, func(r *lc.Record) { r.Reason = "" })
	if res := lc.Evaluate(promo, unexplained, uncleared(tenantA)); !hasFinding(res, lc.CodeRedactionReason, lc.LinkWorkflow) {
		t.Fatalf("unexplained redaction = %+v", res.Findings)
	}

	// A reader of another tenant: the graph is refused and View discloses
	// nothing of tenant-a.
	if res := lc.Evaluate(promo, g, authorized(tenantB)); res.Status != lc.StatusDefective ||
		!slices.ContainsFunc(res.Findings, func(f lc.Finding) bool { return f.Code == lc.CodeCrossTenant && f.Link == "" }) {
		t.Fatalf("foreign reader = %s %+v", res.Status, res.Findings)
	}
	if view := lc.View(g, authorized(tenantB)); len(view.Records) != 0 {
		t.Fatalf("View disclosed %d tenant-a records to tenant-b", len(view.Records))
	}

	// A child whose causation names a parent record in another tenant.
	childCase := caseByID(t, cases, lc.ChildCaseID(lc.PromotionDefinition, "hcmnext.rewards.change_base_pay/v1"))
	child := completeGraph(t, cases, childCase, tenantA)
	cause := ""
	for _, rec := range child.Records {
		if rec.Case == childCase.ID && rec.Link == lc.LinkCausation {
			cause = rec.CausedBy
		}
	}
	foreignParent := withRecord(child, cause, true, func(r *lc.Record) { r.Tenant = tenantB })
	if res := lc.Evaluate(childCase, foreignParent, authorized(tenantA)); !hasFinding(res, lc.CodeCrossTenant, lc.LinkCausation) {
		t.Fatalf("cross-tenant child causation = %+v", res.Findings)
	}
}

// TestTodo_DATA_022_Conformance checks generation against the registries:
// one ROOT per witness, one CHILD per binding child, one TRIGGER per
// non-interactive initiator, NOT_APPLICABLE only where a definition
// declares it, and DATA-015's links computed from its stage vocabulary.
func TestTodo_DATA_022_Conformance(t *testing.T) {
	in := catalogInput()
	cases, findings := lc.GenerateCases(in)
	if len(findings) != 0 {
		t.Fatalf("catalog generation findings: %+v", findings)
	}
	wantChildren, wantTriggers := 0, 0
	for _, b := range in.Bindings {
		wantChildren += len(b.ChildDefinitions)
	}
	byID := map[string]intent.Definition{}
	for _, d := range in.Definitions {
		byID[d.Ref.String()] = d
		for _, i := range []intent.Initiator{intent.InitiatorIntegration, intent.InitiatorSchedule, intent.InitiatorRule, intent.InitiatorSystemEvent} {
			if d.AllowsInitiator(i) {
				wantTriggers++
			}
		}
	}
	counts := map[lc.PathKind]int{}
	for _, c := range cases {
		counts[c.Path]++
		def := byID[c.Definition]
		for _, na := range c.NotApplicable {
			declared := false
			switch na.Link {
			case lc.LinkProposal:
				declared = def.ProposalBindingRule == "NOT_APPLICABLE"
			case lc.LinkRepair:
				declared = def.CompensationRule == "NOT_APPLICABLE"
			case lc.LinkTransaction, lc.LinkOutbox, lc.LinkEffect, lc.LinkObservation, lc.LinkReconciliation:
				declared = !def.SideEffect.Mutates()
			case lc.LinkCorrection:
				declared = !def.SideEffect.Mutates() && def.CorrectionRule == "" && def.CompensationRule == "NOT_APPLICABLE"
			}
			if !declared {
				t.Fatalf("%s marks %s NOT_APPLICABLE without a declaration (%s)", c.ID, na.Link, na.Reason)
			}
		}
		for _, always := range []lc.Link{lc.LinkIntent, lc.LinkWorkflow, lc.LinkEvent, lc.LinkProjection} {
			if !slices.Contains(c.Required, always) {
				t.Fatalf("%s does not require %s", c.ID, always)
			}
		}
		if (c.Path != lc.PathRoot) != (c.Required[0] == lc.LinkCausation) {
			t.Fatalf("%s causation placement wrong: %v", c.ID, c.Required)
		}
	}
	if counts[lc.PathRoot] != len(in.Witnesses.Witnesses) || counts[lc.PathChild] != wantChildren || counts[lc.PathTrigger] != wantTriggers {
		t.Fatalf("paths = %v, want %d roots %d children %d triggers", counts, len(in.Witnesses.Witnesses), wantChildren, wantTriggers)
	}
	if wantChildren == 0 || wantTriggers == 0 {
		t.Fatal("catalog declares no child or trigger path: the conformance fixture would be vacuous")
	}

	producers, err := lc.DefaultProducers()
	if err != nil {
		t.Fatal(err)
	}
	var want []lc.Link
	for _, s := range lineage.Stages {
		l, ok := lc.StageLink(s)
		if !ok {
			t.Fatalf("stage %s unmapped", s)
		}
		want = append(want, l)
	}
	if !slices.Equal(producers[0].Links, want) {
		t.Fatalf("DATA-015 producer links %v, stages give %v", producers[0].Links, want)
	}
	live, err := lc.LoadInput(repoRoot(), asOf)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range live.Producers {
		if err := lc.ValidateProducer(p, live.Todos, live.TestExists); err != nil {
			t.Fatalf("default producer is not backed by the tree: %v", err)
		}
	}
	if len(live.Definitions) != len(definitions.All()) {
		t.Fatal("LoadInput did not read the compiled catalog")
	}
}

// TestTodo_DATA_022_Mutation plants each RED defect in a complete lineage,
// first proving the base lacks it, and requires the exact finding.
func TestTodo_DATA_022_Mutation(t *testing.T) {
	cases, _ := lc.GenerateCases(catalogInput())
	promo := caseByID(t, cases, lc.PromotionDefinition)
	base := promotionGraph(t, tenantA)
	reader := authorized(tenantA)
	baseRes := lc.Evaluate(promo, base, reader)
	if baseRes.Status != lc.StatusComplete || len(baseRes.Findings) != 0 {
		t.Fatalf("base is not clean: %s %+v", baseRes.Status, baseRes.Findings)
	}
	id := func(l lc.Link) string { return lc.PromotionDefinition + "#" + string(l) }
	mutants := []struct {
		name  string
		graph lc.Graph
		code  string
		link  lc.Link
		who   lc.Reader
	}{
		{"missing link", dropLink(base, promo.ID, lc.LinkReconciliation), lc.CodeLinkMissing, lc.LinkReconciliation, reader},
		{"cross-tenant link", withRecord(base, id(lc.LinkOutbox), true, func(r *lc.Record) { r.Tenant = tenantB }), lc.CodeCrossTenant, lc.LinkOutbox, reader},
		{"over-disclosure", withRecord(base, id(lc.LinkWorkflow), true, func(r *lc.Record) { r.Payload, r.Redacted = "approver=hrbp-7", false }), lc.CodeOverDisclosed, lc.LinkWorkflow, uncleared(tenantA)},
		{"non-rebuildable projection", withRecord(base, id(lc.LinkProjection), true, func(r *lc.Record) {
			r.ProjectionDigest = lc.ProjectionReplay([]string{"sha256:some-other-event"})
		}), lc.CodeNotRebuildable, lc.LinkProjection, reader},
		{"projection off watermark", withRecord(base, id(lc.LinkProjection), true, func(r *lc.Record) { r.SourceHead = "payroll@40" }), lc.CodeNotRebuildable, lc.LinkProjection, reader},
		{"projection without sources", withRecord(base, id(lc.LinkProjection), true, func(r *lc.Record) { r.DerivedFrom = nil }), lc.CodeNotRebuildable, lc.LinkProjection, reader},
		{"correction targets nothing", withRecord(base, id(lc.LinkCorrection), true, func(r *lc.Record) { r.Corrects = "" }), lc.CodeHistoryBroken, lc.LinkCorrection, reader},
		{"broken causation", withRecord(base, id(lc.LinkEffect), true, func(r *lc.Record) { r.CausedBy = id(lc.LinkIntent) }), lc.CodeBrokenCausation, lc.LinkEffect, reader},
		{"tampered digest", withRecord(base, id(lc.LinkRepair), false, func(r *lc.Record) { r.Watermark = "repair-svc/v2@later" }), lc.CodeDigestMismatch, lc.LinkRepair, reader},
		{"missing watermark", withRecord(base, id(lc.LinkObservation), true, func(r *lc.Record) { r.Watermark = "" }), lc.CodeWatermarkMissing, lc.LinkObservation, reader},
		{"duplicate link", lc.Graph{Tenant: tenantA, Records: append(slices.Clone(base.Records), func() lc.Record {
			r := base.Records[len(base.Records)-1]
			for _, rec := range base.Records {
				if rec.Link == lc.LinkEvent {
					r = rec
				}
			}
			r.ID += "-shadow"
			return r
		}())}, lc.CodeLinkDuplicate, lc.LinkEvent, reader},
	}
	for _, m := range mutants {
		who := reader
		if m.who.Tenant != "" {
			who = m.who
		}
		if hasFinding(lc.Evaluate(promo, base, who), m.code, m.link) {
			t.Fatalf("%s: base already has %s", m.name, m.code)
		}
		res := lc.Evaluate(promo, m.graph, who)
		if !hasFinding(res, m.code, m.link) || res.Status == lc.StatusComplete {
			t.Fatalf("%s: %s %+v, want %s on %s", m.name, res.Status, res.Findings, m.code, m.link)
		}
	}

	// A correction that rewrites history instead of appending.
	rewritten := withRecord(base, id(lc.LinkEvent), false, func(r *lc.Record) {
		r.SourceDigest = "sha256:corrected-in-place"
		r.Digest = r.ComputeDigest()
	})
	if f := lc.CheckAppendOnly(promo.ID, base, rewritten); len(f) != 1 || f[0].Code != lc.CodeHistoryRewritten {
		t.Fatalf("in-place correction = %+v", f)
	}
	sameID := lc.Graph{Tenant: tenantA, Records: append(slices.Clone(base.Records), rewritten.Records...)}
	if res := lc.Evaluate(promo, sameID, reader); !slices.ContainsFunc(res.Findings, func(f lc.Finding) bool { return f.Code == lc.CodeHistoryRewritten }) {
		t.Fatalf("one identity with two seals = %+v", res.Findings)
	}
	if f := lc.CheckAppendOnly(promo.ID, base, withoutCase(base)); len(f) != len(base.Records) {
		t.Fatalf("erased history findings = %d, want %d", len(f), len(base.Records))
	}

	// Trigger and not-applicable contradictions.
	trigger := caseByID(t, cases, lc.TriggerCaseID("hcmnext.operations.detect_drift/v1", "SCHEDULE"))
	tg := completeGraph(t, cases, trigger, tenantA)
	wrongTrigger := withRecord(tg, trigger.ID+"#CAUSATION", true, func(r *lc.Record) { r.Trigger = "SYSTEM_EVENT" })
	if res := lc.Evaluate(trigger, wrongTrigger, reader); !hasFinding(res, lc.CodeTriggerMismatch, lc.LinkCausation) {
		t.Fatalf("trigger mismatch = %+v", res.Findings)
	}
	if len(trigger.NotApplicable) == 0 {
		t.Fatal("detect_drift declares nothing NOT_APPLICABLE: contradiction fixture would be vacuous")
	}
	na := trigger.NotApplicable[0].Link
	contradicted := lc.Graph{Tenant: tenantA, Records: append(slices.Clone(tg.Records), lc.Record{
		ID: trigger.ID + "#" + string(na), Tenant: tenantA, Case: trigger.ID, Link: na, Watermark: "x@1"})}
	if res := lc.Evaluate(trigger, contradicted, reader); !hasFinding(res, lc.CodeNotApplicableFound, na) {
		t.Fatalf("not-applicable contradiction = %+v", res.Findings)
	}

	// Aggregate true over a missing link: forged even with a recomputed digest.
	partial := lc.Evaluate(promo, dropLink(base, promo.ID, lc.LinkReconciliation), reader)
	forged := lc.Assemble(asOf, "", []lc.CaseResult{partial}, nil)
	if forged.Complete {
		t.Fatal("honest assembly over a missing link reports complete")
	}
	forged.Complete = true
	forged.Cases[0].Status = lc.StatusComplete
	forged = reseal(forged)
	if err := lc.VerifyReport(forged); !errors.Is(err, lc.ErrFalseCompletion) {
		t.Fatalf("forged completion = %v, want ErrFalseCompletion", err)
	}
	// Flip only the aggregate flag: every case status and count stays
	// honest, so the per-case and count checks cannot mask the aggregate
	// completion check (an orchestrator mutation disabling it survived the
	// forgeries above, which also rewrite a case status).
	flagOnly := lc.Assemble(asOf, "", []lc.CaseResult{partial}, nil)
	flagOnly.Complete = true
	if err := lc.VerifyReport(reseal(flagOnly)); !errors.Is(err, lc.ErrFalseCompletion) {
		t.Fatalf("report flipping only the aggregate completion flag = %v, want ErrFalseCompletion", err)
	}
	dropped := lc.Assemble(asOf, "", []lc.CaseResult{partial}, nil)
	dropped.Cases[0].Links = slices.DeleteFunc(slices.Clone(dropped.Cases[0].Links), func(l lc.LinkStatus) bool { return l.State != lc.StateProven })
	dropped.Cases[0].Findings = nil
	dropped.Cases[0].Status = lc.StatusComplete
	dropped.Complete = true
	if err := lc.VerifyReport(reseal(dropped)); !errors.Is(err, lc.ErrFalseCompletion) {
		t.Fatalf("report omitting the missing link = %v, want ErrFalseCompletion", err)
	}
	tampered := lc.Assemble(asOf, "", []lc.CaseResult{partial}, nil)
	tampered.AsOf = "2030-01-01"
	if err := lc.VerifyReport(tampered); !errors.Is(err, lc.ErrDigestMismatch) {
		t.Fatalf("tampered report = %v, want ErrDigestMismatch", err)
	}
}

func withoutCase(g lc.Graph) lc.Graph { return lc.Graph{Tenant: g.Tenant} }

// reseal recomputes a forged report's counts and digest the way an
// attacker would, leaving only the false statuses in place.
func reseal(rep lc.Report) lc.Report {
	counts := map[lc.Status]int{}
	byPath := map[lc.PathKind]map[lc.Status]int{}
	for _, s := range lc.Statuses() {
		counts[s] = 0
	}
	for _, c := range rep.Cases {
		counts[c.Status]++
		if byPath[c.Case.Path] == nil {
			byPath[c.Case.Path] = map[lc.Status]int{}
		}
		byPath[c.Case.Path][c.Status]++
	}
	rep.Counts, rep.ByPath = counts, byPath
	return lc.SealDigest(rep)
}
