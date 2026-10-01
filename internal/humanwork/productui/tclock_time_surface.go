package productui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TimeSurfaceState says whether the time service supplied a usable
// projection. The zero value is unavailable, so a surface that was never
// given data fails closed to a plain explanation instead of an empty grid.
type TimeSurfaceState uint8

const (
	// TimeSurfaceUnavailable is the fail-closed default.
	TimeSurfaceUnavailable TimeSurfaceState = iota
	// TimeSurfaceReady means the projection is authorized and current.
	TimeSurfaceReady
)

// TimeFact is one labelled value: a total, a version, a last-seen moment.
// Both halves arrive already localized; the renderer never computes time.
type TimeFact struct{ Label, Value string }

// Tones a surface may put on a status pill. Colour reinforces the words and
// is never the only carrier of meaning.
const (
	TimeToneOK      = "ok"
	TimeToneWarn    = "warn"
	TimeToneBad     = "bad"
	TimeToneInfo    = "info"
	TimeToneNeutral = "neutral"
)

// validTimeHref keeps every destination on the time surfaces relative and
// inside the time area, so a projection can never send a worker elsewhere.
func validTimeHref(href string) bool {
	parsed, err := url.Parse(href)
	if err != nil || parsed.IsAbs() || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" || parsed.Path == "" {
		return false
	}
	return parsed.Path == "/workspace/app/time" || strings.HasPrefix(parsed.Path, "/workspace/app/time/")
}

func timeLink(view View, action *ActionLinkProps, class string) ui.Node {
	if action == nil || action.Label == "" || !validTimeHref(action.Href) {
		return nil
	}
	copy := *action
	if class != "" {
		copy.Class = class
	}
	if copy.Navigate == nil {
		copy.Navigate = view.Navigate
	}
	return ActionLink(copy)
}

// timeLinkFor is timeLink with the person or item the action applies to added
// as text only a screen reader hears, so a column of identical "Review
// timecard" buttons is not a column of identical announcements.
func timeLinkFor(view View, action *ActionLinkProps, class, subject string) ui.Node {
	if action == nil || action.Label == "" || !validTimeHref(action.Href) {
		return nil
	}
	navigate := action.Navigate
	if navigate == nil {
		navigate = view.Navigate
	}
	children := []ui.Node{ui.Text(action.Label)}
	if strings.TrimSpace(subject) != "" {
		children = append(children, html.Span(html.Props{Class: "sr-only"}, ui.Text(" — "+subject)))
	}
	return softwareLink(navigate, html.Props{Class: class}, action.Href, children...)
}

func timePill(tone, label string) ui.Node {
	if tone == "" {
		tone = TimeToneNeutral
	}
	return html.Span(html.Props{Class: "time-pill", Data: map[string]string{"tone": tone}},
		html.Span(html.Props{Class: "time-pill-dot", Raw: map[string]any{"aria-hidden": "true"}}), ui.Text(label))
}

// timeNotice is the one message box: error, success or plain information,
// announced by role so a screen reader hears what a sighted reader sees.
func timeNotice(kind, text string) ui.Node {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	role := "status"
	class := "clock-notice clock-notice-success"
	switch kind {
	case "error":
		role, class = "alert", "clock-notice clock-notice-error"
	case "info":
		class = "clock-notice clock-notice-busy"
	}
	return html.P(html.Props{Class: class, Role: role}, ui.Text(text))
}

func timeHead(page PageID, id, title, subtitle string) ui.Node {
	return html.Div(html.Props{Class: "page-head time-head", Data: map[string]string{"hcm-page": string(page)}},
		html.Div(html.Props{},
			html.H1(html.Props{ID: id}, ui.Text(title)),
			html.P(html.Props{Class: "subtitle"}, ui.Text(subtitle)),
		),
	)
}

// timeRetryButton asks the same route again. It is a button, not a link, and
// records nothing: an unavailable surface never offers a destination.
func timeRetryButton(view View, page PageID, label string) ui.Node {
	if _, ok := LookupPage(page); !ok || view.Navigate == nil {
		return html.Fragment()
	}
	href := statefulHref(view, page)
	return html.Button(html.Props{Class: "button secondary time-retry", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) { view.Navigate(href) })}, ui.Text(label))
}

func timeUnavailable(view View, page PageID, id, title, subtitle, heading, help string) ui.Node {
	return html.Section(html.Props{Class: "time-page time-unavailable-page", Raw: map[string]any{"aria-labelledby": id}},
		timeHead(page, id, title, subtitle),
		html.Div(html.Props{Class: "surface clock-unavailable", Role: "status"},
			html.Span(html.Props{Class: "clock-unavailable-mark", Raw: map[string]any{"aria-hidden": "true"}}, productIcon("clock", "clock-unavailable-icon")),
			html.Div(html.Props{},
				html.H2(html.Props{}, ui.Text(heading)),
				html.P(html.Props{Class: "muted"}, ui.Text(help)),
				timeRetryButton(view, page, timeText(view.Locale, "retry")),
			),
		),
	)
}

func timeFacts(class string, facts []TimeFact) ui.Node {
	rows := make([]ui.Node, 0, len(facts))
	for _, fact := range facts {
		if strings.TrimSpace(fact.Value) == "" {
			continue
		}
		rows = append(rows, html.Div(html.Props{Class: "time-fact"},
			html.Tag("dt", html.Props{}, ui.Text(fact.Label)),
			html.Tag("dd", html.Props{}, html.Tag("bdi", html.Props{}, ui.Text(fact.Value))),
		))
	}
	if len(rows) == 0 {
		return nil
	}
	return html.Tag("dl", html.Props{Class: "time-facts " + class}, rows...)
}

func timeNodes(nodes ...ui.Node) []ui.Node {
	out := make([]ui.Node, 0, len(nodes))
	for _, node := range nodes {
		if node != nil {
			out = append(out, node)
		}
	}
	return out
}

// timeConfirmPanel is the shared "are you sure" step: it names what will
// happen, offers the named action and offers the way back. Destructive
// confirmations use the destructive variant and the danger surface.
func timeConfirmPanel(id, title, body string, destructive bool, yes, no ui.Node) ui.Node {
	class := "time-confirm"
	if destructive {
		class += " time-confirm-danger"
	}
	return html.Div(html.Props{Class: class, Role: "group", Raw: map[string]any{"aria-labelledby": id}},
		html.P(html.Props{ID: id, Class: "time-confirm-title"}, ui.Text(title)),
		html.P(html.Props{Class: "time-confirm-body"}, ui.Text(body)),
		html.Div(html.Props{Class: "time-confirm-actions"}, yes, no),
	)
}

// timeCountKey picks the singular copy key (key_one) when exactly one item is
// counted; the plural key carries the {count} placeholder.
func timeCountKey(key string, count int) string {
	if count == 1 {
		if _, ok := timeCopyEN[key+"_one"]; ok {
			return key + "_one"
		}
	}
	return key
}
