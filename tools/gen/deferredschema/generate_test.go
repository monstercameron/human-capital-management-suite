package deferredschema

import (
	"path/filepath"
	"testing"
)

// pinnedPreviewDigest is DB-016's golden value: the exact sha256 digest of
// today's generated preview set (ten migration previews plus the
// disposition preview), computed by Generate().Digest(). Any change to
// domains.go, ddl.go or disposition.go that alters a single byte of the
// preview set must update this constant deliberately, in the same change
// that also regenerates testdata/preview.
const pinnedPreviewDigest = "sha256:d7ebdf78cecd22a8a6a19aec97cc1cb81208e60b3ca7fa2bfb72fc11e527c1a4"

// TestTodo_DB_016_Golden pins Generate's digest and proves the checked-in
// testdata/preview files are byte-identical to what Generate produces today,
// so the previews can never silently drift from this package's own logic
// (the same drift class MSRC-010 polices for the generated model/database
// path).
func TestTodo_DB_016_Golden(t *testing.T) {
	set, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := set.Digest(); got != pinnedPreviewDigest {
		t.Fatalf("preview set digest = %s, want %s (pinned). If this change is intentional, "+
			"update pinnedPreviewDigest and regenerate testdata/preview to match", got, pinnedPreviewDigest)
	}

	root := testRepoRoot(t)
	onDisk, err := ReadPreviewSet(filepath.Join(root, "tools", "gen", "deferredschema", "testdata", "preview"))
	if err != nil {
		t.Fatalf("ReadPreviewSet: %v", err)
	}
	if len(onDisk.Files) != len(set.Files) {
		t.Fatalf("testdata/preview has %d files, Generate() produced %d", len(onDisk.Files), len(set.Files))
	}
	for _, f := range set.Files {
		got, ok := onDisk.Lookup(f.Name)
		if !ok {
			t.Fatalf("testdata/preview is missing generated file %s", f.Name)
		}
		if got != f.Content {
			t.Fatalf("testdata/preview/%s has drifted from Generate()'s output", f.Name)
		}
	}
}

func TestGenerateDeterministicAcrossCalls(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest() != b.Digest() {
		t.Fatal("Generate() is not deterministic across calls")
	}
}

func TestGenerateProducesElevenFiles(t *testing.T) {
	set, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 11 {
		t.Fatalf("Generate() produced %d files, want 11 (10 migration previews + 1 disposition preview)", len(set.Files))
	}
	if _, ok := set.Lookup("storage-disposition.deferred.yaml"); !ok {
		t.Fatal("Generate() did not produce storage-disposition.deferred.yaml")
	}
	for _, d := range Domains() {
		if _, ok := set.Lookup(d.PreviewFileName()); !ok {
			t.Fatalf("Generate() did not produce %s", d.PreviewFileName())
		}
	}
}
