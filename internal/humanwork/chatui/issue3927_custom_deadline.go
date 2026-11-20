package chatui

import (
	"errors"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A poll can close at an exact date and clock time the author picks in the
// preview, beside the presets (#3927). The two fields are read when the person
// presses "Set closing time"; the time is worked out once in the page's zone,
// kept with the preview's settings like a preset (#3926), shown on the card
// with its zone (#3925), and checked again when Post is pressed and by the
// server. A time that is past, does not exist or happens twice is refused in
// words and what was typed stays in the fields.

const (
	settingCustomDate = "custom-date"
	settingCustomTime = "custom-time"
	closesCustom      = "custom"
)

// chatcmd003CustomError is the preview's words for why a typed closing time
// was refused.
func chatcmd003CustomError(err error) string {
	switch {
	case errors.Is(err, chat.ErrDeadlinePast):
		return "custom-past"
	case errors.Is(err, chat.ErrDeadlineNotExists):
		return "custom-gap"
	case errors.Is(err, chat.ErrDeadlineAmbiguous):
		return "custom-ambiguous"
	}
	return "custom-invalid"
}

// chatcmd003CustomSet takes date and clock as the closing time. The fields'
// text is kept in the settings whether or not it was accepted, so a refusal
// does not empty them, and the poll's options are not touched.
func chatcmd003CustomSet(m Model, state chatcmd003Preview, date, clock string, now time.Time) chatcmd003Preview {
	settings := map[string]string{}
	for name, value := range state.Settings {
		settings[name] = value
	}
	settings["closes"], settings[settingCustomDate], settings[settingCustomTime] = closesCustom, date, clock
	delete(settings, settingClosesAt)
	at, err := chat.Chatcmd004ResolveDateTime(date, clock, chatcmd003Location(m), now)
	if err == nil {
		settings[settingClosesAt] = at.Format(time.RFC3339Nano)
	}
	state.Settings = settings
	next := chatcmd003Draw(m, state, state.Draft.Card.Kind, state.Draft.Raw)
	if err != nil {
		next.Error = chatcmd003CustomError(err)
	}
	return next
}

// chatcmd003CustomProblem is the refusal for a Custom choice that cannot be
// posted as it stands, or "" when it can (or Custom is not chosen). The time is
// read again against now so a draft that sat past it is not posted.
func chatcmd003CustomProblem(settings map[string]string, now time.Time) string {
	if settings["closes"] != closesCustom {
		return ""
	}
	at, err := time.Parse(time.RFC3339Nano, settings[settingClosesAt])
	if err != nil {
		return "custom-needed"
	}
	if !at.After(now) {
		return "custom-past"
	}
	return ""
}

// chatcmd003CustomKeep carries an applied custom time over a press on the
// Custom choice when it is already the chosen one.
func chatcmd003CustomKeep(settings, before map[string]string) {
	if settings["closes"] == closesCustom && before["closes"] == closesCustom && before[settingClosesAt] != "" {
		settings[settingClosesAt] = before[settingClosesAt]
	}
}

// chatcmd003CustomZone says which zone the typed time is in and that readers
// elsewhere see the same moment on their own clocks.
func chatcmd003CustomZone(m Model, at time.Time) string {
	key := "custom-zone-yours"
	if !chatcmd002ZoneValid(m) {
		key = "custom-zone-device"
	}
	zone := chatcmd003Location(m).String()
	if zone == "Local" {
		zone = ""
	}
	return chatcmd002Fill(chatcmd003Text(m, key), "zone", zone, "offset", chatcmd002Offset(at))
}

// chatcmd003CustomFields is the date and time fields, the zone, and the time
// they resolved to. It is drawn only while Custom is the chosen closing time.
func chatcmd003CustomFields(m Model, state chatcmd003Preview) ui.Node {
	if state.Draft.Card.Poll == nil || state.Settings["closes"] != closesCustom {
		return nil
	}
	now := time.Now().In(chatcmd003Location(m))
	aria := map[string]string{"describedby": "chatcmd003-custom-zone"}
	if len(state.Error) > 7 && state.Error[:7] == "custom-" {
		aria["describedby"] += " chatcmd003-error"
		aria["invalid"] = "true"
	}
	field := func(id, label, kind, setting string, extra map[string]string) ui.Node {
		props := html.Props{ID: id, Class: "chat-input", Type: kind, Name: id, Required: true, Disabled: state.Busy, Aria: aria, Data: map[string]string{"chat-value": state.Settings[setting]}}
		if kind == "date" {
			props.Min = extra["min"]
		}
		return html.Div(html.Props{Class: "chatcmd003-custom-field"}, html.Label(html.Props{For: id, Text: label}), html.Input(props))
	}
	zoneNote := chatcmd003CustomZone(m, now)
	if state.Draft.Card.Poll.ClosesAt == nil {
		// Once a time is set, the closing-time note says this; once, not twice.
		zoneNote += " " + chatcmd003Text(m, "zone-everyone")
	}
	children := []ui.Node{
		html.Span(html.Props{ID: "chatcmd003-custom-label", Class: "chatcmd003-setting-label", Text: chatcmd003Text(m, "closes-custom")}),
		html.Div(html.Props{Class: "chatcmd003-custom-fields"},
			field("chatcmd003-custom-date", chatcmd003Text(m, "custom-date"), "date", settingCustomDate, map[string]string{"min": now.Format("2006-01-02")}),
			field("chatcmd003-custom-time", chatcmd003Text(m, "custom-time"), "time", settingCustomTime, nil),
			html.Button(html.Props{ID: "chatcmd003-custom-apply", Class: "button secondary small", Type: "button", Disabled: state.Busy,
				Data: map[string]string{"action": "chatcmd003-custom"}, Text: chatcmd003Text(m, "custom-apply")})),
		html.P(html.Props{ID: "chatcmd003-custom-zone", Class: "chatcmd003-note", Dir: "auto", Text: zoneNote}),
	}
	if p := state.Draft.Card.Poll; p.ClosesAt != nil {
		children = append(children, html.P(html.Props{ID: "chatcmd003-custom-resolved", Class: "chatcmd003-note", Role: "status", Dir: "auto", Aria: map[string]string{"live": "polite"},
			Text: chatcmd002Fill(chatcmd003Text(m, "custom-resolved"), "date", chatcmd002Deadline(m, *p.ClosesAt))}))
	}
	return html.Div(html.Props{ID: "chatcmd003-custom", Class: "chatcmd003-setting chatcmd003-custom", Role: "group", Aria: map[string]string{"labelledby": "chatcmd003-custom-label"}}, children...)
}
