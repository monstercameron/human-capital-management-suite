package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// modBrowserModel is a model whose copy comes from the product's own catalog,
// the one that answers a key it does not know with the key in brackets.
func modBrowserModel(locale string, admin bool) chatui.Model {
	ctx := productui.ResolveProductLocale(locale)
	return chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) },
		SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "t", IsTenantAdmin: admin,
		Conversations: []chatui.Conversation{{ID: "room", Name: "general", Kind: chatui.PublicChannel}, {ID: "ops", Name: "operations", Kind: chatui.PrivateChannel}, {ID: "dm1", Name: "Jake", Kind: chatui.DirectMessage}}}
}

func modRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(markup)
}

var (
	modSwitchTag  = regexp.MustCompile(`<button[^>]*role="switch"[^>]*>`)
	modTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)
)

// modSwitches maps a rule id to the aria-checked of its switch.
func modSwitches(markup string) map[string]string {
	out := map[string]string{}
	for _, tag := range modSwitchTag.FindAllString(markup, -1) {
		id := regexp.MustCompile(`data-id="([^"]*)"`).FindStringSubmatch(tag)
		checked := regexp.MustCompile(`aria-checked="([^"]*)"`).FindStringSubmatch(tag)
		if id != nil && checked != nil {
			out[id[1]] = checked[1]
		}
	}
	return out
}

// modVisible is the text a reader sees: the markup without its tags.
func modVisible(markup string) string { return modTagPattern.ReplaceAllString(markup, " ") }

func modNoLeaks(t *testing.T, label, markup string) {
	t.Helper()
	if strings.Contains(markup, "⟦") {
		t.Errorf("%s prints a copy key: %s", label, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(markup))
	}
	for _, banned := range []string{"<style", " style=", "context canceled", "builtin-", "invalid definition", "chat.filters.", "modadmin_"} {
		if strings.Contains(modVisible(markup), banned) || banned == "<style" && strings.Contains(markup, banned) || banned == " style=" && strings.Contains(markup, banned) {
			t.Errorf("%s shows %q", label, banned)
		}
	}
}

func modBuiltinsAndCustoms(channel string) ([]chatfilter.Definition, []chatfilter.Enablement) {
	defs := append(chatfilter.Builtins(),
		chatfilter.Definition{ID: "own-1", Name: "Project names", Version: "1.0.0", Kind: "words", Action: "mask", Match: []string{"phoenix", "acme"}, Channels: []string{channel}},
		chatfilter.Definition{ID: "wide-1", Name: "Card numbers", Version: "1.0.0", Kind: "detector", Action: "block", Match: []string{"card"}, Hard: true},
	)
	rows := []chatfilter.Enablement{
		// the channel's row comes before the workspace's: the order must not decide
		{RuleID: "builtin-en-slurs", Channel: channel, Enabled: false, Action: "block"},
		{RuleID: "builtin-en-slurs", Enabled: true, Action: "block"},
		{RuleID: "builtin-en-profanity", Enabled: true, Action: "block"},
		{RuleID: "builtin-de-profanity", Channel: channel, Enabled: true, Action: "mask"},
		{RuleID: "builtin-de-slurs", Enabled: true, Action: "block"},
		{RuleID: "builtin-de-slurs", Channel: channel, Enabled: true, Action: "flag"},
		{RuleID: "own-1", Channel: channel, Enabled: true},
		{RuleID: "wide-1", Enabled: true},
	}
	return defs, rows
}

func TestTodo_CHATMOD_002_Lists_Browser(t *testing.T) {
	type words struct {
		wsOn, off, chanOnly, chanOwn, chanOff, what, reset, own, admin, builtin, ownRule string
		listNames                                                                        [3]string
		languages                                                                        [3]string
	}
	copyOf := map[string]words{
		"en-US": {"On for the whole workspace", "Off", "On in this channel only", "On in this channel with its own choice (the workspace has it on too)", "Off in this channel (on for the workspace)", "What happens", "Use the workspace setting", "This channel's own filters", "Set by your workspace administrators", "Built-in word lists", "Hides a word list from readers", [3]string{"Profanity", "Slurs", "Harassment"}, [3]string{"English", "Deutsch", "العربية"}},
		"de-DE": {"Für den ganzen Arbeitsbereich eingeschaltet", "Aus", "Nur in diesem Kanal eingeschaltet", "In diesem Kanal mit eigener Wahl eingeschaltet (im Arbeitsbereich ebenfalls eingeschaltet)", "In diesem Kanal ausgeschaltet (im Arbeitsbereich eingeschaltet)", "Was passiert", "Einstellung des Arbeitsbereichs verwenden", "Eigene Filter dieses Kanals", "Von Ihren Arbeitsbereichsadministratoren festgelegt", "Integrierte Wortlisten", "Blendet eine Wortliste für Leser aus", [3]string{"Flüche", "Abwertende Ausdrücke", "Belästigung"}, [3]string{"English", "Deutsch", "العربية"}},
		"ar":    {"مفعّلة لمساحة العمل بأكملها", "متوقفة", "مفعّلة في هذه القناة فقط", "مفعّلة في هذه القناة باختيارها الخاص (وهي مفعّلة أيضاً لمساحة العمل)", "متوقفة في هذه القناة (ومفعّلة لمساحة العمل)", "ماذا يحدث", "استخدم إعداد مساحة العمل", "المرشحات الخاصة بهذه القناة", "من إعداد مسؤولي مساحة العمل", "قوائم الكلمات المدمجة", "يخفي قائمة كلمات عن القراء", [3]string{"الألفاظ النابية", "الإهانات التمييزية", "المضايقة"}, [3]string{"English", "Deutsch", "العربية"}},
	}
	defs, rows := modBuiltinsAndCustoms("room")
	for locale, w := range copyOf {
		t.Run(locale, func(t *testing.T) {
			m := modBrowserModel(locale, false)
			panel := func(p chatui.ModAdminProps) string {
				p.Model = m
				p.CanManage = true
				return modRender(t, chatui.ModAdminPanel(p))
			}
			markup := panel(chatui.ModAdminProps{Channel: "room", Definitions: defs, Enablements: rows, Switch: func(chatui.ModSwitch) {}})
			modNoLeaks(t, locale+" channel", markup)
			if want := map[string]string{"ar": "rtl"}[locale]; want != "" && !strings.Contains(markup, `dir="rtl"`) || want == "" && !strings.Contains(markup, `dir="ltr"`) {
				t.Errorf("%s: direction", locale)
			}
			for _, text := range []string{w.builtin, w.own, w.admin, w.what, w.reset} {
				if !strings.Contains(markup, text) {
					t.Errorf("%s: missing %q", locale, text)
				}
			}

			// A real switch per list, with the state this channel has.
			switches := modSwitches(markup)
			wantChecked := map[string]string{
				"builtin-en-profanity": "true", "builtin-en-slurs": "false", "builtin-en-harassment": "false",
				"builtin-de-profanity": "true", "builtin-de-slurs": "true", "builtin-de-harassment": "false",
				"builtin-ar-profanity": "false", "builtin-ar-slurs": "false", "builtin-ar-harassment": "false",
				"own-1": "true",
			}
			if len(switches) != len(wantChecked) {
				t.Errorf("%s: %d switches, want %d: %v", locale, len(switches), len(wantChecked), switches)
			}
			for id, want := range wantChecked {
				if switches[id] != want {
					t.Errorf("%s: switch %s is %q, want %q", locale, id, switches[id], want)
				}
			}
			if _, ok := switches["wide-1"]; ok {
				t.Errorf("%s: a channel manager is offered a switch for a workspace filter", locale)
			}
			if strings.Count(markup, `role="switch"`) != len(wantChecked) {
				t.Errorf("%s: a control that is not a switch carries the role", locale)
			}

			// The status beside each switch says whose setting it is.
			for text, want := range map[string]int{w.wsOn: 1, w.chanOnly: 1, w.chanOwn: 1, w.chanOff: 1} {
				if got := strings.Count(markup, text); got != want {
					t.Errorf("%s: %q appears %d times, want %d", locale, text, got, want)
				}
			}
			// What happens: a select only for a list that is on, showing its outcome.
			if got := strings.Count(markup, `data-action="modadmin-action"`); got != 3 {
				t.Errorf("%s: %d outcome selects, want 3", locale, got)
			}
			for id, selected := range map[string]string{"builtin-en-profanity": "block", "builtin-de-profanity": "mask", "builtin-de-slurs": "flag"} {
				block := regexp.MustCompile(`(?s)<select[^>]*id="modadmin-act-` + id + `".*?</select>`).FindString(markup)
				if !strings.Contains(block, `selected value="`+selected+`"`) || strings.Count(block, "<option") != 3 || strings.Contains(block, "notify") {
					t.Errorf("%s: outcome select of %s: %s", locale, id, block)
				}
			}
			if strings.Contains(markup, `id="modadmin-act-builtin-en-slurs"`) {
				t.Errorf("%s: an outcome select for a list that is off", locale)
			}
			// Use the workspace setting: only where the channel differs from it.
			if got := strings.Count(markup, `data-action="modadmin-reset"`); got != 3 {
				t.Errorf("%s: %d reset links, want 3", locale, got)
			}
			if strings.Contains(markup, `data-action="modadmin-reset" data-id="builtin-en-profanity"`) {
				t.Errorf("%s: reset offered where the channel follows the workspace", locale)
			}

			// Grouped per language, the viewer's own first; each switch is named by
			// its list and its language.
			lang := map[string]int{}
			for _, name := range w.languages {
				lang[name] = strings.Index(markup, ">"+name+"<")
				if lang[name] < 0 {
					t.Errorf("%s: language heading %q missing", locale, name)
				}
			}
			first := map[string]string{"en-US": "English", "de-DE": "Deutsch", "ar": "العربية"}[locale]
			for name, at := range lang {
				if name != first && at < lang[first] {
					t.Errorf("%s: %s comes before the viewer's own language %s", locale, name, first)
				}
			}
			for _, name := range w.listNames {
				if strings.Count(markup, ">"+name+"<") != 3 {
					t.Errorf("%s: list %q should be named once per language", locale, name)
				}
			}
			label := regexp.MustCompile(`aria-labelledby="(modadmin-n-1) (modadmin-g-[a-z]+)"`).FindStringSubmatch(markup)
			if label == nil {
				t.Fatalf("%s: a switch is not named by its list and language", locale)
			}
			for _, id := range label[1:] {
				if !strings.Contains(markup, `id="`+id+`"`) {
					t.Errorf("%s: aria-labelledby points at %s which is not there", locale, id)
				}
			}

			// This channel's own filter, and the workspace's, read only.
			if !strings.Contains(markup, "Project names") || !strings.Contains(markup, `data-action="modadmin-edit" data-id="own-1"`) || !strings.Contains(markup, w.ownRule) {
				t.Errorf("%s: the channel's own filter row is incomplete", locale)
			}
			if !strings.Contains(markup, "Card numbers") || strings.Contains(markup, `data-action="modadmin-edit" data-id="wide-1"`) {
				t.Errorf("%s: the workspace's filter is missing or editable by a channel manager", locale)
			}
			if !strings.Contains(markup, chatui.ModAdminText(m, "a_hint")) || !strings.Contains(markup, chatui.ModAdminText(m, "dm")) {
				t.Errorf("%s: the read-only section does not explain itself", locale)
			}
			if !strings.Contains(markup, "phoenix") || !strings.Contains(markup, chatui.ModAdminText(m, "see")) {
				t.Errorf("%s: no way to see what the filter checks", locale)
			}
			visible := modVisible(markup)
			for _, id := range []string{"own-1", "wide-1", "builtin-en-profanity"} {
				if strings.Contains(visible, id) {
					t.Errorf("%s: a rule id is shown: %s", locale, id)
				}
			}

			// An administrator is sent to the workspace filters to change those.
			adminMarkup := modRender(t, chatui.ModAdminPanel(chatui.ModAdminProps{Model: modBrowserModel(locale, true), CanManage: true, Channel: "room", Definitions: defs, Enablements: rows}))
			if !strings.Contains(adminMarkup, chatui.ModAdminText(m, "a_hint_admin")) || strings.Contains(adminMarkup, chatui.ModAdminText(m, "a_hint")+"<") {
				t.Errorf("%s: an administrator is told they cannot change the workspace filters", locale)
			}

			// The workspace page: the workspace's own rows, never a channel's.
			ws := panel(chatui.ModAdminProps{Workspace: true, Definitions: defs, Enablements: rows, Switch: func(chatui.ModSwitch) {}})
			modNoLeaks(t, locale+" workspace", ws)
			wsChecked := map[string]string{
				"builtin-en-profanity": "true", "builtin-en-slurs": "true", "builtin-en-harassment": "false",
				"builtin-de-profanity": "false", "builtin-de-slurs": "true", "builtin-de-harassment": "false",
				"builtin-ar-profanity": "false", "builtin-ar-slurs": "false", "builtin-ar-harassment": "false",
				"own-1": "true", "wide-1": "true",
			}
			got := modSwitches(ws)
			for id, want := range wsChecked {
				if got[id] != want {
					t.Errorf("%s workspace: switch %s is %q, want %q", locale, id, got[id], want)
				}
			}
			if strings.Contains(ws, `data-action="modadmin-reset"`) || strings.Contains(ws, w.admin) || strings.Contains(ws, w.chanOnly) || strings.Contains(ws, w.chanOff) {
				t.Errorf("%s workspace: a channel's override leaked into the workspace page", locale)
			}

			// States: loading, a failed read that keeps what was delivered, a busy
			// write, a refused write.
			loading := panel(chatui.ModAdminProps{Channel: "room", Loading: true})
			if !strings.Contains(loading, chatui.ModAdminText(m, "loading")) || strings.Contains(loading, `role="switch"`) {
				t.Errorf("%s: loading state", locale)
			}
			failed := panel(chatui.ModAdminProps{Channel: "room", Definitions: defs, Enablements: rows, LoadError: "er_network", Retry: func() {}, Switch: func(chatui.ModSwitch) {}})
			if len(modSwitches(failed)) != len(wantChecked) || !strings.Contains(failed, chatui.ModAdminText(m, "er_network")) || !strings.Contains(failed, chatui.ModAdminText(m, "stale")) || !strings.Contains(failed, chatui.ModAdminText(m, "retry")) {
				t.Errorf("%s: a failed read blanked the controls the server delivered", locale)
			}
			firstLoad := panel(chatui.ModAdminProps{Channel: "room", LoadError: "er_unavail", Retry: func() {}})
			if !strings.Contains(firstLoad, chatui.ModAdminText(m, "load_failed")) || !strings.Contains(firstLoad, chatui.ModAdminText(m, "er_unavail")) {
				t.Errorf("%s: a failed first read says nothing", locale)
			}
			busy := panel(chatui.ModAdminProps{Channel: "room", Definitions: defs, Enablements: rows, Busy: true, Switch: func(chatui.ModSwitch) {}})
			if strings.Count(busy, ` disabled`) < len(wantChecked) {
				t.Errorf("%s: switches stay live while a write is going", locale)
			}
			refused := panel(chatui.ModAdminProps{Channel: "room", Definitions: defs, Enablements: rows, Status: "er_perm", StatusNote: "sf_switch", StatusError: true, Switch: func(chatui.ModSwitch) {}})
			sentence := chatui.ModAdminText(m, "er_perm") + " " + chatui.ModAdminText(m, "sf_switch")
			if !strings.Contains(refused, `role="alert"`) || !strings.Contains(refused, sentence) || switchesDiffer(modSwitches(refused), wantChecked) {
				t.Errorf("%s: a refused write does not say so or moved the switch: %s", locale, regexp.MustCompile(`<p class="chatmod-status"[^>]*>[^<]*`).FindString(refused))
			}
			saved := panel(chatui.ModAdminProps{Channel: "room", Definitions: defs, Enablements: rows, Status: "st_saved", Switch: func(chatui.ModSwitch) {}})
			if !strings.Contains(saved, chatui.ModAdminText(m, "st_saved")) {
				t.Errorf("%s: no saved line", locale)
			}
			denied := modRender(t, chatui.ModAdminPanel(chatui.ModAdminProps{Model: m}))
			if !strings.Contains(denied, chatui.ModAdminText(m, "no_permission")) || strings.Contains(denied, `role="switch"`) {
				t.Errorf("%s: a person who may not manage filters sees controls", locale)
			}
		})
	}
}

func switchesDiffer(got, want map[string]string) bool {
	for id, state := range want {
		if got[id] != state {
			return true
		}
	}
	return false
}

// modDetails renders the whole Chat page and returns Conversation details, with
// the administrator's panels wired in the way the client wires them.
func modDetails(t *testing.T, locale, viewer string) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, OwnerID: "owner", MemberCount: 18, Joined: true}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), ShowDetails: true, SelectedID: room.ID, CurrentUser: map[string]string{"admin": "walt", "manager": "walt", "member": "jake"}[viewer], CurrentTenantID: "t", IsTenantAdmin: viewer == "admin",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{room},
		Members:       []chatui.Member{{ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}, {ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}},
		ChannelTeam:   chatui.ChannelTeamWidget{Members: []chatui.ChannelTeamMember{{HomeTenantID: "t", SubjectID: "jake", Role: "MEMBERSHIP_ROLE_MEMBER"}, {HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}},
		ChannelStatuses: map[string]chatui.ChannelStatusView{room.ID: {
			Status:      chat.ChannelStatus{TenantID: "t", ConversationID: room.ID, Status: chatpolicy.StatusOpen, Revision: 1},
			Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusLocked, Permission: chatpolicy.PermissionChangeChannelStatus}},
		}},
		ChangeChannelStatus: func(chat.ChangeChannelStatusRequest) {},
		Callbacks:           chatui.Callbacks{SetConversationNotification: func(string, chatui.NotificationMode) {}, CopyConversationAPICurl: func(string) {}, ToggleDetails: func(bool) {}},
	}
	defs, rows := modBuiltinsAndCustoms(room.ID)
	m.FilterSettings = func() ui.Node {
		return chatui.ModAdminPanel(chatui.ModAdminProps{Model: m, CanManage: true, Channel: room.ID, Definitions: defs, Enablements: rows, Switch: func(chatui.ModSwitch) {}})
	}
	m.WorkspaceFilterSettings = func() ui.Node {
		return chatui.ModAdminPanel(chatui.ModAdminProps{Model: m, CanManage: true, Workspace: true, Definitions: defs, Enablements: rows, Switch: func(chatui.ModSwitch) {}})
	}
	m.ShowFilterSettings = true
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

func TestTodo_CHATMOD_002_Lists_Browser_Details(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := modBrowserModel(locale, true)
		admin := modDetails(t, locale, "admin")
		manager := modDetails(t, locale, "manager")
		member := modDetails(t, locale, "member")
		for who, panel := range map[string]string{"admin": admin, "manager": manager, "member": member} {
			modNoLeaks(t, locale+" details of "+who, panel)
		}
		if got := len(modSwitches(admin)); got != 10 {
			t.Errorf("%s: the details panel holds %d switches, want 10", locale, got)
		}
		button := "<span>" + chatui.ModAdminText(m, "ws_entry") + "</span>"
		if !strings.Contains(admin, button) {
			t.Errorf("%s: a workspace administrator finds no way to the workspace filters", locale)
		}
		if strings.Contains(manager, button) || strings.Contains(member, button) {
			t.Errorf("%s: only a workspace administrator is offered the workspace filters", locale)
		}
		if strings.Contains(member, `role="switch"`) || strings.Contains(member, "modadmin-") {
			t.Errorf("%s: a plain member sees filter controls", locale)
		}
	}
}
