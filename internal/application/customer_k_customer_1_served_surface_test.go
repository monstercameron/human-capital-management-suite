package application

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding/readiness"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/adoption"
)

func customerReadinessRehearsal() readiness.Rehearsal {
	field := readiness.SourceField{
		Ref: "field:worker.id", CanonicalPath: "person.id", Present: true,
		AuthorityRef: "authority:hris", Classification: "identity",
		Purpose: "identity", EffectiveAt: "2026-01-01", Hint: readiness.Accept,
	}
	return readiness.Rehearsal{
		SchemaVersion: 1, RehearsalID: "customer-002-served", TenantRef: "tenant:demo", SourceSystem: "hris",
		Fields:         []readiness.SourceField{field},
		Records:        []readiness.SourceRecord{{Ref: "record:worker-1", Fields: []readiness.SourceField{field}}},
		Configurations: []readiness.Configuration{{Ref: "config:hris", Version: "v1", Digest: "sha256:config", Compatible: true, AuthorityRef: "authority:config"}},
		Identities:     []readiness.IdentityCrosswalk{{SourceRef: "worker:1", CanonicalRef: "person:1", AuthorityRef: "authority:identity", MatchMethod: "reviewed-crosswalk", Confidence: 1}},
	}
}

func servedCustomerDrill() readiness.CutoverDrill {
	return readiness.CutoverDrill{
		DrillID: "customer-003-served", TenantRef: "tenant:demo", FreezeWatermark: "ledger:head-1",
		Deltas:           []readiness.DeltaItem{{Ref: "worker:1", Digest: "sha256:delta"}},
		Thresholds:       readiness.StopGoThresholds{MaxRPO: time.Minute, MaxRTO: 5 * time.Minute},
		RollbackBoundary: "ledger:head-1",
		Transitions:      []readiness.Transition{{Name: "credential", Owner: "security"}},
		HypercareOwner:   "pilot-commander", CustomerContact: "customer-ops@example",
		ManualContinuity: "manual payroll inbox",
	}
}

func servedCustomerAdoptionMatrix() adoption.Matrix {
	matrix := adoption.Matrix{ID: "customer-004-served", Version: "v1"}
	for _, spec := range adoption.RegistryMatrix() {
		for _, dimension := range spec.Required {
			matrix.Evidence = append(matrix.Evidence, adoption.Evidence{
				Journey: spec.ID, Dimension: dimension, TaskDigest: "build:v1", ResultDigest: "build:v1",
				RecordedAt: time.Unix(1800000000, 0).UTC(), Accessible: true, Trained: true,
				RollbackPath: true, HelpPath: true,
			})
		}
	}
	return matrix
}

// TestTodo_CUSTOMER_002_Served proves the onboarding readiness package is in
// the application boundary used by the shipped command, not only in an
// isolated package test.
func TestTodo_CUSTOMER_002_Served(t *testing.T) {
	surface := NewServedCustomerReadinessSurface()
	if surface.ReadinessVersion == nil || surface.EvaluateReadiness == nil || surface.SignReadiness == nil || surface.VerifyReadiness == nil {
		t.Fatal("served customer surface omitted onboarding readiness operations")
	}
	result, err := surface.EvaluateReadiness(customerReadinessRehearsal())
	if err != nil || !result.Ready || !result.ZeroMutation || result.Digest == "" {
		t.Fatalf("served onboarding readiness = %+v, err=%v", result, err)
	}
	if (&App{}).CustomerReadiness().EvaluateReadiness == nil {
		t.Fatal("composed application omitted onboarding readiness")
	}
}

// TestTodo_CUSTOMER_003_Served proves the cutover drill is reachable through
// the same application boundary as the shipped command.
func TestTodo_CUSTOMER_003_Served(t *testing.T) {
	surface := NewServedCustomerReadinessSurface()
	if surface.ExecuteCutover == nil || surface.NewCutoverCell == nil {
		t.Fatal("served customer surface omitted cutover operations")
	}
	report, err := surface.ExecuteCutover(readiness.CutoverInput{
		Drill: servedCustomerDrill(), At: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		ObservedRPO: 10 * time.Second, ObservedRTO: time.Minute,
	})
	if err != nil || report.Decision != readiness.CutoverGoLive || report.Digest == "" {
		t.Fatalf("served cutover = %+v, err=%v", report, err)
	}
}

// TestTodo_CUSTOMER_004_Served proves adoption evaluation is reachable from
// the serving application and still enforces all role/dimension evidence.
func TestTodo_CUSTOMER_004_Served(t *testing.T) {
	surface := NewServedCustomerReadinessSurface()
	if surface.CheckAdoption == nil || surface.AdoptionRegistry == nil || len(surface.AdoptionRegistry()) != 4 {
		t.Fatal("served customer surface omitted adoption operations")
	}
	report, err := surface.CheckAdoption(servedCustomerAdoptionMatrix(), time.Unix(1800000060, 0).UTC())
	if err != nil || !report.Passed || report.MatrixDigest == "" {
		t.Fatalf("served adoption = %+v, err=%v", report, err)
	}
	if (&App{}).CustomerReadiness().CheckAdoption == nil {
		t.Fatal("composed application omitted adoption readiness")
	}
}
