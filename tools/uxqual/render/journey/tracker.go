package journey

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// journeyGroupingNone is ListView.Grouping's flat-list value.
const journeyGroupingNone = productui.JourneyListGroupNone

// journeyResultCount states how many requests the list shows (UXLIVE-031).
// Without a filter it is the section's existing count chip. With one it is
// the same chip made a polite live status, so applying a filter announces
// its result once, and a narrowed list says "3 of 12" rather than letting
// "3 requests" read as everything the viewer can see.
func journeyResultCount(locale string, v ListView) ui.Node {
	if v.Filter == nil {
		return chip(toneNeutral, countLabelLocale(locale, len(v.Journeys)))
	}
	return html.Span(html.Props{Class: "jn-chip jn-journey-result", DataAttr: html.DataAttribute{Name: "tone", Value: toneNeutral},
		Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}},
		chipIcon(toneNeutral),
		html.Text(v.Filter.Result),
	)
}

// journeyFilterForm is the tracker's search landmark. It is a real GET form
// whose control names are the product route keys, so without the live
// client it still navigates to the filtered address with its Apply button;
// with the client every control applies itself (selects and dates on
// change, the search after a pause) and Enter still submits, so the Apply
// button is left out. A tracker with no request at all offers no filter.
//
// The controls and actions share one grid so the layout is deliberate:
// search, status, order and grouping on the first row; the date range and
// the actions on the second; a single column on a phone.
func journeyFilterForm(l live, f *JourneyFilterView) ui.Node {
	if f == nil || f.Total == 0 || len(f.Fields) == 0 {
		return nil
	}
	actions := []ui.Node{}
	if f.OnSubmit == nil {
		actions = append(actions, html.Button(html.Props{Class: "jn-btn", Type: "submit", DataAttr: html.DataAttribute{Name: "variant", Value: "secondary"}}, html.Text(f.Submit)))
	}
	if f.ClearHref != "" {
		actions = append(actions, html.A(html.Props{Class: "jn-journey-filter-clear", Href: f.ClearHref, OnClick: activate(f.OnClear)}, html.Text(f.ClearLabel)))
	}
	panel := make([]ui.Node, 0, len(f.Fields))
	for _, field := range f.Fields[1:] {
		panel = append(panel, fieldNode(l, field, false))
	}
	if len(actions) > 0 {
		// An empty actions row would hold its height as a gap.
		panel = append(panel, html.Div(html.Props{Class: "jn-journey-filter-actions"}, actions...))
	}
	open := journeyFilterPanelOpen(l, f)
	return html.Form(html.Props{Class: "jn-journey-filter", Method: "get", Role: "search",
		Aria: map[string]string{"label": f.Label}, OnSubmit: l.submitHandler(f.OnSubmit, nil, f.Fields)},
		html.Div(html.Props{Class: "jn-journey-filter-fields"},
			fieldNode(l, f.Fields[0], false),
			journeyFilterToggle(l, f, open),
			html.Div(html.Props{ID: journeyFilterPanelID, Class: "jn-journey-filter-panel",
				DataAttr: html.DataAttribute{Name: "open", Value: strconv.FormatBool(open)}}, panel...),
		),
	)
}

// JourneyFilterPanelField is the controlled value that records whether the
// reader opened or closed the narrow-screen filter panel ("open"/"closed").
const JourneyFilterPanelField = "journey-filter-panel"

const journeyFilterPanelID = "journey-filter-panel"

// journeyFilterPanelOpen is the panel's state: the reader's own choice when
// they made one, otherwise open exactly when a filter other than the search
// is set, so an applied filter is never hidden behind a closed toggle.
func journeyFilterPanelOpen(l live, f *JourneyFilterView) bool {
	switch l.values[JourneyFilterPanelField] {
	case "open":
		return true
	case "closed":
		return false
	}
	return f.PanelActive > 0
}

// journeyFilterToggle is the narrow-screen "Filters (2)" button. It is a
// real button with aria-expanded and aria-controls; from a tablet up the
// stylesheet removes it and shows every control.
func journeyFilterToggle(l live, f *JourneyFilterView, open bool) ui.Node {
	label := f.ToggleLabel
	if label == "" {
		label = "Filters"
	}
	props := html.Props{Type: "button", Class: "jn-btn jn-journey-filter-toggle",
		DataAttr: html.DataAttribute{Name: "variant", Value: "secondary"},
		Aria:     map[string]string{"expanded": strconv.FormatBool(open), "controls": journeyFilterPanelID}}
	if l.onFieldChange != nil {
		next := "open"
		if open {
			next = "closed"
		}
		change := l.onFieldChange
		props.OnClick = ui.UseEvent(func(e ui.MouseEvent) {
			e.PreventDefault()
			change(JourneyFilterPanelField, next)
		})
	}
	return html.Button(props, html.Text(label))
}

// journeyFilterEmpty is the filtered-empty result: it names the filter as
// the reason and offers the one step that recovers the list.
func journeyFilterEmpty(f *JourneyFilterView) ui.Node {
	children := []ui.Node{
		RenderIcon(IconEmpty, "jn-empty-mark", nil),
		html.P(html.Props{Class: "jn-empty-title"}, html.Text(f.EmptyTitle)),
		html.P(html.Props{}, html.Text(f.EmptyDetail)),
	}
	if f.ClearHref != "" {
		children = append(children, html.A(html.Props{Class: "jn-btn", Href: f.ClearHref, OnClick: activate(f.OnClear),
			DataAttr: html.DataAttribute{Name: "variant", Value: "secondary"}}, html.Text(f.ClearLabel)))
	}
	return html.Div(html.Props{Class: "jn-panel jn-empty jn-journey-filter-empty", Role: "status"}, children...)
}
