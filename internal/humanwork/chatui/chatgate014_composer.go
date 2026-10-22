package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATGATE-014: a writer addresses a message to a segment of the channel's gate
// from the composer, and the message carries a quiet tag that says who it is for.
// The choice lives in the select itself: the client reads it when the message is
// sent and puts it back to "Everyone" afterwards, so a message is never addressed
// by a choice left over from the one before.

// GateAudienceField is the id of the composer's "Visible to" select.
const GateAudienceField = "composer-audience"

const (
	gateAudienceToPrefix   = "to:"
	gateAudienceOnlyPrefix = "only:"
)

// GateAudienceChoice is one segment the writer may address, with how many people
// are in it now.
type GateAudienceChoice struct {
	Name   string
	People int
}

// GateAudienceOptions is what the writer may choose in one channel. Restricted is
// true when a message may also be made readable by the segment alone.
type GateAudienceOptions struct {
	Segments   []GateAudienceChoice
	Restricted bool
}

// MessageAudience is who a message addressed to segments is for, as the reader
// may know it. People is how many a restricted message was delivered to.
type MessageAudience struct {
	Segments   []string
	Restricted bool
	People     int
}

// ParseGateAudience reads the value of the select: no segments for "Everyone",
// else the segment named and whether the message is restricted to it.
func ParseGateAudience(value string) ([]string, bool) {
	switch {
	case strings.HasPrefix(value, gateAudienceOnlyPrefix) && len(value) > len(gateAudienceOnlyPrefix):
		return []string{strings.TrimPrefix(value, gateAudienceOnlyPrefix)}, true
	case strings.HasPrefix(value, gateAudienceToPrefix) && len(value) > len(gateAudienceToPrefix):
		return []string{strings.TrimPrefix(value, gateAudienceToPrefix)}, false
	}
	return nil, false
}

const (
	chatgate014KeyVisible  = "chat.gate014.visible_to"
	chatgate014KeyEveryone = "chat.gate014.everyone"
	chatgate014KeyTo       = "chat.gate014.to"
	chatgate014KeyOnlyOne  = "chat.gate014.only_one"
	chatgate014KeyOnlyMany = "chat.gate014.only_many"
	chatgate014KeyHint     = "chat.gate014.hint"
	chatgate014KeyTagTo    = "chat.gate014.tag_to"
	chatgate014KeyTagOne   = "chat.gate014.tag_only_one"
	chatgate014KeyTagMany  = "chat.gate014.tag_only_many"
	chatgate014KeyRefused  = "chat.gate014.refused"
)

var chatgate014ComposerCopy = map[string]map[string]string{
	"en-US": {
		chatgate014KeyVisible:  "Visible to",
		chatgate014KeyEveryone: "Everyone in this channel",
		chatgate014KeyTo:       "For {segment} · everyone can read",
		chatgate014KeyOnlyOne:  "Only {segment} (1 person)",
		chatgate014KeyOnlyMany: "Only {segment} ({n} people)",
		chatgate014KeyHint:     "A message for one group only is hidden from everyone else in the channel.",
		chatgate014KeyTagTo:    "For {segments}",
		chatgate014KeyTagOne:   "Visible to {segments} (1 person)",
		chatgate014KeyTagMany:  "Visible to {segments} ({n} people)",
		chatgate014KeyRefused:  "This message is for one group, so it can't include an agent mention or files. Choose Everyone, or send it without them.",
	},
	"de-DE": {
		chatgate014KeyVisible:  "Sichtbar für",
		chatgate014KeyEveryone: "Alle in diesem Kanal",
		chatgate014KeyTo:       "Für {segment} · alle können es lesen",
		chatgate014KeyOnlyOne:  "Nur {segment} (1 Person)",
		chatgate014KeyOnlyMany: "Nur {segment} ({n} Personen)",
		chatgate014KeyHint:     "Eine Nachricht nur für eine Gruppe ist für alle anderen im Kanal verborgen.",
		chatgate014KeyTagTo:    "Für {segments}",
		chatgate014KeyTagOne:   "Sichtbar für {segments} (1 Person)",
		chatgate014KeyTagMany:  "Sichtbar für {segments} ({n} Personen)",
		chatgate014KeyRefused:  "Diese Nachricht gilt einer Gruppe und kann keine Agenten-Erwähnung oder Dateien enthalten. Wähle „Alle“ oder sende sie ohne.",
	},
	"ar": {
		chatgate014KeyVisible:  "مرئية لـ",
		chatgate014KeyEveryone: "كل من في هذه القناة",
		chatgate014KeyTo:       "إلى {segment} · يمكن للجميع قراءتها",
		chatgate014KeyOnlyOne:  "{segment} فقط (شخص واحد)",
		chatgate014KeyOnlyMany: "{segment} فقط ({n} أشخاص)",
		chatgate014KeyHint:     "الرسالة الموجهة إلى مجموعة واحدة فقط مخفية عن بقية أعضاء القناة.",
		chatgate014KeyTagTo:    "إلى {segments}",
		chatgate014KeyTagOne:   "مرئية لـ {segments} (شخص واحد)",
		chatgate014KeyTagMany:  "مرئية لـ {segments} ({n} أشخاص)",
		chatgate014KeyRefused:  "هذه الرسالة موجهة إلى مجموعة واحدة، فلا يمكن أن تتضمن إشارة إلى وكيل أو ملفات. اختر «الجميع» أو أرسلها بدونها.",
	},
}

// GateAudienceRefusedText is the line a send says when a message for one group
// cannot go the way it was asked: it would have carried an agent mention or a
// file, which the group's messages do not carry. The page never sends it to
// everyone instead.
func GateAudienceRefusedText(locale string) string {
	return laneText(Model{Locale: locale}, chatgate014ComposerCopy, chatgate014KeyRefused)
}

// chatgate014Join writes a list of segment names in the reader's language.
func chatgate014Join(m Model, names []string) string {
	separator := ", "
	if chatEmojiLocale(m.Locale) == "ar" {
		separator = "، "
	}
	return strings.Join(names, separator)
}

// chatgate014Composer is the "Visible to" row above the composer's tools: a
// labelled select and, where a message may be limited to one group, a sentence
// that says what that does. It is absent where the writer has no segment to
// address, which is nearly every channel.
func chatgate014Composer(m Model, disabled bool) ui.Node {
	c := m.selected()
	options := m.GateAudience[c.ID]
	if c.ID == "" || c.Kind == DirectMessage || len(options.Segments) == 0 {
		return nil
	}
	rows := []ui.Node{html.Option(html.Props{Value: "", Text: laneText(m, chatgate014ComposerCopy, chatgate014KeyEveryone), Selected: true})}
	for _, segment := range options.Segments {
		rows = append(rows, html.Option(html.Props{Value: gateAudienceToPrefix + segment.Name, Text: laneTextf(m, chatgate014ComposerCopy, chatgate014KeyTo, map[string]string{"segment": segment.Name})}))
	}
	if options.Restricted {
		for _, segment := range options.Segments {
			key := chatgate014KeyOnlyMany
			if segment.People == 1 {
				key = chatgate014KeyOnlyOne
			}
			rows = append(rows, html.Option(html.Props{Value: gateAudienceOnlyPrefix + segment.Name, Text: laneTextf(m, chatgate014ComposerCopy, key, map[string]string{"segment": segment.Name, "n": strings.TrimSpace(m.n(segment.People))})}))
		}
	}
	label := laneText(m, chatgate014ComposerCopy, chatgate014KeyVisible)
	nodes := []ui.Node{
		html.Label(html.Props{Class: "composer-audience-label", For: GateAudienceField, Text: label}),
		html.Select(html.Props{ID: GateAudienceField, Name: "audience", Class: "composer-audience-select", Disabled: disabled, Dir: "auto"}, rows...),
	}
	if options.Restricted {
		nodes = append(nodes, html.P(html.Props{Class: "composer-audience-hint", Dir: "auto", Text: laneText(m, chatgate014ComposerCopy, chatgate014KeyHint)}))
	}
	return html.WithKey(html.Div(html.Props{Class: "composer-audience", Data: map[string]string{"gate-audience": "true"}}, nodes...), "composer-audience-"+c.ID)
}

// chatgate014Tag is the quiet line on a message addressed to segments: who it is
// for, and for a restricted one how many people it was delivered to. A message
// for everyone has none.
func chatgate014Tag(m Model, msg Message) ui.Node {
	a := msg.Audience
	if a == nil || len(a.Segments) == 0 {
		return nil
	}
	names := chatgate014Join(m, a.Segments)
	text, kind := laneTextf(m, chatgate014ComposerCopy, chatgate014KeyTagTo, map[string]string{"segments": names}), "to"
	if a.Restricted {
		key := chatgate014KeyTagMany
		if a.People == 1 {
			key = chatgate014KeyTagOne
		}
		text, kind = laneTextf(m, chatgate014ComposerCopy, key, map[string]string{"segments": names, "n": strings.TrimSpace(m.n(a.People))}), "only"
	}
	return html.Span(html.Props{Class: "message-audience", Dir: "auto", Data: map[string]string{"gate-audience-tag": kind}, Text: text})
}

// ChatgateComposerStyles draws the row above the composer's tools and the tag on
// a message, both in the quiet type of the composer's own notes.
const ChatgateComposerStyles = `.composer-audience{display:flex;flex-wrap:wrap;align-items:center;gap:4px 8px;margin:0 0 6px;font-size:.8125rem}` +
	`.composer-audience-label{color:var(--muted);font-weight:600}` +
	`.composer-audience-select{min-block-size:32px;max-inline-size:100%;padding-block:2px;padding-inline:8px;font:inherit;color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--line);border-radius:var(--hcm-radius-control,6px)}` +
	`.composer-audience-hint{flex:1 1 100%;margin:0;font-size:.75rem;line-height:1.4;color:var(--muted)}` +
	`.message-audience{font-size:.75rem;color:var(--muted);overflow-wrap:anywhere}`
