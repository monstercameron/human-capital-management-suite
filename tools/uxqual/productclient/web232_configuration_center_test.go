package productclient

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The browser client's view composition carries the island's configuration
// center projection onto the view, and fails closed when a composed session
// carries none.
func TestTodo_WEB_232_LoadingView(t *testing.T) {
	state := State{Page: productui.PageConfigurationCenter, Request: productui.PageRequest{Page: productui.PageConfigurationCenter}}
	base := Session{Tenant: "tenant", Principal: "worker", Roles: []string{"worker_self"}}

	missing := LoadingView(base, state)
	if missing.ConfigurationCenter != nil {
		t.Fatal("a composed session without a configuration center projection did not fail closed")
	}

	live := base
	live.ConfigurationCenter = &productui.ConfigurationCenterProjection{Ready: true, Sections: []productui.ConfigurationSectionProjection{
		{ID: "appearance", Label: "Brand and appearance", Href: "/workspace/app/admin/appearance", State: "published", Version: "v3"},
	}}
	view := LoadingView(live, state)
	if view.ConfigurationCenter == nil || !view.ConfigurationCenter.Ready || len(view.ConfigurationCenter.Sections) != 1 {
		t.Fatalf("live projection did not reach the view: %+v", view.ConfigurationCenter)
	}
}
