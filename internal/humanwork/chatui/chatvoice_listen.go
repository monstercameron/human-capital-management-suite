package chatui

import (
	"slices"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Listen (CHATVOICE-006). The owner's decision of 2026-10-02 sends the message's
// text to OpenAI text to speech through the model gateway. Listen is an action,
// not content: it lives in the message's action bar as an icon and in its
// "More actions" menu, and nothing is drawn under a message until it is
// playing. Where the server has no speech engine, or the channel never uses an
// outside service, the action is absent rather than disabled.

const chatListenCopyTable = "\nlisten\u0000Listen\u0000Anhören\u0000استماع\u0000" +
	"\nlistenlabel\u0000Listen to this message\u0000Diese Nachricht anhören\u0000الاستماع إلى هذه الرسالة\u0000" +
	"\nlistenagent\u0000Listen to this answer\u0000Diese Antwort anhören\u0000الاستماع إلى هذه الإجابة\u0000" +
	"\nlistenthread\u0000Listen to this thread\u0000Diesen Thread anhören\u0000الاستماع إلى هذا النقاش\u0000" +
	"\nready\u0000Ready. Press Play to listen.\u0000Bereit. Zum Anhören auf Wiedergabe drücken.\u0000جاهز. اضغط تشغيل للاستماع.\u0000" +
	"\nplay\u0000Play\u0000Wiedergabe\u0000تشغيل\u0000" +
	"\nplayer\u0000Reading aloud\u0000Wird vorgelesen\u0000القراءة بصوت عالٍ\u0000" +
	"\nloading\u0000Preparing the audio…\u0000Audio wird vorbereitet…\u0000جارٍ تجهيز الصوت…\u0000" +
	"\nplaying\u0000Reading aloud\u0000Wird vorgelesen\u0000جارٍ القراءة بصوت عالٍ\u0000" +
	"\npause\u0000Pause\u0000Pause\u0000إيقاف مؤقت\u0000" +
	"\nresume\u0000Resume\u0000Fortsetzen\u0000متابعة\u0000" +
	"\nclose\u0000Close\u0000Schließen\u0000إغلاق\u0000" +
	"\nprogress\u0000Reading progress\u0000Lesefortschritt\u0000تقدم القراءة\u0000" +
	"\nspeed\u0000Reading speed\u0000Lesegeschwindigkeit\u0000سرعة القراءة\u0000" +
	"\nbarred\u0000This conversation never uses an outside service, so it cannot be read aloud.\u0000Diese Unterhaltung nutzt nie einen externen Dienst und kann daher nicht vorgelesen werden.\u0000لا تستخدم هذه المحادثة خدمة خارجية أبدًا، لذا لا يمكن قراءتها بصوت عالٍ.\u0000" +
	"\ntoolong\u0000This message is too long to read aloud.\u0000Diese Nachricht ist zu lang zum Vorlesen.\u0000هذه الرسالة طويلة جدًا للقراءة بصوت عالٍ.\u0000" +
	"\nunavailable\u0000Listening is not available right now.\u0000Vorlesen ist derzeit nicht verfügbar.\u0000الاستماع غير متاح حاليًا.\u0000" +
	"\nfailed\u0000Could not read this message aloud. Try again.\u0000Die Nachricht konnte nicht vorgelesen werden. Versuchen Sie es erneut.\u0000تعذرت قراءة هذه الرسالة بصوت عالٍ. حاول مجددًا.\u0000"

// ListenCopy returns one piece of Listen text in the reader's language.
func ListenCopy(locale, key string) string { return integrate1Copy(chatListenCopyTable, locale, key) }

// chatListenAvailable is the whole rule for offering Listen on a message: the
// server composed text to speech and does not bar it in this conversation, the
// message carries text a person reads, and it is not a voice message (which has
// its own player and transcript).
func chatListenAvailable(m Model, msg Message) bool {
	c := m.selected()
	if m.ChatFeatures == nil || !m.ChatFeatures.Listen || msg.ID == "" || c.ID == "" || strings.TrimSpace(msg.Body) == "" {
		return false
	}
	if slices.Contains(strings.Split(m.ChatFeatures.ListenBarred, ","), c.ID) {
		return false
	}
	for _, a := range msg.Attachments {
		if chatvoiceMediaType(a.ContentType) {
			return false
		}
	}
	return true
}

// chatListenAction is the Listen action of one message: an icon in the action
// bar, or a labelled item of the "More actions" menu, or nothing. It carries the
// message's identity; the browser bridge asks the server for the speech when it
// is pressed and shows the player row only then.
func chatListenAction(m Model, msg Message, menu bool) ui.Node {
	if !chatListenAvailable(m, msg) {
		return nil
	}
	label := ListenCopy(m.Locale, "listenlabel")
	if msg.PersonaActor != nil && msg.PersonaActor.valid() {
		label = ListenCopy(m.Locale, "listenagent")
	}
	c := m.selected()
	host := c.HostTenantID
	if host == "" {
		host = m.CurrentTenantID
	}
	class, role, text := "message-action chatlisten-action", "button", html.Span(html.Props{Class: "sr-only", Text: ListenCopy(m.Locale, "listen")})
	if menu {
		class, role = "menu-item chatlisten-action", "menuitem"
		text = html.Span(html.Props{Text: ListenCopy(m.Locale, "listen")})
	}
	data := map[string]string{"chatlisten": "start", "chatlisten-tenant": host, "chatlisten-conversation": c.ID, "chatlisten-post": msg.ID, "chatlisten-locale": m.Locale}
	return html.Button(html.Props{Class: class, Type: "button", Role: role, Title: label, Aria: map[string]string{"label": label}, Data: data}, listenSpeaker(), text)
}

// chatListenThreadAvailable is whether the open thread offers "Listen to this
// thread": the same rule as a message (the server composed text to speech and the
// conversation does not bar outside services), and a thread that is open.
func chatListenThreadAvailable(m Model) bool {
	c := m.selected()
	if m.ChatFeatures == nil || !m.ChatFeatures.Listen || !m.ShowThread || m.ThreadParentID == "" || c.ID == "" {
		return false
	}
	return !slices.Contains(strings.Split(m.ChatFeatures.ListenBarred, ","), c.ID)
}

// chatListenThreadAction is the Listen button of the thread header. It reads the
// parent and its replies in order, each led by its author's name; the browser
// bridge gathers the names the page shows and asks the server for the speech.
func chatListenThreadAction(m Model) ui.Node {
	if !chatListenThreadAvailable(m) {
		return nil
	}
	c := m.selected()
	host := c.HostTenantID
	if host == "" {
		host = m.CurrentTenantID
	}
	label := ListenCopy(m.Locale, "listenthread")
	data := map[string]string{"chatlisten": "start", "chatlisten-tenant": host, "chatlisten-conversation": c.ID, "chatlisten-thread": m.ThreadParentID, "chatlisten-locale": m.Locale}
	return html.Button(html.Props{Class: "icon-button thread-listen chatlisten-action", Type: "button", Title: label, Aria: map[string]string{"label": label}, Data: data}, listenSpeaker())
}

// chatListenThreadHost is where the thread's player row appears: an empty block
// under the thread header that the page owns and the bridge fills while reading.
func chatListenThreadHost(m Model) ui.Node {
	if !chatListenThreadAvailable(m) {
		return nil
	}
	return html.Div(html.Props{Class: "chatlisten-thread-host", Data: map[string]string{"chatlisten-host": "thread"}})
}

func listenSpeaker() ui.Node {
	return html.Tag("svg", html.Props{Class: "chat-icon", Raw: map[string]any{"viewBox": "0 0 24 24", "width": "20", "height": "20", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8", "aria-hidden": "true", "focusable": "false"}}, html.Tag("path", html.Props{Raw: map[string]any{"d": "M4 9v6h4l5 4V5L8 9H4zm12 0a4 4 0 0 1 0 6m2.5-9a8 8 0 0 1 0 12"}}))
}

// ListenPlayerProps names the message the player row belongs to.
type ListenPlayerProps struct{ Locale, PostID string }

// RenderListenPlayer is the compact row shown under one message while it is
// being read: pause, progress, speed and close, and a status line. The browser
// bridge inserts it when Listen is pressed and removes it when reading ends or
// is closed.
func RenderListenPlayer(p ListenPlayerProps) ui.Node {
	dir := "ltr"
	if strings.HasPrefix(p.Locale, "ar") {
		dir = "rtl"
	}
	id := "chatlisten-speed-" + p.PostID
	return html.Div(html.Props{Class: "chatlisten-player", Role: "group", Dir: dir, Aria: map[string]string{"label": ListenCopy(p.Locale, "player")}, Data: map[string]string{"chatlisten-player": p.PostID, "chatlisten-locale": p.Locale}},
		html.Span(html.Props{Class: "chatlisten-status", Role: "status", Aria: map[string]string{"live": "polite"}, Data: map[string]string{"chatlisten-status": ""}}),
		// The sentence being read, when the engine reported timings. It is drawn
		// for the eye only: a screen reader reads the message itself.
		html.Span(html.Props{Class: "chatlisten-sentence", Hidden: true, Dir: "auto", Aria: map[string]string{"hidden": "true"}, Data: map[string]string{"chatlisten-sentence": ""}}),
		html.Span(html.Props{Class: "chatlisten-controls", Hidden: true, Data: map[string]string{"chatlisten-controls": ""}},
			html.Button(html.Props{Type: "button", Class: "chatlisten-button", Text: ListenCopy(p.Locale, "pause"), Data: map[string]string{"chatlisten": "pause"}}),
			html.Tag("progress", html.Props{Class: "chatlisten-progress", Raw: map[string]any{"max": 1, "value": 0}, Aria: map[string]string{"label": ListenCopy(p.Locale, "progress")}, Data: map[string]string{"chatlisten-progress": ""}}),
			html.Label(html.Props{For: id, Class: "sr-only", Text: ListenCopy(p.Locale, "speed")}),
			html.Select(html.Props{ID: id, Class: "chatlisten-speed", Data: map[string]string{"chatlisten-speed": ""}}, html.Option(html.Props{Value: "1", Text: "1×"}), html.Option(html.Props{Value: "1.5", Text: "1.5×"}), html.Option(html.Props{Value: "2", Text: "2×"}))),
		html.Button(html.Props{Type: "button", Class: "chatlisten-button chatlisten-close", Title: ListenCopy(p.Locale, "close"), Aria: map[string]string{"label": ListenCopy(p.Locale, "close")}, Text: "×", Data: map[string]string{"chatlisten": "close"}}))
}

const chatListenStyles = `.chatlisten-player{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-block-start:4px;padding:4px 8px;min-width:0;max-width:100%;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink)}.chatlisten-player button,.chatlisten-player select{min-height:44px;max-width:100%;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink)}.chatlisten-player button:focus-visible,.chatlisten-player select:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}.chatlisten-controls{display:inline-flex;flex:1 1 12em;gap:8px;align-items:center;min-width:0}.chatlisten-progress{flex:1 1 4em;min-width:0}.chatlisten-controls[hidden],.chatlisten-player [hidden]{display:none!important}.chatlisten-sentence{flex:1 1 100%;padding:2px 6px;border-radius:var(--hcm-radius-control);background:color-mix(in srgb,var(--accent) 22%,transparent);color:var(--ink);overflow-wrap:anywhere}.chatlisten-thread-host:empty{display:none}.chatlisten-thread-host{padding-inline:12px}.chatlisten-status{flex:1 1 auto;min-width:0;color:var(--muted);overflow-wrap:anywhere}@media(prefers-reduced-motion:reduce){.chatlisten-player *{animation:none!important;transition:none!important}}`
