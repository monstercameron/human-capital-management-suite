package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// TestTodo_CHATUX_041: Conversation details, round three. The role count is the
// labels that are set; the pin buttons say what they pin; the shipped skills read
// as plain sentences in three languages; the joining row says where the gate
// stands; a form's Save waits for a change and a destructive status asks for a
// ticked box; the channel's word lists show the workspace's setting read only
// beside the channel's own switch.
func TestTodo_CHATUX_041(t *testing.T) {
	m := chatux021Model()
	m.Members = append(m.Members, Member{ID: "ana", HomeTenantID: "t", Name: "Ana Flores"})
	m.ChannelTeam.Members = []ChannelTeamMember{
		{HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER", RoleLabel: "Lead"},
		{HomeTenantID: "t", SubjectID: "loretta", Role: "MEMBERSHIP_ROLE_MEMBER"},
		{HomeTenantID: "t", SubjectID: "ana", Role: "MEMBERSHIP_ROLE_MEMBER"},
	}
	openManage := func(extra ...string) handlers {
		groups := map[string]bool{"manage": true}
		for _, key := range extra {
			groups[key] = true
		}
		return handlers{local: localUI{detailGroups: groups}}
	}
	page := chatux021Details(t, m, openManage())

	// Role labels counts labels, not people: one of three is set.
	roles := regexp.MustCompile(`(?s)id="chat-manage-roles-row".*?</button>`).FindString(page)
	if !strings.Contains(roles, "1 set") || strings.Contains(roles, ">3<") {
		t.Errorf("the Role labels row does not count the labels that are set: %s", roles)
	}
	m.ChannelTeam.Members[0].RoleLabel = ""
	if none := regexp.MustCompile(`(?s)id="chat-manage-roles-row".*?</button>`).FindString(chatux021Details(t, m, openManage())); !strings.Contains(none, "None set") {
		t.Errorf("no labels set does not say so: %s", none)
	}

	// The pin icons say what they pin, in words a person uses.
	pin := regexp.MustCompile(`<button[^>]*data-action="team-pin"[^>]*>`).FindString(page)
	if !strings.Contains(pin, `aria-label="Show the purpose and team above the messages"`) || !strings.Contains(pin, `title="Show the purpose and team above the messages"`) || strings.Contains(pin, "widget") {
		t.Errorf("the pin button is not labelled in plain words: %s", pin)
	}

	// The joining row stands for the gate: a read list with no gate means anyone
	// can join; a gate says how many questions and who decides; no list says nothing.
	gate := func(joins map[string]GateJoin) string {
		g := chatux021Model()
		g.GateJoins = joins
		return regexp.MustCompile(`(?s)<a[^>]*data-gate-open.*?</a>`).FindString(chatux021Details(t, g, handlers{}))
	}
	if got := gate(map[string]GateJoin{}); !strings.Contains(got, ">Joining questions<") || !strings.Contains(got, "Anyone can join") {
		t.Errorf("an ungated channel does not say anyone can join: %s", got)
	}
	if got := gate(map[string]GateJoin{"general": {Questions: 3, Mode: "review"}}); !strings.Contains(got, "3 questions · Administrator review") {
		t.Errorf("a gated channel does not say where its gate stands: %s", got)
	}
	if got := gate(nil); strings.Contains(got, "manage-sec-value") {
		t.Errorf("a gate nobody read is described anyway: %s", got)
	}

	// Skills are sentences: the three shipped descriptions, in each language, with
	// the tier in brackets; an unknown skill keeps its first sentence.
	cases := map[string]string{"en-US": "Searches policy documents you can open (read only)", "de-DE": "Durchsucht Richtliniendokumente, die Sie öffnen können (", "ar": "يبحث في مستندات السياسات التي يمكنك فتحها ("}
	for locale, want := range cases {
		rows := personaSkillRows(locale, []PersonaMentionSkill{{Name: "Search deployed tenant policy documents the requesting user and this installation may read. Returns exact document-version citations.", Tier: "T0"}})
		if len(rows) != 1 || !strings.HasPrefix(rows[0], want) {
			t.Errorf("%s: the policy skill reads %q, want it to start %q", locale, rows, want)
		}
	}
	rows := personaSkillRows("en-US", []PersonaMentionSkill{
		{Name: "Prepare a bounded plain-text persona reply for the current private chat. Delivery requires a fresh private-chat authorization.", Tier: "T0"},
		{Name: "Search workspace documents by meaning. Only documents every workspace member may read are returned.", Tier: "T0"},
		{Name: "Something new. Not in the table.", Tier: ""}})
	if strings.Join(rows, "|") != "Writes the agent's reply in a private chat (read only)|Searches documents every workspace member can read (read only)|Something new." {
		t.Errorf("the skills read %q", rows)
	}

	// Save waits for a change: the purpose form, the project form and the milestone adder.
	purposeKey := chatux041DirtyKey(m, "purpose")
	purposeSave := regexp.MustCompile(`(?s)id="channel-team-purpose".*?>Save<`)
	if form := purposeSave.FindString(chatux021Details(t, m, handlers{local: localUI{purposeEditing: true, purposeDraft: "Policy questions"}})); !strings.Contains(form, "disabled") {
		t.Error("the purpose Save is live on an untouched form")
	}
	if form := purposeSave.FindString(chatux021Details(t, m, handlers{local: localUI{purposeEditing: true, purposeDraft: "Policy questions", detailGroups: map[string]bool{purposeKey: true}}})); form == "" || strings.Contains(form[strings.LastIndex(form, "<button"):], "disabled") {
		t.Error("the purpose Save stays disabled after a change")
	}
	m.Callbacks.SetChannelProjectDetails = func(string, string) {}
	project := chatux021Details(t, m, openManage("s:manage|general:project"))
	if !regexp.MustCompile(`(?s)id="channel-project-title".*?<button[^>]*disabled[^>]*>Save<`).MatchString(project) {
		t.Error("the project's Save is live on an untouched form")
	}
	dirty := chatux021Details(t, m, openManage("s:manage|general:project", chatux041DirtyKey(m, "project")))
	if regexp.MustCompile(`(?s)id="channel-project-title".*?<button[^>]*disabled[^>]*>Save<`).MatchString(dirty) {
		t.Error("the project's Save stays disabled after a change")
	}

	// A destructive status needs a ticked box before Confirm is live.
	view := m.ChannelStatuses["general"]
	props := ChannelStatusPanelProps{Model: m, View: view, Change: m.ChangeChannelStatus, Dirty: map[string]bool{}}
	confirm := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`)
	if got := confirm.FindString(renderNode(t, chatux027StatusActions(m, props, "ARCHIVED"))); !strings.Contains(got, "disabled") {
		t.Errorf("Archived can be confirmed without the box: %s", got)
	}
	archived := renderNode(t, chatux027StatusActions(m, props, "ARCHIVED"))
	if !strings.Contains(archived, "I understand the channel becomes read-only and leaves the channel list.") {
		t.Errorf("the destructive status asks for no confirmation: %s", archived)
	}
	props.Dirty = map[string]bool{"ack:status:" + "1" + ":ARCHIVED": true}
	if got := confirm.FindString(renderNode(t, chatux027StatusActions(m, props, "ARCHIVED"))); strings.Contains(got, "disabled") {
		t.Errorf("a ticked box does not enable Confirm: %s", got)
	}
	if got := confirm.FindString(renderNode(t, chatux027StatusActions(m, ChannelStatusPanelProps{Model: m, View: view, Change: m.ChangeChannelStatus}, "ANNOUNCEMENTS"))); strings.Contains(got, "disabled") || strings.Contains(renderNode(t, chatux027StatusActions(m, props, "ANNOUNCEMENTS")), "chatstate-ack") {
		t.Errorf("a status that hides nothing asks for a box: %s", got)
	}

	// A channel's word lists: the workspace's setting as read-only text, then the
	// channel's own switch, under two column names.
	list := chatfilter.Definition{ID: "builtin-en-profanity", Name: "profanity", Language: "en", Kind: "words", Action: "block", Product: true}
	grid := renderNode(t, ModAdminPanel(ModAdminProps{Model: m, CanManage: true, Channel: "general", Definitions: []chatfilter.Definition{list},
		Enablements: []chatfilter.Enablement{{RuleID: list.ID, Enabled: true, Action: "block"}, {RuleID: list.ID, Channel: "general", Enabled: false, Action: "block"}}, Switch: func(ModSwitch) {}}))
	if !strings.Contains(grid, `class="chatmod-cols"`) || !strings.Contains(grid, ">Workspace<") || !strings.Contains(grid, ">This channel<") ||
		!regexp.MustCompile(`class="chatmod-default"[^>]*>On<`).MatchString(grid) || !strings.Contains(grid, `aria-checked="false"`) {
		t.Errorf("the channel grid does not show the workspace default beside the channel's switch: %s", grid)
	}
	if ws := renderNode(t, ModAdminPanel(ModAdminProps{Model: m, CanManage: true, Workspace: true, Definitions: []chatfilter.Definition{list}, Switch: func(ModSwitch) {}})); strings.Contains(ws, "chatmod-cols") || strings.Contains(ws, "chatmod-default") {
		t.Error("the workspace's own panel shows the channel columns")
	}
}

// TestTodo_CHATUX_041_Browser: the copy in German and Arabic, the three type
// sizes and the one secondary button in the shipped stylesheet.
func TestTodo_CHATUX_041_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		for key := range chatux041Copy["en-US"] {
			if got := laneText(m, chatux041Copy, key); got == "" || strings.Contains(got, "⟦") {
				t.Errorf("%s: no text for %s", locale, key)
			}
		}
	}
	for _, rule := range []string{
		".chat-workspace .chat-details .button.secondary{padding-inline:8px;border-color:transparent;background:transparent;color:var(--accent)",
		".chat-workspace .chat-details .agent-facts{display:grid;grid-template-columns:max-content minmax(0,1fr)",
		".chatmod-cols{display:flex",
		"label:has(>input.switch)>input.switch{order:3;flex:none;margin-inline-start:auto}",
		"@supports(animation-timeline:scroll())",
	} {
		if !strings.Contains(Stylesheet, rule) {
			t.Errorf("the stylesheet lacks %s", rule)
		}
	}
	// Three sizes in the panel's own rules: 12, 13 and 16 px.
	for _, size := range []string{".6875rem", ".875rem", ".9375rem", "1.125rem"} {
		if strings.Contains(ChatUX041Styles, "font-size:"+size) {
			t.Errorf("the round-three rules use the off-scale size %s", size)
		}
	}
}
