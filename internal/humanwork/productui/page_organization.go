package productui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func organizationPage(view View) ui.Node {
	relationships := newOrganizationRelationshipIndex(view)
	members := map[string][]OwnershipNodeProps{}
	locations := map[string]bool{}
	payZones := map[string]bool{}
	for index, person := range relationships.people {
		team := valueOrUnavailable(person.Team)
		members[team] = append(members[team], relationships.annotate(view, index))
		if value := strings.TrimSpace(person.Location); value != "" {
			locations[value] = true
		}
		if value := strings.TrimSpace(person.PayZone); value != "" {
			payZones[value] = true
		}
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	groups := make([]OrganizationGroupProps, 0, len(names))
	for _, name := range names {
		sort.SliceStable(members[name], func(i, j int) bool {
			return strings.ToLower(members[name][i].Name) < strings.ToLower(members[name][j].Name)
		})
		groups = append(groups, OrganizationGroupProps{Name: name, Count: len(members[name]), CountLabel: organizationCountLabel(view, "organization.members_count", len(members[name])), Members: members[name]})
	}
	visibleLocations := sortedOrganizationValues(locations)
	number := func(value int) string { return view.Locale.FormatNumber(strconv.Itoa(value), 0) }
	return ui.CreateElement(OrganizationPage, OrganizationPageProps{
		Title: view.Locale.Text("organization.structure_title"), Description: view.Locale.Text("organization.structure_description"), Groups: groups,
		ViewLabel: view.Locale.Text("organization.view_label"), TreeActive: view.OrganizationView == organizationViewTree,
		FlatAction: ActionLinkProps{Label: view.Locale.Text("organization.view_flat"), Href: statefulHref(view, PageOrganization, "org_view", organizationViewFlat), Class: "organization-view-option", Navigate: view.Navigate},
		TreeAction: ActionLinkProps{Label: view.Locale.Text("organization.view_tree"), Href: statefulHref(view, PageOrganization, "org_view", organizationViewTree), Class: "organization-view-option", Navigate: view.Navigate},
		Tree:       relationships.tree(view),
		TreeLabel:  view.Locale.Text("organization.tree_label"),
		Metadata: BusinessMetadataProps{
			Title: view.Locale.Text("organization.metadata_title"), Description: view.Locale.Text("organization.metadata_description"),
			Items: []BusinessMetadataItemProps{
				{Label: view.Locale.Text("organization.business_name"), Value: valueOrUnavailableFor(view.Locale, view.Tenant)},
				{Label: view.Locale.Text("organization.visible_workforce"), Value: number(len(view.People))},
				{Label: view.Locale.Text("organization.units"), Value: number(len(groups))},
				{Label: view.Locale.Text("organization.locations"), Value: number(len(locations))},
				{Label: view.Locale.Text("organization.pay_zones"), Value: number(len(payZones))},
				{Label: view.Locale.Text("organization.access_scope"), Value: valueOrUnavailableFor(view.Locale, view.Scope)},
			},
			FootprintLabel: view.Locale.Text("organization.footprint"), Footprint: visibleLocations,
			Boundary: view.Locale.Text("organization.metadata_boundary"),
		},
		Empty: EmptyStateProps{Title: view.Locale.Text("organization.empty_title"), Description: view.Locale.Text("organization.empty_description")},
	})
}

const (
	organizationViewFlat = "flat"
	organizationViewTree = "tree"
)

func normalizeOrganizationView(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), organizationViewTree) {
		return organizationViewTree
	}
	return organizationViewFlat
}

// organizationRelationshipIndex is the ONE authorized relationship
// projection, resolved once per render, that the flat organization list, the
// organization tree, and (via ownershipTree) the Myself subtree all read --
// UXAUDIT-004's REFACTOR clause. It replaces the previous
// Person.Manager-display-name matching entirely: nesting and per-person
// manager summaries both come from org.ResolveManagerRelationships through
// organization_relationships.go, never from a name.
type organizationRelationshipIndex struct {
	people      []Person
	placements  []organizationPlacement
	parentOf    []int // -1 for a root; index into people otherwise.
	children    map[int][]int
	indexByName map[string]int // person.ID -> index into people, for annotate lookups.
}

// newOrganizationRelationshipIndex resolves every visible person's
// authorized direct-manager hop and turns it into a placement: nested under
// a specific, visible parent, or a root, genuine or explained. See
// organizationPlacementFor for the exhaustive mapping and
// breakOwnershipCycles for why a mutual reporting cycle can never nest.
func newOrganizationRelationshipIndex(view View) organizationRelationshipIndex {
	people := append([]Person(nil), view.People...)
	sort.SliceStable(people, func(i, j int) bool { return strings.ToLower(people[i].Name) < strings.ToLower(people[j].Name) })

	byWorkerID := make(map[string]int, len(people))
	visibleWorkerID := make(map[string]bool, len(people))
	indexByName := make(map[string]int, len(people))
	for index, person := range people {
		if person.ID != "" {
			indexByName[person.ID] = index
		}
		if person.WorkerID == "" {
			continue
		}
		byWorkerID[person.WorkerID] = index
		visibleWorkerID[person.WorkerID] = true
	}

	placements := make([]organizationPlacement, len(people))
	parentOf := make([]int, len(people))
	for index := range parentOf {
		parentOf[index] = -1
	}
	for index, person := range people {
		placement := organizationPlacementFor(buildOrganizationRelationship(person), visibleWorkerID)
		if placement.nested {
			if managerIndex, ok := byWorkerID[placement.managerID]; ok && managerIndex != index {
				parentOf[index] = managerIndex
			} else {
				// The disclosed manager id does not resolve to a visible, distinct
				// person after all (e.g. it names the worker itself) -- fall back
				// to an explained root rather than ever nesting a worker under
				// itself or an id nothing here can point to.
				placement = organizationPlacement{explanation: "organization.relationship_undetermined"}
			}
		}
		placements[index] = placement
	}
	breakOwnershipCycles(parentOf, placements)

	children := make(map[int][]int, len(people))
	for index, parent := range parentOf {
		if parent != -1 {
			children[parent] = append(children[parent], index)
		}
	}
	return organizationRelationshipIndex{people: people, placements: placements, parentOf: parentOf, children: children, indexByName: indexByName}
}

// annotate renders one person's shared organization-node fields -- identity,
// selection, and the manager summary or explanation clause 3 (flat/tree
// parity) requires -- without any nesting. The flat organization list uses
// this directly; tree renders it too and additionally nests and levels it.
func (idx organizationRelationshipIndex) annotate(view View, index int) OwnershipNodeProps {
	person := idx.people[index]
	node := ownershipPerson(view, person)
	placement := idx.placements[index]
	if placement.explanation != "" {
		node.Explanation = view.Locale.Text(placement.explanation)
	} else if placement.nested {
		node.ManagerSummary = fmt.Sprintf(view.Locale.Text("organization.reports_to"), idx.people[idx.parentOf[index]].Name)
	}
	return node
}

// buildFrom renders index's own node at the given display level, recursing
// into its visible reports. It powers both tree (every root at Level 1) and
// findAndReroot (Myself's chosen person at Level 1, re-rooting their own
// subtree rather than the whole organization).
func (idx organizationRelationshipIndex) buildFrom(view View, index, level int) OwnershipNodeProps {
	node := idx.annotate(view, index)
	node.Level = level
	for _, child := range idx.children[index] {
		node.Reports = append(node.Reports, idx.buildFrom(view, child, level+1))
	}
	node.ReportsLabel = organizationCountLabel(view, "organization.reports_count", len(node.Reports))
	return node
}

// tree builds the reporting-line forest: every person renders exactly once,
// either nested under the visible parent org.ResolveManagerRelationships
// actually reports, or as a root -- a genuine top of the organization needs
// no explanation, and everything else renders as a root WITH an honest
// explanation. Nothing is ever silently reparented or dropped.
func (idx organizationRelationshipIndex) tree(view View) []OwnershipNodeProps {
	roots := make([]int, 0, len(idx.people))
	for index, parent := range idx.parentOf {
		if parent == -1 {
			roots = append(roots, index)
		}
	}
	result := make([]OwnershipNodeProps, 0, len(roots))
	for _, root := range roots {
		result = append(result, idx.buildFrom(view, root, 1))
	}
	return result
}

// findAndReroot returns personID's own node from the forest, with Level
// renumbered so personID becomes the display root (Level 1) of its own
// subtree -- what Myself needs (see myselfOwnershipSubtree in page_myself.go).
func (idx organizationRelationshipIndex) findAndReroot(view View, personID string) (OwnershipNodeProps, bool) {
	index, ok := idx.indexByName[personID]
	if !ok {
		return OwnershipNodeProps{}, false
	}
	return idx.buildFrom(view, index, 1), true
}

// ownershipTree is the organization page's own forest -- kept as a thin
// wrapper so existing call sites and tests naming it need not change.
func ownershipTree(view View) []OwnershipNodeProps {
	return newOrganizationRelationshipIndex(view).tree(view)
}

// breakOwnershipCycles forces every node on a nesting cycle to an explained
// root. organizationHopFacts resolves each worker's own hop independently, so
// two people whose manager relationships point at each other can each
// resolve successfully in isolation; without this pass they would nest
// inside one another, which is exactly the invented hierarchy UXAUDIT-004
// forbids. A node found on a cycle is never left nested, in either direction.
func breakOwnershipCycles(parentOf []int, placements []organizationPlacement) {
	const (
		white = iota
		gray
		black
	)
	color := make([]int, len(parentOf))
	var mark func(int)
	mark = func(node int) {
		if color[node] != white {
			return
		}
		color[node] = gray
		if parent := parentOf[node]; parent != -1 {
			switch color[parent] {
			case white:
				mark(parent)
			case gray:
				for cursor := node; ; {
					next := parentOf[cursor]
					placements[cursor] = organizationPlacement{explanation: "organization.relationship_cycle"}
					parentOf[cursor] = -1
					if cursor == parent {
						break
					}
					cursor = next
				}
			}
		}
		color[node] = black
	}
	for index := range parentOf {
		mark(index)
	}
}

func ownershipPerson(view View, person Person) OwnershipNodeProps {
	current := person.ID != "" && person.ID == view.Viewer.PersonID
	selected := person.ID != "" && person.ID == view.SelectedPerson
	href := ""
	if PageVisible(PagePerson, view.Roles) {
		href = statefulHref(view, PagePerson, "person", person.ID)
	} else if current {
		href = statefulHref(view, PageMyself)
	}
	return OwnershipNodeProps{
		ID: person.ID, Name: person.Name, WorkerNumber: person.WorkerNumber, Role: person.Role, Team: person.Team,
		Initials: person.Initials, PhotoURL: person.PhotoURL, Href: href, Navigate: view.Navigate, Current: current, Selected: selected,
	}
}

func organizationCountLabel(view View, key string, count int) string {
	return fmt.Sprintf(view.Locale.Text(key), view.Locale.FormatNumber(strconv.Itoa(count), 0))
}

func sortedOrganizationValues(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
