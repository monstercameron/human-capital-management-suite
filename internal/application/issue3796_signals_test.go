package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

type issue3796Members map[string]chatcore.Membership

func (m issue3796Members) GetMembership(_ context.Context, host, conversation, home, subject string) (chatcore.Membership, error) {
	if v, ok := m[host+"/"+conversation+"/"+home+"/"+subject]; ok {
		return v, nil
	}
	return chatcore.Membership{}, chatcore.ErrNotFound
}

// issue3796Conversations is the conversation authority the notices consult;
// it refuses the people named in deny.
type issue3796Conversations struct {
	chatcore.ConversationService
	deny map[string]bool
}

func (c issue3796Conversations) GetConversation(_ context.Context, r chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	if c.deny[r.Principal.TenantID+"/"+r.Principal.SubjectID] {
		return chatcore.Conversation{}, chatcore.ErrPermissionDenied
	}
	return chatcore.Conversation{ID: r.ConversationID, TenantID: r.TenantID}, nil
}

type issue3796Counts struct {
	mu    sync.Mutex
	asked []chatrecipient.Identity
}

func (c *issue3796Counts) Counts(_ context.Context, id chatrecipient.Identity) (chatrecipient.Counts, error) {
	c.mu.Lock()
	c.asked = append(c.asked, id)
	c.mu.Unlock()
	if id.ConversationID == "broken" {
		return chatrecipient.Counts{}, errors.New("store down")
	}
	return chatrecipient.Counts{Unread: 3, Mentions: 1}, nil
}

func issue3796Fixture(t *testing.T) (*chatCountsSignaler, *issue3796Counts, func(home, subject, conversation string) *chatstream.Subscription) {
	t.Helper()
	config := runtimeConfig("test-key")
	config.Budgets.WatchConcurrent, config.Budgets.TenantConcurrent, config.Budgets.ConversationConcurrent = 8, 8, 8
	runtime, err := NewChatStreamRuntime(config)
	if err != nil {
		t.Fatal(err)
	}
	watch := func(home, subject, conversation string) *chatstream.Subscription {
		sub, lease, werr := runtime.Watch(context.Background(), chatstream.WatchRequest{TenantID: "host", HomeTenantID: home, SubjectID: subject, ConversationID: conversation, MembershipEpoch: 1})
		if werr != nil {
			t.Fatal(werr)
		}
		t.Cleanup(func() { sub.Close(); _ = lease.Release() })
		return sub
	}
	counts := &issue3796Counts{}
	members := issue3796Members{
		"host/room/home/alice": {}, "host/room/home/bob": {}, "host/room/home/carol": {},
	}
	left := time.Now()
	members["host/room/home/dave"] = chatcore.Membership{LeftAt: &left}
	return &chatCountsSignaler{runtime: runtime, membership: members, counts: counts, conversations: issue3796Conversations{deny: map[string]bool{"home/carol": true}}}, counts, watch
}

func issue3796Next(sub *chatstream.Subscription) (chatstream.Event, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	event, err := sub.Next(ctx)
	return event, err == nil
}

// A post reaches every member who has a live tab, in rooms those tabs are not
// watching, and nobody else: not the author, not someone who left, not a
// person who is not a member, and not another home tenant.
func TestIssue3796_PostedCountsReachOnlyCurrentMembersTabs_Security(t *testing.T) {
	g, _, watch := issue3796Fixture(t)
	bobElsewhere := watch("home", "bob", "other-room")
	bobSecond := watch("home", "bob", "third-room")
	author := watch("home", "alice", "room")
	left := watch("home", "dave", "room")
	stranger := watch("home", "erin", "room")
	foreign := watch("away", "bob", "room")
	refusedMember := watch("home", "carol", "room")

	reached := g.NotifyPosted(context.Background(), "host", "room", chatcore.Principal{TenantID: "home", SubjectID: "alice"})
	if reached != 2 {
		t.Fatalf("reached %d tabs, want two", reached)
	}
	for _, tab := range []*chatstream.Subscription{bobElsewhere, bobSecond} {
		event, ok := issue3796Next(tab)
		if !ok || event.Signal != chatstream.SignalConversationUnreadChanged {
			t.Fatalf("tab got %+v ok=%v", event, ok)
		}
		got, err := chatstream.DecodeCountsSignal(event.Payload)
		if err != nil || got.Conversation != "room" || got.Host != "host" || got.Unread != 3 || got.Mentions != 1 {
			t.Fatalf("payload = %+v, %v", got, err)
		}
	}
	for name, tab := range map[string]*chatstream.Subscription{"author": author, "member who left": left, "non member": stranger, "other home tenant": foreign, "member the room refuses": refusedMember} {
		if event, ok := issue3796Next(tab); ok {
			t.Fatalf("%s heard %+v", name, event)
		}
	}
}

// A forward is a post in the destination room and tells its members too.
func TestIssue3796_ForwardToARoomNotifiesItsMembers_Integration(t *testing.T) {
	g, _, watch := issue3796Fixture(t)
	bob := watch("home", "bob", "elsewhere")
	service := &streamingChatService{ConversationService: newChatServiceStub(), runtime: g.runtime, counts: g}
	request := chatcore.ForwardPostRequest{Principal: chatcore.Principal{TenantID: "home", SubjectID: "alice"}, DestinationTenantID: "host", DestinationConversationID: "room"}
	if _, err := service.ForwardPost(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	event, ok := issue3796Next(bob)
	if !ok || event.Signal != chatstream.SignalConversationUnreadChanged {
		t.Fatalf("got %+v ok=%v", event, ok)
	}
}

// counts read earlier never overwrite counts read later: a second post that
// lands while the first notice is being built is published after it.
type issue3796OrderedCounts struct {
	mu      sync.Mutex
	calls   int
	release chan struct{}
	started chan struct{}
}

func (c *issue3796OrderedCounts) Counts(context.Context, chatrecipient.Identity) (chatrecipient.Counts, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.mu.Unlock()
	if n == 1 {
		close(c.started)
		<-c.release
	}
	return chatrecipient.Counts{Unread: uint64(n)}, nil
}

func TestIssue3796_BackToBackPostsNeverPublishStaleCountsLast_Integration(t *testing.T) {
	g, _, watch := issue3796Fixture(t)
	ordered := &issue3796OrderedCounts{release: make(chan struct{}), started: make(chan struct{})}
	g.counts = ordered
	bob := watch("home", "bob", "elsewhere")
	author := chatcore.Principal{TenantID: "home", SubjectID: "alice"}
	g.notifyPostedLater(context.Background(), "host", "room", author)
	<-ordered.started
	g.notifyPostedLater(context.Background(), "host", "room", author)
	close(ordered.release)
	last := uint64(0)
	for {
		event, ok := issue3796Next(bob)
		if !ok {
			break
		}
		got, err := chatstream.DecodeCountsSignal(event.Payload)
		if err != nil {
			t.Fatal(err)
		}
		last = got.Unread
	}
	if last != 2 {
		t.Fatalf("the last count a tab heard was %d, want the newer 2", last)
	}
}

func TestIssue3796_ReadChangedReachesOnlyThePersonsTabs(t *testing.T) {
	g, counts, watch := issue3796Fixture(t)
	a1, a2, bob := watch("home", "alice", "room"), watch("home", "alice", "other"), watch("home", "bob", "room")
	if got := g.NotifyRead(context.Background(), "host", "room", chatcore.Principal{TenantID: "home", SubjectID: "alice"}); got != 2 {
		t.Fatalf("reached %d", got)
	}
	for _, tab := range []*chatstream.Subscription{a1, a2} {
		event, ok := issue3796Next(tab)
		if !ok || event.Signal != chatstream.SignalConversationReadChanged {
			t.Fatalf("got %+v ok=%v", event, ok)
		}
	}
	if event, ok := issue3796Next(bob); ok {
		t.Fatalf("another person heard a read: %+v", event)
	}
	if len(counts.asked) != 1 || counts.asked[0].SubjectID != "alice" {
		t.Fatalf("counts asked for %+v", counts.asked)
	}
}

func TestIssue3796_FailedCountReadPublishesNothingAndNilSignalerIsQuiet(t *testing.T) {
	g, _, watch := issue3796Fixture(t)
	tab := watch("home", "alice", "room")
	if got := g.NotifyRead(context.Background(), "host", "broken", chatcore.Principal{TenantID: "home", SubjectID: "alice"}); got != 0 {
		t.Fatalf("a failed count read reached %d tabs", got)
	}
	if event, ok := issue3796Next(tab); ok {
		t.Fatalf("heard %+v", event)
	}
	var none *chatCountsSignaler
	if none.NotifyPosted(context.Background(), "host", "room", chatcore.Principal{}) != 0 || none.NotifyRead(context.Background(), "host", "room", chatcore.Principal{}) != 0 {
		t.Fatal("a nil signaler published")
	}
	none.notifyPostedLater(context.Background(), "host", "room", chatcore.Principal{})
}

func TestIssue3796_CountsNoticeBecomesAnEphemeralDeliveryWithoutContent(t *testing.T) {
	payload := chatstream.EncodeCountsSignal(chatstream.CountsSignal{Host: "host", Conversation: "room", Unread: 2})
	for signal, id := range map[string]string{
		chatstream.SignalConversationUnreadChanged: chatrecipient.UnreadChangedDeliveryID,
		chatstream.SignalConversationReadChanged:   chatrecipient.ReadChangedDeliveryID,
	} {
		event, ok := chatCountsSignalEvent(chatstream.Event{Signal: signal, Payload: payload}, "cur")
		d := event.EphemeralDelivery
		if !ok || d == nil || d.ID != id || d.Body != string(payload) || !d.OnlyVisibleToYou || d.ThreadID != "" || event.ResumeCursor != "cur" {
			t.Fatalf("%s -> %+v ok=%v", signal, event, ok)
		}
	}
	if _, ok := chatCountsSignalEvent(chatstream.Event{Signal: chatstream.SignalSidebarLayoutChanged}, ""); ok {
		t.Fatal("the sidebar notice was taken for a count notice")
	}
}

type issue3796ReadService struct {
	chatcore.ConversationService
	err error
}

func (s issue3796ReadService) UpdateReadState(_ context.Context, r chatcore.UpdateReadStateRequest) (chatcore.ReadState, error) {
	return r.ReadState, s.err
}

// Marking a room read tells the person's other tabs; a refused write tells nobody.
func TestIssue3796_UpdateReadStatePublishesAfterCommitOnly_Integration(t *testing.T) {
	g, _, watch := issue3796Fixture(t)
	tab := watch("home", "alice", "other")
	request := chatcore.UpdateReadStateRequest{Principal: chatcore.Principal{TenantID: "home", SubjectID: "alice"}, ReadState: chatcore.ReadState{TenantID: "host", ConversationID: "room"}, ExpectedRevision: 1}

	refused := &streamingChatService{ConversationService: issue3796ReadService{err: chatcore.ErrPermissionDenied}, counts: g}
	if _, err := refused.UpdateReadState(context.Background(), request); err == nil {
		t.Fatal("refusal swallowed")
	}
	if event, ok := issue3796Next(tab); ok {
		t.Fatalf("a refused write published %+v", event)
	}
	served := &streamingChatService{ConversationService: issue3796ReadService{}, counts: g}
	if _, err := served.UpdateReadState(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	event, ok := issue3796Next(tab)
	if !ok || event.Signal != chatstream.SignalConversationReadChanged {
		t.Fatalf("got %+v ok=%v", event, ok)
	}
}
