package productui

// UXAUDIT-004: "Render reporting lines as a coherent, navigable ownership
// tree." The live 2026-09-12 audit measured zero role="tree", zero
// role="treeitem" and zero aria-level elements on the reporting-lines view,
// while aria-expanded elements existed elsewhere on the page -- disclosure
// widgets assistive technology could not read as a hierarchy at all. That
// defect had a second, deeper cause this file's fixtures are built to prove
// closed: ownershipTree used to nest workers by matching Person.Manager
// display-name text, a second and disagreeing source of hierarchy truth from
// the authorized org.ResolveManagerRelationships projection PROMOUX-005 added
// for exactly this purpose.
//
// See planning/todos.md "## 69. Live product UX audit remediation",
// UXAUDIT-004.

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	xhtml "golang.org/x/net/html"
)

// uxaudit004ID returns a canonical, values.EntityRef-valid worker id. Real
// production worker ids are UUIDs (workforce.WorkerRow.WorkerID); this
// mirrors internal/domains/org's own test convention
// ("00000000-0000-4000-8000-000000000" + suffix) so every fixture in this
// file exercises the real org.ResolveManagerRelationships validation path
// rather than the "identity could not be validated" fallback.
func uxaudit004ID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

func uxaudit004Person(n int, name, managerID string) Person {
	return Person{ID: fmt.Sprintf("person-%d", n), WorkerID: uxaudit004ID(n), Name: name, ManagerID: managerID}
}

// --- PRIMARY -----------------------------------------------------------

// TestTodo_UXAUDIT_004 is the PRIMARY contract test named in the todo's TEST
// field. It proves the GREEN clause end to end on one small, realistic
// shape: connectors/indentation agree with the authorized manager edges, an
// orphaned (manager not visible) worker and a withheld-relationship worker
// both render as an honestly explained root rather than a fabricated or
// dropped placement, and organizationPlacementFor's mapping is exhaustive
// over every org.ResolutionStatus and people.Disclosure value it can be
// handed (some of which -- AMBIGUOUS, STALE, DISAGREEING -- cannot arise
// from this page's own one-manager-per-worker population today, per
// internal/data/orgfacts.Reader's own doc comment, but must still resolve to
// an explained root rather than a permissive default).
func TestTodo_UXAUDIT_004(t *testing.T) {
	ceo := uxaudit004Person(1, "Casey Okafor", "")
	vp := uxaudit004Person(2, "Val Petrova", ceo.WorkerID)
	director := uxaudit004Person(3, "Devon Reyes", vp.WorkerID)
	orphan := uxaudit004Person(4, "Ori Falk", uxaudit004ID(999)) // a real-shaped id, not among view.People
	withheld := uxaudit004Person(5, "Wren Hale", ceo.WorkerID)
	withheld.ManagerRelationshipWithheld = true

	view := testView(PageOrganization)
	view.People = []Person{ceo, vp, director, orphan, withheld}
	view.SelectedPerson = director.ID

	forest := ownershipTree(view)
	byID := map[string]OwnershipNodeProps{}
	var index func([]OwnershipNodeProps)
	index = func(nodes []OwnershipNodeProps) {
		for _, n := range nodes {
			byID[n.ID] = n
			index(n.Reports)
		}
	}
	index(forest)

	if len(byID) != 5 {
		t.Fatalf("expected every visible worker to render exactly once, got %d: %+v", len(byID), byID)
	}

	// Nesting agrees with the authorized manager edge (GREEN clause 1).
	ceoNode, vpNode, directorNode := byID[ceo.ID], byID[vp.ID], byID[director.ID]
	if ceoNode.Level != 1 || ceoNode.Explanation != "" {
		t.Fatalf("genuine top of the organization must be an unexplained root: %+v", ceoNode)
	}
	if vpNode.Level != 2 || vpNode.Explanation != "" || vpNode.ManagerSummary == "" {
		t.Fatalf("VP must nest one level under the CEO with a manager summary: %+v", vpNode)
	}
	if directorNode.Level != 3 || directorNode.Explanation != "" {
		t.Fatalf("Director must nest two levels under the CEO: %+v", directorNode)
	}
	foundVPUnderCEO, foundDirectorUnderVP := false, false
	for _, r := range ceoNode.Reports {
		if r.ID == vp.ID {
			foundVPUnderCEO = true
		}
	}
	for _, r := range vpNode.Reports {
		if r.ID == director.ID {
			foundDirectorUnderVP = true
		}
	}
	if !foundVPUnderCEO || !foundDirectorUnderVP {
		t.Fatalf("visual nesting disagrees with the authorized manager edges: ceo=%+v vp=%+v", ceoNode, vpNode)
	}

	// Never invent or flatten hierarchy (GREEN clause 2): a manager that is
	// not visible to this viewer, and a withheld relationship, both render as
	// an honestly explained root -- never silently reparented, never dropped.
	orphanNode, withheldNode := byID[orphan.ID], byID[withheld.ID]
	if orphanNode.Level != 1 || orphanNode.Explanation == "" || len(orphanNode.Reports) != 0 {
		t.Fatalf("a worker whose manager is not visible must be an explained root, not reparented or dropped: %+v", orphanNode)
	}
	if withheldNode.Level != 1 || withheldNode.Explanation == "" {
		t.Fatalf("a withheld manager relationship must be an explained root even though the manager IS visible: %+v", withheldNode)
	}
	if withheldNode.Explanation == orphanNode.Explanation {
		t.Fatalf("withheld and not-visible are different findings and must not share one explanation: %q", withheldNode.Explanation)
	}

	// Flat mode must expose the same manager relationship the tree conveys
	// through nesting (GREEN clause 3, flat/tree parity).
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, vpNode.ManagerSummary) {
		t.Fatalf("flat mode lost the manager summary the tree shows: %q not found in flat render", vpNode.ManagerSummary)
	}
	if !strings.Contains(doc, orphanNode.Explanation) || !strings.Contains(doc, withheldNode.Explanation) {
		t.Fatal("flat mode lost an explanation the tree shows")
	}

	// organizationPlacementFor is exhaustive: every org.ResolutionStatus and
	// people.Disclosure branch resolves to SOME placement, and one that is
	// not a genuine top always carries a non-empty explanation -- never a
	// permissive, unexplained root.
	visible := map[string]bool{ceo.WorkerID: true}
	statuses := []org.ResolutionStatus{org.StatusVacant, org.StatusAmbiguous, org.StatusStale, org.StatusDisagreeing, org.StatusResolved, org.ResolutionStatus("UNKNOWN_FUTURE_STATUS")}
	for _, status := range statuses {
		placement := organizationPlacementFor(organizationRelationshipOutcome{resolved: true, resolution: org.ManagerResolution{Status: status}}, visible)
		if status == org.StatusVacant {
			if placement.nested || placement.explanation != "" {
				t.Fatalf("VACANT with no direct hop must be a plain, genuine root: %+v", placement)
			}
			continue
		}
		if placement.nested || placement.explanation == "" {
			t.Fatalf("status %q with no direct hop must be an explained root, never permissive: %+v", status, placement)
		}
	}
	if placement := organizationPlacementFor(organizationRelationshipOutcome{}, visible); placement.nested || placement.explanation == "" {
		t.Fatalf("an outcome this page could not even resolve must be an explained root: %+v", placement)
	}
	disclosures := []people.Disclosure{people.DisclosureFull, people.DisclosurePartial, people.DisclosureWithheld, people.DisclosureUnspecified}
	for _, disclosure := range disclosures {
		hop := org.ManagerHop{Disclosure: disclosure, Manager: org.ManagerReference{Access: people.AccessAuthorized, Value: mustEntityRef(t, ceo.WorkerID)}}
		placement := organizationPlacementFor(organizationRelationshipOutcome{resolved: true, resolution: org.ManagerResolution{Direct: &hop}}, visible)
		switch disclosure {
		case people.DisclosureFull, people.DisclosurePartial:
			if !placement.nested || placement.managerID != ceo.WorkerID {
				t.Fatalf("an authorized, visible manager must nest: disclosure=%v placement=%+v", disclosure, placement)
			}
		default:
			if placement.nested || placement.explanation == "" {
				t.Fatalf("disclosure %v must never nest: %+v", disclosure, placement)
			}
		}
	}
}

func mustEntityRef(t *testing.T, id string) values.EntityRef {
	t.Helper()
	ref, ok := organizationEntityRef(id)
	if !ok {
		t.Fatalf("test id %q did not validate as an EntityRef", id)
	}
	return ref
}

// --- ACCESSIBILITY -------------------------------------------------------

// TestTodo_UXAUDIT_004_Accessibility asserts the real tree contract the live
// audit found missing: a role="tree" container, role="treeitem" nodes,
// aria-level matching actual rendered depth, aria-expanded only on nodes
// that have children, and role="group" wrapping each child set. aria-level
// is checked against the DOM's OWN nesting depth (independently counted by
// walking parsed ancestors), not against a hardcoded number, so a renderer
// that lied about level would be caught.
func TestTodo_UXAUDIT_004_Accessibility(t *testing.T) {
	ceo := uxaudit004Person(1, "Casey Okafor", "")
	vp := uxaudit004Person(2, "Val Petrova", ceo.WorkerID)
	director := uxaudit004Person(3, "Devon Reyes", vp.WorkerID)
	orphan := uxaudit004Person(4, "Ori Falk", uxaudit004ID(999))

	view := testView(PageOrganization)
	view.People = []Person{ceo, vp, director, orphan}
	view = ApplyRequest(view, PageRequest{OrganizationView: "tree"})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}

	trees := uxaudit004FindAll(root, "role", "tree")
	if len(trees) != 1 {
		t.Fatalf("expected exactly one role=tree container, found %d", len(trees))
	}
	tree := trees[0]
	treeitems := uxaudit004FindAll(tree, "role", "treeitem")
	if len(treeitems) != len(view.People) {
		t.Fatalf("expected %d treeitem nodes (one per visible worker), found %d", len(view.People), len(treeitems))
	}
	// Scoped to the tree itself: the page also has an unrelated role=group
	// (the flat/tree view toggle), which must not be held to the tree's own
	// "group wraps a treeitem's children" contract.
	groups := uxaudit004FindAll(tree, "role", "group")

	for _, item := range treeitems {
		levelText, ok := uxaudit004Attr(item, "aria-level")
		if !ok {
			t.Fatalf("treeitem missing aria-level: %s", uxaudit004NodeText(item))
		}
		level, convErr := strconv.Atoi(levelText)
		if convErr != nil {
			t.Fatalf("aria-level %q is not a number", levelText)
		}
		wantLevel := uxaudit004AncestorTreeitemDepth(item) + 1
		if level != wantLevel {
			t.Fatalf("aria-level=%d disagrees with the actual rendered depth %d: %s", level, wantLevel, uxaudit004NodeText(item))
		}
		hasChildGroup := false
		for c := item.FirstChild; c != nil; c = c.NextSibling {
			if v, ok := uxaudit004Attr(c, "role"); ok && v == "group" {
				hasChildGroup = true
			}
		}
		_, expandedSet := uxaudit004Attr(item, "aria-expanded")
		if hasChildGroup != expandedSet {
			t.Fatalf("aria-expanded must be present iff the node has children: hasChildren=%v expandedAttrPresent=%v node=%s", hasChildGroup, expandedSet, uxaudit004NodeText(item))
		}
	}
	for _, group := range groups {
		if group.Parent == nil {
			continue
		}
		if v, ok := uxaudit004Attr(group.Parent, "role"); !ok || v != "treeitem" {
			t.Fatalf("role=group must be a treeitem's child, wrapping its own child set")
		}
		for c := group.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode {
				continue
			}
			if v, ok := uxaudit004Attr(c, "role"); !ok || v != "treeitem" {
				t.Fatalf("role=group must contain only treeitem children")
			}
		}
	}

	// CEO/VP/Director must be levels 1/2/3; the orphan, despite having a
	// real (just not visible) manager on record, must render at level 1 --
	// proving an explained root is never given a fabricated depth.
	levelByName := map[string]string{}
	for _, item := range treeitems {
		levelText, _ := uxaudit004Attr(item, "aria-level")
		levelByName[uxaudit004NodeText(item)] = levelText
	}
	for _, want := range []struct {
		name  string
		level string
	}{{"Casey Okafor", "1"}, {"Val Petrova", "2"}, {"Devon Reyes", "3"}, {"Ori Falk", "1"}} {
		matched := false
		for text, level := range levelByName {
			if strings.Contains(text, want.name) && level == want.level {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("expected %s at aria-level=%s, got levels %v", want.name, want.level, levelByName)
		}
	}
}

// --- PROPERTY --------------------------------------------------------------

// TestTodo_UXAUDIT_004_Property proves, over 200 independently generated
// random populations rather than one hand-built tree, that every node the
// tree renders nested beneath a parent is genuinely reported by the
// authorized projection as reporting to that parent, that every visible
// worker renders exactly once, and that a worker whose ground-truth manager
// could not be honestly nested (not present in the population) always
// carries a non-empty explanation rather than becoming a silent, unexplained
// root.
func TestTodo_UXAUDIT_004_Property(t *testing.T) {
	random := rand.New(rand.NewSource(20260913))
	for trial := 0; trial < 200; trial++ {
		size := 3 + random.Intn(10) // 3..12 workers
		people := make([]Person, size)
		groundTruthManager := make([]string, size) // "" or a WorkerID this worker genuinely reports to.
		for i := 0; i < size; i++ {
			people[i] = uxaudit004Person(trial*100+i, fmt.Sprintf("Worker %d-%d", trial, i), "")
		}
		for i := 0; i < size; i++ {
			switch pick := random.Intn(4); {
			case pick == 0 || i == 0:
				// No manager -- a genuine, unexplained root candidate.
			case pick == 1:
				// Reports to an earlier-indexed worker: always acyclic, so the
				// projection must nest it.
				manager := random.Intn(i)
				groundTruthManager[i] = people[manager].WorkerID
				people[i].ManagerID = people[manager].WorkerID
			default:
				// Reports to a worker id no one in this population has -- the
				// "manager not visible to this viewer" case.
				ghost := uxaudit004ID(1_000_000 + trial*1000 + i)
				groundTruthManager[i] = ghost
				people[i].ManagerID = ghost
			}
		}
		view := testView(PageOrganization)
		view.People = people
		forest := ownershipTree(view)

		seen := map[string]OwnershipNodeProps{}
		parentSeen := map[string]string{} // node id -> its rendered parent's WorkerID, "" for a root.
		var walk func(nodes []OwnershipNodeProps, parentWorkerID string)
		walk = func(nodes []OwnershipNodeProps, parentWorkerID string) {
			for _, n := range nodes {
				seen[n.ID] = n
				parentSeen[n.ID] = parentWorkerID
				var childWorkerID string
				for i := range people {
					if people[i].ID == n.ID {
						childWorkerID = people[i].WorkerID
					}
				}
				walk(n.Reports, childWorkerID)
			}
		}
		walk(forest, "")

		if len(seen) != size {
			t.Fatalf("trial %d: expected every one of %d workers to render exactly once, got %d", trial, size, len(seen))
		}
		for i, person := range people {
			node, ok := seen[person.ID]
			if !ok {
				t.Fatalf("trial %d: worker %d was dropped entirely", trial, i)
			}
			renderedParent := parentSeen[person.ID]
			switch {
			case groundTruthManager[i] == "":
				if renderedParent != "" || node.Explanation != "" {
					t.Fatalf("trial %d: worker %d has no manager and must be a plain root: parent=%q explanation=%q", trial, i, renderedParent, node.Explanation)
				}
			case renderedParent == groundTruthManager[i]:
				// Nested exactly under the authorized manager -- clause 1.
				if node.Explanation != "" {
					t.Fatalf("trial %d: worker %d is correctly nested but still carries an explanation: %q", trial, i, node.Explanation)
				}
			case renderedParent == "":
				// The ground-truth manager is not a worker this population can
				// name (the ghost-id branch) -- clause 2 requires an honest
				// explanation, never a silent, unexplained root.
				if node.Explanation == "" {
					t.Fatalf("trial %d: worker %d's manager %q is not in this population but no explanation was given", trial, i, groundTruthManager[i])
				}
			default:
				t.Fatalf("trial %d: worker %d nested under %q, which is neither its ground-truth manager %q nor unnested", trial, i, renderedParent, groundTruthManager[i])
			}
		}
	}
}

// --- REGRESSION --------------------------------------------------------

// TestTodo_UXAUDIT_004_Regression proves the exact defect RED names cannot
// resurface: two people sharing one display name must never desynchronize
// reporting-line placement, because placement is now resolved by the
// authorized WorkerID-keyed manager relationship, never by matching the
// Manager display-name string. The pre-fix ownershipTree refused to nest
// ANYONE under an ambiguous name; the fix does not need to refuse anything,
// because there is no name lookup left to be ambiguous.
func TestTodo_UXAUDIT_004_Regression(t *testing.T) {
	alexOne := uxaudit004Person(1, "Alex", "")
	alexTwo := uxaudit004Person(2, "Alex", "")
	chris := uxaudit004Person(3, "Chris", alexTwo.WorkerID) // reports to the SECOND Alex specifically.

	view := testView(PageOrganization)
	view.People = []Person{alexOne, alexTwo, chris}
	forest := ownershipTree(view)

	var findByID func(nodes []OwnershipNodeProps, id string) (OwnershipNodeProps, bool)
	findByID = func(nodes []OwnershipNodeProps, id string) (OwnershipNodeProps, bool) {
		for _, n := range nodes {
			if n.ID == id {
				return n, true
			}
			if found, ok := findByID(n.Reports, id); ok {
				return found, true
			}
		}
		return OwnershipNodeProps{}, false
	}

	alexTwoNode, ok := findByID(forest, alexTwo.ID)
	if !ok {
		t.Fatal("the second Alex was dropped")
	}
	chrisNested := false
	for _, r := range alexTwoNode.Reports {
		if r.ID == chris.ID {
			chrisNested = true
		}
	}
	if !chrisNested {
		t.Fatalf("Chris must nest under the specific Alex (WorkerID %s) the authorized relationship names, not be blocked by the name collision: %+v", alexTwo.WorkerID, alexTwoNode)
	}
	alexOneNode, ok := findByID(forest, alexOne.ID)
	if !ok {
		t.Fatal("the first Alex was dropped")
	}
	if len(alexOneNode.Reports) != 0 {
		t.Fatalf("the first Alex, who Chris does not report to, must not receive Chris by name collision: %+v", alexOneNode)
	}
}

// --- BROWSER -------------------------------------------------------------

// TestTodo_UXAUDIT_004_Browser renders the full organization page document
// -- the same server-rendered surface a browser receives for
// /workspace/app/organization?org_view=tree -- and proves the reporting-lines
// view is a real tree, not the flat, unlabeled list of disconnected cards
// RED describes. As with this package's other UXAUDIT Browser tests, this is
// a static SSR/DOM assertion parsing the rendered document; it is not a
// live-browser check.
func TestTodo_UXAUDIT_004_Browser(t *testing.T) {
	ceo := uxaudit004Person(1, "Casey Okafor", "")
	vp := uxaudit004Person(2, "Val Petrova", ceo.WorkerID)
	view := testView(PageOrganization)
	view.People = []Person{ceo, vp}
	view = ApplyRequest(view, PageRequest{OrganizationView: "tree"})

	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{

		`data-organization-view="tree"`, `role="tree"`, `role="treeitem"`, `role="group"`,
		`aria-level="1"`, `aria-level="2"`, "Casey Okafor", "Val Petrova",
		"org_view=flat", // the flat/tree toggle remains software-routable.
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("organization tree document missing %q", want)
		}
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(uxaudit004FindAll(root, "role", "tree")) != 1 {
		t.Fatal("expected exactly one role=tree landmark on the page")
	}
}

// --- test helpers --------------------------------------------------------

func uxaudit004Attr(node *xhtml.Node, key string) (string, bool) {
	for _, a := range node.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func uxaudit004FindAll(root *xhtml.Node, key, val string) []*xhtml.Node {
	var found []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			if v, ok := uxaudit004Attr(n, key); ok && v == val {
				found = append(found, n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

// uxaudit004AncestorTreeitemDepth counts how many ancestor role=treeitem
// elements enclose node -- the real DOM nesting depth, independent of
// whatever the node's own aria-level claims.
func uxaudit004AncestorTreeitemDepth(node *xhtml.Node) int {
	depth := 0
	for p := node.Parent; p != nil; p = p.Parent {
		if v, ok := uxaudit004Attr(p, "role"); ok && v == "treeitem" {
			depth++
		}
	}
	return depth
}

func uxaudit004NodeText(node *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node)
	return b.String()
}
