package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// RED for WEB-229: the authorization-resolved Admin
// home. The registry owns the Admin home, but the home
// is not authorization-resolved: it links every
// capability card — Roles & access, Organization
// visibility, Worker ID rules — to every admitted
// viewer, including viewers who cannot open those
// pages. The compiler needs the home to resolve its
// cards through the same authorization the registry
// enforces, so non-admin viewers are never offered
// admin routes while the unrestricted component preview
// keeps its full card set.
func TestTodo_WEB_229(t *testing.T) {
	adminView := ApplyRoleVisibility(testView(PageAdmin), []string{RoleHCMAdmin})
	adminDoc, err := Render(adminView)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range []string{"Roles &amp; access", "Organization visibility", "Worker ID rules", "Brand &amp; appearance", "Promotion workflows", "Experience configuration"} {
		if !strings.Contains(adminDoc, card) {
			t.Fatalf("admin home hides %q from the platform admin", card)
		}
	}
	workerView := ApplyRoleVisibility(testView(PageAdmin), []string{"worker_self"})
	workerDoc, err := Render(workerView)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/workspace/app/admin/roles", "/workspace/app/admin/organization-visibility", "/workspace/app/admin/worker-ids"} {
		if strings.Contains(workerDoc, route) {
			t.Fatalf("admin home offers %q to a viewer who cannot open it", route)
		}
	}
	rolelessDoc, err := Render(ApplyRoleVisibility(testView(PageAdmin), nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/workspace/app/admin/roles", "/workspace/app/admin/organization-visibility", "/workspace/app/admin/worker-ids", "/workspace/app/appearance", "/workspace/app/studio"} {
		if strings.Contains(rolelessDoc, route) {
			t.Fatalf("admin home offers %q to a role-less viewer", route)
		}
	}
	if strings.Contains(workerDoc, "⟦") || strings.Contains(adminDoc, "⟦") {
		t.Fatal("admin home exposes an unresolved message key")
	}
}

// adminHomeCardsForRoles resolves the Admin home
// capability titles visible to admitted roles.
func adminHomeCardsForRoles(t *testing.T, roles []string) []string {
	t.Helper()
	doc, err := Render(ApplyRoleVisibility(testView(PageAdmin), roles))
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, card := range []string{"Roles &amp; access", "Organization visibility", "Worker ID rules", "Brand &amp; appearance", "Promotion workflows", "Experience configuration"} {
		if strings.Contains(doc, card) {
			titles = append(titles, card)
		}
	}
	sort.Strings(titles)
	return titles
}

// Golden: the authorization-resolved card set per
// admitted role.
func TestTodo_WEB_229_Golden(t *testing.T) {
	golden := ""
	for _, roles := range [][]string{{RoleHCMAdmin}, {"manager"}, {"worker_self"}, {}} {
		golden += fmt.Sprintf("%v=%s\x00", roles, strings.Join(adminHomeCardsForRoles(t, roles), ","))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "cd77ec18d3238bb09ea043015a1417cb08b531dfe3283048057dba1fc97088bf"
	if got != want {
		t.Fatalf("admin home resolution digest = %s, want %s", got, want)
	}
}

// Browser: the resolved Admin home renders
// deterministically per admitted role.
func TestTodo_WEB_229_Browser(t *testing.T) {
	for _, roles := range [][]string{{RoleHCMAdmin}, {"manager"}, {"worker_self"}, {}} {
		first, err := Render(ApplyRoleVisibility(testView(PageAdmin), roles))
		if err != nil {
			t.Fatal(err)
		}
		second, err := Render(ApplyRoleVisibility(testView(PageAdmin), roles))
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Fatalf("admin home renders nondeterministically for %v", roles)
		}
	}
}

// Conformance: the Admin home keeps the registry
// contract — reachable by the platform admin, hidden
// from the role-less baseline and the plain employee,
// and honest in every locale.
func TestTodo_WEB_229_Conformance(t *testing.T) {
	if !PageVisible(PageAdmin, []string{RoleHCMAdmin}) {
		t.Fatal("admin home hidden from the platform admin")
	}
	if PageVisible(PageAdmin, nil) {
		t.Fatal("admin home visible without roles")
	}
	if PageVisible(PageAdmin, []string{"worker_self"}) {
		t.Fatal("admin home visible to the plain employee")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(ApplyRoleVisibility(testView(PageAdmin), []string{RoleHCMAdmin}), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("admin home leaks a key in %s", code)
		}
	}
}
