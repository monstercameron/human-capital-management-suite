package chatui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type VoicePlayerProps struct {
	Locale, ID, URL                  string
	TenantID, ConversationID, PostID string
	DurationMS                       int64
	Transcript                       chat.VoiceTranscript
	Waveform                         []float64
	CanCorrect, CanReport, CanRetry  bool
}

func voiceButton(locale, key, action string, disabled bool) ui.Node {
	return html.Button(html.Props{Type: "button", Text: VoiceCopy(locale, key), Disabled: disabled, Data: map[string]string{"chatvoice-action": action}})
}
func voiceTime(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%d:%02d", ms/60000, ms/1000%60)
}

func RenderVoicePlayer(p VoicePlayerProps) ui.Node {
	playbackID := p.ID
	if p.PostID != "" {
		playbackID = p.PostID + ":" + p.ID
	}
	dir := "ltr"
	if strings.HasPrefix(p.Locale, "ar") {
		dir = "rtl"
	}
	// Only protected same-origin reads or a grant-backed object URL may be used.
	url := p.URL
	if !chat.VoicePlaybackURL(url) {
		url = ""
	}
	children := []ui.Node{voiceButton(p.Locale, "play", "play-toggle", false), html.Tag("audio", html.Props{Raw: map[string]any{"controls": true, "preload": "none", "src": url}, Aria: map[string]string{"label": VoiceCopy(p.Locale, "player")}, Data: map[string]string{"chatvoice-audio": playbackID}}),
		html.Div(html.Props{Class: "chatvoice-controls"}, html.Label(html.Props{For: "voice-speed-" + playbackID, Text: VoiceCopy(p.Locale, "speed")}), html.Select(html.Props{ID: "voice-speed-" + playbackID, Data: map[string]string{"chatvoice-speed": p.ID}}, html.Option(html.Props{Value: "1", Text: "1×"}), html.Option(html.Props{Value: "1.5", Text: "1.5×"}), html.Option(html.Props{Value: "2", Text: "2×"})), html.Span(html.Props{Dir: "ltr", Data: map[string]string{"chatvoice-time": p.ID}, Text: VoiceClock(p.Locale, 0) + " / " + VoiceClock(p.Locale, p.DurationMS)})),
	}
	bars := []ui.Node{}
	for _, v := range p.Waveform {
		if v >= 0 && v <= 1 {
			bars = append(bars, html.Span(html.Props{Class: "chatvoice-bar-" + strconv.Itoa(2+int(v*22))}))
		}
	}
	children = append(children, html.Div(html.Props{Class: "chatvoice-waveform", Data: map[string]string{"chatvoice-waveform": ""}, Aria: map[string]string{"hidden": "true"}}, bars...))
	transcript := []ui.Node{}
	switch p.Transcript.State {
	case chat.TranscriptReady:
		marker := VoiceCopy(p.Locale, "automatic")
		if p.Transcript.Correction != "" {
			marker = VoiceCopy(p.Locale, "edited")
		}
		if p.Transcript.Language != "" && p.Transcript.Language != p.Locale {
			marker += " · " + p.Transcript.Language
		}
		segments := []ui.Node{}
		if p.Transcript.Correction != "" {
			segments = append(segments, ui.Text(p.Transcript.Correction))
		} else {
			for _, s := range p.Transcript.Segments {
				class := "chatvoice-segment"
				title := ""
				if s.Confidence < 0.7 {
					class += " chatvoice-low"
					title = VoiceCopy(p.Locale, "low")
				}
				segments = append(segments, html.Button(html.Props{Type: "button", Class: class, Title: title, Text: s.Text, Data: map[string]string{"chatvoice-seek": strconv.FormatInt(s.StartMS, 10), "chatvoice-end": strconv.FormatInt(s.EndMS, 10)}, Aria: map[string]string{"label": VoiceCopy(p.Locale, "seek") + ": " + s.Text, "description": title}}))
			}
		}
		transcript = append(transcript, html.P(html.Props{Text: marker}), html.Div(html.Props{Class: "chatvoice-preview-text chatvoice-transcript-text", Lang: p.Transcript.Language}, ui.Text(p.Transcript.Text())), chatPolishDisclosure(html.Props{Data: map[string]string{"chatvoice-expand": ""}}, chatPolishDisclosureLabel(html.Props{Text: VoiceCopy(p.Locale, "more")}), html.Div(html.Props{Class: "chatvoice-transcript-text", Lang: p.Transcript.Language}, segments...)))
		if p.CanCorrect {
			transcript = append(transcript, html.Label(html.Props{For: "voice-correction-" + playbackID, Text: VoiceCopy(p.Locale, "correct")}), html.Input(html.Props{ID: "voice-correction-" + playbackID, Type: "text", Data: map[string]string{"chatvoice-correction": ""}}), voiceButton(p.Locale, "correct", "correct", false))
		}
		if p.CanReport {
			transcript = append(transcript, voiceButton(p.Locale, "report", "report", false))
		}
	case chat.TranscriptPending:
		transcript = append(transcript, html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Text: VoiceCopy(p.Locale, "pending")}))
	default:
		key := "none"
		if p.Transcript.State == chat.TranscriptFailed {
			key = "transcriptfailed"
		}
		transcript = append(transcript, html.P(html.Props{Role: "status", Text: VoiceCopy(p.Locale, key)}))
		if p.CanRetry {
			transcript = append(transcript, voiceButton(p.Locale, "retry", "retry", false))
		}
	}
	children = append(children, html.Div(html.Props{Class: "chatvoice-transcript", Data: map[string]string{"chatvoice-transcript": p.ID}}, transcript...))
	children = append(children, html.Label(html.Props{}, html.Input(html.Props{Type: "checkbox", Checked: true, Data: map[string]string{"chatvoice-collapsed": ""}}), ui.Text(VoiceCopy(p.Locale, "collapsed"))), html.P(html.Props{Text: VoiceCopy(p.Locale, "never")}), html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Data: map[string]string{"chatvoice-feedback": ""}}))
	return html.Section(html.Props{Class: "chatvoice", Dir: dir, Data: map[string]string{"chatvoice-player": p.ID, "chatvoice-playback": playbackID, "chatvoice-locale": p.Locale, "chatvoice-tenant": p.TenantID, "chatvoice-conversation": p.ConversationID, "chatvoice-post": p.PostID}, Aria: map[string]string{"label": VoiceCopy(p.Locale, "player")}}, children...)
}

type VoiceComposerProps struct {
	Locale, ConversationID, TenantID string
	Enabled, Disabled                bool
}

func RenderVoiceComposer(p VoiceComposerProps) ui.Node {
	if !p.Enabled {
		return nil
	}
	dir := "ltr"
	if strings.HasPrefix(p.Locale, "ar") {
		dir = "rtl"
	}
	return html.Div(html.Props{Class: "chatvoice-tool", Dir: dir, Data: map[string]string{"chatvoice-recorder": "", "chatvoice-locale": p.Locale, "chatvoice-conversation": p.ConversationID, "chatvoice-tenant": p.TenantID}},
		html.Button(html.Props{Class: "tool-button format-button", Type: "button", TabIndex: -1, Title: VoiceCopy(p.Locale, "record"), Disabled: p.Disabled, Aria: map[string]string{"label": VoiceCopy(p.Locale, "record"), "haspopup": "dialog", "expanded": "false", "controls": "chatvoice-panel-" + p.ConversationID}, Data: map[string]string{"chatvoice-action": "toggle"}}, voiceMicrophone()),
		anchoredChatLayer(html.Props{ID: "chatvoice-panel-" + p.ConversationID, Class: "chatvoice chatvoice-panel", Hidden: true, Role: "dialog", Aria: map[string]string{"label": VoiceCopy(p.Locale, "record")}}, "voice",
			html.P(html.Props{Text: VoiceCopy(p.Locale, "explain")}), voiceButton(p.Locale, "start", "start", p.Disabled),
			html.Div(html.Props{Class: "chatvoice-status", Role: "status", Aria: map[string]string{"live": "polite"}, Data: map[string]string{"chatvoice-status": ""}}),
			html.Tag("meter", html.Props{Raw: map[string]any{"min": 0, "max": 1, "value": 0}, Aria: map[string]string{"label": VoiceCopy(p.Locale, "level")}, Data: map[string]string{"chatvoice-meter": ""}}),
			html.Span(html.Props{Dir: "ltr", Text: VoiceClock(p.Locale, 0) + " / " + VoiceClock(p.Locale, voiceMaxClockMS), Data: map[string]string{"chatvoice-clock": ""}, Aria: map[string]string{"label": VoiceCopy(p.Locale, "remaining")}}),
			html.Div(html.Props{Class: "chatvoice-controls"}, voiceButton(p.Locale, "pause", "pause", true), voiceButton(p.Locale, "stop", "stop", true), voiceButton(p.Locale, "discard", "discard", true), voiceButton(p.Locale, "again", "again", true)),
			html.Tag("audio", html.Props{Raw: map[string]any{"controls": true, "preload": "none"}, Aria: map[string]string{"label": VoiceCopy(p.Locale, "listen")}, Data: map[string]string{"chatvoice-preview": ""}}),
			html.Label(html.Props{For: "chatvoice-note-" + p.ConversationID, Text: VoiceCopy(p.Locale, "note")}), html.Input(html.Props{ID: "chatvoice-note-" + p.ConversationID, Type: "text", Class: "chatvoice-note", Data: map[string]string{"chatvoice-note": ""}}), voiceButton(p.Locale, "send", "send", true)))
}

func chatvoiceComposer(m Model, disabled bool) ui.Node {
	c := m.selected()
	// The recorder is in the page where voice is on: a direct or group conversation
	// until the server says otherwise, a channel once it says voice is enabled
	// there. Where voice is off the Add menu says so and nothing is mounted.
	// The server policy is still mandatory at Send.
	return RenderVoiceComposer(VoiceComposerProps{Locale: m.Locale, ConversationID: c.ID, TenantID: c.HostTenantID, Enabled: composerVoiceOffered(m) && composerVoiceOffText(m) == "", Disabled: disabled})
}

func chatvoiceMediaType(contentType string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return base == "audio/webm" || base == "audio/ogg"
}

func chatvoiceAttachment(m Model, msg Message, a Attachment) ui.Node {
	node := RenderVoicePlayer(VoicePlayerProps{Locale: m.Locale, ID: a.ID, URL: a.URL, TenantID: m.selected().HostTenantID, ConversationID: m.SelectedID, PostID: msg.ID, Transcript: chat.VoiceTranscript{State: chat.TranscriptNone}, CanRetry: true})
	return html.Div(html.Props{Data: map[string]string{"chatvoice-author": msg.AuthorID}}, node)
}

func voiceMicrophone() ui.Node {
	return html.Tag("svg", html.Props{Class: "chat-icon", Raw: map[string]any{"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8", "aria-hidden": "true", "focusable": "false"}}, html.Tag("path", html.Props{Raw: map[string]any{"d": "M9 5a3 3 0 0 1 6 0v7a3 3 0 0 1-6 0V5zm-3 6v1a6 6 0 0 0 12 0v-1M12 18v4m-4 0h8"}}))
}
