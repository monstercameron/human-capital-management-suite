package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func chatlangAdminData() chatui.TranslationAdminData {
	d := chatui.TranslationAdminData{
		Workspace:          chatlang.Workspace{Enabled: true, Languages: []string{"de", "fr"}, BudgetMicros: 2_500_000, ExternalAllowed: true},
		Channel:            &chatlang.Channel{Translation: chatlang.Inherit, External: chatlang.ExternalBarred},
		Effective:          chatlang.ReasonExternalBarred,
		Glossary:           []chatui.TranslationAdminTerm{{ID: "t1", Term: chatlang.Term{Source: "Acme Cloud"}}, {ID: "t2", Term: chatlang.Term{Source: "time off", Language: "de", Target: "Urlaub"}}},
		SpentMicros:        2_500_000,
		BudgetMicros:       2_500_000,
		Paused:             true,
		Supported:          chatlang.SupportedLanguages(),
		CanManageWorkspace: true,
		CanManageChannel:   true,
	}
	d.Engine.Name, d.Engine.Ready, d.Engine.External = "governed model route", true, true
	return d
}

func chatlangWorkspaceMarkup(t *testing.T, m chatui.TranslationAdminModel, text func(string) string) string {
	t.Helper()
	markup, err := ui.RenderToString(chatui.TranslationWorkspaceForm(m, text))
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(markup)
}

func chatlangChannelMarkup(t *testing.T, m chatui.TranslationAdminModel, text func(string) string) string {
	t.Helper()
	node := chatui.TranslationChannelRow(m, text)
	if node == nil {
		return ""
	}
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(markup)
}

// TestTodo_CHATLANG_006_Browser renders the workspace's translation settings
// with the product's own catalog (the one that answers an unknown key with a
// marker) in each language and checks that every control has its words and no
// copy key shows.
func TestTodo_CHATLANG_006_Browser(t *testing.T) {
	want := map[string][]string{
		"en-US": {"Translation", "Translate messages into each reader's language", "Monthly limit (USD)", "Used this month: 2.50 of 2.50", "Translation is paused", "Glossary", "Keep as written", "Acme Cloud (kept as written)", "time off → Urlaub (Deutsch)", "Add term"},
		"de-DE": {"Übersetzung", "Nachrichten in die Sprache jedes Lesers übersetzen", "Monatliches Limit (USD)", "Diesen Monat verbraucht: 2.50 von 2.50", "Die Übersetzung pausiert", "Glossar", "Unverändert lassen", "Acme Cloud (unverändert)", "time off → Urlaub (Deutsch)", "Begriff hinzufügen"},
		"ar":    {"الترجمة", "ترجم الرسائل إلى لغة كل قارئ", "الحد الشهري (دولار أمريكي)", "المستخدم هذا الشهر: 2.50 من 2.50", "الترجمة متوقفة", "المسرد", "أبقه كما هو", "Acme Cloud (يبقى كما هو)", "أضف مصطلحاً"},
	}
	for locale, words := range want {
		ctx := productui.ResolveProductLocale(locale)
		markup := chatlangWorkspaceMarkup(t, chatui.TranslationAdminModel{Locale: ctx.Resolved, Data: chatlangAdminData(), Loaded: true}, func(key string) string { return ctx.Text(key) })
		if strings.Contains(markup, "⟦") {
			t.Errorf("%s: the administration prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(markup))
		}
		if strings.Contains(markup, "chat.langadmin.") || strings.Contains(markup, "<style") || strings.Contains(markup, " style=") {
			t.Errorf("%s: a key, a style element or a style attribute reached the page", locale)
		}
		for _, text := range words {
			if !strings.Contains(markup, text) {
				t.Errorf("%s: missing %q", locale, text)
			}
		}
		wantDir := `dir="ltr"`
		if locale == "ar" {
			wantDir = `dir="rtl"`
		}
		if !strings.Contains(markup, wantDir) {
			t.Errorf("%s: the administration does not set its direction (%s)", locale, wantDir)
		}
		// Every field has a label, every form is named by its heading, and
		// nothing is saved by a button: a change is saved as it is made.
		for _, id := range []string{"chatlangadmin-enabled", "chatlangadmin-limit", "chatlangadmin-external", "chatlangadmin-term", "chatlangadmin-mode", "chatlangadmin-target"} {
			if !strings.Contains(markup, `for="`+id+`"`) {
				t.Errorf("%s: field %s has no label", locale, id)
			}
		}
		if strings.Count(markup, "<form") != 2 || strings.Count(markup, `aria-labelledby="chatlangadmin-`) < 2 {
			t.Errorf("%s: expected two named forms: %s", locale, markup)
		}
		if strings.Contains(markup, `type="submit"`) && strings.Count(markup, `type="submit"`) != 1 {
			t.Errorf("%s: the settings still have Save buttons: %d submit buttons", locale, strings.Count(markup, `type="submit"`))
		}
	}
}

// TestTodo_CHATLANG_007_Browser: the channel's row, in each language: one row
// "Translation" with the channel's choice at the right, under it the choice and
// "Never use an outside service", and no copy key.
func TestTodo_CHATLANG_007_Browser(t *testing.T) {
	want := map[string][]string{
		"en-US": {"Translation", "Follow the workspace", "Translation in this channel", "Never use an outside service", "Messages here are never sent to an outside translation service"},
		"de-DE": {"Übersetzung", "Wie der Arbeitsbereich", "Übersetzung in diesem Kanal", "Nie einen externen Dienst verwenden", "Nachrichten hier werden nie an einen externen Übersetzungsdienst gesendet"},
		"ar":    {"الترجمة", "اتبع مساحة العمل", "الترجمة في هذه القناة", "عدم استخدام خدمة خارجية أبداً", "لا تُرسل الرسائل هنا إلى خدمة ترجمة خارجية أبداً"},
	}
	for locale, words := range want {
		ctx := productui.ResolveProductLocale(locale)
		text := func(key string) string { return ctx.Text(key) }
		markup := chatlangChannelMarkup(t, chatui.TranslationAdminModel{Locale: ctx.Resolved, Loaded: true, Data: chatlangAdminData()}, text)
		if strings.Contains(markup, "⟦") {
			t.Errorf("%s: the row prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(markup))
		}
		for _, w := range words {
			if !strings.Contains(markup, w) {
				t.Errorf("%s: the translation row misses %q: %s", locale, w, markup)
			}
		}
		if strings.Contains(markup, "Save") || strings.Contains(markup, `type="submit"`) || strings.Contains(markup, "Whole workspace") || strings.Contains(markup, "Glossary") {
			t.Errorf("%s: the row carries a Save button or workspace settings: %s", locale, markup)
		}
		if !strings.Contains(markup, "manage-row-summary") || strings.Count(markup, "<form") != 1 {
			t.Errorf("%s: the row is not the panel's disclosure row with one form: %s", locale, markup)
		}
	}
}

func TestTodo_CHATLANG_007(t *testing.T) {
	ctx := productui.ResolveProductLocale("en-US")
	text := func(key string) string { return ctx.Text(key) }
	row := func(m chatui.TranslationAdminModel) string {
		m.Locale = ctx.Resolved
		return chatlangChannelMarkup(t, m, text)
	}
	workspace := func(m chatui.TranslationAdminModel) string {
		m.Locale = ctx.Resolved
		return chatlangWorkspaceMarkup(t, m, text)
	}

	// While the workspace has translation off, the channel has no row: one line
	// says so, with the link to the administration page only for the people who
	// may change it.
	off := chatlangAdminData()
	off.Workspace.Enabled = false
	got := row(chatui.TranslationAdminModel{Loaded: true, Data: off})
	if !strings.Contains(got, "Translation is off for this workspace.") || !strings.Contains(got, `href="/workspace/app/admin/chat-settings`) || strings.Contains(got, "<form") {
		t.Errorf("workspace off, administrator: %s", got)
	}
	off.CanManageWorkspace = false
	got = row(chatui.TranslationAdminModel{Loaded: true, Data: off})
	if !strings.Contains(got, "Translation is off for this workspace.") || strings.Contains(got, "<a ") || strings.Contains(got, "<form") {
		t.Errorf("workspace off, channel manager: %s", got)
	}

	// Nothing until the settings have loaded, nothing when refused, one line
	// when they could not load.
	if got := row(chatui.TranslationAdminModel{}); got != "" {
		t.Errorf("a row before the settings loaded: %s", got)
	}
	if got := row(chatui.TranslationAdminModel{Loaded: true, Denied: true}); got != "" {
		t.Errorf("a row for a refused reader: %s", got)
	}
	if got := row(chatui.TranslationAdminModel{Failed: true}); !strings.Contains(got, "could not load or save") {
		t.Errorf("a failed read says nothing: %s", got)
	}
	// A person who may change neither sees no row.
	plain := chatlangAdminData()
	plain.CanManageChannel, plain.Channel = false, nil
	if got := row(chatui.TranslationAdminModel{Loaded: true, Data: plain}); got != "" {
		t.Errorf("a row for someone who may not change the channel: %s", got)
	}

	// The value at the right follows the choice, and "Saved" stands beside it.
	for choice, value := range map[chatlang.Switch]string{chatlang.Inherit: "Follow the workspace", chatlang.On: ">On<", chatlang.Off: ">Off<"} {
		d := chatlangAdminData()
		d.Channel.Translation = choice
		if got := row(chatui.TranslationAdminModel{Loaded: true, Data: d}); !strings.Contains(got, value) {
			t.Errorf("choice %q: the row does not show %q: %s", choice, value, got)
		}
	}
	saved := row(chatui.TranslationAdminModel{Loaded: true, Saved: true, SavedField: "channel", Data: chatlangAdminData()})
	if !strings.Contains(saved, "chatlangadmin-saved") || !strings.Contains(saved, ">Saved<") || !strings.Contains(saved, `aria-live="polite"`) {
		t.Errorf("no Saved beside the channel's setting: %s", saved)
	}

	// The workspace page: each setting says "Saved" beside itself, only the one
	// that was saved, and no control turns off while it is not saving.
	for field, id := range map[string]string{"enabled": "chatlangadmin-enabled", "external": "chatlangadmin-external", "limit": "chatlangadmin-limit", "language": "chatlangadmin-language-de"} {
		got := workspace(chatui.TranslationAdminModel{Loaded: true, Saved: true, SavedField: field, Data: chatlangAdminData()})
		if strings.Count(got, "chatlangadmin-saved") != 1 || !strings.Contains(got, `id="`+id+`"`) {
			t.Errorf("saving %q: %d Saved marks: %s", field, strings.Count(got, "chatlangadmin-saved"), got)
		}
	}
	if got := workspace(chatui.TranslationAdminModel{Loaded: true, Data: chatlangAdminData()}); strings.Contains(got, "chatlangadmin-saved") {
		t.Errorf("Saved before anything was saved: %s", got)
	}
	if got := workspace(chatui.TranslationAdminModel{Loaded: true, Saved: true, Loading: true, SavedField: "enabled", Data: chatlangAdminData()}); strings.Contains(got, "chatlangadmin-saved") || !strings.Contains(got, "disabled") {
		t.Errorf("Saved while a save is still going: %s", got)
	}
	// The settings belong to the page's administrators.
	manager := chatlangAdminData()
	manager.CanManageWorkspace = false
	if got := workspace(chatui.TranslationAdminModel{Loaded: true, Data: manager}); strings.Contains(got, "Glossary") || strings.Contains(got, "chatlangadmin-enabled") {
		t.Errorf("a channel manager sees the workspace settings: %s", got)
	}

	// The page carries its own rules: the chat sheet is scoped to the chat
	// workspace, which the administration page is not inside.
	for _, rule := range []string{".chatlangadmin-saved", ".chatlangadmin-setting", ".chatlangadmin{"} {
		if !strings.Contains(chatui.TranslationAdminPageStyles, rule) {
			t.Errorf("the administration page's styles lack %s", rule)
		}
	}
	if regexp.MustCompile(`#[0-9a-fA-F]{3,6}\b`).MatchString(chatui.TranslationAdminPageStyles) {
		t.Error("the administration page's styles hold a raw colour")
	}
}

func TestTodo_CHATLANG_006_AdminStates(t *testing.T) {
	ctx := productui.ResolveProductLocale("en-US")
	render := func(m chatui.TranslationAdminModel) string {
		m.Locale = ctx.Resolved
		return chatlangWorkspaceMarkup(t, m, func(key string) string { return ctx.Text(key) })
	}
	if got := render(chatui.TranslationAdminModel{}); !strings.Contains(got, "Loading translation settings") || !strings.Contains(got, `aria-busy="true"`) {
		t.Fatalf("loading: %s", got)
	}
	if got := render(chatui.TranslationAdminModel{Failed: true}); !strings.Contains(got, "could not load or save") || !strings.Contains(got, `role="alert"`) {
		t.Fatalf("failed: %s", got)
	}
	if got := render(chatui.TranslationAdminModel{Loaded: true, Denied: true}); !strings.Contains(got, "Only a workspace administrator or this channel's manager") {
		t.Fatalf("denied: %s", got)
	}
	// Saving is said in words, in a live region, and the controls rest meanwhile.
	saving := render(chatui.TranslationAdminModel{Loaded: true, Loading: true, Data: chatlangAdminData()})
	if !strings.Contains(saving, "disabled") || !strings.Contains(saving, `aria-live="polite"`) || !strings.Contains(saving, "Loading translation settings") {
		t.Fatalf("saving: %s", saving)
	}
	// Nothing paused, nothing said about a pause.
	calm := chatlangAdminData()
	calm.Paused, calm.SpentMicros = false, 100_000
	if got := render(chatui.TranslationAdminModel{Loaded: true, Data: calm}); strings.Contains(got, "paused") || !strings.Contains(got, "Used this month: 0.10 of 2.50") {
		t.Fatalf("calm: %s", got)
	}
	for micros, want := range map[int64]string{0: "0.00", 5_000_000: "5.00", 1_234_567: "1.23", 999_999: "0.99", -5: "0.00", 10_050_000: "10.05"} {
		if got := chatui.TranslationAdminAmount(micros); got != want {
			t.Errorf("amount %d = %q, want %q", micros, got, want)
		}
	}
}

// chatlangDetails renders the Conversation details panel of #general for an
// administrator, a channel manager or a member, with the product's catalog.
func chatlangDetails(t *testing.T, locale, viewer string, features *chatui.ChatFeatures) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, OwnerID: "owner", MemberCount: 3, Joined: true}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), ShowDetails: true, SelectedID: room.ID, CurrentUser: map[string]string{"admin": "walt", "manager": "walt", "member": "jake"}[viewer], CurrentTenantID: "t", IsTenantAdmin: viewer == "admin",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{room},
		Members:       []chatui.Member{{ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}, {ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}},
		ChannelTeam:   chatui.ChannelTeamWidget{Members: []chatui.ChannelTeamMember{{HomeTenantID: "t", SubjectID: "jake", Role: "MEMBERSHIP_ROLE_MEMBER"}, {HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}},
		ChatFeatures:  features,
		Callbacks:     chatui.Callbacks{ToggleDetails: func(bool) {}},
	}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(page, `chat-side chat-details"`)
	if start < 0 {
		t.Fatalf("%s: no details panel", locale)
	}
	start = strings.LastIndex(page[:start], "<aside")
	end := strings.Index(page[start:], "</aside>")
	if end < 0 {
		t.Fatalf("%s: unterminated details panel", locale)
	}
	return html.UnescapeString(page[start : start+end])
}

// TestTodo_CHATLANG_006_Browser_Details: the translation entry sits in
// Conversation details under Manage channel's rules, for the people who may
// administer the conversation, and only where the server has an engine
// composed. Its row appears once the settings have loaded, so a page drawn
// before that shows no whole-workspace settings and no second Save button.
func TestTodo_CHATLANG_006_Browser_Details(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		on := &chatui.ChatFeatures{Translation: true}
		for _, viewer := range []string{"admin", "manager"} {
			panel := chatlangDetails(t, locale, viewer, on)
			if strings.Contains(panel, "⟦") {
				t.Errorf("%s/%s: the details panel prints a copy key: %s", locale, viewer, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(panel))
			}
			for _, gone := range []string{"chatlangadmin-enabled", "chatlangadmin-limit", "chatlangadmin-glossary", "Manage translation"} {
				if strings.Contains(panel, gone) {
					t.Errorf("%s/%s: the workspace's translation settings are still in a channel's details (%s)", locale, viewer, gone)
				}
			}
		}
	}
	room := chatui.Conversation{ID: "general", Kind: chatui.PublicChannel, OwnerID: "owner"}
	base := chatui.Model{SelectedID: "general", CurrentUser: "owner", CurrentTenantID: "t", ChatFeatures: &chatui.ChatFeatures{Translation: true}}
	if got := chatui.TranslationSettingsEntryForTest(base, room); got == nil {
		t.Error("the owner is offered no translation entry")
	}
	member := base
	member.CurrentUser = "jake"
	if got := chatui.TranslationSettingsEntryForTest(member, room); got != nil {
		t.Error("a plain member is offered the translation entry")
	}
	for name, features := range map[string]*chatui.ChatFeatures{"no engine": {Translation: false}, "features unknown": nil} {
		offered := base
		offered.ChatFeatures = features
		if got := chatui.TranslationSettingsEntryForTest(offered, room); got != nil {
			t.Errorf("the translation entry is offered with %s", name)
		}
	}
}
