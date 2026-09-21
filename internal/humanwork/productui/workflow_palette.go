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

// WorkflowPalette renders local search over a server-filtered catalog. Search
// changes only component state, so typing never refetches or remounts the
// workflow graph.
func WorkflowPalette(props WorkflowPaletteProps) ui.Node {
	query := ui.UseState("")
	groups := filterWorkflowPalette(props.Items, query.Get(), props.Text("workflow_palette.other_domain"))
	domains := make([]string, 0, len(groups))
	for domain := range groups {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	groupNodes := make([]ui.Node, 0, len(domains))
	for _, domain := range domains {
		items := groups[domain]
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Name == items[j].Name {
				return items[i].ID < items[j].ID
			}
			return items[i].Name < items[j].Name
		})
		cards := make([]ui.Node, 0, len(items))
		for _, item := range items {
			canInsert := props.OnInsert != nil
			if props.CanInsert != nil {
				canInsert = props.CanInsert(item)
			}
			cards = append(cards, workflowPaletteCard(props.I18nProps, item, props.OnInsert, canInsert))
		}
		groupNodes = append(groupNodes, html.Section(html.Props{Class: "workflow-palette-group", Aria: map[string]string{"label": domain}},
			html.Div(html.Props{Class: "workflow-palette-group-heading"},
				html.H4(html.Props{}, ui.Text(domain)),
				html.Span(html.Props{Class: "count-badge"}, ui.Text(fmt.Sprint(len(items)))),
			),
			html.Ul(html.Props{Class: "workflow-palette-list", Raw: map[string]any{"role": "list"}}, cards...),
		))
	}
	body := ui.Node(html.Div(html.Props{Class: "workflow-palette-groups"}, groupNodes...))
	if len(groupNodes) == 0 {
		key := "workflow_palette.empty"
		if len(props.Items) == 0 {
			key = "workflow_palette.unavailable"
		}
		body = html.P(html.Props{Class: "muted workflow-palette-empty", Raw: map[string]any{"role": "status"}}, ui.Text(props.Text(key)))
	}
	return html.Aside(html.Props{Class: "surface workflow-palette", Aria: map[string]string{"labelledby": "workflow-palette-heading"}},
		html.Div(html.Props{Class: "workflow-palette-heading"},
			html.Div(html.Props{},
				html.H3(html.Props{ID: "workflow-palette-heading"}, ui.Text(props.Text("workflow_palette.title"))),
				html.P(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_palette.detail"))),
			),
			html.Span(html.Props{Class: "count-badge"}, ui.Text(fmt.Sprint(len(props.Items)))),
		),
		html.Label(html.Props{For: "workflow-palette-search", Class: "workflow-palette-search-label"},
			html.Span(html.Props{}, ui.Text(props.Text("workflow_palette.search"))),
			ui.CreateElement(SearchInput, SearchInputProps{
				ID: "workflow-palette-search", Name: "workflow_palette_search", Value: query.Get(),
				Placeholder: props.Text("workflow_palette.search"), AriaLabel: props.Text("workflow_palette.search_aria"), OnInput: query.Set,
			}),
		),
		body,
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

func workflowPaletteCard(i18n I18nProps, item WorkflowPaletteItem, onInsert func(WorkflowPaletteItem), canInsert bool) ui.Node {
	version := fmt.Sprintf("v%d", item.Version)
	heading := html.Div(html.Props{Class: "workflow-palette-card-heading"},
		html.Strong(html.Props{Dir: "auto"}, ui.Text(item.Name)),
		html.Span(html.Props{Class: "workflow-palette-version"}, ui.Text(version)),
	)
	body := []ui.Node{
		html.P(html.Props{Class: "muted"}, ui.Text(item.Description)),
		html.Div(html.Props{Class: "workflow-palette-badges", Aria: map[string]string{"label": i18n.Text("workflow_palette.properties")}},
			html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": "info"}}, ui.Text(workflowPaletteKind(i18n, item.Kind))),
			html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": workflowEffectTone(item.EffectClass)}}, ui.Text(displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "effect", item.EffectClass))),
			html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": workflowReversalTone(item.Reversal)}}, ui.Text(displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "reversal", item.Reversal))),
		),
	}
	if onInsert != nil {
		copyKey := "workflow_palette.add_block"
		if strings.EqualFold(item.Kind, "FRAGMENT") {
			copyKey = "workflow_palette.add_fragment"
		} else if strings.EqualFold(item.Kind, "TEMPLATE") {
			copyKey = "workflow_palette.add_template"
		}
		selected := item
		buttonProps := html.Props{
			Class: "button secondary compact workflow-palette-insert", Type: "button",
		}
		if canInsert {
			buttonProps.OnClick = ui.UseEvent(func(ui.MouseEvent) { onInsert(selected) })
		} else {
			buttonProps.Disabled = true
			buttonProps.Title = i18n.Text("workflow_palette.create_draft_first")
		}
		body = append(body, html.Button(buttonProps, productIcon("plus", "button-icon"), ui.Text(i18n.Text(copyKey, map[string]string{"name": item.Name}))))
	}

	kind := strings.ToLower(strings.TrimSpace(item.Kind))
	itemProps := html.Props{Class: "workflow-palette-item", Data: map[string]string{"kind": kind, "entry-id": item.ID}}
	if kind == "fragment" {
		return html.Li(itemProps,
			html.Details(html.Props{Class: "workflow-palette-fragment", Data: map[string]string{"insert-mode": "group"}},
				html.Summary(html.Props{Aria: map[string]string{"label": i18n.Text("workflow_palette.expand_fragment", map[string]string{"name": item.Name})}}, heading),
				html.Div(html.Props{Class: "workflow-palette-card-body"}, body...),
			),
		)
	}
	return html.Li(itemProps, html.Article(html.Props{}, append([]ui.Node{heading}, body...)...))
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

func workflowPaletteStylesheet() string {
	return `.workflow-palette{padding:var(--hcm-space-2);display:grid;gap:var(--hcm-space-2);position:sticky;inset-block-start:var(--hcm-space-2);max-block-size:calc(100dvh - var(--hcm-space-4));overflow:auto}
.workflow-palette-heading,.workflow-palette-group-heading,.workflow-palette-card-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:var(--hcm-space-1)}
.workflow-palette-heading h3,.workflow-palette-heading p,.workflow-palette-group-heading h4{margin:0}.workflow-palette-heading>div{min-width:0}.workflow-palette-heading p{margin-block-start:var(--hcm-space-1)}
.workflow-palette-search-label{display:grid;gap:var(--hcm-space-1);font:var(--hcm-type-label);color:var(--muted)}.workflow-palette input[type=search]{inline-size:100%;min-block-size:2.5rem}.workflow-palette-groups{display:grid;gap:var(--hcm-space-2)}
.workflow-palette-group{display:grid;gap:var(--hcm-space-1)}.workflow-palette-list{list-style:none;margin:0;padding:0;display:grid;gap:var(--hcm-space-1)}
.workflow-palette-item article,.workflow-palette-fragment{padding:var(--hcm-space-2);border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control);background:var(--surface);transition:border-color var(--hcm-motion-fast) var(--hcm-motion-easing),background-color var(--hcm-motion-fast) var(--hcm-motion-easing)}
.workflow-palette-item article:hover,.workflow-palette-fragment:hover,.workflow-palette-fragment:focus-within{border-color:var(--accent);background:var(--hcm-hover-surface,var(--soft))}.workflow-palette-item p{margin-block:var(--hcm-space-1)}
.workflow-palette-fragment>summary{cursor:pointer;list-style-position:outside}.workflow-palette-card-body{display:grid;gap:var(--hcm-space-1);padding-block-start:var(--hcm-space-1)}.workflow-palette-insert{justify-self:start}.workflow-palette-insert .button-icon{inline-size:1rem;block-size:1rem}
.workflow-palette-version{font-size:var(--hcm-font-size-small);color:var(--muted);white-space:nowrap}.workflow-palette-badges{display:flex;flex-wrap:wrap;gap:var(--hcm-space-1)}
.workflow-palette-badges .status-chip{font-size:var(--hcm-font-size-small)}.workflow-palette-empty{margin:0;padding-block:var(--hcm-space-2)}
@media (max-width:1440px){.workflow-palette{position:static;max-block-size:none}.workflow-palette-groups{display:grid;grid-template-columns:repeat(auto-fit,minmax(16rem,1fr))}}
@media (max-width:640px){.workflow-palette-groups{grid-template-columns:1fr}}`
}
