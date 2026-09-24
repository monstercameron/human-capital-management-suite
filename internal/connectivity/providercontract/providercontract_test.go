package providercontract

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
)

func runProvider001(t *testing.T) {
	t.Helper()
	fixture, err := NewFixture(PlaceholderTopology())
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := CompileEvidence(context.Background(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ProbeHealth != "HEALTHY" || !evidence.IndependentObservation || !evidence.RecoveryVerified || !evidence.NoMutatingCalls || len(evidence.FaultClasses) != 2 {
		t.Fatalf("incomplete evidence: %+v", evidence)
	}
}

func TestSelectedProviderAdapterPassesPinnedSemanticAndFaultConformanceSuite(t *testing.T) {
	runProvider001(t)
}

func TestTodo_PROVIDER_001_Golden(t *testing.T) {
	topology := PlaceholderTopology()
	digest, err := topology.Digest()
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("topology digest = %q, err = %v", digest, err)
	}
	if !topology.Placeholder || !strings.Contains(topology.Explain(), "placeholder=true") {
		t.Fatalf("placeholder decision was not labelled: %s", topology.Explain())
	}
}

func FuzzTodo_PROVIDER_001(f *testing.F) {
	f.Add("safe")
	f.Add("provider-shaped-but-not-a-secret")
	f.Fuzz(func(t *testing.T, value string) {
		topology := PlaceholderTopology()
		topology.SelectionID = value
		if value == "" {
			if topology.Validate() != nil {
				return
			}
		}
		if err := topology.Validate(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTodo_PROVIDER_001_Race(t *testing.T) {
	fixture, err := NewFixture(PlaceholderTopology())
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	start := make(chan struct{})
	results := make(chan Evidence, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			evidence, err := CompileEvidence(context.Background(), fixture)
			if err != nil {
				errs <- err
				return
			}
			results <- evidence
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("concurrent evidence compilation: %v", err)
	}
	var digest string
	count := 0
	for evidence := range results {
		count++
		if !evidence.IndependentObservation || !evidence.RecoveryVerified || evidence.NoMutatingCalls != true {
			t.Errorf("incomplete concurrent evidence: %+v", evidence)
		}
		if digest == "" {
			digest = evidence.ManifestDigest
		} else if evidence.ManifestDigest != digest {
			t.Errorf("nondeterministic manifest digest: %q != %q", evidence.ManifestDigest, digest)
		}
	}
	if count != workers {
		t.Fatalf("compiled evidence %d times, want %d", count, workers)
	}
}
func TestTodo_PROVIDER_001_Integration(t *testing.T) {
	fixture, err := NewFixture(PlaceholderTopology())
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := CompileEvidence(context.Background(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Adapter == nil || fixture.Incumbent == nil || evidence.TopologyDigest == "" || evidence.ManifestDigest == "" {
		t.Fatalf("evidence does not bind fixture adapter and incumbent: evidence=%+v topology=%+v", evidence, fixture.Topology)
	}
}
func TestTodo_PROVIDER_001_Fault(t *testing.T) {
	topology := PlaceholderTopology()
	topology.Faults = nil
	if _, err := NewFixture(topology); err == nil {
		t.Fatal("topology without fault coverage accepted")
	}
}
func TestTodo_PROVIDER_001_Security(t *testing.T) {
	if strings.Contains(PlaceholderTopology().Explain(), "PLACEHOLDER_CREDENTIAL_REF") {
		t.Fatal("credential reference leaked from Explain")
	}
	runProvider001(t)
}
func TestTodo_PROVIDER_001_Conformance(t *testing.T) {
	topology := PlaceholderTopology()
	if err := topology.Validate(); err != nil {
		t.Fatalf("placeholder topology failed conformance: %v", err)
	}
	if len(topology.Capabilities) == 0 || len(topology.Faults) == 0 || len(topology.StopConditions) == 0 {
		t.Fatalf("topology lacks pinned conformance coverage: %+v", topology)
	}
}
func TestTodo_PROVIDER_001_Recovery(t *testing.T) {
	fixture, err := NewFixture(PlaceholderTopology())
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := CompileEvidence(context.Background(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.RecoveryVerified || len(evidence.FaultClasses) < 2 || evidence.ProbeHealth != "HEALTHY" {
		t.Fatalf("recovery evidence missing: %+v", evidence)
	}
}
func BenchmarkTodo_PROVIDER_001(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fixture, err := NewFixture(PlaceholderTopology())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := CompileEvidence(context.Background(), fixture); err != nil {
			b.Fatal(err)
		}
	}
}
func TestTodo_PROVIDER_001_Mutation(t *testing.T) {
	fixture, err := NewFixture(PlaceholderTopology())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Adapter.ReadSnapshot(context.Background(), connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if got := fixture.Incumbent.MutatingCalls(); got != 0 {
		t.Fatalf("mutating calls = %d", got)
	}
}
