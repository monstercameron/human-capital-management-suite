package providerdrift

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providercontract"
)

func contracts(t *testing.T) (Contract, Contract) {
	t.Helper()
	signed, err := FromTopology(providercontract.PlaceholderTopology(), "sha256:placeholder-manifest")
	if err != nil {
		t.Fatal(err)
	}
	observed := cloneContract(signed)
	return signed, observed
}

func cloneContract(in Contract) Contract {
	out := in
	out.SchemaVersions = map[string]string{}
	for k, v := range in.SchemaVersions {
		out.SchemaVersions[k] = v
	}
	out.CapabilityVersions = map[string]string{}
	for k, v := range in.CapabilityVersions {
		out.CapabilityVersions[k] = v
	}
	out.Permissions = map[string]bool{}
	for k, v := range in.Permissions {
		out.Permissions[k] = v
	}
	out.ErrorClasses = append([]string(nil), in.ErrorClasses...)
	return out
}

func TestProviderDriftDetectionQuarantinesAffectedOperationsUntilReviewedCompatibility(t *testing.T) {
	signed, observed := contracts(t)
	observed.SchemaVersions["WORKER"] = "PLACEHOLDER_SCHEMA_V2"
	report, err := Compare(signed, observed)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != StateQuarantined || report.NewDispatchAllowed || report.InFlightDisposition != "PRESERVE_OPERATION_ID_AND_REQUIRE_OBSERVATION" {
		t.Fatalf("report = %+v", report)
	}
	reviewed := Reconcile(report, Review{ReviewID: "PLACEHOLDER_REVIEW", Compatible: true, ProviderID: observed.ProviderID, AdapterVersion: observed.AdapterVersion, ManifestDigest: observed.ManifestDigest, ConfigDigest: "sha256:placeholder-config", ApprovedAt: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)})
	if reviewed.State != StateResumed || !reviewed.NewDispatchAllowed {
		t.Fatalf("reviewed report = %+v", reviewed)
	}
}

func TestTodo_PROVIDER_002_Property(t *testing.T) {
	signed, observed := contracts(t)
	observed.QuotaPerMinute++
	report, err := Compare(signed, observed)
	if err != nil || report.State != StateQuarantined {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
func TestTodo_PROVIDER_002_Golden(t *testing.T) {
	signed, observed := contracts(t)
	report, err := Compare(signed, observed)
	if err != nil || report.State != StateHealthy || report.Digest == "" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
func TestTodo_PROVIDER_002_Integration(t *testing.T) {
	signed, observed := contracts(t)
	observed.SchemaVersions["WORKER"] = "PLACEHOLDER_SCHEMA_V3"
	report, err := Compare(signed, observed)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != StateQuarantined || report.NewDispatchAllowed || len(report.Changes) != 1 || report.Changes[0].Path != "schema.WORKER" {
		t.Fatalf("schema drift did not quarantine the affected operation: %+v", report)
	}
	review := Review{ReviewID: "PLACEHOLDER_REVIEW", Compatible: true, ProviderID: observed.ProviderID, AdapterVersion: observed.AdapterVersion, ManifestDigest: observed.ManifestDigest, ConfigDigest: "sha256:placeholder-config", ApprovedAt: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)}
	if resumed := Reconcile(report, review); resumed.State != StateResumed || !resumed.NewDispatchAllowed {
		t.Fatalf("review failed to resume dispatch: %+v", resumed)
	}
}
func TestTodo_PROVIDER_002_Fault(t *testing.T) {
	signed, observed := contracts(t)
	observed.CapabilityVersions = nil
	if _, err := Compare(signed, observed); err == nil {
		t.Fatal("incomplete observed contract accepted")
	}
}
func TestTodo_PROVIDER_002_Security(t *testing.T) {
	signed, observed := contracts(t)
	observed.Permissions["worker.read"] = false
	report, err := Compare(signed, observed)
	if err != nil || report.NewDispatchAllowed {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
func TestTodo_PROVIDER_002_Conformance(t *testing.T) {
	signed, observed := contracts(t)
	first, err := Compare(signed, observed)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compare(signed, cloneContract(observed))
	if err != nil {
		t.Fatal(err)
	}
	if first.State != StateHealthy || first.Digest == "" || first.Digest != second.Digest || len(first.Changes) != 0 {
		t.Fatalf("matching provider contract comparison is not deterministic: first=%+v second=%+v", first, second)
	}
}
func TestTodo_PROVIDER_002_Recovery(t *testing.T) {
	signed, observed := contracts(t)
	observed.ManifestDigest = "sha256:changed"
	report, _ := Compare(signed, observed)
	recovered := Reconcile(report, Review{ReviewID: "PLACEHOLDER_RESELECT", Reselect: true, ConfigDigest: "sha256:config", ApprovedAt: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)})
	if recovered.State != StateResumed || !recovered.NewDispatchAllowed {
		t.Fatalf("recovered=%+v", recovered)
	}
}
func TestTodo_PROVIDER_002_Mutation(t *testing.T) {
	signed, observed := contracts(t)
	before := signed
	observed.WebhookVersion = "PLACEHOLDER_WEBHOOK_V2"
	report, _ := Compare(signed, observed)
	if before.WebhookVersion != signed.WebhookVersion || report.NewDispatchAllowed {
		t.Fatal("comparison mutated signed contract or allowed drift")
	}
}
