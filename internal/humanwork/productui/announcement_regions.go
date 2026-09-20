package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Announcement is one message the announcements section may
// present. Assertive claims the single assertive live region;
// everything else belongs to a polite region. Messages pass
// through untouched — region membership is the governance,
// never flag rewriting.
type Announcement struct {
	ID        string
	Message   string
	Assertive bool
}

// GovernAnnouncements splits one announcement stream into the
// polite region and the assertive region in admission order.
// The first assertive claim wins; later assertive claims join
// the polite region so assistive technology never receives
// two assertive announcements from one surface.
func GovernAnnouncements(items []Announcement) (polite, assertive []Announcement) {
	polite = make([]Announcement, 0, len(items))
	assertive = make([]Announcement, 0, 1)
	claimed := false
	for _, item := range items {
		if item.Assertive && !claimed {
			claimed = true
			assertive = append(assertive, item)
			continue
		}
		polite = append(polite, item)
	}
	return polite, assertive
}

// AnnouncementsProps carries the governed announcement regions
// end to end: the polite stream and the single assertive claim,
// split by GovernAnnouncements, never hand-split by callers.
type AnnouncementsProps struct {
	Polite    []Announcement
	Assertive []Announcement
}

// Announcements renders the governed announcement regions: the
// polite live region first, then the single assertive region
// only when an assertive claim exists. Messages pass through
// untouched. An empty stream renders nothing.
func Announcements(props AnnouncementsProps) ui.Node {
	if len(props.Polite) == 0 && len(props.Assertive) == 0 {
		return ui.Fragment()
	}
	regions := make([]ui.Node, 0, 2)
	if len(props.Polite) > 0 {
		items := make([]ui.Node, 0, len(props.Polite))
		for _, item := range props.Polite {
			items = append(items, html.Li(html.Props{Class: "home-announcement"}, ui.Text(item.Message)))
		}
		regions = append(regions, html.Div(html.Props{Class: "home-announcements-polite", Raw: map[string]any{"role": "status", "aria-live": "polite", "aria-atomic": "true"}},
			html.Ul(html.Props{Class: "home-announcements-list", Raw: map[string]any{"role": "list"}}, items...)))
	}
	if len(props.Assertive) > 0 {
		items := make([]ui.Node, 0, len(props.Assertive))
		for _, item := range props.Assertive {
			items = append(items, html.Li(html.Props{Class: "home-announcement"}, ui.Text(item.Message)))
		}
		regions = append(regions, html.Div(html.Props{Class: "home-announcements-assertive", Raw: map[string]any{"role": "alert", "aria-live": "assertive", "aria-atomic": "true"}},
			html.Ul(html.Props{Class: "home-announcements-list", Raw: map[string]any{"role": "list"}}, items...)))
	}
	return html.Section(html.Props{Class: "home-announcements"}, regions...)
}

// homeAnnouncementProps resolves the announcements slot for one
// authorized projection: the governed regions plus whether the
// slot shows. The slot shows exactly when the floorplan places
// the announcements section and the stream carries at least one
// message through the governed split.
func homeAnnouncementProps(view View, polite, assertive []Announcement) (AnnouncementsProps, bool) {
	sections := make(map[string]bool, len(HomeFloorplanSections()))
	for _, section := range ResolveHomeFloorplan(view) {
		sections[section.ID] = true
	}
	props := AnnouncementsProps{Polite: polite, Assertive: assertive}
	return props, sections[HomeSectionAnnouncements] && len(polite)+len(assertive) > 0
}
