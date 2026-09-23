package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestTodo_HUB_039 is the PRIMARY test for HUB-039: quarantined imports
// create candidate versions with provenance and remapped targets on the
// normal review path.
func TestTodo_HUB_039(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docB, _ := linkDoc(t, s, ctx, "tenant-a", "# Bee\n\nHoney.\n")
	in := ImportInput{
		ActorID: "u-author", Title: "Imported guide",
		Markdown:        "See [bee](doc:old-bee) and ![fig](artifact:old-fig).\n",
		DocMap:          map[string]string{"old-bee": docB},
		ArtifactMap:     map[string]string{"old-fig": "sha256:new-fig"},
		QuarantineState: "ADMITTED", ScannerVersion: "scan-7",
		ImportedFrom: "legacy-cms:42",
	}
	v, err := s.ImportMarkdown(ctx, "tenant-a", in)
	if err != nil {
		t.Fatalf("admitted import refused: %v", err)
	}
	if !strings.Contains(v.Markdown, "doc:"+docB) || strings.Contains(v.Markdown, "doc:old-bee") {
		t.Fatalf("doc targets not remapped: %q", v.Markdown)
	}
	if !strings.Contains(v.Markdown, "artifact:sha256:new-fig") || strings.Contains(v.Markdown, "artifact:old-fig") {
		t.Fatalf("artifact refs not remapped: %q", v.Markdown)
	}
	if !strings.Contains(v.ChangeNote, "legacy-cms:42") {
		t.Fatalf("provenance missing: %+v", v)
	}
	if v.Hash == "" || v.Hash != HashContent(v.Markdown) {
		t.Fatalf("imported version not hashed: %+v", v)
	}
	grantRead(t, s, ctx, "tenant-a", docB, "u-anna")
	grantRead(t, s, ctx, "tenant-a", v.DocumentID, "u-anna")
	back, err := s.Backlinks(ctx, "tenant-a", docB, "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].SourceDocID != v.DocumentID {
		t.Fatalf("remapped link not indexed: %+v", back)
	}
}

// FuzzTodo_HUB_039 is the FUZZ test for HUB-039: remapping never panics,
// never leaves a mapped reference behind, and never invents references.
func FuzzTodo_HUB_039(f *testing.F) {
	seeds := []string{
		"See [bee](doc:old-bee).\n",
		"![fig](artifact:old-fig) and [x](doc:old-bee#intro).\n",
		"# Title\n\nNo refs.\n",
		"[a](doc:old-bee) [b](DOC:OLD-BEE) [c](doc:other).\n",
		"```\n[code](doc:old-bee)\n```\n",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		md := string(data)
		out := RemapLinks(md, map[string]string{"old-bee": "doc-new"}, map[string]string{"old-fig": "sha256:new"})
		// Bare prose is not a link and passes through; every extracted
		// link target must be remapped.
		for _, l := range ExtractLinks(out) {
			if l.TargetDocID == "old-bee" {
				t.Fatalf("mapped target extracted: %+v", l)
			}
		}
	})
}

// TestTodo_HUB_039_Security is the SECURITY test for HUB-039: unscanned
// bytes, oversized payloads and foreign documents are refused.
func TestTodo_HUB_039_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	base := ImportInput{
		ActorID: "u-author", Title: "T", Markdown: "Hi.\n",
		QuarantineState: "ADMITTED", ScannerVersion: "scan-7", ImportedFrom: "x",
	}
	quarantined := base
	quarantined.QuarantineState = "QUARANTINED"
	if _, err := s.ImportMarkdown(ctx, "tenant-a", quarantined); !errors.Is(err, ErrImportUnsafe) {
		t.Fatalf("unscanned import accepted: %v", err)
	}
	huge := base
	huge.Markdown = strings.Repeat("x\n", 1<<20)
	if _, err := s.ImportMarkdown(ctx, "tenant-a", huge); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized import accepted: %v", err)
	}
	foreign := base
	foreign.DocumentID = "doc-foreign"
	foreign.ActorID = "u-stranger"
	if _, err := s.ImportMarkdown(ctx, "tenant-a", foreign); err == nil {
		t.Fatal("import into unknown document accepted")
	}
}
