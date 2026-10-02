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

func chatlangAdminMarkup(t *testing.T, m chatui.TranslationAdminModel, text func(string) string) string {
	t.Helper()
	markup, err := ui.RenderToString(chatui.TranslationAdminForm(m, text))
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(markup)
}

// TestTodo_CHATLANG_006_Browser renders the administration with the product's
// own catalog (the one that answers an unknown key with a marker) in each
// language and checks that every control has its words and no copy key shows.
func TestTodo_CHATLANG_006_Browser(t *testing.T) {
	want := map[string][]string{
		"en-US": {"Whole workspace", "Translate messages into each reader's language", "Monthly limit (USD)", "Used this month: 2.50 of 2.50", "Translation is paused", "This channel", "Messages here are never sent to an outside translation service", "Glossary", "Keep as written", "Acme Cloud (kept as written)", "time off → Urlaub (Deutsch)", "Add term", "Save"},
		"de-DE": {"Gesamter Arbeitsbereich", "Nachrichten in die Sprache jedes Lesers übersetzen", "Monatliches Limit (USD)", "Diesen Monat verbraucht: 2.50 von 2.50", "Die Übersetzung pausiert", "Dieser Kanal", "Nachrichten hier werden nie an einen externen Übersetzungsdienst gesendet", "Glossar", "Unverändert lassen", "Acme Cloud (unverändert)", "time off → Urlaub (Deutsch)", "Begriff hinzufügen", "Speichern"},
		"ar":    {"مساحة العمل كاملة", "ترجم الرسائل إلى لغة كل قارئ", "الحد الشهري (دولار أمريكي)", "المستخدم هذا الشهر: 2.50 من 2.50", "الترجمة متوقفة", "هذه القناة", "لا تُرسل الرسائل هنا إلى خدمة ترجمة خارجية أبداً", "المسرد", "أبقه كما هو", "Acme Cloud (يبقى كما هو)", "أضف مصطلحاً", "احفظ"},
	}
	for locale, words := range want {
		ctx := productui.ResolveProductLocale(locale)
		markup := chatlangAdminMarkup(t, chatui.TranslationAdminModel{Locale: ctx.Resolved, Conversation: "general", Data: chatlangAdminData(), Loaded: true}, func(key string) string { return ctx.Text(key) })
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
		// Every field has a label; every form is named by its heading.
		for _, id := range []string{"chatlangadmin-enabled", "chatlangadmin-limit", "chatlangadmin-external", "chatlangadmin-channel-switch", "chatlangadmin-channel-barred", "chatlangadmin-term", "chatlangadmin-mode", "chatlangadmin-target"} {
			if !strings.Contains(markup, `for="`+id+`"`) {
				t.Errorf("%s: field %s has no label", locale, id)
			}
		}
		if strings.Count(markup, "<form") != 3 || strings.Count(markup, `aria-labelledby="chatlangadmin-`) < 3 {
			t.Errorf("%s: expected three named forms: %s", locale, markup)
		}
	}
}

func TestTodo_CHATLANG_006_AdminStates(t *testing.T) {
	ctx := productui.ResolveProductLocale("en-US")
	render := func(m chatui.TranslationAdminModel) string {
		m.Locale = ctx.Resolved
		return chatlangAdminMarkup(t, m, func(key string) string { return ctx.Text(key) })
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
	// A channel manager sees their channel and nothing of the workspace.
	manager := chatlangAdminData()
	manager.CanManageWorkspace = false
	got := render(chatui.TranslationAdminModel{Loaded: true, Data: manager})
	if strings.Contains(got, "Whole workspace") || strings.Contains(got, "Glossary") || !strings.Contains(got, "This channel") || strings.Count(got, "<form") != 1 {
		t.Fatalf("a channel manager's view: %s", got)
	}
	// A workspace administrator who is not looking at a channel sees no channel form.
	admin := chatlangAdminData()
	admin.CanManageChannel, admin.Channel = false, nil
	if got := render(chatui.TranslationAdminModel{Loaded: true, Data: admin}); strings.Contains(got, "This channel") || strings.Count(got, "<form") != 2 {
		t.Fatalf("workspace view: %s", got)
	}
	// Saving and saved are said in words, in a live region.
	saved := render(chatui.TranslationAdminModel{Loaded: true, Saved: true, Data: chatlangAdminData()})
	saving := render(chatui.TranslationAdminModel{Loaded: true, Loading: true, Data: chatlangAdminData()})
	if !strings.Contains(saved, "Saved.") || !strings.Contains(saving, "disabled") || !strings.Contains(saved, `aria-live="polite"`) {
		t.Fatalf("saved %v / saving %v", strings.Contains(saved, "Saved."), strings.Contains(saving, "disabled"))
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

// TestTodo_CHATLANG_006_Browser_Details: the translation section sits in
// Conversation details beside Filters, for the people who may administer the
// conversation, and only where the server has an engine composed.
func TestTodo_CHATLANG_006_Browser_Details(t *testing.T) {
	for locale, words := range map[string][2]string{
		"en-US": {"Translation", "Manage translation"},
		"de-DE": {"Übersetzung", "Übersetzung verwalten"},
		"ar":    {"الترجمة", "أدر الترجمة"},
	} {
		on := &chatui.ChatFeatures{Translation: true}
		for _, viewer := range []string{"admin", "manager"} {
			panel := chatlangDetails(t, locale, viewer, on)
			if strings.Contains(panel, "⟦") {
				t.Errorf("%s/%s: the details panel prints a copy key: %s", locale, viewer, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(panel))
			}
			for _, text := range words {
				if !strings.Contains(panel, text) {
					t.Errorf("%s/%s: the translation section misses %q", locale, viewer, text)
				}
			}
		}
		if panel := chatlangDetails(t, locale, "member", on); strings.Contains(panel, words[1]) {
			t.Errorf("%s: a plain member sees the translation settings", locale)
		}
		for name, features := range map[string]*chatui.ChatFeatures{"no engine": {Translation: false}, "features unknown": nil} {
			if panel := chatlangDetails(t, locale, "admin", features); strings.Contains(panel, words[1]) {
				t.Errorf("%s: the translation settings are offered with %s", locale, name)
			}
		}
	}
}
