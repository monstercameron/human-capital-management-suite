package localcommit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
	transactionplan "github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

func instantInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	a, err := time.Parse(time.RFC3339, "2026-10-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	b, err := time.Parse(time.RFC3339, "2027-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	start := values.NewInstant(a)
	end := values.NewInstant(b)
	iv, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func testPreparedPlan(t *testing.T) PreparedPlan {
	t.Helper()
	return testPreparedPlanFor(t, "proposal-1", "sha256:proposal", "plan-1", "idem-1")
}

// testPreparedPlanFor builds the same valid plan with a distinct proposal
// identity, so two proposals can contend for one fenced head.
func testPreparedPlanFor(t *testing.T, revision, proposalDigest, planID, idemKey string) PreparedPlan {
	t.Helper()
	tenant := values.TenantId("11111111-1111-4111-8111-111111111111")
	key, err := values.NewResourceKey(tenant, values.Kind("assignment"), "worker-1", "assignment-1")
	if err != nil {
		t.Fatal(err)
	}
	rev, err := values.NewSequenceRevision("people.assignment/worker-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	subject := intent.SubjectReference{Kind: "worker", SubjectID: "worker-1", AuthorityDomain: "people"}
	iv := instantInterval(t)
	pay1, _ := values.NewMoney("165000.00", "USD", 2, values.RoundingExactRequired)
	pay2, _ := values.NewMoney("180000.00", "USD", 2, values.RoundingExactRequired)
	p := people.PromotionMutation{Tenant: tenant, ProposalRevisionID: revision, ProposalDigest: proposalDigest, ActorPrincipalID: "principal", AuthorityDecision: "authority", WorkerID: "worker-1", AssignmentID: "assignment-1", ResourceKey: key, Subject: subject, Effective: iv, ExpectedRevision: rev, CurrentJobCode: "ENG-3", TargetJobCode: "ENG-MGR", CurrentLevel: "P3", TargetLevel: "M1"}
	orgRev, _ := values.NewSequenceRevision("org.manager_relationship/worker-1", 4)
	o := org.ManagerMutation{Tenant: tenant, ProposalRevisionID: revision, ProposalDigest: proposalDigest, ActorPrincipalID: "principal", AuthorityDecision: "authority", WorkerID: "worker-1", AssignmentID: "assignment-1", ResourceKey: key, Subject: intent.SubjectReference{Kind: "worker", SubjectID: "worker-1", AuthorityDomain: "organization"}, Effective: iv, ExpectedRevision: orgRev, CurrentRelationshipID: "rel-old", TargetRelationshipID: "rel-new", CurrentManagerID: "manager-old", TargetManagerID: "manager-new"}
	cKey, _ := values.NewResourceKey(tenant, values.Kind("compensation_component"), "worker-1", "base-pay-1")
	cRev, _ := values.NewSequenceRevision("rewards.compensation/worker-1", 9)
	c := compensation.PromotionMutation{Tenant: tenant, ProposalRevisionID: revision, ProposalDigest: proposalDigest, ActorPrincipalID: "principal", AuthorityDecision: "authority", WorkerID: "worker-1", PackageID: "package-1", ComponentID: "base-pay-1", ResourceKey: cKey, Subject: intent.SubjectReference{Kind: "worker", SubjectID: "worker-1", AuthorityDomain: "compensation"}, Effective: iv, ExpectedRevision: cRev, CurrentBasePay: pay1, TargetBasePay: pay2}
	pw, _ := p.PlannedWrites()
	ow, _ := o.PlannedWrites()
	cw, _ := c.PlannedWrites()
	writes := append(append(pw, ow...), cw...)
	proposal := intent.ProposalRevision{ProposalRevisionID: revision, IntentID: "intent-1", Revision: 1, Tenant: tenant,
		Subjects: []intent.SubjectReference{subject}, MaterialDigest: digest.Reference{ProfileID: "PROPOSAL", ProfileVersion: 1, SchemaID: "proposal", SchemaVersion: 1, AlgorithmID: "sha256", Digest: proposalDigest},
		Writes: writes, SourceBaselines: []intent.SourceBaseline{{StreamID: rev.Stream(), ExpectedRevision: rev}, {StreamID: orgRev.Stream(), ExpectedRevision: orgRev}, {StreamID: cRev.Stream(), ExpectedRevision: cRev}},
		Effects: []intent.PlannedEffect{{EffectID: "payroll-1", Kind: "external", DestinationRef: "payroll", ObservationRef: "payroll-observation"}}}
	governance, err := decision.Compose(decision.Inputs{ProposalRevisionDigest: proposalDigest, Context: decision.Context{Principal: "principal", Delegation: "none", Capability: "promotion.execute", Resource: "worker-1", Fields: []string{"assignment.job_code", "assignment.grade", "compensation.base_pay"}, CurrentOrganization: "org", TargetOrganization: "org", Purpose: "promotion", Risk: "low", Authority: "people", Legal: "legal:promotion"}, ControlSnapshot: decision.ControlSnapshot{Digest: "sha256:controls"}, Subdecisions: []decision.Subdecision{{ID: "promotion", Source: "promotion", State: decision.Allow}}})
	if err != nil {
		t.Fatal(err)
	}
	heads := localHeads{rev.Stream(): {StreamKey: rev.Stream(), Sequence: 7, Digest: "sha256:head1", DigestAlgorithm: "sha256"}, orgRev.Stream(): {StreamKey: orgRev.Stream(), Sequence: 4, Digest: "sha256:head2", DigestAlgorithm: "sha256"}, cRev.Stream(): {StreamKey: cRev.Stream(), Sequence: 9, Digest: "sha256:head3", DigestAlgorithm: "sha256"}}
	planInput, err := transactionplan.Prepare(t.Context(), heads, transactionplan.PrepareRequest{Proposal: proposal, GovernanceDecision: governance, Events: []transactionplan.PlannedEvent{
		{StreamKey: rev.Stream(), EventType: "people.JobChanged", SchemaRef: "promotion.people/v1", Digest: "sha256:event1"}, {StreamKey: rev.Stream(), EventType: "people.LevelChanged", SchemaRef: "promotion.people/v1", Digest: "sha256:event2"},
		{StreamKey: orgRev.Stream(), EventType: "org.ManagerChanged", SchemaRef: "promotion.org/v1", Digest: "sha256:event3"}, {StreamKey: orgRev.Stream(), EventType: "org.RelationshipChanged", SchemaRef: "promotion.org/v1", Digest: "sha256:event4"},
		{StreamKey: cRev.Stream(), EventType: "compensation.BasePayChanged", SchemaRef: "promotion.compensation/v1", Digest: "sha256:event5"}}, OutboxEffects: []transactionplan.OutboxEffect{{EffectID: "payroll-1", DestinationRef: "payroll", SchemaRef: "promotion.payroll/v1", PayloadDigest: "sha256:payload", IdempotencyKey: "effect-1"}}, IdempotencyKey: idemKey, Now: values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)), ExpiresAt: values.NewInstant(time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC)), PlanID: planID})
	if err != nil {
		t.Fatal(err)
	}
	participants := []intent.PlanParticipant{{ParticipantID: ParticipantPeople, StreamID: "people.assignment/worker-1", StorageClass: "LOCAL_EVENT_STREAM", Local: true}, {ParticipantID: ParticipantOrganization, StreamID: "org.manager_relationship/worker-1", StorageClass: "LOCAL_EVENT_STREAM", Local: true}, {ParticipantID: ParticipantCompensation, StreamID: "rewards.compensation/worker-1", StorageClass: "LOCAL_EVENT_STREAM", Local: true}}
	boundary := transaction.ConsistencyBoundary{BoundaryID: "promotion", Tenant: tenant, CellID: "cell", CoordinatorID: "coordinator", Admitted: []transaction.AdmissionSelector{{StorageClass: "LOCAL_EVENT_STREAM"}}, Isolation: transaction.IsolationSerializable, Protocol: transaction.CommitProtocolSingleDatabaseACID, CrossBoundaryDisposition: transaction.CrossBoundaryDispositionSplitIntoEffects, CoordinatorEpoch: 1, Version: 1}
	res, err := transaction.ResolveConsistencyBoundary(boundary, intent.TransactionPlan{PlanID: planID, Tenant: tenant, Participants: participants}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return PreparedPlan{Plan: planInput, Resolution: res, People: p, Organization: o, Compensation: c, InvariantVersions: []string{"people/1", "org/1", "compensation/1"}}
}

type localHeads map[string]transactionplan.Head

func (h localHeads) CurrentHead(_ context.Context, _ values.TenantId, stream string) (transactionplan.Head, error) {
	return h[stream], nil
}

func TestTodo_PROMO_005(t *testing.T) {
	p := testPreparedPlan(t)
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	c := &Committer{Store: NewStore(), Fence: allowReservations{}}
	r, err := c.Commit(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if r.EventCount != 5 || len(r.OutboxEffectIDs) != 1 {
		t.Fatalf("receipt = %+v", r)
	}
	r2, err := c.Commit(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Replayed {
		t.Fatal("replay was not marked")
	}
}

func TestTodo_PROMO_005_Fault(t *testing.T) {
	for _, stage := range []string{"before-participants", ParticipantPeople, ParticipantOrganization, ParticipantCompensation, "outbox", "receipt", "after-commit"} {
		t.Run(stage, func(t *testing.T) {
			c := &Committer{Store: NewStore(), Fence: allowReservations{}, Failpoint: func(got string) error {
				if got == stage {
					return errors.New("crash")
				}
				return nil
			}}
			if _, err := c.Commit(t.Context(), testPreparedPlan(t)); !errors.Is(err, ErrCommitAborted) {
				t.Fatalf("error = %v", err)
			}
			_, durable := c.Store.lookup("11111111-1111-4111-8111-111111111111\x00idem-1")
			if stage != "after-commit" && durable {
				t.Fatal("fault left a receipt")
			}
			if stage == "after-commit" && !durable {
				t.Fatal("post-commit ambiguity lost the durable receipt")
			}
		})
	}
}

func TestTodo_PROMO_005_Race(t *testing.T) {
	committer := &Committer{Store: NewStore(), Fence: allowReservations{}}
	prepared := testPreparedPlan(t)
	results := make(chan Receipt, 8)
	errs := make(chan error, 8)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			receipt, err := committer.Commit(t.Context(), prepared)
			results <- receipt
			errs <- err
		}()
	}
	group.Wait()
	close(results)
	close(errs)
	var committed int
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for receipt := range results {
		if receipt.PlanDigest == prepared.Plan.Digest {
			committed++
		}
	}
	if committed != 8 {
		t.Fatalf("concurrent commits returned %d valid receipts, want 8", committed)
	}
}

func TestTodo_PROMO_005_Mutation(t *testing.T) {
	p := testPreparedPlan(t)
	p.People.ProposalDigest = "sha256:other"
	if err := p.Validate(); err == nil {
		t.Fatal("accepted changed proposal binding")
	}
}

func TestTodo_MODEL_025(t *testing.T) {
	interval := instantInterval(t)
	result := EvaluateInvariants(InvariantInput{Tenant: values.TenantId("11111111-1111-4111-8111-111111111111"), WorkerID: "worker", ManagerID: "manager", ManagerAncestors: []string{"worker"}, EmploymentIntervals: []values.EffectiveInterval{interval}, AssignmentIntervals: []values.EffectiveInterval{interval}, PositionCapacity: 1, PositionOccupied: 2, CompensationCurrency: "USD", BudgetCurrency: "EUR", References: []TenantReference{{Ref: "other", Tenant: values.TenantId("22222222-2222-4222-8222-222222222222")}}}, "people/1")
	if result.Status != InvariantFail || len(result.Findings) != 4 {
		t.Fatalf("result = %+v", result)
	}
}

func TestTodo_MODEL_025_Property(t *testing.T) {
	a := EvaluateInvariants(InvariantInput{Tenant: values.TenantId("11111111-1111-4111-8111-111111111111"), WorkerID: "worker", ManagerID: "manager", PositionCapacity: 2, PositionOccupied: 1, CompensationCurrency: "USD", BudgetCurrency: "USD"})
	b := EvaluateInvariants(InvariantInput{Tenant: values.TenantId("11111111-1111-4111-8111-111111111111"), WorkerID: "worker", ManagerID: "manager", PositionCapacity: 2, PositionOccupied: 1, CompensationCurrency: "USD", BudgetCurrency: "USD"})
	if a.Status != InvariantUnknown || b.Status != a.Status {
		t.Fatalf("non-deterministic status: %+v %+v", a, b)
	}
}
func TestTodo_MODEL_025_Golden(t *testing.T) {
	if got := Explain(testPreparedPlan(t)); got == "" || !strings.Contains(got, "plan-1") {
		t.Fatalf("Explain = %q", got)
	}
}
func TestTodo_MODEL_025_Security(t *testing.T) {
	result := EvaluateInvariants(InvariantInput{Tenant: values.TenantId("11111111-1111-4111-8111-111111111111"), WorkerID: "worker", ManagerID: "manager", PositionCapacity: 1, PositionOccupied: 1, CompensationCurrency: "USD", BudgetCurrency: "USD", References: []TenantReference{{Ref: "secret", Tenant: values.TenantId("22222222-2222-4222-8222-222222222222")}}})
	if result.Status != InvariantFail {
		t.Fatal("cross-tenant reference was accepted")
	}
}

func FuzzTodo_MODEL_025(f *testing.F) {
	f.Add(int64(1), int64(1))
	f.Fuzz(func(t *testing.T, capacity, occupied int64) {
		result := EvaluateInvariants(InvariantInput{Tenant: values.TenantId("11111111-1111-4111-8111-111111111111"), WorkerID: "worker", ManagerID: "manager", PositionCapacity: capacity, PositionOccupied: occupied, CompensationCurrency: "USD", BudgetCurrency: "USD"})
		if result.Status == "" {
			t.Fatal("invariant evaluation returned no status")
		}
	})
}

func BenchmarkTodo_MODEL_025(b *testing.B) {
	in := InvariantInput{Tenant: values.TenantId("11111111-1111-4111-8111-111111111111"), WorkerID: "worker", ManagerID: "manager", PositionCapacity: 2, PositionOccupied: 1, CompensationCurrency: "USD", BudgetCurrency: "USD"}
	for i := 0; i < b.N; i++ {
		_ = EvaluateInvariants(in, "people/1", "org/1", "compensation/1")
	}
}

func TestTodo_MODEL_025_Mutation(t *testing.T) {
	result := EvaluateInvariants(InvariantInput{Tenant: values.TenantId("11111111-1111-4111-8111-111111111111"), WorkerID: "worker", ManagerID: "manager", PositionCapacity: 1, PositionOccupied: 2, CompensationCurrency: "USD", BudgetCurrency: "EUR"})
	if result.Status != InvariantFail {
		t.Fatal("mutated capacity and currency facts were accepted")
	}
}
