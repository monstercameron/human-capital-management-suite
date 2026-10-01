package synthetic

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/slo"
)

func placeholderJourney() Journey {
	return Journey{
		TenantID: "synthetic-placeholder-tenant", PrincipalID: "synthetic-placeholder-principal",
		ExecutionMode: ExecutionModeProductionSynthetic, Purpose: "production-readiness-probe", CapabilityOwner: OwnerConformancePlatform,
		CredentialReadOnly: true, CustomerMetricsExcluded: true,
		Steps: []Step{
			{Name: "edge", Layer: LayerEdge, TenantID: "synthetic-placeholder-tenant", ReadOnly: true},
			{Name: "auth", Layer: LayerAuth, TenantID: "synthetic-placeholder-tenant", ReadOnly: true},
			{Name: "config", Layer: LayerConfig, TenantID: "synthetic-placeholder-tenant", ReadOnly: true},
			{Name: "read", Layer: LayerRead, TenantID: "synthetic-placeholder-tenant", ReadOnly: true},
			{Name: "simulation", Layer: LayerSimulation, TenantID: "synthetic-placeholder-tenant", ReadOnly: true},
			{Name: "telemetry", Layer: LayerTelemetry, TenantID: "synthetic-placeholder-tenant", ReadOnly: true},
		},
		Dependencies:     []Dependency{{Name: "edge-placeholder", Reachable: true}, {Name: "config-placeholder", Reachable: true}},
		AttemptedEffects: []Effect{EffectDomainWrite, EffectProviderCall},
		SLOTarget: slo.Target{
			ID: "synthetic.read.slo", Capability: "synthetic.read", Indicator: "synthetic.read.availability",
			Query: "good/valid", Window: time.Hour, StalenessBound: 5 * time.Minute, Threshold: .995,
			Owner: "reliability-management", Version: "v1",
		},
		SLOObservation: slo.Observation{
			ObservedAt: time.Date(2026, 9, 29, 12, 4, 0, 0, time.UTC),
			WindowFrom: time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC),
			WindowTo:   time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
			Total:      1000, Good: 1000, Complete: true,
		},
		Now: time.Date(2026, 9, 29, 12, 5, 0, 0, time.UTC),
	}
}

func assertSynthetic(t *testing.T) {
	t.Helper()
	r, err := Evaluate(placeholderJourney())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if r.Status != StatusReady || len(r.FencedEffects) != 2 || !r.CustomerMetricsExcluded || r.Digest == "" || r.SLOTargetID != "synthetic.read.slo" || r.SLOCapability != "synthetic.read" || r.SLOVersion != "v1" || r.SLOStatus != slo.Healthy || r.SLOReason != "TARGET_MET" || r.SLOValue != 1 {
		t.Fatalf("unsafe/incomplete result: %+v", r)
	}
	if err := Refuse(EffectMessageSend); !errors.Is(err, ErrEffectFenced) {
		t.Fatalf("Refuse error = %v", err)
	}
	if strings.Contains(Explain(r), "placeholder-principal") {
		t.Fatalf("explanation leaked principal: %s", Explain(r))
	}
	if !strings.Contains(Explain(r), "owner="+OwnerConformancePlatform) || !strings.Contains(Explain(r), "slo_status=HEALTHY") {
		t.Fatalf("explanation omitted accountable SLO evidence: %s", Explain(r))
	}
}

func TestProductionSyntheticJourneyMeasuresRealPathButCannotMutateOrLeakTenantState(t *testing.T) {
	assertSynthetic(t)
}
func TestTodo_SYNTH_001_Property(t *testing.T) {
	mutations := map[string]func(*Journey){
		"foreign_tenant":  func(j *Journey) { j.Steps[0].TenantID = "customer-tenant" },
		"duplicate_layer": func(j *Journey) { j.Steps[1].Layer = j.Steps[0].Layer },
		"unknown_effect":  func(j *Journey) { j.AttemptedEffects[0] = Effect("UNDECLARED_EFFECT") },
		"missing_owner":   func(j *Journey) { j.CapabilityOwner = "" },
		"wrong_owner":     func(j *Journey) { j.CapabilityOwner = "reliability-management" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			j := placeholderJourney()
			mutate(&j)
			if _, err := Evaluate(j); !errors.Is(err, ErrInvalidJourney) {
				t.Fatalf("Evaluate error = %v, want ErrInvalidJourney", err)
			}
		})
	}
}
func TestTodo_SYNTH_001_Golden(t *testing.T) {
	r, err := Evaluate(placeholderJourney())
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:1726e131a8549f2c40497c54970828b9d4db3613190c4e1dc89077a97e8b4e45"
	if r.Digest != wantDigest {
		t.Fatalf("digest = %q, want %q", r.Digest, wantDigest)
	}
}
func TestTodo_SYNTH_001_Race(t *testing.T) {
	journey := placeholderJourney()
	before := placeholderJourney()
	want, err := Evaluate(journey)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 24
	var wg sync.WaitGroup
	results := make(chan Result, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := Evaluate(journey)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for result := range results {
		if !reflect.DeepEqual(result, want) {
			t.Fatalf("concurrent evaluation diverged: got=%+v want=%+v", result, want)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent journey evaluation: %v", err)
		}
	}
	if !reflect.DeepEqual(journey, before) {
		t.Fatalf("concurrent evaluation mutated the shared input: got=%+v want=%+v", journey, before)
	}
}
func TestTodo_SYNTH_001_Integration(t *testing.T) {
	j := placeholderJourney()
	j.Dependencies[1] = Dependency{Name: "config-placeholder", Reachable: false, Detail: "configuration projection unavailable"}
	r, err := Evaluate(j)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusDegraded || !reflect.DeepEqual(r.DependencyFailures, []string{"config-placeholder"}) || r.SLOStatus != slo.Healthy {
		t.Fatalf("dependency/SLO projection = %+v", r)
	}
}
func TestTodo_SYNTH_001_Fault(t *testing.T) {
	j := placeholderJourney()
	j.Steps[3].TenantID = "customer-tenant"
	_, err := Evaluate(j)
	if !errors.Is(err, ErrInvalidJourney) {
		t.Fatal("cross-tenant read was accepted")
	}
}
func TestTodo_SYNTH_001_Security(t *testing.T) {
	j := placeholderJourney()
	j.CredentialReadOnly = false
	_, err := Evaluate(j)
	if !errors.Is(err, ErrInvalidJourney) {
		t.Fatal("write-capable credential was accepted")
	}
}
func TestTodo_SYNTH_001_Conformance(t *testing.T) {
	r, err := Evaluate(placeholderJourney())
	if err != nil {
		t.Fatal(err)
	}
	wantLayers := []Layer{LayerEdge, LayerAuth, LayerConfig, LayerRead, LayerSimulation, LayerTelemetry}
	if !reflect.DeepEqual(r.ObservedLayers, wantLayers) {
		t.Fatalf("observed layers = %v, want %v", r.ObservedLayers, wantLayers)
	}
	for _, effect := range []Effect{EffectDomainWrite, EffectIntentCreate, EffectWorkItem, EffectMessageSend, EffectOutboxPublish, EffectProviderCall, EffectTelemetryWrite} {
		if err := Refuse(effect); !errors.Is(err, ErrEffectFenced) {
			t.Errorf("Refuse(%s) = %v, want ErrEffectFenced", effect, err)
		}
	}
}
func TestTodo_SYNTH_001_Recovery(t *testing.T) {
	j := placeholderJourney()
	j.Dependencies[0].Reachable = false
	j.SLOObservation.Complete = false
	r, err := Evaluate(j)
	if err != nil || r.Status != StatusDegraded || r.SLOStatus != slo.Unknown || r.SLOReason != "MEASUREMENT_UNKNOWN" {
		t.Fatalf("degraded dependency evidence = %+v, %v", r, err)
	}
}
func BenchmarkTodo_SYNTH_001(b *testing.B) {
	j := placeholderJourney()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = Evaluate(j)
	}
}
func TestTodo_SYNTH_001_Mutation(t *testing.T) {
	j := placeholderJourney()
	j.Steps[0].ReadOnly = false
	_, err := Evaluate(j)
	if !errors.Is(err, ErrInvalidJourney) {
		t.Fatal("mutated step was accepted")
	}
}
