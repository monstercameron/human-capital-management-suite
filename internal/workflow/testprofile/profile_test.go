package testprofile

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_WF_TEST_003(t *testing.T) {
	for _, invalid := range []ProfileRef{{}, {ID: "HarborCare", Version: 1}, {ID: "harborcare-promotion", Version: 0}} {
		if err := invalid.Validate(); !errors.Is(err, ErrMalformed) {
			t.Errorf("invalid profile ref %+v returned %v, want ErrMalformed", invalid, err)
		}
	}
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("load checked-in profiles: %v", err)
	}
	ref := ProfileRef{ID: "harborcare-promotion", Version: 1}
	p, err := registry.ResolveForTenant(ref, "harborcare-demo")
	if err != nil {
		t.Fatalf("resolve demo profile: %v", err)
	}
	if p.Name != "HarborCare promotion" || len(p.Datasets) < 2 {
		t.Fatalf("profile metadata = %q, %d datasets; want named demo seed datasets", p.Name, len(p.Datasets))
	}
	response, err := p.Response("snapshot_worker", workflow.OutcomeSucceeded)
	if err != nil {
		t.Fatalf("resolve seeded worker response: %v", err)
	}
	if response.CapabilityID != "hcmnext.people.explain_worker_state" || response.CapabilityVersion != 1 {
		t.Fatalf("response capability = %s@%d", response.CapabilityID, response.CapabilityVersion)
	}
	if _, err := p.ResponseFor("snapshot_worker", response.CapabilityID, 2, workflow.OutcomeSucceeded); !errors.Is(err, ErrCapabilityMismatch) {
		t.Fatalf("response with wrong bound version = %v, want ErrCapabilityMismatch", err)
	}
	workerID := ""
	for _, field := range response.Fields {
		if field.Path == "worker_id" {
			workerID = field.Value
		}
	}
	if len(response.Fields) != 5 || workerID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("worker sample fields = %+v", response.Fields)
	}
	if _, err := p.Response("snapshot_worker", workflow.OutcomeRejected); err != nil {
		t.Fatalf("rejected branch sample is absent: %v", err)
	}
	if _, err := p.Response("snapshot_worker", workflow.OutcomeUnknown); !errors.Is(err, ErrResponseAbsent) {
		t.Fatalf("undeclared outcome error = %v, want ErrResponseAbsent", err)
	}
	if _, err := registry.ResolveForTenant(ref, "other-tenant"); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("cross-tenant profile resolution = %v, want ErrTenantMismatch", err)
	}
	if _, err := registry.Resolve(ProfileRef{ID: ref.ID, Version: 2}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unpublished profile version = %v, want ErrNotFound", err)
	}
	conformance, err := registry.ResolveForTenant(ProfileRef{ID: "harborcare-conformance", Version: 1}, "acme")
	if err != nil {
		t.Fatalf("resolve conformance profile: %v", err)
	}
	if conformance.Tenant != "acme" || len(conformance.Datasets) != 3 || conformance.Datasets[0].Source == "" {
		t.Fatalf("conformance profile does not pin its source environments: %+v", conformance)
	}

	// Mutating a returned profile cannot alter the version stored in the registry.
	p.Responses[0].Fields[0].Value = "cross-tenant-forgery"
	p.Datasets[0].ID = "rewritten"
	again, err := registry.Resolve(ref)
	if err != nil {
		t.Fatal(err)
	}
	if again.Responses[0].Fields[0].Value == "cross-tenant-forgery" || again.Datasets[0].ID == "rewritten" {
		t.Fatal("resolved profile mutation changed registry data")
	}
}

func TestTodo_WF_TEST_003_Golden(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("load checked-in profiles: %v", err)
	}
	p, err := registry.Resolve(ProfileRef{ID: "harborcare-promotion", Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:e86ff549453d902714f65d8baec7fbbb711b7bed2bf36bc105a4ea7a8e9c274c"
	if got := p.Digest(); got != want {
		t.Fatalf("profile digest = %q, want %q", got, want)
	}
}
