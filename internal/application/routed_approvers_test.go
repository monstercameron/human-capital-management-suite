package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// routedApproverContexts authenticates the two principals the executable plan
// routes a corpus worker's approvals to when a composition names no finance
// partner: PROMOUX-003's class-scoped derivations of cfg.ExecutionApprover
// (the corpus carries no manager relationship to resolve). PROMOUX-015 made a
// decision the caller's own, so the tests that used to decide both approvals
// as their proposer now decide each one as the routed assignee. Neither
// credential holds the execution role: membership, not that role, is a
// decision's authority. comp_admin is only what lets them read the corpus
// worker's compensation to re-simulate the proposal they decide.
func routedApproverContexts(t *testing.T, verifier *trust.HMACVerifier, cfg ServeConfig, at time.Time) (finance, manager context.Context) {
	t.Helper()
	contextFor := func(subject string) context.Context {
		token, err := verifier.Issue(trust.Claims{
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: subject, SubjectKind: "human", Tenant: cfg.Tenant,
			OrganizationScopeID: "org-north-america", Roles: []string{"comp_admin"},
			Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: "session:" + subject, IssuedAtUnix: at.Add(-time.Minute).Unix(), ExpiresAtUnix: at.Add(48 * time.Hour).Unix(),
		})
		if err != nil {
			t.Fatalf("issue routed approver credential %s: %v", subject, err)
		}
		principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
		if err != nil {
			t.Fatalf("verify routed approver credential %s: %v", subject, err)
		}
		return trust.WithPrincipal(context.Background(), principal)
	}
	financeSubject, err := promotionexec.FinanceApproverFor(cfg.ExecutionApprover)
	if err != nil {
		t.Fatalf("FinanceApproverFor: %v", err)
	}
	managerSubject, err := promotionexec.ManagerApproverFor(cfg.ExecutionApprover)
	if err != nil {
		t.Fatalf("ManagerApproverFor: %v", err)
	}
	return contextFor(financeSubject), contextFor(managerSubject)
}
