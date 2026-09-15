package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_WEB_241_ServerRouteGateRequiresCoreFeature(t *testing.T) {
	access := productAccess{
		configured: true, featuresConfigured: true,
		permissions: []roleaccess.PagePermission{{PageID: "people", View: true, Create: true}},
		features:    []roleaccess.FeaturePermission{{PageID: "people", FeatureID: "content", View: true}},
	}
	if !access.can(productui.PagePeople, roleaccess.ActionView) {
		t.Fatal("page content feature did not admit the page")
	}
	if access.can(productui.PagePeople, roleaccess.ActionCreate) {
		t.Fatal("page action was admitted without the core actions feature")
	}
	access.features = append(access.features, roleaccess.FeaturePermission{PageID: "people", FeatureID: "actions", View: true, Create: true})
	if !access.can(productui.PagePeople, roleaccess.ActionCreate) {
		t.Fatal("matching page and core action grants were denied")
	}
}

func TestTodo_WEB_241_Conformance(t *testing.T) {
	config := JourneyConfig{
		TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token",
		FeaturePermissions: []roleaccess.FeaturePermission{},
	}
	doc, err := productShellDocumentForRoute(config, true, productui.ResolveProductLocale("en-US"), productui.PageHome)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `"feature_permissions":[]`) {
		t.Fatal("shell omitted the authoritative empty feature projection")
	}
}
