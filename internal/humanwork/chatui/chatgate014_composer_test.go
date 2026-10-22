package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATGATE_014_Composer: a writer who may address a segment sees "Visible
// to" above the composer's tools, with Everyone first and then each segment they
// may use; a limit to one group is offered only where the channel allows it, with
// the sentence that says what it does; a channel with nothing to address, and a
// direct message, show nothing. A message addressed to segments carries a quiet
// tag naming them and, when restricted, how many could read it; a message for
// everyone carries none. Both are written in English, German and Arabic.
func TestTodo_CHATGATE_014_Composer(t *testing.T) {
	m := chatux030Model(4)
	room := m.SelectedID
	if page := chatux021Details(t, m, handlers{}); strings.Contains(page, GateAudienceField) {
		t.Fatal("the details panel draws the composer's control")
	}
	if got := chatgate014Composer(m, false); got != nil {
		t.Fatal("a channel with nothing to address offers Visible to")
	}
	m.GateAudience = map[string]GateAudienceOptions{room: {Segments: []GateAudienceChoice{{Name: "Denver office", People: 12}, {Name: "Night shift", People: 1}}}}
	html := renderNode(t, chatgate014Composer(m, false))
	for _, want := range []string{`>Visible to<`, `id="composer-audience"`, `>Everyone in this channel<`, `value="to:Denver office"`, `>For Denver office · everyone can read<`, `value="to:Night shift"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the control lacks %s: %s", want, html)
		}
	}
	if strings.Contains(html, "only:") || strings.Contains(html, "hidden from everyone") {
		t.Errorf("a limit to one group is offered where the channel does not allow it: %s", html)
	}
	if strings.Index(html, `>Everyone in this channel<`) > strings.Index(html, `value="to:Denver office"`) {
		t.Error("Everyone is not the first choice")
	}
	// Where restricted visibility is on, the limit is a second choice per segment.
	m.GateAudience[room] = GateAudienceOptions{Segments: m.GateAudience[room].Segments, Restricted: true}
	html = renderNode(t, chatgate014Composer(m, false))
	for _, want := range []string{`value="only:Denver office"`, `>Only Denver office (12 people)<`, `>Only Night shift (1 person)<`, `A message for one group only is hidden from everyone else in the channel.`} {
		if !strings.Contains(html, want) {
			t.Errorf("the restricted choices lack %s: %s", want, html)
		}
	}
	if !regexp.MustCompile(`<select[^>]*disabled`).MatchString(renderNode(t, chatgate014Composer(m, true))) {
		t.Error("a disabled composer leaves the control usable")
	}
	// A direct message never offers it.
	dm := m
	dm.Conversations = append([]Conversation(nil), m.Conversations...)
	dm.Conversations[0].Kind = DirectMessage
	if got := chatgate014Composer(dm, false); got != nil {
		t.Error("a direct message offers Visible to")
	}
	// The value of the control.
	for value, want := range map[string]string{"": "-|false", "to:Denver office": "Denver office|false", "only:Night shift": "Night shift|true", "to:": "-|false", "junk": "-|false"} {
		segments, restricted := ParseGateAudience(value)
		got := "-"
		if len(segments) == 1 {
			got = segments[0]
		}
		if joined := got + "|" + map[bool]string{true: "true", false: "false"}[restricted]; joined != want {
			t.Errorf("%q reads %s, want %s", value, joined, want)
		}
	}

	// The tag.
	tag := func(a *MessageAudience, locale string) string {
		l := m
		l.Locale = locale
		node := chatgate014Tag(l, Message{ID: "p1", Audience: a})
		if node == nil {
			return ""
		}
		return renderNode(t, node)
	}
	if got := tag(nil, "en-US"); got != "" {
		t.Errorf("a message for everyone carries a tag: %s", got)
	}
	if got := tag(&MessageAudience{Segments: []string{"Denver office"}}, "en-US"); !strings.Contains(got, ">For Denver office<") || !strings.Contains(got, `data-gate-audience-tag="to"`) {
		t.Errorf("addressed tag: %s", got)
	}
	if got := tag(&MessageAudience{Segments: []string{"Denver office"}, Restricted: true, People: 12}, "en-US"); !strings.Contains(got, ">Visible to Denver office (12 people)<") || !strings.Contains(got, `data-gate-audience-tag="only"`) {
		t.Errorf("restricted tag: %s", got)
	}
	if got := tag(&MessageAudience{Segments: []string{"Night shift"}, Restricted: true, People: 1}, "en-US"); !strings.Contains(got, ">Visible to Night shift (1 person)<") {
		t.Errorf("restricted tag for one person: %s", got)
	}
	if got := tag(&MessageAudience{Segments: []string{"Denver office"}, Restricted: true, People: 12}, "de-DE"); !strings.Contains(got, ">Sichtbar für Denver office (12 Personen)<") {
		t.Errorf("German tag: %s", got)
	}
	if got := tag(&MessageAudience{Segments: []string{"Denver office", "Night shift"}, Restricted: true, People: 3}, "ar"); !strings.Contains(got, "مرئية لـ Denver office، Night shift (٣ أشخاص)") {
		t.Errorf("Arabic tag: %s", got)
	}
	// The sentence a send says when it cannot carry the audience, in every language.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		if text := GateAudienceRefusedText(locale); text == "" || strings.Contains(text, "chat.gate014") {
			t.Errorf("%s: the refusal reads %q", locale, text)
		}
		for key := range chatgate014ComposerCopy["en-US"] {
			if text := laneText(Model{Locale: locale}, chatgate014ComposerCopy, key); text == "" || strings.Contains(text, "chat.gate014") {
				t.Errorf("%s: %s reads %q", locale, key, text)
			}
		}
	}
}
