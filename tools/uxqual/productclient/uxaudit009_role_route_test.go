package productclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UXAUDIT_009_RolePageRouteRoundTrip(t *testing.T) {
	state, err := ParseState(productui.Path(productui.PageRoles), "locale=de-DE&q=Rafael&role_page=3")
	if err != nil {
		t.Fatal(err)
	}
	if state.Request.Query != "Rafael" || state.Request.RolePage != 3 {
		t.Fatalf("role route projection = %+v", state.Request)
	}
	href := CanonicalHref(state)
	if !strings.Contains(href, "q=Rafael") || !strings.Contains(href, "role_page=3") {
		t.Fatalf("canonical role route discarded page/query: %s", href)
	}
	view := productui.NewView(productui.PageRoles, "Harborcare", "admin", "admin")
	view.People = []productui.Person{{ID: "worker-1", Name: "Rafael"}}
	view = productui.ApplyRequest(view, state.Request)
	if view.RolePage != 1 || strings.Contains(ResolvedCanonicalHref(state, view), "role_page=3") {
		t.Fatalf("resolved route did not clamp out-of-range page: view=%d href=%s", view.RolePage, ResolvedCanonicalHref(state, view))
	}
	if _, err := ParseState(productui.Path(productui.PageRoles), "role_page=-1"); err == nil {
		t.Fatal("malformed role page was accepted")
	}
}
