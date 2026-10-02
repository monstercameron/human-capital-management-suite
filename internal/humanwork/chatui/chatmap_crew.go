package chatui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATMAP-005: the channel's map has its own way in. While anyone in the
// conversation is sharing live, the header shows a small "Map" control with how
// many, and pressing it opens the crew view in Conversation details, as another
// section of that panel. Nobody sharing, no control and no section.

// chatmapLiveShares lists the live shares started to the open conversation that
// are still running, one per share, from the locations the page already read for
// the messages on screen.
func chatmapLiveShares(m Model) []ChatmapEmbed {
	if m.ChatFeatures != nil && !m.ChatFeatures.Locations {
		return nil
	}
	now := m.chatmapNow()
	seen := map[string]bool{}
	var out []ChatmapEmbed
	for _, embeds := range m.MessageLocations {
		for _, e := range embeds {
			s := e.Share
			if !s.Live || s.Ended || s.Place.Position == nil || seen[s.ID] || (s.ExpiresAt != nil && !now.Before(*s.ExpiresAt)) {
				continue
			}
			seen[s.ID] = true
			out = append(out, e)
		}
	}
	return out
}

func (m Model) chatmapNow() time.Time { return time.Now().UTC() }

// ChatmapLiveCount is how many people are sharing live to the open conversation.
func ChatmapLiveCount(m Model) int { return len(chatmapLiveShares(m)) }

// chatmapHeaderMap is the header's Map control, or nil when nobody is sharing.
func chatmapHeaderMap(m Model) ui.Node {
	n := ChatmapLiveCount(m)
	if n == 0 {
		return nil
	}
	label := fmt.Sprintf(ChatmapText(m.Locale, "header_map_label"), m.n(n))
	return actionButton("icon-button chatux001-action chatmap-header-map", "header-map", "", label, m.Callbacks.ToggleDetails == nil,
		icon("pin"),
		html.Span(html.Props{Class: "chatmap-header-word", Aria: map[string]string{"hidden": "true"}, Text: ChatmapText(m.Locale, "header_map")}),
		html.Span(html.Props{Class: "chatux001-count", Aria: map[string]string{"hidden": "true"}, Text: m.n(n)}))
}

// chatmapCrewState is what the crew view shows: nothing yet, an answer, or a
// failure with the last good answer kept.
type chatmapCrewState struct {
	Loaded, Failed bool
	View           chat.LiveMapView
	Picture        string
	// Attempt is bumped by Try again, so the read is made once more.
	Attempt int
}

type chatmapCrewProps struct {
	Locale, Tenant, Conversation, Viewer, ViewerTenant string
	// Names is the author of each message that carries a live share, by message.
	Names map[string]string
}

// chatmapCrewSection is the crew view as a section of Conversation details. It is
// absent while nobody is sharing live.
func chatmapCrewSection(m Model) ui.Node {
	shares := chatmapLiveShares(m)
	if len(shares) == 0 || m.SelectedID == "" {
		return nil
	}
	tenant := m.CurrentTenantID
	for _, c := range m.Conversations {
		if c.ID == m.SelectedID && c.HostTenantID != "" {
			tenant = c.HostTenantID
		}
	}
	names := map[string]string{}
	for _, e := range shares {
		if e.Sharer != "" {
			names[e.Share.PostID] = e.Sharer
		}
	}
	title := ChatmapText(m.Locale, "crew_map")
	return html.Section(html.Props{ID: "chat-details-crewmap", Class: "details-section chatmap-crew-section", TabIndex: -1, Dir: chatmapDirection(m.Locale), Aria: map[string]string{"label": title}},
		html.Div(html.Props{Class: "details-section-head"}, html.H3(html.Props{Text: title})),
		html.WithKey(ui.CreateElement(chatmapCrewPanel, chatmapCrewProps{Locale: m.Locale, Tenant: tenant, Conversation: m.SelectedID, Viewer: m.CurrentUser, ViewerTenant: m.CurrentTenantID, Names: names}), "chatmapcrew-"+m.SelectedID))
}

// chatmapCrewPanel reads the channel's live map when it opens and again every
// half minute while it is open, so the pins follow the people. A read that fails
// keeps what was last shown and says so, with Try again.
func chatmapCrewPanel(props chatmapCrewProps) ui.Node {
	state := ui.UseState(chatmapCrewState{})
	attempt := state.Get().Attempt
	ui.UseEffectOf(func() func() {
		return chatmapCrewWatch(props.Tenant, props.Conversation, props.Locale, props.Names, func(view chat.LiveMapView, picture string, err error) {
			current := state.Get()
			if err != nil {
				current.Failed = true
				state.Set(current)
				return
			}
			current.View, current.Picture, current.Loaded, current.Failed = view, picture, true, false
			state.Set(current)
		})
	}, struct {
		Room, Tenant string
		Attempt      int
	}{props.Conversation, props.Tenant, attempt})
	retry := ui.UseEvent(func() {
		current := state.Get()
		current.Attempt++
		current.Failed = false
		state.Set(current)
	})
	return chatmapCrewPanelView(props, state.Get(), retry)
}

// chatmapCrewPanelView draws the crew view's states: loading, the map, and a
// failure with Try again (the last good map stays above it).
func chatmapCrewPanelView(props chatmapCrewProps, s chatmapCrewState, retry ui.Handler) ui.Node {
	t := func(k string) string { return ChatmapText(props.Locale, k) }
	dir := chatmapDirection(props.Locale)
	var body ui.Node
	switch {
	case !s.Loaded && !s.Failed:
		return html.P(html.Props{Dir: dir, Role: "status", Aria: map[string]string{"busy": "true"}, Text: t("loading")})
	case !s.Loaded:
		body = nil
	default:
		entries := []ChatmapCrewEntry{}
		for _, share := range s.View.Shares {
			name := props.Names[share.PostID]
			if share.SharerID == props.Viewer && share.SharerTenantID == props.ViewerTenant {
				name = t("you")
			} else if name == "" {
				name = share.SharerID
			}
			entries = append(entries, ChatmapCrewEntry{Number: len(entries) + 1, Name: name, Share: share})
		}
		for i := range s.View.Sites {
			site := s.View.Sites[i]
			entries = append(entries, ChatmapCrewEntry{Number: len(entries) + 1, Site: &site})
		}
		body = ChatmapCrewView(props.Locale, entries, s.Picture, chatmapOrigin(), time.Now())
	}
	if !s.Failed {
		return body
	}
	return html.Div(html.Props{Dir: dir},
		body,
		html.Div(html.Props{Class: "manage-sec-error", Role: "alert"},
			html.P(html.Props{Dir: "auto", Text: t("crew_failed")}),
			html.Button(html.Props{Class: "button secondary small", Type: "button", Text: t("retry"), OnClick: retry})))
}

// chatmapPins numbers the pins of a live map the way its list does: people
// first, then job sites.
func chatmapPins(view chat.LiveMapView, names map[string]string) []chat.CrewPin {
	pins := []chat.CrewPin{}
	for _, share := range view.Shares {
		if share.Place.Position == nil {
			continue
		}
		label := names[share.PostID]
		if label == "" {
			label = strconv.Itoa(len(pins) + 1)
		}
		pins = append(pins, chat.CrewPin{Number: len(pins) + 1, Label: label, Position: *share.Place.Position, Paused: share.Paused})
	}
	for _, site := range view.Sites {
		pins = append(pins, chat.CrewPin{Number: len(pins) + 1, Label: site.Label, Position: site.Position, Site: true})
	}
	return pins
}
