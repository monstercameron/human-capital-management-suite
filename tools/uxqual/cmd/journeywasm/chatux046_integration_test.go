//go:build !(js && wasm)

package main

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_046_Integration proves the todo's headline claim against
// the real artifact: it builds the browser client from the current sources
// exactly as the gate does and requires at least 256 KB of headroom below
// each size ceiling (1 MiB of raw room, so a small change cannot silently eat
// the margin, and 256 KiB of transferred room).
func TestTodo_CHATUX_046_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("compiling a wasm module takes tens of seconds; skipped under -short")
	}
	if _, err := goBinary(); err != nil {
		t.Skipf("no go toolchain available to build with: %v", err)
	}
	raw, gzip, buildErr := measureClientBundle(t)
	problems := clientBundleHeadroomProblems(raw, gzip)
	if buildErr != nil {
		if len(problems) != 0 {
			t.Fatalf("the browser client build failed: %v\nThe browser client is running out of room:\n  %s",
				buildErr, strings.Join(problems, "\n  "))
		}
		t.Fatalf("the browser client build failed despite measurable bundle files: %v", buildErr)
	}
	if len(problems) != 0 {
		t.Fatalf("the browser client has less than 256 KB of headroom:\n  %s\n"+
			"Do not raise the ceilings in performance_budget.go. Make the client smaller.",
			strings.Join(problems, "\n  "))
	}
}
