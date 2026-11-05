// TestTodo_SMB_SETUP_003_Integration proves SMB-SETUP-003 adoption records
// persist through the existing governed registry stores: a compiled starter
// adoption publishes as an immutable configuration object, activates once,
// survives a restarted store handle, refuses duplicate publication, and
// stays invisible across tenants.
package configregistry_test

import (
	"encoding/json"
	"testing"
	"time"

	dataconfigregistry "github.com/monstercameron/human-capital-management-suite/internal/data/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
	wfversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func smbStarterAdoptionFixture(t *testing.T) []byte {
	t.Helper()
	modules, err := configbundle.CompileBusinessModules(
		[]configbundle.BusinessModuleManifest{
			{ModuleID: "party", Version: "1.0.0", Owner: "crm", Capabilities: []string{"crm.party.v1"}},
			{ModuleID: "inventory", Version: "1.0.0", Owner: "stock", Dependencies: []string{"party"}, Capabilities: []string{"stock.inventory.v1"}},
		},
		map[string]bool{"crm.party.v1": true, "stock.inventory.v1": true},
	)
	if err != nil {
		t.Fatalf("compile modules: %v", err)
	}
	workflows := wfversion.NewRegistry()
	if err := workflows.Put(wfversion.CompiledVersion{WorkflowID: "onboard-customer", CompiledPlanDigest: "plan-onboard", SemanticVersion: "1.0.0", Status: wfversion.StatusActive}); err != nil {
		t.Fatalf("put workflow: %v", err)
	}
	registry := configbundle.NewStarterProfileRegistry(
		configbundle.NewSetupPlanner(modules, promotion.NewRegistry()),
		configbundle.StarterCatalog{Modules: modules, Workflows: workflows, Terminology: map[string]bool{"engagement": true}},
	)
	compiled, err := registry.Compile([]configbundle.StarterProfileManifest{{
		ProfileID: "professional-services", Version: "1.0.0",
		Modules:     []string{"party"},
		Workflows:   []configbundle.StarterWorkflowRef{{WorkflowID: "onboard-customer", CompiledPlanDigest: "plan-onboard"}},
		Terminology: []string{"engagement"},
		Settings:    map[string]string{"approval.discount.threshold": "200"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	adoption, err := registry.Apply(configbundle.StarterAdoptionRequest{
		Actor: "ada.admin", AdoptionID: "adopt-1", Scope: configbundle.Scope{TenantID: "tenant-a"},
		ProfileID: "professional-services", CompiledDigest: compiled[0].Digest,
		Baseline: configbundle.BaselinePin{
			PackageID: "starter-baseline", Digest: "abc123", Scope: configbundle.Scope{TenantID: "tenant-a"},
			Approver: "release-authority", ExpiresAt: at.Add(24 * time.Hour),
		},
		Overrides:      map[string]string{"approval.discount.threshold": "500"},
		IdempotencyKey: "key-1", At: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(adoption)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestTodo_SMB_SETUP_003_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "starter-adoption")
	store := dataconfigregistry.New(appConn(t, db))
	body := smbStarterAdoptionFixture(t)
	scope := platformconfig.Scope{TenantID: tenantID.String()}
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

	obj, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: platformconfig.KindPolicy, ID: "starter-adoption:adopt-1", Revision: 1,
		Body: body, SchemaRef: "hcmnext.starter.adoption/v1",
		Scope: scope, PublisherPrincipal: "ada.admin", PublishedAt: at,
	})
	if err != nil {
		t.Fatalf("Publish adoption: %v", err)
	}

	// Restarted store handle reads the same durable record.
	restarted := dataconfigregistry.New(appConn(t, db))
	got, found, err := restarted.GetObject(obj.Ref())
	if err != nil || !found {
		t.Fatalf("post-restart GetObject = found %v, err=%v", found, err)
	}
	if got.Digest() != obj.Digest() || string(got.Body) != string(body) {
		t.Fatal("post-restart record differs from the published adoption")
	}
	if err := got.Verify(); err != nil {
		t.Fatalf("Verify on a round-tripped adoption: %v", err)
	}

	// Republishing the identical body is idempotent; a different body under
	// the same revision is refused. History keeps exactly one row.
	republished, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: platformconfig.KindPolicy, ID: "starter-adoption:adopt-1", Revision: 1,
		Body: body, SchemaRef: "hcmnext.starter.adoption/v1",
		Scope: scope, PublisherPrincipal: "ada.admin", PublishedAt: at,
	})
	if err != nil || republished.Digest() != obj.Digest() {
		t.Fatalf("identical republish = %+v, err=%v; want the existing record", republished, err)
	}
	if _, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: platformconfig.KindPolicy, ID: "starter-adoption:adopt-1", Revision: 1,
		Body: []byte(`{"tampered":true}`), SchemaRef: "hcmnext.starter.adoption/v1",
		Scope: scope, PublisherPrincipal: "ada.admin", PublishedAt: at,
	}); err == nil {
		t.Fatal("conflicting adoption publication succeeded, want revision-conflict refusal")
	}
	revisions, err := restarted.ListRevisions(scope, platformconfig.KindPolicy, "starter-adoption:adopt-1")
	if err != nil || len(revisions) != 1 {
		t.Fatalf("ListRevisions = %d entries, err=%v; want exactly one", len(revisions), err)
	}

	// One governed activation act, visible after restart.
	if _, err := platformconfig.Activate(store, obj.Ref(), platformconfig.ActivationEvidence{
		ActivatedBy: "ada.admin", Authority: "release-authority", Reason: "starter adoption", ActivatedAt: at,
	}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	active, found, err := restarted.GetLatestActivation(scope, platformconfig.KindPolicy, "starter-adoption:adopt-1")
	if err != nil || !found {
		t.Fatalf("post-restart GetLatestActivation = found %v, err=%v", found, err)
	}
	if active.ObjectDigest != obj.Digest() || active.Revision != 1 || active.ActivatedBy != "ada.admin" {
		t.Fatalf("activation record = %+v, want the published adoption activation", active)
	}

	// Another tenant sees nothing under the same object id.
	otherID := insertTenant(t, db, "starter-adoption-other")
	otherScope := platformconfig.Scope{TenantID: otherID.String()}
	if _, found, err := restarted.GetObject(platformconfig.ObjectRef{Scope: otherScope, Kind: platformconfig.KindPolicy, ID: "starter-adoption:adopt-1", Revision: 1}); err != nil || found {
		t.Fatalf("cross-tenant GetObject = found %v, err=%v; want not-found", found, err)
	}
}
