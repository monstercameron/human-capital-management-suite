package workspace

import (
	"html"
	"sort"
	"strconv"
	"strings"
)

// This file renders the dev sign-in page's employee directory: a plain GET
// search form and a server-rendered organization tree whose every employee is
// a submit button.
//
// Why not productui.OrganizationOwnershipTree, which is otherwise the right
// renderer for exactly this shape: it cannot work here. Its tree items expand
// and collapse through ui.UseState wired to an OnClick handler
// (productui/organization_components.go, organizationTreeItem), and its large
// trees swap to a scroll-driven virtual window - both are JavaScript. The
// sign-in page is served under `default-src 'none'` with one style hash, no
// script source at all and `form-action 'self'` (csp.go, writeLoginDocument),
// so nothing on it may execute. Its person cards also render navigation links
// to authorized product routes, where this page needs a POST submit control
// per person, which is not a shape that component accepts. The markup below
// therefore keeps the same accessible structure - role="tree" / "group" /
// "treeitem" with aria-level - and gets its expand/collapse from native
// <details>, which needs no script.
//
// One honest caveat, recorded rather than hidden: aria-expanded on a unit
// treeitem states the server-rendered initial disclosure, and native <details>
// toggling afterwards does not update it. The live state a screen reader
// reports comes from the <summary> disclosure itself, which browsers expose
// correctly. Keeping the ARIA tree roles without script also means this tree
// has no roving-tabindex arrow-key navigation: it is operated with Tab and
// Enter/Space over real controls. Both are consequences of a no-script CSP,
// not oversights.

// paramDirectoryQuery names the employee search field. The search is a plain
// GET over an already-public seed plan, so its whole state lives in the URL
// and the page stays addressable and back-button correct without script.
const paramDirectoryQuery = "q"

// paramDirectoryRole names the role filter. It carries a role id ("hr_partner"),
// not the label the option shows, so a shared URL survives a role being
// renamed in the access registry.
const paramDirectoryRole = "role"

// maxDirectoryQueryBytes bounds the reflected search text. The value is HTML
// escaped either way; the bound keeps an absurd URL from becoming an absurd
// document.
const maxDirectoryQueryBytes = 120

// directoryRow is one employee resolved for rendering: the public plan facts
// plus the exact role bundle the server-held credential carries.
type directoryRow struct {
	employee    DevDirectoryEmployee
	unitName    string
	managerName string
	// roles is read from the server-owned persona, never from the directory,
	// so the page can only ever promise the bundle that is actually signed
	// into the credential the button selects.
	roles []string
	// roleLabels is devRoleLabels(roles), resolved once per render: it is
	// both what the card shows and what the free-text search matches on, so
	// searching for the words a tester can see cannot disagree with them.
	roleLabels []string
	// signable is false when no server-held persona answers this employee's
	// id. Such a row renders its facts and no control, rather than a button
	// that would refuse.
	signable bool
}

// directoryRoleOption is one entry of the role filter: the id that travels in
// the URL and the label the tester reads.
type directoryRoleOption struct{ id, label string }

// directoryRender is one render's resolved directory.
type directoryRender struct {
	units      map[string]DevDirectoryUnit
	childCodes map[string][]string
	roots      []string
	byUnit     map[string][]directoryRow
	rows       []directoryRow
	query      string
	needle     string
	// role is the submitted role id, kept even when no employee holds it: a
	// filter that silently stopped applying would report a count for a
	// different question than the one the URL asks.
	role string
	// roleLabel names role for the summary line, falling back to the raw id
	// so an unheld or retired role is still named rather than hidden.
	roleLabel string
	// roleOptions are the roles some signable employee actually holds.
	roleOptions []directoryRoleOption
}

// filtering reports whether either filter is active. It decides three things
// together, so they cannot drift apart: whether the result list is rendered,
// whether the tree hides non-matching people, and whether a unit's disclosure
// follows its matches instead of its depth.
func (d *directoryRender) filtering() bool { return d.query != "" || d.role != "" }

// normalizeDirectoryQuery trims and bounds the submitted search text.
func normalizeDirectoryQuery(raw string) string {
	query := strings.TrimSpace(raw)
	if len(query) > maxDirectoryQueryBytes {
		query = strings.TrimSpace(query[:maxDirectoryQueryBytes])
	}
	return query
}

// newDirectoryRender resolves the snapshot against the server-held personas.
func (h *Handler) newDirectoryRender(query, role string) *directoryRender {
	snapshot := h.directory.DevDirectorySnapshot()
	render := &directoryRender{
		units:      make(map[string]DevDirectoryUnit, len(snapshot.Units)),
		childCodes: make(map[string][]string, len(snapshot.Units)),
		byUnit:     make(map[string][]directoryRow, len(snapshot.Units)),
		query:      query,
		needle:     strings.ToLower(query),
		role:       role,
	}
	for _, unit := range snapshot.Units {
		if strings.TrimSpace(unit.Code) == "" {
			continue
		}
		render.units[unit.Code] = unit
	}
	for _, unit := range snapshot.Units {
		if _, ok := render.units[unit.Code]; !ok {
			continue
		}
		parent := unit.ParentCode
		if parent == "" || parent == unit.Code {
			render.roots = append(render.roots, unit.Code)
			continue
		}
		if _, ok := render.units[parent]; !ok {
			render.roots = append(render.roots, unit.Code)
			continue
		}
		render.childCodes[parent] = append(render.childCodes[parent], unit.Code)
	}
	names := make(map[string]string, len(snapshot.Employees))
	for _, employee := range snapshot.Employees {
		names[employee.WorkerKey] = employee.Name
	}
	for _, employee := range snapshot.Employees {
		row := directoryRow{employee: employee, managerName: names[employee.ManagerKey]}
		if unit, ok := render.units[employee.UnitCode]; ok {
			row.unitName = unit.Name
		}
		if persona, ok := h.devPersonas[employee.PersonaID]; ok && strings.TrimSpace(persona.Token) != "" {
			row.roles = persona.Roles
			row.roleLabels = devRoleLabels(persona.Roles)
			row.signable = true
		}
		render.rows = append(render.rows, row)
		render.byUnit[employee.UnitCode] = append(render.byUnit[employee.UnitCode], row)
	}
	render.roleOptions = directoryRoleOptions(render.rows)
	if role != "" {
		// The registry's own name for the role, falling back to the raw id,
		// so a role nobody currently holds is still named honestly in the
		// summary rather than reported as a bare identifier.
		render.roleLabel = devRoleLabels([]string{role})[0]
	}
	return render
}

// directoryRoleOptions lists the roles at least one signable employee holds,
// ordered by label so the control is stable across renders and independent of
// map iteration. Offering a role nobody holds would be a filter that can only
// ever return nothing; offering one held by an employee with no credential
// would be a filter that can only return rows you cannot sign in as.
func directoryRoleOptions(rows []directoryRow) []directoryRoleOption {
	seen := map[string]string{}
	for _, row := range rows {
		if !row.signable {
			continue
		}
		for index, id := range row.roles {
			label := id
			if index < len(row.roleLabels) {
				label = row.roleLabels[index]
			}
			seen[id] = label
		}
	}
	options := make([]directoryRoleOption, 0, len(seen))
	for id, label := range seen {
		options = append(options, directoryRoleOption{id: id, label: label})
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].label != options[j].label {
			return options[i].label < options[j].label
		}
		return options[i].id < options[j].id
	})
	return options
}

// matches reports whether one row answers the free-text search. Name, worker
// number, job title, org unit and the role labels the card shows are all
// searchable, because a tester looking for "the nurse in Chicago" or "somebody
// who can approve finance" may hold any one of them. Any single field is
// enough: the fields are alternatives to each other, and the role filter below
// is the one that narrows.
func (row directoryRow) matches(needle string) bool {
	if needle == "" {
		return true
	}
	fields := make([]string, 0, 5+len(row.roleLabels))
	fields = append(fields,
		row.employee.Name, row.employee.WorkerNumber, row.employee.JobTitle,
		row.employee.UnitCode, row.unitName,
	)
	fields = append(fields, row.roleLabels...)
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}

// holdsRole reports whether the credential behind this row actually carries
// the filtered role. It reads row.roles - the bundle signed into the token -
// so the filter can never select somebody the credential would not admit.
func (row directoryRow) holdsRole(role string) bool {
	if role == "" {
		return true
	}
	for _, held := range row.roles {
		if held == role {
			return true
		}
	}
	return false
}

// selected combines the two filters. They are deliberately different shapes:
// the free text is an OR across the facts on the card, the role filter is an
// AND on top of it, so ?q=chen&role=manager reads as "someone called Chen who
// is also a people manager".
func (row directoryRow) selected(needle, role string) bool {
	return row.matches(needle) && row.holdsRole(role)
}

// loginDirectorySection renders the whole directory, or nothing at all when
// no directory is composed. A production-shaped cell supplies none, so this
// is the single gate the security test drives.
func (h *Handler) loginDirectorySection(rawQuery, rawRole string) string {
	if h.directory == nil {
		return ""
	}
	query := normalizeDirectoryQuery(rawQuery)
	render := h.newDirectoryRender(query, normalizeDirectoryQuery(rawRole))
	if len(render.rows) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<section class="directory" aria-labelledby="directory-heading">`)
	out.WriteString(`<h2 id="directory-heading">Everyone else</h2>`)
	out.WriteString(`<p class="login-intro">Every seeded employee, signing in with the roles their own job implies. Search for one, or open their team below.</p>`)
	out.WriteString(render.searchForm())
	out.WriteString(render.searchResults())
	out.WriteString(render.tree())
	out.WriteString(`</section>`)
	return out.String()
}

// searchForm is a native GET form: the query is the URL, so a result page is
// linkable, reloadable and reachable with the back button without script.
func (d *directoryRender) searchForm() string {
	var out strings.Builder
	out.WriteString(`<form class="directory-search" method="get" action="` + PathLogin + `" role="search">`)
	out.WriteString(`<label for="directory-query">Find an employee</label>`)
	out.WriteString(`<input type="search" id="directory-query" name="` + paramDirectoryQuery + `" value="` + html.EscapeString(d.query) + `" autocomplete="off" spellcheck="false" placeholder="Name, worker number, job title, team, or role">`)
	out.WriteString(d.roleSelect())
	out.WriteString(`<button type="submit">Search</button>`)
	if d.filtering() {
		// One link clears both filters, because PathLogin carries neither.
		out.WriteString(`<a class="directory-clear" href="` + PathLogin + `">Clear filters</a>`)
	}
	out.WriteString(`</form>`)
	return out.String()
}

// roleSelect is the role filter. It submits with the same GET form, so the two
// filters share one control surface and one URL, and a browser running no
// script still round-trips the selection.
func (d *directoryRender) roleSelect() string {
	if len(d.roleOptions) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<label for="directory-role">Role</label>`)
	out.WriteString(`<select id="directory-role" name="` + paramDirectoryRole + `">`)
	out.WriteString(`<option value=""`)
	if d.role == "" {
		out.WriteString(` selected`)
	}
	out.WriteString(`>Any role</option>`)
	offered := false
	for _, option := range d.roleOptions {
		out.WriteString(`<option value="` + html.EscapeString(option.id) + `"`)
		if option.id == d.role {
			out.WriteString(` selected`)
			offered = true
		}
		out.WriteString(`>` + html.EscapeString(option.label) + `</option>`)
	}
	// A shared URL may name a role nobody holds any more. Keep it selected
	// rather than silently resetting the control to "Any role" while the
	// summary and the tree below it still answer the filtered question.
	if d.role != "" && !offered {
		out.WriteString(`<option value="` + html.EscapeString(d.role) + `" selected>` + html.EscapeString(d.roleLabel) + `</option>`)
	}
	out.WriteString(`</select>`)
	return out.String()
}

// searchResults lists the matches. With no query it says what the search
// covers instead of repeating all sixty people above the tree.
func (d *directoryRender) searchResults() string {
	if !d.filtering() {
		return `<p class="directory-summary" role="status">Showing the whole organization. Search by name, worker number, job title, team, or role to narrow it.</p>`
	}
	matched := make([]directoryRow, 0, len(d.rows))
	for _, row := range d.rows {
		if row.selected(d.needle, d.role) {
			matched = append(matched, row)
		}
	}
	summary := d.summaryLine(len(matched))
	var out strings.Builder
	out.WriteString(`<p class="directory-summary" role="status">` + html.EscapeString(summary) + `</p>`)
	if len(matched) == 0 {
		return out.String()
	}
	out.WriteString(`<ul class="directory-results">`)
	for _, row := range matched {
		out.WriteString(`<li>` + row.entry() + `</li>`)
	}
	out.WriteString(`</ul>`)
	return out.String()
}

// summaryLine states exactly which question the count answers, naming both
// filters when both are set. The role is named by its label rather than
// pluralised into a sentence ("are HR partners"), because several of these
// labels -- "Employee self-service" above all -- have no honest plural.
func (d *directoryRender) summaryLine(matched int) string {
	line := strconv.Itoa(matched) + " of " + strconv.Itoa(len(d.rows)) + " employees "
	switch {
	case d.query != "" && d.role != "":
		line += "match “" + d.query + "” and hold the " + d.roleLabel + " role."
	case d.role != "":
		line += "hold the " + d.roleLabel + " role."
	default:
		line += "match “" + d.query + "”."
	}
	return line
}

// entry renders one employee: enough context to be sure they are the person
// you meant, and the control that signs in as them.
//
// Three lines, in the order a tester scans them: who (name, worker number,
// job title), where (org unit, manager), and what signing in as them grants.
// Identity and job share one line because sixty of these have to fit on a
// screen: a card per person down a single column made the page metres long.
func (row directoryRow) entry() string {
	employee := row.employee
	var out strings.Builder
	out.WriteString(`<div class="directory-entry">`)
	out.WriteString(`<span class="directory-name">` + html.EscapeString(employee.Name))
	out.WriteString(`<span class="directory-ident">` + html.EscapeString(employee.WorkerNumber+" · "+employee.JobTitle) + `</span></span>`)
	context := employee.UnitCode
	if row.unitName != "" {
		context = row.unitName
	}
	if row.managerName != "" {
		context += " · reports to " + row.managerName
	} else {
		context += " · no seeded manager"
	}
	out.WriteString(`<span class="directory-meta">` + html.EscapeString(context) + `</span>`)
	if row.signable {
		if labels := devRoleLabels(row.roles); len(labels) > 0 {
			// "Roles:" rather than a bare list, so the line is a sentence to
			// a screen reader instead of four unexplained proper nouns.
			out.WriteString(`<span class="directory-roles">Roles: ` + html.EscapeString(strings.Join(labels, ", ")) + `</span>`)
		}
		out.WriteString(`<form class="directory-signin" method="post" action="` + PathLogin + `">`)
		// As with the quick-pick cards, the selected identity rides on the
		// submit control itself rather than a hidden input, so the
		// association is explicit to assistive technology and does not
		// depend on a hidden field surviving browser form mediation.
		out.WriteString(`<button type="submit" name="` + paramLoginPersona + `" value="` + html.EscapeString(employee.PersonaID) + `">Sign in as ` + html.EscapeString(employee.Name) + `</button>`)
		out.WriteString(`</form>`)
	} else {
		out.WriteString(`<span class="directory-unavailable">No development credential is composed for this employee.</span>`)
	}
	out.WriteString(`</div>`)
	return out.String()
}

// tree renders the organization as nested disclosures. See this file's header
// for why it is hand-rolled rather than productui's tree component.
func (d *directoryRender) tree() string {
	if len(d.roots) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<h2 id="directory-tree-heading">Organization</h2>`)
	out.WriteString(`<ul class="org-tree" role="tree" aria-labelledby="directory-tree-heading">`)
	visited := make(map[string]bool, len(d.units))
	for _, code := range d.roots {
		markup, _, _ := d.unit(code, 1, visited)
		out.WriteString(markup)
	}
	out.WriteString(`</ul>`)
	return out.String()
}

// unit renders one organization unit and everything beneath it, reporting the
// headcount it covers and how many of those people the active filters select.
//
// visited guards against a malformed snapshot whose parent links form a cycle:
// a directory is seed data, but it arrives through an interface, and an
// infinite render is a worse failure than a missing branch.
func (d *directoryRender) unit(code string, level int, visited map[string]bool) (string, int, int) {
	if visited[code] {
		return "", 0, 0
	}
	visited[code] = true
	unit := d.units[code]

	var children strings.Builder
	people, matches := 0, 0
	for _, child := range d.childCodes[code] {
		markup, childPeople, childMatches := d.unit(child, level+1, visited)
		children.WriteString(markup)
		people += childPeople
		matches += childMatches
	}
	for _, row := range d.byUnit[code] {
		people++
		if !row.selected(d.needle, d.role) {
			// While a filter is active the tree answers the filtered
			// question too: a unit that opens because it holds a match must
			// not then bury that match among the people who do not.
			if d.filtering() {
				continue
			}
		} else {
			matches++
		}
		children.WriteString(`<li class="org-person" role="treeitem" aria-level="` + strconv.Itoa(level+1) + `">` + row.entry() + `</li>`)
	}

	// Sixty people across twenty units is unreadable fully expanded, so the
	// default shows the company and its divisions and leaves the departments
	// closed. An active filter overrides that: a unit opens exactly when it
	// contains a match, so the answer is never hidden behind a disclosure.
	open := level <= 2
	if d.filtering() {
		open = matches > 0
	}
	label := unit.Name
	if label == "" {
		label = code
	}
	// The count states what is actually listed underneath, so a filtered unit
	// never claims a headcount it is no longer showing.
	headcount := strconv.Itoa(people) + " people"
	if people == 1 {
		headcount = "1 person"
	}
	if d.filtering() {
		headcount = strconv.Itoa(matches) + " matches"
		if matches == 1 {
			headcount = "1 match"
		}
	}

	var out strings.Builder
	out.WriteString(`<li class="org-unit" role="treeitem" aria-level="` + strconv.Itoa(level) + `" aria-expanded="` + strconv.FormatBool(open) + `">`)
	out.WriteString(`<details`)
	if open {
		out.WriteString(` open`)
	}
	out.WriteString(`><summary><span class="org-unit-name">` + html.EscapeString(label) + `</span> <span class="org-count">` + html.EscapeString(headcount) + `</span></summary>`)
	if children.Len() > 0 {
		out.WriteString(`<ul role="group">` + children.String() + `</ul>`)
	}
	out.WriteString(`</details></li>`)
	return out.String(), people, matches
}
