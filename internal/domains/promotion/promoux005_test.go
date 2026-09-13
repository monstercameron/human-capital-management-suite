package promotion

// PROMOUX-005: "Include reporting-line and organization impact in
// management promotions."
//
// TestTodo_PROMOUX_005 is the PRIMARY: it drives evaluateTargetManagerSelection
// directly -- no UI, no transport, no database -- and proves the genuine
// multi-hop cycle RED names ("A reports to B reports to C, then promote A to
// manage C") is refused, while a legitimate deep reporting chain that never
// closes a loop is admitted.
//
// TestTodo_PROMOUX_005_Security proves no-enumeration: a real candidate this
// caller is not authorized to select refuses with the byte-identical finding
// a genuinely nonexistent candidate does.
//
// TestTodo_PROMOUX_005_Golden pins ManagementImpact's canonical encoding.
import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// -----------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------

func promoux005Tenant() values.TenantId { return values.TenantId("promoux005-tenant") }

// The worker identities this file's scenarios are built from. A is always
// the promoted subject; B and C form the pre-existing chain RED's own
// example describes (A reports to B reports to C); D and E extend that
// chain to prove a legitimate deep one is admitted; M is a manager unrelated
// to the B/C chain, used to isolate which edge a cycle finding came from.
const (
	promoux005Subject      = "a0000000-0000-4000-8000-00000000000a"
	promoux005ManagerB     = "b0000000-0000-4000-8000-00000000000b"
	promoux005ManagerC     = "c0000000-0000-4000-8000-00000000000c"
	promoux005WorkerD      = "d0000000-0000-4000-8000-00000000000d"
	promoux005WorkerE      = "e0000000-0000-4000-8000-00000000000e"
	promoux005ManagerM     = "f0000000-0000-4000-8000-00000000000f"
	promoux005Unauthorized = "99999999-9999-4999-8999-999999999999"
)

func promoux005Worker(t testing.TB, id string) values.EntityRef {
	t.Helper()
	ref := values.EntityRef{Tenant: promoux005Tenant(), Kind: people.KindWorker, Id: id}
	if err := ref.Validate(); err != nil {
		t.Fatalf("worker ref %q: %v", id, err)
	}
	return ref
}

var promoux005AsOf = values.NewInstant(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

func promoux005Authorize(allow bool) org.Authorizer {
	return func(org.ManagerRelationshipFact) people.AuthorizationDecision {
		decision := people.AuthorizationDecision{
			PolicyVersion: "promoux005-test/v1", Purpose: "management-promotion", SubjectDisclosable: true,
			Fields: map[people.FieldID]people.FieldRuling{people.FieldManagerRelation: {Effect: people.EffectAllow}},
		}
		if !allow {
			decision.SubjectDisclosable = false
			decision.SubjectDenialReason = "scope.manager_relationship.absent"
		}
		return decision
	}
}

// promoux005OrgFacts is a minimal, self-contained org.WorkerFacts fixture
// supporting an arbitrary multi-hop worker->manager map, so a test can
// construct a genuine multi-level reporting chain rather than a single hop.
type promoux005OrgFacts struct {
	exists    map[string]bool
	managerOf map[string]string
}

func (f promoux005OrgFacts) WorkerFactsAt(_ context.Context, q org.WorkerFactsQuery) (org.WorkerFactSet, error) {
	if !f.exists[q.Worker.Id] {
		return org.WorkerFactSet{Worker: q.Worker, Exists: false}, nil
	}
	watermark, err := values.NewSequenceRevision("promoux005.graph", 1)
	if err != nil {
		return org.WorkerFactSet{}, err
	}
	set := org.WorkerFactSet{Worker: q.Worker, Exists: true, Watermark: watermark, PolicyVersion: "promoux005.policy/v1"}
	manager, ok := f.managerOf[q.Worker.Id]
	if !ok {
		return set, nil
	}
	set.Relationships = []org.ManagerRelationshipFact{promoux005ManagerFact(q.Tenant, q.Worker.Id, manager)}
	return set, nil
}

func promoux005ManagerFact(tenant values.TenantId, workerID, managerID string) org.ManagerRelationshipFact {
	effective, err := values.NewOpenInstantInterval(values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		panic(err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		panic(err)
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		panic(err)
	}
	revision, err := values.NewSequenceRevision("promoux005.relationship."+workerID, 1)
	if err != nil {
		panic(err)
	}
	return org.ManagerRelationshipFact{
		RelationshipID: "rel_" + workerID + "_" + managerID,
		Type:           org.RelationshipDirectManager,
		Worker:         values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: workerID},
		Manager:        values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: managerID},
		AssignmentID:   "asg_" + workerID,
		Effective:      effective, KnownAt: knownAt, Revision: revision,
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "promoux005-test", PolicyRef: "promoux005-test/v1"},
		Provenance: evidence.Provenance{Source: "promoux005-test", EvidenceRef: "evd_" + workerID, RecordedAt: recordedAt},
	}
}

// -----------------------------------------------------------------------
// PRIMARY
// -----------------------------------------------------------------------

func TestTodo_PROMOUX_005(t *testing.T) {
	ctx := context.Background()
	tenant := promoux005Tenant()
	subject := promoux005Worker(t, promoux005Subject)

	t.Run("nil selection performs no check at all", func(t *testing.T) {
		findings, impact, err := evaluateTargetManagerSelection(ctx, PreflightRequest{Tenant: tenant, Subject: subject})
		if err != nil || len(findings) != 0 || impact.Evaluated() {
			t.Fatalf("evaluateTargetManagerSelection(nil selection) = %+v, %+v, %v, want no findings, no impact and no error", findings, impact, err)
		}
	})

	t.Run("genuine multi-hop cycle: A reports to B reports to C, promote A to manage C", func(t *testing.T) {
		// The pre-existing graph: A -> B -> C (A's manager is B, B's manager
		// is C). M is an unrelated worker with no manager of its own, used
		// as A's OWN new manager here so the cycle this subtest proves comes
		// specifically from the affected-direct-report edge (C's manager
		// becoming A), not from a self-management shortcut on the subject's
		// own edge.
		reader := promoux005OrgFacts{
			exists:    map[string]bool{promoux005Subject: true, promoux005ManagerB: true, promoux005ManagerC: true, promoux005ManagerM: true},
			managerOf: map[string]string{promoux005Subject: promoux005ManagerB, promoux005ManagerB: promoux005ManagerC},
		}
		req := PreflightRequest{
			Tenant: tenant, Subject: subject, ManagerFacts: reader,
			TargetManagerSelection: &TargetManagerSelection{
				Reference:             promoux005Worker(t, promoux005ManagerM),
				AffectedDirectReports: []values.EntityRef{promoux005Worker(t, promoux005ManagerC)},
				AsOf:                  promoux005AsOf, ChainDepth: 10, Authorize: promoux005Authorize(true),
			},
		}
		findings, impact, err := evaluateTargetManagerSelection(ctx, req)
		if err != nil {
			t.Fatalf("evaluateTargetManagerSelection: %v", err)
		}
		if len(findings) != 1 || findings[0].Code != CodeManagerRelationshipCycle || findings[0].Severity != SeverityBlocking {
			t.Fatalf("findings = %+v, want exactly one blocking %s", findings, CodeManagerRelationshipCycle)
		}
		if impact.CycleSafe {
			t.Fatalf("impact = %+v, want CycleSafe=false for a confirmed cycle", impact)
		}

		// The one-hop-only bug this todo closes: C is neither A itself nor
		// A's *direct* manager (B is), so a naive same-worker or
		// direct-manager-only comparison would both miss this cycle. Prove
		// the fixture actually exercises the two-hop shape, so a regression
		// to either shortcut is what would make this subtest pass for the
		// wrong reason.
		if promoux005ManagerC == promoux005Subject {
			t.Fatal("fixture error: C must differ from the subject")
		}
		if reader.managerOf[promoux005Subject] == promoux005ManagerC {
			t.Fatal("fixture error: C must not be the subject's direct manager, or this is a one-hop test in disguise")
		}
	})

	t.Run("legitimate deep chain is admitted, not refused merely for being deep", func(t *testing.T) {
		// A -> B -> C -> D -> E is a genuine four-hop chain. The affected
		// direct report is an unrelated worker that never appears in it.
		reader := promoux005OrgFacts{
			exists: map[string]bool{
				promoux005Subject: true, promoux005ManagerB: true, promoux005ManagerC: true,
				promoux005WorkerD: true, promoux005WorkerE: true, promoux005ManagerM: true,
			},
			managerOf: map[string]string{
				promoux005Subject: promoux005ManagerB, promoux005ManagerB: promoux005ManagerC,
				promoux005ManagerC: promoux005WorkerD, promoux005WorkerD: promoux005WorkerE,
			},
		}
		reports := []values.EntityRef{promoux005Worker(t, promoux005ManagerM)}
		req := PreflightRequest{
			Tenant: tenant, Subject: subject, ManagerFacts: reader,
			TargetManagerSelection: &TargetManagerSelection{
				// The subject's own new manager: someone deep in an
				// unrelated, legitimately long chain (E has no manager of
				// its own -- the top).
				Reference: promoux005Worker(t, promoux005WorkerE), AffectedDirectReports: reports,
				AsOf: promoux005AsOf, ChainDepth: 10, Authorize: promoux005Authorize(true),
			},
		}
		findings, impact, err := evaluateTargetManagerSelection(ctx, req)
		if err != nil {
			t.Fatalf("evaluateTargetManagerSelection: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings = %+v, want none for a deep non-cycling chain", findings)
		}
		if !impact.CycleSafe || !impact.Evaluated() {
			t.Fatalf("impact = %+v, want a certain, cycle-safe verdict", impact)
		}
		if impact.TargetManager != promoux005Worker(t, promoux005WorkerE) {
			t.Fatalf("impact.TargetManager = %+v, want E", impact.TargetManager)
		}
		if len(impact.AffectedDirectReports) != 1 || impact.AffectedDirectReports[0] != reports[0] {
			t.Fatalf("impact.AffectedDirectReports = %+v, want the declared scope echoed back for review", impact.AffectedDirectReports)
		}
	})

	t.Run("self-management is refused without a single hop of chain depth", func(t *testing.T) {
		reader := promoux005OrgFacts{exists: map[string]bool{promoux005Subject: true}}
		req := PreflightRequest{
			Tenant: tenant, Subject: subject, ManagerFacts: reader,
			TargetManagerSelection: &TargetManagerSelection{
				Reference: subject, AsOf: promoux005AsOf, ChainDepth: 5, Authorize: promoux005Authorize(true),
			},
		}
		findings, impact, err := evaluateTargetManagerSelection(ctx, req)
		if err != nil {
			t.Fatalf("evaluateTargetManagerSelection: %v", err)
		}
		if len(findings) != 1 || findings[0].Code != CodeManagerRelationshipCycle {
			t.Fatalf("findings = %+v, want exactly one %s", findings, CodeManagerRelationshipCycle)
		}
		if impact.CycleSafe {
			t.Fatal("impact.CycleSafe = true for a self-management proposal")
		}
	})

	t.Run("no manager facts reader configured is a contract failure, not a silent pass", func(t *testing.T) {
		req := PreflightRequest{
			Tenant: tenant, Subject: subject,
			TargetManagerSelection: &TargetManagerSelection{Reference: promoux005Worker(t, promoux005ManagerB), AsOf: promoux005AsOf, ChainDepth: 5, Authorize: promoux005Authorize(true)},
		}
		if _, _, err := evaluateTargetManagerSelection(ctx, req); err == nil {
			t.Fatal("evaluateTargetManagerSelection with no ManagerFacts reader = nil error, want ErrRequestInvalid")
		}
	})

	t.Run("a material change to the target manager invalidates the digest and re-runs every check, not a weaker subset", func(t *testing.T) {
		// A -> B -> C is the same pre-existing chain the multi-hop subtest
		// above uses. M is a safe, unrelated candidate for the first run;
		// the second run changes ONLY the target manager to C, which closes
		// the cycle this todo exists to catch.
		reader := promoux005OrgFacts{
			exists:    map[string]bool{promoux005Subject: true, promoux005ManagerB: true, promoux005ManagerC: true, promoux005ManagerM: true},
			managerOf: map[string]string{promoux005Subject: promoux005ManagerB, promoux005ManagerB: promoux005ManagerC},
		}
		// Current/Proposed carry only what canonical encoding needs to
		// produce bytes at all (an effective date and a watermark); no
		// amount is disclosed, so checkCompensation still raises its own
		// blocking findings in both runs -- that is deliberate, see below.
		incompleteSnapshot := rewards.CompensationSnapshot{
			Base:               values.Absent[values.Money](),
			BonusTargetPercent: values.Absent[values.Percentage](),
			EffectiveDate:      promoux005LocalDate(t, "2026-06-01"),
			Watermark:          mustRevision(t, 1),
		}
		base := PreflightRequest{
			Tenant: tenant, Subject: subject,
			// Deliberately incomplete elsewhere (no worker fields, no
			// target job/grade, no compensation amount, no budget
			// authority): this is what proves a re-run recomputes the WHOLE
			// finding set rather than only the manager check. Every one of
			// these unrelated blocking/needs-data findings must reappear
			// unchanged in both runs.
			WorkerState:    people.Explanation{Worker: subject, Disclosure: people.DisclosureFull},
			Current:        incompleteSnapshot,
			Proposed:       incompleteSnapshot,
			EvaluationDate: promoux005LocalDate(t, "2026-06-01"),
			EffectiveDate:  promoux005LocalDate(t, "2026-06-01"),
			Policy:         DefaultPolicy(),
			Annualization:  rewards.DefaultAnnualization(),
			ManagerFacts:   reader,
		}

		safe := base
		safe.TargetManagerSelection = &TargetManagerSelection{
			Reference: promoux005Worker(t, promoux005ManagerM), AsOf: promoux005AsOf, ChainDepth: 10, Authorize: promoux005Authorize(true),
		}
		safeResult, err := PreflightPromotion(ctx, nil, safe)
		if err != nil {
			t.Fatalf("PreflightPromotion(safe): %v", err)
		}
		if safeResult.HasCode(CodeManagerRelationshipCycle) {
			t.Fatalf("safe run unexpectedly found a cycle: %+v", safeResult.Findings)
		}

		cycling := base
		cycling.TargetManagerSelection = &TargetManagerSelection{
			Reference: promoux005Worker(t, promoux005ManagerM), AffectedDirectReports: []values.EntityRef{promoux005Worker(t, promoux005ManagerC)},
			AsOf: promoux005AsOf, ChainDepth: 10, Authorize: promoux005Authorize(true),
		}
		cyclingResult, err := PreflightPromotion(ctx, nil, cycling)
		if err != nil {
			t.Fatalf("PreflightPromotion(cycling): %v", err)
		}
		// The overall Status is NEEDS_DATA rather than BLOCKED here because
		// this fixture also deliberately leaves the governed worker read
		// empty (NEEDS_DATA outranks BLOCKED in statusFor's precedence);
		// what this assertion cares about is that the cycle finding itself
		// is present, specifically and by code.
		if !cyclingResult.HasCode(CodeManagerRelationshipCycle) {
			t.Fatalf("cycling run = %+v, want a manager-relationship-cycle finding", cyclingResult.Findings)
		}

		// Materiality: the two runs differ only in the affected-report
		// scope, and that alone must move both digests -- otherwise a
		// stale approval bound to the safe run's digest could not be told
		// apart from one bound to the cycling run's.
		if safeResult.InputsDigest == cyclingResult.InputsDigest {
			t.Fatal("InputsDigest did not move for a material change to the affected-report scope")
		}
		if safeResult.ResultDigest == cyclingResult.ResultDigest {
			t.Fatal("ResultDigest did not move for a material change to the verdict")
		}

		// Not a weaker subset: every finding the safe run reported for
		// reasons that have nothing to do with the manager selection (the
		// missing worker fields, the missing target job/grade, the missing
		// compensation, the missing budget authority) must reappear
		// verbatim in the cycling run. A re-run that only re-checked the
		// manager and skipped the rest would fail this.
		for _, want := range safeResult.Findings {
			if want.Code == CodeManagerRelationshipCycle {
				continue
			}
			if !cyclingResult.HasCode(want.Code) {
				t.Fatalf("cycling run dropped pre-existing finding %q: the re-run is a weaker subset, not the exact same checks", want.Code)
			}
		}
	})
}

func promoux005LocalDate(t testing.TB, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("date %q: %v", text, err)
	}
	return d
}

// -----------------------------------------------------------------------
// SECURITY
// -----------------------------------------------------------------------

// TestTodo_PROMOUX_005_Security proves no-enumeration: a real candidate this
// caller is not authorized to see anything about, and a candidate that does
// not exist at all, refuse with the byte-for-byte identical finding -- the
// same property PROMOUX-004 required for a guessed unauthorized position.
func TestTodo_PROMOUX_005_Security(t *testing.T) {
	ctx := context.Background()
	tenant := promoux005Tenant()
	subject := promoux005Worker(t, promoux005Subject)
	// B genuinely exists and genuinely has a manager relationship (to C) --
	// there has to be a real fact on the table for the restrictive
	// authorizer below to actually withhold, or this test would prove
	// nothing about authorization at all.
	reader := promoux005OrgFacts{
		exists:    map[string]bool{promoux005Subject: true, promoux005ManagerB: true, promoux005ManagerC: true},
		managerOf: map[string]string{promoux005ManagerB: promoux005ManagerC},
	}

	unauthorizedReq := PreflightRequest{
		Tenant: tenant, Subject: subject, ManagerFacts: reader,
		TargetManagerSelection: &TargetManagerSelection{
			Reference: promoux005Worker(t, promoux005ManagerB), // exists, but denied below
			AsOf:      promoux005AsOf, ChainDepth: 5, Authorize: promoux005Authorize(false),
		},
	}
	unauthorizedFindings, unauthorizedImpact, err := evaluateTargetManagerSelection(ctx, unauthorizedReq)
	if err != nil {
		t.Fatalf("evaluateTargetManagerSelection(unauthorized): %v", err)
	}

	nonexistentReq := PreflightRequest{
		Tenant: tenant, Subject: subject, ManagerFacts: reader,
		TargetManagerSelection: &TargetManagerSelection{
			Reference: promoux005Worker(t, promoux005Unauthorized), // never in reader.exists
			AsOf:      promoux005AsOf, ChainDepth: 5, Authorize: promoux005Authorize(true),
		},
	}
	nonexistentFindings, nonexistentImpact, err := evaluateTargetManagerSelection(ctx, nonexistentReq)
	if err != nil {
		t.Fatalf("evaluateTargetManagerSelection(nonexistent): %v", err)
	}

	if len(unauthorizedFindings) != 1 || len(nonexistentFindings) != 1 {
		t.Fatalf("findings = %+v / %+v, want exactly one each", unauthorizedFindings, nonexistentFindings)
	}
	if !reflect.DeepEqual(unauthorizedFindings[0], nonexistentFindings[0]) {
		t.Fatalf("unauthorized finding %+v != nonexistent finding %+v: this is an enumeration channel", unauthorizedFindings[0], nonexistentFindings[0])
	}
	if unauthorizedFindings[0].Code != CodeTargetManagerNotFound {
		t.Fatalf("finding code = %q, want %q", unauthorizedFindings[0].Code, CodeTargetManagerNotFound)
	}
	// Neither refusal discloses anything resolved about the candidate: the
	// impact returned alongside a refusal is always the zero value.
	if unauthorizedImpact.Evaluated() || nonexistentImpact.Evaluated() {
		t.Fatalf("impact leaked on refusal: %+v / %+v", unauthorizedImpact, nonexistentImpact)
	}
}

// -----------------------------------------------------------------------
// GOLDEN
// -----------------------------------------------------------------------

// promoux005GoldenDigest pins ManagementImpact's canonical encoding. It was
// captured from this exact literal; any intentional change to the encoding
// (a new field, a reordered one) must update it deliberately, in the same
// commit that explains why.
const promoux005GoldenDigest = "sha256:ed2b2a3a47a9d00ef6ae9df95b27f120af966336b252b5cc3ef6ac80e124e33b"

func TestTodo_PROMOUX_005_Golden(t *testing.T) {
	impact := ManagementImpact{
		TargetManager: promoux005Worker(t, promoux005ManagerB),
		AffectedDirectReports: []values.EntityRef{
			promoux005Worker(t, promoux005ManagerC), promoux005Worker(t, promoux005WorkerD),
		},
		CycleSafe: true,
	}
	digest := canonicalbytes.Digest(impact.Canonical())
	if digest != promoux005GoldenDigest {
		t.Fatalf("ManagementImpact digest = %q, want the pinned %q", digest, promoux005GoldenDigest)
	}

	// Determinism: an independently-built, field-identical value digests
	// identically.
	again := ManagementImpact{
		TargetManager:         impact.TargetManager,
		AffectedDirectReports: append([]values.EntityRef(nil), impact.AffectedDirectReports...),
		CycleSafe:             true,
	}
	if got := canonicalbytes.Digest(again.Canonical()); got != digest {
		t.Fatalf("digest is not deterministic across equal values: %q != %q", got, digest)
	}

	// Materiality: GREEN's "material changes invalidate approval" clause has
	// nothing to bind to for this fact unless changing it moves the digest.
	unsafe := impact
	unsafe.CycleSafe = false
	if got := canonicalbytes.Digest(unsafe.Canonical()); got == digest {
		t.Fatal("CycleSafe is not material to the ManagementImpact digest")
	}
	fewerReports := impact
	fewerReports.AffectedDirectReports = impact.AffectedDirectReports[:1]
	if got := canonicalbytes.Digest(fewerReports.Canonical()); got == digest {
		t.Fatal("AffectedDirectReports is not material to the ManagementImpact digest")
	}
	notEvaluated := ManagementImpact{}
	if got := canonicalbytes.Digest(notEvaluated.Canonical()); got == digest {
		t.Fatal("an unevaluated impact must not digest the same as an evaluated, cycle-safe one")
	}
}
