package productui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const roleDirectoryPageSize = 20

type RolesPageProps struct {
	I18nProps
	Roles                   []AccessRole
	Assignments             []WorkerRoleAssignment
	Policies                []OrganizationVisibilityPolicy
	PagePermissions         []RolePagePermission
	FeaturePermissions      []RoleFeaturePermission
	Pages                   []RolePageOption
	People                  []Person
	Query                   string
	Page                    int
	FilterHref              string
	Navigate                func(string)
	Back                    ActionLinkProps
	CanCreate               bool
	CanUpdate               bool
	OnSaveRole              func(AccessRole)
	OnAssign                func(WorkerRoleAssignment)
	OnSavePermission        func(RolePagePermission)
	OnSaveFeaturePermission func(RoleFeaturePermission)
}

// RolePageOption is registry metadata for one configurable page. It keeps
// the reusable editor independent from the application page registry.
type RolePageOption struct {
	ID          PageID
	Label       string
	Description string
	Published   bool
	Features    []FeatureDefinition
}

func RolesPage(props RolesPageProps) ui.Node {
	filterQuery := ui.UseState(props.Query)
	ui.UseEffectOf(func() func() { filterQuery.Set(props.Query); return nil }, props.Query)
	filterInput := SearchInputProps{ID: "role-directory-query", Name: "q", Value: filterQuery.Get(), Placeholder: props.Text("roles.placeholder"), OnInput: filterQuery.Set}
	filterSubmit := submitRoleFilter(props.Navigate, props.FilterHref, filterQuery.Get)
	roleCards := make([]ui.Node, 0, len(props.Roles))
	for _, role := range props.Roles {
		kind := props.Text("roles.customer_role")
		if role.System {
			kind = props.Text("roles.system_role")
		}
		roleCards = append(roleCards, ui.CreateElement(RoleAccessCard, roleAccessCardProps{Page: props, Role: role, Kind: kind}))
	}

	assignments := make(map[string]WorkerRoleAssignment, len(props.Assignments))
	for _, assignment := range props.Assignments {
		assignments[strings.ToLower(strings.TrimSpace(assignment.WorkerRef))] = assignment
	}
	// Keep the directory bounded even when the admitted projection contains a
	// large workforce. This is presentation-only paging: authorization and the
	// admitted population remain server-owned, and saves remain per worker.
	query := strings.ToLower(strings.TrimSpace(filterQuery.Get()))
	filteredPeople := make([]Person, 0, len(props.People))
	for _, person := range props.People {
		if query != "" && !strings.Contains(strings.ToLower(strings.Join([]string{person.Name, person.Role, person.Team, person.WorkerNumber}, " ")), query) {
			continue
		}
		filteredPeople = append(filteredPeople, person)
	}
	window := PaginateCollection(filteredPeople, props.Page, roleDirectoryPageSize)
	people := make([]ui.Node, 0, len(window.Items))
	for _, person := range window.Items {
		assignment, ok := assignments[strings.ToLower(strings.TrimSpace(person.ID))]
		if !ok {
			assignment = WorkerRoleAssignment{WorkerRef: person.ID}
		}
		people = append(people, ui.CreateElement(WorkerRoleEditor, workerRoleEditorProps{I18n: props.I18nProps, Person: person, Roles: props.Roles, Assignment: assignment, Editable: props.CanUpdate, Save: props.OnAssign}))
	}
	noPeopleMatch := len(filteredPeople) == 0
	if noPeopleMatch {
		people = append(people, html.Div(html.Props{Class: "empty-state"}, html.Strong(html.Props{}, ui.Text(props.Text("roles.no_match")))))
	}
	var assignmentTable ui.Node
	if noPeopleMatch {
		assignmentTable = html.Div(html.Props{Class: "employee-role-table-wrap employee-role-list"}, people...)
	} else {
		assignmentTable = html.Div(html.Props{Class: "employee-role-table-wrap employee-role-list role-page-table-wrap data-table-scroll", TabIndex: html.TabIndexZero, Raw: map[string]any{"role": "region", "aria-label": props.Text("roles.assignments"), "data-preserve-scroll": "true", "data-preserve-focus": "true"}},
			html.Table(html.Props{Class: "data-table employee-role-table role-page-table", ID: "roles-assignment-table"},
				html.Thead(html.Props{}, html.Tr(html.Props{},
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.column_employee"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.column_current"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.column_assign"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.column_action"))),
				)),
				html.Tbody(html.Props{}, people...),
			),
		)
	}
	pageControls := roleDirectoryPagination(props, window)

	return html.Div(html.Props{Class: "roles-access-page"},
		html.Div(html.Props{Class: "roles-access-intro"},
			html.Nav(html.Props{Class: "roles-page-jumps", Aria: map[string]string{"label": props.Text("roles.sections")}},
				html.A(html.Props{Href: "#role-catalog"}, ui.Text(props.Text("roles.catalog"))),
				html.A(html.Props{Href: "#role-assignments"}, ui.Text(props.Text("roles.assignments"))),
			),
			props.BackNode(),
		),
		html.Div(html.Props{Class: "roles-access-layout"},
			html.Section(html.Props{Class: "surface role-catalog", ID: "role-catalog"},
				ui.CreateElement(SectionHeading, SectionHeadingProps{Title: props.Text("roles.catalog"), Description: props.Text("roles.catalog_help")}),
				html.Tag("details", html.Props{Class: "role-create-disclosure"}, html.Tag("summary", html.Props{}, ui.Text(props.Text("roles.create"))), createRoleForm(props.I18nProps, props.CanCreate, props.OnSaveRole)),
				html.Div(html.Props{Class: "access-role-grid"}, roleCards...),
			),
			html.Section(html.Props{Class: "surface employee-role-directory", ID: "role-assignments"},
				ui.CreateElement(SectionHeading, SectionHeadingProps{Title: props.Text("roles.assignments"), Description: props.Text("roles.assignments_help")}),
				html.Form(html.Props{Class: "role-directory-filter", Action: props.FilterHref, Method: "get", OnSubmit: filterSubmit}, html.Label(html.Props{For: "role-directory-query"}, ui.Text(props.Text("roles.find"))), html.Div(html.Props{}, ui.CreateElement(SearchInput, filterInput), html.Button(html.Props{Class: "button secondary", Type: "submit"}, ui.Text(props.Text("roles.filter"))))),
				html.Div(html.Props{Class: "role-directory-guidance", Raw: map[string]any{"role": "status"}}, html.Span(html.Props{Class: "muted"}, ui.Text(props.Text("roles.per_worker_guidance")))),
				html.P(html.Props{Class: "role-effective-boundary muted"}, ui.Text(props.Text("roles.effective_boundary"))),
				html.P(html.Props{Class: "muted role-page-scroll-hint role-effective-boundary"}, ui.Text(props.Text("roles.scroll_hint"))),
				assignmentTable,
				pageControls,
			),
		),
	)
}

type roleAccessCardProps struct {
	Page RolesPageProps
	Role AccessRole
	Kind string
}

func RoleAccessCard(input roleAccessCardProps) ui.Node {
	return roleAccessCard(input.Page, input.Role, input.Kind)
}

type workerRoleEditorProps struct {
	I18n       I18nProps
	Person     Person
	Roles      []AccessRole
	Assignment WorkerRoleAssignment
	Editable   bool
	Save       func(WorkerRoleAssignment)
}

func WorkerRoleEditor(input workerRoleEditorProps) ui.Node {
	return workerRoleAssignmentEditorWithSelection(input.I18n, input.Person, input.Roles, input.Assignment, input.Editable, input.Save, false, nil)
}

func roleAccessCard(props RolesPageProps, role AccessRole, kind string) ui.Node {
	permissions := make(map[PageID]RolePagePermission, len(props.PagePermissions))
	for _, permission := range props.PagePermissions {
		if permission.RoleID == role.ID {
			permissions[permission.Page] = permission
		}
	}
	rows := make([]ui.Node, 0, len(props.Pages))
	unpublishedRows := make([]ui.Node, 0)
	for _, page := range props.Pages {
		permission := permissions[page.ID]
		permission.RoleID = role.ID
		permission.Page = page.ID
		row := ui.CreateElement(RolePagePermissionRow, RolePagePermissionRowProps{
			I18nProps: props.I18nProps, Page: page, Permission: permission, Editable: props.CanUpdate, Save: props.OnSavePermission,
		})
		if page.Published {
			rows = append(rows, row)
		} else {
			unpublishedRows = append(unpublishedRows, row)
		}
	}
	var policy OrganizationVisibilityPolicy
	for _, candidate := range props.Policies {
		if candidate.RoleID == role.ID {
			policy = candidate
			break
		}
	}
	definition := roleDefinitionDisclosure(props, role, policy, roleGrantedPageCount(props.PagePermissions, role.ID))
	featureAccess := roleFeaturePermissionEditor(props, role, permissions)
	return html.Tag("details", html.Props{Class: "access-role-card"},
		html.Tag("summary", html.Props{},
			html.Div(html.Props{Class: "access-role-identity"}, html.Strong(html.Props{}, ui.Text(role.Name)), html.Code(html.Props{}, ui.Text(role.ID))),
			html.Span(html.Props{Class: "status"}, ui.Text(kind)),
			html.P(html.Props{Class: "muted"}, ui.Text(role.Description)),
			html.Small(html.Props{Class: "muted access-role-expand"}, ui.Text(props.Text("roles.inspect_role"))),
		),
		definition,
		html.Tag("details", html.Props{Class: "role-page-access"},
			html.Tag("summary", html.Props{}, ui.Text(props.Text("roles.page_access"))),
			html.P(html.Props{Class: "muted role-page-access-help"}, ui.Text(props.Text("roles.page_access_help"))),
			html.P(html.Props{Class: "muted role-page-scroll-hint"}, ui.Text(props.Text("roles.scroll_hint"))),
			rolePagePermissionTable(props, rows, props.Text("roles.published_pages")),
			html.Tag("details", html.Props{Class: "role-unpublished-pages"},
				html.Tag("summary", html.Props{}, ui.Text(props.Text("roles.unpublished_pages")+" ("+strconv.Itoa(len(unpublishedRows))+")")),
				html.P(html.Props{Class: "muted"}, ui.Text(props.Text("roles.unpublished_pages_help"))),
				html.P(html.Props{Class: "muted role-page-scroll-hint"}, ui.Text(props.Text("roles.scroll_hint"))),
				rolePagePermissionTable(props, unpublishedRows, props.Text("roles.unpublished_pages")),
			),
		),
		featureAccess,
	)
}

func rolePagePermissionTable(props RolesPageProps, rows []ui.Node, label string) ui.Node {
	return html.Div(html.Props{Class: "role-page-table-wrap", TabIndex: html.TabIndexZero, Raw: map[string]any{"role": "region", "aria-label": label}},
		html.Table(html.Props{Class: "role-page-table"},
			html.Thead(html.Props{}, html.Tr(html.Props{}, html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.page"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.view"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.create_permission"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.update_permission"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.delete_permission"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.action"))))),
			html.Tbody(html.Props{}, rows...),
		),
	)
}

func roleGrantedPageCount(permissions []RolePagePermission, roleID string) int {
	count := 0
	for _, permission := range permissions {
		if permission.RoleID == roleID && (permission.View || permission.Create || permission.Update || permission.Delete) {
			count++
		}
	}
	return count
}

// roleDefinitionDisclosure is deliberately projection-only. It exposes the
// complete role definition admitted by the service (identity, page grants,
// organization scope and data domains) without deriving effective permission
// from worker checkboxes or claiming that a browser-side preview is an
// authorization decision.
func roleDefinitionDisclosure(props RolesPageProps, role AccessRole, policy OrganizationVisibilityPolicy, pageCount int) ui.Node {
	units := append([]string(nil), policy.OrganizationUnits...)
	domains := append([]string(nil), policy.DataDomains...)
	definitionFields := []ui.Node{
		html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(props.Text("roles.role_id"))), html.Tag("dd", html.Props{}, html.Code(html.Props{}, ui.Text(role.ID)))),
	}
	// Scope and domains are optional because older server adapters do not
	// provide those projections. Omitting them is safer than describing an
	// invented empty/global scope to an administrator.
	if mode := strings.TrimSpace(policy.Mode); mode != "" {
		definitionFields = append(definitionFields,
			html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(props.Text("organization_visibility.scope_title"))), html.Tag("dd", html.Props{}, ui.Text(localizedRoleMode(props, mode)))),
		)
	}
	if len(units) > 0 {
		definitionFields = append(definitionFields,
			html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(props.Text("organization_visibility.units_title"))), html.Tag("dd", html.Props{}, ui.Text(strings.Join(units, ", ")))),
		)
	}
	if len(domains) > 0 {
		definitionFields = append(definitionFields,
			html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(props.Text("data_domain_scope.title"))), html.Tag("dd", html.Props{}, ui.Text(strings.Join(domains, ", ")))),
		)
	}
	definitionFields = append(definitionFields,
		html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(props.Text("roles.page_access"))), html.Tag("dd", html.Props{}, ui.Text(strconv.Itoa(pageCount)))),
	)
	return html.Div(html.Props{Class: "role-definition"},
		html.H3(html.Props{}, ui.Text(props.Text("roles.definition"))),
		html.P(html.Props{Class: "muted"}, ui.Text(role.Description)),
		html.Tag("dl", html.Props{}, definitionFields...),
		html.P(html.Props{Class: "muted role-definition-boundary"}, ui.Text(props.Text("organization_visibility.boundary_detail"))),
	)
}

func localizedRoleMode(props RolesPageProps, mode string) string {
	key := map[string]string{"ALL": "organization_visibility.mode_all", "OWN_UNIT": "organization_visibility.mode_own", "ALLOWLIST": "organization_visibility.mode_allow", "DENYLIST": "organization_visibility.mode_deny"}[strings.ToUpper(strings.TrimSpace(mode))]
	if key == "" {
		return props.Text("organization_visibility.scope_empty")
	}
	return props.Text(key)
}

type RolePagePermissionRowProps struct {
	I18nProps
	Page       RolePageOption
	Permission RolePagePermission
	Editable   bool
	Save       func(RolePagePermission)
}

// rolePermissionDraftState is the row's client draft plus the server
// snapshot it was seeded from. Local edits live in the booleans; the
// key never carries edits, so re-renders keep the draft while drift
// resets it.
type rolePermissionDraftState struct {
	key                          RolePagePermission
	view, create, update, remove bool
}

// permissionSnapshot extracts the server-addressed grant snapshot a
// draft reconciles against: role, page, version, and the four grants.
func permissionSnapshot(permission RolePagePermission) RolePagePermission {
	return RolePagePermission{Version: permission.Version, RoleID: permission.RoleID, Page: permission.Page,
		View: permission.View, Create: permission.Create, Update: permission.Update, Delete: permission.Delete}
}

// reconcileRolePermissionDraft keeps local edits while the incoming
// projection matches the seeded snapshot and resets to the incoming
// grants on drift: a version bump, changed grants, or another role or
// page. The second result reports whether the draft was replaced, so
// the component can invalidate its client cache exactly on drift.
func reconcileRolePermissionDraft(incoming RolePagePermission, current rolePermissionDraftState) (rolePermissionDraftState, bool) {
	key := permissionSnapshot(incoming)
	if current.key == key {
		return current, false
	}
	return rolePermissionDraftState{key: key, view: incoming.View, create: incoming.Create, update: incoming.Update, remove: incoming.Delete}, true
}

// RolePagePermissionRow owns draft checkbox state so dependent CRUD controls
// update immediately and never submit a mutation grant without page access.
// The draft reconciles against the incoming projection: local edits survive
// re-renders, authority drift resets to the server grants.
func RolePagePermissionRow(props RolePagePermissionRowProps) ui.Node {
	seeded := rolePermissionDraftState{key: permissionSnapshot(props.Permission),
		view: props.Permission.View, create: props.Permission.Create, update: props.Permission.Update, remove: props.Permission.Delete}
	state := ui.UseState(seeded)
	draft, reset := reconcileRolePermissionDraft(props.Permission, state.Get())
	if reset {
		state.Set(draft)
	}
	toggle := func(action string) {
		next := state.Get()
		switch action {
		case "view":
			next.view = !next.view
			if !next.view {
				next.create, next.update, next.remove = false, false, false
			}
		case "create":
			next.create = !next.create
			if next.create {
				next.view = true
			}
		case "update":
			next.update = !next.update
			if next.update {
				next.view = true
			}
		case "delete":
			next.remove = !next.remove
			if next.remove {
				next.view = true
			}
		}
		state.Set(next)
	}
	checked := map[string]bool{"view": draft.view, "create": draft.create, "update": draft.update, "delete": draft.remove}
	id := func(action string) string {
		return "role-page-" + props.Permission.RoleID + "-" + string(props.Page.ID) + "-" + action
	}
	check := func(action, label string) ui.Node {
		input := html.Props{ID: id(action), Type: "checkbox", Checked: checked[action], Disabled: !props.Editable, Aria: map[string]string{"label": label + " — " + props.Page.Label}}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) { toggle(action) })
		return html.Label(html.Props{For: id(action), Class: "permission-check"}, html.Input(input), html.Span(html.Props{Class: "sr-only"}, ui.Text(label)))
	}
	saved := props.Permission
	saved.View, saved.Create, saved.Update, saved.Delete = draft.view, draft.create, draft.update, draft.remove
	return html.Tr(html.Props{},
		html.Th(html.Props{Raw: map[string]any{"scope": "row"}}, html.Strong(html.Props{}, ui.Text(props.Page.Label)), html.Small(html.Props{Class: "muted"}, ui.Text(props.Page.Description))),
		html.Td(html.Props{}, check("view", props.Text("roles.view"))),
		html.Td(html.Props{}, check("create", props.Text("roles.create_permission"))),
		html.Td(html.Props{}, check("update", props.Text("roles.update_permission"))),
		html.Td(html.Props{}, check("delete", props.Text("roles.delete_permission"))),
		html.Td(html.Props{}, permissionSaveButton(props, saved)),
	)
}

func permissionSaveButton(props RolePagePermissionRowProps, draft RolePagePermission) ui.Node {
	statusID := "role-page-permission-status-" + draft.RoleID + "-" + string(draft.Page)
	if !props.Editable || props.Save == nil {
		return html.Span(html.Props{Class: "muted permission-read-only"}, ui.Text(props.Text("roles.read_only")))
	}
	return html.Form(html.Props{OnSubmit: savePagePermission(props.Save, draft)}, html.Button(html.Props{Class: "button secondary compact", Type: "submit"}, ui.Text(props.Text("roles.save_page"))), html.Span(html.Props{ID: statusID, Class: "sr-only", Raw: map[string]any{"role": "status", "aria-live": "polite"}}))
}

func submitRoleFilter(navigate func(string), action string, query func() string) ui.Handler {
	if navigate == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if query == nil {
			return
		}
		navigate(roleFilterHref(action, query()))
	})
}

func roleFilterHref(action, query string) string {
	parsed, err := url.Parse(action)
	if err != nil {
		return action
	}
	values := parsed.Query()
	values.Set("q", strings.TrimSpace(query))
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func (props RolesPageProps) BackNode() ui.Node { return ui.CreateElement(ActionLink, props.Back) }

func createRoleForm(i18n I18nProps, editable bool, save func(AccessRole)) ui.Node {
	if !editable || save == nil {
		return html.Div(html.Props{Class: "create-role-form role-read-only"}, html.Strong(html.Props{}, ui.Text(i18n.Text("roles.read_only_heading"))), html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text("roles.read_only_help"))))
	}
	draft := AccessRole{Active: true}
	id := html.Props{ID: "new-role-id", Name: "role_id", Type: "text", Pattern: "[a-z][a-z0-9_]{1,62}", AutoComplete: "off", Required: true}
	id.OnInput = ui.UseEvent(func(event ui.InputEvent) { draft.ID = event.GetValue() })
	name := html.Props{ID: "new-role-name", Name: "role_name", Type: "text", MaxLength: 96, AutoComplete: "off", Required: true}
	name.OnInput = ui.UseEvent(func(event ui.InputEvent) { draft.Name = event.GetValue() })
	description := html.Props{ID: "new-role-description", Name: "role_description", MaxLength: 400}
	description.OnInput = ui.UseEvent(func(event ui.InputEvent) { draft.Description = event.GetValue() })
	return html.Form(html.Props{Class: "create-role-form", OnSubmit: saveRole(save, &draft)},
		html.H3(html.Props{}, ui.Text(i18n.Text("roles.create"))),
		html.Div(html.Props{Class: "create-role-fields"},
			ui.CreateElement(LabeledControl, LabeledControlProps{For: id.ID, Label: i18n.Text("roles.role_id"), Control: html.Input(id), Help: i18n.Text("roles.role_id_help")}),
			ui.CreateElement(LabeledControl, LabeledControlProps{For: name.ID, Label: i18n.Text("roles.display_name"), Control: html.Input(name)}),
			ui.CreateElement(LabeledControl, LabeledControlProps{For: description.ID, Label: i18n.Text("roles.description_label"), Control: html.Textarea(description)}),
		),
		html.Div(html.Props{Class: "role-form-actions"}, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(i18n.Text("roles.create_action"))), html.P(html.Props{ID: "role-access-status", Class: "muted", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(i18n.Text("roles.status")))),
	)
}

func workerRoleAssignmentEditor(i18n I18nProps, person Person, roles []AccessRole, assignment WorkerRoleAssignment, editable bool, save func(WorkerRoleAssignment)) ui.Node {
	return workerRoleAssignmentEditorWithSelection(i18n, person, roles, assignment, editable, save, false, nil)
}

func workerRoleAssignmentEditorWithSelection(i18n I18nProps, person Person, roles []AccessRole, assignment WorkerRoleAssignment, editable bool, save func(WorkerRoleAssignment), workerSelected bool, onSelect func(string, bool)) ui.Node {
	// A table row keeps the directory scannable at hundreds or thousands of
	// workers. Each row owns a compact multi-select fieldset; expanding one
	// worker must never push the rest of the directory out of view.
	state := ui.UseState(append([]string(nil), assignment.RoleIDs...))
	selectedRoles := func() map[string]bool {
		selected := make(map[string]bool, len(state.Get()))
		for _, roleID := range state.Get() {
			selected[roleID] = true
		}
		return selected
	}
	selected := selectedRoles()
	choices := make([]ui.Node, 0, len(roles))
	for _, role := range roles {
		if !role.Active {
			continue
		}
		roleID := role.ID
		input := html.Props{ID: "employee-role-" + person.ID + "-" + roleID, Type: "checkbox", Name: "role", Value: roleID, Checked: selected[roleID], Disabled: !editable, Aria: map[string]string{"label": role.Name}}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) {
			next := append([]string(nil), state.Get()...)
			found := false
			for index, candidate := range next {
				if candidate == roleID {
					next = append(next[:index], next[index+1:]...)
					found = true
					break
				}
			}
			if !found {
				next = append(next, roleID)
			}
			state.Set(next)
		})
		choices = append(choices, html.Label(html.Props{For: input.ID, Class: "employee-role-choice"}, html.Input(input), html.Span(html.Props{}, ui.Text(role.Name))))
	}
	badges := make([]ui.Node, 0, len(assignment.RoleIDs))
	currentRoleIDs := state.Get()
	for _, roleID := range currentRoleIDs {
		label := roleID
		for _, role := range roles {
			if role.ID == roleID && strings.TrimSpace(role.Name) != "" {
				label = role.Name
				break
			}
		}
		badges = append(badges, html.Span(html.Props{Class: "status"}, ui.Text(label)))
	}
	if len(badges) == 0 {
		badges = append(badges, html.Span(html.Props{Class: "muted"}, ui.Text(i18n.Text("roles.no_explicit_assignment"))))
	}
	actions := []ui.Node{html.Small(html.Props{Class: "muted"}, ui.Text(i18n.Text("roles.read_only")))}
	draft := assignment
	draft.RoleIDs = append([]string(nil), currentRoleIDs...)
	formID := "employee-role-form-" + person.ID
	if editable && save != nil {
		actions = []ui.Node{html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: len(currentRoleIDs) == 0, Raw: map[string]any{"form": formID}}, ui.Text(i18n.Text("roles.save_employee")))}
	}
	return html.Tr(html.Props{Class: "employee-role-editor", DataAttr: html.DataAttribute{Name: "worker-ref", Value: person.ID}},
		html.Th(html.Props{Raw: map[string]any{"scope": "row"}},
			html.Div(html.Props{Class: "employee-role-identity"},
				personAvatar(person.Name, person.Initials, person.PhotoURL, "small"),
				html.Strong(html.Props{}, ui.Text(ResolveWorkerIdentity(i18n.Locale, person, nil).Label)),
				html.Small(html.Props{Class: "muted"}, ui.Text(strings.Trim(strings.Join([]string{person.Role, person.Team}, " · "), " ·"))),
			),
		),
		html.Td(html.Props{}, html.Div(html.Props{Class: "employee-role-badges"}, badges...)),
		html.Td(html.Props{}, html.Form(html.Props{ID: formID, Class: "employee-role-form", OnSubmit: saveAssignment(save, &draft)}, html.Tag("details", html.Props{Class: "employee-role-assignment"}, html.Tag("summary", html.Props{}, ui.Text(i18n.Text("roles.assigned_roles"))), html.Fieldset(html.Props{}, html.Legend(html.Props{Class: "sr-only"}, ui.Text(i18n.Text("roles.assigned_roles"))), html.Div(html.Props{Class: "employee-role-choices"}, choices...))))),
		html.Td(html.Props{}, html.Div(html.Props{Class: "role-form-actions"}, actions...)),
	)
}

func roleDirectoryPagination(props RolesPageProps, window PaginateWindow[Person]) ui.Node {
	links := make([]ui.Node, 0, 2)
	if window.Page > 1 {
		links = append(links, softwareLink(props.Navigate, html.Props{Class: "button secondary"}, roleDirectoryPageHref(props, window.Page-1), ui.Text(props.Text("common.previous"))))
	}
	if window.Page < window.PageCount {
		links = append(links, softwareLink(props.Navigate, html.Props{Class: "button secondary"}, roleDirectoryPageHref(props, window.Page+1), ui.Text(props.Text("common.next"))))
	}
	return html.Nav(html.Props{Class: "role-directory-pagination", Aria: map[string]string{"label": props.Text("roles.assignments")}},
		html.Span(html.Props{Class: "muted"}, ui.Text(strconv.Itoa(window.First)+"–"+strconv.Itoa(window.Last)+" / "+strconv.Itoa(window.Total))),
		html.Div(html.Props{Class: "role-form-actions"}, links...))
}

func roleDirectoryPageHref(props RolesPageProps, page int) string {
	parsed, err := url.Parse(props.FilterHref)
	if err != nil {
		return props.FilterHref
	}
	values := parsed.Query()
	if props.Query != "" {
		values.Set("q", props.Query)
	}
	values.Set("role_page", strconv.Itoa(page))
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func roleDirectoryWindowPage(people []Person, query string, page int) int {
	query = strings.ToLower(strings.TrimSpace(query))
	count := 0
	for _, person := range people {
		if query == "" || strings.Contains(strings.ToLower(strings.Join([]string{person.Name, person.Role, person.Team, person.WorkerNumber}, " ")), query) {
			count++
		}
	}
	return PaginateBounds(count, page, roleDirectoryPageSize).Page
}

func savePagePermission(save func(RolePagePermission), draft RolePagePermission) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(draft) })
}

func saveRole(save func(AccessRole), draft *AccessRole) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(*draft) })
}

func saveAssignment(save func(WorkerRoleAssignment), draft *WorkerRoleAssignment) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(*draft) })
}
