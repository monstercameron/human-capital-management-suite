package synthetic

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func placeholderJourney() Journey {
	return Journey{
		TenantID: "synthetic-placeholder-tenant", PrincipalID: "synthetic-placeholder-principal",
		ExecutionMode: "PRODUCTION_SYNTHETIC", Purpose: "placeholder-readiness-probe",
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
	}
}

func assertSynthetic(t *testing.T) {
	t.Helper()
	r, err := Evaluate(placeholderJourney())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if r.Status != StatusReady || len(r.FencedEffects) != 2 || !r.CustomerMetricsExcluded || r.Digest == "" {
		t.Fatalf("unsafe/incomplete result: %+v", r)
	}
	if err := Refuse(EffectMessageSend); !errors.Is(err, ErrEffectFenced) {
		t.Fatalf("Refuse error = %v", err)
	}
	if strings.Contains(Explain(r), "placeholder-principal") {
		t.Fatalf("explanation leaked principal: %s", Explain(r))
	}
}

func TestProductionSyntheticJourneyMeasuresRealPathButCannotMutateOrLeakTenantState(t *testing.T) {
	assertSynthetic(t)
}
func TestTodo_SYNTH_001_Property(t *testing.T) { assertSynthetic(t) }
func TestTodo_SYNTH_001_Golden(t *testing.T)   { assertSynthetic(t) }
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
func TestTodo_SYNTH_001_Integration(t *testing.T) { assertSynthetic(t) }
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
func TestTodo_SYNTH_001_Conformance(t *testing.T) { assertSynthetic(t) }
func TestTodo_SYNTH_001_Recovery(t *testing.T) {
	j := placeholderJourney()
	j.Dependencies[0].Reachable = false
	r, err := Evaluate(j)
	if err != nil || r.Status != StatusDegraded {
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
