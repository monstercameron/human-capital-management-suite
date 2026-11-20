package chatui

// #3867: on a phone the thread, details, person and Saved panels each fill the
// screen. The conversation (or the list) under them is still in the page, so
// Tab walked onto controls nobody could see. While such a panel covers the
// screen the page behind it is made inert and focus goes into the panel; when
// it closes the page is live again and focus goes back to the control that
// opened it.

// issue3867Panel is one panel as measured in the page.
type issue3867Panel struct {
	Kind    string
	Visible bool
	Width   float64
}

// issue3867PanelSelectors lists the panels in stacking order, lowest first: the
// last one that covers the screen is the one that owns focus.
var issue3867PanelSelectors = []struct{ Kind, Selector string }{
	{"details", ".chat-side.chat-details"},
	{"person", ".chat-side.person-pane"},
	{"thread", ".chat-side.thread-pane"},
	{"saved", ".chatsave-panel:not([hidden])"},
}

// issue3867FocusSelector finds the first control a panel can hand focus to.
const issue3867FocusSelector = "button:not([disabled]),a[href],input:not([disabled]),textarea:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex='-1'])"

// issue3867Plan says which panel owns the screen and which parts of the page
// behind it are inert. An empty Kind means no panel covers the screen.
type issue3867Plan struct {
	Kind  string
	Inert []string
}

func issue3867PlanPanels(panels []issue3867Panel, viewportWidth float64) issue3867Plan {
	for i := len(panels) - 1; i >= 0; i-- {
		p := panels[i]
		if !p.Visible || viewportWidth <= 0 || p.Width < viewportWidth-1 {
			continue
		}
		if p.Kind == "saved" {
			// The Saved panel sits over the whole layout, side panels included.
			return issue3867Plan{Kind: p.Kind, Inert: []string{".chat-layout", ".chat-skip"}}
		}
		return issue3867Plan{Kind: p.Kind, Inert: []string{".chat-main", ".chat-rail", ".chat-skip"}}
	}
	return issue3867Plan{}
}
