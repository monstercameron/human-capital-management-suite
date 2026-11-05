package app

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentdefinitions "github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// UXBLIND-005: the inspect path projects executability from the same
// execution-authority gate Execute enforces. A caller the gate refuses (the
// live Luis Ortega case: initiator without the execution role) gets
// CanExecute=false; composition without an authority, or an unresolvable
// definition, fails closed.
func TestTodo_UXBLIND_005_CanExecute(t *testing.T) {
	reg, err := intentdefinitions.NewRegistry()
	if err != nil {
		t.Fatalf("definitions.NewRegistry: %v", err)
	}
	var ref intent.Ref
	for _, d := range intentdefinitions.All() {
		if d.Ref.TypeID == promotion.IntentType {
			ref = d.Ref
		}
	}
	if ref.TypeID == "" {
		t.Fatalf("live catalog has no %s", promotion.IntentType)
	}
	newEngine := func(authority *ExecutionAuthority) *journeyEngine {
		return &journeyEngine{svc: &IntentService{defs: reg, executionAuthority: authority}}
	}
	principal := func(t *testing.T, roles ...string) *trust.Principal {
		t.Helper()
		now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
		p, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant: "acme-corp", Subject: "viewer-1", SubjectKind: trust.SubjectKindHuman,
			Roles: roles, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance: trust.AssuranceHigh, SessionRef: "uxblind-005",
			IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "uxblind-005",
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	authority := &ExecutionAuthority{
		AdmittedIntentTypes: map[string]bool{promotion.IntentType: true},
		RequiredRole:        "promotion_executor",
	}

	if !newEngine(authority).journeyExecuteCapability(principal(t, "promotion_executor"), ref) {
		t.Fatal("caller with the execution role cannot execute")
	}
	if newEngine(authority).journeyExecuteCapability(principal(t, "hiring_manager"), ref) {
		t.Fatal("caller without the execution role can execute")
	}
	if newEngine(nil).journeyExecuteCapability(principal(t, "promotion_executor"), ref) {
		t.Fatal("composition without an execution authority can execute")
	}
	if newEngine(authority).journeyExecuteCapability(principal(t, "promotion_executor"), intent.Ref{}) {
		t.Fatal("unresolvable definition can execute")
	}
	var nilEngine *journeyEngine
	if nilEngine.journeyExecuteCapability(principal(t, "promotion_executor"), ref) {
		t.Fatal("nil engine can execute")
	}
}
