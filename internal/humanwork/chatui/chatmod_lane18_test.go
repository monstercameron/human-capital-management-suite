package chatui

import (
	stdhtml "html"
	"regexp"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

var chatmodLocales = []string{"en-US", "de-DE", "ar"}

// chatmodHasAttr reports whether an element carries an attribute at all.
func chatmodHasAttr(n *xhtml.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// chatmodVisible is the text a reader sees: the markup without its tags.
func chatmodVisible(markup string) string {
	return regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(markup, " ")
}

// TestTodo_CHATMOD_004_Browser_Selected: the Remove dialog lists the messages
// around the one it was opened on with a tick box each, the one it was opened
// on ticked, as a form that counts before it removes.
func TestTodo_CHATMOD_004_Browser_Selected(t *testing.T) {
	at := time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC)
	long := strings.Repeat("spam and more spam ", 20)
	english := map[string]string{"choose": "Choose which messages to remove", "selected": "Selected messages", "preview": "Count messages", "none_selected": "Tick at least one message first."}
	for key, want := range english {
		if got := chatremoveText("en-US", key); got != want {
			t.Fatalf("English %q is %q, want %q", key, got, want)
		}
	}
	for _, locale := range chatmodLocales {
		model := ModerationDialogModel{Model: Model{Locale: locale}, Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{"p2"}}, Action: "remove",
			Target: ModerationTargetView{AuthorID: "jake-internal", HomeTenantID: "t", AuthorName: "Jake Sullivan", Body: "second"}, TimeZone: time.UTC,
			Choices: []ModerationChoice{{ID: "p1", AuthorName: "Jake Sullivan", Body: "first **bold**\nline two", At: at}, {ID: "p2", AuthorName: "Jake Sullivan", Body: "second", At: at.Add(time.Minute)}, {ID: "p3", AuthorName: "", Body: long, At: at.Add(2 * time.Minute)}}}
		markup := stdhtml.UnescapeString(chatremoveMarkup(t, ModerationDialog(model)))
		forms := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "form" && chatPolishAttr(n, "data-chatremove-selection") == "picked"
		})
		if len(forms) != 1 {
			t.Fatalf("%s: %d forms of ticked messages, want 1: %s", locale, len(forms), markup)
		}
		form := forms[0]
		if chatPolishAttr(form, "data-chatremove") != "preview" || chatPolishAttr(form, "data-conversation-id") != "room" || chatPolishAttr(form, "data-removal-action") != "remove" || chatPolishAttr(form, "data-post-id") != "" {
			t.Fatalf("%s: the form does not count first, or carries one message of its own: %s", locale, markup)
		}
		boxes := chatPolishNodesIn(form, func(n *xhtml.Node) bool { return n.Data == "input" && chatPolishAttr(n, "type") == "checkbox" })
		if len(boxes) != 3 {
			t.Fatalf("%s: %d tick boxes, want one for each of the 3 messages", locale, len(boxes))
		}
		for _, box := range boxes {
			id, value := chatPolishAttr(box, "id"), chatPolishAttr(box, "value")
			if chatPolishAttr(box, "name") != "post" || value == "" {
				t.Fatalf("%s: a tick box is not a message the client collects: %s", locale, markup)
			}
			checked := chatmodHasAttr(box, "checked")
			if checked != (value == "p2") {
				t.Errorf("%s: message %s ticked=%v; only the message the dialog was opened on starts ticked", locale, value, checked)
			}
			labels := chatPolishNodesIn(form, func(n *xhtml.Node) bool { return n.Data == "label" && chatPolishAttr(n, "for") == id })
			if len(labels) != 1 {
				t.Errorf("%s: tick box %s has %d labels", locale, value, len(labels))
			}
		}
		if radios := chatPolishNodesIn(form, func(n *xhtml.Node) bool { return n.Data == "input" && chatPolishAttr(n, "name") == "reason" }); len(radios) != 4 {
			t.Errorf("%s: the form has %d reasons, want the list of 4", locale, len(radios))
		}
		visible := chatmodVisible(markup)
		for _, key := range []string{"choose", "choose_help", "selected", "preview", "several"} {
			if text := chatremoveText(locale, key); text == "" || !strings.Contains(visible, text) {
				t.Errorf("%s: the dialog does not show %q (%q)", locale, key, text)
			}
		}
		// A long message is one short line, a nameless author is "Someone", and
		// nothing internal is printed.
		if strings.Contains(visible, long) || !strings.Contains(visible, "…") || !strings.Contains(visible, chatremoveText(locale, "someone")) {
			t.Errorf("%s: a long message was not shortened, or a nameless author has no name", locale)
		}
		if strings.Contains(visible, "-internal") || strings.Contains(markup, "⟦") || strings.Contains(visible, "{name}") {
			t.Errorf("%s: the dialog prints an identifier, a marker or a placeholder", locale)
		}
		if dir := map[string]string{"ar": "rtl"}[locale]; dir != "" && !strings.Contains(markup, `dir="rtl"`) {
			t.Errorf("%s: the dialog is not right-to-left", locale)
		}
		// One message is what the first form removes: no list for it.
		model.Choices = model.Choices[1:2]
		if alone := chatremoveMarkup(t, ModerationDialog(model)); strings.Contains(alone, `data-chatremove-selection`) {
			t.Errorf("%s: a list of one message is offered", locale)
		}
		// A dialog that decides a queue item, a report and a restore offer no list.
		model.Choices = []ModerationChoice{{ID: "p1"}, {ID: "p2"}}
		for name, change := range map[string]func(*ModerationDialogModel){"queue": func(m *ModerationDialogModel) { m.CaseID = "report:r" }, "report": func(m *ModerationDialogModel) { m.Report = true }, "restore": func(m *ModerationDialogModel) { m.Action = "restore" }} {
			other := model
			change(&other)
			if got := chatremoveMarkup(t, ModerationDialog(other)); strings.Contains(got, `data-chatremove-selection`) {
				t.Errorf("%s: the %s dialog offers the list", locale, name)
			}
		}
	}
	if !strings.Contains(Stylesheet, ".chatremove-pick label{") || !strings.Contains(Stylesheet, ".chatremove .chatremove-picks{") {
		t.Fatal("the list of messages to tick has no styles in the page's stylesheet")
	}
}

// TestTodo_CHATMOD_004_Browser_Restored: a message an administrator put back
// while the page was open says so under its text, once, as a status.
func TestTodo_CHATMOD_004_Browser_Restored(t *testing.T) {
	if got := chatremoveText("en-US", "restored_notice"); got != "An administrator restored this message." {
		t.Fatalf("English line is %q", got)
	}
	msg := Message{ID: "m1", AuthorID: "jake", Author: "Jake Sullivan", Body: "back again", SentAt: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)}
	for _, locale := range chatmodLocales {
		m := Model{State: StateReady, Locale: locale, SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "t"}
		plain := stdhtml.UnescapeString(chatremoveMarkup(t, message(m, handlers{}, msg, false)))
		line := chatremoveText(locale, "restored_notice")
		if strings.Contains(plain, line) || strings.Contains(plain, "chatremove-restored") {
			t.Fatalf("%s: a message nobody restored says it was restored", locale)
		}
		m.Moderation = m.Moderation.WithRestored("m1")
		restored := stdhtml.UnescapeString(chatremoveMarkup(t, message(m, handlers{}, msg, false)))
		rows := chatPolishNodes(t, restored, func(n *xhtml.Node) bool { return n.Data == "article" })
		if len(rows) != 1 || !chatPolishHasClass(rows[0], "message") {
			t.Fatalf("%s: the restored message is not one message row: %s", locale, restored)
		}
		lines := chatPolishNodesIn(rows[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chatremove-restored") })
		if len(lines) != 1 || lines[0].Parent != rows[0] || chatPolishAttr(lines[0], "role") != "status" || !strings.Contains(restored, line) {
			t.Fatalf("%s: the row does not end with one status line %q: %s", locale, line, restored)
		}
		if !strings.Contains(restored, "back again") || strings.Count(restored, line) != 1 {
			t.Fatalf("%s: the message's own text is gone, or the line is written twice", locale)
		}
		// Another message of the same list is untouched.
		other := msg
		other.ID = "m2"
		if got := chatremoveMarkup(t, message(m, handlers{}, other, false)); strings.Contains(got, "chatremove-restored") {
			t.Fatalf("%s: the line is on a message that was not restored", locale)
		}
	}
	// The state is a value: marking one copy does not mark another, and a
	// message removed again is forgotten.
	base := ModerationState{}
	marked := base.WithRestored("m1").WithRestored("m2")
	if base.Restored["m1"] || !marked.Restored["m1"] || !marked.Restored["m2"] {
		t.Fatalf("marking changed the wrong copy: base=%v marked=%v", base.Restored, marked.Restored)
	}
	again := marked.WithoutRestored("m1")
	if again.Restored["m1"] || !again.Restored["m2"] || !marked.Restored["m1"] {
		t.Fatalf("forgetting changed the wrong copy: %v %v", again.Restored, marked.Restored)
	}
	if !strings.Contains(Stylesheet, ".chat-workspace .message>.chatremove-restored{grid-column:2;") {
		t.Fatal("the restored line is not laid out in the text's column")
	}
}

// TestTodo_CHATMOD_003_Browser_Hits: the filter panel lists what the filters
// caught and sums up each dry run, in words, without an identifier and without
// any text of a message.
func TestTodo_CHATMOD_003_Browser_Hits(t *testing.T) {
	at := time.Date(2026, 10, 1, 14, 5, 0, 0, time.UTC)
	hit := func(id int64, rule, name, action, channel, subject string, dry bool, target string) chatfilter.Record {
		return chatfilter.Record{ID: id, Tenant: "t", Channel: channel, Subject: subject, At: at.Add(time.Duration(id) * time.Minute),
			Hit: chatfilter.Hit{RuleID: rule, RuleName: name, Version: "1.2.0", Action: action, Digest: "sha256:abcdef", Masked: "[removed word]", DryRun: dry, Target: target}}
	}
	records := []chatfilter.Record{
		hit(1, "r-codes", "Client names", "block", "room", "jake-internal", false, ""),
		hit(2, "r-cards", "Card numbers", "mask", "room", "jake-internal", true, ""),
		hit(3, "r-cards", "Card numbers", "mask", "ops-private", "", true, ""),
		hit(4, "r-tell", "Leaks", "notify", "", "walt-internal", false, "#security"),
		hit(5, "r-flag", "Slang", "flag", "room", "walt-internal", true, ""),
	}
	if got := ModHitsShown(records, ModHitsAll); len(got) != 5 || got[0].ID != 5 || got[4].ID != 1 {
		t.Fatalf("all matches, newest first: %+v", got)
	}
	if got := ModHitsShown(records, ModHitsActed); len(got) != 2 || got[0].ID != 4 || got[1].ID != 1 {
		t.Fatalf("acted on: %+v", got)
	}
	if got := ModHitsShown(records, ModHitsDry); len(got) != 3 {
		t.Fatalf("dry run only: %+v", got)
	}
	dry := ModDrySummary(records)
	if len(dry) != 2 || dry[0].RuleID != "r-cards" || dry[0].Count != 2 || dry[1].RuleID != "r-flag" || dry[1].Count != 1 {
		t.Fatalf("dry-run summary: %+v", dry)
	}
	english := map[string]string{"hits_title": "What the filters caught", "hits_show": "Show matches", "hits_dry_title": "Dry-run results", "h_block": "Not sent", "h_dry_mask": "The word would have been hidden", "hits_private": "A conversation you are not in"}
	for key, want := range english {
		if got := modadminText(Model{Locale: "en-US"}, key); got != want {
			t.Fatalf("English %q is %q, want %q", key, got, want)
		}
	}
	for _, locale := range chatmodLocales {
		m := Model{State: StateReady, Locale: locale, SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "t", IsTenantAdmin: true,
			Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel}}}
		text := func(key string) string { return modadminText(m, key) }
		render := func(p ModHitsProps) string {
			p.Model, p.Now = m, at.Add(24*time.Hour)
			return stdhtml.UnescapeString(chatremoveMarkup(t, ModHitsPanel(p)))
		}
		// Before anything was read: one button, and no table.
		closed := render(ModHitsProps{Load: func(bool) {}})
		if !strings.Contains(closed, text("hits_title")) || !strings.Contains(closed, text("hits_show")) || strings.Contains(closed, "<table") {
			t.Fatalf("%s: the closed section: %s", locale, closed)
		}
		if busy := render(ModHitsProps{Loading: true, Load: func(bool) {}}); !strings.Contains(busy, text("hits_loading")) || !strings.Contains(busy, `aria-busy="true"`) {
			t.Fatalf("%s: a read in flight is not said: %s", locale, busy)
		}
		// A read that failed says so and offers the read again.
		if failed := render(ModHitsProps{Error: "er_network", Load: func(bool) {}}); !strings.Contains(failed, `role="alert"`) || !strings.Contains(failed, text("er_network")) || !strings.Contains(failed, text("retry")) {
			t.Fatalf("%s: a failed read: %s", locale, failed)
		}
		open := render(ModHitsProps{Records: records, Loaded: true, Older: true, Load: func(bool) {}})
		visible := chatmodVisible(open)
		rows := chatPolishNodes(t, open, func(n *xhtml.Node) bool { return n.Data == "tr" && n.Parent != nil && n.Parent.Data == "tbody" })
		if len(rows) != 5 {
			t.Fatalf("%s: %d rows for 5 matches: %s", locale, len(rows), open)
		}
		if heads := chatPolishNodes(t, open, func(n *xhtml.Node) bool { return n.Data == "th" && chatPolishAttr(n, "scope") == "col" }); len(heads) != 4 {
			t.Errorf("%s: %d column headings, want When, Filter, What happened, Where", locale, len(heads))
		}
		for _, want := range []string{text("hits_dry_title"), "Client names · v1.2.0", text("h_block"), text("h_dry_mask"), text("h_dry_flag"), "#general", text("hits_private"), text("hits_outside"),
			modadminFormat(text("h_notify"), "target", "#security"), text("hits_older"), text("hits_again"), modadminFormat(text("hits_count"), "n", chatNumeral(locale, "5"))} {
			if want == "" || !strings.Contains(visible, want) {
				t.Errorf("%s: the open section does not show %q", locale, want)
			}
		}
		// After a failed second read the matches already shown stay.
		if kept := render(ModHitsProps{Records: records, Loaded: true, Error: "er_unavail", Load: func(bool) {}}); !strings.Contains(kept, "Client names") || !strings.Contains(kept, text("er_unavail")) {
			t.Errorf("%s: a failed read blanked the matches", locale)
		}
		for _, banned := range []string{"-internal", "ops-private", "sha256", "[removed word]", "r-codes", "{n}", "{target}", "{what}", "⟦"} {
			if strings.Contains(visible, banned) {
				t.Errorf("%s: the section prints %q", locale, banned)
			}
		}
		if empty := render(ModHitsProps{Loaded: true, Load: func(bool) {}}); !strings.Contains(empty, text("hits_none")) || strings.Contains(empty, "<table") {
			t.Errorf("%s: an empty list: %s", locale, empty)
		}
		// The panel draws the section under the filters, and not without a reader.
		defs := []chatfilter.Definition{{ID: "r-codes", Name: "Client names", Version: "1.2.0", Kind: "words", Action: "block", Match: []string{"quartz"}, Channels: []string{"room"}, Authority: chatfilter.AuthorityChannel}}
		with := stdhtml.UnescapeString(chatremoveMarkup(t, ModAdminPanel(ModAdminProps{Model: m, Channel: "room", CanManage: true, Definitions: defs, Hits: &ModHitsProps{Load: func(bool) {}}})))
		without := stdhtml.UnescapeString(chatremoveMarkup(t, ModAdminPanel(ModAdminProps{Model: m, Channel: "room", CanManage: true, Definitions: defs})))
		if !strings.Contains(with, text("hits_title")) || strings.Contains(without, text("hits_title")) {
			t.Errorf("%s: the panel does not place the section", locale)
		}
	}
	if !strings.Contains(Stylesheet, ".chatmod-hits-table{") {
		t.Fatal("the matches table has no styles in the page's stylesheet")
	}
}

// TestTodo_CHATMOD_003_Editor_WorkspaceOwned: a channel's manager sees a filter
// an administrator wrote for the channel as one they cannot change; the notify
// action says who is told; an unknown channel has its own sentence.
func TestTodo_CHATMOD_003_Editor_WorkspaceOwned(t *testing.T) {
	defs := []chatfilter.Definition{
		{ID: "theirs", Name: "Client names", Version: "1.0.0", Kind: "words", Action: "block", Match: []string{"quartz"}, Channels: []string{"room"}, Authority: chatfilter.AuthorityWorkspace},
		{ID: "legacy", Name: "Old filter", Version: "1.0.0", Kind: "words", Action: "block", Match: []string{"opal"}, Channels: []string{"room"}},
		{ID: "mine", Name: "Project names", Version: "1.0.0", Kind: "words", Action: "mask", Match: []string{"garnet"}, Channels: []string{"room"}, Authority: chatfilter.AuthorityChannel},
		{ID: "wide", Name: "Card numbers", Version: "1.0.0", Kind: "detector", Action: "block", Match: []string{"card"}},
	}
	ids := func(list []chatfilter.Definition) string {
		var out []string
		for _, d := range list {
			out = append(out, d.ID)
		}
		return strings.Join(out, ",")
	}
	manager := ModGroupFor(defs, "room", false, "en-US", false)
	if ids(manager.Own) != "mine" {
		t.Fatalf("a manager's own filters: %s", ids(manager.Own))
	}
	if len(manager.Admin) != 3 {
		t.Fatalf("a manager's read-only filters: %s, want the workspace's and the two an administrator owns", ids(manager.Admin))
	}
	administrator := ModGroupFor(defs, "room", false, "en-US", true)
	if len(administrator.Own) != 3 || ids(administrator.Admin) != "wide" {
		t.Fatalf("an administrator's sections: own=%s admin=%s", ids(administrator.Own), ids(administrator.Admin))
	}
	// On the page: no switch and no Edit for the administrator's filter.
	m := Model{State: StateReady, Locale: "en-US", SelectedID: "room", CurrentUser: "manager", CurrentTenantID: "t", Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel}}}
	page := stdhtml.UnescapeString(chatremoveMarkup(t, ModAdminPanel(ModAdminProps{Model: m, Channel: "room", CanManage: true, Definitions: defs, Switch: func(ModSwitch) {}})))
	for id, changeable := range map[string]bool{"theirs": false, "legacy": false, "mine": true, "wide": false} {
		if got := strings.Contains(page, `data-action="modadmin-edit" data-id="`+id+`"`) || strings.Contains(page, `data-id="`+id+`" data-action="modadmin-edit"`); got != changeable {
			t.Errorf("filter %s: an Edit control is offered=%v, want %v", id, got, changeable)
		}
	}
	if ModErrorKey("unknown_target") != "er_target" || !strings.HasPrefix(modadminText(m, "er_target"), "No channel with a manager has that name.") {
		t.Fatalf("an unknown channel is not said in its own words: %q", modadminText(m, ModErrorKey("unknown_target")))
	}
	for _, locale := range chatmodLocales {
		for _, key := range []string{"act_notify", "ah_notify", "e_target", "e_target_ph", "o_notify", "v_target", "er_target"} {
			text := modadminText(Model{Locale: locale}, key)
			if text == "" || locale == "en-US" && strings.Contains(strings.ToLower(text), "agent") {
				t.Errorf("%s %q: %q; the action tells a channel's managers and no agent", locale, key, text)
			}
		}
	}
	if got := modadminText(Model{Locale: "en-US"}, "act_notify"); got != "Tell a channel's managers" {
		t.Fatalf("the action is named %q", got)
	}
}

// TestTodo_CHATMOD_005_Browser_Permissions: the Permissions tab is a table of
// roles and the four permissions; each cell is a switch that says what the role
// may do and where the answer comes from, and carries the assignment it makes.
func TestTodo_CHATMOD_005_Browser_Permissions(t *testing.T) {
	rows := []chat.ModerationPermissionRow{
		{Role: "MANAGER", Permission: chat.PermissionManageFilters, Allowed: false},
		{Role: "hr_partner", Permission: chat.PermissionReviewRemovedMessages, Allowed: true},
		{ConversationID: "room", Role: "MANAGER", Permission: chat.PermissionManageFilters, Allowed: true},
		{ConversationID: "room", Role: "MEMBER", Permission: chat.PermissionReport, Allowed: false},
	}
	workspace := ModerationPermissionsModel{Rows: rows, Channels: []ModerationChannelChoice{{ID: "room", Name: "general"}, {ID: "ops", Name: "operations"}}}
	channel := workspace
	channel.ConversationID, channel.ConversationName = "room", "general"
	for _, tc := range []struct {
		model            ModerationPermissionsModel
		role, permission string
		want             ModerationPermissionCell
	}{
		{workspace, "WORKSPACE_ADMIN", chat.PermissionManageFilters, ModerationPermissionCell{true, "default"}},
		{workspace, "MANAGER", chat.PermissionRemoveMessages, ModerationPermissionCell{true, "default"}},
		{workspace, "MANAGER", chat.PermissionReviewRemovedMessages, ModerationPermissionCell{false, "default"}},
		{workspace, "MANAGER", chat.PermissionManageFilters, ModerationPermissionCell{false, "workspace"}},
		{workspace, "MEMBER", chat.PermissionReport, ModerationPermissionCell{true, "default"}},
		{workspace, "MEMBER", chat.PermissionRemoveMessages, ModerationPermissionCell{false, "default"}},
		{workspace, "hr_partner", chat.PermissionReviewRemovedMessages, ModerationPermissionCell{true, "workspace"}},
		{workspace, "hr_partner", chat.PermissionReport, ModerationPermissionCell{false, "default"}},
		{channel, "MANAGER", chat.PermissionManageFilters, ModerationPermissionCell{true, "own"}},
		{channel, "MEMBER", chat.PermissionReport, ModerationPermissionCell{false, "own"}},
		{channel, "hr_partner", chat.PermissionReviewRemovedMessages, ModerationPermissionCell{true, "workspace"}},
	} {
		if got := ModerationPermissionOf(tc.model, tc.role, tc.permission); got != tc.want {
			t.Errorf("%s / %s in %q: %+v, want %+v", tc.role, tc.permission, tc.model.ConversationID, got, tc.want)
		}
	}
	extra := workspace
	extra.ExtraRole = " payroll_admin "
	if got := strings.Join(ModerationPermissionRoles(extra), ","); got != "WORKSPACE_ADMIN,MANAGER,MEMBER,hr_partner,payroll_admin" {
		t.Fatalf("roles: %s", got)
	}
	if got := chatremoveText("en-US", "tab_permissions"); got != "Permissions" {
		t.Fatalf("English tab is %q", got)
	}
	for _, locale := range chatmodLocales {
		page := func(m ModerationPageModel) string {
			m.Locale, m.State = locale, StateReady
			return stdhtml.UnescapeString(chatremoveMarkup(t, ModerationPage(m)))
		}
		text := func(key string) string { return chatremoveText(locale, key) }
		// The tab is offered to an administrator only, on every tab.
		if queue := page(ModerationPageModel{Tab: "open", CanAssign: true}); !strings.Contains(queue, text("tab_permissions")) || !strings.Contains(queue, "tab=permissions") {
			t.Fatalf("%s: an administrator's queue does not offer the tab", locale)
		}
		if queue := page(ModerationPageModel{Tab: "open"}); strings.Contains(queue, "tab=permissions") {
			t.Fatalf("%s: a moderator who is not an administrator is offered the tab", locale)
		}
		markup := page(ModerationPageModel{Tab: "permissions", CanAssign: true, Permissions: &workspace})
		tabs := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "tab" })
		if len(tabs) != 3 || chatPolishAttr(tabs[2], "aria-selected") != "true" || chatPolishAttr(tabs[0], "aria-selected") != "false" {
			t.Fatalf("%s: three tabs with Permissions chosen are not drawn: %s", locale, markup)
		}
		switches := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishAttr(n, "role") == "switch" })
		if len(switches) != 16 {
			t.Fatalf("%s: %d switches, want 4 roles by 4 permissions", locale, len(switches))
		}
		found := false
		for _, s := range switches {
			role, permission := chatPolishAttr(s, "data-role"), chatPolishAttr(s, "data-permission")
			cell := ModerationPermissionOf(workspace, role, permission)
			if chatPolishAttr(s, "data-chatremove-act") != "permission" || !chat.ValidModerationPermission(permission) || role == "" {
				t.Fatalf("%s: a switch does not carry an assignment: role=%q permission=%q", locale, role, permission)
			}
			if chatPolishAttr(s, "aria-checked") != boolString(cell.Allowed) || chatPolishAttr(s, "data-allowed") != boolString(!cell.Allowed) || chatPolishAttr(s, "data-conversation") != "" {
				t.Errorf("%s: %s / %s shows %s and would set %s", locale, role, permission, chatPolishAttr(s, "aria-checked"), chatPolishAttr(s, "data-allowed"))
			}
			if label := chatPolishAttr(s, "aria-label"); !strings.Contains(label, text(moderationPermissionKeys[permission])) || !strings.Contains(label, moderationRoleName(locale, role)) {
				t.Errorf("%s: the switch is named %q, not by its permission and role", locale, label)
			}
			found = found || role == "hr_partner"
		}
		if !found {
			t.Errorf("%s: a role with a stored answer has no row", locale)
		}
		visible := chatmodVisible(markup)
		for _, key := range []string{"perm_intro", "perm_scope_ws", "perm_choose", "perm_role", "perm_report", "perm_remove", "perm_review", "perm_filters", "role_WORKSPACE_ADMIN", "role_MANAGER", "role_MEMBER", "perm_yes", "perm_no", "perm_src_default", "perm_src_workspace", "perm_note", "perm_other", "perm_other_hint", "perm_other_add"} {
			if want := text(key); want == "" || !strings.Contains(visible, want) {
				t.Errorf("%s: the tab does not show %q (%q)", locale, key, want)
			}
		}
		for _, banned := range []string{"WORKSPACE_ADMIN", "MANAGER", "MEMBER", "{name}", "{role}", "{permission}", "⟦"} {
			if strings.Contains(visible, banned) {
				t.Errorf("%s: the tab prints %q", locale, banned)
			}
		}
		if !strings.Contains(markup, `data-chatremove-open="`+moderationPermissionsHref(locale, "room", "")+`"`) {
			t.Errorf("%s: a channel cannot be chosen", locale)
		}
		// One channel: its own answers are marked, and the way back is offered.
		own := page(ModerationPageModel{Tab: "permissions", CanAssign: true, Permissions: &channel})
		ownVisible := chatmodVisible(own)
		if !strings.Contains(ownVisible, strings.ReplaceAll(text("perm_scope_ch"), "{name}", "general")) || !strings.Contains(ownVisible, text("perm_src_own")) || !strings.Contains(ownVisible, text("perm_back_ws")) {
			t.Errorf("%s: the channel's table does not say whose answers it shows", locale)
		}
		for _, s := range chatPolishNodes(t, own, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishAttr(n, "role") == "switch" }) {
			if chatPolishAttr(s, "data-conversation") != "room" {
				t.Fatalf("%s: a switch of the channel's table would write the workspace's answer", locale)
			}
		}
		if dir := map[string]string{"ar": "rtl"}[locale]; dir != "" && !strings.Contains(markup, `dir="rtl"`) {
			t.Errorf("%s: the tab is not right-to-left", locale)
		}
	}
	if !strings.Contains(Stylesheet, ".chatmod005-perm-table{") {
		t.Fatal("the permissions table has no styles in the page's stylesheet")
	}
}

// TestTodo_CHATMOD_005_Accessibility_Permissions: the table is a table with
// column and row headings; every switch has a name and a size a finger can
// press; the role box has a label and its hint.
func TestTodo_CHATMOD_005_Accessibility_Permissions(t *testing.T) {
	model := ModerationPermissionsModel{Channels: []ModerationChannelChoice{{ID: "room", Name: "general"}}}
	markup := stdhtml.UnescapeString(chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: "en-US", State: StateReady, Tab: "permissions", CanAssign: true, Permissions: &model})))
	if cols := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "th" && chatPolishAttr(n, "scope") == "col" }); len(cols) != 5 {
		t.Fatalf("%d column headings, want Role and the four permissions", len(cols))
	}
	if heads := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "th" && chatPolishAttr(n, "scope") == "row" }); len(heads) != 3 {
		t.Fatalf("%d row headings, want one for each role", len(heads))
	}
	for _, s := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishAttr(n, "role") == "switch" }) {
		if chatPolishAttr(s, "aria-label") == "" || chatPolishAttr(s, "type") != "button" {
			t.Fatalf("a switch has no name or would submit a form")
		}
		if !chatmodHasAttr(s, "aria-checked") {
			t.Fatal("a switch does not say whether it is on")
		}
	}
	inputs := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "input" && chatPolishAttr(n, "name") == "role" })
	if len(inputs) != 1 || chatPolishAttr(inputs[0], "aria-describedby") != "chatremove-role-hint" || !strings.Contains(markup, `for="chatremove-role"`) || !strings.Contains(markup, `id="chatremove-role-hint"`) {
		t.Fatalf("the role box has no label or hint: %s", markup)
	}
	if regions := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chatmod005-perm-scroll") }); len(regions) != 1 || chatPolishAttr(regions[0], "tabindex") != "0" || chatPolishAttr(regions[0], "aria-labelledby") != "chatmod005-perm-scope" {
		t.Fatal("the table's scrolling region cannot be reached by keyboard or has no name")
	}
	if alerts := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chatmod005-perm-error") }); len(alerts) != 1 || chatPolishAttr(alerts[0], "role") != "alert" {
		t.Fatal("a refused change has nowhere to be said")
	}
	for _, want := range []string{".chatremove .chatmod005-perm-switch{min-block-size:44px;min-inline-size:44px", ".chatmod005-perm-scroll:focus-visible{outline:"} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("the stylesheet misses %q", want)
		}
	}
}

// TestTodo_CHATMOD_003_Browser_FilterNotice: the notice a "notify" filter sends
// names the filter and links the conversation, and prints nothing else.
func TestTodo_CHATMOD_003_Browser_FilterNotice(t *testing.T) {
	notice := chat.ModerationNotice{TenantID: "t", ID: "filter:r:abc:1", ConversationID: "room-internal", Reason: "Client names", Outcome: chat.ModerationOutcomeFilterNotify, At: time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)}
	if got := chatremoveText("en-US", "filter_notice"); got != "A filter matched a message" {
		t.Fatalf("English title is %q", got)
	}
	for _, locale := range chatmodLocales {
		markup := stdhtml.UnescapeString(chatremoveMarkup(t, ModerationAuthorNotice(locale, notice)))
		visible := chatmodVisible(markup)
		for _, want := range []string{chatremoveText(locale, "filter_notice"), strings.ReplaceAll(chatremoveText(locale, "filter_notice_rule"), "{name}", "Client names"), chatremoveText(locale, "filter_notice_open"), "2026-10-02"} {
			if want == "" || !strings.Contains(visible, want) {
				t.Errorf("%s: the notice does not show %q: %s", locale, want, visible)
			}
		}
		if strings.Contains(visible, "room-internal") || strings.Contains(visible, "filter_notify") || strings.Contains(visible, "{name}") || strings.Contains(visible, chatremoveText(locale, "appeal")) {
			t.Errorf("%s: the notice prints an identifier, a placeholder or an appeal: %s", locale, visible)
		}
		if !strings.Contains(markup, `href="`+ChannelReferenceURL("room-internal")+`"`) {
			t.Errorf("%s: the notice does not lead to the conversation: %s", locale, markup)
		}
	}
	// A filter's notice is not a removal: it is not counted as the viewer's own.
	state := ModerationStateFromSummary(chat.ModerationSummary{Notices: []chat.ModerationNotice{notice}, NoticeCount: 1})
	if len(state.Notices) != 0 || state.Attention() != 1 || !state.Visible() {
		t.Fatalf("state from a filter notice: %+v", state)
	}
}
