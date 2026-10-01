package application

import (
	"testing"
	"time"

	connectivityapplication "github.com/monstercameron/human-capital-management-suite/internal/connectivity/application"
)

// TestTodo_APP_005_Served proves the shipped serve composition reaches the
// installation lifecycle and that its control decision remains fail-closed
// after composition, rather than merely linking the package as dead code.
func TestTodo_APP_005_Served(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())

	lifecycle := composed.InstallationLifecycle()
	if lifecycle == nil {
		t.Fatal("serve composition returned no installation lifecycle")
	}
	component, ok := composed.Graph().Component(ComponentInstallationLifecycle)
	if !ok || component.Kind != KindGovernance {
		t.Fatalf("installation lifecycle graph component = %+v, present=%t", component, ok)
	}

	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	installation := connectivityapplication.Installation{
		ID: "served-installation", TenantID: "tenant-served", CellID: "cell-served", ApplicationID: "app-served",
		Version: "v1", VersionDigest: "digest-v1", Revision: 1, State: connectivityapplication.Active,
		Scope: connectivityapplication.Scope{
			TenantID: "tenant-served", OrganizationID: "org-served", Population: "workers",
			DataClasses: []string{"identity"}, Fields: []string{"worker.name"}, Purpose: "workforce",
			Capabilities: []string{"workers.read"},
		},
		TokenRefs: []string{"token:served"}, SubscriptionRefs: []string{"subscription:served"}, EffectRefs: []string{"effect:served"},
		CreatedAt: at, UpdatedAt: at,
	}
	if err := lifecycle.Register(installation); err != nil {
		t.Fatalf("Register served installation: %v", err)
	}
	controlled, err := lifecycle.Quarantine(installation.ID, "operator", "approver", "served incident", "evidence:served", at.Add(time.Minute))
	if err != nil {
		t.Fatalf("Quarantine served installation: %v", err)
	}
	if controlled.Current.State != connectivityapplication.Quarantined || !controlled.Receipt.WithinSLO {
		t.Fatalf("served quarantine = %+v, want quarantined within SLO", controlled)
	}
	if allowed, reason := lifecycle.AuthorizeUse(installation.ID); allowed || reason != "INSTALLATION_CONTROLLED" {
		t.Fatalf("served authorization = %t, %q; want fail-closed control", allowed, reason)
	}
}
