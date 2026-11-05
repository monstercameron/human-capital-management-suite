package workspace

import (
	"strings"
	"testing"
)

// UXBLIND-053 addendum (2026-10-23): the no-script sign-in tree must expose
// exactly one expanded/collapsed state per unit. The treeitem no longer
// carries its own aria-expanded (no script can keep it true after a native
// toggle); the <details>/<summary> disclosure is the single authoritative
// source in both initial-state directions.
func TestTodo_UXBLIND_053_DisclosureCoherence(t *testing.T) {
	h := newDirectoryHandler(t)

	unfiltered := getLoginPage(t, h, "")
	if strings.Contains(unfiltered, "aria-expanded") {
		t.Fatal("directory treeitems still claim an expand state no script can keep true")
	}
	// Both initial-state directions: open divisions and closed departments.
	if !strings.Contains(unfiltered, "<details open><summary>") {
		t.Fatal("initially open units lost their native disclosure state")
	}
	if !strings.Contains(unfiltered, "<details><summary>") {
		t.Fatal("initially closed units lost their native disclosure")
	}
	if !strings.Contains(unfiltered, `<li class="org-unit" role="treeitem" aria-level="3"><details>`) {
		t.Fatal("closed department is not a bare native disclosure")
	}

	searched := getLoginPage(t, h, "Evelyn")
	if strings.Contains(searched, "aria-expanded") {
		t.Fatal("filtered tree reintroduced a treeitem expand state")
	}
	if !strings.Contains(searched, `<li class="org-unit" role="treeitem" aria-level="3"><details open>`) {
		t.Fatal("the unit holding the match is not an open native disclosure")
	}
	if strings.Contains(searched, "<details><summary>") {
		t.Fatal("a filtered unit stayed closed behind its own disclosure")
	}
}
