package chatui

import (
	"strings"
	"testing"
)

func TestPersonDepartmentName(t *testing.T) {
	for in, want := range map[string]string{
		"project-management": "Project Management",
		"people_ops":         "People Ops",
		"Project Management": "Project Management",
		"Safety & Quality":   "Safety & Quality",
		"Sales":              "Sales",
		"sales":              "sales",
		"":                   "",
	} {
		if got := personDepartmentName(in); got != want {
			t.Errorf("personDepartmentName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A seeded person: every row has a value, the department reads as a name, and
// the manager and report avatars use the photo Chat already holds for them
// even when the details did not carry one.
func TestPersonDetailsSeededPersonShowsEveryFieldAndPhotos(t *testing.T) {
	m := Model{State: StateReady, ShowPerson: true,
		PhotoURLs: map[string]string{"priya": "/workspace/assets/person-hc-031-small.jpg", "sofia": "/workspace/assets/person-hc-027-small.jpg"},
		PersonDetails: &PersonDetails{ID: "greg", Name: "Greg Novak", JobTitle: "Project Manager", Manager: "Priya Raman", ManagerID: "priya",
			Department: "project-management", Phone: "(303) 555-0105", Email: "greg.novak@ironridge.example", Location: "Denver, CO",
			Company: "Ironridge Builders, Inc.", BusinessUnit: "Operations", Ready: true,
			DirectReports: []PersonLink{{ID: "sofia", Name: "Sofia Beltran"}}},
		Callbacks: Callbacks{OpenPerson: func(string) {}, ClosePerson: func() {}},
	}
	markup := render(t, m)
	for _, want := range []string{"Project Management", "(303) 555-0105", "greg.novak@ironridge.example", "Denver, CO", "Operations",
		`src="/workspace/assets/person-hc-031-small.jpg"`, `src="/workspace/assets/person-hc-027-small.jpg"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q in %s", want, markup)
		}
	}
	for _, bad := range []string{"Not available", ">project-management<"} {
		if strings.Contains(markup, bad) {
			t.Errorf("printed %q in %s", bad, markup)
		}
	}
}
