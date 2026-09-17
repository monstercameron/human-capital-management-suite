package productui

import (
	"fmt"
	"strings"
	"testing"
)

// TestTodo_UXAUDIT_009_Browser is the BROWSER matrix test for
// planning/todos.md's UXAUDIT-009. By this repo's testing convention (see
// TestTodo_UX_002_Browser) it is a Go-side check of the served document, so
// it needs no JS engine to run: the scaled roles surface must stay operable
// from the rendered document alone. The filter resolves to a real GET form,
// directory paging to real links carrying the page address, the create-role
// and per-worker assignment controls to named, labeled submittable
// controls, and the long directory to a keyboard-focusable scroll region --
// so a browser without the wasm client can still search, page, create and
// assign, with the wasm client only progressively enhancing them.
func TestTodo_UXAUDIT_009_Browser(t *testing.T) {
	view := testView(PageRoles)
	view.EffectivePermissions = []RolePagePermission{{Page: PageRoles, View: true, Create: true, Update: true}}
	view.SaveAccessRole = func(AccessRole) {}
	view.SaveWorkerRoleAssignment = func(WorkerRoleAssignment) {}
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Description: "Full description", Active: true}}
	people := make([]Person, 41)
	for index := range people {
		people[index] = Person{ID: fmt.Sprintf("worker-%02d", index), Name: fmt.Sprintf("Worker %02d", index)}
	}
	view.People = people
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	// The filter degrades to a plain GET against the roles route: the named
	// query input and its submit control are in the document, not behind
	// wasm.
	for _, want := range []string{`<form`, `action="/workspace/app/admin/roles`, `method="get"`, `name="q"`, `type="submit"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("role filter has no document-level fallback bearing %q", want)
		}
	}
	// The first window is bounded and addressed: the browser can follow the
	// next-page link without client state, and workers outside the window
	// are not in the document at all.
	for _, want := range []string{"1–20 / 41", "role_page=2", `data-worker-ref="worker-00"`, `data-worker-ref="worker-19"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("role directory first window missing %q", want)
		}
	}
	if strings.Contains(doc, `data-worker-ref="worker-20"`) {
		t.Error("role directory first window leaks workers outside its bounded window")
	}
	// The stable action area precedes the long directory, and the create
	// controls are labeled inputs with a submit -- not a control that only
	// exists once wasm hydrates.
	if catalog, assignments := strings.Index(doc, `id="role-catalog"`), strings.Index(doc, `id="role-assignments"`); catalog < 0 || assignments < 0 || catalog >= assignments {
		t.Error("create-role action area must precede the long worker directory in the served document")
	}
	for _, want := range []string{`id="new-role-id"`, `name="role_id"`, `id="new-role-name"`, `name="role_name"`, `id="new-role-description"`, `name="role_description"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("create-role fallback missing %q", want)
		}
	}
	// Each directory row carries a named assignment control set bound to
	// its own form, so per-worker assignment stays submittable per worker.
	for _, want := range []string{`id="employee-role-form-worker-00"`, `form="employee-role-form-worker-00"`, `name="role"`, "Save employee roles"} {
		if !strings.Contains(doc, want) {
			t.Errorf("per-worker assignment fallback missing %q", want)
		}
	}
	// The bounded directory is a keyboard-focusable region with an
	// accessible name: narrow-screen and keyboard users can reach the
	// scrolled controls the visual viewport hides. Attribute names are
	// ASCII case-insensitive in HTML, so the match lowers the document
	// rather than pinning the serializer's tabIndex casing.
	folded := strings.ToLower(doc)
	if !strings.Contains(folded, `tabindex="0"`) {
		t.Error(`role directory scroll region is not keyboard-focusable (no tabindex="0")`)
	}
	for _, want := range []string{`role="region"`, `aria-label="Employee role assignments"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("role directory scroll region missing %q", want)
		}
	}

	// A middle window links both directions and renders only its own
	// workers: paging is navigation, not a client-side filter over a full
	// dump.
	paged := ApplyRequest(view, PageRequest{Page: PageRoles, RolePage: 2})
	pagedDoc, err := Render(paged)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"21–40 / 41", "role_page=1", "role_page=3", `data-worker-ref="worker-20"`, `data-worker-ref="worker-39"`} {
		if !strings.Contains(pagedDoc, want) {
			t.Errorf("role directory second window missing %q", want)
		}
	}
	for _, forbidden := range []string{`data-worker-ref="worker-00"`, `data-worker-ref="worker-40"`} {
		if strings.Contains(pagedDoc, forbidden) {
			t.Errorf("role directory second window leaks %q from another window", forbidden)
		}
	}
}
