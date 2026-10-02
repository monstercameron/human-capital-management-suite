package chatui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func TestTodo_CHATBUG_039(t *testing.T) {
	for _, tc := range []struct{ localized, english, want string }{
		{"Deutsch", "English", "Deutsch"}, {"العربية", "English", "العربية"},
		{"", "English", "English"}, {"  ", "English", "English"},
		{"chat.missing", "English", "English"}, {"⟦chat.missing⟧", "English", "English"},
		{"prefix ⟦chat.missing⟧", "English", "English"}, {"broken⟧", "English", "English"},
		{"⟦chat.missing⟧", "chat.missing", ""}, {"", "⟦chat.missing⟧", ""},
	} {
		if got := chatbug039Text("chat.missing", tc.localized, tc.english); got != tc.want {
			t.Errorf("resolve(%q, %q) = %q, want %q", tc.localized, tc.english, got, tc.want)
		}
	}
	table := "\nhello\x00Hello\x00Hallo\x00مرحباً\x00\nfallback\x00Fallback\x00\x00⟦missing⟧\x00"
	for locale, want := range map[string]string{"en-US": "Hello", "de-DE": "Hallo", "ar": "مرحباً", "fr": "Hello"} {
		if got := chatbug039CompactText(table, locale, "hello"); got != want {
			t.Errorf("%s compact copy = %q, want %q", locale, got, want)
		}
	}
	for _, key := range []string{"", "missing", "bad\nkey", "bad\x00key"} {
		if got := chatbug039CompactText(table, "ar", key); got != "" {
			t.Errorf("unknown compact key %q = %q", key, got)
		}
	}
	if got := chatbug039CompactText(table, "ar", "fallback"); got != "Fallback" {
		t.Errorf("compact English fallback = %q", got)
	}
	m := Model{Text: func(key string) string { return "⟦" + key + "⟧" }}
	if got := m.t(KeySend); got != englishCopy[KeySend] {
		t.Errorf("core catalog fallback = %q", got)
	}
	if got := m.t("chat.missing"); got != "" {
		t.Errorf("unknown core key = %q", got)
	}
	if got := (Model{}).t(KeySend); got != englishCopy[KeySend] {
		t.Errorf("nil catalog fallback = %q", got)
	}
	if got := (Model{Text: func(string) string { return "Translated" }}).t(KeySend); got != "Translated" {
		t.Errorf("core catalog translation = %q", got)
	}
	english := SavedMessagesCopy("en-US")
	resolved := reflect.ValueOf(chatbug039SavedCopy(SavedCopy{}, english))
	original := reflect.ValueOf(english)
	for i := 0; i < resolved.NumField(); i++ {
		if resolved.Field(i).String() != original.Field(i).String() {
			t.Errorf("Saved field %s lost its English fallback", resolved.Type().Field(i).Name)
		}
	}
	if got := chatbug039SavedCopy(SavedCopy{Save: "Speichern"}, english).Save; got != "Speichern" {
		t.Errorf("Saved local copy = %q", got)
	}
	featureModel := Model{Locale: "de-DE", Text: func(string) string { return "Catalog override" }}
	for name, pair := range map[string][2]string{
		"emoji":    {chatEmojiText(featureModel, emojiKeyTitle), chatEmojiCopy["de-DE"][emojiKeyTitle]},
		"composer": {composerText(featureModel, keyComposerAddPoll), composerToolsCopy["de-DE"][keyComposerAddPoll]},
		"search":   {chatux001Text(featureModel, "chat.ux001.search"), chatux001De["chat.ux001.search"]},
		"filters":  {chatfilterText(featureModel, "manage"), "Filter verwalten"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s: feature table lost priority: got %q, want %q", name, pair[0], pair[1])
		}
	}
	t.Run("copy helpers cannot bypass shared resolution", chatbug039Conformance)
}

func chatbug039Conformance(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	calls := map[string][]string{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") || strings.HasPrefix(path, ".") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			functions[fn.Name.Name] = fn
			models := map[string]bool{}
			for _, fields := range []*ast.FieldList{fn.Type.Params, fn.Recv} {
				if fields == nil {
					continue
				}
				for _, field := range fields.List {
					kind := field.Type
					if pointer, ok := kind.(*ast.StarExpr); ok {
						kind = pointer.X
					}
					if named, ok := kind.(*ast.Ident); ok && named.Name == "Model" {
						for _, name := range field.Names {
							models[name.Name] = true
						}
					}
				}
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch f := call.Fun.(type) {
				case *ast.Ident:
					calls[fn.Name.Name] = append(calls[fn.Name.Name], f.Name)
				case *ast.SelectorExpr:
					calls[fn.Name.Name] = append(calls[fn.Name.Name], f.Sel.Name)
					receiver, isIdentifier := f.X.(*ast.Ident)
					if f.Sel.Name == "Text" && isIdentifier && models[receiver.Name] && fn.Name.Name != "chatbug039CatalogText" {
						t.Errorf("%s: direct catalog lookup bypasses feature tables", fn.Name.Name)
					}
				}
				return true
			})
		}
	}
	var reachesShared func(string, map[string]bool) bool
	reachesShared = func(name string, seen map[string]bool) bool {
		if name == "chatbug039Text" {
			return true
		}
		if seen[name] {
			return false
		}
		seen[name] = true
		for _, next := range calls[name] {
			if reachesShared(next, seen) {
				return true
			}
		}
		return false
	}
	checked := 0
	for name, fn := range functions {
		if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
			continue
		}
		result, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
		if !ok || result.Name != "string" {
			continue
		}
		keyed := false
		for _, field := range fn.Type.Params.List {
			for _, param := range field.Names {
				keyed = keyed || param.Name == "key"
			}
		}
		// This reads a raw JavaScript object field; it is not a copy lookup.
		if !keyed || name == "giphyJSString" {
			continue
		}
		checked++
		if !reachesShared(name, map[string]bool{}) {
			t.Errorf("%s: keyed text helper must resolve through chatbug039Text", name)
		}
	}
	if checked < 30 {
		t.Fatalf("only %d copy helpers inspected", checked)
	}
}

// Chatbug039SurfacesForTest exposes native component trees to the external
// real-product-catalog test without introducing a productui import cycle.
func Chatbug039SurfacesForTest(t *testing.T, catalog Model) map[string]string {
	t.Helper()
	m := chat4Fixture(catalog.Locale, "answered", false)
	m.Text, m.Direction = catalog.Text, catalog.Direction
	m.IsTenantAdmin, m.ShowDetails = true, true
	m.Conversations[0].OwnerID = m.CurrentUser
	m.Members = []Member{{ID: "alice", Name: "Alice", HomeTenantID: "tenant"}, {ID: "bob", Name: "Bob", HomeTenantID: "tenant"}}
	m.Chatattach001 = &Chatattach001Composer{Choose: func() {}}
	m.Callbacks.OpenChannelPoll, m.Callbacks.OpenChannelTodo = func(string) {}, func() {}
	m.Callbacks.SavePreferences = func(Preferences) {}
	m.Callbacks.OpenBrowse, m.Callbacks.OpenCreate = func() {}, func() {}
	m.Callbacks.ShareAgentAnswer = func(string) {}
	m.Callbacks.SetConversationNotification = func(string, NotificationMode) {}
	m.ShowThread, m.ThreadParentID = true, "question"
	m.ThreadMessages = []Message{{ID: "reply", Author: "Bob", AuthorID: "bob", Body: "Thanks!", TimeLabel: "9:32"}}
	m.FilterSettings = func() ui.Node { return ModAdminPanel(ModAdminProps{Model: m, CanManage: true}) }
	h := handlers{local: localUI{
		commandMenu:  composerCommandMenu{Target: "chat-composer", Open: true},
		detailGroups: map[string]bool{chatux005GroupManage: true, chatux005GroupProject: true},
	}, mentionView: mentionState{Target: "chat-composer", Open: true}}
	surfaces := map[string]string{}
	withEmojiHost(t, m, func(*localUI) {
		add := func(name string, node ui.Node) { surfaces[name] = renderNode(t, node) }
		add("conversation, messages and header", Build(m))
		add("composer with add and command menus", composer(m, h))
		add("mention menu", mentionMenu(m, h.mentionView, "chat-composer"))
		add("sidebar with preferences and Channels menu", rail(m, h))
		add("details with Manage channel open", details(m, h))
		add("thread pane", threadPane(m, h))
		rows := personaReplyRowsForPost(m, h.local, "question", time.Now())
		if len(rows) != 1 {
			t.Fatalf("answer-card fixture: got %d rows", len(rows))
		}
		add("agent answer card", rows[0])
		add("filters settings with editor", ModAdminPanel(ModAdminProps{Model: m, CanManage: true, Workspace: true, Editor: &ModEditorState{Open: true, Kind: ModKindWords, Action: "block"}}))
		add("Saved panel", RenderSavedMessages(SavedMessagesView{
			Locale: m.Locale, Model: m, TodoCount: 1, AllCount: 1,
			Active: "question", ReminderMenu: "question", Now: time.Now(),
			Rows: []SavedMessageRow{{PostID: "question", ConversationID: m.SelectedID, Author: "Alice", AuthorID: "alice", Channel: "general", InChannel: true, Body: "Check this policy", SentAt: time.Now()}},
		}))
		add("search results", RenderChatSearch(m.Locale, ChatSearchView{Query: "policy", Response: chatsearch.Response{
			Groups: []chatsearch.Group{{Kind: chatsearch.Message, Rows: []chatsearch.Row{{Kind: chatsearch.Message, Text: "Check this policy"}}}},
		}}))
		add("moderation dialog", ModerationDialog(ModerationDialogModel{Model: m, Report: true, Target: ModerationTargetView{AuthorName: "Bob", Body: "A message"}, Selection: chat.RemovalSelection{PostIDs: []string{"question"}, ConversationID: m.SelectedID}}))
		add("blocked draft", RenderFilterBlockedDraft(m, "Check this policy", chatfilter.Span{Start: 0, End: 5}))
		add("masked message", RenderFilterMaskedText(m, "[removed word]"))
		add("language settings", RenderingSettings(RenderingSettingsModel{Locale: m.Locale}))
		add("writing style toolbar", RenderChattoneToolbar(ChattoneToolbarProps{Locale: m.Locale, Enabled: true, Styles: chatrewrite.DefaultStyles(), Draft: "Please check this policy"}))
		add("voice player", RenderVoicePlayer(VoicePlayerProps{Locale: m.Locale, ID: "voice", URL: "/media/voice", DurationMS: 1000}))
		add("writing style preview", RenderChattonePreview(ChattonePreviewProps{Locale: m.Locale, Original: "Original", Rewritten: "Reviewed", ShowChanges: true}))
		add("voice composer", RenderVoiceComposer(VoiceComposerProps{Locale: m.Locale, ConversationID: m.SelectedID, Enabled: true}))
		add("language status", RenderingLanguageStatusIndicator(m.Locale, map[string]int{"en": 2, "de": 1}, false, false))
		add("language view", RenderingView(m.Locale, chatrender.Rendering{Text: "Reviewed translation"}, chatrender.Mark{State: "ready", Kinds: []chatrender.Kind{chatrender.Translate}, SourceLanguage: "en", CanShowOriginal: true}, ui.Handler{}, ui.Handler{}))
		add("personal language settings", RenderingPersonalSettings(m.Locale, m.SelectedID))
		add("ambient task card", RenderAgentUXAmbientCard(m, AgentUXAmbientCard{ID: "task", Conversation: m.SelectedID, Scope: "PRIVATE", Person: m.CurrentUser, AgentName: "Policy assistant", Kind: "TASK", State: "SUGGESTED", Reason: "self_commitment", Title: "Read this policy", CanManage: true}))
		add("ambient reminder editor", RenderAgentUXAmbientCard(m, AgentUXAmbientCard{ID: "reminder", Conversation: m.SelectedID, Scope: "CHANNEL", AgentName: "Policy assistant", Kind: "REMINDER", State: "NEEDS_TIME", Reason: "addressed", Title: "Read this policy", CanManage: true, Error: true}))
		add("ambient list loading", RenderAgentUXAmbientList(m, nil, "loading"))
		add("ambient list error", RenderAgentUXAmbientList(m, nil, "error"))
		add("ambient list empty", RenderAgentUXAmbientList(m, nil, ""))
		add("agent announcement", RenderAgentAnnouncementMessage(m, AgentAnnouncementMessage{AgentName: "Policy assistant", OwnerName: "Alice", Text: "Check this policy", Scheduled: true}))
		add("agent progress", RenderPersonaProgress(m, PersonaProgressProjection{ViewerID: m.CurrentUser, InvokerID: m.CurrentUser, AgentName: "Policy assistant", Progress: &PersonaProgressProps{Visible: true, InvokerID: m.CurrentUser, AgentName: "Policy assistant", ElapsedSeconds: 20}}))
		approvalModel := m
		approvalModel.CurrentUser = "user-1"
		now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		add("agent approval card", RenderPersonaApprovalCard(approvalModel, personaApprovalCardFixture(t, now), now))
		add("join gate", RenderGate(GateView{Locale: m.Locale, State: "loading"}))
		direct := m
		direct.Conversations = []Conversation{{ID: m.SelectedID, Kind: DirectMessage, Name: "Bob", Joined: true}}
		add("direct composer with voice menu", composer(direct, h))
		add("search return bar", RenderChatSearchReturn(m.Locale))
		add("translation administration", TranslationWorkspaceForm(TranslationAdminModel{Locale: m.Locale, Loaded: true, Data: TranslationAdminData{CanManageWorkspace: true, Supported: []string{"en", "de", "ar"}}}, m.Text))
		add("translation channel row", TranslationChannelRow(TranslationAdminModel{Locale: m.Locale, Loaded: true, Data: TranslationAdminData{Workspace: chatlang.Workspace{Enabled: true}, Channel: &chatlang.Channel{}, CanManageChannel: true}}, m.Text))
		poll, err := chat.Chatcmd003ParsePoll("Where? 1=Here 2=There", time.Now(), nil)
		if err != nil {
			t.Fatal(err)
		}
		add("poll command card", Chatcmd002RenderCard(m, "poll", chat.Chatcmd002View{Card: poll.Card, ResultsVisible: true}, false))
		add("poll command preview", Chatcmd003RenderPreview(m, poll))
		todo, err := chat.Chatcmd004ParseTodo("Launch 1=Send invites", nil, m.CurrentUser, time.Now(), chat.Chatcmd004ResolveDate)
		if err != nil {
			t.Fatal(err)
		}
		add("todo command preview", Chatcmd003RenderPreview(m, todo))
		add("moderation tombstone", ModerationTombstone(m, Message{ID: "removed", AuthorID: "bob", Author: "Bob"}, true))
		add("moderation page", ModerationPage(ModerationPageModel{Locale: m.Locale, State: StateReady}))
		add("moderation author notice", ModerationAuthorNotice(m.Locale, chat.ModerationNotice{Outcome: "remove", CanAppeal: true}))
		add("moderation restored notice", ModerationRestoredNotice(m.Locale))
		add("moderation menu entries", ui.Fragment(ModerationMenuEntries(m.Locale, m.SelectedID, "question", true)...))
		status := ChannelStatusView{Status: chat.ChannelStatus{TenantID: m.CurrentTenantID, ConversationID: m.SelectedID, Name: "general", Status: chatpolicy.StatusLocked, Revision: 1}, ActorName: "Alice", Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusOpen}}}
		add("channel status badge", ChannelStatusBadge(m, status))
		add("channel status chip", ChannelStatusChip(m, status))
		add("channel status composer notice", ChannelStatusComposerNotice(m, status))
		add("channel status panel", ChannelStatusPanel(ChannelStatusPanelProps{Model: m, View: status, Now: time.Now(), Change: func(chat.ChangeChannelStatusRequest) {}}))
		add("channel status system line", ChannelStatusSystemLine(m, status))
		add("channel status directory", ChannelStatusDirectory(ChannelStatusDirectoryProps{Model: m, Views: []ChannelStatusView{status}, Open: func(string) {}}))
		add("location share sheet", ChatmapShareSheet(m.Locale, "location", m.CurrentTenantID, m.SelectedID, false))
		add("location embed", ChatmapLocationEmbed(ChatmapEmbed{Locale: m.Locale, Now: time.Now(), Sharer: "Alice", Share: chat.LocationShare{ID: "location", SharedAt: time.Now(), Place: chat.LocationPlace{Label: "Office", Position: &chat.LocationPosition{Latitude: 40, Longitude: -74, Accuracy: 10}}}}))
		add("agent profile", PersonaProfileCard(m.Locale, m.ResolvedPersonaMentions[0]))
		add("agent badge", AgentBadgeLabel(m.Locale))
		add("ephemeral message", RenderEphemeralMessage(m, m.EphemeralMessages[0], time.Now()))
	})
	surfaces["emoji picker"] = EmojiPickerMarkupForTest(t, m, "browse")
	surfaces["emoji search"] = EmojiPickerMarkupForTest(t, m, "search")
	surfaces["emoji tone menu"] = EmojiPickerMarkupForTest(t, m, "tone")
	return surfaces
}
