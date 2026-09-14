package productui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func organizationPage(view View) ui.Node {
	return organizationPageWithCopy(view, view.Locale.Text("organization.structure_title"), view.Locale.Text("organization.structure_description"), false)
}

func organizationRoutePage(view View, forceTree bool) ui.Node {
	return organizationPageWithCopy(view, view.Title, view.Subtitle, forceTree)
}

func organizationPageWithCopy(view View, title, description string, forceTree bool) ui.Node {
	population := admittedPeople(view)
	filtered := filterOrganizationPeople(population, view.Query)
	scoped := view
	scoped.People = population
	relationships := newOrganizationRelationshipIndex(scoped)
	visible := make(map[string]bool, len(filtered))
	for _, person := range filtered {
		visible[person.ID] = true
	}
	members := map[string][]OwnershipNodeProps{}
	locations := map[string]bool{}
	payZones := map[string]bool{}
	for index, person := range relationships.people {
		if !visible[person.ID] {
			continue
		}
		team := valueOrUnavailable(person.Team)
		members[team] = append(members[team], relationships.annotate(scoped, index))
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

		I18nProps: I18nProps{Locale: view.Locale}, Title: title, Description: description, Groups: groups,
		Summary: OrganizationSummaryProps{VisiblePeople: len(population), Units: len(groups), Scope: view.Scope,
			CompactLabel: densityLabel(view.Locale, "compact"), ComfortableLabel: densityLabel(view.Locale, "comfortable"), SpaciousLabel: densityLabel(view.Locale, "spacious"),
			ExpandAllLabel: view.Locale.Text("nav.expand"), CollapseAllLabel: view.Locale.Text("nav.collapse")}, Density: view.Appearance.Density,
		ViewLabel: view.Locale.Text("organization.view_label"), TreeActive: forceTree || view.OrganizationView == organizationViewTree, TreeLocked: forceTree,
		FlatAction: ActionLinkProps{Label: view.Locale.Text("organization.view_flat"), Href: statefulHref(view, view.Page, "org_view", organizationViewFlat, "q", view.Query, "person", view.SelectedPerson), Class: "organization-view-option", Navigate: view.Navigate},
		TreeAction: ActionLinkProps{Label: view.Locale.Text("organization.view_tree"), Href: statefulHref(view, view.Page, "org_view", organizationViewTree, "q", view.Query, "person", view.SelectedPerson), Class: "organization-view-option", Navigate: view.Navigate},
		Search: OrganizationSearchProps{
			Query: view.Query, Action: statefulHref(view, view.Page, "org_view", normalizeOrganizationView(view.OrganizationView), "person", view.SelectedPerson),
			ClearHref: withExplicitEmptyQuery(statefulHref(view, view.Page, "org_view", normalizeOrganizationView(view.OrganizationView), "person", view.SelectedPerson), "q"),
			Summary:   organizationSearchSummary(view, len(filtered), len(population)), HiddenInputs: organizationSearchHiddenInputs(view), Navigate: view.Navigate,
			OnFilter: func(query string) {
				if view.Navigate != nil {
					view.Navigate(organizationFilterHref(view, query))
				}
			},
		},
		Tree:      filterOwnershipTree(relationships.tree(scoped), visible),
		TreeLabel: view.Locale.Text("organization.tree_label"),
		Metadata: BusinessMetadataProps{
			Title: view.Locale.Text("organization.metadata_title"), Description: view.Locale.Text("organization.metadata_description"),
			Items: []BusinessMetadataItemProps{
				{Label: view.Locale.Text("organization.business_name"), Value: valueOrUnavailableFor(view.Locale, view.Tenant)},
				{Label: view.Locale.Text("organization.visible_workforce"), Value: number(len(population))},
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

func organizationFilterHref(view View, query string) string {
	href := statefulHref(view, view.Page, "org_view", normalizeOrganizationView(view.OrganizationView), "person", view.SelectedPerson, "q", strings.TrimSpace(query))
	if strings.TrimSpace(query) == "" {
		return withExplicitEmptyQuery(href, "q")
	}
	return href
}

func organizationSearchHiddenInputs(view View) map[string]string {
	values := currentPageAddressState(view, view.NavCollapsed)
	values.Del("q")
	result := make(map[string]string, len(values))
	for name, entries := range values {
		if len(entries) != 0 && entries[0] != "" {
			result[name] = entries[0]
		}
	}
	return result
}

func organizationSearchSummary(view View, filtered, total int) string {
	if strings.TrimSpace(view.Query) == "" {
		return organizationCountLabel(view, "organization.members_count", total)
	}
	return view.Locale.Text("people.filtered_count", map[string]string{
		"filtered": view.Locale.FormatNumber(strconv.Itoa(filtered), 0),
		"total":    view.Locale.FormatNumber(strconv.Itoa(total), 0),
	})
}

func densityLabel(locale LocaleContext, option string) string {
	for _, choice := range DensityOptions() {
		if choice.ID == option {
			return choice.Label
		}
	}
	return locale.Text("appearance.density")
}

// filterOrganizationPeople keeps search scoped to the already-admitted
// worker projection. Each query token may match a field directly or with a
// small edit distance, which makes common misspellings useful without broad
// enumerating a population outside the server's disclosure boundary.
func filterOrganizationPeople(people []Person, query string) []Person {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return append([]Person(nil), people...)
	}
	tokens := strings.Fields(query)
	result := make([]Person, 0, len(people))
	for _, person := range people {
		searchable := organizationSearchText(person)
		matched := true
		for _, token := range tokens {
			if !organizationFuzzyTokenMatch(searchable, token) {
				matched = false
				break
			}
		}
		if matched {
			result = append(result, person)
		}
	}
	return result
}

func organizationSearchText(person Person) string {
	return strings.ToLower(strings.Join([]string{
		normalizedPerson(person).search, person.Initials, person.LegalName,
		person.PreferredName, person.WorkerID,
	}, " "))
}

func organizationFuzzyTokenMatch(searchable, query string) bool {
	if strings.Contains(searchable, query) {
		return true
	}
	if len([]rune(query)) < 4 {
		return false
	}
	for _, candidate := range strings.Fields(searchable) {
		if organizationEditDistance(candidate, query) <= 1 {
			return true
		}
	}
	return false
}

func organizationEditDistance(left, right string) int {
	leftRunes, rightRunes := []rune(left), []rune(right)
	if len(leftRunes) < len(rightRunes) {
		leftRunes, rightRunes = rightRunes, leftRunes
	}
	if len(leftRunes)-len(rightRunes) > 1 {
		return 2
	}
	previous := make([]int, len(rightRunes)+1)
	for index := range previous {
		previous[index] = index
	}
	for i, leftRune := range leftRunes {
		current := make([]int, len(rightRunes)+1)
		current[0] = i + 1
		for j, rightRune := range rightRunes {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[j+1] = minOrganizationInt(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(rightRunes)]
}

func minOrganizationInt(values ...int) int {
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
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
		manager := idx.people[idx.parentOf[index]]
		node.ManagerSummary = fmt.Sprintf(view.Locale.Text("organization.reports_to"), organizationFieldLabel(view, manager.ID, "name", manager.Name))
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
	scoped := view
	scoped.People = admittedPeople(view)
	return newOrganizationRelationshipIndex(scoped).tree(scoped)
}

// A search result retains its admitted reporting ancestors so a filtered
// tree cannot turn a report into an apparent root or invent a new parent.
func filterOwnershipTree(nodes []OwnershipNodeProps, matching map[string]bool) []OwnershipNodeProps {
	result := make([]OwnershipNodeProps, 0, len(nodes))
	for _, node := range nodes {
		node.Reports = filterOwnershipTree(node.Reports, matching)
		if matching[node.ID] || len(node.Reports) > 0 {
			result = append(result, node)
		}
	}
	return result
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
	if view.Can(PagePerson, "view") {
		href = statefulHref(view, PagePerson, "person", person.ID)
	} else if current && view.Can(PageMyself, "view") {
		href = statefulHref(view, PageMyself)
	}

	return OwnershipNodeProps{
		I18nProps: I18nProps{Locale: view.Locale}, ID: person.ID, Name: organizationFieldLabel(view, person.ID, "name", person.Name), WorkerNumber: organizationFieldLabel(view, person.ID, "worker_number", person.WorkerNumber), Role: organizationFieldLabel(view, person.ID, "role", person.Role), Team: organizationFieldLabel(view, person.ID, "organization_unit", person.Team), Manager: organizationFieldLabel(view, person.ID, "manager", person.Manager), Location: organizationFieldLabel(view, person.ID, "work_location", person.Location),
		Initials: person.Initials, PhotoURL: person.PhotoURL, Href: href, Navigate: view.Navigate, Current: current, Selected: selected,
	}
}

// organizationFieldLabel preserves the legacy record-level discovery
// contract when no field verdict was supplied, but honors every explicit
// field verdict before a worker fact reaches an organization node. This keeps
// a manager-name denial from leaking through a report's summary while older
// record-only projections remain compatible.
func organizationFieldLabel(view View, recordID, field, raw string) string {
	if len(view.RecordVerdicts) == 0 {
		return raw
	}
	record, ok := view.RecordVerdicts[recordID]
	if !ok || !record.Disclosable {
		return ""
	}
	verdict, explicit := record.Fields[field]
	if !explicit {
		return raw
	}
	return ProjectAuthorizedValue(view.Locale, raw, verdict).Text
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
