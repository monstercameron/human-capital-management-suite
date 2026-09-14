package repairworkbench_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/repairworkbench"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

var now = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func principal(t *testing.T, tenant string, roles ...string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: "operator:ana", SubjectKind: trust.SubjectKindHuman,
		Roles: roles, Purposes: []string{"operator_diagnostics"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-1", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-1"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func finding(id, tenant, slice string, kind repairworkbench.FindingKind, sev repairworkbench.Severity, age time.Duration) repairworkbench.Finding {
	return repairworkbench.Finding{ID: id, Slice: slice, Tenant: values.TenantId(tenant), Kind: kind, Severity: sev,
		Resource: "workflow_instance", TargetRefs: []string{"ref:" + id}, EvidenceRef: "evidence:" + id, ObservedAt: now.Add(-age)}
}

func findings() []repairworkbench.Finding {
	return []repairworkbench.Finding{
		finding("f-minor", "acme", "promotion", repairworkbench.KindProjectionStale, repairworkbench.SeverityMinor, time.Minute),
		finding("f-critical", "acme", "promotion", repairworkbench.KindWorkflowStuck, repairworkbench.SeverityCritical, time.Minute),
		finding("f-major", "acme", "promotion", repairworkbench.KindOutboxStuck, repairworkbench.SeverityMajor, 2*time.Minute),
		finding("f-other-slice", "acme", "payroll", repairworkbench.KindOutboxStuck, repairworkbench.SeverityCritical, time.Minute),
		finding("f-other-tenant", "globex", "promotion", repairworkbench.KindReconciliationMismatch, repairworkbench.SeverityCritical, time.Minute),
	}
}

// countingExecutor performs repairs behind the real operator gateway.
type countingExecutor struct{ n atomic.Int64 }

func (c *countingExecutor) Apply(_ context.Context, auth operator.Authorization, req operator.Request) (string, error) {
	if err := auth.Require(req.Kind, req.Tenant); err != nil {
		return "", err
	}
	return fmt.Sprintf("repair:%d", c.n.Add(1)), nil
}

func gateway(t *testing.T, exec operator.Executor) *operator.Gateway {
	t.Helper()
	execs := map[operator.Kind]operator.Executor{}
	for _, k := range operator.Kinds() {
		execs[k] = exec
	}
	g, err := operator.NewGateway(operator.NewMemoryJournal(), execs, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func governance(t *testing.T, kind operator.Kind, role jit.Role, scope operator.Scope) operator.Request {
	t.Helper()
	g, err := jit.New("jit-"+string(kind), jit.Request{Principal: "operator:ana", Tenant: "acme", Role: role, TicketRef: "INC-9",
		Justification: "repair", Capabilities: []string{string(kind)}, Purpose: "repair", TTL: time.Hour},
		jit.Approval{Approver: "approver:lead", At: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	return operator.Request{JIT: g, SecondApprover: "operator:ben", Simulation: &operator.Simulation{Digest: "sha256:sim", Scope: scope, At: now.Add(-time.Minute)}}
}

// TestTodo_ALIGN_052 proves the workbench is bounded, tenant- and
// slice-scoped, ordered by urgency, and that a governed repair submitted from
// it reaches the operator gateway exactly once with the finding's scope.
func TestTodo_ALIGN_052(t *testing.T) {
	p := principal(t, "acme", admin.OperatorRole)
	view, err := repairworkbench.Open(p, "promotion", findings())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, item := range view.Items {
		ids = append(ids, item.FindingID)
	}
	if strings.Join(ids, ",") != "f-critical,f-major,f-minor" || view.Truncated {
		t.Fatalf("view items = %v truncated=%v", ids, view.Truncated)
	}
	exec := &countingExecutor{}
	gw := gateway(t, exec)
	stuck := view.Items[0]
	scope := operator.Scope{Resource: "workflow_instance", IDs: []string{"ref:f-critical"}}
	receipt, err := repairworkbench.Submit(context.Background(), gw, p, "promotion", findings(), repairworkbench.Action{
		FindingID: stuck.FindingID, EvidenceDigest: stuck.EvidenceDigest, Kind: operator.KindWorkflowRetryNode,
		IdempotencyKey: "repair-1", Reason: "node stuck", TicketRef: "INC-9",
		Governance: governance(t, operator.KindWorkflowRetryNode, jit.RoleIntegrityRepair, scope),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if receipt.Outcome != operator.OutcomeApplied || exec.n.Load() != 1 || receipt.Scope.IDs[0] != "ref:f-critical" || receipt.Operator != "operator:ana" {
		t.Fatalf("receipt = %+v effects=%d", receipt, exec.n.Load())
	}
}

// TestTodo_ALIGN_052_Property proves the view never exceeds its bound, never
// mixes tenants or slices, and is independent of input order.
func TestTodo_ALIGN_052_Property(t *testing.T) {
	p := principal(t, "acme", admin.OperatorRole)
	for _, n := range []int{0, 1, repairworkbench.MaxItems, repairworkbench.MaxItems + 7} {
		var in []repairworkbench.Finding
		for i := range n {
			in = append(in, finding(fmt.Sprintf("f-%03d", i), "acme", "promotion", repairworkbench.KindOutboxStuck, repairworkbench.SeverityMajor, time.Duration(i)*time.Second))
			in = append(in, finding(fmt.Sprintf("g-%03d", i), "globex", "promotion", repairworkbench.KindOutboxStuck, repairworkbench.SeverityCritical, 0))
		}
		forward, err := repairworkbench.Open(p, "promotion", in)
		if err != nil {
			t.Fatal(err)
		}
		reversed := make([]repairworkbench.Finding, len(in))
		for i := range in {
			reversed[len(in)-1-i] = in[i]
		}
		backward, _ := repairworkbench.Open(p, "promotion", reversed)
		if forward.Digest() != backward.Digest() {
			t.Fatalf("n=%d: view depends on input order", n)
		}
		if len(forward.Items) > repairworkbench.MaxItems || forward.Truncated != (n > repairworkbench.MaxItems) {
			t.Fatalf("n=%d: %d items truncated=%v", n, len(forward.Items), forward.Truncated)
		}
		for _, item := range forward.Items {
			if strings.HasPrefix(item.FindingID, "g-") {
				t.Fatalf("n=%d: foreign tenant finding %s in view", n, item.FindingID)
			}
		}
	}
}

// TestTodo_ALIGN_052_Golden pins the view encoding.
func TestTodo_ALIGN_052_Golden(t *testing.T) {
	p := principal(t, "acme", admin.OperatorRole)
	view, err := repairworkbench.Open(p, "promotion", findings()[:1])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(view)
	want := `{"contract_version":1,"tenant":"acme","slice":"promotion","items":[{"finding_id":"f-minor","kind":"PROJECTION_STALE","severity":"MINOR","resource":"workflow_instance","target_count":1,"evidence_digest":"` +
		findings()[0].EvidenceDigest() + `","actions":["PROJECTION_REBUILD"]}],"truncated":false}`
	if string(b) != want {
		t.Fatalf("view = %s\nwant   %s", b, want)
	}
}

// TestTodo_ALIGN_052_Security refuses non-operators, and refuses actions on
// foreign findings, inadmissible actions and stale evidence before the
// gateway sees them.
func TestTodo_ALIGN_052_Security(t *testing.T) {
	if _, err := repairworkbench.Open(principal(t, "acme"), "promotion", findings()); !errors.Is(err, repairworkbench.ErrUnauthorized) {
		t.Fatalf("non-operator open = %v", err)
	}
	if _, err := repairworkbench.Open(nil, "promotion", findings()); !errors.Is(err, repairworkbench.ErrUnauthorized) {
		t.Fatalf("nil principal open = %v", err)
	}
	p := principal(t, "acme", admin.OperatorRole)
	exec := &countingExecutor{}
	gw := gateway(t, exec)
	all := findings()
	byID := map[string]repairworkbench.Finding{}
	for _, f := range all {
		byID[f.ID] = f
	}
	for name, a := range map[string]repairworkbench.Action{
		"foreign tenant": {FindingID: "f-other-tenant", EvidenceDigest: byID["f-other-tenant"].EvidenceDigest(), Kind: operator.KindDatabaseRepair},
		"other slice":    {FindingID: "f-other-slice", EvidenceDigest: byID["f-other-slice"].EvidenceDigest(), Kind: operator.KindConnectorRedrive},
		"unknown":        {FindingID: "f-none", Kind: operator.KindConnectorRedrive},
		"inadmissible":   {FindingID: "f-minor", EvidenceDigest: byID["f-minor"].EvidenceDigest(), Kind: operator.KindKeyRotation},
		"stale evidence": {FindingID: "f-major", EvidenceDigest: "sha256:old", Kind: operator.KindConnectorRedrive},
	} {
		_, err := repairworkbench.Submit(context.Background(), gw, p, "promotion", all, a)
		want := map[string]error{"foreign tenant": repairworkbench.ErrNotInView, "other slice": repairworkbench.ErrNotInView, "unknown": repairworkbench.ErrNotInView,
			"inadmissible": repairworkbench.ErrActionRefused, "stale evidence": repairworkbench.ErrStaleEvidence}[name]
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", name, err, want)
		}
	}
	if exec.n.Load() != 0 {
		t.Fatalf("refused actions performed %d repairs", exec.n.Load())
	}
	if _, err := repairworkbench.Submit(context.Background(), gw, principal(t, "acme"), "promotion", all, repairworkbench.Action{FindingID: "f-minor"}); !errors.Is(err, repairworkbench.ErrUnauthorized) {
		t.Fatalf("non-operator submit = %v", err)
	}
}

// TestTodo_ALIGN_052_Integration proves the workbench composes with the real
// gateway's governance: a repair without the dual control its kind demands is
// refused by the gateway, a governed one applies, and a repeat replays.
func TestTodo_ALIGN_052_Integration(t *testing.T) {
	p := principal(t, "acme", admin.OperatorRole)
	exec := &countingExecutor{}
	gw := gateway(t, exec)
	mismatch := finding("f-recon", "acme", "promotion", repairworkbench.KindReconciliationMismatch, repairworkbench.SeverityCritical, time.Minute)
	all := []repairworkbench.Finding{mismatch}
	scope := operator.Scope{Resource: "workflow_instance", IDs: []string{"ref:f-recon"}}
	gov := governance(t, operator.KindDatabaseRepair, jit.RoleIntegrityRepair, scope)
	undual := gov
	undual.SecondApprover = ""
	action := repairworkbench.Action{FindingID: "f-recon", EvidenceDigest: mismatch.EvidenceDigest(), Kind: operator.KindDatabaseRepair,
		IdempotencyKey: "recon-1", Reason: "mismatch", TicketRef: "INC-9", Governance: undual}
	if _, err := repairworkbench.Submit(context.Background(), gw, p, "promotion", all, action); operator.CodeOf(err) != operator.CodeDualControlRequired {
		t.Fatalf("undual repair = %v", err)
	}
	action.Governance = gov
	first, err := repairworkbench.Submit(context.Background(), gw, p, "promotion", all, action)
	if err != nil || first.Outcome != operator.OutcomeApplied {
		t.Fatalf("governed repair = %+v, %v", first, err)
	}
	again, err := repairworkbench.Submit(context.Background(), gw, p, "promotion", all, action)
	if err != nil || again.Outcome != operator.OutcomeDuplicate || exec.n.Load() != 1 {
		t.Fatalf("repeat = %+v, %v, effects %d", again, err, exec.n.Load())
	}
}

// TestTodo_ALIGN_052_Fault rejects malformed findings and wiring.
func TestTodo_ALIGN_052_Fault(t *testing.T) {
	p := principal(t, "acme", admin.OperatorRole)
	bad := finding("f-bad", "acme", "promotion", "BESPOKE", repairworkbench.SeverityMajor, 0)
	if _, err := repairworkbench.Open(p, "promotion", []repairworkbench.Finding{bad}); !errors.Is(err, repairworkbench.ErrInvalid) {
		t.Fatalf("unknown kind = %v", err)
	}
	noRefs := finding("f-norefs", "acme", "promotion", repairworkbench.KindOutboxStuck, repairworkbench.SeverityMajor, 0)
	noRefs.TargetRefs = nil
	if _, err := repairworkbench.Open(p, "promotion", []repairworkbench.Finding{noRefs}); !errors.Is(err, repairworkbench.ErrInvalid) {
		t.Fatalf("no target refs = %v", err)
	}
	if _, err := repairworkbench.Open(p, " ", findings()); !errors.Is(err, repairworkbench.ErrInvalid) {
		t.Fatalf("blank slice = %v", err)
	}
	if _, err := repairworkbench.Submit(context.Background(), nil, p, "promotion", findings(), repairworkbench.Action{}); !errors.Is(err, repairworkbench.ErrInvalid) {
		t.Fatalf("nil gateway = %v", err)
	}
	badSev := finding("f-sev", "acme", "promotion", repairworkbench.KindOutboxStuck, "URGENT", 0)
	if _, err := repairworkbench.Open(p, "promotion", []repairworkbench.Finding{badSev}); !errors.Is(err, repairworkbench.ErrInvalid) {
		t.Fatalf("unknown severity = %v", err)
	}
}

// TestTodo_ALIGN_052_Conformance pins the closed kind-to-action map: every
// admitted action is a registered operator kind, and no view item carries a
// payload-bearing field.
func TestTodo_ALIGN_052_Conformance(t *testing.T) {
	registered := map[operator.Kind]bool{}
	for _, k := range operator.Kinds() {
		registered[k] = true
	}
	for _, k := range []repairworkbench.FindingKind{repairworkbench.KindProjectionStale, repairworkbench.KindOutboxStuck, repairworkbench.KindWorkflowStuck, repairworkbench.KindReconciliationMismatch} {
		actions := repairworkbench.ActionsFor(k)
		if len(actions) == 0 {
			t.Errorf("%s admits no action", k)
		}
		for _, a := range actions {
			if _, ok := operator.PolicyFor(a); !ok || !registered[a] {
				t.Errorf("%s admits unregistered action %s", k, a)
			}
		}
	}
	if repairworkbench.ActionsFor("BESPOKE") != nil {
		t.Error("an unknown kind admits actions")
	}
	b, _ := json.Marshal(repairworkbench.Item{})
	for _, forbidden := range []string{"payload", "value", "target_refs"} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("view item carries %q", forbidden)
		}
	}
}
