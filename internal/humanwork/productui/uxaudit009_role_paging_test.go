package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_009_RolePageWindowAndAddress(t *testing.T) {
	people := make([]Person, 61)
	for index := range people {
		people[index] = Person{ID: fmt.Sprintf("worker-%02d", index), Name: fmt.Sprintf("Worker %02d", index)}
	}
	view := NewView(PageRoles, "Harborcare", "admin", "admin")
	view.People = people
	view = ApplyRequest(view, PageRequest{Page: PageRoles, RolePage: 3})
	if view.RolePage != 3 || !strings.Contains(currentPageHref(view, false), "role_page=3") {
		t.Fatalf("role page lost from admitted view or address: page=%d href=%s", view.RolePage, currentPageHref(view, false))
	}
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-worker-ref="worker-40"`) || !strings.Contains(markup, `data-worker-ref="worker-59"`) || strings.Contains(markup, `data-worker-ref="worker-20"`) {
		t.Fatal("page three did not render its own bounded employee window")
	}
	if !strings.Contains(markup, "role_page=2") || !strings.Contains(markup, "role_page=4") {
		t.Fatal("role directory lacks adjacent page navigation")
	}
	view = ApplyRequest(view, PageRequest{Page: PageRoles, RolePage: 500})
	if view.RolePage != 4 {
		t.Fatalf("out-of-range role page = %d, want 4", view.RolePage)
	}
}

func TestRoleDirectoryPagingClampsFilteredAndEmptyPopulations(t *testing.T) {
	people := make([]Person, 41)
	for index := range people {
		people[index] = Person{ID: fmt.Sprintf("worker-%02d", index), Name: fmt.Sprintf("Worker %02d", index)}
	}
	for _, test := range []struct {
		name, query string
		page, want  int
	}{
		{name: "empty", query: "nobody", page: 50, want: 1},
		{name: "negative", page: -1, want: 1},
		{name: "last-partial", page: 50, want: 3},
		{name: "filtered-single", query: "Worker 40", page: 2, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := roleDirectoryWindowPage(people, test.query, test.page); got != test.want {
				t.Fatalf("page = %d, want %d", got, test.want)
			}
		})
	}
}
