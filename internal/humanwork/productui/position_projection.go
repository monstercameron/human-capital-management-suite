package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PositionOptionProjection is one server-authorized position selectable by
// its canonical revision reference.
type PositionOptionProjection struct {
	Reference    string
	PositionID   string
	Title        string
	Organization string
	JobCode      string
	OrgUnit      string
}

// PositionObjectProjection is a server-authorized presentation of a current
// position revision. An empty projection means the reference did not resolve.
type PositionObjectProjection struct {
	PositionID string
	Revision   string
	JobCode    string
	OrgUnit    string
	Lifecycle  string
	Compatible bool
}

// PositionOccupancyProjection carries capacity results already computed by
// the Position domain from occupants read for this same position.
type PositionOccupancyProjection struct {
	PositionID     string
	CapacityFTE    string
	CapacityHeads  int64
	ConsumedFTE    string
	ConsumedHeads  int64
	AvailableFTE   string
	AvailableHeads int64
	Occupants      []PositionOccupantProjection
}

// PositionOccupantProjection is one worker reference and its allocated FTE
// from the server-authorized occupancy result.
type PositionOccupantProjection struct {
	WorkerID string
	FTE      string
}

func positionOptionSelector(view View) ui.Node {
	options := []ui.Node{
		html.Option(html.Props{Value: "", Selected: strings.TrimSpace(view.PositionReference) == "", Raw: map[string]any{"value": ""}},
			ui.Text(view.Locale.Text("position_selector.choose"))),
	}
	for _, option := range view.PositionOptions {
		reference := strings.TrimSpace(option.Reference)
		if reference == "" {
			continue
		}
		options = append(options, html.Option(html.Props{Value: reference, Selected: reference == view.PositionReference},
			ui.Text(positionOptionLabel(option))))
	}
	selectProps := html.Props{
		ID: "position-reference", Name: "position_ref", Value: view.PositionReference,
		Raw: map[string]any{"aria-describedby": "position-selector-help"},
	}
	if view.Navigate != nil {
		selectProps.OnChange = ui.UseEvent(func(event ui.InputEvent) {
			selected := strings.TrimSpace(event.GetValue())
			view.Navigate(positionOptionHref(view, selected))
		})
	}
	children := []ui.Node{
		html.Label(html.Props{For: "position-reference"}, ui.Text(view.Locale.Text("position_selector.label"))),
		html.Select(selectProps, options...),
		html.P(html.Props{ID: "position-selector-help", Class: "position-selector-help"}, ui.Text(view.Locale.Text("position_selector.help"))),
	}
	if len(options) == 1 {
		children = append(children, html.P(html.Props{Class: "position-selector-empty"}, ui.Text(view.Locale.Text("position_selector.empty"))))
	}
	if locale := view.Locale.normalized(); locale.Resolved != DefaultProductLocale {
		children = append(children, html.Tag("input", html.Props{Name: "locale", Value: locale.Resolved, Raw: map[string]any{"type": "hidden"}}))
	}
	if view.NavCollapsed {
		children = append(children, html.Tag("input", html.Props{Name: "nav", Value: "collapsed", Raw: map[string]any{"type": "hidden"}}))
	}
	children = append(children, html.Button(html.Props{Class: "button secondary position-selector-submit", Type: "submit", Disabled: len(options) == 1}, ui.Text(view.Locale.Text("position_selector.open"))))
	return html.Form(html.Props{Class: "position-selector", Action: pageHref(view.Page), Method: "get"}, children...)
}

func positionOptionHref(view View, reference string) string {
	return statefulHref(view, view.Page, "position_ref", strings.TrimSpace(reference))
}

func positionOptionLabel(option PositionOptionProjection) string {
	primary := strings.TrimSpace(option.Title)
	if primary == "" {
		primary = strings.TrimSpace(option.PositionID)
	}
	secondary := make([]string, 0, 3)
	for _, label := range []string{option.Organization, option.JobCode, option.OrgUnit} {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		duplicate := false
		for _, existing := range secondary {
			if strings.EqualFold(existing, label) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			secondary = append(secondary, label)
		}
	}
	if len(secondary) == 0 {
		return primary
	}
	return primary + " · " + strings.Join(secondary, " · ")
}
