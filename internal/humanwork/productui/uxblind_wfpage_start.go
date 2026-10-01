package productui

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// WorkflowStartAvailability is the safe UI vocabulary for the server's
// start-authority projection.
type WorkflowStartAvailability string

const (
	WorkflowStartAvailable           WorkflowStartAvailability = "available"
	WorkflowStartNoCapability        WorkflowStartAvailability = "no_capability"
	WorkflowStartMissingAuthority    WorkflowStartAvailability = "missing_authority"
	WorkflowStartMissingPrerequisite WorkflowStartAvailability = "missing_prerequisite"
	WorkflowStartQuarantined         WorkflowStartAvailability = "quarantined"
)

// WorkflowStartItem is display metadata plus a bounded availability answer.
// It contains no subject facts, capability names, or raw authorization data.
type WorkflowStartItem struct {
	WorkflowID      string
	Version         uint32
	SemanticVersion string
	Name            string
	Description     string
	Category        string
	Keywords        []string
	Icon            string
	Owner           string
	Availability    WorkflowStartAvailability
	Favorite        bool
	RecentRank      int64
}

type WorkflowStartPageProps struct {
	I18nProps
	Catalog    []WorkflowStartItem
	Favorites  []string
	Recent     []string
	DeepLinkID string
	// CanDesign is true when the viewer administers workflows, so empty and
	// unavailable states name the real cause instead of pointing them at
	// themselves.
	CanDesign        bool
	BaseHref         string
	Navigate         func(string)
	OnToggleFavorite func(string, bool)
}

func (workflowStartPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("workflow_catalog", "Workflow catalog", "Browse workflows the viewer may discover", true, false, false, false),
		feature("workflow_start", "Workflow starts", "Open an authorized governed workflow start", true, true, false, false),
		feature("workflow_favorites", "Workflow favorites", "Save workflow starts for this viewer", true, true, true, false),
	}
}

// WorkflowStartHref is the canonical deep-link form. The workflow id is
// escaped as a path segment; unknown ids are handled by the page as a generic
// not-found state and never reveal whether a hidden definition exists.
func WorkflowStartHref(view View, workflowID string) string {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return statefulHref(view, PageWorkflowStart)
	}
	href := pageHref(PageWorkflowStart) + "/start/" + url.PathEscape(workflowID)
	if locale := view.Locale.normalized(); locale.Resolved != DefaultProductLocale || locale.Requested != "" {
		href += "?locale=" + url.QueryEscape(locale.Resolved)
	}
	return href
}

func workflowStartPage(view View) ui.Node {
	favorites := workflowStartPreferenceIDs(view.WorkflowStartCatalog, view.WorkflowStartFavorites, true)
	recent := workflowStartPreferenceIDs(view.WorkflowStartCatalog, view.WorkflowStartRecent, false)
	return ui.CreateElement(WorkflowStartPage, WorkflowStartPageProps{
		I18nProps:  I18nProps{Locale: view.Locale},
		Catalog:    append([]WorkflowStartItem(nil), view.WorkflowStartCatalog...),
		Favorites:  favorites,
		Recent:     recent,
		DeepLinkID: strings.TrimSpace(view.WorkflowStartWorkflowID),
		CanDesign:  workflowStartViewerCanDesign(view.Navigation),
		BaseHref:   statefulHref(view, PageWorkflowStart), Navigate: view.Navigate,
		OnToggleFavorite: func(id string, favorite bool) {
			if view.ToggleWorkflowFavorite != nil {
				view.ToggleWorkflowFavorite(id, favorite)
			}
			if view.SaveFavorite != nil {
				view.SaveFavorite(PageID(workflowStartFavoritePageID(id)), favorite)
			}
		},
	})
}

func workflowStartFavoritePageID(id string) string { return "workflow-start:" + strings.TrimSpace(id) }

func workflowStartPreferenceIDs(catalog []WorkflowStartItem, ids []string, favorites bool) []string {
	if len(ids) > 0 {
		return append([]string(nil), ids...)
	}
	if favorites {
		result := make([]string, 0, len(catalog))
		for _, item := range catalog {
			if item.Favorite {
				result = append(result, item.WorkflowID)
			}
		}
		return result
	}
	ordered := append([]WorkflowStartItem(nil), catalog...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].RecentRank != ordered[j].RecentRank {
			return ordered[i].RecentRank > ordered[j].RecentRank
		}
		return strings.ToLower(ordered[i].Name) < strings.ToLower(ordered[j].Name)
	})
	result := make([]string, 0, len(ordered))
	for _, item := range ordered {
		if item.RecentRank > 0 {
			result = append(result, item.WorkflowID)
		}
	}
	return workflowStartRecentIDs(result)
}

// RankWorkflowStartItems is the page's bounded type-ahead primitive. Name is
// strongest, followed by business keywords and category; ties remain stable
// and deterministic for keyboard users and SSR.
func RankWorkflowStartItems(items []WorkflowStartItem, query string) []WorkflowStartItem {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if len(tokens) == 0 {
		return append([]WorkflowStartItem(nil), items...)
	}
	type scored struct {
		item  WorkflowStartItem
		score int
		index int
	}
	scoredItems := make([]scored, 0, len(items))
	for index, item := range items {
		name := strings.ToLower(item.Name)
		category := strings.ToLower(item.Category)
		fields := append([]string{strings.ToLower(item.WorkflowID), name, strings.ToLower(item.Description), category}, lowerStrings(item.Keywords)...)
		score := 0
		matched := true
		for _, token := range tokens {
			found := false
			for _, field := range fields {
				if strings.Contains(field, token) {
					found = true
					break
				}
			}
			if !found {
				matched = false
				break
			}
			if strings.HasPrefix(name, token) {
				score += 80
			} else if strings.Contains(name, token) {
				score += 60
			}
			if strings.Contains(category, token) {
				score += 35
			}
			for _, keyword := range item.Keywords {
				if strings.Contains(strings.ToLower(keyword), token) {
					score += 30
				}
			}
		}
		if matched {
			scoredItems = append(scoredItems, scored{item: item, score: score, index: index})
		}
	}
	sort.SliceStable(scoredItems, func(i, j int) bool {
		if scoredItems[i].score != scoredItems[j].score {
			return scoredItems[i].score > scoredItems[j].score
		}
		return strings.ToLower(scoredItems[i].item.Name) < strings.ToLower(scoredItems[j].item.Name)
	})
	result := make([]WorkflowStartItem, 0, len(scoredItems))
	for _, value := range scoredItems {
		result = append(result, value.item)
	}
	return result
}

func lowerStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, strings.ToLower(value))
	}
	return result
}

func workflowStartResolveIDs(catalog []WorkflowStartItem, ids []string, availableOnly bool) []WorkflowStartItem {
	byID := make(map[string]WorkflowStartItem, len(catalog))
	for _, item := range catalog {
		if availableOnly && item.Availability != WorkflowStartAvailable {
			continue
		}
		byID[item.WorkflowID] = item
	}
	result := make([]WorkflowStartItem, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if item, ok := byID[id]; ok {
			result = append(result, item)
			seen[id] = true
		}
	}
	return result
}

const workflowStartRecentLimit = 10

// workflowStartRecentIDs keeps the server's newest-first preference projection
// bounded at the page boundary. It also removes duplicates and stale ids so a
// user never sees more than ten actionable recent starts.
func workflowStartRecentIDs(ids []string) []string {
	result := make([]string, 0, minInt(len(ids), workflowStartRecentLimit))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || len(result) == workflowStartRecentLimit {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

func workflowStartAvailabilityKey(availability WorkflowStartAvailability) string {
	if availability == "" {
		return string(WorkflowStartMissingAuthority)
	}
	return string(availability)
}

func workflowStartDeepLink(catalog []WorkflowStartItem, id string) (WorkflowStartItem, bool) {
	for _, item := range catalog {
		if item.WorkflowID == id {
			return item, true
		}
	}
	return WorkflowStartItem{}, false
}

func WorkflowStartPage(props WorkflowStartPageProps) ui.Node {
	query := ui.UseState("")
	active := ui.UseState(0)
	favorites := ui.UseState(append([]string(nil), props.Favorites...))
	recent := workflowStartPreferenceIDs(props.Catalog, props.Recent, false)
	inputRef := ui.UseDOMRef()
	ui.UseAutoFocus(inputRef, true)
	onInput := ui.UseEvent(func(event ui.InputEvent) { query.Set(event.GetValue()); active.Set(0) })
	items := RankWorkflowStartItems(props.Catalog, query.Get())
	if active.Get() >= len(items) {
		active.Set(0)
	}
	onKeyDown := ui.UseEvent(func(event ui.KeyboardEvent) {
		switch event.GetKey() {
		case "ArrowDown":
			if len(items) > 0 {
				event.PreventDefault()
				active.Set((active.Get() + 1) % len(items))
			}
		case "ArrowUp":
			if len(items) > 0 {
				event.PreventDefault()
				active.Set((active.Get() - 1 + len(items)) % len(items))
			}
		case "Enter":
			if len(items) > 0 && props.Navigate != nil {
				event.PreventDefault()
				props.Navigate(WorkflowStartHref(View{Locale: props.Locale}, items[active.Get()].WorkflowID))
			}
		case "Escape":
			if query.Get() != "" {
				event.PreventDefault()
				query.Set("")
				active.Set(0)
			}
		}
	})
	deepLinkID := strings.TrimSpace(props.DeepLinkID)
	deepItem, deepFound := workflowStartDeepLink(props.Catalog, deepLinkID)
	deepUnavailable := deepFound && deepItem.Availability != WorkflowStartAvailable
	if deepLinkID != "" && !deepFound {
		return workflowStartFrame(props, props.Text("workflow_start.not_found_title"), props.Text("workflow_start.not_found_detail"), nil)
	}
	if deepUnavailable {
		detailKey := "workflow_start.unavailable_detail"
		if props.CanDesign {
			detailKey += "_designer"
		}
		detail := props.Text(detailKey, map[string]string{"reason": props.Text("workflow_start.availability_" + workflowStartAvailabilityKey(deepItem.Availability))})
		return workflowStartFrame(props, props.Text("workflow_start.unavailable_title"), detail, &deepItem)
	}
	if len(props.Catalog) == 0 {
		return workflowStartFrame(props, props.Text("workflow_start.empty_title"), props.Text(workflowStartEmptyDetailKey(props.CanDesign)), nil)
	}

	favoriteItems := workflowStartResolveIDs(props.Catalog, favorites.Get(), true)
	recentItems := workflowStartResolveIDs(props.Catalog, workflowStartRecentIDs(recent), true)
	children := []ui.Node{
		html.Header(html.Props{Class: "workflow-start-header"},
			html.Div(html.Props{Class: "workflow-start-title"}, html.Span(html.Props{Class: "eyebrow", Text: props.Text("workflow_start.eyebrow")}), html.H1(html.Props{ID: "workflow-start-heading", Text: props.Text("workflow_start.title")}), html.P(html.Props{Class: "muted", Text: props.Text("workflow_start.description")})),
		),
		html.Div(html.Props{Class: "workflow-start-search", Role: "search"}, html.Label(html.Props{For: "workflow-start-search-input", Class: "sr-only", Text: props.Text("workflow_start.search_label")}), html.Input(html.WithProps(html.Props{ID: "workflow-start-search-input", Type: "search", Placeholder: props.Text("workflow_start.search_placeholder"), AutoComplete: "off", OnInput: onInput, OnKeyDown: onKeyDown, Aria: map[string]string{"controls": "workflow-start-results", "expanded": "true"}}, html.Ref(inputRef))), html.P(html.Props{Class: "workflow-start-count", Role: "status", Aria: map[string]string{"live": "polite"}, Text: props.Text("workflow_start.result_count", map[string]string{"count": formatWorkflowStartCount(len(items))})})),
	}
	toggleFavorite := func(id string) {
		next := append([]string(nil), favorites.Get()...)
		found := false
		for i, value := range next {
			if value == id {
				next = append(next[:i], next[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			next = append(next, id)
		}
		favorites.Set(next)
		if props.OnToggleFavorite != nil {
			props.OnToggleFavorite(id, !found)
		}
	}
	if len(favoriteItems) > 0 {
		children = append(children, workflowStartSection(props, "workflow_start.favorites", favoriteItems, favorites.Get(), active.Get(), items, toggleFavorite))
	}
	if len(recentItems) > 0 {
		children = append(children, workflowStartSection(props, "workflow_start.recent", recentItems, favorites.Get(), active.Get(), items, toggleFavorite))
	}
	children = append(children, html.Div(html.Props{ID: "workflow-start-results", Class: "workflow-start-results", Aria: map[string]string{"label": props.Text("workflow_start.catalog_label")}}, workflowStartCatalogGroups(props, items, favorites.Get(), active.Get(), toggleFavorite)...))
	return html.Section(html.Props{Class: "workflow-start-page", Aria: map[string]string{"labelledby": "workflow-start-heading"}}, children...)
}

func formatWorkflowStartCount(count int) string { return strconv.Itoa(count) }

func workflowStartFrame(props WorkflowStartPageProps, title, detail string, item *WorkflowStartItem) ui.Node {
	children := []ui.Node{html.H1(html.Props{ID: "workflow-start-heading", Text: props.Text("workflow_start.title")}), html.Div(html.Props{Class: "workflow-start-state surface", Raw: map[string]any{"role": "status"}}, html.H2(html.Props{Text: title}), html.P(html.Props{Text: detail}))}
	if item != nil && item.WorkflowID != "" {
		children = append(children, ui.CreateElement(workflowStartCard, workflowStartCardProps{I18nProps: props.I18nProps, Item: *item, Favorite: item.Favorite, Index: -1, Active: -2, Navigate: props.Navigate}))
	}
	return html.Section(html.Props{Class: "workflow-start-page", Aria: map[string]string{"labelledby": "workflow-start-heading"}}, children...)
}

func workflowStartSection(props WorkflowStartPageProps, titleKey string, items []WorkflowStartItem, favorites []string, active int, all []WorkflowStartItem, toggleFavorite func(string)) ui.Node {
	return html.Section(html.Props{Class: "workflow-start-section"}, html.H2(html.Props{Text: props.Text(titleKey)}), html.Div(html.Props{Class: "workflow-start-grid"}, workflowStartCards(props, items, favorites, active, all, toggleFavorite)...))
}

func workflowStartCatalogGroups(props WorkflowStartPageProps, items []WorkflowStartItem, favorites []string, active int, toggleFavorite func(string)) []ui.Node {
	groups := make(map[string][]WorkflowStartItem)
	for _, item := range items {
		key := strings.TrimSpace(item.Category)
		if key == "" {
			key = props.Text("workflow_start.uncategorized")
		}
		groups[key] = append(groups[key], item)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]ui.Node, 0, len(keys))
	for _, key := range keys {
		result = append(result, html.Section(html.Props{Class: "workflow-start-section"}, html.H2(html.Props{Text: key}), html.Div(html.Props{Class: "workflow-start-grid"}, workflowStartCards(props, groups[key], favorites, active, items, toggleFavorite)...)))
	}
	return result
}

func workflowStartCards(props WorkflowStartPageProps, items []WorkflowStartItem, favorites []string, active int, all []WorkflowStartItem, toggleFavorite func(string)) []ui.Node {
	result := make([]ui.Node, 0, len(items))
	for _, item := range items {
		index := workflowStartItemIndex(all, item.WorkflowID)
		result = append(result, ui.CreateElement(workflowStartCard, workflowStartCardProps{I18nProps: props.I18nProps, Item: item, Favorite: workflowStartContains(favorites, item.WorkflowID), Index: index, Active: active, Navigate: props.Navigate, OnToggleFavorite: toggleFavorite}))
	}
	return result
}

func workflowStartItemIndex(items []WorkflowStartItem, id string) int {
	for index, item := range items {
		if item.WorkflowID == id {
			return index
		}
	}
	return -1
}

type workflowStartCardProps struct {
	I18nProps
	Item             WorkflowStartItem
	Favorite         bool
	Index, Active    int
	Navigate         func(string)
	OnToggleFavorite func(string)
}

func workflowStartCard(props workflowStartCardProps) ui.Node {
	item := props.Item
	isFavorite := props.Favorite || item.Favorite
	toggle := ui.UseEvent(func() {
		if props.OnToggleFavorite != nil {
			props.OnToggleFavorite(item.WorkflowID)
		}
	})
	start := WorkflowStartHref(View{Locale: props.Locale}, item.WorkflowID)
	cardClass := "workflow-start-card"
	if props.Active == props.Index {
		cardClass += " active"
	}
	favoriteKey := "workflow_start.favorite_add"
	if isFavorite {
		favoriteKey = "workflow_start.favorite_remove"
	}
	favoriteLabel := props.Text(favoriteKey)
	favorite := html.Button(html.Props{
		Class:   "workflow-start-favorite",
		Type:    "button",
		OnClick: toggle,
		Aria:    map[string]string{"pressed": formatBool(isFavorite), "label": favoriteLabel},
		Raw:     map[string]any{"title": favoriteLabel},
	}, productIcon("favorite", "workflow-start-favorite-icon"))
	open := ui.Node(nil)
	if item.Availability == WorkflowStartAvailable {
		label := props.Text("workflow_start.start_action", map[string]string{"workflow": item.Name})
		open = html.A(html.Props{Class: "button primary workflow-start-open", Href: start, OnClick: workflowStartNavigate(props.Navigate, start)}, ui.Text(label))
	} else {
		reason := props.Text("workflow_start.availability_" + workflowStartAvailabilityKey(item.Availability))
		open = html.Div(html.Props{Class: "workflow-start-card-meta"}, html.Span(html.Props{Class: "workflow-start-unavailable", Aria: map[string]string{"role": "status"}, Data: map[string]string{"availability": string(item.Availability)}, Text: reason}))
	}
	return html.Article(html.Props{Class: cardClass, Data: map[string]string{"workflow-id": item.WorkflowID}}, html.Div(html.Props{Class: "workflow-start-card-head"}, html.Div(html.Props{Class: "workflow-start-card-icon", Aria: map[string]string{"hidden": "true"}}, productIcon(workflowStartIcon(item), "")), favorite), html.H3(html.Props{Text: item.Name}), html.P(html.Props{Class: "muted", Text: oneLineWorkflowDescription(item.Description)}), open)
}

func workflowStartIcon(item WorkflowStartItem) string {
	if icon := strings.TrimSpace(item.Icon); icon != "" && iconPath(icon) != fallbackIconPath {
		return icon
	}
	category := strings.ToLower(strings.TrimSpace(item.Category + " " + item.WorkflowID))
	switch {
	case strings.Contains(category, "time"), strings.Contains(category, "clock"), strings.Contains(category, "attendance"), strings.Contains(category, "punch"):
		return "clock"
	case strings.Contains(category, "people"), strings.Contains(category, "employee"), strings.Contains(category, "hire"), strings.Contains(category, "onboard"), strings.Contains(category, "leave"):
		return "people"
	case strings.Contains(category, "work"), strings.Contains(category, "operation"), strings.Contains(category, "project"):
		return "work"
	default:
		return "journeys"
	}
}

func workflowStartNavigate(navigate func(string), href string) ui.Handler {
	if navigate == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.MouseEvent) { event.PreventDefault(); navigate(href) })
}
func oneLineWorkflowDescription(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
func workflowStartContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func formatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func workflowStartEmptyDetailKey(canDesign bool) string {
	if canDesign {
		return "workflow_start.empty_detail_designer"
	}
	return "workflow_start.empty_detail"
}

// workflowStartViewerCanDesign reports whether the viewer's own navigation
// reaches the Workflow Designer, which is what publishes startable workflows.
func workflowStartViewerCanDesign(items []NavItem) bool {
	for _, item := range items {
		if item.Page == PageWorkflowDesigner || workflowStartViewerCanDesign(item.Children) {
			return true
		}
	}
	return false
}
