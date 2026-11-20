package chat

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// Issue 3804. What the agents of a channel remember.
//
// A person asks the channel to remember one message. That makes a PROPOSED
// item bound to the message it came from, readable by the channel's members
// only, kept for 90 days once approved. Nothing reaches an agent until a
// channel manager approves it. The item is retired the moment its source
// message is deleted or edited, because the store reads the source on every
// use; anyone who may forget it can end it earlier, and ending it erases its
// words. A memory never outlives the access it was given: a conversation that
// is not a channel (a direct or group message) has none.

const (
	// MemoryTextMax is the longest fact one item holds.
	MemoryTextMax = 500
	// MemoryDefaultTTLDays is how long an approved item serves.
	MemoryDefaultTTLDays = 90
	// MemoryListLimit is how many items one list returns.
	MemoryListLimit = 100
	// MemoryServeLimit is how many items an agent run is given.
	MemoryServeLimit = 20
	// MemoryAudienceChannel is the only audience an item can have.
	MemoryAudienceChannel = "CHANNEL_MEMBERS"
	// MemoryClassification is the sensitivity every item carries; it is the
	// class of the channel's own messages, never anything wider.
	MemoryClassification = "INTERNAL"
)

// MemoryStatus is where an item stands.
type MemoryStatus string

const (
	MemoryProposed  MemoryStatus = "PROPOSED"
	MemoryApproved  MemoryStatus = "APPROVED"
	MemoryForgotten MemoryStatus = "FORGOTTEN"
)

// MemorySource says whether the message an item came from still stands as it
// was when the item was proposed.
type MemorySource string

const (
	MemorySourceLive    MemorySource = "LIVE"
	MemorySourceRemoved MemorySource = "REMOVED"
	MemorySourceEdited  MemorySource = "EDITED"
)

// ChannelMemory is one remembered item.
type ChannelMemory struct {
	ID, TenantID, ConversationID string
	Text                         string
	SourcePostID                 string
	SourceRevision               uint64
	SourceSequence               uint64
	ProposerHomeTenantID         string
	ProposerID                   string
	Audience, Classification     string
	TTLDays                      int
	Status                       MemoryStatus
	CreatedAt                    time.Time
	ApprovedByHomeTenantID       string
	ApprovedByID                 string
	ApprovedAt, ExpiresAt        time.Time
	ForgottenAt                  time.Time
	// Source is read from the message at the moment of the read.
	Source MemorySource
}

// MemoryStore keeps items. Every method is scoped by the conversation's tenant;
// the service has already decided that the caller may act.
type MemoryStore interface {
	// InsertMemory stores a proposal. When a live (not forgotten) item already
	// exists for the same source message it returns that one and stores nothing.
	InsertMemory(ctx context.Context, m ChannelMemory) (ChannelMemory, error)
	GetMemory(ctx context.Context, tenant, conversation, id string) (ChannelMemory, error)
	// ListMemory returns the conversation's items that are not forgotten, newest
	// first, each with its source read as of now.
	ListMemory(ctx context.Context, tenant, conversation string, limit int) ([]ChannelMemory, error)
	// ApproveMemory moves a PROPOSED item whose source still stands to
	// APPROVED and sets its expiry. It returns ErrNotFound for an unknown item
	// and ErrConflict for one that is not proposed or whose source changed.
	ApproveMemory(ctx context.Context, tenant, conversation, id, homeTenant, approver string, at time.Time) (ChannelMemory, error)
	// ForgetMemory ends a PROPOSED or APPROVED item and erases its words. It
	// returns ErrNotFound for an unknown or already forgotten item.
	ForgetMemory(ctx context.Context, tenant, conversation, id, homeTenant, actor string, at time.Time) error
	// ServingMemory returns the items an agent may read now: approved, not
	// expired, with their source message standing exactly as proposed.
	ServingMemory(ctx context.Context, tenant, conversation string, at time.Time, limit int) ([]ChannelMemory, error)
}

// MemoryView is one item as one reader may act on it.
type MemoryView struct {
	ChannelMemory
	// CanApprove is true for a manager looking at a proposed item whose source stands.
	CanApprove bool
	// CanForget is true for a manager and for the person who proposed the item.
	CanForget bool
}

// MemoryList is what one reader sees of a channel's memory.
type MemoryList struct {
	Items []MemoryView
	// CanRemember says the reader may ask the channel to remember a message.
	CanRemember bool
	// IsManager says the reader manages the channel.
	IsManager bool
}

// MemoryService proposes, lists, approves and forgets items over the Chat
// service, so every step is held to the same membership and channel rules as a
// message.
type MemoryService struct {
	Chat  *Service
	Store MemoryStore
}

// MemoryProposeRequest asks the channel to remember one message.
type MemoryProposeRequest struct {
	Principal                Principal
	TenantID, ConversationID string
	SourcePostID             string
	// TTLDays is how long the item serves once approved. Zero is the default of
	// 90 days; a value outside 1 to 365 is refused.
	TTLDays int
}

// MemoryActRequest approves or forgets one item.
type MemoryActRequest struct {
	Principal                Principal
	TenantID, ConversationID string
	ID                       string
}

// NormalizeMemoryText folds a message into the one line an item holds.
func NormalizeMemoryText(text string) (string, bool) {
	if !utf8.ValidString(text) || strings.Contains(text, Chatcmd002BodyMarker) {
		return "", false
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || utf8.RuneCountInString(text) > MemoryTextMax {
		return "", false
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return text, true
}

// MemoryID is the identity of a new proposal. A forgotten item keeps its row,
// so a message remembered again gets a fresh identity; the store hands back the
// live item for a message that already has one.
func MemoryID() string {
	return uuid.NewString()
}

func (s *MemoryService) usable(p Principal, tenant, conversation string) bool {
	return s != nil && s.Chat != nil && s.Store != nil && p.SubjectID != "" && p.TenantID != "" && tenant != "" && conversation != ""
}

func memoryKindAllowed(c Conversation) bool {
	return (c.Kind == PublicChannel || c.Kind == PrivateChannel) && !c.Archived
}

// authorize returns the conversation and the caller's current membership, or a
// refusal. A caller who is not a current member learns nothing.
func (s *MemoryService) authorize(ctx context.Context, p Principal, tenant, conversation string, write bool) (Conversation, Membership, error) {
	c, err := s.Chat.store.GetConversation(ctx, tenant, conversation)
	if err != nil {
		return Conversation{}, Membership{}, err
	}
	m, err := s.Chat.store.GetMembership(ctx, tenant, conversation, p.TenantID, p.SubjectID)
	if err != nil || !currentReferenceMembership(m, tenant, conversation, p.TenantID, p.SubjectID) {
		return Conversation{}, Membership{}, ErrPermissionDenied
	}
	action := chatpolicy.ActionRead
	if write {
		action = chatpolicy.ActionPost
	}
	if err := s.Chat.authorize(ctx, p, c, action); err != nil {
		return Conversation{}, Membership{}, err
	}
	return c, m, nil
}

func memoryIsMine(item ChannelMemory, p Principal) bool {
	return item.ProposerHomeTenantID == p.TenantID && item.ProposerID == p.SubjectID
}

// Propose asks the channel to remember a message. The person must manage the
// channel or have written the message; the message must be one they can read,
// not a card, and short enough to hold. Authorization comes first: nothing is
// stored for a caller who may not ask.
func (s *MemoryService) Propose(ctx context.Context, r MemoryProposeRequest) (ChannelMemory, error) {
	if !s.usable(r.Principal, r.TenantID, r.ConversationID) || strings.TrimSpace(r.SourcePostID) == "" || r.TTLDays < 0 || r.TTLDays > 365 {
		return ChannelMemory{}, ErrInvalidArgument
	}
	c, m, err := s.authorize(ctx, r.Principal, r.TenantID, r.ConversationID, true)
	if err != nil {
		return ChannelMemory{}, err
	}
	if !memoryKindAllowed(c) {
		return ChannelMemory{}, ErrInvalidArgument
	}
	post, err := s.Chat.store.GetPost(ctx, r.TenantID, r.ConversationID, r.SourcePostID)
	if err != nil || post.Deleted || !s.Chat.postVisibleTo(ctx, r.Principal, c, post) {
		return ChannelMemory{}, ErrNotFound
	}
	mine := post.AuthorHomeTenantID == r.Principal.TenantID && post.AuthorID == r.Principal.SubjectID
	if post.AuthorHomeTenantID == "" {
		mine = post.AuthorID == r.Principal.SubjectID && r.Principal.TenantID == r.TenantID
	}
	if m.Role != Manager && !mine {
		return ChannelMemory{}, ErrPermissionDenied
	}
	text, ok := NormalizeMemoryText(post.Body)
	if !ok {
		return ChannelMemory{}, ErrInvalidArgument
	}
	if err := s.Chat.checkContent(ctx, ContentInput{Principal: r.Principal, Conversation: c, Body: text}); err != nil {
		return ChannelMemory{}, err
	}
	ttl := r.TTLDays
	if ttl == 0 {
		ttl = MemoryDefaultTTLDays
	}
	stored, err := s.Store.InsertMemory(ctx, ChannelMemory{ID: MemoryID(), TenantID: r.TenantID, ConversationID: r.ConversationID,
		Text: text, SourcePostID: post.ID, SourceRevision: post.Revision, SourceSequence: post.Sequence,
		ProposerHomeTenantID: r.Principal.TenantID, ProposerID: r.Principal.SubjectID, Audience: MemoryAudienceChannel, Classification: MemoryClassification,
		TTLDays: ttl, Status: MemoryProposed, CreatedAt: s.Chat.now()})
	if err != nil {
		return ChannelMemory{}, err
	}
	if stored.Source == "" {
		stored.Source = MemorySourceLive
	}
	return stored, nil
}

func (s *MemoryService) view(item ChannelMemory, p Principal, manager bool) MemoryView {
	v := MemoryView{ChannelMemory: item}
	v.CanForget = manager || memoryIsMine(item, p)
	v.CanApprove = manager && item.Status == MemoryProposed && item.Source == MemorySourceLive
	return v
}

// List returns what the reader may see of the channel's memory, newest first.
// Approved items are the channel's to read; a proposal is seen by the person
// who made it and by managers. An item whose source message was deleted shows
// no words to anyone, and only to those who may forget it, so it can be
// cleared. An item from a message the reader cannot read is not shown.
func (s *MemoryService) List(ctx context.Context, p Principal, tenant, conversation string) (MemoryList, error) {
	if !s.usable(p, tenant, conversation) {
		return MemoryList{}, ErrInvalidArgument
	}
	c, m, err := s.authorize(ctx, p, tenant, conversation, false)
	if err != nil {
		return MemoryList{}, err
	}
	out := MemoryList{Items: []MemoryView{}}
	if !memoryKindAllowed(c) {
		// A direct message or group has no channel memory; the page then offers none.
		return MemoryList{}, ErrInvalidArgument
	}
	manager := m.Role == Manager
	out.IsManager = manager
	_, _, werr := s.authorize(ctx, p, tenant, conversation, true)
	out.CanRemember = werr == nil
	items, err := s.Store.ListMemory(ctx, tenant, conversation, MemoryListLimit)
	if err != nil {
		return MemoryList{}, err
	}
	for _, item := range items {
		v := s.view(item, p, manager)
		if item.Status == MemoryProposed && !manager && !memoryIsMine(item, p) {
			continue
		}
		if item.Source != MemorySourceLive {
			// The words of a message that was deleted or changed are not shown.
			if !v.CanForget {
				continue
			}
			v.Text = ""
			out.Items = append(out.Items, v)
			continue
		}
		post, perr := s.Chat.store.GetPost(ctx, tenant, conversation, item.SourcePostID)
		if perr != nil || post.Deleted || !s.Chat.postVisibleTo(ctx, p, c, post) {
			continue
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}

// Approve lets a channel manager put a proposed item to work.
func (s *MemoryService) Approve(ctx context.Context, r MemoryActRequest) (ChannelMemory, error) {
	if !s.usable(r.Principal, r.TenantID, r.ConversationID) || strings.TrimSpace(r.ID) == "" {
		return ChannelMemory{}, ErrInvalidArgument
	}
	c, m, err := s.authorize(ctx, r.Principal, r.TenantID, r.ConversationID, true)
	if err != nil {
		return ChannelMemory{}, err
	}
	if !memoryKindAllowed(c) {
		return ChannelMemory{}, ErrInvalidArgument
	}
	if m.Role != Manager {
		return ChannelMemory{}, ErrPermissionDenied
	}
	return s.Store.ApproveMemory(ctx, r.TenantID, r.ConversationID, r.ID, r.Principal.TenantID, r.Principal.SubjectID, s.Chat.now())
}

// Forget ends an item: a manager may forget any, the person who proposed it
// may forget their own. The words are erased.
func (s *MemoryService) Forget(ctx context.Context, r MemoryActRequest) error {
	if !s.usable(r.Principal, r.TenantID, r.ConversationID) || strings.TrimSpace(r.ID) == "" {
		return ErrInvalidArgument
	}
	_, m, err := s.authorize(ctx, r.Principal, r.TenantID, r.ConversationID, false)
	if err != nil {
		return err
	}
	item, err := s.Store.GetMemory(ctx, r.TenantID, r.ConversationID, r.ID)
	if err != nil {
		return err
	}
	if item.Status == MemoryForgotten {
		return ErrNotFound
	}
	if m.Role != Manager && !memoryIsMine(item, r.Principal) {
		return ErrPermissionDenied
	}
	return s.Store.ForgetMemory(ctx, r.TenantID, r.ConversationID, r.ID, r.Principal.TenantID, r.Principal.SubjectID, s.Chat.now())
}

// ServingNotes is what an agent run in this channel may be told: the words of
// every approved, unexpired item whose source message still stands, newest
// first. It makes no decision about the run itself; the run's own admission has
// already established that the asker is a current member. A conversation that
// is not a live channel has none, and neither does an unreadable store: the
// agent then answers from the thread alone.
func (s *MemoryService) ServingNotes(ctx context.Context, tenant, conversation string) ([]string, error) {
	if s == nil || s.Chat == nil || s.Store == nil || tenant == "" || conversation == "" {
		return nil, ErrInvalidArgument
	}
	c, err := s.Chat.store.GetConversation(ctx, tenant, conversation)
	if err != nil {
		return nil, err
	}
	if !memoryKindAllowed(c) {
		return nil, nil
	}
	items, err := s.Store.ServingMemory(ctx, tenant, conversation, s.Chat.now(), MemoryServeLimit)
	if err != nil {
		return nil, err
	}
	notes := make([]string, 0, len(items))
	for _, item := range items {
		if item.Status == MemoryApproved && strings.TrimSpace(item.Text) != "" {
			notes = append(notes, item.Text)
		}
	}
	return notes, nil
}
