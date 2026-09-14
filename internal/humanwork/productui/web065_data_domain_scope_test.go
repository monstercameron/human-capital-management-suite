package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-065: data-domain presentation scope. The role-visibility
// editor resolves which organization units a role may discover, but it
// never resolves which data domains its surfaces may present: every
// domain reads as admitted regardless of policy. Each editor must resolve
// the admitted domains — granted names intersected with the available
// spec domains — through one pure resolver that never invents scope:
// unknown names and blank names resolve to nothing, matching is
// case-insensitive, output carries available-casing, sorted and
// de-duplicated, inputs never mutated.
func TestTodo_WEB_065(t *testing.T) {
	available := []string{"People", "Position", "Organization", "Compensation"}
	for _, resolution := range []struct {
		granted   []string
		available []string
		want      []string
	}{
		{nil, available, nil},
		{[]string{"People"}, available, []string{"People"}},
		{[]string{"people", "PEOPLE", " Position "}, available, []string{"People", "Position"}},
		{[]string{"Compensation", "Organization", "Position", "People"}, available, []string{"Compensation", "Organization", "People", "Position"}},
		{[]string{"Ghost"}, available, nil},
		{[]string{"Payroll", "Ghost"}, available, nil},
		{[]string{"", "  "}, available, nil},
		{[]string{"People"}, nil, nil},
		{nil, nil, nil},
		{[]string{"People"}, []string{"people"}, []string{"people"}},
		{[]string{"people", "Payroll"}, available, []string{"People"}},
	} {
		if got := ResolveDataDomainScope(resolution.granted, resolution.available); !reflect.DeepEqual(got, resolution.want) {
			t.Fatalf("ResolveDataDomainScope(%q, %q) = %q, want %q", resolution.granted, resolution.available, got, resolution.want)
		}
	}
	before := []string{"people", "Position"}
	_ = ResolveDataDomainScope(before, available)
	if !reflect.DeepEqual(before, []string{"people", "Position"}) {
		t.Fatal("domain scope resolution mutates its inputs")
	}

	// Only a role with admitted domains displays a domain scope. An ungranted
	// role must not imply that the server supplied an empty domain projection.
	doc, err := Render(web065View("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if body := textContent(root); !strings.Contains(body, "Data domains") {
		t.Fatal("visibility page resolves no data-domain scope")
	}
	manager := web064RoleScope(t, root, "manager", "data-domain-scope")
	for _, want := range []string{"Data domains", "People", "Position"} {
		if !strings.Contains(manager, want) {
			t.Fatalf("manager domain scope misses %q: %q", want, manager)
		}
	}
	if findClassToken(supportNode(t, root, "support"), "data-domain-scope") != nil {
		t.Fatal("support role fabricated an empty data-domain scope")
	}
	if chips := countClassTokens(supportNode(t, root, "support"), "data-domain-scope-domain"); chips != 0 {
		t.Fatalf("ungranted role renders %d domain chips", chips)
	}
}

// supportNode returns the role editor subtree for one role ID.
func supportNode(t *testing.T, root *xhtml.Node, roleID string) *xhtml.Node {
	t.Helper()
	var editor *xhtml.Node
	var find func(node *xhtml.Node)
	find = func(node *xhtml.Node) {
		if editor != nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if (attr.Key == "data-role-id" || attr.Key == "data-hcm-role-id") && attr.Val == roleID {
					editor = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			find(child)
		}
	}
	find(root)
	if editor == nil {
		t.Fatalf("no editor for role %q", roleID)
	}
	return editor
}

// countClassTokens counts descendants (and self) carrying token as a whole
// whitespace-separated class token.
func countClassTokens(root *xhtml.Node, token string) int {
	count := 0
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "class" {
					for _, field := range strings.Fields(attr.Val) {
						if field == token {
							count++
						}
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return count
}

// Golden: the domain-scope resolution matrix digest.
func TestTodo_WEB_065_Golden(t *testing.T) {
	var builder strings.Builder
	available := []string{"People", "Position", "Organization", "Compensation"}
	for _, granted := range [][]string{nil, {"People"}, {"people", "Position"}, {"Payroll", "Ghost"}, {"Compensation", "Organization", "Position", "People"}} {
		builder.WriteString(strings.Join(granted, ","))
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(available, ","))
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(ResolveDataDomainScope(granted, available), ","))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "55ea6ea21d08fe50b5d6fcabfc3f70747702d1f20fa3449ef07cd19348ced065"
	if got != want {
		t.Fatalf("domain scope matrix digest = %s, want %s", got, want)
	}
}

// Browser: the armed visibility document carries only admitted data-domain
// scope blocks, with list semantics and no positive
// tabindex stops.
func TestTodo_WEB_065_Browser(t *testing.T) {
	doc, err := Render(web065View("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if count := countClassTokens(root, "data-domain-scope"); count != 1 {
		t.Fatalf("visibility document resolves %d data-domain scopes, want 1 admitted role", count)
	}
	for _, roleID := range []string{"manager"} {
		block := findClassToken(supportNode(t, root, roleID), "data-domain-scope")
		if block == nil {
			t.Fatalf("role %q resolves no data-domain scope block", roleID)
		}
		var heading, list, empty bool
		var walk func(node *xhtml.Node)
		walk = func(node *xhtml.Node) {
			if node.Type == xhtml.ElementNode {
				if node.Data == "h3" {
					heading = true
				}
				if node.Data == "ul" || node.Data == "p" {
					for _, attr := range node.Attr {
						if attr.Key == "class" {
							for _, field := range strings.Fields(attr.Val) {
								if field == "data-domain-scope-domains" {
									list = true
								}
								if field == "data-domain-scope-empty" {
									empty = true
								}
							}
						}
					}
				}
				for _, attr := range node.Attr {
					if attr.Key == "tabindex" && strings.TrimSpace(attr.Val) != "" && attr.Val != "0" && !strings.HasPrefix(attr.Val, "-") {
						t.Fatalf("role %q domain block carries positive tabindex %q", roleID, attr.Val)
					}
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(block)
		if !heading {
			t.Fatalf("role %q domain block has no accessible heading", roleID)
		}
		if !list && !empty {
			t.Fatalf("role %q domain block has neither a domain list nor the empty state", roleID)
		}
	}
}

// Conformance: domain-scope copy in three locales, resolver determinism.
func TestTodo_WEB_065_Conformance(t *testing.T) {
	for locale, title := range map[string]string{"en-US": "Data domains", "de-DE": "Datenbereiche", "ar": "مجالات البيانات"} {
		lc := ResolveProductLocale(locale)
		if got := lc.Text("data_domain_scope.title"); got != title {
			t.Fatalf("%s domain scope title reads %q, want %q", locale, got, title)
		}
		if key := lc.Text("data_domain_scope.empty"); strings.Contains(key, "⟦") {
			t.Fatalf("%s domain scope empty unresolved: %q", locale, key)
		}
		doc, err := Render(web065View(locale))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("%s visibility render leaks an unresolved key", locale)
		}
	}
	first := ResolveDataDomainScope([]string{"people", "Payroll"}, []string{"People", "Position", "Organization", "Compensation"})
	second := ResolveDataDomainScope([]string{"people", "Payroll"}, []string{"People", "Position", "Organization", "Compensation"})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("domain scope resolution is nondeterministic")
	}
}

// Security: presentation withholds every non-admitted domain. A role
// granted only workforce domains never renders compensation or
// organization chips, and a policy naming only unknown domains resolves
// to the empty state with no chips at all.
func TestTodo_WEB_065_Security(t *testing.T) {
	doc, err := Render(web065View("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	manager := web064RoleScope(t, root, "manager", "data-domain-scope")
	for _, denied := range []string{"Organization", "Compensation", "Ghost", "Payroll"} {
		if strings.Contains(manager, denied) {
			t.Fatalf("manager domain scope leaks non-admitted %q: %q", denied, manager)
		}
	}
	view := web065View("en-US")
	view.AccessRoles = append(view.AccessRoles, AccessRole{ID: "contractor", Name: "Contractor", Active: true})
	view.RoleVisibilityPolicies = append(view.RoleVisibilityPolicies, OrganizationVisibilityPolicy{RoleID: "contractor", Mode: "ALLOWLIST", OrganizationUnits: []string{"Platform"}, DataDomains: []string{"Payroll", "Ghost"}})
	unknown, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	unknownRoot, err := xhtml.Parse(strings.NewReader(unknown))
	if err != nil {
		t.Fatal(err)
	}
	if findClassToken(supportNode(t, unknownRoot, "contractor"), "data-domain-scope") != nil {
		t.Fatal("unknown-domain policy rendered an unsupported domain scope")
	}
}

func web065View(locale string) View {
	view := testView(PageOrganizationVisibility)
	view.Locale = ResolveProductLocale(locale)
	view = ApplyLocale(view, view.Locale)
	view.People = []Person{
		{ID: "worker-ava", Name: "Ava Reyes", Team: "Platform"},
		{ID: "worker-ivan", Name: "Ivan Petrov", Team: "Engineering"},
		{ID: "worker-dana", Name: "Dana Kim", Team: "Design"},
	}
	view.AccessRoles = []AccessRole{
		{ID: "manager", Name: "Manager", Active: true},
		{ID: "support", Name: "Support", Active: true},
		{ID: "archived", Name: "Archived", Active: false},
	}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{
		{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering"}, DataDomains: []string{"people", "Position"}},
		{RoleID: "support", Mode: "DENYLIST", OrganizationUnits: []string{"Platform"}},
	}
	return view
}
