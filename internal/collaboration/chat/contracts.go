// Package chat contains the transport-neutral contracts for native company chat.
package chat

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidArgument  = errors.New("chat: invalid argument")
	ErrUnauthenticated  = errors.New("chat: unauthenticated")
	ErrPermissionDenied = errors.New("chat: permission denied")
	ErrNotFound         = errors.New("chat: not found")
	ErrAlreadyExists    = errors.New("chat: already exists")
	ErrConflict         = errors.New("chat: conflict")
	ErrUnavailable      = errors.New("chat: unavailable")
)

type Principal struct {
	TenantID, SubjectID   string
	Roles, Qualifications []string
}

type ConversationKind string

const (
	PublicChannel  ConversationKind = "PUBLIC_CHANNEL"
	PrivateChannel ConversationKind = "PRIVATE_CHANNEL"
	Direct         ConversationKind = "DIRECT"
	Group          ConversationKind = "GROUP"
)

type Conversation struct {
	ID, TenantID  string
	Kind          ConversationKind
	Name, OwnerID string
	Revision      uint64
	Archived      bool
	// Joined reports an active membership for the listing caller. It is set only
	// where the question has an answer — a discoverable listing, which mixes
	// rooms the caller is in with public channels it could join.
	Joined bool
	// MemberCount is how many active memberships the conversation holds, and
	// LastActivityAt is when its newest untombstoned post was created — nil when
	// it has none. Both are derived by the store as part of the same query that
	// reads the row, so a channel directory can describe a room without a
	// follow-up call per row. Neither is caller-settable.
	MemberCount    uint32
	LastActivityAt *time.Time
}

type MemberRef struct {
	TenantID, SubjectID string
}

type MembershipRole string

const (
	Member  MembershipRole = "MEMBER"
	Manager MembershipRole = "MANAGER"
)

type ReadHistoryFrom string

const (
	NoHistory   ReadHistoryFrom = "NONE"
	FromJoin    ReadHistoryFrom = "FROM_JOIN"
	FullHistory ReadHistoryFrom = "FULL_HISTORY"
)

type Membership struct {
	ConversationID, TenantID, HomeTenantID, SubjectID string
	Role                                              MembershipRole
	JoinedAt, LeftAt                                  *time.Time
	HistoryVisibility                                 ReadHistoryFrom
	Revision                                          uint64
}

type Post struct {
	ID, ConversationID, TenantID, AuthorID, Body string
	AuthorHomeTenantID                           string
	Sequence, Revision                           uint64
	ParentID                                     string
	Deleted                                      bool
	CreatedAt                                    time.Time
	References                                   []Reference
	SourceAttribution                            *SourceAttribution
}
type Reaction struct {
	ConversationID, PostID, TenantID, SubjectID, Emoji string
	HomeTenantID                                       string
	CreatedAt                                          time.Time
}
type Pin struct {
	ConversationID, PostID, TenantID, PinnedBy string
	PinnedByHomeTenantID                       string
	Revision                                   uint64
	CreatedAt                                  time.Time
}
type ReadState struct {
	ConversationID, TenantID, SubjectID string
	HomeTenantID                        string
	LastReadSequence, Revision          uint64
}
type NotificationPreferences struct {
	ConversationID, TenantID, SubjectID string
	HomeTenantID                        string
	Muted, MentionsOnly                 bool
	Revision                            uint64
}

type Page struct {
	Cursor   string
	PageSize uint32
}
type CreateConversationRequest struct {
	Principal Principal
	TenantID  string
	// ConversationID is supplied by the core route coordinator when a route
	// reservation must precede chat persistence. Empty keeps legacy callers
	// compatible and lets the service generate an ID.
	ConversationID                string
	Kind                          ConversationKind
	Name, OwnerID, IdempotencyKey string
	Members                       []MemberRef
	// MemberIDs remains for source compatibility with the initial local service.
	// New callers should use Members so home tenants remain explicit.
	MemberIDs []string
}
type ListConversationsRequest struct {
	Principal Principal
	TenantID  string
	Page      Page
	// IncludeDiscoverable widens the listing from "the rooms I am in" to "the
	// rooms I am in, plus the public channels in this tenant I could join".
	// Every added row is still filtered by the discovery policy, so it is a
	// scope option and not a way around visibility.
	IncludeDiscoverable bool
}

// ConversationScope is the store-level projection of the listing scope. It is a
// type rather than a bare bool so widening the scope later does not change every
// Store implementation's signature again.
type ConversationScope struct{ IncludeDiscoverable bool }
type GetConversationRequest struct {
	Principal                Principal
	TenantID, ConversationID string
}
type UpdateConversationRequest struct {
	Principal        Principal
	Conversation     Conversation
	ExpectedRevision uint64
}
type ListMembershipsRequest struct {
	Principal                Principal
	TenantID, ConversationID string
	Page                     Page
}
type AddMembershipRequest struct {
	Principal  Principal
	Membership Membership
}
type RemoveMembershipRequest struct {
	Principal                                         Principal
	TenantID, HomeTenantID, ConversationID, SubjectID string
	ExpectedRevision                                  uint64
}
type SendPostRequest struct {
	Principal                                                Principal
	TenantID, ConversationID, Body, ParentID, IdempotencyKey string
	References                                               []Reference
	SourceAttribution                                        *SourceAttribution
}
type ListPostsRequest struct {
	Principal                Principal
	TenantID, ConversationID string
	AfterSequence            uint64
	Page                     Page
	// Descending pages backward: newest first, oldest last. A client opening a
	// conversation wants its newest page, not the first page it ever had, and
	// without this it had to walk the whole history forward to find the end.
	Descending bool
	// BeforeSequence is the exclusive upper edge of a backward page. Zero starts
	// at the newest post. It is ignored on a forward page.
	BeforeSequence uint64
}

// PostWindow is the store-level projection of one page's direction and far edge.
type PostWindow struct {
	Descending     bool
	BeforeSequence uint64
}
type EditPostRequest struct {
	Principal                              Principal
	PostID, TenantID, ConversationID, Body string
	ExpectedRevision                       uint64
}
type DeletePostRequest struct {
	Principal                        Principal
	TenantID, ConversationID, PostID string
	ExpectedRevision                 uint64
}
type SearchRequest struct {
	Principal                                 Principal
	TenantID, Query, ConversationID, AuthorID string
	Page                                      Page
}
type UpdateReadStateRequest struct {
	Principal        Principal
	ReadState        ReadState
	ExpectedRevision uint64
}
type GetReadStateRequest struct {
	Principal                Principal
	TenantID, ConversationID string
}
type UpdatePreferencesRequest struct {
	Principal        Principal
	Preferences      NotificationPreferences
	ExpectedRevision uint64
}
type GetPreferencesRequest struct {
	Principal                Principal
	TenantID, ConversationID string
}
type AddReactionRequest struct {
	Principal Principal
	Reaction  Reaction
}
type RemoveReactionRequest struct {
	Principal                               Principal
	TenantID, ConversationID, PostID, Emoji string
}
type ListReactionsRequest struct {
	Principal                        Principal
	TenantID, ConversationID, PostID string
	Page                             Page
}
type ListReactionsResponse struct {
	Reactions  []Reaction
	NextCursor string
}
type PinPostRequest struct {
	Principal Principal
	Pin       Pin
}
type UnpinPostRequest struct {
	Principal                        Principal
	TenantID, ConversationID, PostID string
	ExpectedRevision                 uint64
}
type ListPinsRequest struct {
	Principal                Principal
	TenantID, ConversationID string
}
type WatchConversationRequest struct {
	Principal                Principal
	TenantID, ConversationID string
	AfterSequence            uint64
	ResumeCursor             string
}

type ListConversationsResponse struct {
	Conversations []Conversation
	NextCursor    string
}
type ListMembershipsResponse struct {
	Memberships []Membership
	NextCursor  string
}
type ListPostsResponse struct {
	Posts      []Post
	NextCursor string
}
type SearchResult struct{ Post Post }
type SearchResponse struct {
	Results    []SearchResult
	NextCursor string
}
type ConversationEventKind string

const (
	PostCreated         ConversationEventKind = "POST_CREATED"
	PostEdited          ConversationEventKind = "POST_EDITED"
	PostDeleted         ConversationEventKind = "POST_DELETED"
	MembershipChanged   ConversationEventKind = "MEMBERSHIP_CHANGED"
	ConversationUpdated ConversationEventKind = "CONVERSATION_UPDATED"
	ReactionChanged     ConversationEventKind = "REACTION_CHANGED"
	PinChanged          ConversationEventKind = "PIN_CHANGED"
)

type ConversationEvent struct {
	Kind               ConversationEventKind
	Sequence, Revision uint64
	Post               *Post
	Membership         *Membership
	Conversation       *Conversation
	Reaction           *Reaction
	Pin                *Pin
	Removed            bool
}
type WatchEvent struct {
	Event        ConversationEvent
	ResumeCursor string
}

type ChatCounts struct {
	TenantID, ConversationID, SubjectID, HomeTenantID string
	UnreadCount, MentionCount                         uint64
}
type ThreadFollow struct {
	TenantID, ConversationID, SubjectID, HomeTenantID, RootPostID string
	Followed                                                      bool
	Revision                                                      uint64
}
type SidebarState struct {
	TenantID, SubjectID, HomeTenantID, LayoutJSON string
	Revision                                      uint64
}
type QuietHours struct {
	TenantID, SubjectID, HomeTenantID, Timezone string
	StartMinute, EndMinute                      uint32
	Enabled                                     bool
	Revision                                    uint64
}

// ResolveReferencesRequest and response are the canonical RPC projection of
// the reference directory lookup; display values are snapshots, never keys.
type ResolveReferencesRequest struct {
	Principal                Principal
	TenantID, ConversationID string
	Kind                     ReferenceKind
	Query                    string
}
type ResolveReferencesResponse struct{ Candidates []ReferenceCandidate }
type CreateShareLinkRequest struct {
	Principal                        Principal
	TenantID, ConversationID, PostID string
}
type CreateShareLinkResponse struct{ Link ConversationLink }
type ResolveShareLinkRequest struct {
	Principal Principal
	Token     string
}
type ResolveShareLinkResponse struct {
	Conversation Conversation
	Post         *Post
}
type ForwardPostRPCRequest struct {
	Principal                                          Principal
	SourceTenantID, SourceConversationID, SourcePostID string
	DestinationTenantID, DestinationConversationID     string
	IdempotencyKey                                     string
	SourceAttribution                                  *SourceAttribution
}
type ForwardPostResponse struct{ Post Post }

// ErrorReportingWatcher is the optional extension a chat owner implements when
// its watch can fail after the subscription opened.
//
// WatchConversation alone cannot say why a stream stopped: a closed event
// channel is all the caller sees, so a failed durable read, a revoked
// membership and a backpressure close were all indistinguishable from a healthy
// end of stream. A transport that finds this interface reports the terminal
// cause instead of a clean end. The error channel carries at most one value and
// is closed with the event channel.
type ErrorReportingWatcher interface {
	WatchConversationWithErrors(context.Context, WatchConversationRequest) (<-chan WatchEvent, <-chan error, error)
}

// ConversationService is the application boundary implemented by chat owners.
// WatchConversation returns a receive-only event stream and a cancellation function.
type ConversationService interface {
	CreateConversation(context.Context, CreateConversationRequest) (Conversation, error)
	ListConversations(context.Context, ListConversationsRequest) (ListConversationsResponse, error)
	GetConversation(context.Context, GetConversationRequest) (Conversation, error)
	UpdateConversation(context.Context, UpdateConversationRequest) (Conversation, error)
	ListMemberships(context.Context, ListMembershipsRequest) (ListMembershipsResponse, error)
	AddMembership(context.Context, AddMembershipRequest) (Membership, error)
	RemoveMembership(context.Context, RemoveMembershipRequest) (Membership, error)
	SendPost(context.Context, SendPostRequest) (Post, error)
	ListPosts(context.Context, ListPostsRequest) (ListPostsResponse, error)
	EditPost(context.Context, EditPostRequest) (Post, error)
	DeletePost(context.Context, DeletePostRequest) (Post, error)
	Search(context.Context, SearchRequest) (SearchResponse, error)
	GetReadState(context.Context, GetReadStateRequest) (ReadState, error)
	UpdateReadState(context.Context, UpdateReadStateRequest) (ReadState, error)
	GetPreferences(context.Context, GetPreferencesRequest) (NotificationPreferences, error)
	UpdatePreferences(context.Context, UpdatePreferencesRequest) (NotificationPreferences, error)
	AddReaction(context.Context, AddReactionRequest) (Reaction, error)
	RemoveReaction(context.Context, RemoveReactionRequest) error
	ListReactions(context.Context, ListReactionsRequest) (ListReactionsResponse, error)
	PinPost(context.Context, PinPostRequest) (Pin, error)
	UnpinPost(context.Context, UnpinPostRequest) error
	ListPins(context.Context, ListPinsRequest) ([]Pin, error)
	WatchConversation(context.Context, WatchConversationRequest) (<-chan WatchEvent, error)
}
