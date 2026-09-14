package workflowcontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestPreflightSimulationSealsRetryEvidence proves Simulate is a dry run --
// the predicted retry writes nothing -- and that a controller composed with
// WithPreflightSimulation retries a failed idempotent node without
// operator-held simulation evidence, sealing the prediction into the receipt,
// while a controller without it still refuses.
func TestPreflightSimulationSealsRetryEvidence(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.auth.simulate = false
	failed, _ := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}})
	cmd := retryCmd(f, failed, "execute_promotion", 1, "retry-preflight")

	plain := f.controller(operator.NewMemoryJournal())
	r, err := plain.RetryNode(ctx, cmd)
	expect(t, "retry without preflight", r, err, OutcomeDenied, operator.CodeSimulationRequired)

	journal := operator.NewMemoryJournal()
	c, err := New(f.conn, journal, plans{f.plan}, f.auth, func() time.Time { return fixedNow }, nil, WithPreflightSimulation())
	if err != nil {
		t.Fatal(err)
	}
	sim, predicted, err := c.Simulate(ctx, operator.KindWorkflowRetryNode, cmd)
	if err != nil || predicted.Outcome != OutcomeApplied || predicted.Attempt != 2 || !strings.HasPrefix(sim.Digest, "sha256:") ||
		!sim.At.Equal(fixedNow) || sim.Scope.IDs[0] != failed.String()+"/execute_promotion#1" {
		t.Fatalf("simulate = %+v, %+v, %v", sim, predicted, err)
	}
	if _, nodes := f.load(failed); len(nodes) != 1 || f.readyWork(failed, "execute_promotion") != 0 {
		t.Fatalf("the dry run wrote: %d attempts, %d ready work", len(nodes), f.readyWork(failed, "execute_promotion"))
	}
	again, _, err := c.Simulate(ctx, operator.KindWorkflowRetryNode, cmd)
	if err != nil || again.Digest != sim.Digest {
		t.Fatalf("simulation is not deterministic: %v vs %v (%v)", again, sim, err)
	}

	retried, err := c.RetryNode(ctx, cmd)
	expect(t, "retry with preflight", retried, err, OutcomeApplied, "")
	if retried.Attempt != 2 || f.readyWork(failed, "execute_promotion") != 1 {
		t.Fatalf("retry = %+v, ready work %d", retried, f.readyWork(failed, "execute_promotion"))
	}
	receipt, ok, err := journal.Lookup(ctx, f.key, cmd.IdempotencyKey)
	if err != nil || !ok || receipt.SimulationDigest != sim.Digest {
		t.Fatalf("receipt simulation = %q (ok %v, %v), want %s", receipt.SimulationDigest, ok, err, sim.Digest)
	}

	// A simulation the operator already holds is never replaced.
	f.auth.simulate = true
	held, _ := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}})
	heldCmd := retryCmd(f, held, "execute_promotion", 1, "retry-held-sim")
	r, err = c.RetryNode(ctx, heldCmd)
	expect(t, "retry with held simulation", r, err, OutcomeApplied, "")
	if receipt, _, _ := journal.Lookup(ctx, f.key, heldCmd.IdempotencyKey); receipt.SimulationDigest != "sha256:sim" {
		t.Fatalf("preflight replaced the operator's simulation: %q", receipt.SimulationDigest)
	}

	// Refusals and failures.
	if _, _, err := c.Simulate(ctx, operator.KindWorkflowRetryNode, Command{}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("invalid simulate = %v", err)
	}
	if _, _, err := c.Simulate(ctx, operator.KindConnectorRedrive, f.cmd(held, 1, "k")); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("non-control simulate = %v", err)
	}
	if sim, res, err := c.Simulate(ctx, operator.KindWorkflowCancel, f.cmd(uuid.New(), 1, "k")); err != nil || res.Code != runtime.CodeInstanceNotFound || sim == nil {
		t.Fatalf("simulate a missing instance = %+v, %+v, %v", sim, res, err)
	}
	broken, err := New(failingBeginner{}, operator.NewMemoryJournal(), plans{f.plan}, f.auth, nil, WithPreflightSimulation())
	if err != nil {
		t.Fatal(err)
	}
	f.auth.simulate = false
	if _, err := broken.RetryNode(ctx, cmd); err == nil || !strings.Contains(err.Error(), "preflight simulation") {
		t.Fatalf("preflight failure = %v", err)
	}
}

// TestInstanceScopedGrantCarriesDualControl proves a JIT grant narrowed to the
// addressed instance presents its distinct approver as the second approval,
// that a broad grant or a grant for another instance presents none, and that
// kinds without dual control never borrow an approver.
func TestInstanceScopedGrantCarriesDualControl(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	ids := KeyedTenantIDs(func(string) uuid.UUID { return tenant })
	instance := uuid.NewString()
	scoped := func(field string) *jit.Grant {
		g, err := jit.Restore(jit.Stored{ID: "g-" + field, Principal: "operator:ana", Tenant: "acme", Role: jit.RoleIncidentResponder, TicketRef: "INC-1",
			Justification: "j", Capabilities: []string{string(operator.KindWorkflowCancel), string(operator.KindWorkflowPause)}, Fields: []string{field},
			Purpose: "p", Approver: "approver:lead", IssuedAt: fixedNow.Add(-time.Minute), ExpiresAt: fixedNow.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	broad := restored(t, jit.RoleIncidentResponder, string(operator.KindWorkflowCancel))
	cases := []struct {
		name   string
		grants []*jit.Grant
		kind   operator.Kind
		second string
	}{
		{"instance-scoped cancel", []*jit.Grant{broad, scoped(InstanceApprovalField(instance))}, operator.KindWorkflowCancel, "approver:lead"},
		{"broad cancel", []*jit.Grant{broad}, operator.KindWorkflowCancel, ""},
		{"other instance", []*jit.Grant{scoped(InstanceApprovalField(uuid.NewString()))}, operator.KindWorkflowCancel, ""},
		{"pause never borrows", []*jit.Grant{scoped(InstanceApprovalField(instance))}, operator.KindWorkflowPause, ""},
	}
	for _, tc := range cases {
		a := JITAuthority{Grants: &fakeGrants{grants: tc.grants}, TenantIDs: ids, Clock: func() time.Time { return fixedNow }}
		auth, err := a.ResolveAuthority(ctx, "acme", "operator:ana", tc.kind, instance)
		if err != nil || auth.JIT == nil || auth.SecondApprover != tc.second {
			t.Fatalf("%s: authority = %+v, %v; want second approver %q", tc.name, auth, err, tc.second)
		}
	}
}

type failingBeginner struct{}

func (failingBeginner) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("database unavailable")
}
