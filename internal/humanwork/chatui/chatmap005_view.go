package chatui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ChatmapSharingSection is the part of the location sheet that lists what the
// person is sharing now (with a way to stop each) and opens the channel's crew
// map. Both regions are filled by the browser code after a press; nothing is
// requested until then.
func ChatmapSharingSection(locale string) ui.Node {
	t := func(k string) string { return ChatmapText(locale, k) }
	return html.Div(html.Props{Class: "chatmap-sharing", Data: map[string]string{"chatmap-sharing-section": "true"}},
		html.Div(html.Props{Class: "chatmap-actions"},
			html.Button(html.Props{Type: "button", Text: t("mine"), Data: map[string]string{"chatmap-action": "sharing"}}),
			html.Button(html.Props{Type: "button", Text: t("crew_map"), Data: map[string]string{"chatmap-action": "crewmap"}})),
		html.Div(html.Props{Role: "region", Aria: map[string]string{"label": t("mine"), "live": "polite"}, Data: map[string]string{"chatmap-sharing": "true"}}),
		html.Div(html.Props{Role: "region", Aria: map[string]string{"label": t("crew_map"), "live": "polite"}, Data: map[string]string{"chatmap-crew": "true"}}))
}

// ChatmapCrewEntry is one line of the crew map's list.
type ChatmapCrewEntry struct {
	Number int
	Name   string
	Share  chat.LocationShare
	Site   *chat.LocationSite
}

// ChatmapCrewView is the channel's map: the picture (drawn by the product, from
// its own origin), then a list that carries everything the picture does. Each
// person's line opens their message. Nobody is listed without a live share.
func ChatmapCrewView(locale string, entries []ChatmapCrewEntry, pictureURL, origin string, now time.Time) ui.Node {
	t := func(k string) string { return ChatmapText(locale, k) }
	dir := chatmapDirection(locale)
	if len(entries) == 0 {
		return html.P(html.Props{Dir: dir, Text: t("crew_empty")})
	}
	people, sites := 0, 0
	items := []ui.Node{}
	for _, e := range entries {
		if e.Site != nil {
			sites++
			items = append(items, html.Li(html.Props{Dir: "auto", Text: fmt.Sprintf("%d. %s · %s · %s", e.Number, e.Site.Label, t("crew_sites"), e.Site.Address)}))
			continue
		}
		people++
		s := e.Share
		since := s.SharedAt
		if s.PositionAt != nil {
			since = *s.PositionAt
		}
		line := fmt.Sprintf("%d. %s · %s · %s", e.Number, e.Name, fmt.Sprintf(t("within"), strconv.FormatFloat(chat.LocationAccuracy(s.Place), 'f', 0, 64)), fmt.Sprintf(t("age"), strconv.Itoa(max(0, int(now.Sub(since).Minutes())))))
		if s.Paused {
			line += " · " + t("paused")
		}
		items = append(items, html.Li(html.Props{Dir: "auto"},
			html.Span(html.Props{Dir: "auto", Text: line + " "}),
			html.Button(html.Props{Type: "button", Text: t("crew_open"), Data: map[string]string{"chatmap-action": "crew-open", "post": s.PostID}})))
	}
	alt := fmt.Sprintf(t("crew_alt"), strconv.Itoa(people), strconv.Itoa(sites))
	if !ChatmapOwnPictureURL(pictureURL, origin) {
		pictureURL = ""
	}
	return html.Div(html.Props{Class: "chatmap-crew-view", Dir: dir},
		html.Img(html.Props{Class: "chatmap-picture", Src: pictureURL, Alt: alt, Width: "400", Height: "300", Data: map[string]string{"chatmap-crew-picture": "true"}}),
		html.P(html.Props{Text: t("schematic")}),
		html.Ol(html.Props{Aria: map[string]string{"label": t("crew_map")}}, items...))
}

// ChatmapLiveBanner is the persistent line on the sharer's screen while a live
// share runs: who can see them, how long is left, and one press to stop.
func ChatmapLiveBanner(locale string, minutesLeft int) ui.Node {
	t := func(k string) string { return ChatmapText(locale, k) }
	return html.Div(html.Props{Class: "chatmap-live-banner", Dir: chatmapDirection(locale), Role: "status", Aria: map[string]string{"live": "polite"}, Data: map[string]string{"chatmap-live-banner": "true"}},
		html.Span(html.Props{Text: fmt.Sprintf(t("live_banner"), strconv.Itoa(max(0, minutesLeft)))}),
		html.Button(html.Props{Type: "button", Text: t("stop_live"), Data: map[string]string{"chatmap-action": "stop-all"}}))
}

// ChatmapPolicy is the channel's location settings as the form shows them.
type ChatmapPolicy struct {
	SharingEnabled, LiveEnabled, ExactAllowed bool
	MaxLiveSeconds, MaxRetentionSeconds       int
}

// ChatmapPolicyForm is the administrator's settings form. The browser shows it
// only when the server says the person may change the settings; the server
// checks again when it is saved.
func ChatmapPolicyForm(locale string, p ChatmapPolicy) ui.Node {
	t := func(k string) string { return ChatmapText(locale, k) }
	check := func(field, key string, on bool) ui.Node {
		return html.Label(html.Props{}, html.Input(html.Props{Type: "checkbox", Checked: on, Data: map[string]string{"chatmap-policy": field}}), ui.Text(" "+t(key)))
	}
	choose := func(field, key string, current int, values []int, labels []string) ui.Node {
		options := []ui.Node{}
		for i, v := range values {
			options = append(options, html.Option(html.Props{Value: strconv.Itoa(v), Selected: v == current, Text: t(labels[i])}))
		}
		return html.Label(html.Props{}, ui.Text(t(key)), html.Select(html.Props{Data: map[string]string{"chatmap-policy": field}}, options...))
	}
	return html.Div(html.Props{Class: "chatmap-policy", Dir: chatmapDirection(locale), Role: "group", Aria: map[string]string{"label": t("settings")}, Data: map[string]string{"chatmap-policy-form": "true"}},
		html.H3(html.Props{Text: t("settings")}),
		check("sharing", "set_sharing", p.SharingEnabled),
		check("live", "set_live", p.LiveEnabled),
		check("exact", "set_exact", p.ExactAllowed),
		choose("maxlive", "set_max_live", p.MaxLiveSeconds, []int{900, 3600, 28800, 86400}, []string{"d15m", "d1h", "d8h", "d24h"}),
		choose("retention", "set_retention", p.MaxRetentionSeconds, []int{3600, 28800, 86400}, []string{"d1h", "d8h", "d24h"}),
		html.P(html.Props{Class: "field-hint", Text: t("set_note")}),
		html.Button(html.Props{Type: "button", Text: t("save"), Data: map[string]string{"chatmap-action": "save-policy"}}))
}
