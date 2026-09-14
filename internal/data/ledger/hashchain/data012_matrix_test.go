package hashchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func testDigester(t *testing.T) *Digester {
	t.Helper()
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return NewDigester(registry)
}

func scanFixture(t *testing.T, digester *Digester, stream string, sequences int64) StreamView {
	t.Helper()
	view := StreamView{StreamKey: stream, ProjectedThrough: sequences}
	prevHash := GenesisHash
	for sequence := int64(1); sequence <= sequences; sequence++ {
		id := uuid.New()
		eventDigest := "sha256:event-" + stream + "-" + string(rune('0'+sequence))
		_, chainHash, err := digester.Link(prevHash, eventDigest)
		if err != nil {
			t.Fatalf("Link: %v", err)
		}
		view.Events = append(view.Events, EventDigest{Sequence: sequence, EventID: id, Digest: eventDigest})
		view.Links = append(view.Links, ChainedLink{
			StreamKey: stream, Sequence: sequence, EventID: id,
			PrevHash: prevHash, ChainHash: chainHash, Algorithm: "sha256",
		})
		view.Outbox = append(view.Outbox, id)
		view.Provenance = append(view.Provenance, id)
		prevHash = chainHash
	}
	return view
}

func mustScan(t *testing.T, in ScanInput) ScanReport {
	t.Helper()
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	report, err := Scan(NewDigester(registry), in)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return report
}

// TestTodo_DATA_012_Property: each defect class emits its exact code
// with severity and watermark; compound defects accumulate.
func TestTodo_DATA_012_Property(t *testing.T) {
	digester := testDigester(t)
	codes := func(view StreamView) map[string]ScanFinding {
		report := mustScan(t, ScanInput{Mode: ScanFull, Streams: []StreamView{view}})
		out := map[string]ScanFinding{}
		for _, finding := range report.Findings {
			out[finding.Code] = finding
		}
		return out
	}
	mutated := scanFixture(t, digester, "s", 4)
	mutated.Links[2].ChainHash = "sha256:forged"
	if found := codes(mutated); found[FindingDigestMismatch].Severity != "high" {
		t.Fatalf("digest mismatch: %+v", found)
	}
	orphanOutbox := scanFixture(t, digester, "s", 4)
	orphanOutbox.Outbox = orphanOutbox.Outbox[:3]
	if found := codes(orphanOutbox); found[FindingOrphanOutbox].Severity != "medium" {
		t.Fatalf("orphan outbox: %+v", found)
	}
	behind := scanFixture(t, digester, "s", 4)
	behind.ProjectedThrough = 2
	if found := codes(behind); found[FindingOrphanProjection].Severity != "medium" {
		t.Fatalf("orphan projection: %+v", found)
	}
	unproven := scanFixture(t, digester, "s", 4)
	unproven.Provenance = unproven.Provenance[:2]
	if found := codes(unproven); found[FindingProvenanceMissing].Severity != "low" {
		t.Fatalf("provenance: %+v", found)
	}
	duped := scanFixture(t, digester, "s", 4)
	duped.Settlements = []string{"settle-1", "settle-1"}
	if found := codes(duped); found[FindingDuplicateSettle].Severity != "high" {
		t.Fatalf("duplicate settlement: %+v", found)
	}
}

// TestTodo_DATA_012_Golden pins the scan digest oracle.
func TestTodo_DATA_012_Golden(t *testing.T) {
	digester := testDigester(t)
	report := mustScan(t, ScanInput{Mode: ScanFull, Streams: []StreamView{scanFixture(t, digester, "ledger-stream-1", 8)}})
	raw, err := os.ReadFile(filepath.Join("testdata", "data012.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.TrimSpace(string(raw)); report.Digest != want {
		t.Fatalf("scan digest mismatch:\n got=%q\nwant=%q", report.Digest, want)
	}
}

// TestTodo_DATA_012_Mutation: mode and readability edges resolve on
// the documented side.
func TestTodo_DATA_012_Mutation(t *testing.T) {
	digester := testDigester(t)
	// TARGETED scans only the named stream.
	first := scanFixture(t, digester, "a", 4)
	second := scanFixture(t, digester, "b", 4)
	second.Links = append(second.Links[:2], second.Links[3:]...)
	targeted := mustScan(t, ScanInput{Mode: ScanTargeted, Target: "a", Streams: []StreamView{first, second}})
	if targeted.Quality != QualityHealthy || targeted.Watermark != 4 {
		t.Fatalf("targeted: %+v", targeted)
	}
	if _, err := Scan(digester, ScanInput{Mode: ScanTargeted, Target: "missing", Streams: []StreamView{first}}); err == nil {
		t.Fatal("targeted scan of an absent stream admitted")
	}
	// SAMPLE classifies the sampled links; PARTITION bounds the window.
	sampled := scanFixture(t, digester, "s", 8)
	sampled.SampleEvery = 2
	if report := mustScan(t, ScanInput{Mode: ScanSample, Streams: []StreamView{sampled}}); report.Quality != QualityHealthy {
		t.Fatalf("sampled: %+v", report)
	}
	part := scanFixture(t, digester, "s", 8)
	part.PartitionLo, part.PartitionHi = 3, 6
	// An intact window verifies healthy without a genesis anchor;
	// forging one link inside the window degrades exactly.
	if report := mustScan(t, ScanInput{Mode: ScanPartition, Streams: []StreamView{part}}); report.Quality != QualityHealthy || report.Watermark != 6 {
		t.Fatalf("partition: %+v", report)
	}
	part.Links[4].ChainHash = "sha256:forged"
	if report := mustScan(t, ScanInput{Mode: ScanPartition, Streams: []StreamView{part}}); report.Quality != QualityDegraded {
		t.Fatalf("forged partition: %+v", report)
	}
	// An unscannable stream reports unknown, never healthy.
	dark := scanFixture(t, digester, "s", 4)
	dark.Unscannable = true
	if report := mustScan(t, ScanInput{Mode: ScanFull, Streams: []StreamView{dark}}); report.Quality != QualityUnknown {
		t.Fatalf("unscannable: %+v", report)
	}
	// An empty stream verifies vacuously with a zero watermark.
	if report := mustScan(t, ScanInput{Mode: ScanFull, Streams: []StreamView{{StreamKey: "empty"}}}); report.Quality != QualityHealthy || report.Watermark != 0 {
		t.Fatalf("empty: %+v", report)
	}
	if _, err := Scan(nil, ScanInput{}); err == nil {
		t.Fatal("nil digester admitted")
	}
}
