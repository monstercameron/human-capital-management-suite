package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3815: a post that asks its audience to acknowledge it. The sender of a
// post, when they own or manage the channel, marks it; each other active member
// of the channel acknowledges it once; the people who manage the channel see who
// has and who has not. Every decision is made here, before anything is written.
//
// The audience is resolved on the server from the channel's current
// memberships: active members of this company, other than the sender. A read is
// never an acknowledgement; only Acknowledge writes one.

var (
	ErrChatAckInvalid     = errors.New("chat acknowledgement: invalid request")
	ErrChatAckNotFound    = errors.New("chat acknowledgement: post not found")
	ErrChatAckDenied      = errors.New("chat acknowledgement: not allowed")
	ErrChatAckUnavailable = errors.New("chat acknowledgement: unavailable")
)

// ChatAckChat is the conversation service as acknowledgements use it.
type ChatAckChat interface {
	GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
	ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error)
}

// ChatAckStore keeps the requirements and the acknowledgements.
type ChatAckStore interface {
	RequireAck(context.Context, chatstore.AckRequirement) (bool, error)
	AckRequirements(ctx context.Context, tenant, conversation string, posts []string) (map[string]chatstore.AckRequirement, error)
	RecordAck(context.Context, chatstore.Ack) (bool, error)
	AckCounts(ctx context.Context, tenant, conversation string, posts []string) (map[string]int, error)
	MyAcks(ctx context.Context, tenant, conversation string, posts []string, subject string) (map[string]time.Time, error)
	ListAcks(ctx context.Context, tenant, conversation, post string) ([]chatstore.Ack, error)
}

// ChatAckView is what one reader may see of one post that requires
// acknowledgement: their own state, and, for the people who manage the channel,
// the roll. Total, Acknowledged and Pending are filled only when CanManage.
type ChatAckView struct {
	PostID         string     `json:"post_id"`
	Required       bool       `json:"required"`
	CanAcknowledge bool       `json:"can_acknowledge"`
	Acknowledged   bool       `json:"acknowledged"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	CanManage      bool       `json:"can_manage"`
	Total          int        `json:"total,omitempty"`
	AckCount       int        `json:"ack_count,omitempty"`
	Pending        []string   `json:"pending,omitempty"`
	PendingMore    int        `json:"pending_more,omitempty"`
}

// ChatAckReply answers a status read: the views of the posts that require
// acknowledgement, and whether this reader may mark a post of their own.
type ChatAckReply struct {
	Views      map[string]ChatAckView `json:"views"`
	CanRequire bool                   `json:"can_require"`
}

// ChatPostAcks is the use case behind the acknowledgement routes.
type ChatPostAcks struct {
	Chat  ChatAckChat
	Store ChatAckStore
}

const (
	chatAckMaxViews   = 50
	chatAckMaxPending = 200
	chatAckMaxPages   = 50
)

func (c *ChatPostAcks) ready() bool { return c != nil && c.Chat != nil && c.Store != nil }

func chatAckPrincipal(human *trust.Principal) (chat.Principal, error) {
	p, err := chatWorkPrincipal(human)
	if err != nil {
		return chat.Principal{}, ErrChatAckDenied
	}
	return p, nil
}

func chatAckChatError(err error) error {
	switch {
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
		return ErrChatAckNotFound
	default:
		return ErrChatAckUnavailable
	}
}

// chatAckRoll is who is in a channel right now.
type chatAckRoll struct {
	members map[string]bool // active members of this company
	manager map[string]bool // those of them who manage the channel
}

// roll reads the channel's memberships as the reader.
func (c *ChatPostAcks) roll(ctx context.Context, p chat.Principal, conv chat.Conversation) (chatAckRoll, error) {
	out := chatAckRoll{members: map[string]bool{}, manager: map[string]bool{}}
	cursor := ""
	for page := 0; page < chatAckMaxPages; page++ {
		res, err := c.Chat.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: p, TenantID: p.TenantID, ConversationID: conv.ID, Page: chat.Page{Cursor: cursor, PageSize: 200}})
		if err != nil {
			return chatAckRoll{}, chatAckChatError(err)
		}
		for _, m := range res.Memberships {
			if m.JoinedAt == nil || m.LeftAt != nil || m.HomeTenantID != p.TenantID {
				continue
			}
			out.members[m.SubjectID] = true
			if strings.EqualFold(string(m.Role), string(chat.Manager)) {
				out.manager[m.SubjectID] = true
			}
		}
		if res.NextCursor == "" {
			return out, nil
		}
		cursor = res.NextCursor
	}
	return chatAckRoll{}, ErrChatAckUnavailable
}

func (r chatAckRoll) manages(conv chat.Conversation, subject string) bool {
	return conv.OwnerID == subject || r.manager[subject]
}

// audience is the members who are asked to acknowledge a post the sender wrote.
func (r chatAckRoll) audience(sender string) []string {
	out := make([]string, 0, len(r.members))
	for id := range r.members {
		if id != sender {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func chatAckChannel(conv chat.Conversation) bool {
	return conv.Kind == chat.PublicChannel || conv.Kind == chat.PrivateChannel
}

// views builds what the reader may see of each required post.
func (c *ChatPostAcks) views(ctx context.Context, p chat.Principal, conv chat.Conversation, reqs map[string]chatstore.AckRequirement, ids []string, roll chatAckRoll) (map[string]ChatAckView, error) {
	out := map[string]ChatAckView{}
	if len(ids) == 0 {
		return out, nil
	}
	mine, err := c.Store.MyAcks(ctx, p.TenantID, conv.ID, ids, p.SubjectID)
	if err != nil {
		return nil, ErrChatAckUnavailable
	}
	for _, id := range ids {
		req := reqs[id]
		view := ChatAckView{PostID: id, Required: true}
		inAudience := roll.members[p.SubjectID] && p.SubjectID != req.RequiredByID
		at, done := mine[id]
		if done {
			view.Acknowledged, view.AcknowledgedAt = true, &at
		}
		view.CanAcknowledge = inAudience && !done
		if p.SubjectID == req.RequiredByID || roll.manages(conv, p.SubjectID) {
			view.CanManage = true
			acks, err := c.Store.ListAcks(ctx, p.TenantID, conv.ID, id)
			if err != nil {
				return nil, ErrChatAckUnavailable
			}
			acked := map[string]bool{}
			for _, a := range acks {
				acked[a.SubjectID] = true
			}
			audience := roll.audience(req.RequiredByID)
			view.Total = len(audience)
			for _, who := range audience {
				switch {
				case acked[who]:
					view.AckCount++
				case len(view.Pending) < chatAckMaxPending:
					view.Pending = append(view.Pending, who)
				default:
					view.PendingMore++
				}
			}
		}
		out[id] = view
	}
	return out, nil
}

// Reply reads the acknowledgement state of the given posts. A post that does
// not require acknowledgement, or that the reader cannot open, is left out.
func (c *ChatPostAcks) Reply(ctx context.Context, human *trust.Principal, conversation string, postIDs []string) (ChatAckReply, error) {
	p, err := chatAckPrincipal(human)
	if err != nil {
		return ChatAckReply{}, err
	}
	if !c.ready() {
		return ChatAckReply{}, ErrChatAckUnavailable
	}
	if strings.TrimSpace(conversation) == "" || len(postIDs) == 0 || len(postIDs) > chatAckMaxViews {
		return ChatAckReply{}, ErrChatAckInvalid
	}
	conv, err := c.Chat.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: conversation})
	if err != nil {
		return ChatAckReply{}, chatAckChatError(err)
	}
	reqs, err := c.Store.AckRequirements(ctx, p.TenantID, conversation, postIDs)
	if err != nil {
		return ChatAckReply{}, ErrChatAckUnavailable
	}
	reply := ChatAckReply{Views: map[string]ChatAckView{}}
	if conv.OwnerID == p.SubjectID && chatAckChannel(conv) && !conv.Archived {
		reply.CanRequire = true
	}
	if len(reqs) == 0 && (reply.CanRequire || !chatAckChannel(conv) || conv.Archived) {
		return reply, nil
	}
	roll, err := c.roll(ctx, p, conv)
	if err != nil {
		return ChatAckReply{}, err
	}
	if roll.manages(conv, p.SubjectID) && chatAckChannel(conv) && !conv.Archived {
		reply.CanRequire = true
	}
	var ids []string
	for _, id := range postIDs {
		if _, seen := reply.Views[id]; seen {
			continue
		}
		if _, ok := reqs[id]; !ok {
			continue
		}
		// A required post the reader may not open is not described to them.
		if _, post, err := c.Chat.ReadAuthorizedReference(ctx, p, p.TenantID, conversation, id); err != nil || post == nil || post.Deleted || post.ID != id || post.ConversationID != conversation {
			delete(reqs, id)
			continue
		}
		reply.Views[id] = ChatAckView{}
		ids = append(ids, id)
	}
	views, err := c.views(ctx, p, conv, reqs, ids, roll)
	if err != nil {
		return ChatAckReply{}, err
	}
	reply.Views = views
	return reply, nil
}

// post reads a post the reader may open in a conversation they may open.
func (c *ChatPostAcks) post(ctx context.Context, p chat.Principal, conversation, postID string) (chat.Conversation, chat.Post, error) {
	if strings.TrimSpace(conversation) == "" || strings.TrimSpace(postID) == "" {
		return chat.Conversation{}, chat.Post{}, ErrChatAckInvalid
	}
	conv, post, err := c.Chat.ReadAuthorizedReference(ctx, p, p.TenantID, conversation, postID)
	if err != nil {
		return chat.Conversation{}, chat.Post{}, chatAckChatError(err)
	}
	if post == nil || post.ID != postID || post.ConversationID != conversation || post.Deleted {
		return chat.Conversation{}, chat.Post{}, ErrChatAckNotFound
	}
	return conv, *post, nil
}

// Require marks a post as needing acknowledgement. Only the sender of the post
// may, and only while they own or manage the channel it is in.
func (c *ChatPostAcks) Require(ctx context.Context, human *trust.Principal, conversation, postID string) (ChatAckView, error) {
	p, err := chatAckPrincipal(human)
	if err != nil {
		return ChatAckView{}, err
	}
	if !c.ready() {
		return ChatAckView{}, ErrChatAckUnavailable
	}
	conv, post, err := c.post(ctx, p, conversation, postID)
	if err != nil {
		return ChatAckView{}, err
	}
	if post.AuthorID != p.SubjectID || (post.AuthorHomeTenantID != "" && post.AuthorHomeTenantID != p.TenantID) {
		return ChatAckView{}, ErrChatAckDenied
	}
	roll, err := c.roll(ctx, p, conv)
	if err != nil {
		return ChatAckView{}, err
	}
	if !roll.manages(conv, p.SubjectID) {
		return ChatAckView{}, ErrChatAckDenied
	}
	// A system card or a note about a membership is not a policy.
	if !chatAckChannel(conv) || conv.Archived || strings.HasPrefix(post.Body, "⁣") || strings.TrimSpace(post.Body) == "" {
		return ChatAckView{}, ErrChatAckInvalid
	}
	req := chatstore.AckRequirement{TenantID: p.TenantID, ConversationID: conversation, PostID: postID, RequiredByID: p.SubjectID, PostRevision: post.Revision, RequiredAt: time.Now().UTC()}
	if _, err := c.Store.RequireAck(ctx, req); err != nil {
		return ChatAckView{}, ErrChatAckUnavailable
	}
	// Whatever was already marked is the requirement that stands.
	stored, err := c.Store.AckRequirements(ctx, p.TenantID, conversation, []string{postID})
	if err != nil || stored[postID].PostID == "" {
		return ChatAckView{}, ErrChatAckUnavailable
	}
	views, err := c.views(ctx, p, conv, stored, []string{postID}, roll)
	if err != nil {
		return ChatAckView{}, err
	}
	return views[postID], nil
}

// Acknowledge records that the person has read and understood the post. It is
// kept once: a second call returns the same answer and writes nothing.
func (c *ChatPostAcks) Acknowledge(ctx context.Context, human *trust.Principal, conversation, postID string) (ChatAckView, error) {
	p, err := chatAckPrincipal(human)
	if err != nil {
		return ChatAckView{}, err
	}
	if !c.ready() {
		return ChatAckView{}, ErrChatAckUnavailable
	}
	conv, post, err := c.post(ctx, p, conversation, postID)
	if err != nil {
		return ChatAckView{}, err
	}
	reqs, err := c.Store.AckRequirements(ctx, p.TenantID, conversation, []string{postID})
	if err != nil {
		return ChatAckView{}, ErrChatAckUnavailable
	}
	req, ok := reqs[postID]
	if !ok {
		return ChatAckView{}, ErrChatAckNotFound
	}
	roll, err := c.roll(ctx, p, conv)
	if err != nil {
		return ChatAckView{}, err
	}
	if !roll.members[p.SubjectID] || p.SubjectID == req.RequiredByID {
		return ChatAckView{}, ErrChatAckDenied
	}
	ack := chatstore.Ack{TenantID: p.TenantID, ConversationID: conversation, PostID: postID, SubjectID: p.SubjectID, PostRevision: post.Revision, AcknowledgedAt: time.Now().UTC()}
	if _, err := c.Store.RecordAck(ctx, ack); err != nil {
		if errors.Is(err, chat.ErrNotFound) {
			return ChatAckView{}, ErrChatAckNotFound
		}
		return ChatAckView{}, ErrChatAckUnavailable
	}
	views, err := c.views(ctx, p, conv, reqs, []string{postID}, roll)
	if err != nil {
		return ChatAckView{}, err
	}
	return views[postID], nil
}

type chatAckChatSet struct {
	chat.ConversationService
	links interface {
		ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
	}
}

func (s chatAckChatSet) ReadAuthorizedReference(ctx context.Context, who chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	return s.links.ReadAuthorizedReference(ctx, who, tenant, conversation, post)
}

// NewChatPostAcks composes acknowledgements over the served conversation
// service and Chat store, or returns nil when either is not composed.
func NewChatPostAcks(links interface {
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
}, service chat.ConversationService, store *chatstore.Store) *ChatPostAcks {
	if links == nil || service == nil || store == nil {
		return nil
	}
	return &ChatPostAcks{Chat: chatAckChatSet{ConversationService: service, links: links}, Store: store}
}
