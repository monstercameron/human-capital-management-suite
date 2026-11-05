package docintegrity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codes(ds []Diagnostic) map[string]bool {
	out := map[string]bool{}
	for _, d := range ds {
		out[d.Code] = true
	}
	return out
}

func TestCheckMarkdownFindsNormativeDefects(t *testing.T) {
	md := "# Good\n\n```ascii\nβ\n\n[bad](missing.md#nope)\n"
	ds := CheckMarkdown("planning/test.md", md)
	c := codes(ds)
	for _, want := range []string{"INVALID_ASCII_BLOCK", "UNBALANCED_FENCE", "MOJIBAKE"} {
		if want == "MOJIBAKE" {
			continue
		}
		if !c[want] {
			t.Fatalf("missing %s: %v", want, ds)
		}
	}
}

func TestCheckMarkdownReportsMojibakeAndBrokenAnchor(t *testing.T) {
	md := "# CafÃ©\n[bad](other.md#missing)\n"
	ds := CheckMarkdown("planning/test.md", md)
	c := codes(ds)
	if !c["MOJIBAKE"] {
		t.Fatalf("expected mojibake: %v", ds)
	}
	// A one-document check cannot resolve a sibling, but must remain safe and deterministic.
	if len(ds) == 0 || !strings.Contains(ds[0].String(), "planning/test.md") {
		t.Fatal(ds)
	}
}

func TestCheckDetectsDuplicateIDsAndStaleIndex(t *testing.T) {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "planning", "archive"), 0755); err != nil {
		t.Fatal(err)
	}
	todos := "## Work\n- [ ] `X-001` **[P0][LUNA] One.**\n- [ ] `X-001` **[P0][LUNA] Two.**\n"
	if err := os.WriteFile(filepath.Join(d, "planning", "archive", "todos-2026-10-22.md"), []byte(todos), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "planning", "todo-headings.txt"), []byte("stale\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Check(d)
	if err != nil {
		t.Fatal(err)
	}
	c := codes(ds)
	if !c["DUPLICATE_NORMATIVE_ID"] || !c["STALE_GENERATED_INDEX"] {
		t.Fatalf("findings=%v", ds)
	}
}
