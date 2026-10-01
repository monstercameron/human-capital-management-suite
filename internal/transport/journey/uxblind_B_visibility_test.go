package journey

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_UXBLIND_004(t *testing.T) {
	principal := uxblindBPrincipal(t, "darius")
	all := []workspace.WorkerSummary{
		{WorkerRef: "darius"},
		{WorkerRef: "linh", ManagerRef: "darius"},
		{WorkerRef: "skip-level", ManagerRef: "linh"},
		{WorkerRef: "outside", ManagerRef: "unrelated"},
	}
	visible, _, err := withManagedReports(principal, []string{"hiring_manager", "manager"}, all, all[:1], workspace.WorkforceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(visible))
	for _, worker := range visible {
		seen[worker.WorkerRef] = true
	}
	for _, want := range []string{"darius", "linh", "skip-level"} {
		if !seen[want] {
			t.Fatalf("reporting-line worker %q was hidden: %v", want, seen)
		}
	}
	if seen["outside"] {
		t.Fatalf("worker outside the reporting line was disclosed: %v", seen)
	}
}

func TestTodo_UXBLIND_004_Browser(t *testing.T) {
	byRef := map[string]workspace.WorkerSummary{
		"darius": {WorkerRef: "darius"},
		"linh":   {WorkerRef: "linh", ManagerRef: "darius"},
	}
	if !reportsTo(byRef["linh"], "darius", byRef) {
		t.Fatal("direct report did not resolve to its manager")
	}
}

func TestTodo_UXBLIND_004_Security(t *testing.T) {
	byRef := map[string]workspace.WorkerSummary{
		"darius":    {WorkerRef: "darius"},
		"linh":      {WorkerRef: "linh", ManagerRef: "darius"},
		"unrelated": {WorkerRef: "unrelated"},
		"outside":   {WorkerRef: "outside", ManagerRef: "unrelated"},
	}
	if reportsTo(byRef["outside"], "darius", byRef) {
		t.Fatal("outside reporting line was treated as a report")
	}
}

func uxblindBPrincipal(t *testing.T, subject string) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "acme-corp", Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "uxblind-b-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "uxblind-b-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}
