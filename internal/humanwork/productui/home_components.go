package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type HomePageProps struct {
	Work              WorkCollectionProps
	ShowWork          bool
	Overview          SummaryCardProps
	ShowOverview      bool
	QuickStart        QuickActionsProps
	Drafts            WorkCollectionProps
	ShowDrafts        bool
	DraftsEmptyTitle  string
	DraftsEmptyDetail string
	Tracked           TrackedRequestsProps
	ShowTracked       bool
	RecentPeople      RecentPeopleProps
	ShowPeople        bool
	Recent            RecentActivityProps
	ShowRecent        bool
	// CompactEmpty keeps a quiet workspace focused on its next authorized
	// action. It is set by the server-backed page projection, not inferred
	// from client state.
	CompactEmpty bool
	EmptyTitle   string
	EmptyDetail  string
}

type SummaryCardProps struct {
	Title       string
	Description string
	Facts       []FactProps
}

type QuickActionsProps struct {
	Title   string
	Class   string
	Actions []ActionLinkProps
}

type RecentActivityProps struct {
	Title            string
	Items            []ActivityProps
	EmptyTitle       string
	EmptyDescription string
}

type RecentPeopleProps struct {
	Title       string
	Description string
	Items       []RecentPerson
	EmptyTitle  string
	EmptyDetail string
}

func HomePage(props HomePageProps) ui.Node {
	if props.CompactEmpty {
		return homeEmptyPage(props)
	}
	gridClass := "home-grid"
	primary := make([]ui.Node, 0, 6)
	if props.ShowWork {
		primary = append(primary, ui.CreateElement(WorkCollection, props.Work))
	} else {
		gridClass += " home-grid-without-work"
	}
	if len(props.QuickStart.Actions) > 0 {
		primary = append(primary, ui.CreateElement(QuickActions, props.QuickStart))
	}
	if props.ShowDrafts {
		if len(props.Drafts.Rows) > 0 {
			primary = append(primary, ui.CreateElement(WorkCollection, props.Drafts))
		} else {
			primary = append(primary, ui.CreateElement(Panel, PanelProps{
				Title: props.Drafts.Title, Class: "home-continuity-card home-drafts-card",
				Body: homeCardEmpty(props.DraftsEmptyTitle, props.DraftsEmptyDetail),
			}))
		}
	}
	if props.ShowTracked {
		primary = append(primary, ui.CreateElement(TrackedRequests, props.Tracked))
	}
	if props.ShowRecent {
		primary = append(primary, ui.CreateElement(RecentActivity, props.Recent))
	}
	primaryRail := html.Div(html.Props{Class: "home-primary-rail side-stack"}, primary...)
	supporting := make([]ui.Node, 0, 2)
	if props.ShowOverview {
		supporting = append(supporting, ui.CreateElement(SummaryCard, props.Overview))
	}
	if props.ShowPeople {
		supporting = append(supporting, ui.CreateElement(RecentPeoplePanel, props.RecentPeople))
	}
	return html.Div(html.Props{}, html.Div(html.Props{Class: gridClass}, primaryRail, html.Div(html.Props{Class: "home-supporting-rail side-stack"}, supporting...)))
}

func homeEmptyPage(props HomePageProps) ui.Node {
	primary := make([]ui.Node, 0, 2)
	if len(props.QuickStart.Actions) > 0 {
		actions := append([]ActionLinkProps(nil), props.QuickStart.Actions...)
		actions[0].Class = "button primary"
		primary = append(primary, ui.CreateElement(QuickActions, QuickActionsProps{
			Title: props.QuickStart.Title, Class: "home-quick-actions home-empty-primary", Actions: actions,
		}))
	}
	continuityTitle := props.EmptyTitle
	if continuityTitle == "" {
		continuityTitle = props.Drafts.Title
	}
	continuityDetail := props.EmptyDetail
	if continuityDetail == "" {
		continuityDetail = props.DraftsEmptyDetail
	}
	primary = append(primary, ui.CreateElement(Panel, PanelProps{
		Title: continuityTitle, Class: "home-continuity-summary",
		Body: html.P(html.Props{Class: "muted"}, ui.Text(continuityDetail)),
	}))
	return html.Div(html.Props{}, html.Div(html.Props{Class: "home-grid home-grid-empty"},
		html.Div(html.Props{Class: "home-primary-rail side-stack"}, primary...),
	))
}

func homeCardEmpty(title, detail string) ui.Node {
	return html.Div(html.Props{Class: "collection-empty home-card-empty"},
		html.Strong(html.Props{}, ui.Text(title)),
		html.Small(html.Props{}, ui.Text(detail)),
	)
}

func SummaryCard(props SummaryCardProps) ui.Node {
	body := []ui.Node{}
	if props.Description != "" {
		body = append(body, html.P(html.Props{Class: "summary-scope muted"}, ui.Text(props.Description)))
	}
	body = append(body, FactList(props.Facts))
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: html.Div(html.Props{}, body...)})
}

func QuickActions(props QuickActionsProps) ui.Node {
	actions := make([]ui.Node, 0, len(props.Actions))
	for _, action := range props.Actions {
		actions = append(actions, ui.CreateElement(ActionLink, action))
	}
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Class: props.Class, Body: html.Div(html.Props{Class: "quick-actions"}, actions...)})
}

func RecentActivity(props RecentActivityProps) ui.Node {
	return ui.CreateElement(Panel, PanelProps{
		Title: props.Title,
		Body:  ActivityList(props.Items, props.EmptyTitle, props.EmptyDescription),
	})
}

func RecentPeoplePanel(props RecentPeopleProps) ui.Node {
	if len(props.Items) == 0 {
		return ui.CreateElement(Panel, PanelProps{
			Title: props.Title, Class: "recent-people-panel home-continuity-card",
			Body: homeCardEmpty(props.EmptyTitle, props.EmptyDetail),
		})
	}
	rows := make([]ui.Node, 0, len(props.Items))
	for _, person := range props.Items {
		identity := html.Strong(html.Props{}, ui.Text(person.Name))
		if person.Href != "" {
			identity = softwareLink(person.Navigate, html.Props{}, person.Href, ui.Text(person.Name))
		}
		rows = append(rows, html.Li(html.Props{Class: "activity"},
			personAvatar(person.Name, person.Initials, person.PhotoURL, ""),
			html.Span(html.Props{Class: "row-main"}, identity, html.Small(html.Props{}, ui.Text(person.Role))),
		))
	}
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Class: "recent-people-panel", Body: html.Div(html.Props{}, html.P(html.Props{Class: "muted recent-people-intro"}, ui.Text(props.Description)), html.Ul(html.Props{Class: "recent", Raw: map[string]any{"role": "list"}}, rows...))})
}
