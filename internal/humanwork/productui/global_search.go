package productui

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const globalSearchLimit = 10

// GlobalSearchItem is a presentation-only destination built from the
// authorized View projection. Search never broadens record or action access;
// every destination is authorized again by its owning service.
type GlobalSearchItem struct {
	ID          string
	Kind        string
	KindLabel   string
	Label       string
	Description string
	Href        string
	Icon        string
	Initials    string
	PhotoURL    string
	Keywords    []string
}

// GlobalSearchProps keeps the shell search independently composable and easy
// to exercise without passing the page-wide View into the component.
type GlobalSearchProps struct {
	I18nProps
	Items        []GlobalSearchItem
	Navigate     func(string)
	FallbackHref string
	InitialQuery string
	HiddenInputs map[string]string
}

func globalSearchProps(view View) GlobalSearchProps {
	hidden := make(map[string]string)
	if locale := view.Locale.normalized(); locale.Resolved != DefaultProductLocale {
		hidden["locale"] = locale.Resolved
	}
	if view.NavCollapsed {
		hidden["nav"] = "collapsed"
	}
	if view.MenuQuery != "" {
		hidden["menu_q"] = view.MenuQuery
	}
	if len(view.FavoritePages) > 0 {
		favorites := make([]string, 0, len(view.FavoritePages))
		for _, page := range view.FavoritePages {
			favorites = append(favorites, string(page))
		}
		hidden["favorites"] = strings.Join(favorites, ",")
	}
	fallback := pageHref(PageHome)
	if PageVisible(PagePeople, view.Roles) {
		fallback = pageHref(PagePeople)
	}
	return GlobalSearchProps{
		I18nProps: I18nProps{Locale: view.Locale}, Items: globalSearchItems(view),
		Navigate: view.Navigate, FallbackHref: fallback, HiddenInputs: hidden,
	}
}

func globalSearchItems(view View) []GlobalSearchItem {
	items := make([]GlobalSearchItem, 0, len(view.People)*2+len(view.Work)+24)
	allowed := make(map[PageID]bool)
	seenPages := make(map[PageID]bool)
	var addNavigation func([]NavItem)
	addNavigation = func(navigation []NavItem) {
		for _, item := range navigation {
			allowed[item.Page] = true
			if !seenPages[item.Page] {
				seenPages[item.Page] = true
				definition, ok := LookupPage(item.Page)
				if ok {
					kind := "page"
					if item.Page == PageSettings || item.Page == PageAppearance || item.Page == PageAdmin || item.Page == PageStudio {
						kind = "setting"
					}
					items = append(items, GlobalSearchItem{
						ID: "page:" + string(item.Page), Kind: kind, KindLabel: globalSearchKindLabel(view.Locale, kind),
						Label: item.Label, Description: item.Description, Href: statefulHref(view, item.Page), Icon: definition.Icon,
						Keywords: append(append([]string(nil), item.Keywords...), definition.SearchTerms...),
					})
				}
			}
			addNavigation(item.Children)
		}
	}
	addNavigation(view.Navigation)

	// Help and session settings are shell-level support destinations and are
	// exposed regardless of the tenant's primary navigation composition.
	for _, page := range []PageID{PageHelp, PageSettings} {
		allowed[page] = true
		if seenPages[page] {
			continue
		}
		definition, _ := LookupPage(page)
		kind := "page"
		if page == PageSettings {
			kind = "setting"
		}
		items = append(items, GlobalSearchItem{
			ID: "page:" + string(page), Kind: kind, KindLabel: globalSearchKindLabel(view.Locale, kind),
			Label: view.Locale.Text(definition.LabelKey), Description: view.Locale.Text(definition.SubtitleKey),
			Href: statefulHref(view, page), Icon: definition.Icon, Keywords: append([]string(nil), definition.SearchTerms...),
		})
	}

	if allowed[PagePeople] {
		for _, person := range view.People {
			if !DiscoveryAdmitted(person.ID, view.RecordVerdicts) {
				continue
			}
			items = append(items, GlobalSearchItem{
				ID: "person:" + person.ID, Kind: "person", KindLabel: globalSearchKindLabel(view.Locale, "person"),
				Label: DiscoveryLabel(view.Locale, person.ID, person.Name, "name", view.RecordVerdicts), Description: strings.Trim(strings.Join([]string{person.Role, person.Team, person.Location}, " · "), " ·"),
				Href: statefulHref(view, PagePerson, "person", person.ID), Initials: person.Initials, PhotoURL: person.PhotoURL,
				Keywords: []string{person.WorkerNumber, person.JobCode, person.Team, person.Location, "employee", "worker", "profile"},
			})
		}
		items = append(items, GlobalSearchItem{
			ID: "component:people-directory", Kind: "component", KindLabel: globalSearchKindLabel(view.Locale, "component"),
			Label: view.Locale.Text("people.find"), Description: view.Locale.Text("people.filter_placeholder"),
			Href: statefulHref(view, PagePeople), Icon: "people", Keywords: []string{"directory", "employee filters", "sort people", "teams", "locations"},
		})
	}

	if allowed[PageJourneys] {
		seenWorkflows := map[string]bool{"promotion": true}
		items = append(items, GlobalSearchItem{
			ID: "workflow:promotion", Kind: "workflow", KindLabel: globalSearchKindLabel(view.Locale, "workflow"),
			Label: view.Locale.Text("history.promotion"), Description: view.Locale.Text("page.journeys.subtitle"),
			Href: statefulHref(view, PageJourneys), Icon: "journeys", Keywords: []string{"promote", "career", "compensation", "approval", "employee workflow"},
		})
		for _, workflow := range view.PersonWorkflows {
			if seenWorkflows[workflow.ID] {
				continue
			}
			seenWorkflows[workflow.ID] = true
			href := workflow.Href
			if href == "" {
				href = statefulHref(view, PageJourneys)
			} else {
				href = globalSearchStatefulHref(view, href)
			}
			items = append(items, GlobalSearchItem{
				ID: "workflow:" + workflow.ID, Kind: "workflow", KindLabel: globalSearchKindLabel(view.Locale, "workflow"),
				Label: workflow.Name, Description: strings.Trim(strings.Join([]string{workflow.Category, workflow.Description}, " · "), " ·"),
				Href: href, Icon: "journeys", Keywords: []string{workflow.ID, workflow.Category, workflow.Description, "employee workflow"},
			})
		}
		for _, person := range view.People {
			if !DiscoveryAdmitted(person.ID, view.RecordVerdicts) {
				continue
			}
			if !personPromotionEligible(person) {
				continue
			}
			items = append(items, GlobalSearchItem{
				ID: "action:promotion:" + person.ID, Kind: "action", KindLabel: globalSearchKindLabel(view.Locale, "action"),
				Label:       view.Locale.Text("global_search.promote_person", map[string]string{"name": DiscoveryLabel(view.Locale, person.ID, person.Name, "name", view.RecordVerdicts)}),
				Description: strings.Trim(strings.Join([]string{person.Role, person.Team}, " · "), " ·"),
				Href:        JourneyProposalHref(view, person.ID), Icon: "journeys",
				Keywords: []string{"promotion", "promote", "start workflow", person.WorkerNumber, person.JobCode},
			})
		}
	}

	if allowed[PageHistory] {
		items = append(items, GlobalSearchItem{
			ID: "component:workflow-history", Kind: "component", KindLabel: globalSearchKindLabel(view.Locale, "component"),
			Label: view.Locale.Text("history.global_title"), Description: view.Locale.Text("history.global_detail"),
			Href: statefulHref(view, PageHistory), Icon: "history", Keywords: []string{"past workflows", "filter history", "sort history", "audit outcomes"},
		})
	}

	if allowed[PageJourneys] {
		for _, work := range view.Work {
			if !DiscoveryAdmitted(work.ID, view.RecordVerdicts) {
				continue
			}
			details := []string{work.Status, work.Summary}
			if work.CompletedAt != "" {
				details = append(details, view.Locale.Text("global_search.closed", map[string]string{"value": work.CompletedAt}))
			} else if work.EffectiveDate != "" {
				details = append(details, view.Locale.Text("global_search.effective", map[string]string{"value": work.EffectiveDate}))
			}
			items = append(items, GlobalSearchItem{
				ID: "workflow-instance:" + work.ID, Kind: "workflow", KindLabel: globalSearchKindLabel(view.Locale, "workflow"),
				Label:       strings.TrimSpace(DiscoveryLabel(view.Locale, work.ID, work.Title, "title", view.RecordVerdicts) + " · " + DiscoveryLabel(view.Locale, work.ID, work.Person, "person", view.RecordVerdicts)),
				Description: strings.Trim(strings.Join(details, " · "), " ·"),
				Href:        JourneyDetailHref(view, work.ID), Icon: "journeys", Initials: work.Initials, PhotoURL: work.PhotoURL,
				Keywords: []string{work.PersonRef, work.InstanceID, work.MaterialDigest, "workflow record", "journey"},
			})
		}
	}

	if allowed[PageSettings] {
		items = append(items,
			searchComponent(view, "user-profile", "settings.profile_title", "settings.profile_description", PageSettings, "user account avatar photo profile personal settings"),
			searchComponent(view, "language", "settings.locale_title", "settings.locale_description", PageSettings, "language locale region translation"),
			searchComponent(view, "accessibility", "accessibility.title", "accessibility.description", PageSettings, "text size contrast reduced motion link visibility"),
			searchComponent(view, "access-context", "settings.access_title", "settings.access_description", PageSettings, "permissions authorization session principal scope"),
		)
	}
	if allowed[PageAppearance] {
		items = append(items, searchComponent(view, "brand-appearance", "page.appearance.label", "appearance.intro_detail", PageAppearance, "branding logo theme colors dark mode shapes glyphs typography"))
	}
	return items
}

func globalSearchStatefulHref(view View, href string) string {
	parsed, err := url.Parse(href)
	if err != nil || !strings.HasPrefix(parsed.Path, "/workspace/app/") {
		return statefulHref(view, PageJourneys)
	}
	values := parsed.Query()
	if view.NavCollapsed {
		values.Set("nav", "collapsed")
	}
	setMenuAddressState(values, view)
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func searchComponent(view View, id, labelKey, descriptionKey string, page PageID, keywords string) GlobalSearchItem {
	return GlobalSearchItem{
		ID: "component:" + id, Kind: "component", KindLabel: globalSearchKindLabel(view.Locale, "component"),
		Label: view.Locale.Text(labelKey), Description: view.Locale.Text(descriptionKey), Href: statefulHref(view, page),
		Icon: "settings", Keywords: strings.Fields(keywords),
	}
}

func globalSearchKindLabel(locale LocaleContext, kind string) string {
	return locale.Text("global_search.kind_" + kind)
}

type scoredGlobalSearchItem struct {
	item  GlobalSearchItem
	score int
}

// SearchGlobalItems performs deterministic typo-tolerant ranking over the
// local authorized projection. It intentionally does not send keystrokes to a
// server or reveal records outside the already-resolved View.
func SearchGlobalItems(items []GlobalSearchItem, query string, limit int) []GlobalSearchItem {
	tokens := strings.Fields(normalizeNavigationSearch(query))
	if len(tokens) == 0 || limit <= 0 {
		return nil
	}
	scored := make([]scoredGlobalSearchItem, 0, len(items))
	for _, item := range items {
		if score := globalSearchScore(item, tokens); score > 0 {
			scored = append(scored, scoredGlobalSearchItem{item: item, score: score})
		}
	}
	sort.SliceStable(scored, func(left, right int) bool {
		if scored[left].score != scored[right].score {
			return scored[left].score > scored[right].score
		}
		if globalSearchKindPriority(scored[left].item.Kind) != globalSearchKindPriority(scored[right].item.Kind) {
			return globalSearchKindPriority(scored[left].item.Kind) < globalSearchKindPriority(scored[right].item.Kind)
		}
		return strings.ToLower(scored[left].item.Label) < strings.ToLower(scored[right].item.Label)
	})
	results := make([]GlobalSearchItem, 0, minInt(limit, len(scored)))
	perKind := make(map[string]int)
	for _, candidate := range scored {
		if perKind[candidate.item.Kind] >= 4 {
			continue
		}
		results = append(results, candidate.item)
		perKind[candidate.item.Kind]++
		if len(results) == limit {
			break
		}
	}
	return results
}

func globalSearchScore(item GlobalSearchItem, tokens []string) int {
	fields := []struct {
		value  string
		weight int
	}{
		{item.Label, 48}, {item.Description, 16}, {item.KindLabel, 8}, {item.Kind, 6},
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

func globalSearchKindPriority(kind string) int {
	switch kind {
	case "person":
		return 0
	case "workflow":
		return 1
	case "page", "setting":
		return 2
	case "component":
		return 3
	default:
		return 4
	}
}

// GlobalSearch is the top-level command/search surface shared by every page.
func GlobalSearch(props GlobalSearchProps) ui.Node {
	query := ui.UseState(props.InitialQuery)
	open := ui.UseState(strings.TrimSpace(props.InitialQuery) != "")
	active := ui.UseState(0)
	results := SearchGlobalItems(props.Items, query.Get(), globalSearchLimit)
	activeIndex := active.Get()
	if activeIndex >= len(results) && len(results) > 0 {
		activeIndex = len(results) - 1
	}

	navigate := func(item GlobalSearchItem) {
		query.Set("")
		open.Set(false)
		active.Set(0)
		if props.Navigate != nil {
			props.Navigate(item.Href)
		}
	}
	inputAria := map[string]string{
		"label": props.Text("global_search.label"), "autocomplete": "list", "controls": "global-search-results",
		"expanded": fmt.Sprint(open.Get() && strings.TrimSpace(query.Get()) != ""),
	}
	if open.Get() && len(results) > 0 {
		inputAria["activedescendant"] = "global-search-result-" + fmt.Sprint(activeIndex)
	}
	inputProps := html.Props{
		ID: "global-search-input", Name: "q", Value: query.Get(), Class: "global-search-input", Aria: inputAria,
		Raw: map[string]any{"type": "search", "role": "combobox", "placeholder": props.Text("global_search.placeholder"), "autocomplete": "off", "spellcheck": "false"},
		OnInput: ui.UseEvent(func(event ui.InputEvent) {
			query.Set(event.GetValue())
			open.Set(strings.TrimSpace(event.GetValue()) != "")
			active.Set(0)
		}),
		OnFocus: ui.UseEvent(func(ui.FocusEvent) {
			open.Set(strings.TrimSpace(query.Get()) != "")
		}),
		OnKeyDown: ui.UseEvent(func(event ui.KeyboardEvent) {
			switch event.GetKey() {
			case "Escape":
				open.Set(false)
			case "ArrowDown":
				if len(results) > 0 {
					event.PreventDefault()
					open.Set(true)
					active.Set((active.Get() + 1) % len(results))
				}
			case "ArrowUp":
				if len(results) > 0 {
					event.PreventDefault()
					open.Set(true)
					active.Set((active.Get() - 1 + len(results)) % len(results))
				}
			case "Enter":
				if open.Get() && len(results) > 0 {
					event.PreventDefault()
					navigate(results[activeIndex])
				}
			}
		}),
	}

	children := []ui.Node{
		html.Div(html.Props{Class: "global-search-control"},
			// PROMOUX-015: a real associated label, not only aria-label, so the
			// control is named by every assistive technology and by the
			// repository's document qualification checks.
			html.Label(html.Props{Class: "sr-only", For: "global-search-input"}, ui.Text(props.Text("global_search.label"))),
			html.Span(html.Props{Class: "global-search-glyph", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text("⌕")),
			html.Tag("input", inputProps),
		),
	}
	hiddenNames := make([]string, 0, len(props.HiddenInputs))
	for name := range props.HiddenInputs {
		hiddenNames = append(hiddenNames, name)
	}
	sort.Strings(hiddenNames)
	for _, name := range hiddenNames {
		value := props.HiddenInputs[name]
		children = append(children, html.Tag("input", html.Props{Name: name, Value: value, Raw: map[string]any{"type": "hidden"}}))
	}
	if open.Get() && strings.TrimSpace(query.Get()) != "" {
		children = append(children, globalSearchResults(props, results, activeIndex, navigate))
	}
	formProps := html.Props{
		Class: "global-search", Action: props.FallbackHref, Method: "get", Raw: map[string]any{"role": "search"},
		OnSubmit: ui.UseEvent(func(event ui.FormEvent) {
			if props.Navigate == nil || len(results) == 0 {
				return
			}
			event.PreventDefault()
			navigate(results[activeIndex])
		}),
	}
	return html.Form(formProps, children...)
}

func globalSearchResults(props GlobalSearchProps, results []GlobalSearchItem, active int, navigate func(GlobalSearchItem)) ui.Node {
	children := []ui.Node{
		html.Div(html.Props{Class: "global-search-panel-head"},
			html.Strong(html.Props{}, ui.Text(props.Text("global_search.results"))),
			html.Span(html.Props{}, ui.Text(props.Text("global_search.hint"))),
		),
	}
	if len(results) == 0 {
		children = append(children, html.Div(html.Props{Class: "global-search-empty", Raw: map[string]any{"role": "status"}}, ui.Text(props.Text("global_search.no_results"))))
	} else {
		for index, result := range results {
			item := result
			class := "global-search-result"
			selected := index == active
			if selected {
				class += " active"
			}
			lead := navIcon(item.Icon)
			if item.Initials != "" || item.PhotoURL != "" {
				lead = personAvatar(item.Label, item.Initials, item.PhotoURL, "small")
			}
			children = append(children, softwareLink(func(href string) { navigate(item) }, html.Props{
				ID: "global-search-result-" + fmt.Sprint(index), Class: class,
				Raw: map[string]any{"role": "option", "aria-selected": fmt.Sprint(selected)},
			}, item.Href,
				lead,
				html.Span(html.Props{Class: "global-search-copy"},
					html.Strong(html.Props{}, ui.Text(item.Label)),
					html.Small(html.Props{}, ui.Text(item.Description)),
				),
				html.Span(html.Props{Class: "global-search-kind"}, ui.Text(item.KindLabel)),
			))
		}
	}
	return ui.CreateElement(PopoverSurface, PopoverSurfaceProps{
		ID: "global-search-results", Class: "global-search-panel",
		Raw: map[string]any{"role": "listbox", "aria-label": props.Text("global_search.results")}, Children: children,
	})
}
