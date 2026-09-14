package productui

import (
	"fmt"
	"sort"
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const (
	actionLauncherLimit        = 10
	actionLauncherInitialLimit = 5
)

type ActionLauncherItemKind string

const (
	ActionLauncherAction      ActionLauncherItemKind = "action"
	ActionLauncherDestination ActionLauncherItemKind = "destination"
)

const (
	// SemanticActionPromoteWorker is the stable server/browser identifier for
	// the governed promotion start exposed through the global launcher.
	SemanticActionPromoteWorker = "promote-worker"
	actionLauncherPromoteWorker = SemanticActionPromoteWorker
	actionLauncherBrowsePeople  = "destination:people"
	actionLauncherViewJourneys  = "destination:journeys"
)

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
		gwccss.Display.Grid, gwccss.Raw("grid-template-rows", "auto minmax(0,1fr)"),
		gwccss.Raw("inset-block-start", "48px"), gwccss.Raw("inset-inline-end", "0"),
		gwccss.W(gwccss.MinLen(gwccss.Px(360), gwccss.RawLength("calc(100vw - 28px)"))),
		gwccss.MaxHeight(gwccss.MinLen(gwccss.Vh(70), gwccss.Px(560))),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Padding(gwccss.Px(16)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Px(0), gwccss.Px(18), gwccss.Px(48), gwccss.Zero, gwccss.Hex("10223822"))),
	)
	declareGlobal(".action-launcher-dialog-hidden", gwccss.Display.None)
	declareGlobal(".action-launcher-head",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("margin-bottom", "12px"), gwccss.Raw("padding-bottom", "8px"),
	)
	declareGlobal(".action-launcher-panel,.action-launcher-results-wrap",
		gwccss.MinHeight(gwccss.Zero), gwccss.Raw("overflow-y", "auto"),
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
		gwccss.Raw("text-decoration", "none"), gwccss.Raw("text-align", "start"),
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

	declareGlobal(".action-launcher-result[aria-disabled=true]",
		gwccss.Raw("cursor", "not-allowed"),
		gwccss.Raw("opacity", ".78"),
		gwccss.Raw("border-color", "var(--line)"),
		gwccss.Raw("background", "var(--surface-subtle,var(--soft))"),
	)
	declareGlobal(".action-launcher-result small",
		gwccss.Display.Block,
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.72)),
	)
	declareGlobal(".action-launcher-reason",
		gwccss.Raw("margin-top", "3px"),
		gwccss.Raw("font-weight", "650"),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".action-launcher-recovery",
		gwccss.Raw("margin", "6px 0 2px 42px"),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".action-launcher-dialog",
		mediaRule(gwccss.MaxW(760),
			gwccss.Position.Fixed,
			gwccss.Raw("inset-block-start", "68px"),
			gwccss.Raw("inset-inline", "12px"),
			gwccss.W(gwccss.RawLength("auto")),
			gwccss.MaxHeight(gwccss.RawLength("calc(100dvh - 80px)")),
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
	Page                    PageID
	Reason                  string
	IsNavigationDestination bool
	Kind                    ActionLauncherItemKind
	// Action is the capability operation that makes this start available.
	// Navigation visibility is a separate boundary; a route alone never
	// grants an action.
	Action      string
	ID          string
	Label       string
	Description string
	Href        string
	Icon        string
	Keywords    []string
	// SearchOnly worker results stay out of the initial menu. WorkerLookup is
	// the already-authorized identity needed to reveal one on a named search.
	SearchOnly   bool
	WorkerLookup []string
	Priority     int64
	Availability ActionState
}

// ActionLauncherProps keeps the shell launcher independently composable and
// easy to exercise without passing the page-wide View into the component.
type ActionLauncherProps struct {
	I18nProps
	Items        []ActionLauncherItem
	Navigate     func(string)
	InitialQuery string
}

type semanticLauncherDefinition struct {
	ID             string
	Page           PageID
	Action         string
	LabelKey       string
	DescriptionKey string
	Icon           string
	Keywords       []string
	UsageID        string
	Href           func(View) string
}

var semanticLauncherRegistry = []semanticLauncherDefinition{{
	ID: SemanticActionPromoteWorker, Page: PagePeople, Action: "create_promotion",
	LabelKey: "action_launcher.promote_worker", DescriptionKey: "action_launcher.promote_worker_description",
	Icon: "people", Keywords: []string{"promote", "promotion", "career", "compensation", "employee", "worker"}, UsageID: "promotion",
	Href: func(view View) string { return statefulHref(view, PagePeople, "eligible", "1") },
}}

func actionLauncherProps(view View) ActionLauncherProps {
	items := make([]ActionLauncherItem, 0, len(view.Navigation)+len(view.NavigationSupport)+len(semanticLauncherRegistry))
	for _, definition := range semanticLauncherRegistry {
		projection, ok := uniqueLauncherActionProjection(view.LauncherActions, definition.ID)
		if !ok {
			continue
		}
		priority := projection.Priority
		if useCount := view.WorkflowUses[definition.UsageID]; useCount > priority {
			priority = useCount
		}
		if priority < 100 {
			priority = 100
		}
		items = append(items, ActionLauncherItem{
			Page: definition.Page, Kind: ActionLauncherAction, Action: definition.Action,
			ID: definition.ID, Label: view.Locale.Text(definition.LabelKey), Description: view.Locale.Text(definition.DescriptionKey),
			Href: definition.Href(view), Icon: definition.Icon,
			Keywords: append([]string(nil), definition.Keywords...), Priority: priority, Availability: projection.State,
		})
	}
	// A named-worker start comes from the same admitted workflow catalogue and
	// per-worker verdict as the People row. The shell revalidates each item
	// against this derived inventory before it can be displayed.
	items = append(items, personActionLauncherItems(view)...)

	seen := map[PageID]bool{view.Page: true}
	items = appendActionLauncherDestinations(items, view, view.Navigation, seen)
	items = appendActionLauncherDestinations(items, view, view.NavigationSupport, seen)
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
	workerVerdicts := workerIdentityVerdicts(view)
	for _, person := range population {
		identity := ResolveWorkerIdentity(view.Locale, person, workerVerdicts)
		workerLookup := make([]string, 0, 2)
		if identity.NameStatus == WorkerFactPresent {
			workerLookup = append(workerLookup, identity.Name)
		}
		if identity.WorkerNumberStatus == WorkerFactPresent {
			workerLookup = append(workerLookup, identity.WorkerNumber)
		}
		keywords := compactDiscoveryKeywords(
			identity.Name,
			discoverySearchKeyword(person.ID, person.WorkerNumber, "worker_number", workerVerdicts),
			discoverySearchKeyword(person.ID, person.Team, "organization_unit", workerVerdicts),
			discoverySearchKeyword(person.ID, person.Location, "work_location", workerVerdicts),
			discoverySearchKeyword(person.ID, person.Role, "role", workerVerdicts),
		)
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
				Page: PageJourneys, Kind: ActionLauncherAction, Action: "create_promotion", Availability: ActionState{Availability: ActionAvailable}, ID: fmt.Sprintf("action:%s:%d", person.ID, index),
				Label: label, Description: identity.Role, Href: action.Href,
				Keywords: keywords, SearchOnly: true, WorkerLookup: workerLookup,
			})
		}
		if !authorized {
			reason, reasonWorkflow = PromotionAvailabilityReason(view.Locale, PromotionWithheld), catalog[0].Name
		}
		if reason != "" {
			items = append(items, ActionLauncherItem{
				Page: PageJourneys, Kind: ActionLauncherAction, Action: "create_promotion", Availability: ActionState{Availability: ActionUnavailable, Reason: reason}, ID: "action-unavailable:" + person.ID,
				Label:       view.Locale.Text("people.workflow_aria", map[string]string{"workflow": reasonWorkflow, "name": identity.Label}),
				Description: identity.Role, Reason: reason,
				Keywords: keywords, SearchOnly: true, WorkerLookup: workerLookup,
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
				Page: PageJourneys, Kind: ActionLauncherDestination, Action: "view", Availability: ActionState{Availability: ActionAvailable},
				ID: "start:journeys", Label: view.Locale.Text(definition.LabelKey), Description: view.Locale.Text(definition.SubtitleKey),
				Href: statefulHref(view, PageJourneys), Icon: definition.Icon,
				Keywords:                append(append([]string{"start", "new", "create"}, definition.SearchTerms...), view.Locale.Text(definition.TitleKey)),
				IsNavigationDestination: true,
			})
		}
	}
	if view.Allows(PagePeople, "view") {
		if definition, ok := LookupPage(PagePeople); ok {
			items = append(items, ActionLauncherItem{
				Page: PagePeople, Kind: ActionLauncherDestination, Action: "view", Availability: ActionState{Availability: ActionAvailable},
				ID: "start:people", Label: view.Locale.Text(definition.LabelKey), Description: view.Locale.Text(definition.SubtitleKey),
				Href: statefulHref(view, PagePeople), Icon: definition.Icon,
				Keywords:                append(append([]string{"find", "select", "choose"}, definition.SearchTerms...), view.Locale.Text(definition.TitleKey)),
				IsNavigationDestination: true,
			})
		}
	}
	return items
}

func actionLauncherIsNavigationOnly(items []ActionLauncherItem) bool {
	return !actionLauncherHasAction(items)
}

func appendActionLauncherDestinations(items []ActionLauncherItem, view View, navigation []NavItem, seen map[PageID]bool) []ActionLauncherItem {
	for _, destination := range navigation {
		if destination.Page != "" && !seen[destination.Page] {
			seen[destination.Page] = true
			label, description := destination.Label, destination.Description
			keywords := append([]string(nil), destination.Keywords...)
			switch destination.Page {
			case PagePeople:
				label, description = view.Locale.Text("action_launcher.browse_people"), view.Locale.Text("action_launcher.browse_people_description")
				keywords = append(keywords, "find", "browse", "directory", "employee", "worker")
			case PageJourneys:
				label, description = view.Locale.Text("action_launcher.view_journeys"), view.Locale.Text("action_launcher.view_journeys_description")
				keywords = append(keywords, "view", "track", "workflow", "request", "journey")
			}
			href := destination.Href
			if href == "" {
				href = statefulHref(view, destination.Page)
			}
			items = append(items, ActionLauncherItem{
				Page: destination.Page, Kind: ActionLauncherDestination, Action: "view",
				ID: actionLauncherDestinationID(destination.Page), Label: label, Description: description,
				Href: href, Icon: destination.Icon, Keywords: keywords, Priority: actionLauncherDestinationPriority(destination.Page), Availability: ActionState{Availability: ActionAvailable},
			})
		}
		items = appendActionLauncherDestinations(items, view, destination.Children, seen)
	}
	return items
}

func actionLauncherDestinationPriority(page PageID) int64 {
	switch page {
	case PagePeople:
		return 80
	case PageWork:
		return 70
	case PageJourneys:
		return 60
	case PageMyself:
		return 50
	case PageHelp:
		return 20
	default:
		return 0
	}
}

func actionLauncherDestinationID(page PageID) string {
	return "destination:" + string(page)
}

func semanticLauncherDefinitionByID(id string) (semanticLauncherDefinition, bool) {
	for _, definition := range semanticLauncherRegistry {
		if definition.ID == id {
			return definition, true
		}
	}
	return semanticLauncherDefinition{}, false
}

func uniqueLauncherActionProjection(values []LauncherActionProjection, id string) (LauncherActionProjection, bool) {
	var result LauncherActionProjection
	found := false
	for _, value := range values {
		if value.ID != id {
			continue
		}
		if found {
			return LauncherActionProjection{}, false
		}
		result, found = value, true
	}
	return result, found
}

func personWorkflowByID(workflows []PersonWorkflow, id string) (PersonWorkflow, bool) {
	for _, workflow := range workflows {
		if workflow.ID == id {
			return workflow, true
		}
	}
	return PersonWorkflow{}, false
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
		ordered := append([]ActionLauncherItem(nil), items...)
		sort.SliceStable(ordered, func(left, right int) bool {
			if ordered[left].Priority != ordered[right].Priority {
				return ordered[left].Priority > ordered[right].Priority
			}
			if ordered[left].Kind != ordered[right].Kind {
				return ordered[left].Kind == ActionLauncherAction
			}
			return strings.ToLower(ordered[left].Label) < strings.ToLower(ordered[right].Label)
		})
		results := make([]ActionLauncherItem, 0, minInt(limit, len(ordered)))
		for _, item := range ordered {
			if item.SearchOnly {
				continue
			}
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
		if item.SearchOnly && !actionLauncherWorkerQueryMatches(item.WorkerLookup, tokens) {
			continue
		}
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
	cutoff := 0
	if len(scoredItems) > 0 && scoredItems[0].score >= 130 {
		cutoff = scoredItems[0].score - 55
	}
	for _, candidate := range scoredItems {
		if candidate.score < cutoff {
			continue
		}
		results = append(results, candidate.item)
		if len(results) == limit {
			break
		}
	}
	return results
}

func actionLauncherWorkerQueryMatches(lookup, tokens []string) bool {
	for _, token := range tokens {
		for _, value := range lookup {
			if fuzzyFieldScore(value, token) > 0 {
				return true
			}
		}
	}
	return false
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
	props.Items = presentableActionLauncherItems(props.Items)
	query := ui.UseState(props.InitialQuery)
	open := ui.UseState(strings.TrimSpace(props.InitialQuery) != "")
	active := ui.UseState(0)
	usePopoverFocusDismissal("action-launcher", "action-launcher-trigger", open.Get(), func() { open.Set(false) })
	ui.UseEffectOf(func() func() {
		if open.Get() {
			// Run after the reconciler commits the dialog. The native helper is a
			// no-op, so SSR and WASM share one component contract.
			focusPopoverElement("action-launcher-input")
		}
		return nil
	}, open.Get())
	limit := actionLauncherLimit
	if strings.TrimSpace(query.Get()) == "" {
		limit = actionLauncherInitialLimit
	}
	results := RankActionLauncherItems(props.Items, query.Get(), limit)
	activeIndex := active.Get()
	if activeIndex >= len(results) && len(results) > 0 {
		activeIndex = len(results) - 1
	}
	ui.UseEffectOf(func() func() {
		if open.Get() && len(results) > 0 {
			scrollPopoverElementIntoView("action-launcher-result-" + fmt.Sprint(activeIndex))
		}
		return nil
	}, struct {
		Open  bool
		Index int
		Query string
	}{open.Get(), activeIndex, query.Get()})

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
	triggerKey, dialogKey, filterKey, placeholderKey := actionLauncherCopyKeys(props.Items)
	trigger := html.Button(html.Props{
		ID: "action-launcher-trigger", Class: "action-launcher-trigger", Type: "button",
		Aria: map[string]string{

			"label": props.Text(triggerKey), "haspopup": "dialog",
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
	}, navIcon("actions"), html.Span(html.Props{Class: "action-launcher-label"}, ui.Text(props.Text(triggerKey))))

	// The dialog scaffold (title, input) stays in the document while
	// closed so its accessible name resolves without client state; hidden
	// keeps it out of the accessibility tree and layout until the trigger
	// opens it. The results list is different: it can name every ranked
	// worker this viewer is authorized to act on, so -- like GlobalSearch
	// -- it renders only once open is actually true, never unconditionally
	// into every page's markup regardless of whether anyone opened it.
	dialogHidden := !open.Get()
	inputAria := map[string]string{
		"label": props.Text(filterKey), "autocomplete": "list",
		"expanded": fmt.Sprint(open.Get() && len(results) > 0),
	}
	if open.Get() && len(results) > 0 {
		inputAria["controls"] = "action-launcher-results"
		inputAria["activedescendant"] = "action-launcher-result-" + fmt.Sprint(activeIndex)
	}
	dialogProps := html.Props{
		ID: "action-launcher-dialog", Class: "action-launcher-dialog",

		Raw: map[string]any{"role": "dialog", "aria-label": props.Text(dialogKey)},
	}
	if dialogHidden {
		dialogProps.Class += " action-launcher-dialog-hidden"
		dialogProps.Raw["hidden"] = "hidden"
		dialogProps.Raw["aria-hidden"] = "true"
	}
	dialogChildren := []ui.Node{
		html.Div(html.Props{Class: "action-launcher-head"},
			html.Strong(html.Props{}, ui.Text(props.Text(dialogKey))),
			html.Label(html.Props{Class: "sr-only", For: "action-launcher-input"}, ui.Text(props.Text(filterKey))),
			html.Tag("input", html.Props{
				ID: "action-launcher-input", Name: "action", Value: query.Get(), Class: "action-launcher-input", Aria: inputAria,
				Raw: map[string]any{"type": "search", "role": "combobox", "placeholder": props.Text(placeholderKey), "autocomplete": "off", "spellcheck": "false"},
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
							if actionLauncherItemAvailable(results[activeIndex]) {
								navigate(results[activeIndex])
							}
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

func presentableActionLauncherItems(items []ActionLauncherItem) []ActionLauncherItem {
	result := make([]ActionLauncherItem, 0, len(items))
	for _, item := range items {
		// Legacy compositional callers pass an explicit explanation without an
		// ActionState. It is a disabled presentation, never a launch grant.
		if item.Availability.Availability == "" && strings.TrimSpace(item.Reason) != "" {
			item.Availability = ActionState{Availability: ActionUnavailable, Reason: item.Reason}
		}
		switch item.Availability.Availability {
		case ActionAvailable, ActionUnavailable:
			result = append(result, item)
		}
	}
	return result
}

func actionLauncherCopyKeys(items []ActionLauncherItem) (trigger, dialog, filter, placeholder string) {
	if actionLauncherHasAction(items) {
		return "action_launcher.trigger", "action_launcher.dialog_title", "action_launcher.filter_label", "action_launcher.filter_placeholder"
	}
	return "action_launcher.navigation_trigger", "action_launcher.navigation_title", "action_launcher.navigation_filter_label", "action_launcher.navigation_filter_placeholder"
}

func actionLauncherItemAvailable(item ActionLauncherItem) bool {
	return item.Availability.Availability == ActionAvailable
}

func actionLauncherResults(props ActionLauncherProps, results []ActionLauncherItem, active int, navigate func(ActionLauncherItem)) ui.Node {
	if len(results) == 0 {
		actionMode := actionLauncherHasAction(props.Items)
		titleKey, descriptionKey := "action_launcher.navigation_empty_title", "action_launcher.navigation_empty_description"
		if actionMode {
			titleKey, descriptionKey = "action_launcher.empty_title", "action_launcher.empty_description"
		}
		title, description := props.Text(titleKey), props.Text(descriptionKey)
		if len(props.Items) > 0 {
			titleKey, descriptionKey = "action_launcher.navigation_no_matches_title", "action_launcher.navigation_no_matches_description"
			if actionMode {
				titleKey, descriptionKey = "action_launcher.no_matches_title", "action_launcher.no_matches_description"
			}
			title, description = props.Text(titleKey), props.Text(descriptionKey)
		}
		// This is a status message, not an expanded listbox with missing options.
		return html.Div(html.Props{ID: "action-launcher-empty", Raw: map[string]any{"role": "status"}},
			unavailablePanel(title, description))
	}
	children := make([]ui.Node, 0, len(results))
	recoveries := make([]ui.Node, 0, len(results))
	for index, result := range results {
		item := result
		class := "action-launcher-result"
		selected := index == active
		if selected {
			class += " active"
		}

		reason := strings.TrimSpace(item.Availability.Reason)
		if reason == "" {
			reason = strings.TrimSpace(item.Reason)
		}
		visibleReason := compactActionLauncherReason(reason)
		reasonID := "action-launcher-reason-" + fmt.Sprint(index)
		copy := []ui.Node{html.Strong(html.Props{}, ui.Text(item.Label))}
		if strings.TrimSpace(item.Description) != "" {
			descriptionProps := html.Props{}
			if !actionLauncherItemAvailable(item) && item.Description == reason {
				descriptionProps.ID = reasonID
			}
			copy = append(copy, html.Small(descriptionProps, ui.Text(item.Description)))
		}
		if !actionLauncherItemAvailable(item) && reason != "" && item.Description != reason {
			copy = append(copy, html.Small(html.Props{ID: reasonID, Class: "action-launcher-reason"}, ui.Text(visibleReason)))
		}
		content := []ui.Node{navIcon(item.Icon), html.Span(html.Props{Class: "action-launcher-copy"}, copy...)}
		resultProps := html.Props{
			ID: "action-launcher-result-" + fmt.Sprint(index), Class: class,
			Raw: map[string]any{"role": "option", "aria-selected": fmt.Sprint(selected), "tabindex": "-1"},
		}
		if !actionLauncherItemAvailable(item) {
			resultProps.Type = "button"
			resultProps.Disabled = true
			resultProps.Raw["aria-disabled"] = "true"
			if reason != "" {
				resultProps.Raw["aria-describedby"] = reasonID
			}
			if recovery := item.Availability.Recovery; recovery.Href != "" {
				recoveryID := "action-launcher-recovery-" + fmt.Sprint(index)
				resultProps.Raw["aria-describedby"] = recoveryID
				recoveries = append(recoveries, html.Div(html.Props{ID: recoveryID, Class: "action-launcher-recovery"},
					softwareLink(func(href string) { navigate(ActionLauncherItem{Href: href}) }, html.Props{}, recovery.Href, ui.Text(recovery.Label))))
			}
			children = append(children, html.Button(resultProps, content...))
			continue
		}
		children = append(children, softwareLink(func(href string) { navigate(item) }, resultProps, item.Href, content...))
	}

	listbox := ui.CreateElement(PopoverSurface, PopoverSurfaceProps{
		ID: "action-launcher-results", Class: "action-launcher-panel",
		Raw: map[string]any{"role": "listbox", "aria-label": props.Text(actionLauncherListboxLabelKey(props.Items))}, Children: children,
	})
	if len(recoveries) == 0 {
		return listbox
	}
	return html.Div(html.Props{Class: "action-launcher-results-wrap"}, append([]ui.Node{listbox}, recoveries...)...)
}

// The full server reason remains on the item for audit and recovery. A dense
// launcher row presents its first actionable sentence once, not a paragraph.
func compactActionLauncherReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) <= 100 {
		return reason
	}
	if end := strings.Index(reason, ". "); end > 0 {
		return reason[:end+1]
	}
	return reason
}

func actionLauncherHasAction(items []ActionLauncherItem) bool {
	for _, item := range items {
		if item.Kind == ActionLauncherAction && item.Availability.Availability != ActionHidden {
			return true
		}
	}
	return false
}

func actionLauncherListboxLabelKey(items []ActionLauncherItem) string {
	_, dialog, _, _ := actionLauncherCopyKeys(items)
	return dialog
}
