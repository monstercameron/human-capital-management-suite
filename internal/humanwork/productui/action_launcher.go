package productui

import (
	"fmt"
	"sort"
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const actionLauncherLimit = 10

// actionLauncherStylesheet builds the launcher styles from typed css rules.
// Raw covers only what has no typed constructor (var() fallbacks, logical
// inset properties, system colors, text alignment); everything else is typed.
func actionLauncherStylesheet() string {
	return buildTypedSheet(declareActionLauncherStyles)
}

func declareActionLauncherStyles() {
	declareGlobal(".action-launcher", gwccss.Position.Relative, gwccss.MinWidth(gwccss.Px(0)))
	declareGlobal(".action-launcher-trigger",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(7)),
		gwccss.MinHeight(gwccss.Px(42)), gwccss.MaxWidth(gwccss.Px(230)),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.78)), gwccss.Raw("font-weight", "700"), gwccss.Raw("text-align", "start"),
		hoverRule(
			gwccss.Raw("border-color", "var(--hcm-hover-border,var(--accent))"),
			gwccss.Raw("background", "var(--surface-hover,var(--soft))"),
			gwccss.TextColor(gwccss.Var("accent")),
		),
	)
	declareGlobal(".action-launcher-trigger .nav-icon", gwccss.Raw("flex", "none"))
	declareGlobal(".action-launcher-dialog",
		gwccss.Position.Absolute, gwccss.ZIndex(30),
		gwccss.Raw("inset-block-start", "48px"), gwccss.Raw("inset-inline-end", "0"),
		gwccss.W(gwccss.MinLen(gwccss.Px(360), gwccss.RawLength("calc(100vw - 28px)"))),
		gwccss.MaxHeight(gwccss.MinLen(gwccss.Vh(70), gwccss.Px(560))),
		gwccss.Raw("overflow", "auto"),
		gwccss.Padding(gwccss.Px(16)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Px(0), gwccss.Px(18), gwccss.Px(48), gwccss.Zero, gwccss.Hex("10223822"))),
	)
	declareGlobal(".action-launcher-dialog-hidden", gwccss.Display.None)
	declareGlobal(".action-launcher-head",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("margin-bottom", "12px"),
	)
	declareGlobal(".action-launcher-input",
		gwccss.MinHeight(gwccss.Px(42)),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.85)),
	)
	declareGlobal(".action-launcher-result",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(10)),
		gwccss.MinHeight(gwccss.Px(48)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Raw("border", "1px solid var(--line)"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("text-decoration", "none"),
		hoverRule(
			gwccss.Raw("border-color", "var(--accent)"),
			gwccss.Raw("background", "var(--surface-hover,var(--soft))"),
		),
	)
	declareGlobal(".action-launcher-result.active",
		gwccss.Raw("border-color", "var(--accent)"),
		gwccss.Raw("background", "var(--surface-hover,var(--soft))"),
	)
	declareGlobal(".action-launcher-result-unavailable",
		gwccss.Raw("cursor", "default"), gwccss.Raw("opacity", "0.72"),
	)
	declareGlobal(".action-launcher-result-unavailable:hover",
		gwccss.Raw("border-color", "var(--line)"), gwccss.Raw("background", "var(--surface)"),
	)
	declareGlobal(".action-launcher-result small",
		gwccss.Display.Block,
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.72)),
	)
	declareGlobal(".action-launcher-dialog",
		mediaRule(gwccss.MaxW(760),
			gwccss.Raw("inset-inline-start", "0"), gwccss.Raw("inset-inline-end", "auto"),
		),
	)
	declareGlobal(".action-launcher-trigger",
		mediaRule(gwccss.MaxW(760), gwccss.MaxWidth(gwccss.Px(190))),
		mediaRule(gwccss.MaxW(430), gwccss.MaxWidth(gwccss.RawLength("100%"))),
	)
	declareGlobal(".action-launcher-result",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("transition", "none")),
	)
	declareGlobal(".action-launcher-trigger",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("border-color", "CanvasText"), gwccss.Raw("background", "Canvas"), gwccss.Raw("color", "CanvasText"),
		),
	)
	declareGlobal(".action-launcher-dialog",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("border-color", "CanvasText"), gwccss.Raw("background", "Canvas"), gwccss.Raw("color", "CanvasText"),
		),
	)
	declareGlobal(".action-launcher-result",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("border-color", "CanvasText"), gwccss.Raw("background", "Canvas"), gwccss.Raw("color", "CanvasText"),
		),
	)
	declareGlobal(".action-launcher-result small",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("color", "GrayText")),
	)
	declareGlobal(".action-launcher",
		mediaRule(gwccss.RawMedia("print"), gwccss.MarkImportant(gwccss.Display.None)),
	)
}

// ActionLauncherItem is one entry the shell launcher may offer: either a
// ranked, viewer-specific authorized action (IsNavigationDestination
// false) or a plain "go to this page" destination (true). An action item
// with a blank Href is not launchable -- Reason explains why, without
// disclosing anything the viewer is not entitled to, and the launcher
// renders it as a described, disabled row rather than a link. Href and
// Reason are never both non-blank. The launcher never executes an
// intent, decision, or approval from a row: a non-blank Href is always a
// navigation, never an RPC.
type ActionLauncherItem struct {
	Page        PageID
	ID          string
	Label       string
	Description string
	Href        string
	Icon        string
	Keywords    []string
	// Reason explains a blank-Href item's unavailability. See the type
	// doc: this is the PROMOUX-001-style disclosure-safe text, never the
	// raw underlying fact.
	Reason string
	// IsNavigationDestination marks a plain page destination rather than
	// a ranked action on a specific authorized record.
	IsNavigationDestination bool
}

// ActionLauncherProps keeps the shell launcher independently composable and
// easy to exercise without passing the page-wide View into the component.
type ActionLauncherProps struct {
	I18nProps
	Items        []ActionLauncherItem
	Navigate     func(string)
	InitialQuery string
}

// actionLauncherProps derives the shell launcher's items from the same
// registries the People directory already uses for its own row actions
// (personActionLauncherItems, backed by personWorkflowActions in
// page_people.go) plus the plain page destinations a viewer with no
// resolved action can still reach. It keeps no page-specific action
// inventory of its own.
func actionLauncherProps(view View) ActionLauncherProps {
	items := append(personActionLauncherItems(view), navigationLauncherItems(view)...)
	return ActionLauncherProps{
		I18nProps: I18nProps{Locale: view.Locale}, Items: items, Navigate: view.Navigate,
	}
}

// personActionLauncherItems ranks one authorized action per person per
// launchable workflow from the shared PersonWorkflow catalogue, reusing
// personWorkflowActions exactly as the People directory rows do.
//
// A viewer with no PageJourneys create authority at all never sees a
// launchable (Href != "") item -- authorization is checked before any
// per-person business state, exactly as ResolvePromotionAvailability
// checks it first (promotion_availability.go). Such a viewer still sees
// one explained, disabled item per named worker, but every one of them
// carries the identical PromotionWithheld text: the People directory
// already shows this same generic reason per row for an unauthorized
// viewer (peopleRowProps), so the launcher does too rather than
// resurfacing "no reason" or, worse, staying silent about a control the
// viewer can see exists. A workforce action is not a secret; whether a
// specific worker's is currently exercisable can be, and that is exactly
// what stays collapsed to one indistinguishable reason here.
func personActionLauncherItems(view View) []ActionLauncherItem {
	catalog := rankedPersonWorkflows(view.PersonWorkflows, view.WorkflowUses)
	if len(catalog) == 0 {
		return nil
	}
	workflows := catalog
	authorized := view.Allows(PageJourneys, "create")
	if !authorized {
		workflows = nil
	}
	// admittedPeople, not view.People directly: a discovery-denied
	// record (WEB-067/WEB-072) must not resurface here just because this
	// control builds its own item list instead of reading page rows.
	population := admittedPeople(view)
	items := make([]ActionLauncherItem, 0, len(population))
	for _, person := range population {
		actions, reason, reasonWorkflow := personWorkflowActions(view, person, workflows)
		for index, action := range actions {
			// AccessibleLabel already carries the person-specific phrasing
			// ("Start {workflow} for {name}"); the launcher is a ranked
			// list of actions on named records, not a bare workflow menu,
			// so it is the label here too, not a footnote.
			label := action.AccessibleLabel
			if label == "" {
				label = action.Label
			}
			items = append(items, ActionLauncherItem{
				Page: PageJourneys, ID: fmt.Sprintf("action:%s:%d", person.ID, index),
				Label: label, Description: person.Role, Href: action.Href,
				Keywords: []string{person.Name, person.Team, person.Location, person.WorkerNumber, person.Role},
			})
		}
		if !authorized {
			reason, reasonWorkflow = PromotionAvailabilityReason(view.Locale, PromotionWithheld), catalog[0].Name
		}
		if reason != "" {
			items = append(items, ActionLauncherItem{
				Page: PageJourneys, ID: "action-unavailable:" + person.ID,
				Label:       view.Locale.Text("people.workflow_aria", map[string]string{"workflow": reasonWorkflow, "name": person.Name}),
				Description: reason, Reason: reason,
				Keywords: []string{person.Name, person.Team, person.Location, person.WorkerNumber, person.Role},
			})
		}
	}
	return items
}

// navigationLauncherItems is the plain "go to this page" fallback: the
// two shell destinations most directly related to starting and finding
// promotion work, offered whenever the viewer is admitted to them,
// regardless of whether any ranked action also exists.
func navigationLauncherItems(view View) []ActionLauncherItem {
	items := make([]ActionLauncherItem, 0, 2)
	if view.Allows(PageJourneys, "create") {
		if definition, ok := LookupPage(PageJourneys); ok {
			items = append(items, ActionLauncherItem{
				Page: PageJourneys,
				ID:   "start:journeys", Label: view.Locale.Text(definition.LabelKey), Description: view.Locale.Text(definition.SubtitleKey),
				Href: statefulHref(view, PageJourneys), Icon: definition.Icon,
				Keywords:                append(append([]string{"start", "new", "create"}, definition.SearchTerms...), view.Locale.Text(definition.TitleKey)),
				IsNavigationDestination: true,
			})
		}
	}
	if view.Allows(PagePeople, "view") {
		if definition, ok := LookupPage(PagePeople); ok {
			items = append(items, ActionLauncherItem{
				Page: PagePeople,
				ID:   "start:people", Label: view.Locale.Text(definition.LabelKey), Description: view.Locale.Text(definition.SubtitleKey),
				Href: statefulHref(view, PagePeople), Icon: definition.Icon,
				Keywords:                append(append([]string{"find", "select", "choose"}, definition.SearchTerms...), view.Locale.Text(definition.TitleKey)),
				IsNavigationDestination: true,
			})
		}
	}
	return items
}

// actionLauncherIsNavigationOnly reports whether every item is a plain
// destination. It is derived from Items rather than carried as a
// separate prop: recomputing it from whatever Items actually rendered
// means a caller can never pass a stale or forgotten flag that disagrees
// with what the dialog shows. Vacuously true for an empty list: with
// nothing resolved at all, the control must not claim action framing
// either.
func actionLauncherIsNavigationOnly(items []ActionLauncherItem) bool {
	for _, item := range items {
		if !item.IsNavigationDestination {
			return false
		}
	}
	return true
}

// RankActionLauncherItems performs deterministic typo-tolerant ranking over
// the local authorized starts. It never contacts a server and never reveals
// records outside the already-resolved projection.
func RankActionLauncherItems(items []ActionLauncherItem, query string, limit int) []ActionLauncherItem {
	if limit <= 0 {
		return nil
	}
	tokens := strings.Fields(normalizeNavigationSearch(query))
	if len(tokens) == 0 {
		results := make([]ActionLauncherItem, 0, minInt(limit, len(items)))
		for _, item := range items {
			results = append(results, item)
			if len(results) == limit {
				break
			}
		}
		return results
	}
	type scored struct {
		item  ActionLauncherItem
		score int
	}
	scoredItems := make([]scored, 0, len(items))
	for _, item := range items {
		if score := actionLauncherScore(item, tokens); score > 0 {
			scoredItems = append(scoredItems, scored{item: item, score: score})
		}
	}
	sort.SliceStable(scoredItems, func(left, right int) bool {
		if scoredItems[left].score != scoredItems[right].score {
			return scoredItems[left].score > scoredItems[right].score
		}
		return strings.ToLower(scoredItems[left].item.Label) < strings.ToLower(scoredItems[right].item.Label)
	})
	results := make([]ActionLauncherItem, 0, minInt(limit, len(scoredItems)))
	for _, candidate := range scoredItems {
		results = append(results, candidate.item)
		if len(results) == limit {
			break
		}
	}
	return results
}

func actionLauncherScore(item ActionLauncherItem, tokens []string) int {
	fields := []struct {
		value  string
		weight int
	}{
		{item.Label, 48}, {item.Description, 16},
	}
	for _, keyword := range item.Keywords {
		fields = append(fields, struct {
			value  string
			weight int
		}{keyword, 30})
	}
	total := 0
	for _, token := range tokens {
		best := 0
		for _, field := range fields {
			if score := fuzzyFieldScore(field.value, token); score > 0 && score+field.weight > best {
				best = score + field.weight
			}
		}
		if best == 0 {
			return 0
		}
		total += best
	}
	return total
}

// ActionLauncher is the shell "Start an action" control: a trigger button
// opening a non-modal dialog that filters the authorized starts locally.
func ActionLauncher(props ActionLauncherProps) ui.Node {
	query := ui.UseState(props.InitialQuery)
	open := ui.UseState(strings.TrimSpace(props.InitialQuery) != "")
	active := ui.UseState(0)
	usePopoverFocusDismissal("action-launcher", "action-launcher-trigger", open.Get(), func() { open.Set(false) })
	results := RankActionLauncherItems(props.Items, query.Get(), actionLauncherLimit)
	activeIndex := active.Get()
	if activeIndex >= len(results) && len(results) > 0 {
		activeIndex = len(results) - 1
	}

	// GREEN's last clause: a control offering only page destinations must
	// not claim it starts actions it cannot offer. Derived from Items
	// itself (see actionLauncherIsNavigationOnly) so the label can never
	// disagree with what the dialog actually lists.
	navigationOnly := actionLauncherIsNavigationOnly(props.Items)
	triggerKey := "action_launcher.trigger"
	if navigationOnly {
		triggerKey = "action_launcher.navigate_trigger"
	}
	triggerLabel := props.Text(triggerKey)

	navigate := func(item ActionLauncherItem) {
		if item.Href == "" {
			// An explained-unavailable row: nothing to navigate to, and
			// nothing here executes an intent from a row.
			return
		}
		query.Set("")
		open.Set(false)
		active.Set(0)
		if props.Navigate != nil {
			props.Navigate(item.Href)
		}
	}
	trigger := html.Button(html.Props{
		ID: "action-launcher-trigger", Class: "action-launcher-trigger", Type: "button",
		Aria: map[string]string{
			"label": triggerLabel, "haspopup": "dialog",
			"expanded": fmt.Sprint(open.Get()), "controls": "action-launcher-dialog",
		},
		OnClick: ui.UseEvent(func(ui.MouseEvent) {
			if open.Get() {
				open.Set(false)
				return
			}
			open.Set(true)
			active.Set(0)
		}),
	}, navIcon("actions"), html.Span(html.Props{Class: "action-launcher-label"}, ui.Text(triggerLabel)))

	// The dialog scaffold (title, input) stays in the document while
	// closed so its accessible name resolves without client state; hidden
	// keeps it out of the accessibility tree and layout until the trigger
	// opens it. The results list is different: it can name every ranked
	// worker this viewer is authorized to act on, so -- like GlobalSearch
	// -- it renders only once open is actually true, never unconditionally
	// into every page's markup regardless of whether anyone opened it.
	dialogHidden := !open.Get()
	inputAria := map[string]string{
		"label": props.Text("action_launcher.filter_label"), "autocomplete": "list",
		"expanded": fmt.Sprint(open.Get() && len(results) > 0),
	}
	if open.Get() && len(results) > 0 {
		inputAria["controls"] = "action-launcher-results"
		inputAria["activedescendant"] = "action-launcher-result-" + fmt.Sprint(activeIndex)
	}
	dialogTitleKey := "action_launcher.dialog_title"
	if navigationOnly {
		dialogTitleKey = "action_launcher.navigate_trigger"
	}
	dialogTitle := props.Text(dialogTitleKey)
	dialogProps := html.Props{
		ID: "action-launcher-dialog", Class: "action-launcher-dialog",
		Raw: map[string]any{"role": "dialog", "aria-label": dialogTitle},
	}
	if dialogHidden {
		dialogProps.Class += " action-launcher-dialog-hidden"
		dialogProps.Raw["hidden"] = "hidden"
		dialogProps.Raw["aria-hidden"] = "true"
	}
	dialogChildren := []ui.Node{
		html.Div(html.Props{Class: "action-launcher-head"},
			html.Strong(html.Props{}, ui.Text(dialogTitle)),
			html.Label(html.Props{Class: "sr-only", For: "action-launcher-input"}, ui.Text(props.Text("action_launcher.filter_label"))),
			html.Tag("input", html.Props{
				ID: "action-launcher-input", Name: "action", Value: query.Get(), Class: "action-launcher-input", Aria: inputAria,
				Raw: map[string]any{"type": "search", "role": "combobox", "placeholder": props.Text("action_launcher.filter_placeholder"), "autocomplete": "off", "spellcheck": "false"},
				OnInput: ui.UseEvent(func(event ui.InputEvent) {
					query.Set(event.GetValue())
					active.Set(0)
				}),
				OnKeyDown: ui.UseEvent(func(event ui.KeyboardEvent) {
					switch {
					case drawerEscapeCloses(event.GetKey()):
						// The shared predicate every shell dialog (search,
						// launcher, drawer) uses, so Escape cannot drift key
						// by key between them (landmarks.go).
						open.Set(false)
					case event.GetKey() == "ArrowDown":
						if len(results) > 0 {
							event.PreventDefault()
							active.Set((active.Get() + 1) % len(results))
						}
					case event.GetKey() == "ArrowUp":
						if len(results) > 0 {
							event.PreventDefault()
							active.Set((active.Get() - 1 + len(results)) % len(results))
						}
					case event.GetKey() == "Enter":
						if len(results) > 0 && results[activeIndex].Href != "" {
							event.PreventDefault()
							navigate(results[activeIndex])
						}
					}
				}),
			}),
		),
	}
	if open.Get() {
		dialogChildren = append(dialogChildren, actionLauncherResults(props, results, activeIndex, navigate))
	}
	// UIPOLISH-004 "overlay": this was the fourth ad hoc scroll region RED
	// names by hand -- overflow:auto on both axes, no scrollbar tokens.
	// ScrollRegion's shared stylesheet (scroll_region.go) narrows it to
	// vertical-only and tokenizes its scrollbar; the dialog keeps its own
	// role/aria-label/hidden wiring in Raw exactly as before.
	dialog := ui.CreateElement(ScrollRegion, ScrollRegionProps{
		ID: dialogProps.ID, Class: dialogProps.Class, Raw: dialogProps.Raw, Children: dialogChildren,
	})
	class := "action-launcher"
	if !dialogHidden {
		class += " action-launcher-open"
	}
	return html.Div(html.Props{ID: "action-launcher", Class: class}, trigger, dialog)
}

func actionLauncherResults(props ActionLauncherProps, results []ActionLauncherItem, active int, navigate func(ActionLauncherItem)) ui.Node {
	if len(results) == 0 {
		title, description := props.Text("action_launcher.empty_title"), props.Text("action_launcher.empty_description")
		if len(props.Items) > 0 {
			title, description = props.Text("action_launcher.no_matches_title"), props.Text("action_launcher.no_matches_description")
		}
		// This is a status message, not an expanded listbox with missing options.
		return html.Div(html.Props{ID: "action-launcher-empty", Raw: map[string]any{"role": "status"}},
			unavailablePanel(title, description))
	}
	children := make([]ui.Node, 0, len(results))
	for index, result := range results {
		item := result
		class := "action-launcher-result"
		selected := index == active
		if selected {
			class += " active"
		}
		if item.Href == "" {
			// An authorized-in-general but currently-blocked action:
			// explained, never a link -- there is nothing for a click or
			// Enter to execute, and no-disclosure means the reason is the
			// only fact this row carries about why.
			descID := "action-launcher-result-" + fmt.Sprint(index) + "-reason"
			children = append(children, html.Div(html.Props{
				ID: "action-launcher-result-" + fmt.Sprint(index), Class: class + " action-launcher-result-unavailable",
				Raw: map[string]any{"role": "option", "aria-selected": fmt.Sprint(selected), "aria-disabled": "true", "aria-describedby": descID},
			},
				navIcon(item.Icon),
				html.Span(html.Props{Class: "action-launcher-copy"},
					html.Strong(html.Props{}, ui.Text(item.Label)),
					html.Small(html.Props{ID: descID}, ui.Text(item.Reason)),
				),
			))
			continue
		}
		children = append(children, softwareLink(func(href string) { navigate(item) }, html.Props{
			ID: "action-launcher-result-" + fmt.Sprint(index), Class: class,
			Raw: map[string]any{"role": "option", "aria-selected": fmt.Sprint(selected)},
		}, item.Href,
			navIcon(item.Icon),
			html.Span(html.Props{Class: "action-launcher-copy"},
				html.Strong(html.Props{}, ui.Text(item.Label)),
				html.Small(html.Props{}, ui.Text(item.Description)),
			),
		))
	}
	listboxLabelKey := "action_launcher.dialog_title"
	if actionLauncherIsNavigationOnly(props.Items) {
		listboxLabelKey = "action_launcher.navigate_trigger"
	}
	return ui.CreateElement(PopoverSurface, PopoverSurfaceProps{
		ID: "action-launcher-results", Class: "action-launcher-panel",
		Raw: map[string]any{"role": "listbox", "aria-label": props.Text(listboxLabelKey)}, Children: children,
	})
}
