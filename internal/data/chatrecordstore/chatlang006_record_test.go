package chatrecordstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_CHATLANG_006_Security states the record rule for translations: the
// original is the only record. Holds, retention and exports are built from the
// durable tables below, so a translation (chatrender_rendering) or anything the
// translation settings keep (chatlang_*) must never be one of them, and no
// record, hold, retention or export code may read them.
func TestTodo_CHATLANG_006_Security(t *testing.T) {
	for _, table := range durableTables {
		if strings.HasPrefix(table, "chatrender_") || strings.HasPrefix(table, "chatlang_") {
			t.Fatalf("a derived table is part of the record: %s", table)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal("no source files", err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, derived := range []string{"chatrender_rendering", "chatrender_job", "chatrender_report", "chatlang_"} {
			if strings.Contains(string(raw), derived) {
				t.Errorf("%s reads %s: a hold, retention or export must use the original", file, derived)
			}
		}
	}
}
