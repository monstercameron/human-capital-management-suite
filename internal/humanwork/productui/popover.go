package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const transientPopoverGraceMilliseconds = "180"

// PopoverSurfaceProps is the shared visual contract for every floating surface,
// including stateful listboxes that do not use a details/summary disclosure.
type PopoverSurfaceProps struct {
	ID       string
	Class    string
	Raw      map[string]any
	Children []ui.Node
}

// PopoverSurface renders the common themed, animated popover panel.
func PopoverSurface(props PopoverSurfaceProps) ui.Node {
	class := strings.TrimSpace("popover-surface " + props.Class)
	return html.Div(html.Props{ID: props.ID, Class: class, Raw: props.Raw, Data: map[string]string{"hcm-popover-surface": "true"}}, props.Children...)
}

// TransientPopoverProps is the narrow interaction contract shared by header
// menus and contextual row actions.
type TransientPopoverProps struct {
	Kind          string
	Class         string
	TriggerClass  string
	PanelClass    string
	Label         string
	Title         string
	DescriptionID string
	Trigger       []ui.Node
	Children      []ui.Node
	// Group joins this disclosure to a set that may have at most one member
	// open. It becomes the native <details name>, so the browser closes
	// whichever sibling was open when this one opens -- two row menus in a
	// dense table used to stay open together and overlap (UXLIVE-021).
	Group string
}

// TransientPopover preserves native details/summary behavior as the
// progressive-enhancement baseline. The WASM controller adds delayed pointer
// dismissal, outside-click dismissal, and Escape handling.
func TransientPopover(props TransientPopoverProps) ui.Node {
	kind := strings.TrimSpace(props.Kind)
	if kind == "" {
		kind = "generic"
	}
	rootClass := strings.TrimSpace("popover-root " + props.Class)
	triggerProps := html.Props{Class: props.TriggerClass}
	if props.Label != "" {
		triggerProps.Aria = map[string]string{"label": props.Label}
	}
	if props.Title != "" {
		triggerProps.Raw = map[string]any{"title": props.Title}
	}
	if props.DescriptionID != "" {
		if triggerProps.Raw == nil {
			triggerProps.Raw = map[string]any{}
		}
		triggerProps.Raw["aria-describedby"] = props.DescriptionID
	}
	rootProps := html.Props{Class: rootClass, Data: map[string]string{
		"hcm-transient-popover": kind,
		"hcm-popover-grace-ms":  transientPopoverGraceMilliseconds,
	}}
	if group := strings.TrimSpace(props.Group); group != "" {
		rootProps.Raw = map[string]any{"name": group}
	}
	return html.Details(rootProps,
		html.Summary(triggerProps, props.Trigger...),
		ui.CreateElement(PopoverSurface, PopoverSurfaceProps{Class: props.PanelClass, Children: props.Children}),
	)
}
