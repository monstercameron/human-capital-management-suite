package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_009(t *testing.T) {
	people := make([]Person, 120)
	for index := range people {
		people[index] = Person{ID: fmt.Sprintf("worker-%03d", index), Name: fmt.Sprintf("Worker %03d", index)}
	}
	markup, err := ui.RenderToString(RolesPage(RolesPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		People:    people, Roles: []AccessRole{{ID: "viewer", Name: "Viewer", Description: "Full description", Active: true}},
		FilterHref: "/workspace/app/admin/roles", CanCreate: true, OnSaveRole: func(AccessRole) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="data-table`) || !strings.Contains(markup, `data-worker-ref="worker-019"`) {
		t.Fatalf("roles workforce should use a bounded, scannable data region: table=%v row=%v", strings.Contains(markup, `class="data-table`), strings.Contains(markup, `data-worker-ref="worker-019"`))
	}
	if !strings.Contains(markup, `data-worker-ref="worker-000"`) || strings.Contains(markup, `data-worker-ref="worker-100"`) {
		t.Fatal("role directory rendered outside its bounded first window")
	}
	if !strings.Contains(markup, "1–20 / 120") || !strings.Contains(markup, "role_page=2") {
		t.Fatal("role directory omitted paging or selection affordances")
	}
	if !strings.Contains(markup, "Full description") || !strings.Contains(markup, "Create a role") {
		t.Fatal("role definitions and stable create action must remain discoverable")
	}
}

func TestTodo_UXAUDIT_009_Integration(t *testing.T) {
	markup, err := ui.RenderToString(RolesPage(RolesPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		People:    []Person{{ID: "worker-1", Name: "Priya Patel"}},
		Roles:     []AccessRole{{ID: "viewer", Name: "Viewer", Active: true}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "bulk save") || strings.Contains(markup, "Bulk assign") {
		t.Fatal("the UI must not fabricate a bulk mutation contract")
	}
	if !strings.Contains(markup, "saved per worker and validated by the server") {
		t.Fatal("per-worker governed result expectation is not disclosed")
	}
}

func TestTodo_UXAUDIT_009_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(RolesPage(RolesPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		People:    []Person{{ID: "worker-1", Name: "Priya Patel"}},
		Roles:     []AccessRole{{ID: "viewer", Name: "Viewer", Active: true}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="region"`, `aria-label="Employee role assignments"`, `scope="col"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("roles surface missing accessible contract %q", want)
		}
	}
}

func TestTodo_UXAUDIT_009_Performance(t *testing.T) {
	people := make([]Person, 1000)
	for index := range people {
		people[index] = Person{ID: fmt.Sprintf("worker-%d", index), Name: fmt.Sprintf("Worker %d", index)}
	}
	markup, err := ui.RenderToString(RolesPage(RolesPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, People: people,
		Roles: []AccessRole{{ID: "viewer", Name: "Viewer", Active: true}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, `data-worker-ref=`) != 20 {
		t.Fatalf("bounded directory rendered %d rows, want 20", strings.Count(markup, `data-worker-ref=`))
	}
}

func TestTodo_UXAUDIT_009_Security(t *testing.T) {
	markup, err := ui.RenderToString(RolesPage(RolesPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		People:    []Person{{ID: "worker-1", Name: "Priya Patel"}}, Roles: []AccessRole{{ID: "viewer", Name: "Viewer", Active: true}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "validated by the server") || strings.Contains(markup, "effective access: viewer") {
		t.Fatal("browser must not claim effective authorization truth")
	}
}
