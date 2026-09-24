package chatui

import (
	"strings"
	"testing"
)

func TestChannelTodoAndPinsRenderIndependentlyOfTimeline(t *testing.T) {
	m := Model{
		State: StateReady, SelectedID: "room", ShowDetails: true,
		Conversations: []Conversation{{ID: "room", Name: "Room", Kind: PublicChannel}},
		ChannelPins:   []ChannelPin{{PostID: "old-pin", Author: "Ari", Body: "Decision from last month", Sequence: 12}},
		ChannelTodo:   ChannelTodoList{Revision: 3, Pinned: true, Items: []ChannelTodoItem{{ID: "task-1", Text: "Confirm budget", SourcePostID: "old-pin"}}},
		Callbacks:     Callbacks{JumpToPin: func(string, uint64) {}, CopyPinReference: func(string) {}, OpenChannelTodo: func() {}, AddChannelTodo: func(string, string) {}, SetChannelTodoCompleted: func(string, bool) {}, DeleteChannelTodo: func(string) {}},
	}
	markup := renderWithTray(t, m, "todo")
	for _, want := range []string{`class="channel-todo-trigger"`, `data-action="open-todo"`, `data-action="pin-jump" data-id="old-pin"`, `data-action="pin-copy" data-id="old-pin"`, `data-action="todo-toggle" data-id="task-1"`, `id="chat-todo-pin"`, `Decision from last month`, `aria-label="Open to-do list, 1 open"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(markup, "channel-todo-inline") {
		t.Fatal("task list occupied the conversation column")
	}
	m.ChannelTodo.Pinned = false
	if markup = renderWithTray(t, m, "todo"); !strings.Contains(markup, `class="channel-todo-trigger"`) || strings.Contains(markup, "channel-todo-inline") {
		t.Fatal("compact task entry point missing or inline list returned")
	}
	m.Conversations[0].Kind = GroupChat
	markup = renderWithTray(t, m, "todo")
	if strings.Contains(markup, `id="chat-todo-section"`) || strings.Contains(markup, `channel-todo-trigger`) || strings.Contains(markup, `channel-todo-inline`) {
		t.Fatal("group chat exposed channel checklist")
	}
}

func TestChannelTodoCompletionNameAndGuestReference(t *testing.T) {
	m := Model{
		State: StateReady, SelectedID: "room", ShowDetails: true, CurrentUser: "viewer", CurrentTenantID: "home", CurrentUserName: "Ari", PinReferenceUnavailable: true,
		Conversations: []Conversation{{ID: "room", Name: "Room", Kind: PrivateChannel}},
		Members:       []Member{{ID: "worker", HomeTenantID: "other", Name: "Sam Lee"}},
		ChannelPins:   []ChannelPin{{PostID: "p", Body: "Decision", Sequence: 9}},
		ChannelTodo:   ChannelTodoList{Revision: 2, Items: []ChannelTodoItem{{ID: "a", Text: "Review", Completed: true, CompletedBySubjectID: "worker", CompletedByHomeTenantID: "other"}}},
		Callbacks:     Callbacks{SetChannelTodoCompleted: func(string, bool) {}, JumpToPin: func(string, uint64) {}, CopyPinReference: func(string) {}},
	}
	markup := renderWithTray(t, m, "todo")
	for _, want := range []string{"Completed by Sam Lee", `class="channel-todo-row"`, "References cannot be copied from a guest channel", `data-action="pin-jump" data-id="p"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	m.ChannelTodo.Items[0].CompletedBySubjectID = "unknown"
	m.ChannelTodo.Items[0].CompletedByHomeTenantID = "unknown"
	if !strings.Contains(renderWithTray(t, m, "todo"), "Completed by channel member") {
		t.Fatal("unknown completer lacked safe fallback")
	}
}

func TestChannelTodoActions(t *testing.T) {
	var got []string
	m := Model{ChannelTodo: ChannelTodoList{Items: []ChannelTodoItem{{ID: "a", Completed: false}}}, ChannelPins: []ChannelPin{{PostID: "p", Sequence: 9}}, Callbacks: Callbacks{
		JumpToPin:        func(id string, seq uint64) { got = append(got, "jump:"+id) },
		CopyPinReference: func(id string) { got = append(got, "copy:"+id) },
		SetChannelTodoCompleted: func(id string, value bool) {
			if value {
				got = append(got, "done:"+id)
			}
		},
		DeleteChannelTodo: func(id string) { got = append(got, "delete:"+id) },
		SetChannelTodoPinned: func(value bool) {
			if value {
				got = append(got, "pin")
			}
		},
	}}
	for _, action := range []struct{ action, id string }{{"pin-jump", "p"}, {"pin-copy", "p"}, {"todo-toggle", "a"}, {"todo-delete", "a"}, {"todo-pin", ""}} {
		m.act(action.action, action.id)
	}
	if joined := strings.Join(got, ","); joined != "jump:p,copy:p,done:a,delete:a,pin" {
		t.Fatalf("actions = %s", joined)
	}
}

func TestChannelTodoCompletionPolicyControlsAndEnforcement(t *testing.T) {
	selected := ChannelTodoSelectedMember{HomeTenantID: "home", SubjectID: "sam"}
	var calls []string
	m := Model{State: StateReady, SelectedID: "room", ShowDetails: true, CurrentUser: "ari", CurrentTenantID: "home", Conversations: []Conversation{{ID: "room", Kind: PublicChannel}}, Members: []Member{{ID: "sam", HomeTenantID: "home", Name: "Sam Lee"}, {ID: "jo", HomeTenantID: "guest", Name: "Jo Chen"}}, ChannelTodo: ChannelTodoList{Revision: 3, Items: []ChannelTodoItem{{ID: "restricted", Text: "Approve", CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []ChannelTodoSelectedMember{selected}, CanManageCompletionPolicy: true, CanToggle: false}}}, Callbacks: Callbacks{SetChannelTodoCompleted: func(string, bool) { calls = append(calls, "toggle") }, SetChannelTodoPolicy: func(_ string, mode string, members []ChannelTodoSelectedMember) {
		calls = append(calls, mode+":"+string(rune('0'+len(members))))
	}}}
	markup := renderWithTray(t, m, "todo")
	for _, want := range []string{`data-action="todo-policy-mode"`, `data-chat-select-value="ME_AND_SELECTED"`, `Who can complete or reopen this task`, `Only the task creator and selected members can complete or reopen this task`, `Sam Lee`, `Jo Chen`, `data-action="todo-policy-remove"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	m.act("todo-toggle", "restricted")
	if len(calls) != 0 {
		t.Fatal("forbidden toggle reached callback")
	}
	m.actWith("todo-policy-remove", "restricted", "0")
	if len(calls) != 1 || calls[0] != "ME_AND_SELECTED:0" {
		t.Fatalf("policy changes = %v", calls)
	}
	m.ChannelTodo.Items[0].CanToggle = true
	m.act("todo-toggle", "restricted")
	if len(calls) != 2 || calls[1] != "toggle" {
		t.Fatalf("allowed toggle changes = %v", calls)
	}
}

func TestChannelTodoNewTaskPolicyAndSelectedMember(t *testing.T) {
	var selected []ChannelTodoSelectedMember
	var mode string
	m := Model{State: StateReady, SelectedID: "room", ShowDetails: true, CurrentUser: "ari", CurrentTenantID: "home", Conversations: []Conversation{{ID: "room", Kind: PublicChannel}}, Members: []Member{{ID: "sam", HomeTenantID: "home", Name: "Sam Lee"}}, ChannelTodo: ChannelTodoList{Revision: 1}, ChannelTodoNewMode: "ME_AND_SELECTED", ChannelTodoNewSelected: []ChannelTodoSelectedMember{{HomeTenantID: "home", SubjectID: "sam"}}, Callbacks: Callbacks{AddChannelTodo: func(string, string) {}, SetChannelTodoNewPolicy: func(nextMode string, next []ChannelTodoSelectedMember) { mode, selected = nextMode, next }}}
	markup := renderWithTray(t, m, "todo")
	for _, want := range []string{`id="chat-todo-new-mode"`, `id="chat-todo-new-member"`, `data-chat-select-value="__none__"`, `data-action="todo-new-policy-remove"`, `Sam Lee`, `Who can complete or reopen this task`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	m.actWith("todo-new-policy-remove", "", "0")
	if mode != "ME_AND_SELECTED" || len(selected) != 0 {
		t.Fatalf("remove selection: %q %#v", mode, selected)
	}
	m.ChannelTodoNewMode, m.ChannelTodoNewSelected = "", nil
	if !strings.Contains(renderWithTray(t, m, "todo"), `<option selected value="EVERYONE"`) {
		t.Fatal("new task did not visibly default to Everyone")
	}
}

func TestChannelTodoSourcePinRequiresCurrentAuthorizedPin(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "second-room", ShowDetails: true, Conversations: []Conversation{{ID: "second-room", Kind: PublicChannel}}, ChannelTodo: ChannelTodoList{Revision: 1}, ChannelTodoSourcePin: "pin-from-first-room", Callbacks: Callbacks{AddChannelTodo: func(string, string) {}}}
	if got := todoAuthorizedSourcePin(m); got != "" {
		t.Fatalf("stale channel pin selected: %q", got)
	}
	markup := renderWithTray(t, m, "todo")
	if !strings.Contains(markup, `value=""`) || !strings.Contains(markup, `No linked message`) {
		t.Fatal("no-pin option does not have an explicit empty value")
	}
	m.ChannelPins = []ChannelPin{{PostID: "pin-from-first-room"}}
	if got := todoAuthorizedSourcePin(m); got != "pin-from-first-room" {
		t.Fatalf("authorized pin = %q", got)
	}
}

func TestChannelTodoZeroOpenHasReadableCount(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Kind: PublicChannel}}, ChannelTodo: ChannelTodoList{Revision: 1, Items: []ChannelTodoItem{{ID: "done", Text: "Done", Completed: true}}}, Number: func(n int) string {
		if n == 0 {
			return ""
		}
		return "1"
	}}
	if markup := renderWithTray(t, m, "todo"); !strings.Contains(markup, "No open tasks") || strings.Contains(markup, "> open<") {
		t.Fatalf("zero count text missing: %s", markup)
	}
}
