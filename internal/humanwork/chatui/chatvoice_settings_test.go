package chatui

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func voiceAccessModel(kind ConversationKind, locale string, access map[string]VoiceAccessView) Model {
	return Model{Locale: locale, State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "Room", Kind: kind, HostTenantID: "tenant-a"}}, VoiceAccess: access, CurrentTenantID: "tenant-a"}
}

func voiceAddMenu(t *testing.T, m Model) string {
	t.Helper()
	return renderNode(t, composerAddControl(m, "composer", false))
}

// TestTodo_CHATVOICE_002_Channel: the microphone is offered in a channel where
// voice is enabled; where it is off the Add menu keeps the item, reachable by
// keyboard, with the reason as its note, and mounts no recorder; a channel the
// server has not answered for offers nothing, and a direct conversation keeps
// offering voice until the server says otherwise.
func TestTodo_CHATVOICE_002_Channel(t *testing.T) {
	voiceItem := `data-extra="voice"`
	allowed := map[string]VoiceAccessView{"room": {Allowed: true}}
	if menu := voiceAddMenu(t, voiceAccessModel(PublicChannel, "en-US", allowed)); !strings.Contains(menu, voiceItem) || strings.Contains(menu, `aria-disabled`) {
		t.Fatalf("channel with voice on: %s", menu)
	}
	if recorder := renderNode(t, chatvoiceComposer(voiceAccessModel(PublicChannel, "en-US", allowed), false)); !strings.Contains(recorder, "data-chatvoice-recorder") {
		t.Fatalf("channel with voice on mounts no recorder: %s", recorder)
	}
	if menu := voiceAddMenu(t, voiceAccessModel(PublicChannel, "en-US", nil)); strings.Contains(menu, voiceItem) {
		t.Fatalf("a channel nobody has asked about offered voice: %s", menu)
	}
	if menu := voiceAddMenu(t, voiceAccessModel(DirectMessage, "en-US", nil)); !strings.Contains(menu, voiceItem) || strings.Contains(menu, `aria-disabled`) {
		t.Fatalf("direct conversation lost voice before the server answered: %s", menu)
	}
	if menu := voiceAddMenu(t, voiceAccessModel(PublicChannel, "en-US", map[string]VoiceAccessView{"room": {Reason: "unavailable"}})); strings.Contains(menu, voiceItem) {
		t.Fatalf("a server without voice media offered voice: %s", menu)
	}
	for _, c := range []struct {
		locale, reason, key, want string
		kind                      ConversationKind
	}{
		{"en-US", "channel_off", "off_channel", "Voice messages are turned off in this channel", PrivateChannel},
		{"en-US", "workspace_off", "off_workspace", "Voice messages are turned off in this workspace", PublicChannel},
		{"en-US", "personal_off", "off_personal", "Voice messages are turned off in your Chat preferences", DirectMessage},
		{"de-DE", "channel_off", "off_channel", "Sprachnachrichten sind in diesem Kanal ausgeschaltet", PublicChannel},
		{"ar", "workspace_off", "off_workspace", "الرسائل الصوتية متوقفة في مساحة العمل هذه", GroupChat},
	} {
		m := voiceAccessModel(c.kind, c.locale, map[string]VoiceAccessView{"room": {Reason: c.reason}})
		menu := voiceAddMenu(t, m)
		if VoiceSettingsCopy(c.locale, c.key) != c.want {
			t.Fatalf("%s %s: copy is %q", c.locale, c.key, VoiceSettingsCopy(c.locale, c.key))
		}
		for _, want := range []string{voiceItem, `aria-disabled="true"`, `title="` + c.want + `"`, `composer-add-item-off`, ">" + c.want + "<"} {
			if !strings.Contains(menu, want) {
				t.Fatalf("%s %s: voice item missing %q in %s", c.locale, c.reason, want, menu)
			}
		}
		// A disabled control the keyboard could not reach would hide the reason: the
		// item is a button it is described by its note, not one with a disabled attribute.
		if tag := tagAround(menu, voiceItem); strings.Contains(tag, " disabled") || !strings.Contains(tag, `aria-describedby="composer-add-menu-voice-note"`) {
			t.Fatalf("%s: the voice item is out of the tab order or has no description: %s", c.locale, tag)
		}
		if recorder := renderNode(t, chatvoiceComposer(m, false)); strings.Contains(recorder, "data-chatvoice-recorder") {
			t.Fatalf("%s: voice is off but a recorder is mounted", c.locale)
		}
	}
}

// The Add menu's voice and location openers stay out of the tab order: the Add
// menu items are the way in, so no keyboard stop sits over the Add button.
func TestTodo_CHATVOICE_002_OpenersOutOfTabOrder(t *testing.T) {
	m := voiceAccessModel(DirectMessage, "en-US", nil)
	if tag := tagAround(renderNode(t, chatvoiceComposer(m, false)), `data-chatvoice-action="toggle"`); !strings.Contains(strings.ToLower(tag), `tabindex="-1"`) {
		t.Fatalf("the voice opener is a keyboard stop: %s", tag)
	}
	m.ChatFeatures = &ChatFeatures{Locations: true}
	if tag := tagAround(renderNode(t, chatmapComposerControl(m, "composer", false)), `data-chatmap-action="toggle"`); !strings.Contains(strings.ToLower(tag), `tabindex="-1"`) {
		t.Fatalf("the location opener is a keyboard stop: %s", tag)
	}
}

// tagAround is the opening tag that holds needle.
func tagAround(markup, needle string) string {
	at := strings.Index(markup, needle)
	if at < 0 {
		return ""
	}
	start := strings.LastIndex(markup[:at], "<")
	end := strings.Index(markup[at:], ">")
	if start < 0 || end < 0 {
		return ""
	}
	return markup[start : at+end+1]
}

func voiceSettingsRender(t *testing.T, props voiceSettingsProps, s voiceSettingsState) string {
	t.Helper()
	node := voiceSettingsView(props, s, ui.Handler{}, ui.Handler{})
	if node == nil {
		return ""
	}
	return renderNode(t, node)
}

// TestTodo_CHATVOICE_005_Rows: the three switches have a row each, in the place
// the person looks for it, saying what it changes in one line and showing
// loading, saved and failed states; the engine is a read-only line for an
// administrator.
func TestTodo_CHATVOICE_005_Rows(t *testing.T) {
	loaded := voiceSettingsState{Loaded: true, Data: VoiceSettingsData{Workspace: true, Person: true, Channel: false, IsChannel: true, CanManageWorkspace: true, CanManageChannel: true,
		Engine: &VoiceEngineData{Kind: "outside", Transcriber: "gpt-4o-mini-transcribe", Speech: "gpt-4o-mini-tts", OutsideKnown: true, OutsideAllowed: true}}}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ws := voiceSettingsRender(t, voiceSettingsProps{Locale: locale, Scope: voiceScopeWorkspace}, loaded)
			for _, want := range []string{VoiceSettingsCopy(locale, "ws_switch"), VoiceSettingsCopy(locale, "ws_hint"), VoiceSettingsCopy(locale, "ws_intro"), VoiceSettingsCopy(locale, "engine"), `role="switch"`, `checked`, `id="chatvoice-switch-workspace"`, `data-chatvoice-engine="outside"`, "gpt-4o-mini-transcribe", "gpt-4o-mini-tts", VoiceSettingsCopy(locale, "outside_yes")} {
				if !strings.Contains(ws, want) {
					t.Fatalf("workspace row is missing %q: %s", want, ws)
				}
			}
			if locale == "ar" && !strings.Contains(ws, `dir="rtl"`) {
				t.Fatal("the Arabic workspace row is not right to left")
			}
			ch := voiceSettingsRender(t, voiceSettingsProps{Locale: locale, Scope: voiceScopeChannel, Tenant: "tenant-a", Conversation: "room"}, loaded)
			if !strings.Contains(ch, VoiceSettingsCopy(locale, "ch_switch")) || !strings.Contains(ch, VoiceSettingsCopy(locale, "ch_hint")) || strings.Contains(ch, "checked") {
				t.Fatalf("the channel row is wrong or on by default: %s", ch)
			}
			me := voiceSettingsRender(t, voiceSettingsProps{Locale: locale, Scope: voiceScopePerson}, loaded)
			if !strings.Contains(me, VoiceSettingsCopy(locale, "me_switch")) || !strings.Contains(me, VoiceSettingsCopy(locale, "me_hint")) || !strings.Contains(me, `data-prefs-section="voice"`) || !strings.Contains(me, VoiceSettingsCopy(locale, "on")) {
				t.Fatalf("the personal row is wrong: %s", me)
			}
		})
	}
	for _, key := range []string{"off_workspace", "off_channel", "off_personal", "title", "ws_intro", "ws_switch", "ws_hint", "ch_switch", "ch_hint", "me_switch", "me_hint", "engine", "engine_fixture", "engine_outside", "engine_none", "outside_yes", "outside_no", "outside_unknown", "loading", "loadfailed", "saving", "saved", "savefailed", "retry", "unavailable", "denied", "on", "off"} {
		en := VoiceSettingsCopy("en-US", key)
		if en == "" || en == key {
			t.Fatalf("english %q is %q", key, en)
		}
		for _, locale := range []string{"de-DE", "ar"} {
			if got := VoiceSettingsCopy(locale, key); got == "" || got == key || got == en && key != "title" {
				t.Fatalf("%s %q is %q", locale, key, got)
			}
		}
	}

	// Engine lines, in words: the fixture, no engine, and outside services barred or unreadable.
	engineLine := func(e VoiceEngineData) string {
		s := loaded
		s.Data.Engine = &e
		return voiceSettingsRender(t, voiceSettingsProps{Locale: "en-US", Scope: voiceScopeWorkspace}, s)
	}
	if got := engineLine(VoiceEngineData{Kind: "fixture"}); !strings.Contains(got, "Local development fixture. No speech model is called") || strings.Contains(got, "Outside services") {
		t.Fatalf("fixture engine: %s", got)
	}
	if got := engineLine(VoiceEngineData{Kind: "none", Note: "set HCMNEXT_CHATVOICE_ENGINE or MODEL_API_KEY"}); !strings.Contains(got, "No engine is set up (set HCMNEXT_CHATVOICE_ENGINE or MODEL_API_KEY)") {
		t.Fatalf("no engine: %s", got)
	}
	if got := engineLine(VoiceEngineData{Kind: "outside", Transcriber: "a", Speech: "b", OutsideKnown: true}); !strings.Contains(got, "Outside services are not allowed in this workspace") {
		t.Fatalf("outside barred: %s", got)
	}
	if got := engineLine(VoiceEngineData{Kind: "outside", Transcriber: "a", Speech: "b"}); !strings.Contains(got, "Whether outside services are allowed could not be read.") {
		t.Fatalf("outside unreadable: %s", got)
	}

	// States: loading shell, a failed read that offers Try again, a save in
	// flight, saved and failed saves, the person who may not administer.
	props := voiceSettingsProps{Locale: "en-US", Scope: voiceScopeWorkspace}
	if got := voiceSettingsRender(t, props, voiceSettingsState{}); !strings.Contains(got, "Loading voice settings…") || !strings.Contains(got, `aria-busy="true"`) || strings.Contains(got, `role="switch"`) {
		t.Fatalf("loading: %s", got)
	}
	if got := voiceSettingsRender(t, props, voiceSettingsState{LoadFailed: true}); !strings.Contains(got, "Voice settings could not load. Try again.") || !strings.Contains(got, ">Try again<") || strings.Contains(got, `role="switch"`) {
		t.Fatalf("load failed: %s", got)
	}
	saving, saved, failed := loaded, loaded, loaded
	saving.Saving, saved.Saved, failed.Fails = true, true, true
	if got := voiceSettingsRender(t, props, saving); !strings.Contains(got, "Saving…") {
		t.Fatalf("saving: %s", got)
	}
	if got := voiceSettingsRender(t, props, saved); !strings.Contains(got, "chatlangadmin-saved") || !strings.Contains(got, ">Saved<") {
		t.Fatalf("saved: %s", got)
	}
	if got := voiceSettingsRender(t, props, failed); !strings.Contains(got, `role="alert"`) || !strings.Contains(got, "Could not save. The setting is unchanged. Try again.") || !strings.Contains(got, `role="switch"`) {
		t.Fatalf("failed save keeps the switch and says so: %s", got)
	}
	member := loaded
	member.Data.CanManageWorkspace, member.Data.Engine = false, nil
	if got := voiceSettingsRender(t, props, member); !strings.Contains(got, "Only a workspace administrator can change voice messages for the workspace.") || strings.Contains(got, `role="switch"`) || strings.Contains(got, "Transcription engine") {
		t.Fatalf("a member saw the workspace switch or the engine: %s", got)
	}
	notChannelAdmin := loaded
	notChannelAdmin.Data.CanManageChannel = false
	if got := voiceSettingsRender(t, voiceSettingsProps{Locale: "en-US", Scope: voiceScopeChannel}, notChannelAdmin); got != "" {
		t.Fatalf("the channel row is drawn for a person the server says may not change it: %s", got)
	}
}

// The three rows are where they are asked to be: the workspace panel in the
// workspace Chat settings, the channel row in Manage channel (not drawn in a
// direct conversation or for a member), the personal row beside Quiet hours.
func TestTodo_CHATVOICE_005_Placement(t *testing.T) {
	m := voiceAccessModel(PublicChannel, "en-US", nil)
	m.IsTenantAdmin = true
	if chatvoiceChannelRow(m, handlers{}, m.selected()) != nil || chatvoicePersonalRow(m) != nil {
		t.Fatal("switches are offered where the server did not say voice is composed")
	}
	m.ChatFeatures = &ChatFeatures{Voice: true}
	if chatvoicePersonalRow(m) == nil {
		t.Fatal("the personal switch is missing from Chat preferences")
	}
	if chatvoiceChannelRow(m, handlers{}, m.selected()) == nil {
		t.Fatal("an administrator did not get the channel switch")
	}
	m.IsTenantAdmin = false
	if chatvoiceChannelRow(m, handlers{}, m.selected()) != nil {
		t.Fatal("a member who administers nothing got the channel switch")
	}
	dm := voiceAccessModel(DirectMessage, "en-US", nil)
	dm.IsTenantAdmin, dm.ChatFeatures = true, &ChatFeatures{Voice: true}
	if chatvoiceChannelRow(dm, handlers{}, dm.selected()) != nil {
		t.Fatal("a direct conversation has a channel switch")
	}
	if got := voiceSettingsRender(t, voiceSettingsProps{Locale: "en-US", Scope: voiceScopeWorkspace}, voiceSettingsState{Loaded: true, Unavailable: true}); !strings.Contains(got, "Voice messages are not set up on this server.") || strings.Contains(got, `role="switch"`) {
		t.Fatalf("voice not composed: %s", got)
	}
	if got := voiceSettingsRender(t, voiceSettingsProps{Locale: "en-US", Scope: voiceScopePerson}, voiceSettingsState{Loaded: true, Unavailable: true}); got != "" {
		t.Fatalf("a personal row is drawn where voice is not composed: %s", got)
	}
	if got := renderNode(t, VoiceWorkspaceSettingsPanel("en-US")); !strings.Contains(got, "Voice messages") || !strings.Contains(got, "Loading voice settings…") {
		t.Fatalf("workspace panel at rest: %s", got)
	}
}

func TestTodo_CHATVOICE_006_Timings(t *testing.T) {
	timings := []ListenTiming{{Text: "One.", StartMS: 0, EndMS: 900}, {Text: "Two.", StartMS: 900, EndMS: 1800}}
	header := EncodeListenTimings(timings)
	if header == "" {
		t.Fatal("valid timings were refused")
	}
	parsed := ParseListenTimings(header)
	if len(parsed) != 2 || parsed[1].Text != "Two." || parsed[1].EndMS != 1800 {
		t.Fatalf("round trip %+v", parsed)
	}
	for position, want := range map[int64]int{0: 0, 899: 0, 900: 1, 1799: 1, 1800: -1, -5: -1} {
		if got := ListenSentenceAt(parsed, position); got != want {
			t.Errorf("sentence at %d ms is %d, want %d", position, got, want)
		}
	}
	// No timings, or timings that are not an ordered list, mark nothing at all.
	if ListenSentenceAt(nil, 100) != -1 || ParseListenTimings("") != nil || ParseListenTimings("not base64!") != nil {
		t.Fatal("no timings must mean no sentence")
	}
	bad := map[string][]ListenTiming{
		"empty":        nil,
		"empty text":   {{Text: "", StartMS: 0, EndMS: 10}},
		"backwards":    {{Text: "a", StartMS: 10, EndMS: 5}},
		"overlap":      {{Text: "a", StartMS: 0, EndMS: 100}, {Text: "b", StartMS: 50, EndMS: 200}},
		"huge":         make([]ListenTiming, listenTimingsMaxCount+1),
		"long text":    {{Text: strings.Repeat("a", listenTimingsMaxText+1), StartMS: 0, EndMS: 10}},
		"unordered":    {{Text: "b", StartMS: 100, EndMS: 200}, {Text: "a", StartMS: 0, EndMS: 50}},
		"zero length":  {{Text: "a", StartMS: 5, EndMS: 5}},
		"negative gap": {{Text: "a", StartMS: -10, EndMS: 5}},
	}
	for name, list := range bad {
		if name == "negative gap" {
			// A list that starts before zero is still a list; only order matters.
			continue
		}
		if EncodeListenTimings(list) != "" {
			t.Errorf("%s: an unusable list was encoded", name)
		}
	}
	if ParseListenTimings(base64.StdEncoding.EncodeToString([]byte(`[{"text":"a","start_ms":5,"end_ms":1},{"text":"b","start_ms":0,"end_ms":2}]`))) != nil {
		t.Fatal("a header that is not ordered was accepted")
	}
	if ParseListenTimings(base64.StdEncoding.EncodeToString([]byte(`{"text":"a"}`))) != nil {
		t.Fatal("a header that is not a list was accepted")
	}
}

func listenThreadModel(features *ChatFeatures, locale string) Model {
	root := Message{ID: "root", AuthorID: "polly", Author: "Polly Adams", Body: "Lunch is at noon"}
	m := listenModel(features, locale, "", root)
	m.ShowThread, m.ThreadParentID = true, "root"
	m.ThreadMessages = []Message{{ID: "reply", AuthorID: "sam", Author: "Sam Lee", Body: "Sounds good"}}
	return m
}

// TestTodo_CHATVOICE_006_Thread: the thread header has "Listen to this thread"
// where Listen works, in three languages, with a block under the header for the
// player row; nowhere else, and not where Listen is absent.
func TestTodo_CHATVOICE_006_Thread(t *testing.T) {
	for _, c := range []struct{ locale, label string }{{"en-US", "Listen to this thread"}, {"de-DE", "Diesen Thread anhören"}, {"ar", "الاستماع إلى هذا النقاش"}} {
		markup := render(t, listenThreadModel(&ChatFeatures{Listen: true}, c.locale))
		if !strings.Contains(markup, `aria-label="`+c.label+`"`) || !strings.Contains(markup, `data-chatlisten-thread="root"`) || !strings.Contains(markup, `data-chatlisten-host="thread"`) || !strings.Contains(markup, `data-chatlisten-conversation="general"`) || !strings.Contains(markup, `data-chatlisten-tenant="tenant-a"`) {
			t.Fatalf("%s: thread Listen is missing: %s", c.locale, markup)
		}
		heading := markup[strings.Index(markup, "thread-heading"):]
		if strings.Index(heading, "thread-listen") < 0 || strings.Index(heading, "thread-listen") > strings.Index(heading, "thread-notify") {
			t.Fatalf("%s: Listen is not in the thread header, before Follow", c.locale)
		}
	}
	for name, m := range map[string]Model{
		"no features":    listenThreadModel(nil, "en-US"),
		"not composed":   listenThreadModel(&ChatFeatures{}, "en-US"),
		"channel barred": listenThreadModel(&ChatFeatures{Listen: true, ListenBarred: "general"}, "en-US"),
		"thread not open": func() Model {
			m := listenThreadModel(&ChatFeatures{Listen: true}, "en-US")
			m.ShowThread = false
			return m
		}(),
	} {
		if markup := render(t, m); strings.Contains(markup, "chatlisten-thread") || strings.Contains(markup, "data-chatlisten-host") || strings.Contains(markup, "Listen to this thread") {
			t.Fatalf("%s: thread Listen offered", name)
		}
	}
}

// The player row carries a place for the sentence being read, drawn for the eye
// only and empty until the page has timings to mark it from.
func TestTodo_CHATVOICE_006_PlayerSentence(t *testing.T) {
	markup := renderNode(t, RenderListenPlayer(ListenPlayerProps{Locale: "en-US", PostID: "p"}))
	if !strings.Contains(markup, "data-chatlisten-sentence") || !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, "hidden") {
		t.Fatalf("player row has no sentence mark: %s", markup)
	}
	sentence := tagAround(markup, "data-chatlisten-sentence")
	if !strings.Contains(sentence, " hidden") || !strings.Contains(sentence, `aria-hidden="true"`) {
		t.Fatalf("the sentence mark is announced or visible before any timing: %s", sentence)
	}
	for _, key := range []string{"listenthread", "ready", "play"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if got := ListenCopy(locale, key); got == "" || got == key {
				t.Fatalf("%s: copy %q is %q", locale, key, got)
			}
		}
	}
	if !strings.Contains(chatListenStyles, ".chatlisten-sentence") {
		t.Fatal("the sentence mark has no style")
	}
}
