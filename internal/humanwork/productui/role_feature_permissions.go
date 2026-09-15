package productui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type RoleFeaturePermissionRowProps struct {
	I18nProps
	Page       RolePageOption
	Feature    FeatureDefinition
	Permission RoleFeaturePermission
	Editable   bool
	Save       func(RoleFeaturePermission)
}

func roleFeaturePermissionEditor(props RolesPageProps, role AccessRole, pagePermissions map[PageID]RolePagePermission) ui.Node {
	permissions := make(map[string]RoleFeaturePermission, len(props.FeaturePermissions))
	for _, permission := range props.FeaturePermissions {
		if permission.RoleID == role.ID {
			permissions[string(permission.Page)+"\x00"+string(permission.Feature)] = permission
		}
	}
	pageGroups := make([]ui.Node, 0)
	featureCount := 0
	for _, page := range props.Pages {
		if !page.Published || !pagePermissions[page.ID].View {
			continue
		}
		rows := make([]ui.Node, 0, len(page.Features))
		for _, feature := range page.Features {
			permission := permissions[string(page.ID)+"\x00"+string(feature.ID)]
			permission.RoleID = role.ID
			permission.Page = page.ID
			permission.Feature = feature.ID
			rows = append(rows, ui.CreateElement(RoleFeaturePermissionRow, RoleFeaturePermissionRowProps{
				I18nProps: props.I18nProps, Page: page, Feature: feature, Permission: permission,
				Editable: props.CanUpdate, Save: props.OnSaveFeaturePermission,
			}))
		}
		if len(rows) > 0 {
			featureCount += len(rows)
			pageGroups = append(pageGroups, roleFeaturePageEditor(props, page, rows))
		}
	}
	if featureCount == 0 {
		return html.Tag("details", html.Props{Class: "role-feature-access"},
			html.Tag("summary", html.Props{}, ui.Text(props.Text("roles.feature_access"))),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Text("roles.feature_access_empty"))),
		)
	}
	return html.Tag("details", html.Props{Class: "role-feature-access"},
		html.Tag("summary", html.Props{}, ui.Text(props.Text("roles.feature_access")+" ("+strconv.Itoa(featureCount)+")")),
		html.P(html.Props{Class: "muted role-page-access-help"}, ui.Text(props.Text("roles.feature_access_help"))),
		html.Div(html.Props{Class: "role-feature-page-groups"}, pageGroups...),
	)
}

func roleFeaturePageEditor(props RolesPageProps, page RolePageOption, rows []ui.Node) ui.Node {
	label := page.Label + " (" + strconv.Itoa(len(rows)) + ")"
	return html.Tag("details", html.Props{Class: "role-feature-page", DataAttr: html.DataAttribute{Name: "page-id", Value: string(page.ID)}},
		html.Tag("summary", html.Props{},
			html.Span(html.Props{Class: "role-feature-page-name"}, ui.Text(page.Label)),
			html.Span(html.Props{Class: "badge"}, ui.Text(strconv.Itoa(len(rows)))),
		),
		html.Div(html.Props{Class: "role-page-table-wrap", TabIndex: html.TabIndexZero, Raw: map[string]any{"role": "region", "aria-label": label}},
			html.Table(html.Props{Class: "role-page-table role-feature-table"},
				html.Thead(html.Props{}, html.Tr(html.Props{},
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.feature"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.view"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.create_permission"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.update_permission"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.delete_permission"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.action"))),
				)),
				html.Tbody(html.Props{}, rows...),
			),
		),
	)
}

func RoleFeaturePermissionRow(props RoleFeaturePermissionRowProps) ui.Node {
	state := ui.UseState(props.Permission)
	if state.Get().Version != props.Permission.Version || state.Get().RoleID != props.Permission.RoleID || state.Get().Page != props.Permission.Page || state.Get().Feature != props.Permission.Feature {
		state.Set(props.Permission)
	}
	draft := state.Get()
	toggle := func(action string) {
		next := state.Get()
		switch action {
		case "view":
			next.View = !next.View
			if !next.View {
				next.Create, next.Update, next.Delete = false, false, false
			}
		case "create":
			next.Create = !next.Create
			next.View = next.View || next.Create
		case "update":
			next.Update = !next.Update
			next.View = next.View || next.Update
		case "delete":
			next.Delete = !next.Delete
			next.View = next.View || next.Delete
		}
		state.Set(next)
	}
	id := func(action string) string {
		return "role-feature-" + props.Permission.RoleID + "-" + string(props.Page.ID) + "-" + string(props.Feature.ID) + "-" + action
	}
	check := func(action, label string, supported, checked bool) ui.Node {
		input := html.Props{ID: id(action), Type: "checkbox", Checked: checked, Disabled: !props.Editable || !supported, Aria: map[string]string{"label": label + " — " + props.Page.Label + " / " + props.Feature.Label}}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) { toggle(action) })
		return html.Label(html.Props{For: id(action), Class: "permission-check"}, html.Input(input), html.Span(html.Props{Class: "sr-only"}, ui.Text(label)))
	}
	return html.Tr(html.Props{DataAttr: html.DataAttribute{Name: "feature-id", Value: string(props.Feature.ID)}},
		html.Th(html.Props{Raw: map[string]any{"scope": "row"}},
			html.Strong(html.Props{}, ui.Text(props.Feature.Label)),
			html.Small(html.Props{Class: "muted role-feature-description"}, ui.Text(props.Feature.Description)),
		),
		html.Td(html.Props{}, check("view", props.Text("roles.view"), props.Feature.View, draft.View)),
		html.Td(html.Props{}, check("create", props.Text("roles.create_permission"), props.Feature.Create, draft.Create)),
		html.Td(html.Props{}, check("update", props.Text("roles.update_permission"), props.Feature.Update, draft.Update)),
		html.Td(html.Props{}, check("delete", props.Text("roles.delete_permission"), props.Feature.Delete, draft.Delete)),
		html.Td(html.Props{}, featurePermissionSaveButton(props, draft)),
	)
}

func featurePermissionSaveButton(props RoleFeaturePermissionRowProps, draft RoleFeaturePermission) ui.Node {
	if !props.Editable || props.Save == nil {
		return html.Span(html.Props{Class: "muted permission-read-only"}, ui.Text(props.Text("roles.read_only")))
	}
	return html.Form(html.Props{OnSubmit: saveFeaturePermission(props.Save, draft)},
		html.Button(html.Props{Class: "button secondary compact", Type: "submit"}, ui.Text(props.Text("roles.save_page"))),
	)
}

func saveFeaturePermission(save func(RoleFeaturePermission), draft RoleFeaturePermission) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		save(draft)
	})
}
