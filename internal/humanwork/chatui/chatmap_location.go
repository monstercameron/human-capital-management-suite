package chatui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatmapComposerControl(m Model, id string, disabled bool) ui.Node {
	if !composerLocationAvailable(m) {
		return nil
	}
	return ChatmapShareSheet(m.Locale, id, m.CurrentTenantID, m.SelectedID, disabled)
}
func ChatmapShareSheet(locale, id, tenant, conversation string, disabled bool) ui.Node {
	t := func(k string) string { return ChatmapText(locale, k) }
	button := func(action, key string) ui.Node {
		return html.Button(html.Props{Type: "button", Class: "button secondary small", Text: t(key), Title: t(key), Disabled: disabled, Data: map[string]string{"chatmap-action": action}})
	}
	option := func(value, key string) ui.Node { return html.Option(html.Props{Value: value, Text: t(key)}) }
	segments := []ui.Node{}
	for _, entry := range []struct{ source, key string }{{"device", "where"}, {"typed_address", "address"}, {"job_site", "site"}} {
		segments = append(segments, html.Button(html.Props{Type: "button", Text: t(entry.key), Data: map[string]string{"chatmap-action": "source", "source": entry.source}, Aria: map[string]string{"pressed": boolString(entry.source == "device")}}))
	}
	nudges := []ui.Node{}
	for _, key := range []string{"north", "south", "west", "east"} {
		nudges = append(nudges, button(key, key))
	}
	panel := anchoredChatLayer(html.Props{Class: "chatmap-sheet", Dir: chatmapDirection(locale), ID: id + "-location", Hidden: true, Role: "dialog", Data: map[string]string{"chatmap-sheet": "true", "tenant": tenant, "conversation": conversation, "locale": locale, "chatmap-source": "device", "chatmap-precision": "approximate"}, Aria: map[string]string{"label": t("location")}}, "location",
		html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{Text: t("location")}), button("sheet-close", "close")),
		html.P(html.Props{Text: t("explain")}),
		html.Div(html.Props{Class: "chatmap-segments", Role: "group", Aria: map[string]string{"label": t("location")}}, segments...),
		html.Div(html.Props{Class: "chatmap-choice", Data: map[string]string{"chatmap-choice": "device"}}, button("device", "device")),
		html.Div(html.Props{Class: "chatmap-choice", Hidden: true, Data: map[string]string{"chatmap-choice": "typed_address"}}, html.Label(html.Props{}, ui.Text(t("address")), html.Input(html.Props{Type: "text", Class: "chat-input", Data: map[string]string{"chatmap-field": "readable"}, Aria: map[string]string{"describedby": id + "-location-status"}})), button("lookup", "find"), html.Select(html.Props{Hidden: true, Data: map[string]string{"chatmap-field": "lookup"}, Aria: map[string]string{"label": t("address")}}), html.P(html.Props{Class: "field-hint", Hidden: true, Data: map[string]string{"chatmap-unavailable": "lookup"}, Text: t("lookup")})),
		html.Div(html.Props{Class: "chatmap-choice", Hidden: true, Data: map[string]string{"chatmap-choice": "job_site"}}, html.Label(html.Props{}, ui.Text(t("site")), html.Select(html.Props{Data: map[string]string{"chatmap-field": "siteid"}}, html.Option(html.Props{Text: t("site")}))), html.P(html.Props{Class: "field-hint", Hidden: true, Data: map[string]string{"chatmap-unavailable": "sites"}, Text: t("sites_unavailable")})),
		html.Div(html.Props{Class: "chatmap-preview-frame", TabIndex: 0, Role: "group", Aria: map[string]string{"label": t("preview")}, Data: map[string]string{"chatmap-preview-region": "true"}}, html.Div(html.Props{Class: "chatmap-preview"}, icon("pin"), html.Img(html.Props{Class: "chatmap-picture", Hidden: true, Alt: t("preview"), Width: "400", Height: "200", Data: map[string]string{"chatmap-preview": "true"}})), html.Div(html.Props{Class: "chatmap-nudges"}, nudges...)),
		html.Label(html.Props{}, ui.Text(t("precision")), html.Select(html.Props{Data: map[string]string{"chatmap-field": "precision"}}, option("approximate", "approximate"), option("exact", "exact"))),
		html.Label(html.Props{}, ui.Text(t("note")), html.Textarea(html.Props{Rows: 2, Data: map[string]string{"chatmap-field": "note"}, Aria: map[string]string{"describedby": id + "-location-status"}})),
		html.Label(html.Props{}, ui.Text(t("duration")), html.Select(html.Props{Data: map[string]string{"chatmap-field": "duration"}}, option("hour", "hour"), option("day", "day"), option("keep", "keep"), option("live15", "live15"), option("live60", "live60"), option("live480", "live480"))),
		html.P(html.Props{Class: "field-hint", Text: t("retention")}),
		html.P(html.Props{Class: "field-hint", Hidden: true, Data: map[string]string{"chatmap-live-note": "true"}, Text: t("live_note")}),
		html.P(html.Props{ID: id + "-location-status", Role: "status", Aria: map[string]string{"live": "polite"}, Data: map[string]string{"chatmap-status": "true"}}),
		html.Button(html.Props{Type: "button", Class: "chatmap-send", Text: t("send"), Disabled: true, Data: map[string]string{"chatmap-action": "send"}}),
		ChatmapSharingSection(locale))
	return html.Div(html.Props{Class: "chatmap-control"}, html.Button(html.Props{Type: "button", Class: "tool-button", TabIndex: -1, Title: t("location"), Disabled: disabled, Data: map[string]string{"chatmap-action": "toggle"}, Aria: map[string]string{"label": t("location"), "expanded": "false", "controls": id + "-location"}}, icon("pin")), panel)
}

type ChatmapEmbed struct {
	MessageRevision                                                   uint64
	Share                                                             chat.LocationShare
	Sharer, PictureURL, Attribution, ViewerID, ViewerTenantID, Locale string
	Now                                                               time.Time
	Origin                                                            string
}

func ChatmapLocationEmbed(v ChatmapEmbed) ui.Node {
	t := func(k string) string { return ChatmapText(v.Locale, k) }
	s := v.Share
	if strings.TrimSpace(s.Place.Label) == "" {
		s.Place.Label = t("location")
	}
	if v.Attribution == "" {
		v.Attribution = t("schematic")
	}
	props := html.Props{Class: "chatmap-card", Dir: chatmapDirection(v.Locale), Aria: map[string]string{"live": "polite"}, Data: map[string]string{"share": s.ID, "tenant": s.TenantID, "conversation": s.ConversationID, "post": s.PostID, "locale": v.Locale}}
	if s.ExpiresAt != nil {
		props.Data["expires"] = strconv.FormatInt(s.ExpiresAt.UnixMilli(), 10)
	}
	if s.Live && !s.Ended {
		props.Data["live"] = "true"
	}
	if s.Ended || (s.ExpiresAt != nil && !v.Now.Before(*s.ExpiresAt)) {
		gone := []ui.Node{html.P(html.Props{Text: t("expired")})}
		// The card says when sharing ended; where it was is not kept.
		endedAt := s.EndedAt
		if endedAt == nil && s.ExpiresAt != nil && !v.Now.Before(*s.ExpiresAt) {
			endedAt = s.ExpiresAt
		}
		if endedAt != nil {
			gone = append(gone, html.P(html.Props{Data: map[string]string{"chatmap-ended-ms": strconv.FormatInt(endedAt.UnixMilli(), 10)}, Text: fmt.Sprintf(t("ended_at"), endedAt.UTC().Format("15:04 MST"))}))
		}
		return html.Article(props, gone...)
	}
	// A typed address shared without a position has no pin to draw; the
	// address itself is the message and the map says it is unavailable.
	if s.Place.Position == nil && strings.TrimSpace(s.Place.Address) == "" {
		return html.Article(props, html.P(html.Props{Text: t("unavailable")}))
	}
	p := s.Place.Position
	if !ChatmapOwnPictureURL(v.PictureURL, v.Origin) {
		v.PictureURL = ""
	}
	accuracy, coordinates := "", ""
	if p != nil {
		accuracy = fmt.Sprintf(t("within"), strconv.FormatFloat(chat.LocationAccuracy(s.Place), 'f', 0, 64))
		coordinates = fmt.Sprintf("%.5f, %.5f", p.Latitude, p.Longitude)
	}
	sharedAt := s.SharedAt
	if sharedAt.IsZero() {
		sharedAt = s.Place.CapturedAt
	}
	age := fmt.Sprintf(t("age"), strconv.Itoa(max(0, int(v.Now.Sub(sharedAt).Minutes()))))
	by := fmt.Sprintf(t("by"), v.Sharer)
	facts := chatmapJoin(accuracy, age, by)
	alternative := chatmapJoin(s.Place.Label, s.Place.Address, facts, coordinates, t("unavailable"), v.Attribution)
	nodes := []ui.Node{
		html.Img(html.Props{Class: "chatmap-picture", Src: v.PictureURL, Alt: alternative, Width: "400", Height: "200", Raw: map[string]any{"loading": "lazy"}}),
		html.Strong(html.Props{Dir: "auto", Text: s.Place.Label}),
		html.P(html.Props{Dir: "auto", Text: s.Place.Address, Data: map[string]string{"chatmap-address": "true"}}),
		html.P(html.Props{Text: facts}),
		html.P(html.Props{Text: alternative}),
		html.P(html.Props{Text: v.Attribution}),
	}
	if v.PictureURL == "" {
		nodes = append(nodes, html.P(html.Props{Role: "status", Text: t("unavailable")}))
	}
	if s.Live {
		nodes = append(nodes, html.P(html.Props{Class: "chatmap-live", Text: t("live")}))
		if s.Paused {
			nodes = append(nodes, html.P(html.Props{Role: "status", Text: t("paused")}))
		}
	}
	if s.Place.Source == chat.LocationAgent || s.SharerKind == "agent" {
		nodes = append(nodes, html.P(html.Props{Text: t("agent_supplied")}))
	}
	if s.Place.Source == chat.LocationIntegration || s.SharerKind == "integration" || s.SharerKind == "service" {
		nodes = append(nodes, html.P(html.Props{Text: t("integration_supplied")}))
	}
	actions := []ui.Node{}
	for _, k := range []string{"open", "copy"} {
		actions = append(actions, html.Button(html.Props{Type: "button", Text: t(k), Disabled: k == "copy" && s.Place.Address == "", Data: map[string]string{"chatmap-action": k}}))
	}
	directions := "geo:0,0?q=" + url.QueryEscape(s.Place.Address)
	if p != nil {
		directions = fmt.Sprintf("geo:%.6f,%.6f?q=%s", p.Latitude, p.Longitude, url.QueryEscape(s.Place.Label))
	}
	actions = append(actions, html.A(html.Props{Text: t("directions"), Href: directions}))
	if s.SharerID == v.ViewerID && s.SharerTenantID == v.ViewerTenantID {
		actions = append(actions, html.Button(html.Props{Type: "button", Text: t("stop"), Data: map[string]string{"chatmap-action": "stop"}}))
		if s.ExpiresAt != nil {
			nodes = append(nodes, html.P(html.Props{Text: fmt.Sprintf(t("remaining"), strconv.Itoa(max(0, int(s.ExpiresAt.Sub(v.Now).Minutes()))))}))
		}
	}
	nodes = append(nodes, html.Div(html.Props{Class: "chatmap-actions"}, actions...))
	controls := []ui.Node{}
	for _, key := range []string{"zoom_in", "zoom_out", "north", "south", "east", "west", "close"} {
		controls = append(controls, html.Button(html.Props{Type: "button", Text: t(key), Data: map[string]string{"chatmap-action": key}}))
	}
	nodes = append(nodes, html.Dialog(html.Props{Class: "chatmap-large", Aria: map[string]string{"label": s.Place.Label}}, html.Img(html.Props{Class: "chatmap-picture", Alt: alternative, Width: "800", Height: "400", Data: map[string]string{"chatmap-large-picture": "true"}}), html.Div(html.Props{Class: "chatmap-actions"}, controls...)))
	return html.Article(props, nodes...)
}

// ChatmapSlashCommand reports whether a composer line is the /location command.
// The command registry (composer_commands.go) owns the name; this reads it.
func ChatmapSlashCommand(body string) bool {
	name, args, ok := parseComposerCommand(body)
	return ok && strings.EqualFold(name, "location") && args == ""
}
func ChatmapOwnPictureURL(raw, origin string) bool {
	if raw == "" {
		return true
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.ContainsAny(raw, "?\\#") {
		return true
	}
	return (strings.HasPrefix(origin, "https://") || strings.HasPrefix(origin, "http://")) && strings.HasPrefix(raw, "blob:"+origin+"/")
}

// chatmapJoin joins the non-empty parts with the card's separator.
func chatmapJoin(parts ...string) string {
	kept := []string{}
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

func chatmapDirection(locale string) string {
	if strings.HasPrefix(locale, "ar") {
		return "rtl"
	}
	return "ltr"
}
