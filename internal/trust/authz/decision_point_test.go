package authz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/breakglass"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

type decisionAudit struct {
	events []authz.SensitiveDecisionEvidence
	err    error
}

func (a *decisionAudit) RecordSensitiveDecision(_ context.Context, event authz.SensitiveDecisionEvidence) error {
	a.events = append(a.events, event)
	return a.err
}

func decisionPointAuthorities(t *testing.T, req authz.Request, capability string) (*trust.EffectiveAuthority, *jit.Grant, *breakglass.Grant) {
	t.Helper()
	now := baseTime
	fields := make([]string, 0, len(req.Fields))
	for _, field := range req.Fields {
		fields = append(fields, string(field))
	}
	delegation := &trust.EffectiveAuthority{
		Delegate: req.Principal.Subject(), Tenant: req.Principal.Tenant(),
		Capabilities: []string{capability}, Resources: []string{req.Subject.String()},
		Fields: fields, Purposes: []string{req.Purpose}, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	jitGrant, err := jit.New("jit-1", jit.Request{
		Principal: req.Principal.Subject(), Tenant: req.Principal.Tenant(), Role: jit.RoleIncidentResponder,
		TicketRef: "INC-1", Justification: "restore service", Capabilities: []string{capability}, Fields: fields,
		Purpose: req.Purpose, TTL: time.Hour,
	}, jit.Approval{Approver: "approver", At: now.Add(-time.Minute)}, now)
	if err != nil {
		t.Fatalf("jit.New: %v", err)
	}
	breakGrant, err := breakglass.Open("bg-1", breakglass.Request{
		User: req.Principal.Subject(), IncidentRef: "INC-1", Justification: "restore service",
		Capabilities: []string{capability}, TTL: time.Hour,
	}, breakglass.Approval{Approver: "approver", At: now.Add(-time.Minute)}, now)
	if err != nil {
		t.Fatalf("breakglass.Open: %v", err)
	}
	return delegation, jitGrant, breakGrant
}

func TestTodo_RBAC_RT_013(t *testing.T) {
	req := allowedRequest(t)
	audit := &decisionAudit{}
	point := authz.NewDecisionPoint(audit, authz.WithDecisionPointClock(func() time.Time { return baseTime }))
	delegation, jitGrant, breakGrant := decisionPointAuthorities(t, req, "worker.read")
	decision, err := point.Decide(context.Background(), authz.DecisionPointRequest{
		Policy: req, Capability: "worker.read", Actor: req.Principal.Subject(), Delegation: delegation,
		JIT: jitGrant, BreakGlass: breakGrant, Sensitive: true, Action: "read worker record",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.EvidenceID == "" || len(audit.events) != 1 {
		t.Fatalf("decision/audit = %q/%d, want evidence and one audit event", decision.EvidenceID, len(audit.events))
	}
	if audit.events[0].Outcome != "ALLOWED" || audit.events[0].Capability != "worker.read" {
		t.Fatalf("audit event = %+v", audit.events[0])
	}
	if len(jitGrant.Evidence()) != 2 || len(breakGrant.Evidence()) != 2 {
		t.Fatal("successful decision did not record both bounded authority uses")
	}
}

func TestTodo_RBAC_RT_013_Security(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*authz.DecisionPointRequest)
	}{
		{name: "delegation cannot widen capability", mutate: func(r *authz.DecisionPointRequest) { r.Delegation.Capabilities = []string{"payroll.write"} }},
		{name: "delegation cannot widen subject", mutate: func(r *authz.DecisionPointRequest) { r.Delegation.Resources = []string{"other-subject"} }},
		{name: "expired jit is refused", mutate: func(r *authz.DecisionPointRequest) { r.JIT.ExpiresAt = baseTime.Add(-time.Second) }},
		{name: "break glass cannot replace policy", mutate: func(r *authz.DecisionPointRequest) { r.Policy.Subject.Tenant = tenantVendor }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := allowedRequest(t)
			audit := &decisionAudit{}
			point := authz.NewDecisionPoint(audit, authz.WithDecisionPointClock(func() time.Time { return baseTime }))
			delegation, jitGrant, breakGrant := decisionPointAuthorities(t, req, "worker.read")
			call := authz.DecisionPointRequest{Policy: req, Capability: "worker.read", Actor: req.Principal.Subject(), Delegation: delegation, JIT: jitGrant, BreakGlass: breakGrant, Sensitive: true, Action: "read worker record"}
			tc.mutate(&call)
			if _, err := point.Decide(context.Background(), call); err == nil || !errors.Is(err, authz.ErrDecisionPointDenied) {
				t.Fatalf("Decide error = %v, want policy decision denial", err)
			}
			if len(audit.events) != 1 || audit.events[0].Outcome != "DENIED" {
				t.Fatalf("denied decision audit = %+v", audit.events)
			}
		})
	}
}

func TestTodo_RBAC_RT_013_Property(t *testing.T) {
	req := allowedRequest(t)
	point := authz.NewDecisionPoint(nil, authz.WithDecisionPointClock(func() time.Time { return baseTime }))
	delegation, jitGrant, breakGrant := decisionPointAuthorities(t, req, "worker.read")
	for _, capability := range []string{"worker.read", "worker.write", "payroll.write"} {
		call := authz.DecisionPointRequest{Policy: req, Capability: capability, Actor: req.Principal.Subject(), Delegation: delegation, JIT: jitGrant, BreakGlass: breakGrant, Action: "read worker record"}
		_, err := point.Decide(context.Background(), call)
		if capability == "worker.read" && err != nil {
			t.Fatalf("covered capability refused: %v", err)
		}
		if capability != "worker.read" && err == nil {
			t.Fatalf("uncovered capability %q was allowed", capability)
		}
	}
}

func TestTodo_RBAC_RT_013_Integration(t *testing.T) {
	req := allowedRequest(t)
	delegation, jitGrant, breakGrant := decisionPointAuthorities(t, req, "worker.read")
	point := authz.NewDecisionPoint(nil, authz.WithDecisionPointClock(func() time.Time { return baseTime }))
	call := authz.DecisionPointRequest{Policy: req, Capability: "worker.read", Actor: req.Principal.Subject(), Delegation: delegation, JIT: jitGrant, BreakGlass: breakGrant, Sensitive: true, Action: "read worker record"}
	if _, err := point.Decide(context.Background(), call); !errors.Is(err, authz.ErrSensitiveAuditRequired) {
		t.Fatalf("sensitive decision without audit = %v, want ErrSensitiveAuditRequired", err)
	}
	if len(jitGrant.Evidence()) != 1 || len(breakGrant.Evidence()) != 1 {
		t.Fatalf("missing audit consumed grant evidence: jit=%d break-glass=%d", len(jitGrant.Evidence()), len(breakGrant.Evidence()))
	}
	audit := &decisionAudit{err: errors.New("ledger unavailable")}
	point = authz.NewDecisionPoint(audit, authz.WithDecisionPointClock(func() time.Time { return baseTime }))
	delegation, jitGrant, breakGrant = decisionPointAuthorities(t, req, "worker.read")
	call.Delegation, call.JIT, call.BreakGlass = delegation, jitGrant, breakGrant
	if _, err := point.Decide(context.Background(), call); !errors.Is(err, authz.ErrSensitiveAuditRequired) {
		t.Fatalf("audit failure = %v, want ErrSensitiveAuditRequired", err)
	}
	if len(jitGrant.Evidence()) != 1 || len(breakGrant.Evidence()) != 1 {
		t.Fatalf("failed audit consumed grant evidence: jit=%d break-glass=%d", len(jitGrant.Evidence()), len(breakGrant.Evidence()))
	}
}
