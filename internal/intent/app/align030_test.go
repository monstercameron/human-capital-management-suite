package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactionplan "github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

var (
	align030Tenant   = values.TenantId("acme")
	align030Now      = values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	align030Expiry   = values.NewInstant(time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC))
	align030StreamID = "people.assignment/worker-1"
)

type align030Heads map[string]transactionplan.Head

func (h align030Heads) CurrentHead(_ context.Context, _ values.TenantId, stream string) (transactionplan.Head, error) {
	return h[stream], nil
}

func align030Proposal(t *testing.T) intent.ProposalRevision {
	t.Helper()
	key, err := values.NewResourceKey(align030Tenant, values.Kind("assignment"), "worker-1", "assignment-1")
	if err != nil {
		t.Fatal(err)
	}
	rev, err := values.NewSequenceRevision(align030StreamID, 7)
	if err != nil {
		t.Fatal(err)
	}
	subject := intent.SubjectReference{Kind: "worker", SubjectID: "worker-1", AuthorityDomain: "people"}
	return intent.ProposalRevision{
		ProposalRevisionID: "proposal-1", IntentID: "intent-1", Revision: 1, Tenant: align030Tenant,
		Subjects: []intent.SubjectReference{subject},
		MaterialDigest: digest.Reference{ProfileID: "PROPOSAL", ProfileVersion: 1, SchemaID: "proposal",
			SchemaVersion: 1, AlgorithmID: "sha256", Digest: "sha256:proposal"},
		Writes: []intent.PlannedWrite{{
			Subject: subject, ResourceKey: key, FieldPath: "assignment.job_code",
			CurrentCanonicalText: "ENG-3", ProposedCanonicalText: "ENG-MGR",
			SourceAuthorityDecision: "authority", ExpectedRevision: rev,
		}},
		SourceBaselines: []intent.SourceBaseline{{StreamID: rev.Stream(), ExpectedRevision: rev}},
		Effects:         []intent.PlannedEffect{{EffectID: "payroll-1", Kind: "external", DestinationRef: "payroll", ObservationRef: "payroll-observation"}},
	}
}

func align030Governance(t *testing.T) decision.Decision {
	t.Helper()
	g, err := decision.Compose(decision.Inputs{ProposalRevisionDigest: "sha256:proposal", Context: decision.Context{
		Principal: "principal-admin", Delegation: "none", Capability: "promotion.execute", Resource: "worker-1",
		Fields: []string{"assignment.job_code"}, CurrentOrganization: "org", TargetOrganization: "org",
		Purpose: "promotion", Risk: "low", Authority: "people", Legal: "legal:promotion",
	}, ControlSnapshot: decision.ControlSnapshot{Digest: "sha256:controls"},
		Subdecisions: []decision.Subdecision{{ID: "promotion", Source: "promotion", State: decision.Allow}}})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func align030Plan(t *testing.T, idempotencyKey, planID string, heads align030Heads) transactionplan.TransactionPlan {
	t.Helper()
	plan, err := transactionplan.Prepare(context.Background(), heads, transactionplan.PrepareRequest{
		Proposal: align030Proposal(t), GovernanceDecision: align030Governance(t),
		Events: []transactionplan.PlannedEvent{{
			StreamKey: align030StreamID, EventType: "people.JobChanged",
			SchemaRef: "promotion.people/v1", Digest: "sha256:event1",
		}},
		OutboxEffects: []transactionplan.OutboxEffect{{
			EffectID: "payroll-1", DestinationRef: "payroll", SchemaRef: "promotion.payroll/v1",
			PayloadDigest: "sha256:payload", IdempotencyKey: "effect-1",
		}},
		IdempotencyKey: idempotencyKey, Now: align030Now, ExpiresAt: align030Expiry, PlanID: planID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func align030Action() AcceptedAction {
	return AcceptedAction{
		Tenant: align030Tenant, DecisionID: "00000000-0000-4000-8000-000000000030", ActionID: "promotion.execute", IntentID: "intent-1",
		ProposalRevisionID: "proposal-1", ProposalDigest: "sha256:proposal",
		AcceptedBy: "principal-admin", AcceptedAt: align030Now, IdempotencyKey: "idem-1",
	}
}

func align030DefaultHeads() align030Heads {
	return align030Heads{align030StreamID: {StreamKey: align030StreamID, Sequence: 7, Digest: "sha256:head1", DigestAlgorithm: "sha256"}}
}

// TestTodo_ALIGN_030 proves an accepted action binds to exactly one
// transaction plan: same tenant, same proposal revision and digest, same
// semantic idempotency key, and a plan whose digest verifies.
func TestTodo_ALIGN_030(t *testing.T) {
	plan := align030Plan(t, "idem-1", "plan-1", align030DefaultHeads())
	b, err := BindAcceptedAction(align030Action(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if b.Tenant != align030Tenant || b.ActionID != "promotion.execute" || b.IntentID != "intent-1" {
		t.Fatalf("binding identity = %+v", b)
	}
	if b.PlanID != "plan-1" || b.PlanDigest != plan.Digest || b.ProposalDigest != "sha256:proposal" {
		t.Fatalf("binding pins = %+v", b)
	}
	if err := b.VerifyDigest(); err != nil {
		t.Fatalf("binding VerifyDigest: %v", err)
	}
	if got := b.Check(align030Action(), plan); got != ActionPlanCurrent {
		t.Fatalf("binding check = %s, want CURRENT", got)
	}
	if b.Explain() == "" || b.DigestValue() == "" {
		t.Fatal("binding has no bounded explanation or digest")
	}
}

func TestTodo_ALIGN_030_Property(t *testing.T) {
	plan := align030Plan(t, "idem-1", "plan-1", align030DefaultHeads())
	one, err := BindAcceptedAction(align030Action(), plan)
	if err != nil {
		t.Fatal(err)
	}
	two, err := BindAcceptedAction(align030Action(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if one.DigestValue() != two.DigestValue() {
		t.Fatalf("binding digest is not deterministic: %q != %q", one.DigestValue(), two.DigestValue())
	}
	otherDecision := align030Action()
	otherDecision.DecisionID = "00000000-0000-4000-8000-000000000031"
	otherBinding, err := BindAcceptedAction(otherDecision, plan)
	if err != nil {
		t.Fatal(err)
	}
	if otherBinding.DigestValue() == one.DigestValue() || one.Check(otherDecision, plan) != ActionPlanChanged {
		t.Fatal("binding did not pin the accepted decision identity")
	}
	other := align030Plan(t, "idem-2", "", align030DefaultHeads())
	three, err := BindAcceptedAction(align030Action(), other)
	if err == nil {
		t.Fatal("action bound to a plan prepared under another idempotency key")
	}
	_ = three
}

func TestTodo_ALIGN_030_Golden(t *testing.T) {
	plan := align030Plan(t, "idem-1", "plan-1", align030DefaultHeads())
	b, err := BindAcceptedAction(align030Action(), plan)
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:5f91ca87a683927d001e2e5260eec66f7f2b17d6a66b22efaf5d154ca2ec775b"
	if b.DigestValue() != wantDigest {
		t.Fatalf("binding digest=%q want=%q", b.DigestValue(), wantDigest)
	}
	if len(plan.Events) != 1 || len(plan.OutboxEffects) != 1 || len(plan.Streams) != 1 {
		t.Fatalf("bound plan effects = %+v", plan)
	}
}

func TestTodo_ALIGN_030_Security(t *testing.T) {
	plan := align030Plan(t, "idem-1", "plan-1", align030DefaultHeads())
	cases := map[string]func() (AcceptedAction, transactionplan.TransactionPlan){
		"cross tenant": func() (AcceptedAction, transactionplan.TransactionPlan) {
			a := align030Action()
			a.Tenant = values.TenantId("other")
			return a, plan
		},
		"wrong proposal digest": func() (AcceptedAction, transactionplan.TransactionPlan) {
			a := align030Action()
			a.ProposalDigest = "sha256:something-else"
			return a, plan
		},
		"wrong revision": func() (AcceptedAction, transactionplan.TransactionPlan) {
			a := align030Action()
			a.ProposalRevisionID = "proposal-2"
			return a, plan
		},
		"missing idempotency": func() (AcceptedAction, transactionplan.TransactionPlan) {
			a := align030Action()
			a.IdempotencyKey = ""
			return a, plan
		},
		"empty action": func() (AcceptedAction, transactionplan.TransactionPlan) {
			a := align030Action()
			a.ActionID = ""
			return a, plan
		},
		"tampered plan digest": func() (AcceptedAction, transactionplan.TransactionPlan) {
			p := plan
			p.Digest = "sha256:tampered"
			return align030Action(), p
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			action, p := setup()
			if _, err := BindAcceptedAction(action, p); err == nil {
				t.Fatalf("%s was bound", name)
			}
		})
	}
	b, err := BindAcceptedAction(align030Action(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Explain(); strings.Contains(got, "principal-admin") {
		t.Fatalf("Explain exposed the accepting principal: %q", got)
	}
	tampered := b
	tampered.PlanDigest = "sha256:tampered"
	if err := tampered.VerifyDigest(); err == nil {
		t.Fatal("tampered binding verified")
	}
	if got := tampered.Check(align030Action(), plan); got != ActionPlanTampered {
		t.Fatalf("tampered binding check = %s, want TAMPERED", got)
	}
}

func TestTodo_ALIGN_030_Integration(t *testing.T) {
	heads := align030DefaultHeads()
	plan := align030Plan(t, "idem-1", "plan-1", heads)
	action := align030Action()
	bound, err := BindAcceptedAction(action, plan)
	if err != nil {
		t.Fatal(err)
	}
	// The bound plan carries the exact persistence effects the acceptance
	// authorizes: one stream, one event, one outbox effect.
	if len(plan.Streams) != 1 || plan.Streams[0].ExpectedSequence != 7 {
		t.Fatalf("bound plan streams = %+v", plan.Streams)
	}
	if got := bound.Check(action, plan); got != ActionPlanCurrent {
		t.Fatalf("fresh binding check = %s, want CURRENT", got)
	}
	// A successor plan prepared under a new idempotency key supersedes the
	// binding instead of silently replacing it.
	successor := align030Plan(t, "idem-2", "", heads)
	if successor.PlanID == bound.PlanID {
		t.Fatalf("successor plan reused plan id %q", successor.PlanID)
	}
	if got := bound.Check(action, successor); got != ActionPlanSuperseded {
		t.Fatalf("successor check = %s, want SUPERSEDED", got)
	}
	// An edited acceptance no longer matches the binding.
	edited := action
	edited.ProposalDigest = plan.ProposalDigest
	edited.AcceptedBy = "principal-other"
	if got := bound.Check(edited, plan); got != ActionPlanChanged {
		t.Fatalf("edited acceptance check = %s, want ACTION_CHANGED", got)
	}
}

func TestTodo_ALIGN_030_Fault(t *testing.T) {
	plan := align030Plan(t, "idem-1", "plan-1", align030DefaultHeads())
	// An acceptance after plan expiry binds to nothing.
	expired := align030Action()
	expired.AcceptedAt = values.NewInstant(align030Expiry.Time().Add(time.Minute))
	if _, err := BindAcceptedAction(expired, plan); err == nil {
		t.Fatal("expired plan was bound")
	}
	// A moved stream head means the plan was never prepared: there is no
	// plan digest to pin, so binding is impossible.
	moved := align030DefaultHeads()
	moved[align030StreamID] = transactionplan.Head{StreamKey: align030StreamID, Sequence: 8, Digest: "sha256:head2", DigestAlgorithm: "sha256"}
	if _, err := transactionplan.Prepare(context.Background(), moved, transactionplan.PrepareRequest{
		Proposal: align030Proposal(t), GovernanceDecision: align030Governance(t),
		Events: []transactionplan.PlannedEvent{{
			StreamKey: align030StreamID, EventType: "people.JobChanged",
			SchemaRef: "promotion.people/v1", Digest: "sha256:event1",
		}},
		OutboxEffects: []transactionplan.OutboxEffect{{
			EffectID: "payroll-1", DestinationRef: "payroll", SchemaRef: "promotion.payroll/v1",
			PayloadDigest: "sha256:payload", IdempotencyKey: "effect-1",
		}},
		IdempotencyKey: "idem-1", Now: align030Now, ExpiresAt: align030Expiry, PlanID: "plan-1",
	}); err == nil {
		t.Fatal("plan prepared over a moved head")
	}
	// An empty plan carries no verifiable digest.
	if _, err := BindAcceptedAction(align030Action(), transactionplan.TransactionPlan{}); err == nil {
		t.Fatal("empty plan was bound")
	}
}

func TestTodo_ALIGN_030_Conformance(t *testing.T) {
	plan := align030Plan(t, "idem-1", "plan-1", align030DefaultHeads())
	bound, err := BindAcceptedAction(align030Action(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyDigest(); err != nil {
		t.Fatalf("bound plan VerifyDigest: %v", err)
	}
	if err := bound.VerifyDigest(); err != nil {
		t.Fatalf("binding VerifyDigest: %v", err)
	}
	if bound.Version() != 1 {
		t.Fatalf("binding version = %d, want 1", bound.Version())
	}
}
