package chatui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// ModEditorState is the editor's own state: whether it is open, which filter it
// edits, the choices that decide which fields show, and the one-line problem
// each field has. Typed text is never in it: the fields are read when Save or
// Try is pressed, so a render never writes over what is being typed.
type ModEditorState struct {
	Open bool
	// EditID, EditVersion, EditHard and EditChannels are the loaded definition's
	// when an existing filter is being edited; EditID is "" for a new filter.
	EditID, EditVersion string
	EditHard            bool
	EditChannels        []string
	Kind, Action        string
	Detector, Scope     string
	// Errors maps a field to the copy line that says what is wrong with it.
	Errors map[string]string
	// Tried is true once Try was pressed in this editor session; TriedDry is the
	// "record only" choice at that moment.
	Tried, TriedDry bool
}

// ModAdminProps is everything the administrator's filter panel needs. The same
// panel serves a channel's details (Workspace false) and the workspace's own
// settings (Workspace true).
type ModAdminProps struct {
	Model Model
	// Workspace makes every switch write the workspace's row.
	Workspace bool
	// Channel is the open conversation; the Model's selected one when empty.
	Channel     string
	Definitions []chatfilter.Definition
	Enablements []chatfilter.Enablement
	Now         time.Time
	// Loading is true until the first read answered. Busy is true while a write
	// is going; Trying while a sample is being tried.
	Loading, Busy, Trying bool
	// LoadError is the copy line of a failed read. Controls delivered by an
	// earlier read stay on the page beside it.
	LoadError string
	CanManage bool
	// Status is the copy line of the last action's outcome, StatusNote a second
	// line that follows it, StatusError whether it is a refusal.
	Status, StatusNote string
	StatusError        bool
	// Revision changes after every write so the selects redraw from the server's
	// answer, which is what a refused write leaves them showing.
	Revision int
	// Result is the service's answer to the last tried sample, TrySample that
	// sample, TryError the copy line of a refusal.
	Result    *chatfilter.Result
	TrySample string
	TryError  string
	// Editor seeds the editor's state, for a page drawn already open.
	Editor *ModEditorState
	Switch func(ModSwitch)
	Save   func(def chatfilter.Definition, dryRun bool, done func(ok bool))
	Try    func(def chatfilter.Definition, sample string)
	Retry  func()
	// Hits is the "What the filters caught" section (chatmod003_hits.go); nil
	// where the page has no way to read the matches.
	Hits *ModHitsProps
}

// ModAdminPanel is the administrator's filter panel.
func ModAdminPanel(props ModAdminProps) ui.Node { return ui.CreateElement(modAdminPanel, props) }

type modHandlers struct{ toggle, action, reset, edit ui.Handler }

func modSafeID(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// modChannels is the conversations a filter can be written for: channels, not
// direct conversations.
func modChannels(m Model) []Conversation {
	var out []Conversation
	for _, c := range m.Conversations {
		if c.ID != "" && (c.Kind == PublicChannel || c.Kind == PrivateChannel) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func modChannelName(m Model, id string) string {
	for _, c := range m.Conversations {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}

func modAdminPanel(props ModAdminProps) ui.Node {
	m := props.Model
	t := func(key string) string { return modadminText(m, key) }
	channel := props.Channel
	if props.Workspace {
		channel = ""
	} else if channel == "" {
		channel = m.SelectedID
	}
	now := props.Now
	if now.IsZero() {
		now = time.Now()
	}
	initial := ModEditorState{Kind: ModKindWords, Action: "block", Detector: "card", Scope: "workspace"}
	if props.Editor != nil {
		initial = *props.Editor
	}
	editor := ui.UseState(initial)
	choices := modChannels(m)

	switchRequest := func(request ModSwitch) {
		if props.Switch != nil {
			props.Switch(request)
		}
	}
	toggle := ui.UseEvent(func(e ui.MouseEvent) {
		_, id, _ := eventAction(e)
		if def, ok := ModDefinitionByID(props.Definitions, id); ok {
			switchRequest(ModToggleRequest(def, props.Enablements, channel, props.Workspace, now))
		}
	})
	action := ui.UseEvent(func(e ui.ChangeEvent) {
		_, id, _ := eventAction(e)
		def, ok := ModDefinitionByID(props.Definitions, id)
		next := domValue("modadmin-act-" + modSafeID(id))
		if ok && def.Product && (next == "block" || next == "mask" || next == "flag") {
			switchRequest(ModActionRequest(def, channel, props.Workspace, next))
		}
	})
	reset := ui.UseEvent(func(e ui.MouseEvent) {
		_, id, _ := eventAction(e)
		if def, ok := ModDefinitionByID(props.Definitions, id); ok {
			switchRequest(ModResetRequest(def, props.Enablements, channel, now))
		}
	})
	setField := func(id, value string) { setDOMValue("modadmin-"+id, value) }
	openEditor := func(state ModEditorState, v ModFormValues) {
		setField("name", v.Name)
		setField("match", v.Match)
		setField("domains", v.Domains)
		setField("target", v.Target)
		setField("roles", v.Roles)
		setField("agents", v.Agents)
		setField("sample", "")
		setField("kind", state.Kind)
		setField("detector", state.Detector)
		setField("action", state.Action)
		setField("scope", state.Scope)
		filterSetChecked("modadmin-hard", v.Hard)
		filterSetChecked("modadmin-dry", v.DryRun)
		for i, c := range choices {
			filterSetChecked("modadmin-ch-"+strconv.Itoa(i), modContains(v.Channels, c.ID))
		}
		state.Open = true
		editor.Set(state)
		focusField("modadmin-name")
	}
	edit := ui.UseEvent(func(e ui.MouseEvent) {
		_, id, _ := eventAction(e)
		def, ok := ModDefinitionByID(props.Definitions, id)
		if !ok || !ModIsCustom(def) {
			return
		}
		v := ModFormValuesOf(def)
		v.DryRun = ModResolve(def, props.Enablements, channel, now).DryRun
		detector := v.Detector
		if detector == "" {
			detector = "card"
		}
		openEditor(ModEditorState{EditID: def.ID, EditVersion: def.Version, EditHard: def.Hard, EditChannels: def.Channels, Kind: v.Kind, Action: def.Action, Detector: detector, Scope: "workspace"}, v)
	})
	create := ui.UseEvent(func() {
		openEditor(ModEditorState{Kind: ModKindWords, Action: "block", Detector: "card", Scope: "workspace"}, ModFormValues{Kind: ModKindWords, Action: "block"})
	})
	closeEditor := ui.UseEvent(func() {
		editor.Update(func(s ModEditorState) ModEditorState { s.Open, s.Errors = false, nil; return s })
	})
	kindChange := ui.UseEvent(func() {
		editor.Update(func(s ModEditorState) ModEditorState {
			if next := domValue("modadmin-kind"); modContains(ModKinds, next) {
				s.Kind = next
			}
			s.Errors = nil
			return s
		})
	})
	actionChange := ui.UseEvent(func() {
		editor.Update(func(s ModEditorState) ModEditorState {
			if next := domValue("modadmin-action"); modContains(ModActions, next) {
				s.Action = next
			}
			delete(s.Errors, "target")
			return s
		})
	})
	detectorChange := ui.UseEvent(func() {
		editor.Update(func(s ModEditorState) ModEditorState {
			if next := domValue("modadmin-detector"); modContains(ModDetectors, next) {
				s.Detector = next
			}
			return s
		})
	})
	scopeChange := ui.UseEvent(func() {
		editor.Update(func(s ModEditorState) ModEditorState {
			if next := domValue("modadmin-scope"); next == "workspace" || next == "chosen" {
				s.Scope = next
			}
			delete(s.Errors, "scope")
			return s
		})
	})
	readForm := func(forTry bool) ModForm {
		state := editor.Get()
		f := ModForm{Edit: state.EditID != "", ID: state.EditID, Version: state.EditVersion, KeepChannels: state.EditChannels, Name: domValue("modadmin-name"), Kind: domValue("modadmin-kind"), Match: domValue("modadmin-match"),
			Detector: domValue("modadmin-detector"), Domains: domValue("modadmin-domains"), Action: domValue("modadmin-action"), Target: domValue("modadmin-target"),
			Scope: "channel", Channel: channel, Roles: domValue("modadmin-roles"), Agents: domValue("modadmin-agents"), Hard: filterChecked("modadmin-hard"), Admin: m.IsTenantAdmin, ForTry: forTry}
		if props.Workspace {
			f.Scope = "workspace"
			if domValue("modadmin-scope") == "chosen" {
				f.Scope = "chosen"
				for i, c := range choices {
					if filterChecked("modadmin-ch-" + strconv.Itoa(i)) {
						f.Chosen = append(f.Chosen, c.ID)
					}
				}
			}
		}
		return f
	}
	failField := func(field, key string) {
		editor.Update(func(s ModEditorState) ModEditorState {
			s.Errors = map[string]string{field: key}
			s.Tried = false
			return s
		})
	}
	save := ui.UseEvent(func(e ui.FormEvent) {
		e.PreventDefault()
		def, problem := ModDefinitionFromForm(readForm(false), nil)
		if problem != nil {
			failField(problem.Field, problem.Key)
			return
		}
		editor.Update(func(s ModEditorState) ModEditorState { s.Errors = nil; return s })
		if props.Save != nil {
			props.Save(def, filterChecked("modadmin-dry"), func(ok bool) {
				if ok {
					editor.Update(func(s ModEditorState) ModEditorState { s.Open, s.Errors, s.Tried = false, nil, false; return s })
				}
			})
		}
	})
	try := ui.UseEvent(func() {
		def, problem := ModDefinitionFromForm(readForm(true), nil)
		if problem != nil {
			failField(problem.Field, problem.Key)
			return
		}
		sample := domValue("modadmin-sample")
		if strings.TrimSpace(sample) == "" {
			failField("sample", "try_empty")
			return
		}
		dry := filterChecked("modadmin-dry")
		editor.Update(func(s ModEditorState) ModEditorState { s.Errors, s.Tried, s.TriedDry = nil, true, dry; return s })
		if props.Try != nil {
			props.Try(def, sample)
		}
	})
	retry := ui.UseEvent(func() {
		if props.Retry != nil {
			props.Retry()
		}
	})

	state := editor.Get()
	intro := t("intro")
	if props.Workspace {
		intro = t("intro_ws")
	}
	nodes := []ui.Node{html.P(html.Props{Class: "chatmod-intro", Text: intro})}
	if !props.CanManage {
		nodes = append(nodes, html.P(html.Props{Role: "alert", Text: t("no_permission")}))
		return html.Section(html.Props{Class: "chatfilter-panel chatmod", Dir: direction(m.Locale)}, nodes...)
	}
	status := t(props.Status)
	if props.Status != "" && props.StatusNote != "" {
		status += " " + t(props.StatusNote)
	}
	topStatus, formStatus := status, ""
	if state.Open {
		topStatus, formStatus = "", status
	}
	tone := ""
	if props.StatusError {
		tone = "error"
	}
	statusRole := "status"
	if props.StatusError {
		statusRole = "alert"
	}
	nodes = append(nodes, html.P(html.Props{ID: "modadmin-status", Class: "chatmod-status", Role: statusRole, Data: map[string]string{"tone": tone}, Text: topStatus}))
	delivered := len(props.Definitions) > 0 || len(props.Enablements) > 0
	if props.Loading && !delivered {
		nodes = append(nodes, ChatLoadingFrame(LoadingFrame{Locale: m.Locale, Shape: LoadingShapeSection, Rows: 3, Status: t("loading")}))
	}
	if props.LoadError != "" {
		message := t("load_failed")
		if delivered {
			message = t("stale")
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chatmod-section"},
			html.P(html.Props{Role: "alert", Class: "chatmod-error", Text: t(props.LoadError) + " " + message}),
			html.Button(html.Props{Type: "button", Text: t("retry"), OnClick: retry, Disabled: props.Retry == nil || props.Loading})))
	}
	lists := ModGroupFor(props.Definitions, channel, props.Workspace, m.Locale, m.IsTenantAdmin)
	handlers := modHandlers{toggle: toggle, action: action, reset: reset, edit: edit}
	var view []ui.Node
	if delivered {
		view = append(view, modBuiltinSection(m, props, lists, channel, now, handlers))
		view = append(view, modOwnSection(m, props, lists, channel, now, handlers))
		if !props.Workspace && len(lists.Admin) > 0 {
			view = append(view, modAdminSection(m, props, lists, channel, now))
		}
		view = append(view, html.Div(html.Props{Class: "chatmod-actions"}, html.Button(html.Props{Type: "button", Class: "chatmod-primary", Text: t("create"), OnClick: create, Disabled: props.Busy})))
		if props.Hits != nil {
			hits := *props.Hits
			hits.Model, hits.Workspace, hits.Now = m, props.Workspace, now
			view = append(view, ModHitsPanel(hits))
		}
	}
	nodes = append(nodes, html.Div(html.Props{Class: "chatmod-view", Hidden: state.Open}, view...))
	nodes = append(nodes, modEditor(m, props, state, choices, formStatus, tone, statusRole, modEditorHandlers{save: save, try: try, close: closeEditor, kind: kindChange, action: actionChange, detector: detectorChange, scope: scopeChange}))
	return html.Section(html.Props{Class: "chatfilter-panel chatmod", Dir: direction(m.Locale)}, nodes...)
}

// modDescribe is the one line that says what a filter does.
func modDescribe(m Model, d chatfilter.Definition) string {
	verb := modadminText(m, "v_"+d.Action)
	key := "o_" + d.Kind
	if d.Kind == "detector" && len(d.Match) > 0 {
		key = "o_" + d.Match[0]
	}
	object := modadminText(m, key)
	if verb == "" || object == "" {
		return ""
	}
	return modadminFormat(verb, "what", object)
}

// modScopeText says where a custom filter applies.
func modScopeText(m Model, d chatfilter.Definition) string {
	switch len(d.Channels) {
	case 0:
		return modadminText(m, "sc_ws")
	case 1:
		if name := modChannelName(m, d.Channels[0]); name != "" {
			return modadminText(m, "sc_one") + ": " + name
		}
		return modadminText(m, "sc_one")
	}
	return modadminFormat(modadminText(m, "sc_n"), "n", chatCount(m.Locale, len(d.Channels)))
}

var modLanguageNames = map[string]string{"en": "English", "de": "Deutsch", "ar": "العربية"}

func modLanguageName(code string) string {
	if name, ok := modLanguageNames[code]; ok {
		return name
	}
	return strings.ToUpper(code)
}

// modListName is the name a built-in list is shown under.
func modListName(m Model, d chatfilter.Definition) string {
	if name := modadminText(m, "list_"+d.Name); name != "" {
		return name
	}
	return d.Name
}

func modSwitchButton(m Model, on, disabled bool, ruleID, labelledBy, describedBy string, click ui.Handler) ui.Node {
	text := modadminText(m, "off")
	if on {
		text = modadminText(m, "on")
	}
	aria := map[string]string{"checked": boolString(on), "labelledby": labelledBy}
	if describedBy != "" {
		aria["describedby"] = describedBy
	}
	return html.Button(html.Props{Type: "button", Role: "switch", Class: "chatmod-switch", Disabled: disabled, OnClick: click,
		Aria: aria,
		Data: map[string]string{"action": "modadmin-switch", "id": ruleID}},
		html.Span(html.Props{Class: "chatmod-switch-track", Aria: map[string]string{"hidden": "true"}}),
		html.Span(html.Props{Class: "chatmod-switch-text", Text: text}))
}

func modBuiltinSection(m Model, props ModAdminProps, lists ModLists, channel string, now time.Time, h modHandlers) ui.Node {
	t := func(key string) string { return modadminText(m, key) }
	hint := t("b_hint") + " " + t("b_hint_ch")
	if props.Workspace {
		hint = t("b_hint") + " " + t("b_hint_ws")
	}
	section := []ui.Node{html.H4(html.Props{Text: t("b_title")}), html.P(html.Props{Class: "chatmod-hint", Text: hint})}
	row := 0
	for _, language := range lists.Languages {
		headingID := "modadmin-g-" + language
		var items []ui.Node
		for _, d := range lists.Builtin[language] {
			row++
			n := strconv.Itoa(row)
			state := ModResolve(d, props.Enablements, channel, now)
			name := modListName(m, d)
			more := []ui.Node{}
			if state.On {
				var options []ui.Node
				for _, key := range []string{"block", "mask", "flag"} {
					options = append(options, html.Option(html.Props{Value: key, Text: t("act_" + key), Selected: key == state.Action}))
				}
				selectID := "modadmin-act-" + modSafeID(d.ID)
				more = append(more, html.Div(html.Props{Class: "chatmod-field"},
					html.Label(html.Props{For: selectID, Text: t("what")}),
					html.Select(html.Props{ID: selectID, Key: "act-" + strconv.Itoa(props.Revision), Class: "chatmod-select", Disabled: props.Busy || props.Switch == nil, OnChange: h.action, Data: map[string]string{"action": "modadmin-action", "id": d.ID}}, options...)))
				if state.Action == "mask" {
					// One line of what readers will see (CHATUX-018).
					more = append(more, html.P(html.Props{Class: "chatmod-hint chatmod-example", Dir: "auto"}, RenderFilterMaskedText(m, t("ex_mask"))))
				}
			}
			if !props.Workspace && state.HasOverride && state.Differs {
				more = append(more, html.Button(html.Props{Type: "button", Class: "chatmod-link", Text: t("reset"), OnClick: h.reset, Disabled: props.Busy || props.Switch == nil,
					Aria: map[string]string{"label": t("reset") + ": " + name}, Data: map[string]string{"action": "modadmin-reset", "id": d.ID}}))
			}
			items = append(items, html.Li(html.Props{Class: "chatmod-row"},
				html.Div(html.Props{Class: "chatmod-row-main"},
					html.Span(html.Props{ID: "modadmin-n-" + n, Class: "chatmod-name", Dir: "auto", Text: name}),
					modSwitchButton(m, state.On, props.Busy || props.Switch == nil, d.ID, "modadmin-n-"+n+" "+headingID, "modadmin-s-"+n, h.toggle)),
				html.Span(html.Props{ID: "modadmin-s-" + n, Class: "chatmod-state", Text: t(ModStatusKey(state, props.Workspace))}),
				html.Div(html.Props{Class: "chatmod-row-more"}, more...)))
		}
		section = append(section, html.Div(html.Props{Class: "chatmod-group"},
			html.H5(html.Props{ID: headingID, Lang: language, Dir: direction(language), Text: modLanguageName(language)}),
			html.Ul(html.Props{Class: "chatmod-list"}, items...)))
	}
	return html.Section(html.Props{Class: "chatmod-section"}, section...)
}

// modTerms shows what a custom filter checks.
func modTerms(m Model, d chatfilter.Definition) ui.Node {
	list := func(terms []string) ui.Node {
		var items []ui.Node
		for _, term := range terms {
			items = append(items, html.Li(html.Props{Dir: "auto", Text: term}))
		}
		return html.Ul(html.Props{Class: "chatmod-terms"}, items...)
	}
	switch ModKindChoice(d) {
	case ModKindSensitive:
		return html.P(html.Props{Class: "chatmod-hint", Text: modadminText(m, "d_"+d.Match[0])})
	case ModKindLinks:
		if len(d.Match) > 1 {
			return list(d.Match[1:])
		}
		return html.P(html.Props{Class: "chatmod-hint", Text: modadminText(m, "kh_links")})
	}
	return list(d.Match)
}

func modOwnSection(m Model, props ModAdminProps, lists ModLists, channel string, now time.Time, h modHandlers) ui.Node {
	t := func(key string) string { return modadminText(m, key) }
	title, empty := t("c_title_ch"), t("c_empty_ch")
	if props.Workspace {
		title, empty = t("c_title_ws"), t("c_empty_ws")
	}
	section := []ui.Node{html.H4(html.Props{Text: title})}
	if len(lists.Own) == 0 {
		section = append(section, html.P(html.Props{Class: "chatmod-hint", Text: empty}))
	}
	var items []ui.Node
	for i, d := range lists.Own {
		n := "c" + strconv.Itoa(i+1)
		state := ModResolve(d, props.Enablements, channel, now)
		extra := []ui.Node{}
		if state.DryRun {
			extra = append(extra, html.Span(html.Props{ID: "modadmin-s-" + n, Class: "chatmod-state", Text: t("s_dry")}))
		}
		if props.Workspace {
			extra = append(extra, html.Span(html.Props{Class: "chatmod-state", Text: modScopeText(m, d)}))
		}
		if d.Hard {
			extra = append(extra, html.Span(html.Props{Class: "chatmod-state", Text: t("dm")}))
		}
		describedBy := ""
		if state.DryRun {
			describedBy = "modadmin-s-" + n
		}
		items = append(items, html.Li(html.Props{Class: "chatmod-row"},
			html.Div(html.Props{Class: "chatmod-row-main"},
				html.Span(html.Props{ID: "modadmin-n-" + n, Class: "chatmod-name", Dir: "auto", Text: d.Name}),
				modSwitchButton(m, state.On, props.Busy || props.Switch == nil, d.ID, "modadmin-n-"+n, describedBy, h.toggle),
				html.Button(html.Props{Type: "button", Class: "chatmod-link", Text: t("edit"), OnClick: h.edit, Disabled: props.Busy,
					Aria: map[string]string{"label": t("edit") + ": " + d.Name}, Data: map[string]string{"action": "modadmin-edit", "id": d.ID}})),
			html.P(html.Props{Class: "chatmod-desc", Text: modDescribe(m, d)}),
			html.Div(html.Props{Class: "chatmod-row-more"}, extra...),
			html.Details(html.Props{Class: "chatmod-details"}, html.Summary(html.Props{Text: t("see")}), modTerms(m, d))))
	}
	if len(items) > 0 {
		section = append(section, html.Ul(html.Props{Class: "chatmod-list"}, items...))
	}
	return html.Section(html.Props{Class: "chatmod-section"}, section...)
}

func modAdminSection(m Model, props ModAdminProps, lists ModLists, channel string, now time.Time) ui.Node {
	t := func(key string) string { return modadminText(m, key) }
	hint := t("a_hint")
	if m.IsTenantAdmin {
		hint = t("a_hint_admin")
	}
	var items []ui.Node
	for i, d := range lists.Admin {
		n := "a" + strconv.Itoa(i+1)
		state := ModResolve(d, props.Enablements, channel, now)
		stateText := t("off")
		if state.DryRun {
			stateText = t("s_dry")
		} else if state.On {
			stateText = t("on")
		}
		extra := []ui.Node{}
		if d.Hard {
			extra = append(extra, html.Span(html.Props{Class: "chatmod-state", Text: t("dm")}))
		}
		items = append(items, html.Li(html.Props{Class: "chatmod-row"},
			html.Div(html.Props{Class: "chatmod-row-main"},
				html.Span(html.Props{ID: "modadmin-n-" + n, Class: "chatmod-name", Dir: "auto", Text: d.Name}),
				html.Span(html.Props{Class: "chatmod-switch-text", Text: stateText})),
			html.P(html.Props{Class: "chatmod-desc", Text: modDescribe(m, d)}),
			html.Div(html.Props{Class: "chatmod-row-more"}, extra...)))
	}
	return html.Section(html.Props{Class: "chatmod-section"}, html.H4(html.Props{Text: t("a_title")}), html.P(html.Props{Class: "chatmod-hint", Text: hint}), html.Ul(html.Props{Class: "chatmod-list"}, items...))
}

type modEditorHandlers struct{ save, try, close, kind, action, detector, scope ui.Handler }

func modFieldError(m Model, state ModEditorState, field string) ui.Node {
	if key := state.Errors[field]; key != "" {
		return html.P(html.Props{ID: "modadmin-e-" + field, Class: "chatmod-error", Role: "alert", Text: modadminText(m, key)})
	}
	return nil
}

// modFieldAria ties a field to its hint and, when it has one, to its error.
func modFieldAria(state ModEditorState, field, hint string) map[string]string {
	described := hint
	aria := map[string]string{}
	if state.Errors[field] != "" {
		described = strings.TrimSpace(described + " modadmin-e-" + field)
		aria["invalid"] = "true"
	}
	if described != "" {
		aria["describedby"] = described
	}
	return aria
}

func modEditor(m Model, props ModAdminProps, state ModEditorState, choices []Conversation, formStatus, tone, statusRole string, h modEditorHandlers) ui.Node {
	t := func(key string) string { return modadminText(m, key) }
	options := func(keys []string, prefix, selected string) []ui.Node {
		var out []ui.Node
		for _, key := range keys {
			out = append(out, html.Option(html.Props{Value: key, Text: t(prefix + key), Selected: key == selected}))
		}
		return out
	}
	kind := state.Kind
	if !modContains(ModKinds, kind) {
		kind = ModKindWords
	}
	action := state.Action
	if !modContains(ModActions, action) {
		action = "block"
	}
	title := t("create")
	if state.EditID != "" {
		title = t("e_title_edit")
	}
	matchShown := kind == ModKindWords || kind == ModKindPattern || kind == ModKindAttachment
	matchKey := kind
	if !matchShown {
		matchKey = ModKindWords
	}
	var where []ui.Node
	whereLabel := html.Span(html.Props{Class: "chatmod-label", Text: t("e_where")})
	switch {
	case !props.Workspace:
		where = []ui.Node{html.Span(html.Props{Class: "chatmod-name", Text: t("w_channel")})}
	case state.EditID != "":
		where = []ui.Node{html.P(html.Props{Class: "chatmod-hint", Text: t("w_locked")})}
	default:
		whereLabel = html.Label(html.Props{Class: "chatmod-label", For: "modadmin-scope", Text: t("e_where")})
		var boxes []ui.Node
		for i, c := range choices {
			id := "modadmin-ch-" + strconv.Itoa(i)
			boxes = append(boxes, html.Label(html.Props{For: id}, html.Input(html.Props{ID: id, Type: "checkbox"}), html.Span(html.Props{Dir: "auto", Text: c.Name})))
		}
		if len(choices) == 0 {
			boxes = append(boxes, html.P(html.Props{Class: "chatmod-hint", Text: t("w_none")}))
		}
		where = []ui.Node{
			html.Select(html.Props{ID: "modadmin-scope", Class: "chatmod-select", OnChange: h.scope, Aria: modFieldAria(state, "scope", "")},
				html.Option(html.Props{Value: "workspace", Text: t("w_ws"), Selected: state.Scope != "chosen"}),
				html.Option(html.Props{Value: "chosen", Text: t("w_chosen"), Selected: state.Scope == "chosen"})),
			html.Fieldset(html.Props{Hidden: state.Scope != "chosen"}, html.Legend(html.Props{Class: "chatmod-hint", Text: t("w_pick")}), html.Div(html.Props{Class: "chatmod-channels"}, boxes...)),
			modFieldError(m, state, "scope"),
		}
	}
	hardLocked := state.EditID != "" && state.EditHard
	var hard ui.Node
	if m.IsTenantAdmin {
		hint := t("e_hard_hint")
		if hardLocked {
			hint = t("e_hard_locked")
		}
		hard = html.Div(html.Props{Class: "chatmod-field"},
			html.Label(html.Props{Class: "chatfilter-check", For: "modadmin-hard"}, html.Input(html.Props{ID: "modadmin-hard", Type: "checkbox", Disabled: hardLocked, Aria: map[string]string{"describedby": "modadmin-h-hard"}}), html.Span(html.Props{Text: t("e_hard")})),
			html.P(html.Props{ID: "modadmin-h-hard", Class: "chatmod-hint", Text: hint}))
	}
	tryDisabled := props.Try == nil || props.Trying || props.Busy
	return html.Form(html.Props{Class: "chatmod-editor", Hidden: !state.Open, OnSubmit: h.save, Aria: map[string]string{"label": title}},
		html.H4(html.Props{Text: title}),
		html.Button(html.Props{Type: "button", Class: "chatmod-link", Text: t("e_back"), OnClick: h.close}),
		html.Div(html.Props{Class: "chatmod-field"},
			html.Label(html.Props{For: "modadmin-name", Text: t("e_name")}),
			html.Input(html.Props{ID: "modadmin-name", Name: "name", Type: "text", AutoComplete: "off", Placeholder: t("e_name_ph"), Aria: modFieldAria(state, "name", "modadmin-h-name")}),
			html.P(html.Props{ID: "modadmin-h-name", Class: "chatmod-hint", Text: t("e_name_hint")}),
			modFieldError(m, state, "name")),
		html.Div(html.Props{Class: "chatmod-field"},
			html.Label(html.Props{For: "modadmin-kind", Text: t("e_kind")}),
			html.Select(html.Props{ID: "modadmin-kind", Name: "kind", OnChange: h.kind, Aria: map[string]string{"describedby": "modadmin-h-kind"}}, options(ModKinds, "k_", kind)...),
			html.P(html.Props{ID: "modadmin-h-kind", Class: "chatmod-hint", Text: t("kh_" + kind)})),
		html.Div(html.Props{Class: "chatmod-field", Hidden: !matchShown},
			html.Label(html.Props{For: "modadmin-match", Text: t("ml_" + matchKey)}),
			html.Textarea(html.Props{ID: "modadmin-match", Name: "match", Rows: 4, Placeholder: t("ph_" + matchKey), Aria: modFieldAria(state, "match", "modadmin-h-kind")}),
			modFieldError(m, state, "match")),
		html.Div(html.Props{Class: "chatmod-field", Hidden: kind != ModKindSensitive},
			html.Label(html.Props{For: "modadmin-detector", Text: t("ml_sensitive")}),
			html.Select(html.Props{ID: "modadmin-detector", Name: "detector", OnChange: h.detector, Aria: modFieldAria(state, "detector", "modadmin-h-kind")}, options(ModDetectors, "d_", state.Detector)...),
			modFieldError(m, state, "detector")),
		html.Div(html.Props{Class: "chatmod-field", Hidden: kind != ModKindLinks},
			html.Label(html.Props{For: "modadmin-domains", Text: t("ml_links")}),
			html.Textarea(html.Props{ID: "modadmin-domains", Name: "domains", Rows: 3, Placeholder: t("ph_links"), Aria: modFieldAria(state, "domains", "modadmin-h-kind")}),
			modFieldError(m, state, "domains")),
		html.Div(html.Props{Class: "chatmod-field"},
			html.Label(html.Props{For: "modadmin-action", Text: t("e_action")}),
			html.Select(html.Props{ID: "modadmin-action", Name: "action", OnChange: h.action, Aria: map[string]string{"describedby": "modadmin-h-action"}}, options(ModActions, "act_", action)...),
			html.P(html.Props{ID: "modadmin-h-action", Class: "chatmod-hint", Text: t("ah_" + action)})),
		html.Div(html.Props{Class: "chatmod-field", Hidden: action != "notify"},
			html.Label(html.Props{For: "modadmin-target", Text: t("e_target")}),
			html.Input(html.Props{ID: "modadmin-target", Name: "target", Type: "text", AutoComplete: "off", Placeholder: t("e_target_ph"), Aria: modFieldAria(state, "target", "")}),
			modFieldError(m, state, "target")),
		html.Div(html.Props{Class: "chatmod-field"}, append([]ui.Node{whereLabel}, where...)...),
		html.Details(html.Props{Class: "chatmod-details"},
			html.Summary(html.Props{Text: t("e_exempt")}),
			html.P(html.Props{Class: "chatmod-hint", Text: t("ex_hint")}),
			html.Div(html.Props{Class: "chatmod-field"}, html.Label(html.Props{For: "modadmin-roles", Text: t("ex_roles")}), html.Textarea(html.Props{ID: "modadmin-roles", Name: "roles", Rows: 2})),
			html.Div(html.Props{Class: "chatmod-field"}, html.Label(html.Props{For: "modadmin-agents", Text: t("ex_agents")}), html.Textarea(html.Props{ID: "modadmin-agents", Name: "agents", Rows: 2}))),
		html.Div(html.Props{Class: "chatmod-field"},
			html.Label(html.Props{Class: "chatfilter-check", For: "modadmin-dry"}, html.Input(html.Props{ID: "modadmin-dry", Type: "checkbox", Aria: map[string]string{"describedby": "modadmin-h-dry"}}), html.Span(html.Props{Text: t("e_dry")})),
			html.P(html.Props{ID: "modadmin-h-dry", Class: "chatmod-hint", Text: t("e_dry_hint")})),
		hard,
		modTryBox(m, props, state, kind, tryDisabled, h.try),
		html.P(html.Props{ID: "modadmin-form-status", Class: "chatmod-status", Role: statusRole, Data: map[string]string{"tone": tone}, Text: formStatus}),
		html.P(html.Props{Class: "chatmod-hint", Text: t("e_save_hint")}),
		html.Div(html.Props{Class: "chatmod-actions"},
			html.Button(html.Props{Type: "submit", Class: "chatmod-primary", Text: t("e_save"), Disabled: props.Save == nil || props.Busy}),
			html.Button(html.Props{Type: "button", Text: t("e_cancel"), OnClick: h.close})))
}

// modTryBox is the "Try a message" box: a sample, a button, and what the sample
// would meet, in one plain sentence.
func modTryBox(m Model, props ModAdminProps, state ModEditorState, kind string, disabled bool, try ui.Handler) ui.Node {
	t := func(key string) string { return modadminText(m, key) }
	label := t("try_btn")
	if props.Trying {
		label = t("try_busy")
	}
	var outcome []ui.Node
	switch {
	case !state.Tried || props.Trying:
	case props.TryError != "":
		outcome = append(outcome, html.P(html.Props{Class: "chatmod-error", Text: t(props.TryError) + " " + t("sf_try")}))
	case props.Result != nil:
		outcome = modOutcomeNodes(m, ModOutcomeOf(*props.Result, props.TrySample), state.TriedDry)
	}
	return html.Section(html.Props{Class: "chatmod-try", Aria: map[string]string{"labelledby": "modadmin-try-title"}},
		html.H5(html.Props{ID: "modadmin-try-title", Text: t("try_title")}),
		html.P(html.Props{Class: "chatmod-hint", Text: t("try_hint")}),
		html.P(html.Props{Class: "chatmod-hint", Hidden: kind != ModKindAttachment, Text: t("try_att")}),
		html.Div(html.Props{Class: "chatmod-field", Hidden: kind == ModKindAttachment},
			html.Label(html.Props{For: "modadmin-sample", Text: t("try_label")}),
			html.Textarea(html.Props{ID: "modadmin-sample", Name: "sample", Rows: 3, Aria: modFieldAria(state, "sample", "")}),
			modFieldError(m, state, "sample"),
			html.Div(html.Props{Class: "chatmod-actions"}, html.Button(html.Props{Type: "button", Text: label, OnClick: try, Disabled: disabled, Aria: map[string]string{"busy": boolString(props.Trying)}}))),
		html.Div(html.Props{ID: "modadmin-outcome", Role: "status", Aria: map[string]string{"live": "polite"}}, outcome...))
}

// modOutcomeNodes writes what a tried message would meet.
func modOutcomeNodes(m Model, outcome ModOutcome, dry bool) []ui.Node {
	t := func(key string) string { return modadminText(m, key) }
	var nodes []ui.Node
	switch outcome.Kind {
	case "block":
		text := modadminFormat(t("o_block"), "term", outcome.Term, "rule", outcome.Rule)
		if outcome.Term == "" {
			text = modadminFormat(t("o_block_plain"), "rule", outcome.Rule)
		}
		nodes = append(nodes, html.P(html.Props{Class: "chatmod-outcome", Dir: "auto", Text: text}))
	case "mask":
		nodes = append(nodes, html.P(html.Props{Class: "chatmod-outcome"}, ui.Text(t("o_mask")+" "), RenderFilterMaskedText(m, ModExcerpt(outcome.Masked))))
	case "flag":
		nodes = append(nodes, html.P(html.Props{Class: "chatmod-outcome", Text: t("o_flag")}))
	case "notify":
		nodes = append(nodes, html.P(html.Props{Class: "chatmod-outcome", Dir: "auto", Text: modadminFormat(t("o_notify"), "target", outcome.Target)}))
	default:
		nodes = append(nodes, html.P(html.Props{Class: "chatmod-outcome", Text: t("o_none")}))
	}
	if dry && outcome.Kind != "none" {
		nodes = append(nodes, html.P(html.Props{Class: "chatmod-hint", Text: t("o_dry")}))
	}
	return nodes
}
