package application

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// chatfilterRows is the stored "Manage filters" answers of a fixture, by
// conversation ("" is the workspace) and role.
type chatfilterRows struct {
	rows map[string]map[string]bool
	err  error
}

func (f chatfilterRows) ManageFiltersRows(_ context.Context, _ string, conversation string) (map[string]bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]bool{}
	for role, allowed := range f.rows[""] {
		out[role] = allowed
	}
	for role, allowed := range f.rows[conversation] {
		if conversation != "" {
			out[role] = allowed
		}
	}
	return out, nil
}

// TestTodo_CHATMOD_003_Authority_Permissions: the filter service's authority
// reads the "Manage filters" rows. A row takes the permission from a role that
// has it by default, gives it to one that has not, and a channel's row wins in
// that channel.
func TestTodo_CHATMOD_003_Authority_Permissions(t *testing.T) {
	ctx := filterHTTPContext(t)
	actor := chatfilter.Actor{Tenant: "tenant-a", Subject: "admin"}
	now := time.Now().UTC()
	manager := chatfilterMembership{membership: chat.Membership{Role: chat.Manager, JoinedAt: &now}}
	member := chatfilterMembership{membership: chat.Membership{Role: chat.Member, JoinedAt: &now}}
	left := chatfilterMembership{membership: chat.Membership{Role: chat.Manager, JoinedAt: &now, LeftAt: &now}}
	for _, tc := range []struct {
		name       string
		roles      []string
		membership chatfilterMembership
		rows       map[string]map[string]bool
		channel    string
		want       bool
	}{
		{"an administrator, by default, in the workspace", []string{"hcm_admin"}, member, nil, "", true},
		{"an administrator, by default, in any channel", []string{"hcm_admin"}, member, nil, "c", true},
		{"an administrator the workspace took it from", []string{"hcm_admin"}, member, map[string]map[string]bool{"": {"WORKSPACE_ADMIN": false}}, "", false},
		{"that administrator in a channel that gives it back", []string{"hcm_admin"}, member, map[string]map[string]bool{"": {"WORKSPACE_ADMIN": false}, "c": {"WORKSPACE_ADMIN": true}}, "c", true},
		{"a manager, by default, in their channel", []string{}, manager, nil, "c", true},
		{"a manager, by default, in the workspace", []string{}, manager, nil, "", false},
		{"a manager the workspace took it from", []string{}, manager, map[string]map[string]bool{"": {"MANAGER": false}}, "c", false},
		{"that manager in a channel that gives it back", []string{}, manager, map[string]map[string]bool{"": {"MANAGER": false}, "c": {"MANAGER": true}}, "c", true},
		{"a manager whose channel took it away", []string{}, manager, map[string]map[string]bool{"c": {"MANAGER": false}}, "c", false},
		{"a manager who left", []string{}, left, nil, "c", false},
		{"a member, by default", []string{"employee"}, member, nil, "c", false},
		{"a member of a channel that gives members the permission", []string{"employee"}, member, map[string]map[string]bool{"c": {"MEMBER": true}}, "c", true},
		{"a member in another channel", []string{"employee"}, member, map[string]map[string]bool{"other": {"MEMBER": true}}, "c", false},
		{"a workspace role the workspace gave it to", []string{"employee", "filter_admin"}, member, map[string]map[string]bool{"": {"filter_admin": true}}, "", true},
		{"that role, in a channel", []string{"employee", "filter_admin"}, member, map[string]map[string]bool{"": {"filter_admin": true}}, "c", true},
		{"that role, in a channel that took it away", []string{"filter_admin"}, member, map[string]map[string]bool{"": {"filter_admin": true}, "c": {"filter_admin": false}}, "c", false},
		{"a role with a row for another role", []string{"employee"}, member, map[string]map[string]bool{"": {"filter_admin": true}}, "", false},
	} {
		authority := ChatFilterAuthority{Facts: chatfilterHTTPFacts{Roles: tc.roles}, Conversations: newChatServiceStub(), Membership: tc.membership, Permissions: chatfilterRows{rows: tc.rows}, Now: func() time.Time { return now }}
		err := authority.AuthorizeFilters(ctx, actor, tc.channel)
		if (err == nil) != tc.want {
			t.Errorf("%s: %v, want allowed=%v", tc.name, err, tc.want)
		}
		if err != nil && !errors.Is(err, chatfilter.ErrDenied) {
			t.Errorf("%s: refused with %v, want a permission refusal", tc.name, err)
		}
	}
	// Rows that cannot be read are not replaced by the defaults: the row that
	// was not read may be the one that takes the permission away.
	broken := ChatFilterAuthority{Facts: chatfilterHTTPFacts{}, Permissions: chatfilterRows{err: errors.New("database is away")}, Now: func() time.Time { return now }}
	if err := broken.AuthorizeFilters(ctx, actor, ""); !errors.Is(err, chatfilter.ErrUnavailable) {
		t.Fatalf("an administrator was let through unread rows: %v", err)
	}
	// Managing filters still buys no way to read a private room.
	if (ChatFilterAuthority{Facts: chatfilterHTTPFacts{}, Permissions: chatfilterRows{}}).CanReadFilterConversation(ctx, actor, "private") {
		t.Fatal("the permission bought private access")
	}
	// Through the service: a manager the workspace took the permission from can
	// neither list nor write the channel's filters.
	store := &chatfilterHTTPStore{}
	service := &chatfilter.Service{Store: store, Registry: chatfilter.NewRegistry(),
		Authority: ChatFilterAuthority{Facts: chatfilterHTTPFacts{Roles: []string{}}, Conversations: newChatServiceStub(), Membership: manager, Permissions: chatfilterRows{rows: map[string]map[string]bool{"": {"MANAGER": false}}}, Now: func() time.Time { return now }}}
	channelActor := chatfilter.Actor{Tenant: "tenant-a", Subject: "admin", Channel: "c"}
	if _, err := service.List(ctx, channelActor); !errors.Is(err, chatfilter.ErrDenied) {
		t.Fatalf("list: %v", err)
	}
	d := chatfilter.Definition{ID: "names", Name: "Names", Version: "1.0.0", Kind: "words", Action: "block", Match: []string{"quartz"}, Channels: []string{"c"}}
	if err := service.CreateVersion(ctx, channelActor, d); !errors.Is(err, chatfilter.ErrDenied) || store.writes != 0 {
		t.Fatalf("create: %v writes=%d", err, store.writes)
	}
}

// TestTodo_CHATMOD_003_Security_Errors: the two new refusals reach the client
// as codes it has words for, and neither says anything of a message.
func TestTodo_CHATMOD_003_Security_Errors(t *testing.T) {
	status, body := ChatFilterErrorBody(chatfilter.ErrUnknownTarget)
	if status != http.StatusBadRequest || body.Code != "unknown_target" || body.RuleName != "" || body.Span != nil {
		t.Fatalf("unknown target: %d %+v", status, body)
	}
	status, body = ChatFilterErrorBody(chatfilter.ErrDeadline)
	if status != http.StatusServiceUnavailable || body.Code != "filters_unavailable" {
		t.Fatalf("deadline: %d %+v", status, body)
	}
	// Any other invalid filter keeps its own code.
	if status, body = ChatFilterErrorBody(chatfilter.ErrInvalid); status != http.StatusBadRequest || body.Code != "invalid_filter" {
		t.Fatalf("invalid: %d %+v", status, body)
	}
}

var chatmodTickBox = regexp.MustCompile(`<input[^>]*name="post"[^>]*>`)

// TestTodo_CHATMOD_004_Integration_Selected: the served Remove dialog lists the
// messages around the one it was opened on, and the messages ticked in it are
// removed together by the ordinary count-then-apply commands.
func TestTodo_CHATMOD_004_Integration_Selected(t *testing.T) {
	f := chatmodSetup(t)
	second := f.send("author", "a second message", "second")
	third := f.send("member", "somebody else's message", "third")
	fourth := f.send("author", "a fourth message", "fourth")
	dialog := func(subject, post, extra string) (int, string) {
		response := f.call(subject, http.MethodGet, ChatModerationPagePath+"?action=remove&conversation=room&post="+post+"&locale=en-US&tz=-240"+extra, nil)
		return response.Code, response.Body.String()
	}
	code, markup := dialog("owner", second.ID, "")
	if code != http.StatusOK {
		t.Fatalf("dialog: %d %s", code, markup)
	}
	boxes := chatmodTickBox.FindAllString(markup, -1)
	if len(boxes) != 4 {
		t.Fatalf("%d tick boxes, want the four messages of the channel: %s", len(boxes), markup)
	}
	order := []string{f.post.ID, second.ID, third.ID, fourth.ID}
	for i, box := range boxes {
		if !strings.Contains(box, `value="`+order[i]+`"`) {
			t.Fatalf("tick box %d is not message %d, oldest first: %s", i, i, box)
		}
		if checked := strings.Contains(box, "checked"); checked != (order[i] == second.ID) {
			t.Fatalf("tick box %d checked=%v; only the message the dialog was opened on starts ticked", i, checked)
		}
	}
	for _, want := range []string{"Choose which messages to remove", "Alex Author", "Mia Member", "somebody else&#39;s message", `data-chatremove-selection="picked"`, `data-chatremove="quick"`} {
		if !strings.Contains(markup, want) && !strings.Contains(markup, strings.ReplaceAll(want, "&#39;", "'")) {
			t.Fatalf("the dialog misses %q: %s", want, markup)
		}
	}
	if strings.Count(markup, `type="radio"`) != 12 {
		t.Fatalf("three forms with the list of four reasons were expected: %d radios", strings.Count(markup, `type="radio"`))
	}
	// The dialog of a queue item decides that item alone: no list.
	if _, queued := dialog("owner", second.ID, "&case="+url.QueryEscape("report:none")); strings.Contains(queued, `data-chatremove-selection`) {
		t.Fatal("a queue item's dialog offers the list")
	}
	// A person who may not remove gets no dialog and so no list.
	if code, _ = dialog("member", second.ID, ""); code != http.StatusForbidden {
		t.Fatalf("a plain member's dialog: %d", code)
	}

	// Two of the four are ticked and removed together; the count comes first.
	response := f.removal("owner", "remove", "spam", "cleanup", second.ID, fourth.ID)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"Count":2`) {
		t.Fatalf("removing two ticked messages: %d %s", response.Code, response.Body.String())
	}
	page, err := f.core.ListPosts(f.as("member"), chat.ListPostsRequest{Principal: chat.Principal{TenantID: f.tenant, SubjectID: "member"}, TenantID: f.tenant, ConversationID: "room", Page: chat.Page{PageSize: 50}})
	if err != nil {
		t.Fatal(err)
	}
	removed := map[string]bool{}
	for _, post := range page.Posts {
		removed[post.ID] = post.Body == chat.RemovedByAdministrator
	}
	if !removed[second.ID] || !removed[fourth.ID] || removed[third.ID] || removed[f.post.ID] {
		t.Fatalf("what a reader sees removed: %v", removed)
	}
	if got := f.rows(`SELECT post_id FROM chat_admin_removal WHERE tenant_id=$1 AND reason_code='spam' ORDER BY post_id`, f.tenant); len(got) != 2 {
		t.Fatalf("removal records: %v", got)
	}
	// The removed ones are no longer offered; the list is not drawn for one message.
	if _, markup = dialog("owner", third.ID, ""); len(chatmodTickBox.FindAllString(markup, -1)) != 2 || strings.Contains(markup, `value="`+second.ID+`"`) {
		t.Fatalf("the list after the removal: %s", markup)
	}
}

// TestTodo_CHATMOD_005_Integration_PermissionsPage: the Permissions tab as it
// is served: only a workspace administrator gets it, a switch's assignment is
// stored and read back, and the decision functions obey it.
func TestTodo_CHATMOD_005_Integration_PermissionsPage(t *testing.T) {
	f := chatmodSetup(t)
	page := func(subject, query string) (int, string) {
		response := f.call(subject, http.MethodGet, ChatModerationPagePath+"?locale=en-US"+query, nil)
		return response.Code, response.Body.String()
	}
	// The administrator's queue offers the tab; a manager's does not, and asking
	// for it by address gives them the queue.
	if code, markup := page("admin", ""); code != http.StatusOK || !strings.Contains(markup, "tab=permissions") || !strings.Contains(markup, ">Permissions<") {
		t.Fatalf("the administrator's queue: %d %s", code, markup)
	}
	for _, subject := range []string{"owner", "member"} {
		code, markup := page(subject, "&tab=permissions")
		if code != http.StatusOK || strings.Contains(markup, `role="switch"`) || strings.Contains(markup, "tab=permissions") {
			t.Fatalf("%s reached the Permissions tab: %d %s", subject, code, markup)
		}
	}
	code, markup := page("admin", "&tab=permissions")
	if code != http.StatusOK || strings.Count(markup, `role="switch"`) != 12 || !strings.Contains(markup, "In the whole workspace") || !strings.Contains(markup, "Channel managers, in their channel") {
		t.Fatalf("the tab: %d %s", code, markup)
	}
	if strings.Contains(markup, "<style") || strings.Contains(markup, "⟦") {
		t.Fatalf("the tab carries a style element or a copy marker: %s", markup)
	}
	// A manager removes by default. The administrator takes that away for the
	// workspace: the page shows it and the command refuses.
	if response := f.call("owner", http.MethodPost, ChatModerationPath+"/permissions", chatremoveInput{Role: "MANAGER", Permission: chat.PermissionRemoveMessages, Allowed: false}); response.Code != http.StatusForbidden {
		t.Fatalf("a manager assigned a permission: %d", response.Code)
	}
	if response := f.call("admin", http.MethodPost, ChatModerationPath+"/permissions", chatremoveInput{Role: "MANAGER", Permission: chat.PermissionRemoveMessages, Allowed: false}); response.Code != http.StatusOK {
		t.Fatalf("the assignment: %d %s", response.Code, response.Body.String())
	}
	_, markup = page("admin", "&tab=permissions")
	cell := regexp.MustCompile(`<button[^>]*data-role="MANAGER"[^>]*>`).FindAllString(markup, -1)
	found := false
	for _, button := range cell {
		if strings.Contains(button, `data-permission="Remove messages"`) {
			found = true
			if !strings.Contains(button, `aria-checked="false"`) || !strings.Contains(button, `data-allowed="true"`) {
				t.Fatalf("the switch after the assignment: %s", button)
			}
		}
	}
	if !found || !strings.Contains(markup, "Set for the workspace") {
		t.Fatalf("the stored answer is not shown as the workspace's: %s", markup)
	}
	if code, _ := f.removalCode("owner", f.post.ID); code != http.StatusForbidden {
		t.Fatalf("a manager removed after the workspace took the permission: %d", code)
	}
	// A role typed by name gets a row, with nothing stored for it.
	if _, markup = page("admin", "&tab=permissions&role=hr_partner"); strings.Count(markup, `data-role="hr_partner"`) != 4 {
		t.Fatalf("a row for a typed role: %s", markup)
	}
	if got := f.rows(`SELECT role FROM chat_moderation_permission WHERE tenant_id=$1`, f.tenant); len(got) != 1 || got[0] != "MANAGER" {
		t.Fatalf("stored rows: %v", got)
	}
	// The administrator is not in this public channel's member list, so it is
	// not among the channels offered; the address of one they are not in falls
	// back to the workspace.
	if _, markup = page("admin", "&tab=permissions&conversation=room"); !strings.Contains(markup, "In the whole workspace") {
		t.Fatalf("a channel the administrator is not in was opened: %s", markup)
	}
}

// removalCode is the status of the count step of a removal, without applying.
func (f *chatmodServed) removalCode(subject string, ids ...string) (int, string) {
	f.t.Helper()
	input := chatremoveInput{Removal: chat.RemovalRequest{Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: ids}, ReasonCode: "spam", Action: "remove"}}
	response := f.call(subject, http.MethodPost, ChatModerationPath+"/preview", input)
	return response.Code, response.Body.String()
}
