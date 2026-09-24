package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestTodo_TOOL_011_Golden pins the diagnostic path emitted for a known
// formatting violation, including normalized relative-path formatting.
func TestTodo_TOOL_011_Golden(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "drift.go"), []byte("package drift\nfunc f(){x:=1;_ = x}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := gofmtViolations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"drift.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gofmt diagnostic paths = %v, want %v", got, want)
	}
}
