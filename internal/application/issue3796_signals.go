package application

import (
	"context"
	"sync"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

// Issue 3796: one person-scoped live channel. After a post commits, every
// member who has a live tab hears the new counts for that room at once; after a
// read position moves, the person's other tabs hear the new counts. Both are
// counts only. A notice that is dropped (no tab open, queue full) costs nothing:
// the tab's periodic reconciliation reads the counts again.

// chatCountsSource is the per-person count read the notices are built from.
type chatCountsSource interface {
	Counts(context.Context, chatrecipient.Identity) (chatrecipient.Counts, error)
}

// chatCountsSignaler builds and publishes the count notices.
type chatCountsSignaler struct {
	runtime    *ChatStreamRuntime
	membership chatMembershipResolver
	counts     chatCountsSource
	// conversations is the same authority the watch itself asks (GetConversation
	// as the person): a member whose access is refused for any reason hears
	// nothing about the room.
	conversations chatcore.ConversationService
	// inflight bounds the background fan-outs; a burst beyond it is left to the
	// tab's periodic reconciliation.
	inflight chan struct{}
	mu       sync.Mutex
	running  map[string]bool
	dirty    map[string]bool
}

func (g *chatCountsSignaler) ready() bool {
	return g != nil && g.runtime != nil && g.runtime.stream != nil && g.membership != nil && g.counts != nil && g.conversations != nil
}

func chatCountsSignalName(signal string) string {
	if signal == chatstream.SignalConversationReadChanged {
		return chatstream.SignalConversationReadChanged
	}
	return chatstream.SignalConversationUnreadChanged
}

// publishCounts reads one person's current counts for one conversation and
// hands them to that person's live tabs. It returns how many tabs it reached.
func (g *chatCountsSignaler) publishCounts(ctx context.Context, signal, host, home, subject, conversation string) int {
	counts, err := g.counts.Counts(ctx, chatrecipient.Identity{HostTenantID: host, HomeTenantID: home, SubjectID: subject, ConversationID: conversation})
	if err != nil {
		return 0
	}
	payload := chatstream.EncodeCountsSignal(chatstream.CountsSignal{Host: host, Conversation: conversation, Unread: counts.Unread, Mentions: counts.Mentions})
	return g.runtime.stream.PublishSignal(ctx, home, subject, chatCountsSignalName(signal), payload)
}

// NotifyPosted tells every member with a live tab that the room has a new post.
// Membership is read now, so a person who has left hears nothing about the room;
// the author's own counts do not move and they are skipped.
func (g *chatCountsSignaler) NotifyPosted(ctx context.Context, host, conversation string, author chatcore.Principal) int {
	if !g.ready() || host == "" || conversation == "" {
		return 0
	}
	reached := 0
	for _, live := range g.runtime.stream.LiveSubjects() {
		if live.HomeTenantID == author.TenantID && live.SubjectID == author.SubjectID {
			continue
		}
		member, err := g.membership.GetMembership(ctx, host, conversation, live.HomeTenantID, live.SubjectID)
		if err != nil || member.LeftAt != nil {
			continue
		}
		if _, err := g.conversations.GetConversation(ctx, chatcore.GetConversationRequest{Principal: chatcore.Principal{TenantID: live.HomeTenantID, SubjectID: live.SubjectID}, TenantID: host, ConversationID: conversation}); err != nil {
			continue
		}
		reached += g.publishCounts(ctx, chatstream.SignalConversationUnreadChanged, host, live.HomeTenantID, live.SubjectID, conversation)
	}
	return reached
}

// NotifyRead tells the person's own tabs that their read position in a room
// moved. It runs after the position was stored by a caller who may read the
// room, so it names only that caller.
func (g *chatCountsSignaler) NotifyRead(ctx context.Context, host, conversation string, p chatcore.Principal) int {
	if !g.ready() || host == "" || conversation == "" || p.TenantID == "" || p.SubjectID == "" {
		return 0
	}
	return g.publishCounts(ctx, chatstream.SignalConversationReadChanged, host, p.TenantID, p.SubjectID, conversation)
}

// notifyPostedLater runs NotifyPosted off the request path so a large room never
// slows a send. Posts to one room that arrive while its notice is being built
// are folded into one more pass that starts after the first ends, so counts read
// earlier can never be published after counts read later.
func (g *chatCountsSignaler) notifyPostedLater(ctx context.Context, host, conversation string, author chatcore.Principal) {
	if !g.ready() {
		return
	}
	key := host + "/" + conversation
	g.mu.Lock()
	if g.running == nil {
		g.running, g.dirty = map[string]bool{}, map[string]bool{}
	}
	if g.running[key] {
		g.dirty[key] = true
		g.mu.Unlock()
		return
	}
	if g.inflight != nil {
		select {
		case g.inflight <- struct{}{}:
		default:
			g.mu.Unlock()
			return
		}
	}
	g.running[key] = true
	g.mu.Unlock()
	detached := context.WithoutCancel(ctx)
	go func() {
		if g.inflight != nil {
			defer func() { <-g.inflight }()
		}
		for {
			bounded, cancel := context.WithTimeout(detached, 5*time.Second)
			g.NotifyPosted(bounded, host, conversation, author)
			cancel()
			g.mu.Lock()
			if !g.dirty[key] {
				delete(g.running, key)
				g.mu.Unlock()
				return
			}
			delete(g.dirty, key)
			g.mu.Unlock()
			// A later post may have had another author; the later counts are
			// true for everyone, so nobody is skipped on the repeat pass.
			author = chatcore.Principal{}
		}
	}()
}

// chatCountsSignalEvent is the watch event a count notice becomes, or false for
// any other signal.
func chatCountsSignalEvent(event chatstream.Event, cursor string) (chatcore.WatchEvent, bool) {
	id := ""
	switch event.Signal {
	case chatstream.SignalConversationUnreadChanged:
		id = chatrecipient.UnreadChangedDeliveryID
	case chatstream.SignalConversationReadChanged:
		id = chatrecipient.ReadChangedDeliveryID
	default:
		return chatcore.WatchEvent{}, false
	}
	return chatcore.WatchEvent{
		EphemeralDelivery: &chatcore.EphemeralDelivery{ID: id, Body: string(event.Payload), OnlyVisibleToYou: true},
		ResumeCursor:      cursor,
	}, true
}

// UpdateReadState stores the read position, then tells the person's other tabs.
// The notice follows the commit and only a successful write sends one.
func (s *streamingChatService) UpdateReadState(ctx context.Context, r chatcore.UpdateReadStateRequest) (chatcore.ReadState, error) {
	state, err := s.ConversationService.UpdateReadState(ctx, r)
	if err == nil {
		s.counts.NotifyRead(ctx, r.ReadState.TenantID, r.ReadState.ConversationID, r.Principal)
	}
	return state, err
}

// newChatCountsSignaler wires the notices to the stream and the recipient
// store. Without recipient state it returns nil and no notice is sent.
func newChatCountsSignaler(runtime *ChatStreamRuntime, membership chatMembershipResolver, conversations chatcore.ConversationService, extensions *ChatExtensions) *chatCountsSignaler {
	if runtime == nil || membership == nil || conversations == nil || extensions == nil || extensions.Recipients == nil || extensions.Recipients.Repo == nil {
		return nil
	}
	return &chatCountsSignaler{runtime: runtime, membership: membership, counts: extensions.Recipients.Repo, conversations: conversations, inflight: make(chan struct{}, 16)}
}
