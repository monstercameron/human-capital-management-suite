package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	customdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/custom"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func customServedDefinition(version uint64, withRegion bool) customdomain.CustomObjectDefinition {
	fields := map[string]customdomain.FieldDefinition{
		"registration": {Type: "string", Classification: customdomain.FieldClassification{AuthZDomain: "worker.core", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}},
	}
	if withRegion {
		fields["region"] = customdomain.FieldDefinition{Type: "string", Classification: customdomain.FieldClassification{AuthZDomain: "worker.core", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}}
	}
	return customdomain.CustomObjectDefinition{Kind: "Vehicle", Namespace: "tenant.fleet", Version: version, Fields: fields}
}

func customServedRecord(t *testing.T, version uint64, registration string) customdomain.CustomRecordRevision {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	interval, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatal(err)
	}
	return customdomain.CustomRecordRevision{
		ObjectID: "vehicle-1", ObjectKind: "Vehicle", Namespace: "tenant.fleet", DefinitionVersion: version,
		Effective: interval, FieldValues: map[string]customdomain.TypedValue{
			"registration": {FieldName: "registration", Type: "string", Value: registration},
		},
	}
}

// TestTodo_CUSTOM_005_Served proves the generated native and grpcbridge
// clients are reachable through the application dependency closure.
func TestTodo_CUSTOM_005_Served(t *testing.T) {
	surface := application.NewServedCustomSurface()
	manifest, err := surface.GenerateCapabilities(customServedDefinition(1, false), "fleet.operations")
	if err != nil {
		t.Fatal(err)
	}
	native, err := surface.NewCapabilityClient(manifest, func(context.Context, customdomain.Capability, customdomain.Invocation) (customdomain.CommitReceipt, error) {
		return customdomain.CommitReceipt{}, nil
	})
	if err != nil || native == nil {
		t.Fatalf("native client = %v, err=%v", native, err)
	}
	grpc, err := surface.NewGRPCCapabilityClient(manifest, func(context.Context, customdomain.Capability, customdomain.Invocation) (customdomain.CommitReceipt, error) {
		return customdomain.CommitReceipt{}, nil
	})
	if err != nil || grpc == nil {
		t.Fatalf("grpc client = %v, err=%v", grpc, err)
	}
	if err := surface.CheckClientParity(manifest, manifest); err != nil {
		t.Fatalf("client parity: %v", err)
	}
}

// TestTodo_CUSTOM_006_Served proves served search and rebuild preserve the
// domain's authorization and freshness-bearing result contract.
func TestTodo_CUSTOM_006_Served(t *testing.T) {
	surface := application.NewServedCustomSurface()
	store := customdomain.NewEventStore()
	definition := customServedDefinition(1, false)
	_, err := surface.Commit(context.Background(), store, customdomain.MutationRequest{
		Tenant: "tenant-a", ObjectID: "vehicle-1", Definition: definition, Record: customServedRecord(t, 1, "ABC-1"),
		Operation: customdomain.OperationCreate, Actor: "actor-1", Purpose: "fleet.operations", EvidenceDigest: "evidence-1",
		GrantedDomains: map[string]bool{"worker.core": true}, OccurredAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := surface.Search(context.Background(), store, customdomain.SearchRequest{
		Tenant: "tenant-a", Definition: definition, Query: map[string]string{"registration": "ABC-1"},
		RequestedFields: []string{"registration"}, GrantedDomains: map[string]bool{"worker.core": true}, Purpose: "fleet.operations", RequiredSchemaVersion: 1,
	})
	if err != nil || report.Count != 1 || report.SchemaVersion != 1 || report.ProjectionSequence != 1 {
		t.Fatalf("search report = %+v, err=%v", report, err)
	}
	rebuilt, err := surface.Rebuild(context.Background(), store, "tenant-a", "Vehicle", "vehicle-1")
	if err != nil || rebuilt.SourceHead != 1 || rebuilt.SchemaVersion != 1 {
		t.Fatalf("rebuild report = %+v, err=%v", rebuilt, err)
	}
}

// TestTodo_CUSTOM_007_Served proves version planning, record migration,
// rollback, and explicit retirement are all reachable from the served app.
func TestTodo_CUSTOM_007_Served(t *testing.T) {
	surface := application.NewServedCustomSurface()
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plan, err := surface.PlanMigration(customServedDefinition(1, false), customServedDefinition(2, true), customdomain.MigrationOptions{
		RollbackRef: "rollback:vehicle:1-2", EvidenceDigest: "evidence:migration", EffectiveAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := surface.MigrateRecord(plan, customServedRecord(t, 1, "ABC-1"))
	if err != nil || record.DefinitionVersion != 2 || record.FieldValues["region"].FieldName != "region" {
		t.Fatalf("migrated record = %+v, err=%v", record, err)
	}
	if _, err := surface.RollbackPlan(plan); err != nil {
		t.Fatalf("rollback plan: %v", err)
	}
	retirement, err := surface.MarkRetired(plan, at.Add(time.Hour), "evidence:retire")
	if err != nil || retirement.RetiredVersion != 1 || retirement.SuccessorVersion != 2 {
		t.Fatalf("retirement = %+v, err=%v", retirement, err)
	}
}

func TestTodo_CUSTOM_ServedApp(t *testing.T) {
	var app application.App
	surface := app.Custom()
	if surface.GenerateCapabilities == nil || surface.Search == nil || surface.Rebuild == nil || surface.PlanMigration == nil || surface.MarkRetired == nil {
		t.Fatal("composed application omitted custom-object capabilities")
	}
}
