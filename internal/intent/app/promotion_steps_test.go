package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// stepRoleAccess is a role-access store whose Load answers a fixed snapshot.
type stepRoleAccess struct {
	// Store is embedded nil so methods this fake does not override stay
	// unimplemented rather than tracking every roleaccess.Store addition.
	roleaccess.Store
	snapshot roleaccess.Snapshot
	err      error
}

func (s stepRoleAccess) Bootstrap(context.Context, values.TenantId, string) error { return nil }
func (s stepRoleAccess) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return s.snapshot, s.err
}
func (s stepRoleAccess) SaveRole(context.Context, values.TenantId, string, roleaccess.Role) (roleaccess.Role, error) {
	return roleaccess.Role{}, errors.New("unused")
}
func (s stepRoleAccess) SaveAssignment(context.Context, values.TenantId, string, roleaccess.Assignment) (roleaccess.Assignment, error) {
	return roleaccess.Assignment{}, errors.New("unused")
}
func (s stepRoleAccess) SaveVisibility(context.Context, values.TenantId, string, string, roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	return roleaccess.VisibilityPolicy{}, errors.New("unused")
}
func (s stepRoleAccess) SavePagePermission(context.Context, values.TenantId, string, roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	return roleaccess.PagePermission{}, errors.New("unused")
}

type stepHarness struct {
	cell     *Cell
	services *PromotionStepServices
	call     PromotionStepCall
}

func newStepHarness(t *testing.T, access roleaccess.Store) *stepHarness {
	t.Helper()
	store := newMemLifecycleStore()
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	var bound *PromotionStepServices
	cell, err := NewCell(CellConfig{
		Store: store, Audience: "hcm-next-api", RoleAccess: access,
		Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) {
			return nil, errors.New("verification bypassed")
		}),
		ExecutionAuthority: &ExecutionAuthority{AdmittedIntentTypes: map[string]bool{promotion.IntentType: true}, RequiredRole: "promotion_operator"},
		BindPromotionSteps: func(s *PromotionStepServices) error { bound = s; return nil },
	})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	if bound == nil {
		t.Fatal("NewCell did not bind the promotion step services")
	}
	principal := submitPrincipal(t)
	created, err := cell.Service.CreateIntent(trust.WithPrincipal(context.Background(), principal), promoteWorkerCreateRequest(t, principal, "idem-wfrun034-steps"))
	if err != nil {
		fatalWithDiagnostic(t, "CreateIntent", err)
	}
	now := time.Now().UTC()
	return &stepHarness{cell: cell, services: bound, call: PromotionStepCall{
		Delegation: runtime.ExecutionDelegation{
			Subject: principal.Subject(), SubjectKind: "human", TenantKey: string(fixtures.Tenant), OrganizationScopeID: principal.OrganizationScopeID(),
			Roles: []string{"intent_author", string(authz.RoleCompAdmin), "promotion_operator"}, Purposes: []string{authz.PurposeCompensationReview},
			AuthenticationMethod: "bearer_token", Assurance: "high", SessionRef: "session-submit-harness", EvidenceRef: principal.EvidenceID(),
			RecordedAt: now.Add(-time.Minute),
		},
		IntentID: created.GetIntent().GetIntentId(), NodeID: "snapshot_worker", IdempotencyKey: "workflow:t:i:snapshot_worker:1",
		Deadline: now.Add(time.Minute), DeclaredEffects: []capability.EffectClass{capability.EffectPure, capability.EffectReadOnly},
	}}
}

// TestPromotionStepServicesInvokeTheGatewayAsTheDelegation proves every step
// invocation passes the cell's gateway as the delegated subject with the
// purpose, deadline, idempotency key and effect class recorded as evidence,
// and that the threshold inputs come from the governed reads.
func TestPromotionStepServicesInvokeTheGatewayAsTheDelegation(t *testing.T) {
	h := newStepHarness(t, nil)
	ctx := context.Background()
	before := h.cell.Evidence.Len()

	for name, run := range map[string]func(context.Context, PromotionStepCall) (PromotionStepAnswer, error){
		"snapshot": h.services.SnapshotWorker, "simulate": h.services.SimulateCompensation,
		"band": h.services.EvaluateBand, "commit": h.services.AuthorizeCommit,
	} {
		answer, err := run(ctx, h.call)
		if err != nil || !strings.HasPrefix(answer.Digest, "sha256:") || len(answer.EvidenceIDs) == 0 || answer.Subject != h.call.Delegation.Subject {
			t.Fatalf("%s = %+v, %v", name, answer, err)
		}
	}
	in, answer, err := h.services.ThresholdInputs(ctx, h.call)
	if err != nil || answer.Digest == "" {
		t.Fatalf("ThresholdInputs = %+v, %+v, %v", in, answer, err)
	}
	if !in.GradeChange || in.BudgetAuthority != rules.BudgetAuthoritySufficient || !in.BandPosition.Valid() || in.BandPosition == rules.BandPositionUnknown {
		t.Fatalf("threshold inputs = %+v, want a grade change, sufficient budget and a known band position", in)
	}
	decision, err := rules.EvaluatePromotionApproval(rules.PromotionApprovalThresholdTable(), in)
	if err != nil || decision.Tier != rules.ApprovalTierFinanceRequired {
		t.Fatalf("decision over the governed inputs = %+v, %v; want FINANCE_REQUIRED", decision, err)
	}

	records := h.cell.Evidence.Records()[before:]
	if len(records) == 0 {
		t.Fatal("no gateway evidence was recorded")
	}
	for _, rec := range records {
		if rec.Decision != "INVOKED" || rec.SubjectRef != h.call.Delegation.Subject || rec.Purpose != authz.PurposeCompensationReview ||
			!strings.HasPrefix(rec.IdempotencyKey, h.call.IdempotencyKey+"/") || !rec.Deadline.Equal(h.call.Deadline) || rec.EffectClass != string(capability.EffectReadOnly) {
			t.Fatalf("evidence record = %+v, want the delegated governed invocation", rec)
		}
	}

	got, readAnswer, err := h.services.GovernedRead(ctx, h.call, promotionexec.CapabilityObservePayroll, func(context.Context) (any, error) { return "observed", nil })
	if err != nil || got != "observed" || len(readAnswer.EvidenceIDs) != 1 {
		t.Fatalf("GovernedRead = %v, %+v, %v", got, readAnswer, err)
	}
	last := h.cell.Evidence.Records()[h.cell.Evidence.Len()-1]
	if last.CapabilityID != promotionexec.CapabilityObservePayroll || last.Decision != "INVOKED" || last.SubjectRef != h.call.Delegation.Subject {
		t.Fatalf("governed read evidence = %+v", last)
	}
	if _, _, err := h.services.GovernedRead(ctx, h.call, "hcmnext.unknown", func(context.Context) (any, error) { return nil, nil }); err == nil {
		t.Fatal("GovernedRead invoked an unpublished capability")
	}
	if _, _, err := h.services.GovernedRead(ctx, h.call, promotionexec.CapabilityRevalidate, nil); err == nil {
		t.Fatal("GovernedRead ran without a reader")
	}
}

// TestPromotionStepServicesFailClosed proves a revoked execution role, an
// invalid delegation, an expired deadline and an undeclared effect each
// refuse the step before any handler answers, with refusal evidence.
func TestPromotionStepServicesFailClosed(t *testing.T) {
	ctx := context.Background()
	subject := submitPrincipal(t).Subject()
	revoked := newStepHarness(t, stepRoleAccess{snapshot: roleaccess.Snapshot{Assignments: []roleaccess.Assignment{{WorkerRef: subject, RoleIDs: []string{"intent_author", "comp_admin"}}}}})
	before := revoked.cell.Evidence.Len()
	if _, err := revoked.services.SnapshotWorker(ctx, revoked.call); !errors.Is(err, ErrDelegationRevoked) {
		t.Fatalf("SnapshotWorker after revocation = %v, want ErrDelegationRevoked", err)
	}
	if _, _, err := revoked.services.GovernedRead(ctx, revoked.call, promotionexec.CapabilityRevalidate, func(context.Context) (any, error) {
		t.Fatal("a revoked delegation ran a governed read")
		return nil, nil
	}); !errors.Is(err, ErrDelegationRevoked) {
		t.Fatalf("GovernedRead after revocation = %v, want ErrDelegationRevoked", err)
	}
	refusals := revoked.cell.Evidence.Records()[before:]
	if len(refusals) != 2 || refusals[0].Decision != "REFUSED" || refusals[0].ReasonCode != capability.CodeUnauthorized || refusals[1].Decision != "REFUSED" {
		t.Fatalf("revocation evidence = %+v, want two REFUSED records", refusals)
	}

	unavailable := newStepHarness(t, stepRoleAccess{err: errors.New("role store down")})
	if _, err := unavailable.services.SimulateCompensation(ctx, unavailable.call); !errors.Is(err, ErrDelegationRevoked) {
		t.Fatalf("SimulateCompensation with no role store = %v, want ErrDelegationRevoked", err)
	}

	h := newStepHarness(t, nil)
	invalid := h.call
	invalid.Delegation.SubjectKind = "robot"
	if _, err := h.services.EvaluateBand(ctx, invalid); !errors.Is(err, ErrDelegationInvalid) {
		t.Fatalf("EvaluateBand with an unknown subject kind = %v, want ErrDelegationInvalid", err)
	}
	missing := h.call
	missing.Delegation.Purposes = nil
	if _, _, err := h.services.ThresholdInputs(ctx, missing); !errors.Is(err, ErrDelegationInvalid) {
		t.Fatalf("ThresholdInputs with no purpose = %v, want ErrDelegationInvalid", err)
	}
	expired := h.call
	expired.Deadline = time.Now().UTC().Add(-time.Hour)
	if _, err := h.services.AuthorizeCommit(ctx, expired); !errors.Is(err, ErrDelegationInvalid) {
		t.Fatalf("AuthorizeCommit past its deadline = %v, want ErrDelegationInvalid", err)
	}
	undeclared := h.call
	undeclared.DeclaredEffects = []capability.EffectClass{capability.EffectPure}
	var gwErr *capability.GatewayError
	if _, err := h.services.SnapshotWorker(ctx, undeclared); !errors.As(err, &gwErr) || gwErr.Code != capability.CodeEffectUndeclared {
		t.Fatalf("SnapshotWorker with a PURE effect set = %v, want %s", err, capability.CodeEffectUndeclared)
	}
	unknown := h.call
	unknown.IntentID = "00000000-0000-0000-0000-000000000000"
	if _, err := h.services.SnapshotWorker(ctx, unknown); err == nil {
		t.Fatal("SnapshotWorker resolved an intent that does not exist")
	}
}

func TestPromotionStepServicesComposition(t *testing.T) {
	if _, err := NewPromotionStepServices(nil); err == nil {
		t.Fatal("NewPromotionStepServices accepted no cell")
	}
	store := newMemLifecycleStore()
	verifier := trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, nil })
	cell, err := NewCell(CellConfig{Store: store, Verifier: verifier, Audience: "hcm-next-api"})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	if _, err := NewPromotionStepServices(cell); err == nil {
		t.Fatal("step services composed over a cell with no execution authority")
	}
	fault := errors.New("bind refused")
	if _, err := NewCell(CellConfig{Store: store, Verifier: verifier, Audience: "hcm-next-api",
		BindPromotionSteps: func(*PromotionStepServices) error { return fault }}); err == nil {
		t.Fatal("NewCell bound step services with no execution authority")
	}
	if _, err := NewCell(CellConfig{Store: store, Verifier: verifier, Audience: "hcm-next-api",
		ExecutionAuthority: &ExecutionAuthority{RequiredRole: "promotion_operator"},
		BindPromotionSteps: func(*PromotionStepServices) error { return fault }}); !errors.Is(err, fault) {
		t.Fatalf("NewCell with a refusing binder = %v, want the binder's error", err)
	}

	principal := submitPrincipal(t)
	if executionDelegation(principal, " ") != nil {
		t.Fatal("a delegation was pinned with no purpose")
	}
	d := executionDelegation(principal, authz.PurposeCompensationReview)
	if d == nil || d.Validate() != nil || d.Subject != principal.Subject() || d.EvidenceRef != principal.EvidenceID() ||
		d.AuthenticationMethod != "bearer_token" || d.Assurance != "high" || d.Purposes[0] != authz.PurposeCompensationReview {
		t.Fatalf("executionDelegation = %+v", d)
	}
}
