package main

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type chatSoundPolicyStream struct {
	responses []*chatv1.WatchConversationResponse
	index     int
}

func (s *chatSoundPolicyStream) Recv() (*chatv1.WatchConversationResponse, error) {
	if s.index >= len(s.responses) {
		return nil, io.EOF
	}
	response := s.responses[s.index]
	s.index++
	return response, nil
}

func TestChatSoundPolicyTargetsNewDirectMessagesAndMentions(t *testing.T) {
	now := time.Date(2026, time.September, 22, 16, 0, 0, 0, time.UTC)
	model := chatui.Model{
		CurrentUser: "ari", CurrentUserName: "Ari Smith",
		Conversations: []chatui.Conversation{{ID: "dm", Kind: chatui.DirectMessage}, {ID: "channel", Kind: chatui.PublicChannel}},
		Preferences:   chatui.Preferences{Notifications: map[string]chatui.NotificationMode{"dm": chatui.NotifyAll, "channel": chatui.NotifyAll}},
	}
	post := func(id, author, body string, created time.Time) *chatv1.Post {
		return &chatv1.Post{Id: id, AuthorId: author, Body: body, Sequence: 1, CreatedAt: timestamppb.New(created)}
	}
	for _, tc := range []struct {
		name string
		room string
		post *chatv1.Post
		want bool
	}{
		{"incoming dm", "dm", post("dm-1", "sam", "Hello", now), true},
		{"own post", "dm", post("dm-2", "ari", "Hello", now), false},
		{"channel mention by display name", "channel", post("ch-1", "sam", "Hi @Ari Smith!", now), true},
		{"mention id at end", "channel", post("ch-2", "sam", "FYI @ari", now), true},
		{"not a full mention token", "channel", post("ch-3", "sam", "@ariana", now), false},
		{"unmentioned channel post", "channel", post("ch-4", "sam", "Hello team", now), false},
		{"old event", "dm", post("old", "sam", "Hello", now.Add(-chatSoundFreshness-time.Second)), false},
		{"missing creation time", "dm", &chatv1.Post{Id: "no-time", AuthorId: "sam", Sequence: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPlayChatSound(model, tc.room, tc.post, now, false, false); got != tc.want {
				t.Fatalf("shouldPlayChatSound() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChatSoundBootstrapRequiresPostBeyondLoadedTimeline(t *testing.T) {
	if !chatPostNewerThanLoadedTimeline(chatui.Model{}, &chatv1.Post{Id: "first", Sequence: 1}) {
		t.Fatal("first post in a genuinely empty room was not considered new")
	}
	model := chatui.Model{Messages: []chatui.Message{{ID: "latest", Sequence: 42}}}
	preexisting := &chatv1.Post{Id: "recent-replay", Sequence: 41}
	newPost := &chatv1.Post{Id: "new", Sequence: 43}
	if chatPostNewerThanLoadedTimeline(model, preexisting) {
		t.Fatal("recent replay below loaded watermark was considered new")
	}
	if !chatPostNewerThanLoadedTimeline(model, newPost) {
		t.Fatal("post beyond loaded watermark was not considered new")
	}
	model.HasNewer = true
	if chatPostNewerThanLoadedTimeline(model, newPost) {
		t.Fatal("anchored initial projection with newer history was considered live")
	}
}

func TestChatSoundFailsClosedUntilConversationPreferenceIsLoaded(t *testing.T) {
	now := time.Now()
	model := chatui.Model{CurrentUser: "ari", Conversations: []chatui.Conversation{{ID: "dm", Kind: chatui.DirectMessage}}}
	post := &chatv1.Post{Id: "p", AuthorId: "sam", Sequence: 1, CreatedAt: timestamppb.New(now)}
	if shouldPlayChatSound(model, "dm", post, now, false, false) {
		t.Fatal("missing mute preference should fail closed")
	}
}

func TestChatSoundPollStartsAfterBaselineAndDedupeWatermark(t *testing.T) {
	posts := []*chatv1.Post{
		{Id: "old-2", Sequence: 2},
		{Id: "new-4", Sequence: 4},
		{Id: "new-3", Sequence: 3},
		{Id: "old-1", Sequence: 1},
	}
	if got := chatSoundLatestPostSequence(posts); got != 4 {
		t.Fatalf("baseline = %d, want 4", got)
	}
	if got := chatSoundEventsAfter(posts, 2); len(got) != 2 || got[0].GetSequence() != 3 || got[1].GetSequence() != 4 {
		t.Fatalf("posts after watermark = %v, want ordered sequences 3 and 4", got)
	}
	if got := chatSoundEventsAfter(posts, 4); len(got) != 0 {
		t.Fatalf("replayed posts after watermark = %v, want none", got)
	}
}

func TestChatSoundPollBatchIsBoundedAndRoundRobin(t *testing.T) {
	conversations := make([]chatui.Conversation, 9)
	for i := range conversations {
		conversations[i].ID = fmt.Sprintf("room-%d", i)
	}
	conversations[4].ID = ""
	first, cursor := chatSoundPollBatch(conversations, 0, 3)
	if len(first) != 3 || first[0].ID != "room-0" || first[2].ID != "room-2" || cursor != 3 {
		t.Fatalf("first batch=%v cursor=%d", first, cursor)
	}
	second, cursor := chatSoundPollBatch(conversations, cursor, 3)
	if len(second) != 3 || second[0].ID != "room-3" || second[1].ID != "room-5" || second[2].ID != "room-6" || cursor != 7 {
		t.Fatalf("second batch=%v cursor=%d", second, cursor)
	}
	third, cursor := chatSoundPollBatch(conversations, cursor, 3)
	if len(third) != 3 || third[0].ID != "room-7" || third[1].ID != "room-8" || third[2].ID != "room-0" || cursor != 1 {
		t.Fatalf("third batch=%v cursor=%d", third, cursor)
	}
	wrapped, cursor := chatSoundPollBatch(conversations, cursor, 3)
	if len(wrapped) != 3 || wrapped[0].ID != "room-1" || wrapped[2].ID != "room-3" || cursor != 4 {
		t.Fatalf("wrapped batch=%v cursor=%d", wrapped, cursor)
	}
}

func TestChatSoundPollBatchPrioritizesDirectMessages(t *testing.T) {
	conversations := []chatui.Conversation{
		{ID: "dm-1", Kind: chatui.DirectMessage},
		{ID: "dm-2", Kind: chatui.DirectMessage},
		{ID: "dm-3", Kind: chatui.DirectMessage},
		{ID: "dm-4", Kind: chatui.DirectMessage},
		{ID: "channel-1", Kind: chatui.PublicChannel},
		{ID: "channel-2", Kind: chatui.PublicChannel},
	}
	batch, _ := chatSoundPollBatch(conversations, 0, 3)
	if len(batch) != 3 || batch[0].Kind != chatui.DirectMessage || batch[1].Kind != chatui.DirectMessage || batch[2].Kind != chatui.PublicChannel {
		t.Fatalf("batch did not reserve two slots for direct messages and one for channels: %#v", batch)
	}
}

func TestChatSoundPolicyRespectsMuteQuietHoursAndReducedInterruption(t *testing.T) {
	now := time.Date(2026, time.September, 22, 23, 30, 0, 0, time.UTC)
	post := &chatv1.Post{Id: "p", AuthorId: "sam", Body: "Hi @ari", Sequence: 1, CreatedAt: timestamppb.New(now)}
	model := chatui.Model{
		CurrentUser: "ari", Conversations: []chatui.Conversation{{ID: "channel", Kind: chatui.PublicChannel}},
		Preferences: chatui.Preferences{Notifications: map[string]chatui.NotificationMode{"channel": chatui.NotifyAll}},
	}
	allowed := func() bool { return shouldPlayChatSound(model, "channel", post, now, false, false) }
	if !allowed() {
		t.Fatal("explicit mention should notify by default")
	}
	model.Preferences.Notifications["channel"] = chatui.NotifyMute
	if allowed() {
		t.Fatal("muted conversation should not make sound")
	}
	model.Preferences.Notifications["channel"] = chatui.NotifyAll
	model.Conversations[0].Muted = true
	if allowed() {
		t.Fatal("conversation mute projection should not make sound")
	}
	model.Conversations[0].Muted = false
	model.Preferences.QuietHours = true
	model.Preferences.QuietTimezone = "UTC"
	model.Preferences.QuietStartMinute = 23 * 60
	model.Preferences.QuietEndMinute = 7 * 60
	if allowed() {
		t.Fatal("quiet hours should suppress sound across midnight")
	}
	model.Preferences.QuietHours = false
	if shouldPlayChatSound(model, "channel", post, now, true, false) || shouldPlayChatSound(model, "channel", post, now, false, true) {
		t.Fatal("reduced interruption settings should suppress sound")
	}
	model.Preferences.Notifications["channel"] = chatui.NotifyMention
	post.Body = "General update"
	if allowed() {
		t.Fatal("mentions-only preference should suppress non-mentions")
	}
	model.Preferences.Notifications["channel"] = chatui.NotifyAll
	if allowed() {
		t.Fatal("all notifications preference should not sound for ordinary channel posts")
	}
	model.Conversations[0].Kind = chatui.DirectMessage
	if !allowed() {
		t.Fatal("direct message should sound with all notifications enabled")
	}
}

func TestChatSoundOnlyReceivesAppliedStreamEvents(t *testing.T) {
	stream := &chatSoundPolicyStream{responses: []*chatv1.WatchConversationResponse{
		{Event: &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED}},
		{Event: &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED}},
	}}
	applied := 0
	_, reason, err := drainChatStream(context.Background(), stream, chatStreamHooks{
		Apply: func(*chatv1.ConversationEvent, string) (chatEventOutcome, bool) {
			if stream.index == 1 {
				return chatEventDuplicate, true
			}
			return chatEventApplied, true
		},
		Applied: func(*chatv1.ConversationEvent) { applied++ },
	})
	if err != io.EOF || reason != chatStreamEndTransport {
		t.Fatalf("drain reason=%v err=%v, want transport EOF", reason, err)
	}
	if applied != 1 {
		t.Fatalf("Applied called %d times, want once for the new event after dropping replay", applied)
	}
}
