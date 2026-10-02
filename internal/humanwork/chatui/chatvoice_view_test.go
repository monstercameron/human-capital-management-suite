package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_CHATVOICE_004_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		p := VoicePlayerProps{Locale: locale, ID: "audio", URL: "blob:fixture", DurationMS: 120000, CanCorrect: true, CanReport: true, Transcript: chat.VoiceTranscript{State: chat.TranscriptReady, Language: "en-US", Segments: []chat.TranscriptSegment{{StartMS: 0, EndMS: 1000, Text: "first sentence", Confidence: 0.4}}}}
		markup, err := ui.RenderToString(RenderVoicePlayer(p))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"<audio", `preload="none"`, "2:00", VoiceCopy(locale, "more"), VoiceCopy(locale, "automatic"), VoiceCopy(locale, "correct"), VoiceCopy(locale, "report"), "chatvoice-low", `data-chatvoice-seek="0"`, `data-chatvoice-end="1000"`, "1.5×", "2×"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("locale=%s missing %q", locale, want)
			}
		}
		if strings.Contains(markup, "autoplay") {
			t.Fatal("autoplay default violated")
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("RTL absent")
		}
	}
}

func TestTodo_CHATVOICE_004(t *testing.T) {
	for _, kind := range []string{"audio/webm", "audio/ogg;codecs=opus", " AUDIO/WEBM "} {
		if !chatvoiceMediaType(kind) {
			t.Fatalf("voice media type refused: %s", kind)
		}
	}
	for _, kind := range []string{"audio/mpeg", "audio/wav", "text/plain"} {
		if chatvoiceMediaType(kind) {
			t.Fatalf("ordinary attachment treated as recorded voice: %s", kind)
		}
	}
	for _, post := range []string{"first", "second"} {
		markup, err := ui.RenderToString(RenderVoicePlayer(VoicePlayerProps{Locale: "en-US", ID: "same-artifact", PostID: post}))
		if err != nil || !strings.Contains(markup, `data-chatvoice-audio="`+post+`:same-artifact"`) {
			t.Fatalf("repeated artifact conflates players: %s,%v", markup, err)
		}
	}
	for _, state := range []chat.TranscriptState{chat.TranscriptPending, chat.TranscriptReady} {
		p := VoicePlayerProps{Locale: "en-US", ID: "voice", Waveform: []float64{0, 0.5, 1}, Transcript: chat.VoiceTranscript{State: state, Correction: "author wording"}}
		markup, err := ui.RenderToString(RenderVoicePlayer(p))
		if err != nil || !strings.Contains(markup, "chatvoice-transcript") || !strings.Contains(markup, `data-chatvoice-waveform=""`) || !strings.Contains(markup, `class="chatvoice-bar-13"`) {
			t.Fatalf("reserved regions/waveform=%s,%v", markup, err)
		}
		if state == chat.TranscriptReady && (!strings.Contains(markup, "Edited by author") || !strings.Contains(markup, "author wording")) {
			t.Fatal("author correction absent")
		}
	}
}
func TestTodo_CHATVOICE_004_Accessibility(t *testing.T) {
	for _, state := range []chat.TranscriptState{chat.TranscriptPending, chat.TranscriptUnavailable, chat.TranscriptFailed} {
		markup, err := ui.RenderToString(RenderVoicePlayer(VoicePlayerProps{Locale: "en-US", ID: "audio", Transcript: chat.VoiceTranscript{State: state}, CanRetry: true}))
		if err != nil {
			t.Fatal(err)
		}
		want := "Transcript unavailable"
		if state == chat.TranscriptPending {
			want = "Transcribing…"
		}
		if state == chat.TranscriptFailed {
			want = VoiceCopy("en-US", "transcriptfailed")
		}
		if !strings.Contains(markup, want) || !strings.Contains(markup, `role="status"`) || !strings.Contains(markup, `aria-label="Listen to voice message"`) {
			t.Fatal(markup)
		}
	}
	for _, want := range []string{"min-height:44px", "focus-visible", "prefers-reduced-motion", "min-width:0", "var(--hcm-"} {
		if !strings.Contains(ChatVoiceStyles, want) {
			t.Fatal("style missing " + want)
		}
	}
	if strings.Contains(ChatVoiceStyles, "#") || strings.Contains(ChatVoiceStyles, "font-family") {
		t.Fatal("branding bypass")
	}
}
func TestTodo_CHATVOICE_004_Security(t *testing.T) {
	markup, err := ui.RenderToString(RenderVoicePlayer(VoicePlayerProps{Locale: "en-US", ID: "audio", URL: "https://outside.invalid/audio", Transcript: chat.VoiceTranscript{State: chat.TranscriptReady, Segments: []chat.TranscriptSegment{{Text: "<script>bad()</script>"}}}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "outside.invalid") || strings.Contains(markup, "<script>") {
		t.Fatal("unsafe audio or transcript escaped boundary")
	}
}
func TestTodo_CHATVOICE_002_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(RenderVoiceComposer(VoiceComposerProps{Locale: locale, ConversationID: "room", Enabled: true}))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"explain", "start", "pause", "stop", "discard", "again", "send", "note", "listen", "level", "remaining"} {
			if !strings.Contains(markup, VoiceCopy(locale, key)) {
				t.Fatalf("%s missing %s", locale, key)
			}
		}
		if regexp.MustCompile(`<input[^>]*\bvalue=`).MatchString(markup) {
			t.Fatal("typed input bound to render value")
		}
	}
	if RenderVoiceComposer(VoiceComposerProps{Enabled: false}) != nil {
		t.Fatal("voice off control rendered")
	}
	m := Model{Locale: "en-US", SelectedID: "room", Conversations: []Conversation{{ID: "room", Kind: DirectMessage}}}
	markup, err := ui.RenderToString(chatvoiceComposer(m, false))
	if err != nil || !strings.Contains(markup, "Record voice message") {
		t.Fatalf("composer hook=%s,%v", markup, err)
	}
	markup, err = ui.RenderToString(chatvoiceAttachment(m, Message{ID: "post"}, Attachment{ID: "audio", ContentType: "audio/webm"}))
	if err != nil || !strings.Contains(markup, "Transcript unavailable") {
		t.Fatalf("attachment hook=%s,%v", markup, err)
	}
}
