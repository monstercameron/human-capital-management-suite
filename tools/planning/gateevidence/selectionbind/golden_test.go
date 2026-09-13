package selectionbind

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
)

// Golden identities of the signed NEXT-002 documents. Re-signing either
// document is a deliberate act that must update these, the evidence report
// and internal/commercial.P1AManifestDigest together.
const (
	goldenP1ADigest = "e9d7ffe66f019dc8ea8b4252303cde9dfc6b22cf705a8de5c6a024d1fbcd34dc"
	goldenP1BDigest = "fdcd55a46a26dad80c1e9a980a5b1f6acfcedee64a818f29a18cd13cc8f602d2"
)

const goldenReportPath = "testdata/live-completeness-report.golden.json"

// TestTodo_NEXT_002_Golden pins the exact bytes NEXT-002 produces: both
// documents' canonical digests, the commercial registry's copy of the P1A
// identity, and the full rendered completeness report for the live
// repository as of the pinned evaluation date. Set NEXT002_UPDATE_GOLDEN=1
// to rewrite the report fixture after a deliberate change.
func TestTodo_NEXT_002_Golden(t *testing.T) {
	m := mustLoadLiveManifest(t)
	tpl := mustLoadLiveTemplate(t)
	if d, _ := m.CanonicalDigest(); d != goldenP1ADigest {
		t.Errorf("P1A canonical digest = %s, golden %s", d, goldenP1ADigest)
	}
	if d, _ := tpl.CanonicalDigest(); d != goldenP1BDigest {
		t.Errorf("P1B canonical digest = %s, golden %s", d, goldenP1BDigest)
	}
	if commercial.P1AManifestDigest != goldenP1ADigest {
		t.Errorf("internal/commercial.P1AManifestDigest = %s, golden %s", commercial.P1AManifestDigest, goldenP1ADigest)
	}

	r, err := Evaluate(m, liveOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := RenderJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := RenderJSON(r)
	if !bytes.Equal(got, again) {
		t.Fatal("RenderJSON is not deterministic")
	}
	if os.Getenv("NEXT002_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenReportPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenReportPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenReportPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("live completeness report differs from %s:\n%s", goldenReportPath, got)
	}
}
