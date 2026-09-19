package productui

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// UXLIVE-020's RED was filed from a live observation that focus did not
// return to the "Page utilities" trigger after Escape. Reading the code
// corrected the diagnosis: the utility drawer does restore focus
// (drawer_focus_wasm.go returns focus to the previously focused element and
// falls back to its trigger), and the live measurement restored focus to
// where it actually was -- the element focused when the drawer was opened
// from a script rather than by the trigger.
//
// The real gap the same reading found is the action launcher: a dialog with
// aria-haspopup, aria-controls and aria-expanded that wired no focus trap at
// all, so opening it left focus behind and dismissing it returned focus
// nowhere. It now uses the same hook as the drawer.
//
// The behaviour itself is js/wasm-only (drawer_focus_native.go is a no-op on
// this test path), so this proves the wiring exists and is shared, which is
// what a browser test would need to already be true.

func uxlive020Source(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// TestTodo_UXLIVE_020 is the primary red/green test: every overlay that
// declares itself a dialog wires the shared focus contract.
func TestTodo_UXLIVE_020(t *testing.T) {
	launcher := uxlive020Source(t, "action_launcher.go")
	if !strings.Contains(launcher, `useDrawerFocusTrap("action-launcher-dialog", "action-launcher-trigger"`) {
		t.Fatalf("the action launcher declares a dialog but wires no focus contract")
	}

	drawer := uxlive020Source(t, "utility_drawer.go")
	if !strings.Contains(drawer, `useDrawerFocusTrap("utility-drawer-dialog", "utility-drawer-trigger"`) {
		t.Fatalf("the utility drawer lost its focus contract")
	}

	// One implementation, not two: the restore lives in the shared hook.
	trap := uxlive020Source(t, "drawer_focus_wasm.go")
	if !strings.Contains(trap, "previouslyFocused") || !strings.Contains(trap, `doc.Call("getElementById", triggerID)`) {
		t.Fatalf("the shared trap no longer restores focus to the opener or its trigger:\n%s", trap)
	}
}

// TestTodo_UXLIVE_020_Browser holds the line that every dialog in this
// package is wired to the shared hook, so a new overlay cannot ship without
// the contract.
func TestTodo_UXLIVE_020_Browser(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	dialogID := regexp.MustCompile(`ID:\s*"([a-z-]+-dialog)"`)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source := uxlive020Source(t, name)
		for _, match := range dialogID.FindAllStringSubmatch(source, -1) {
			id := match[1]
			if !strings.Contains(source, `useDrawerFocusTrap("`+id+`"`) {
				t.Fatalf("%s renders dialog %q without the shared focus contract", name, id)
			}
		}
	}
}
