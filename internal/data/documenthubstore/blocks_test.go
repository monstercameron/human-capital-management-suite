package documenthubstore

import (
	"context"
	"testing"
)

// TestTodo_HUB_023 is the PRIMARY test for HUB-023: heading-derived block
// IDs survive redeploys unchanged, and only an explicit heading edit moves
// an anchor.
func TestTodo_HUB_023(t *testing.T) {
	blocks := DeriveBlocks("# Guide\n\n## Intro\n\nText.\n\n## Intro\n\n### Deep Dive!\n")
	ids := map[string]string{}
	for _, b := range blocks {
		ids[b.Heading] = b.ID
	}
	if ids["Guide"] != "guide" || ids["Deep Dive!"] != "deep-dive" {
		t.Fatalf("slugs wrong: %+v", blocks)
	}
	seen := map[string]int{}
	for _, b := range blocks {
		seen[b.ID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("duplicate anchor %q", id)
		}
	}
	if len(blocks) != 4 {
		t.Fatalf("blocks wrong: %+v", blocks)
	}
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "# Guide\n\n## Intro\n\nOne.\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "# Guide\n\n## Intro\n\nTwo.\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []Version{v1, v2} {
		if err := s.StoreBlocks(ctx, "tenant-a", docID, v.ID, DeriveBlocks(v.Markdown)); err != nil {
			t.Fatal(err)
		}
	}
	anchors, err := s.ResolveBlock(ctx, "tenant-a", docID, "intro")
	if err != nil || len(anchors) != 2 {
		t.Fatalf("anchor did not survive redeploy: %+v err=%v", anchors, err)
	}
	for _, a := range anchors {
		if a.Heading != "Intro" {
			t.Fatalf("anchor heading drifted: %+v", a)
		}
	}
	v3, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "# Guide\n\n## Opening\n\nThree.\n"}, v2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StoreBlocks(ctx, "tenant-a", docID, v3.ID, DeriveBlocks(v3.Markdown)); err != nil {
		t.Fatal(err)
	}
	moved, err := s.ResolveBlock(ctx, "tenant-a", docID, "opening")
	if err != nil || len(moved) != 1 || moved[0].VersionID != v3.ID {
		t.Fatalf("renamed anchor wrong: %+v err=%v", moved, err)
	}
	anchors, err = s.ResolveBlock(ctx, "tenant-a", docID, "intro")
	if err != nil || len(anchors) != 2 {
		t.Fatalf("old anchor versions lost: %+v err=%v", anchors, err)
	}
}

// TestTodo_HUB_023_Property is the PROPERTY test for HUB-023: derivation is
// deterministic, unique within a version, and immune to body edits.
func TestTodo_HUB_023_Property(t *testing.T) {
	bodies := []string{"# A\n\n## B\n", "# A\n\nBody.\n\n## B\n\nMore.\n", "# A  \n\n## B?\n"}
	first := DeriveBlocks(bodies[0])
	for _, body := range bodies[1:] {
		next := DeriveBlocks(body)
		if len(next) != len(first) {
			t.Fatalf("body edit moved anchors: %+v vs %+v", first, next)
		}
		for i := range first {
			if next[i].ID != first[i].ID {
				t.Fatalf("anchor unstable: %+v vs %+v", first, next)
			}
		}
	}
	if got := DeriveBlocks("# A\n"); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("single heading wrong: %+v", got)
	}
	if got := DeriveBlocks("no headings\n"); len(got) != 0 {
		t.Fatalf("paragraphs produced anchors: %+v", got)
	}
}

// TestTodo_HUB_023_Golden is the GOLDEN test for HUB-023: the canonical
// anchor manifest pins byte-for-byte.
func TestTodo_HUB_023_Golden(t *testing.T) {
	blocks := DeriveBlocks("# Guide\n\n## Intro\n\nText.\n\n## Intro\n\n### Deep Dive!\n")
	assertGoldenJSON(t, "testdata/hub023_blocks.golden.json", blockManifest(blocks))
}
