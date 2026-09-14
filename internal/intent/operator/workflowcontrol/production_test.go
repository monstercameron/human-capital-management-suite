package workflowcontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type fakeGrants struct {
	grants []*jit.Grant
	err    error
	asked  string
}

func (f *fakeGrants) ActiveJITGrants(_ context.Context, _ uuid.UUID, _ values.TenantId, requester, capability string, _ time.Time) ([]*jit.Grant, error) {
	f.asked = requester + "|" + capability
	return f.grants, f.err
}

func restored(t *testing.T, role jit.Role, caps ...string) *jit.Grant {
	t.Helper()
	g, err := jit.Restore(jit.Stored{ID: "g-" + string(role), Principal: "operator:ana", Tenant: "acme", Role: role, TicketRef: "INC-1",
		Justification: "j", Capabilities: caps, Purpose: "p", Approver: "approver:lead", IssuedAt: fixedNow.Add(-time.Minute), ExpiresAt: fixedNow.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestJITAuthorityPlanSetAndTenantMapping(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	ids := KeyedTenantIDs(func(string) uuid.UUID { return tenant })
	source := &fakeGrants{grants: []*jit.Grant{
		restored(t, jit.RoleSupportReadOnly, string(operator.KindWorkflowPause)),
		restored(t, jit.RoleIncidentResponder, string(operator.KindWorkflowPause)),
	}}
	a := JITAuthority{Grants: source, TenantIDs: ids, Clock: func() time.Time { return fixedNow }}
	auth, err := a.ResolveAuthority(ctx, "acme", "operator:ana", operator.KindWorkflowPause, "i")
	if err != nil || auth.JIT == nil || auth.JIT.Role != jit.RoleIncidentResponder || source.asked != "operator:ana|WORKFLOW_PAUSE" {
		t.Fatalf("authority = %+v, %v (asked %s)", auth, err, source.asked)
	}
	if auth.SecondApprover != "" || auth.Simulation != nil || auth.Emergency != nil {
		t.Fatal("JIT authority invented dual control, simulation or emergency evidence")
	}
	readOnly := &fakeGrants{grants: []*jit.Grant{restored(t, jit.RoleSupportReadOnly, string(operator.KindWorkflowPause))}}
	if auth, err := (JITAuthority{Grants: readOnly, TenantIDs: ids}).ResolveAuthority(ctx, "acme", "operator:ana", operator.KindWorkflowPause, "i"); err != nil || auth.JIT != nil {
		t.Fatalf("a grant whose role cannot authorize the kind was presented: %+v, %v", auth, err)
	}
	boom := errors.New("db down")
	if _, err := (JITAuthority{Grants: &fakeGrants{err: boom}, TenantIDs: ids}).ResolveAuthority(ctx, "acme", "o", operator.KindWorkflowPause, "i"); !errors.Is(err, boom) {
		t.Fatalf("source failure = %v", err)
	}
	if _, err := (JITAuthority{}).ResolveAuthority(ctx, "acme", "o", operator.KindWorkflowPause, "i"); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("unwired authority = %v", err)
	}
	if _, err := (JITAuthority{Grants: source, TenantIDs: KeyedTenantIDs(func(string) uuid.UUID { return uuid.Nil })}).ResolveAuthority(ctx, "acme", "o", operator.KindWorkflowPause, "i"); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("unmapped tenant = %v", err)
	}
	if _, err := KeyedTenantIDs(nil)("acme"); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("nil mapper = %v", err)
	}
	if _, err := ids(""); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("blank tenant = %v", err)
	}

	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	set := NewPlanSet(plan, nil)
	if got, err := set.ResolvePlan(ctx, nil, runtime.Instance{CompiledPlanHash: plan.Digest()}); err != nil || got != plan {
		t.Fatalf("resolve pinned plan = %v, %v", got, err)
	}
	if _, err := set.ResolvePlan(ctx, nil, runtime.Instance{CompiledPlanHash: "sha256:unknown"}); err == nil {
		t.Fatal("an unknown plan digest resolved")
	}
}
