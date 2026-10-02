package main

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATBUG-072: a new channel is sorted into place in its section, and the
// service's refusal of a name is told from any other failure.
func TestTodo_CHATBUG_072_NewChannelSortsIntoPlace(t *testing.T) {
	names := func(rooms []chatui.Conversation) []string {
		var out []string
		for _, r := range rooms {
			out = append(out, r.Name)
		}
		return out
	}
	list := []chatui.Conversation{{Name: "announcements"}, {Name: "design"}, {Name: "sales"}}
	for added, want := range map[string][]string{
		"general-chat": {"announcements", "design", "general-chat", "sales"},
		"aardvark":     {"aardvark", "announcements", "design", "sales"},
		"zebra":        {"announcements", "design", "sales", "zebra"},
		"Design":       {"announcements", "design", "Design", "sales"},
	} {
		if got := names(insertChannelInOrder(list, chatui.Conversation{Name: added})); !reflect.DeepEqual(got, want) {
			t.Errorf("adding %q: %v, want %v", added, got, want)
		}
	}
	if len(list) != 3 {
		t.Error("the section's own list was changed")
	}
	if got := names(insertChannelInOrder(nil, chatui.Conversation{Name: "first"})); !reflect.DeepEqual(got, []string{"first"}) {
		t.Errorf("an empty section: %v", got)
	}
	// A list the person ordered by hand keeps its order around the new row.
	manual := []chatui.Conversation{{Name: "sales"}, {Name: "design"}}
	if got := names(insertChannelInOrder(manual, chatui.Conversation{Name: "general"})); !reflect.DeepEqual(got, []string{"sales", "design", "general"}) {
		t.Errorf("a hand-ordered list: %v", got)
	}
}

func TestTodo_CHATBUG_072_NameRefusalIsTold(t *testing.T) {
	detail := &commonv1.ErrorDetail{ReasonRef: chat.ReasonChannelName}
	refused, err := status.New(codes.InvalidArgument, "bad name").WithDetails(detail)
	if err != nil {
		t.Fatal(err)
	}
	other, err := status.New(codes.InvalidArgument, "bad").WithDetails(&commonv1.ErrorDetail{ReasonRef: "chat.invalid_argument"})
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"the name rule":        {refused.Err(), true},
		"another invalid":      {other.Err(), false},
		"permission denied":    {status.Error(codes.PermissionDenied, "no"), false},
		"not a status":         {errors.New("network"), false},
		"no error":             {nil, false},
		"the right reason, ok": {status.Error(codes.OK, ""), false},
	} {
		if got := channelNameRefused(tc.err); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

// CHATUX-021: the purpose answers "Saved" only once the save went through.
func TestTodo_CHATUX_021_PurposeSavedConfirmation(t *testing.T) {
	run := func(models []chatui.Model) (shown []bool, slept []time.Duration) {
		i := 0
		read := func() chatui.Model {
			m := models[min(i, len(models)-1)]
			return m
		}
		confirmPurposeSaved(" Hiring ", read, func(saved bool) { shown = append(shown, saved) }, func(d time.Duration) { slept = append(slept, d); i++ })
		return shown, slept
	}
	room := func(purpose string, pending bool, failure string) chatui.Model {
		m := chatui.Model{SelectedID: "general", ChannelWidgetsPending: pending, ChannelWidgetsError: failure}
		m.ChannelTeam.Purpose = purpose
		return m
	}
	// The read that opens the watch, then: pending, pending, saved.
	shown, slept := run([]chatui.Model{room("Old", false, ""), room("Old", true, ""), room("Old", true, ""), room("Hiring", false, "")})
	if !reflect.DeepEqual(shown, []bool{true, false}) {
		t.Fatalf("Saved shown %v, want on then off", shown)
	}
	if slept[len(slept)-1] != purposeSavedFor || purposeSavedFor != 2*time.Second {
		t.Errorf("Saved stays for %v (%v)", slept[len(slept)-1], slept)
	}
	// A failed save says nothing.
	if shown, _ := run([]chatui.Model{room("Old", false, ""), room("Old", true, ""), room("Old", false, "save")}); len(shown) != 0 {
		t.Errorf("a failed save was confirmed: %v", shown)
	}
	// Moving to another room says nothing.
	elsewhere := room("Old", true, "")
	elsewhere.SelectedID = "sales"
	if shown, _ := run([]chatui.Model{room("Old", false, ""), elsewhere}); len(shown) != 0 {
		t.Errorf("another room was confirmed: %v", shown)
	}
	// A save that never settles gives up.
	if shown, slept := run([]chatui.Model{room("Old", false, ""), room("Old", true, "")}); len(shown) != 0 || len(slept) != 50 {
		t.Errorf("a save that never settles: shown %v after %d waits", shown, len(slept))
	}
}

// CHATUX-019: who else reacted with an emoji is recorded for the chip's name.
func TestTodo_CHATUX_019_ReactionPeopleAreRecorded(t *testing.T) {
	members := map[chatReactionIdentity]struct{}{
		{homeTenantID: "t", subjectID: "me", emoji: "👀"}:  {},
		{homeTenantID: "t", subjectID: "sam", emoji: "👀"}: {},
		{homeTenantID: "t", subjectID: "kim", emoji: "👍"}: {},
		{homeTenantID: "t", subjectID: "sam", emoji: "👍"}: {},
		{homeTenantID: "t", subjectID: "kim", emoji: "🎉"}: {},
	}
	chips := chatReactionChipsFromMembers(members, "me")
	byEmoji := map[string]chatui.ReactionChip{}
	for _, c := range chips {
		byEmoji[c.Emoji] = c
	}
	if c := byEmoji["👀"]; c.Count != 2 || !c.Mine || !reflect.DeepEqual(c.PeopleIDs, []string{"sam"}) {
		t.Errorf("eyes: %+v", c)
	}
	if c := byEmoji["👍"]; c.Count != 2 || c.Mine || !reflect.DeepEqual(c.PeopleIDs, []string{"kim", "sam"}) {
		t.Errorf("thumbs up: %+v", c)
	}
	if c := byEmoji["🎉"]; c.Count != 1 || !reflect.DeepEqual(c.PeopleIDs, []string{"kim"}) {
		t.Errorf("party: %+v", c)
	}
	// A repeated person is listed once, and the viewer never.
	again := []chatui.ReactionChip{{Emoji: "👀", Count: 1}}
	notePersonOnChip(again, "👀", "sam", false)
	notePersonOnChip(again, "👀", "sam", false)
	notePersonOnChip(again, "👀", "me", true)
	notePersonOnChip(again, "🎉", "kim", false)
	notePersonOnChip(again, "👀", "", false)
	if !reflect.DeepEqual(again[0].PeopleIDs, []string{"sam"}) {
		t.Errorf("people on a chip: %v", again[0].PeopleIDs)
	}
}

// CHATUX-019: a pin carries who pinned it and when.
func TestTodo_CHATUX_019_PinProjection(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 41, 0, 0, time.UTC)
	pin := &chatv1.Pin{PostId: "p1", PinnedBy: "walt", CreatedAt: timestamppb.New(at), Post: &chatv1.Post{Id: "p1", AuthorId: "jake", Body: "**Policy**", Sequence: 7, Revision: 2}}
	got := chatPinProjection(pin, map[string]string{"walt": "Walt Brennan", "jake": "Jake Sullivan"})
	if got.PostID != "p1" || got.Author != "Jake Sullivan" || got.PinnedBy != "Walt Brennan" || !got.PinnedAt.Equal(at) || got.Sequence != 7 || got.Revision != 2 || got.Body != "**Policy**" {
		t.Errorf("projection: %+v", got)
	}
	bare := chatPinProjection(&chatv1.Pin{PostId: "p2", Post: &chatv1.Post{AuthorId: "jake"}}, map[string]string{"jake": "Jake Sullivan"})
	if bare.PinnedBy != "" || !bare.PinnedAt.IsZero() {
		t.Errorf("a pin with no pinner or time: %+v", bare)
	}
}

// CHATUX-020: a conversation marked unread shows as unread until it is opened,
// including in the copy each sidebar section holds.
func TestTodo_CHATUX_020_ManualUnread(t *testing.T) {
	setManualUnread("sales", false)
	setManualUnread("quiet", false)
	model := &chatui.Model{SelectedID: "general",
		Conversations: []chatui.Conversation{{ID: "general"}, {ID: "sales"}, {ID: "quiet", Unread: 3}},
		Sections:      []chatui.SidebarSection{{ID: "channels", Chats: []chatui.Conversation{{ID: "general"}, {ID: "sales"}}}},
	}
	markConversationUnreadInModel(model, "sales", true)
	setManualUnread("sales", true)
	if model.Conversations[1].Unread != 1 || model.Sections[0].Chats[1].Unread != 1 || model.Conversations[0].Unread != 0 {
		t.Errorf("marking unread: %+v", model)
	}
	// A projection reload that says zero keeps it unread; one that says more keeps the more.
	model.Conversations[1].Unread = 0
	model.Conversations[2].Unread = 0
	setManualUnread("quiet", true)
	applyManualUnread(model)
	if model.Conversations[1].Unread != 1 || model.Conversations[2].Unread != 1 {
		t.Errorf("a reload dropped the mark: %+v", model.Conversations)
	}
	model.Conversations[1].Unread = 5
	applyManualUnread(model)
	if model.Conversations[1].Unread != 5 {
		t.Errorf("the mark lowered a real count: %d", model.Conversations[1].Unread)
	}
	// Opening the conversation reads it and drops the mark for good.
	model.SelectedID = "sales"
	model.Conversations[1].Unread = 0
	applyManualUnread(model)
	model.SelectedID = "general"
	applyManualUnread(model)
	if model.Conversations[1].Unread != 0 {
		t.Errorf("an opened conversation stayed unread: %+v", model.Conversations[1])
	}
	// Marking read clears it in every copy.
	markConversationUnreadInModel(model, "quiet", false)
	if model.Conversations[2].Unread != 0 || model.Conversations[2].Mentions != 0 {
		t.Errorf("marking read: %+v", model.Conversations[2])
	}
	setManualUnread("quiet", false)
}
