package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedDeliveryManifest() DeliveryManifest {
	return DeliveryManifest{
		Version:         1,
		ConfigDigest:    "sha256:otel-config",
		RuleDigest:      "sha256:alert-rules",
		DashboardDigest: "sha256:dashboards",
		BackendDigest:   "sha256:backend",
		Cardinality:     CardinalityReport{Series: 1200, Limit: 5000, Cost: "$41/mo"},
		RestoreTest:     "restore-drill-8",
		Runbooks: []Runbook{
			{Name: "collector", Version: 2, RehearsedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
			{Name: "backend", Version: 2, RehearsedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
			{Name: "privacy", Version: 1, RehearsedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
			{Name: "cardinality", Version: 1, RehearsedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
			{Name: "noisy-neighbor", Version: 1, RehearsedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
		},
	}
}

func seedHealthySink() FakeSink {
	required := DefaultRequiredSignals()
	return FakeSink{
		Metrics:   append([]string(nil), required.RequiredMetrics...),
		LogEvents: append([]string(nil), required.RequiredLogEvents...),
	}
}

func seedGappedSink() FakeSink {
	healthy := seedHealthySink()
	return FakeSink{
		Metrics:   healthy.Metrics[1:],
		LogEvents: healthy.LogEvents,
	}
}

func mustPublishEvidence(t *testing.T, manifest DeliveryManifest, signals Report, at time.Time) DeliveryEvidence {
	t.Helper()
	evidence, err := publishEvidence(manifest, signals, at)
	if err != nil {
		t.Fatalf("publishEvidence: %v", err)
	}
	return evidence
}

func mustCheck(t *testing.T, sink Sink, at time.Time) Report {
	t.Helper()
	report, err := Check(DefaultRequiredSignals(), sink, PipelineFaults{}, at)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return report
}

// TestTodo_OBS_008_Golden pins the delivery evidence digest oracle.
func TestTodo_OBS_008_Golden(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	evidence := mustPublishEvidence(t, seedDeliveryManifest(), mustCheck(t, seedHealthySink(), at), at)
	raw, err := os.ReadFile(filepath.Join("testdata", "obs008.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.TrimSpace(string(raw)); evidence.Digest != want {
		t.Fatalf("evidence digest mismatch:\n got=%q\nwant=%q", evidence.Digest, want)
	}
}

// TestTodo_OBS_008_Integration: the completeness gate and the delivery
// gate compose — healthy signals publish, faulted pipelines reject.
func TestTodo_OBS_008_Integration(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	manifest := seedDeliveryManifest()
	healthy := mustCheck(t, seedHealthySink(), at)
	if healthy.Health != HealthHealthy {
		t.Fatalf("healthy signals: %+v", healthy)
	}
	evidence := mustPublishEvidence(t, manifest, healthy, at)
	if evidence.Runbooks != len(RequiredRunbooks) || evidence.Health != HealthHealthy {
		t.Fatalf("evidence: %+v", evidence)
	}
	lagged, err := Check(DefaultRequiredSignals(), seedHealthySink(), PipelineFaults{BackendLag: time.Hour, BackendLagBound: time.Minute}, at)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if isRejected(manifest, lagged, at) != true {
		t.Fatalf("faulted pipeline published: %+v", lagged)
	}
}

// TestTodo_OBS_008_Fault: every incomplete manifest refuses with the
// exact offending field.
func TestTodo_OBS_008_Fault(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	healthy := mustCheck(t, seedHealthySink(), at)
	cases := []struct {
		name   string
		mutate func(DeliveryManifest) DeliveryManifest
		field  string
	}{
		{"no config", func(m DeliveryManifest) DeliveryManifest { m.ConfigDigest = ""; return m }, "manifest.config_digest"},
		{"no rules", func(m DeliveryManifest) DeliveryManifest { m.RuleDigest = ""; return m }, "manifest.rule_digest"},
		{"no dashboards", func(m DeliveryManifest) DeliveryManifest { m.DashboardDigest = ""; return m }, "manifest.dashboard_digest"},
		{"no backend", func(m DeliveryManifest) DeliveryManifest { m.BackendDigest = ""; return m }, "manifest.backend_digest"},
		{"no cardinality", func(m DeliveryManifest) DeliveryManifest { m.Cardinality.Limit = 0; return m }, "manifest.cardinality"},
		{"over limit", func(m DeliveryManifest) DeliveryManifest { m.Cardinality.Series = 9000; return m }, "manifest.cardinality"},
		{"no cost", func(m DeliveryManifest) DeliveryManifest { m.Cardinality.Cost = ""; return m }, "manifest.cost"},
		{"no restore test", func(m DeliveryManifest) DeliveryManifest { m.RestoreTest = ""; return m }, "manifest.restore_test"},
		{"missing runbook", func(m DeliveryManifest) DeliveryManifest { m.Runbooks = m.Runbooks[:4]; return m }, "manifest.runbooks.noisy-neighbor"},
		{"unrehearsed runbook", func(m DeliveryManifest) DeliveryManifest { m.Runbooks[0].RehearsedAt = time.Time{}; return m }, "manifest.runbooks.collector"},
	}
	for _, tc := range cases {
		if rejected := isRejected(tc.mutate(seedDeliveryManifest()), healthy, at); !rejected {
			t.Fatalf("%s published", tc.name)
			continue
		}
		err := mustRejectErr(t, tc.mutate(seedDeliveryManifest()), healthy, at)
		if err.Field != tc.field {
			t.Fatalf("%s field=%q want=%q", tc.name, err.Field, tc.field)
		}
	}
}

func isRejected(manifest DeliveryManifest, signals Report, at time.Time) bool {
	_, err := publishEvidence(manifest, signals, at)
	_, rejected := AsDeliveryRejected(err)
	return rejected
}

func mustRejectErr(t *testing.T, manifest DeliveryManifest, signals Report, at time.Time) *DeliveryError {
	t.Helper()
	_, err := publishEvidence(manifest, signals, at)
	rejected, ok := AsDeliveryRejected(err)
	if !ok {
		t.Fatalf("expected OBS_008_REJECTED, got %v", err)
	}
	return rejected
}

// TestTodo_OBS_008_Security: evidence digests bind the manifest and
// the signal health; tampering changes the receipt.
func TestTodo_OBS_008_Security(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	healthy := mustCheck(t, seedHealthySink(), at)
	honest := mustPublishEvidence(t, seedDeliveryManifest(), healthy, at)
	retargeted := seedDeliveryManifest()
	retargeted.BackendDigest = "sha256:rogue-backend"
	other := mustPublishEvidence(t, retargeted, healthy, at)
	if honest.Digest == other.Digest {
		t.Fatal("retargeted backend verifies against the honest digest")
	}
	// A degraded signal set never publishes, even with a full manifest.
	degraded := healthy
	degraded.Health = HealthDegraded
	if rejected := isRejected(seedDeliveryManifest(), degraded, at); !rejected {
		t.Fatal("degraded signals published")
	}
}

// TestTodo_OBS_008_Recovery: a rejected delivery heals by restoring
// the missing signal and republishes receipt-stable.
func TestTodo_OBS_008_Recovery(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	manifest := seedDeliveryManifest()
	gapped := mustCheck(t, seedGappedSink(), at)
	if rejected := isRejected(manifest, gapped, at); !rejected {
		t.Fatalf("gapped signals published: %+v", gapped)
	}
	healed := mustPublishEvidence(t, manifest, mustCheck(t, seedHealthySink(), at), at)
	again := mustPublishEvidence(t, manifest, mustCheck(t, seedHealthySink(), at), at)
	if healed.Digest != again.Digest {
		t.Fatalf("republish drift:\n got=%q\nwant=%q", again.Digest, healed.Digest)
	}
}

// TestTodo_OBS_008_Mutation: manifest edges resolve on the documented
// side.
func TestTodo_OBS_008_Mutation(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	healthy := mustCheck(t, seedHealthySink(), at)
	// Cardinality exactly at the limit publishes.
	edge := seedDeliveryManifest()
	edge.Cardinality.Series = edge.Cardinality.Limit
	mustPublishEvidence(t, edge, healthy, at)
	// One series over refuses.
	edge.Cardinality.Series++
	if rejected := isRejected(edge, healthy, at); !rejected {
		t.Fatal("over-limit cardinality published")
	}
	// A runbook rehearsed exactly at publish time counts.
	edge = seedDeliveryManifest()
	edge.Runbooks[0].RehearsedAt = at
	mustPublishEvidence(t, edge, healthy, at)
	// A future-dated rehearsal refuses.
	edge.Runbooks[0].RehearsedAt = at.Add(time.Hour)
	if rejected := isRejected(edge, healthy, at); !rejected {
		t.Fatal("future rehearsal published")
	}
	// A zero manifest version refuses.
	edge = seedDeliveryManifest()
	edge.Version = 0
	if rejected := isRejected(edge, healthy, at); !rejected {
		t.Fatal("unversioned manifest published")
	}
}
