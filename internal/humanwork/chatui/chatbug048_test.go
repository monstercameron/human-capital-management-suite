package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func chatbug048Model() Model {
	m := chatux021Model()
	m.ChatFeatures = &ChatFeatures{Translation: true, Filters: true, Renderings: true}
	m.FilterSettings = func() ui.Node { return html.P(html.Props{Text: "channel filter settings"}) }
	m.WorkspaceFilterSettings = func() ui.Node { return html.P(html.Props{Text: "workspace filter settings"}) }
	return m
}

func chatbug048Manage(t *testing.T, m Model) string {
	t.Helper()
	c := m.selected()
	built := channelWidgetPartsFor(m, handlers{})
	return renderNode(t, chatux005Manage(m, handlers{}, c, &built))
}

// Every row of Manage channel is the same disclosure row (a 12 px label, the
// current value at the right, a chevron) under an 11 px group caption; a note
// shows only where it applies; workspace-wide translation settings are not here.
func TestTodo_CHATBUG_048(t *testing.T) {
	m := chatbug048Model()
	got := chatbug048Manage(t, m)
	// Three groups, three captions in one style, and no section titles of their own.
	if strings.Count(got, `class="manage-caption"`) != 3 {
		t.Errorf("%d captions, want 3: %s", strings.Count(got, `class="manage-caption"`), got)
	}
	for _, caption := range []string{">Channel<", ">Rules and language<", ">Apps<"} {
		if !strings.Contains(got, caption) {
			t.Errorf("caption %q is missing", caption)
		}
	}
	if strings.Contains(got, "<h3") || strings.Contains(got, "details-section-head") && !strings.Contains(got, "chat-details-group-project") {
		t.Errorf("a row brings its own heading style: %s", got)
	}
	// The rows, each a disclosure row: Status, Role labels, Project and
	// milestones, filters (channel and workspace), and the app request.
	for _, row := range []string{">Status<", "Role labels", "Project and milestones", ">Manage filters<", ">Workspace filters<", ">Integrations<"} {
		if !strings.Contains(got, row) {
			t.Errorf("row %q is missing: %s", row, got)
		}
	}
	// The workspace's own translation settings are not in a channel's panel.
	for _, gone := range []string{"Monthly limit", "Glossary", "Whole workspace", "Manage translation"} {
		if strings.Contains(got, gone) {
			t.Errorf("Manage channel holds %q", gone)
		}
	}
	// The sentence about direct messages is not said in a channel.
	if strings.Contains(got, "Direct messages follow only") {
		t.Errorf("a channel's Manage section talks about direct messages: %s", got)
	}
	// A direct message has no Manage section at all (CHATUX-021), and a group
	// says the sentence where it is true.
	group := m
	group.Conversations = []Conversation{{ID: "huddle", Name: "huddle", Kind: GroupChat, OwnerID: "walt", Joined: true}}
	group.SelectedID = "huddle"
	group.ChannelStatuses = nil
	if groupManage := renderNode(t, filterSettingsEntry(group, group.Conversations[0])); !strings.Contains(groupManage, "Direct messages follow only the workspace") {
		t.Errorf("a group does not carry the note: %s", groupManage)
	}
	dm := group
	dm.Conversations = []Conversation{{ID: "walt-jake", Name: "Jake", Kind: DirectMessage, Joined: true}}
	dm.SelectedID = "walt-jake"
	if chatux005Manage(dm, handlers{}, dm.Conversations[0], nil) != nil {
		t.Error("a direct message has a Manage section")
	}
}

// Every disclosure row of the section is the one row: the summary class, a label
// in a span, the value at the right and the chevron after it.
func TestTodo_CHATBUG_048_RowShape(t *testing.T) {
	row := renderNode(t, manageRow("x", "Status", html.Span(html.Props{Text: "Open"}), html.P(html.Props{Text: "body"})))
	if !strings.Contains(row, `class="manage-row-summary chat-disclosure-button"`) || !strings.Contains(row, "<span>Status</span>") || !strings.Contains(row, `class="manage-row-value"`) || !strings.Contains(row, "icon-chevron-down") {
		t.Errorf("the disclosure row: %s", row)
	}
	if strings.Index(row, "<span>Status</span>") > strings.Index(row, "manage-row-value") || strings.Index(row, "manage-row-value") > strings.Index(row, "icon-chevron-down") {
		t.Errorf("label, value, chevron are out of order: %s", row)
	}
	// The section that draws its body on demand has the same summary.
	summary := renderNode(t, manageSummary(html.Props{Type: "button", Aria: map[string]string{"expanded": "false"}}, "Manage filters", nil))
	if !strings.Contains(summary, `class="manage-row-summary "`) || !strings.Contains(summary, "<span>Manage filters</span>") || !strings.Contains(summary, "icon-chevron-down") {
		t.Errorf("the on-demand summary: %s", summary)
	}
	static := renderNode(t, manageStatic("x", "Filters", nil))
	if strings.Contains(static, "<button") || !strings.Contains(static, "manage-row-summary") {
		t.Errorf("the static row: %s", static)
	}
}

// One size for labels and one for captions, in the stylesheet that ships.
func TestTodo_CHATBUG_048_Sizes(t *testing.T) {
	rule := func(selector string) string {
		at := strings.Index(ChatBug048Styles, selector)
		if at < 0 {
			t.Fatalf("no rule for %s", selector)
		}
		return ChatBug048Styles[at : at+strings.Index(ChatBug048Styles[at:], "}")]
	}
	if r := rule(".details-manage .manage-caption{"); !strings.Contains(r, "font-size:.6875rem") {
		t.Errorf("the caption is not 11 px: %s", r)
	}
	if r := rule(".details-manage .manage-row-summary>span:first-child"); !strings.Contains(r, "font-size:.75rem") {
		t.Errorf("the label is not 12 px: %s", r)
	}
	if r := rule(".details-manage .manage-row-value{"); !strings.Contains(r, "font-size:.75rem") {
		t.Errorf("the value is not 12 px: %s", r)
	}
	if regexp.MustCompile(`#[0-9a-fA-F]{3,6}\b`).MatchString(ChatBug048Styles + ChatUX021Styles + ChatUX020Styles) {
		t.Error("the panel's styles hold a raw colour")
	}
	if !strings.Contains(Stylesheet, ChatLane3Styles) {
		t.Error("this lane's styles are not in the stylesheet")
	}
}
