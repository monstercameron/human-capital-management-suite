package productui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// WorkflowPaletteItem is the transport-neutral, already-authorized catalog
// record the workflow editor may render. The browser never filters authority:
// an entry absent from this slice is absent from markup and component state.
type WorkflowPaletteItem struct {
	ID, Name, Kind, Domain, Description string
	EffectClass, Reversal, Status       string
	StepType                            string
	Version                             uint32
}

type WorkflowPaletteProps struct {
	I18nProps
	Items     []WorkflowPaletteItem
	OnInsert  func(WorkflowPaletteItem)
	CanInsert func(WorkflowPaletteItem) bool
}

// WorkflowPalette is the step library beside the path. Search changes only
// component state, so typing never refetches or remounts the draft.
//
// Every entry is one row: its name, one plain sentence, and an add button. An
// entry is marked only when it does something an author has to weigh, which
// is changing data or being hard to reverse; "pure, no effect" on every row
// said nothing and was removed.
func WorkflowPalette(props WorkflowPaletteProps) ui.Node {
	query := ui.UseState("")
	// Below 1400px the library folds to one row above the path so the path
	// keeps its place. Folded, it showed four of twenty-two steps and nothing
	// said there were more; the author opens it when they want to browse.
	expanded := ui.UseState(false)
	toggle := ui.UseEvent(func() { expanded.Set(!expanded.Get()) })
	localized := make([]WorkflowPaletteItem, 0, len(props.Items))
	for _, item := range props.Items {
		// Search and headings work on what the author reads, so an entry is
		// found by its name in their language and filed under their word for
		// its group.
		shown := item
		shown.Name = workflowPaletteName(props.I18nProps, item)
		shown.Description = workflowPaletteDescription(props.I18nProps, item)
		shown.Domain = workflowPaletteDomain(props.I18nProps, item.Domain)
		localized = append(localized, shown)
	}
	groups := filterWorkflowPalette(localized, query.Get(), props.Text("workflow_palette.other_domain"))
	domains := make([]string, 0, len(groups))
	for domain := range groups {
		domains = append(domains, domain)
	}
	// Groups keep one order everywhere: the building blocks everyone needs
	// first. Sorting the translated headings put an untranslated group above
	// them in Arabic and buried "Flow" at the bottom.
	rank := func(domain string) int {
		for index, key := range []string{"control_flow", "people", "rewards", "operations", "intelligence", "registry"} {
			if domain == workflowPaletteDomain(props.I18nProps, strings.ReplaceAll(key, "_", " ")) {
				return index
			}
		}
		return 99
	}
	sort.SliceStable(domains, func(i, j int) bool {
		if rank(domains[i]) != rank(domains[j]) {
			return rank(domains[i]) < rank(domains[j])
		}
		return domains[i] < domains[j]
	})
	groupNodes := make([]ui.Node, 0, len(domains))
	for _, domain := range domains {
		items := groups[domain]
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Name == items[j].Name {
				return items[i].ID < items[j].ID
			}
			return items[i].Name < items[j].Name
		})
		rows := make([]ui.Node, 0, len(items))
		for _, item := range items {
			canInsert := props.OnInsert != nil
			if props.CanInsert != nil {
				canInsert = canInsert && props.CanInsert(item)
			}
			rows = append(rows, html.WithKey(ui.CreateElement(workflowPaletteRow, workflowPaletteRowProps{
				I18nProps: props.I18nProps, Item: item, OnInsert: props.OnInsert, CanInsert: canInsert,
			}), fmt.Sprintf("entry-%s-%d", item.ID, item.Version)))
		}
		groupNodes = append(groupNodes, html.Section(html.Props{Key: "domain-" + domain, Class: "workflow-palette-group", Aria: map[string]string{"label": domain}},
			html.H4(html.Props{}, ui.Text(domain)),
			html.Ul(html.Props{Class: "workflow-palette-list", Raw: map[string]any{"role": "list"}}, rows...),
		))
	}
	body := ui.Node(html.Div(html.Props{Key: "groups", ID: "workflow-palette-groups", Class: "workflow-palette-groups"}, groupNodes...))
	if len(groupNodes) == 0 {
		key := "workflow_palette.empty"
		if len(props.Items) == 0 {
			key = "workflow_palette.unavailable"
		}
		body = html.P(html.Props{Key: "empty", Class: "muted workflow-palette-empty", Raw: map[string]any{"role": "status"}}, ui.Text(props.Text(key)))
	}
	class, moreKey := "workflow-palette", "workflow_editor.library_show_all"
	if expanded.Get() {
		class, moreKey = class+" expanded", "workflow_editor.library_show_fewer"
	}
	var more ui.Node
	if steps := workflowPaletteStepCount(props.Items); len(groupNodes) > 0 && steps > 4 {
		more = html.Button(html.Props{Key: "more", Class: "button ghost compact workflow-palette-more", Type: "button", OnClick: toggle,
			Aria: map[string]string{"expanded": fmt.Sprint(expanded.Get()), "controls": "workflow-palette-groups"}},
			ui.Text(props.Locale.Plural(moreKey, int64(steps))))
	}
	return html.Aside(html.Props{Class: class, Aria: map[string]string{"labelledby": "workflow-palette-heading"}},
		html.H3(html.Props{Key: "title", ID: "workflow-palette-heading"},
			ui.Text(props.Text("workflow_editor.library_title")),
			html.Span(html.Props{Class: "workflow-palette-count"}, ui.Text(props.Locale.Plural("workflow_editor.library_count", int64(workflowPaletteStepCount(props.Items))))),
		),
		html.Div(html.Props{Key: "search", Class: "workflow-palette-search"},
			html.Label(html.Props{For: "workflow-palette-search", Class: "sr-only"}, ui.Text(props.Text("workflow_palette.search"))),
			ui.CreateElement(SearchInput, SearchInputProps{
				ID: "workflow-palette-search", Name: "workflow_palette_search", Value: query.Get(),
				Placeholder: props.Text("workflow_palette.search"), AriaLabel: props.Text("workflow_palette.search"), OnInput: query.Set,
			}),
		),
		body,
		more,
	)
}

func filterWorkflowPalette(items []WorkflowPaletteItem, query, otherDomain string) map[string][]WorkflowPaletteItem {
	normalized := strings.ToLower(strings.TrimSpace(query))
	groups := make(map[string][]WorkflowPaletteItem)
	for _, item := range items {
		haystack := strings.ToLower(strings.Join([]string{item.ID, item.Name, item.Kind, item.Domain, item.Description, item.EffectClass, item.Reversal, item.StepType}, " "))
		if normalized != "" && !strings.Contains(haystack, normalized) {
			continue
		}
		domain := strings.TrimSpace(item.Domain)
		if domain == "" {
			domain = otherDomain
		}
		groups[domain] = append(groups[domain], item)
	}
	return groups
}

type workflowPaletteRowProps struct {
	I18nProps
	Item      WorkflowPaletteItem
	OnInsert  func(WorkflowPaletteItem)
	CanInsert bool
}

func workflowPaletteRow(props workflowPaletteRowProps) ui.Node {
	item := props.Item
	click := ui.UseEvent(func(ui.MouseEvent) {
		if props.CanInsert && props.OnInsert != nil {
			props.OnInsert(item)
		}
	})
	tokens := WorkflowViewerProps{I18nProps: props.I18nProps}
	kind := strings.ToLower(strings.TrimSpace(item.Kind))

	flags := make([]ui.Node, 0, 3)
	if kind != "block" {
		flags = append(flags, html.Span(html.Props{Key: "kind", Class: "workflow-palette-flag"}, ui.Text(workflowPaletteKind(props.I18nProps, item.Kind))))
	}
	if workflowEffectTone(item.EffectClass) == "warning" {
		flags = append(flags, html.Span(html.Props{Key: "effect", Class: "workflow-palette-flag", Data: map[string]string{"tone": "warning"}}, ui.Text(displayWorkflowToken(tokens, "effect", item.EffectClass))))
	}
	if reversal := strings.ToUpper(strings.TrimSpace(item.Reversal)); reversal != "" && reversal != "NO_EFFECT" {
		flags = append(flags, html.Span(html.Props{Key: "reversal", Class: "workflow-palette-flag", Data: map[string]string{"tone": workflowReversalTone(item.Reversal)}}, ui.Text(displayWorkflowToken(tokens, "reversal", item.Reversal))))
	}

	copyKey := "workflow_palette.add_block"
	switch kind {
	case "fragment":
		copyKey = "workflow_palette.add_fragment"
	case "template":
		copyKey = "workflow_palette.add_template"
	}
	label := props.Text(copyKey, map[string]string{"name": item.Name})
	// The whole row adds the step. Only the small plus used to, and the name
	// beside it is what people click first.
	button := html.Props{Class: "workflow-palette-row workflow-palette-insert", Type: "button", OnClick: click, Aria: map[string]string{"label": label}}
	if !props.CanInsert {
		button.Disabled = true
		button.Title = props.Text("workflow_palette.cannot_add")
	}
	return html.Li(html.Props{Class: "workflow-palette-item", Data: map[string]string{"kind": kind, "entry-id": item.ID}},
		html.Button(button,
			html.Span(html.Props{Class: "workflow-palette-copy"},
				html.Strong(html.Props{Dir: "auto"}, ui.Text(item.Name)),
				html.Span(html.Props{Class: "muted workflow-palette-description", Dir: "auto"}, ui.Text(item.Description)),
				html.Span(html.Props{Class: "workflow-palette-flags"}, flags...),
			),
			productIcon("plus", "workflow-palette-plus"),
		),
	)
}

// workflowPaletteName gives a built-in step type the same name the path and
// the inspector use for it. The library once said "Approval" above a German
// sentence while the step it added was labelled "Genehmigung".
func workflowPaletteName(i18n I18nProps, item WorkflowPaletteItem) string {
	if workflowPaletteIsBuiltIn(item) {
		return displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "step", item.StepType)
	}
	return item.Name
}

func workflowPaletteIsBuiltIn(item WorkflowPaletteItem) bool {
	return strings.EqualFold(item.Kind, "BLOCK") && strings.TrimSpace(item.StepType) != "" &&
		strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(DisplayLabel(item.StepType)))
}

func workflowPaletteDomain(i18n I18nProps, domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	key := "workflow_palette.domain." + strings.ReplaceAll(strings.ToLower(domain), " ", "_")
	if text := i18n.Text(key); !strings.HasPrefix(text, "⟦") {
		return text
	}
	return domain
}

// workflowPaletteDescription prefers reviewed catalog copy for the built-in
// step types, whose server descriptions are a generated English sentence, and
// keeps the registry's own description for everything a tenant registers.
func workflowPaletteDescription(i18n I18nProps, item WorkflowPaletteItem) string {
	if workflowPaletteIsBuiltIn(item) {
		key := "workflow_palette.step." + strings.ToLower(strings.TrimSpace(item.StepType))
		if text := i18n.Text(key); !strings.HasPrefix(text, "⟦") {
			return text
		}
	}
	return item.Description
}

func workflowPaletteKind(i18n I18nProps, kind string) string {
	key := "workflow_palette.kind_" + strings.ToLower(strings.TrimSpace(kind))
	translated := i18n.Text(key)
	if strings.HasPrefix(translated, "⟦") {
		return strings.ToUpper(strings.TrimSpace(kind))
	}
	return translated
}

func workflowEffectTone(effect string) string {
	if strings.Contains(strings.ToUpper(effect), "MUTATION") {
		return "warning"
	}
	return "neutral"
}

func workflowReversalTone(reversal string) string {
	if strings.EqualFold(reversal, "FORWARD_CORRECTION") || strings.EqualFold(reversal, "UNRESOLVED") {
		return "warning"
	}
	return "neutral"
}

// workflowPaletteStepCount counts what the heading calls steps. A template is
// a whole workflow, and it leaves the library once the draft has a step, so
// counting it made the number drop by one after the author's first click.
func workflowPaletteStepCount(items []WorkflowPaletteItem) int {
	count := 0
	for _, item := range items {
		if !strings.EqualFold(item.Kind, "TEMPLATE") {
			count++
		}
	}
	return count
}
