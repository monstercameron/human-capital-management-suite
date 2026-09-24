package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesGalleryOfEveryKind(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nested", "gallery.html")
	if err := run(out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, kind := range []string{"flowchart", "sequence", "pie", "xychart", "gantt", "timeline", "journey"} {
		if !strings.Contains(page, `class="docs-diagram docs-diagram-`+kind+`"`) {
			t.Errorf("gallery lacks a %s diagram", kind)
		}
	}
	if !strings.Contains(page, ".docs-diagram .series-7{") || !strings.Contains(page, `id="dark"`) {
		t.Error("gallery lacks the stylesheet or the dark toggle")
	}
	if strings.Count(page, "could not be drawn") != 1 {
		t.Error("exactly the unsupported example should fall back to code")
	}
}

func TestRunReportsUnwritablePath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(filepath.Join(file, "gallery.html")); err == nil {
		t.Fatal("writing beneath a file should fail")
	}
}
