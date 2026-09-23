package documenthubstore

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// assertGoldenJSON pins the canonical JSON encoding of v byte-for-byte
// against a checked-in oracle.
func assertGoldenJSON(t *testing.T, path string, v any) {
	t.Helper()
	canonical, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(append(canonical, '\n'), want) {
		t.Fatalf("golden %s changed:\n got %s\nwant %s", path, canonical, want)
	}
}

// TestTodo_HUB_019 is the PRIMARY test for HUB-019: extraction stores the
// stable target ID with optional pinned version and block, never a title
// or slug that breaks on rename.
func TestTodo_HUB_019(t *testing.T) {
	links := ExtractLinks("See [policy](doc:abc123) and [pinned](doc:abc123@docv-9#intro) plus [frag](doc:abc123#intro).\n")
	if len(links) != 3 {
		t.Fatalf("extracted %d links: %+v", len(links), links)
	}
	if links[0].TargetDocID != "abc123" || links[0].PinnedVersion != "" || links[0].Block != "" || links[0].State != LinkValid {
		t.Fatalf("plain link wrong: %+v", links[0])
	}
	if links[1].TargetDocID != "abc123" || links[1].PinnedVersion != "docv-9" || links[1].Block != "intro" {
		t.Fatalf("pinned link wrong: %+v", links[1])
	}
	if links[2].Block != "intro" || links[2].PinnedVersion != "" {
		t.Fatalf("fragment link wrong: %+v", links[2])
	}
	links = ExtractLinks("External [web](https://example.com/x) and [[Title Link]] stay out.\n")
	if len(links) != 0 {
		t.Fatalf("non-canonical links extracted: %+v", links)
	}
	links = ExtractLinks("Forged [evil](doc:abc/../../other) and [empty](doc:) drop out.\n")
	for _, l := range links {
		if l.State == LinkValid && (l.TargetDocID == "" || strings.ContainsAny(l.TargetDocID, "/. \t")) {
			t.Fatalf("forged target accepted as valid: %+v", l)
		}
	}
	for _, l := range links {
		if l.TargetDocID == "" && l.State == LinkValid {
			t.Fatalf("empty target valid: %+v", l)
		}
	}
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-1", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "See [policy](doc:abc123).\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	stored := ExtractLinks("See [policy](doc:abc123) and [pinned](doc:abc123@docv-9#intro).\n")
	if err := s.StoreLinks(ctx, "tenant-a", docID, v.ID, stored); err != nil {
		t.Fatalf("store links: %v", err)
	}
	var count int
	var state string
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_link WHERE tenant_id=$1 AND source_version_id=$2`, "tenant-a", v.ID).Scan(&count); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT state FROM document_link WHERE tenant_id=$1 AND source_version_id=$2 AND target_document_id=$3`, "tenant-a", v.ID, "abc123").Scan(&state)
	}); err != nil || count != 2 || state != LinkValid {
		t.Fatalf("stored links wrong: count=%d state=%q err=%v", count, state, err)
	}
	if err := s.StoreLinks(ctx, "tenant-a", docID, v.ID, stored[:1]); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_link WHERE tenant_id=$1 AND source_version_id=$2`, "tenant-a", v.ID).Scan(&count)
	}); err != nil || count != 1 {
		t.Fatalf("rebuild did not replace: count=%d err=%v", count, err)
	}
}

// TestTodo_HUB_019_Golden is the GOLDEN test for HUB-019: the canonical
// link manifest of a fixed document is pinned byte-for-byte.
func TestTodo_HUB_019_Golden(t *testing.T) {
	links := ExtractLinks("# Guide\n\nSee [policy](doc:abc123) and [pinned](doc:abc123@docv-9#intro).\n")
	assertGoldenJSON(t, "testdata/hub019_links.golden.json", linkManifest(links))
}

// FuzzTodo_HUB_019 is the FUZZ test for HUB-019: extraction never panics,
// and a link marked valid always carries a clean stable target.
func FuzzTodo_HUB_019(f *testing.F) {
	seeds := []string{
		"[a](doc:abc)",
		"[a](doc:abc@docv-1#frag)",
		"[a](doc:)",
		"[a](doc:../evil)",
		"[[title]]",
		"[a](https://example.com)",
		"[unclosed(doc:abc)",
		"![img](doc:abc#frag)",
		"[a](DOC:ABC)",
		"[a](doc:abc)(doc:def)",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		for _, l := range ExtractLinks(src) {
			if l.State == LinkValid {
				if l.TargetDocID == "" || strings.ContainsAny(l.TargetDocID, " \t\n\r\"'()<>") {
					t.Fatalf("invalid valid target: %+v from %q", l, src)
				}
				if strings.ContainsAny(l.PinnedVersion+l.Block, " \t\n\r\"'()<>") {
					t.Fatalf("invalid pin/block: %+v from %q", l, src)
				}
			}
		}
	})
}
