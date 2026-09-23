package productui

import (
	"strings"
	"sync"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ColumnChooserProps is independent of the table's records and transport.
// Selected is an allowlisted comma-separated column selection supplied by the adapter.
type ColumnChooserProps struct {
	I18nProps
	ID, Selected string
	Options      []ColumnChoice
	Apply        func(string)
	Reset        func()
	Draft        *ColumnChooserDraft
}

type ColumnChoice struct {
	ID, Label string
	Required  bool
}

type columnChooserState struct {
	Input, Selected string
	Open            bool
}

// ColumnChooserDraft belongs to the application instance, not a package global.
// It retains only unsaved display choices across live-data outlet remounts.
// Saved preferences still live on the server and supersede a stale draft.
type ColumnChooserDraft struct {
	mu    sync.Mutex
	state columnChooserState
}

func (draft *ColumnChooserDraft) load() columnChooserState {
	draft.mu.Lock()
	defer draft.mu.Unlock()
	return draft.state
}
func (draft *ColumnChooserDraft) save(value columnChooserState) {
	draft.mu.Lock()
	defer draft.mu.Unlock()
	draft.state = value
}

// ColumnChooser batches edits until Apply. Native disclosure and checkbox
// semantics provide keyboard support without another bespoke popover lifecycle.
func ColumnChooser(props ColumnChooserProps) ui.Node {
	state := ui.UseState(columnChooserState{Input: props.Selected, Selected: props.Selected})
	current := state.Get()
	if props.Draft != nil {
		current = props.Draft.load()
	}
	save := func(value columnChooserState) {
		if props.Draft != nil {
			props.Draft.save(value)
		}
		state.Set(value)
	}
	if current.Input != props.Selected {
		current = columnChooserState{Input: props.Selected, Selected: props.Selected}
		save(current)
	}
	options := make([]ui.Node, 0, len(props.Options))
	for _, option := range props.Options {
		checked := option.Required || strings.Contains(","+current.Selected+",", ","+option.ID+",")
		input := html.Props{ID: props.ID + "-" + option.ID, Type: "checkbox", Checked: checked, Disabled: option.Required || props.Apply == nil}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) {
			value := current
			ids := toggleColumnChoice(value.Selected, option.ID)
			value.Selected = ids
			save(value)
		})
		options = append(options, html.Label(html.Props{Class: "column-choice", For: input.ID}, html.Input(input), ui.Text(option.Label)))
	}
	details := html.Props{Class: "column-chooser"}
	if current.Open {
		details.Raw = map[string]any{"open": true}
	}
	summary := html.Props{OnClick: ui.UseEvent(func(event ui.MouseEvent) {
		event.PreventDefault()
		value := current
		value.Open = !value.Open
		save(value)
	})}
	return html.Details(details,
		html.Summary(summary, ui.Text(props.Text("table.columns"))),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Text("table.columns_help"))),
		html.Div(html.Props{Class: "column-choices"}, options...),
		html.Div(html.Props{Class: "column-choice-actions"},
			html.Button(html.Props{Class: "button primary", Type: "button", Disabled: props.Apply == nil, OnClick: ui.UseEvent(func() {
				if props.Apply != nil {
					value := current
					value.Open = false
					save(value)
					props.Apply(value.Selected)
				}
			})}, ui.Text(props.Text("table.columns_apply"))),
			html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: props.Reset == nil, OnClick: ui.UseEvent(func() {
				if props.Reset != nil {
					value := current
					value.Open = false
					save(value)
					props.Reset()
				}
			})}, ui.Text(props.Text("table.columns_reset"))),
		),
	)
}

func toggleColumnChoice(selected, id string) string {
	ids := []string{}
	found := false
	for _, value := range strings.Split(selected, ",") {
		if value == id {
			found = true
		} else if value != "" {
			ids = append(ids, value)
		}
	}
	if !found {
		ids = append(ids, id)
	}
	return strings.Join(ids, ",")
}

func columnChooserStylesheet() string {
	return `
.column-chooser{border:1px solid var(--line);border-radius:var(--hcm-radius-surface);padding:var(--hcm-space-1);background:var(--surface);color:var(--ink);margin-block-end:var(--hcm-space-2);min-width:0}
.column-chooser>summary{cursor:pointer;font-weight:600;padding:var(--hcm-space-1);width:fit-content}
.column-chooser>p{margin:var(--hcm-space-1)}
.column-choices{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,12rem),1fr));gap:var(--hcm-space-1);padding:var(--hcm-space-1)}
.column-choice{display:flex;align-items:center;gap:var(--hcm-space-1);min-height:44px;cursor:pointer;overflow-wrap:anywhere}
.column-choice input{width:1.1rem;height:1.1rem;flex:none;accent-color:var(--accent)}
.column-choice-actions{display:flex;flex-wrap:wrap;gap:var(--hcm-space-1);padding:var(--hcm-space-1)}
.people-search-panel{position:relative;border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface);min-width:0}
.people-search-panel>.people-filter{border:0;background:transparent;margin:0;box-shadow:none;min-width:0;grid-template-columns:minmax(0,1fr)}
.people-page .people-search-panel>.people-filter>label{min-height:44px;display:flex;align-items:center;padding-inline-end:10.5rem}
.people-search-panel>.column-chooser{border:0;border-radius:0;background:transparent;margin:0;padding:0 var(--hcm-space-2) var(--hcm-space-2)}
.people-search-panel>.column-chooser:not([open]){padding:0}
.people-search-panel>.column-chooser>summary{position:absolute;inset-block-start:var(--hcm-space-2);inset-inline-end:var(--hcm-space-2);min-height:44px;box-sizing:border-box;border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control);color:var(--ink);font-size:var(--hcm-font-size-small);padding:12px}
.people-search-panel>.column-chooser[open]>summary{background:var(--soft);border-color:var(--accent)}
.people-search-panel>.column-chooser>p{border-block-start:1px solid var(--line);padding-block-start:var(--hcm-space-2);margin-block-start:0}
@media(max-width:480px){.people-search-panel>.people-filter{padding-inline:var(--hcm-space-1)}.people-page .people-search-panel>.people-filter>label{padding-inline-end:9rem}.people-search-panel>.column-chooser>summary{inset-inline-end:var(--hcm-space-1);padding-inline:var(--hcm-space-1)}.people-search-panel :is(input,select){box-sizing:border-box;max-width:100%}}
`
}
