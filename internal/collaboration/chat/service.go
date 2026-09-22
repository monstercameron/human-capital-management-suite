package chat

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// Clock is injected so authorization and mutation tests are deterministic.
type Clock func() time.Time

// Authority resolves current identity, membership, grants, and policy inputs.
// The authenticated transport remains the source of Principal; implementations
// must not infer authority from display fields or stale role claims.
type Authority interface {
	Authorize(context.Context, Principal, Conversation, chatpolicy.Action, time.Time) (chatpolicy.Input, error)
}

// Store is the durable chat boundary. Implementations must make SendPost and
// other mutation methods transactional, including their outbox records.
// The service performs all caller and policy checks before invoking a mutator.
type Store interface {
	CreateConversation(context.Context, Conversation, []Membership, string) (Conversation, error)
	ListConversations(context.Context, Principal, string, Page, ConversationScope) (ListConversationsResponse, error)
	GetConversation(context.Context, string, string) (Conversation, error)
	UpdateConversation(context.Context, Conversation, uint64) (Conversation, error)
	ListMemberships(context.Context, string, string, Page) (ListMembershipsResponse, error)
	GetMembership(context.Context, string, string, string, string) (Membership, error)
	GetPost(context.Context, string, string, string) (Post, error)
	PutMembership(context.Context, Principal, Membership) (Membership, error)
	RemoveMembership(context.Context, Principal, string, string, string, string, uint64) (Membership, error)
	SendPost(context.Context, SendPostRequest, Post) (Post, error)
	ListPosts(context.Context, Principal, string, string, uint64, Page, PostWindow) (ListPostsResponse, error)
	EditPost(context.Context, EditPostRequest) (Post, error)
	DeletePost(context.Context, DeletePostRequest) (Post, error)
	Search(context.Context, SearchRequest) (SearchResponse, error)
	GetReadState(context.Context, string, string, string, string) (ReadState, error)
	PutReadState(context.Context, ReadState, uint64) (ReadState, error)
	GetPreferences(context.Context, string, string, string, string) (NotificationPreferences, error)
	PutPreferences(context.Context, NotificationPreferences, uint64) (NotificationPreferences, error)
	PutReaction(context.Context, Reaction) (Reaction, error)
	RemoveReaction(context.Context, string, string, string, string, string, string) error
	ListReactions(context.Context, Principal, string, string, string, Page) (ListReactionsResponse, error)
	PutPin(context.Context, Pin) (Pin, error)
	RemovePin(context.Context, Principal, string, string, string, string, uint64) error
	ListPins(context.Context, string, string) ([]Pin, error)
	Watch(context.Context, WatchConversationRequest) (<-chan WatchEvent, error)
}

// Service is the application implementation shared by RPC and HTTP
// transports. It intentionally contains no process-wide mutable state.
type Service struct {
	store              Store
	clock              Clock
	authority          Authority
	referenceDirectory ReferenceDirectory
	disclosureChecker  DisclosureChecker
	linkCodec          LinkCodec
	mediaDirectory     MediaDirectory
}

// SetAuthority installs the current-authority resolver used by subsequent
// reads and writes. It is intended for composition roots and tests.
func (s *Service) SetAuthority(a Authority) { s.authority = a }

func NewService(store Store, clock Clock) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{store: store, clock: clock}
}

func (s *Service) now() time.Time { return s.clock().UTC() }

// ValidateCreate performs the complete request and principal validation that
// precedes any durable side effect. Route coordinators call this before
// reserving a core route; CreateConversation repeats it before chat writes.
func (s *Service) ValidateCreate(ctx context.Context, r CreateConversationRequest) error {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || !validKind(r.Kind) {
		return errOr(err, ErrInvalidArgument)
	}
	if r.Principal.TenantID != r.TenantID {
		return ErrPermissionDenied
	}
	if len(strings.TrimSpace(r.Name)) > 200 || len(strings.TrimSpace(r.IdempotencyKey)) > 200 {
		return ErrInvalidArgument
	}
	owner := r.OwnerID
	if owner == "" {
		owner = r.Principal.SubjectID
	}
	if owner != r.Principal.SubjectID {
		return ErrPermissionDenied
	}
	if s.authority == nil {
		return ErrUnavailable
	}
	checkID := r.ConversationID
	if checkID == "" {
		checkID = "chat:create-authority-check"
	}
	in, err := s.authority.Authorize(ctx, r.Principal, Conversation{ID: checkID, TenantID: r.TenantID, Kind: PublicChannel, Revision: 1}, chatpolicy.ActionDiscover, s.now())
	if err != nil {
		return ErrPermissionDenied
	}
	if _, err := chatpolicy.Evaluate(chatpolicy.ActionDiscover, in); err != nil {
		return ErrPermissionDenied
	}
	refs := r.Members
	if len(refs) == 0 {
		for _, id := range r.MemberIDs {
			refs = append(refs, MemberRef{TenantID: r.TenantID, SubjectID: id})
		}
	}
	if r.Kind == Direct && len(refs) > 1 {
		return ErrInvalidArgument
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		key := strings.TrimSpace(ref.TenantID) + "\x00" + strings.TrimSpace(ref.SubjectID)
		if strings.TrimSpace(ref.SubjectID) == "" || strings.TrimSpace(ref.TenantID) == "" || seen[key] {
			return ErrInvalidArgument
		}
		seen[key] = true
	}
	return nil
}

func (s *Service) CreateConversation(ctx context.Context, r CreateConversationRequest) (Conversation, error) {
	if err := s.ValidateCreate(ctx, r); err != nil {
		return Conversation{}, err
	}
	id := r.ConversationID
	if id == "" {
		id = uuid.NewString()
	}
	owner := r.OwnerID
	if owner == "" {
		owner = r.Principal.SubjectID
	}
	if owner != r.Principal.SubjectID {
		return Conversation{}, ErrPermissionDenied
	}
	c := Conversation{ID: id, TenantID: r.TenantID, Kind: r.Kind, Name: strings.TrimSpace(r.Name), OwnerID: owner, Revision: 1}
	refs := r.Members
	if len(refs) == 0 {
		for _, id := range r.MemberIDs {
			refs = append(refs, MemberRef{TenantID: r.TenantID, SubjectID: id})
		}
	}
	if c.Kind == Direct && len(refs) > 1 {
		return Conversation{}, ErrInvalidArgument
	}
	members := make([]Membership, 0, len(refs)+1)
	seen := map[string]bool{}
	for _, ref := range refs {
		if strings.TrimSpace(ref.SubjectID) == "" || strings.TrimSpace(ref.TenantID) == "" || seen[ref.TenantID+"\x00"+ref.SubjectID] {
			return Conversation{}, ErrInvalidArgument
		}
		seen[ref.TenantID+"\x00"+ref.SubjectID] = true
		members = append(members, Membership{ConversationID: id, TenantID: r.TenantID, HomeTenantID: ref.TenantID, SubjectID: ref.SubjectID, Role: Member, JoinedAt: timePtr(s.now()), HistoryVisibility: FullHistory, Revision: 1})
	}
	if !seen[r.TenantID+"\x00"+r.Principal.SubjectID] {
		members = append(members, Membership{ConversationID: id, TenantID: r.TenantID, HomeTenantID: r.TenantID, SubjectID: r.Principal.SubjectID, Role: Manager, JoinedAt: timePtr(s.now()), HistoryVisibility: FullHistory, Revision: 1})
	}
	return s.store.CreateConversation(ctx, c, members, r.IdempotencyKey)
}

func (s *Service) ListConversations(ctx context.Context, r ListConversationsRequest) (ListConversationsResponse, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil {
		return ListConversationsResponse{}, err
	}
	if err := validatePage(r.Page); err != nil {
		return ListConversationsResponse{}, err
	}
	out, err := s.store.ListConversations(ctx, r.Principal, r.TenantID, r.Page, ConversationScope{IncludeDiscoverable: r.IncludeDiscoverable})
	if err != nil || !r.IncludeDiscoverable {
		return out, err
	}
	// The store widened the listing by kind and tenant; only the policy can say
	// whether this caller may see a channel it has not joined. Joined rooms are
	// kept without a discovery check, because membership already answered it and
	// re-asking would drop a member out of its own sidebar the moment a channel
	// gained a role requirement.
	visible := make([]Conversation, 0, len(out.Conversations))
	for _, c := range out.Conversations {
		if c.Joined || s.authorize(ctx, r.Principal, c, chatpolicy.ActionDiscover) == nil {
			visible = append(visible, c)
		}
	}
	out.Conversations = visible
	return out, nil
}

func (s *Service) GetConversation(ctx context.Context, r GetConversationRequest) (Conversation, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || strings.TrimSpace(r.ConversationID) == "" {
		return Conversation{}, errOr(err, ErrInvalidArgument)
	}
	c, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return Conversation{}, err
	}
	if err := s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead); err != nil {
		return Conversation{}, err
	}
	return c, nil
}

func (s *Service) UpdateConversation(ctx context.Context, r UpdateConversationRequest) (Conversation, error) {
	if err := validatePrincipal(r.Principal, r.Conversation.TenantID); err != nil || r.ExpectedRevision == 0 || r.Conversation.ID == "" {
		return Conversation{}, errOr(err, ErrInvalidArgument)
	}
	c, err := s.store.GetConversation(ctx, r.Conversation.TenantID, r.Conversation.ID)
	if err != nil {
		return Conversation{}, err
	}
	if c.OwnerID != r.Principal.SubjectID || r.Principal.TenantID != c.TenantID {
		return Conversation{}, ErrPermissionDenied
	}
	if err := s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead); err != nil {
		return Conversation{}, err
	}
	if r.Conversation.Kind != c.Kind {
		return Conversation{}, ErrConflict
	}
	if r.Conversation.TenantID != c.TenantID {
		return Conversation{}, ErrPermissionDenied
	}
	return s.store.UpdateConversation(ctx, r.Conversation, r.ExpectedRevision)
}

func (s *Service) ListMemberships(ctx context.Context, r ListMembershipsRequest) (ListMembershipsResponse, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" {
		return ListMembershipsResponse{}, errOr(err, ErrInvalidArgument)
	}
	if err := validatePage(r.Page); err != nil {
		return ListMembershipsResponse{}, err
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return ListMembershipsResponse{}, err
	}
	return s.store.ListMemberships(ctx, r.TenantID, r.ConversationID, r.Page)
}

func (s *Service) AddMembership(ctx context.Context, r AddMembershipRequest) (Membership, error) {
	m := r.Membership
	if err := validatePrincipal(r.Principal, m.TenantID); err != nil || m.TenantID == "" || m.HomeTenantID == "" || m.ConversationID == "" || m.SubjectID == "" {
		return Membership{}, errOr(err, ErrInvalidArgument)
	}
	c, err := s.store.GetConversation(ctx, m.TenantID, m.ConversationID)
	if err != nil {
		return Membership{}, err
	}
	// Two ways in. The owner adds anybody, as before. A caller may also add
	// itself to a public channel, which is what makes a channel directory usable:
	// without it a discoverable channel is one nobody can enter without asking
	// its owner. Adding somebody else, or entering a private room, still needs
	// the owner.
	selfJoin := isSelfJoin(r.Principal, m, c)
	if !selfJoin && (c.OwnerID != r.Principal.SubjectID || r.Principal.TenantID != c.TenantID) {
		return Membership{}, ErrPermissionDenied
	}
	action := chatpolicy.ActionRead
	if selfJoin {
		// ActionJoin is the rule that was declared and never consulted. A public
		// channel with a role or qualification requirement is still closed to a
		// caller that does not meet it.
		action = chatpolicy.ActionJoin
	}
	if err := s.authorize(ctx, r.Principal, c, action); err != nil {
		return Membership{}, err
	}
	if selfJoin {
		// A self-join enters as an ordinary member. Letting the request name the
		// role would let a caller make itself a manager of any public channel.
		m.Role = Member
		if m.HistoryVisibility == "" {
			m.HistoryVisibility = FromJoin
		}
	}
	m.JoinedAt = timePtr(s.now())
	m.LeftAt = nil
	return s.store.PutMembership(ctx, r.Principal, m)
}

// isSelfJoin reports whether the request is a caller entering a public channel
// under its own identity. Everything about the subject is compared against the
// authenticated principal, never against a field the request supplies alone.
func isSelfJoin(p Principal, m Membership, c Conversation) bool {
	return c.Kind == PublicChannel && !c.Archived &&
		m.SubjectID == p.SubjectID && m.HomeTenantID == p.TenantID &&
		m.TenantID == c.TenantID
}

func (s *Service) RemoveMembership(ctx context.Context, r RemoveMembershipRequest) (Membership, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" || r.SubjectID == "" || r.ExpectedRevision == 0 {
		return Membership{}, errOr(err, ErrInvalidArgument)
	}
	homeTenant := r.HomeTenantID
	if homeTenant == "" {
		homeTenant = r.Principal.TenantID
	}
	if r.SubjectID == r.Principal.SubjectID && homeTenant != r.Principal.TenantID {
		return Membership{}, ErrPermissionDenied
	}
	if r.SubjectID != r.Principal.SubjectID && r.HomeTenantID == "" {
		return Membership{}, ErrInvalidArgument
	}
	c, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return Membership{}, err
	}
	if c.OwnerID == r.SubjectID && (r.Principal.SubjectID != c.OwnerID || r.Principal.TenantID != c.TenantID) {
		return Membership{}, ErrPermissionDenied
	}
	if (c.OwnerID != r.Principal.SubjectID || r.Principal.TenantID != c.TenantID) && r.SubjectID != r.Principal.SubjectID {
		return Membership{}, ErrPermissionDenied
	}
	if err := s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead); err != nil {
		return Membership{}, err
	}
	return s.store.RemoveMembership(ctx, r.Principal, r.TenantID, r.ConversationID, homeTenant, r.SubjectID, r.ExpectedRevision)
}

func (s *Service) SendPost(ctx context.Context, r SendPostRequest) (Post, error) {
	return s.sendPost(ctx, r, false)
}

func (s *Service) sendPost(ctx context.Context, r SendPostRequest, trustedSource bool) (Post, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" || strings.TrimSpace(r.Body) == "" || r.IdempotencyKey == "" {
		return Post{}, errOr(err, ErrInvalidArgument)
	}
	c, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return Post{}, err
	}
	if err := s.authorize(ctx, r.Principal, c, chatpolicy.ActionPost); err != nil {
		return Post{}, err
	}
	if r.SourceAttribution != nil && !trustedSource {
		return Post{}, ErrPermissionDenied
	}
	for _, ref := range r.References {
		if err := s.validateReference(ctx, r.Principal, r.TenantID, r.ConversationID, ref); err != nil {
			return Post{}, err
		}
	}
	p := Post{ID: uuid.NewString(), ConversationID: r.ConversationID, TenantID: r.TenantID, AuthorID: r.Principal.SubjectID, AuthorHomeTenantID: r.Principal.TenantID, Body: strings.TrimSpace(r.Body), ParentID: r.ParentID, Revision: 1, CreatedAt: s.now(), References: append([]Reference(nil), r.References...), SourceAttribution: cloneSourceAttribution(r.SourceAttribution)}
	return s.store.SendPost(ctx, r, p)
}

func cloneSourceAttribution(v *SourceAttribution) *SourceAttribution {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

func (s *Service) ListPosts(ctx context.Context, r ListPostsRequest) (ListPostsResponse, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" {
		return ListPostsResponse{}, errOr(err, ErrInvalidArgument)
	}
	if err := validatePage(r.Page); err != nil {
		return ListPostsResponse{}, err
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return ListPostsResponse{}, err
	}
	if r.Descending && r.AfterSequence != 0 {
		// A page cannot start after one sequence and end before another edge in
		// the same breath; the request contradicts itself.
		return ListPostsResponse{}, ErrInvalidArgument
	}
	return s.store.ListPosts(ctx, r.Principal, r.TenantID, r.ConversationID, r.AfterSequence, r.Page, PostWindow{Descending: r.Descending, BeforeSequence: r.BeforeSequence})
}

func (s *Service) EditPost(ctx context.Context, r EditPostRequest) (Post, error) {
	return s.mutatePost(ctx, r.Principal, r.TenantID, r.ConversationID, r.PostID, r.ExpectedRevision, func() error {
		if strings.TrimSpace(r.Body) == "" {
			return ErrInvalidArgument
		}
		return nil
	}, func() (Post, error) { return s.store.EditPost(ctx, r) })
}
func (s *Service) DeletePost(ctx context.Context, r DeletePostRequest) (Post, error) {
	return s.mutatePost(ctx, r.Principal, r.TenantID, r.ConversationID, r.PostID, r.ExpectedRevision, nil, func() (Post, error) { return s.store.DeletePost(ctx, r) })
}

func (s *Service) mutatePost(ctx context.Context, p Principal, tenant, conversation, post string, revision uint64, extra func() error, fn func() (Post, error)) (Post, error) {
	if err := validatePrincipal(p, tenant); err != nil || conversation == "" || post == "" || revision == 0 {
		return Post{}, errOr(err, ErrInvalidArgument)
	}
	if extra != nil {
		if err := extra(); err != nil {
			return Post{}, err
		}
	}
	c, err := s.store.GetConversation(ctx, tenant, conversation)
	if err != nil {
		return Post{}, err
	}
	if err = s.authorize(ctx, p, c, chatpolicy.ActionPost); err != nil {
		return Post{}, err
	}
	current, err := s.store.GetPost(ctx, tenant, conversation, post)
	if err != nil {
		return Post{}, ErrNotFound
	}
	if current.ConversationID != conversation {
		return Post{}, ErrNotFound
	}
	if current.AuthorID != p.SubjectID || current.AuthorHomeTenantID != p.TenantID {
		return Post{}, ErrPermissionDenied
	}
	return fn()
}

func (s *Service) Search(ctx context.Context, r SearchRequest) (SearchResponse, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil {
		return SearchResponse{}, err
	}
	if strings.TrimSpace(r.Query) == "" || len(r.Query) > 512 {
		return SearchResponse{}, ErrInvalidArgument
	}
	if err := validatePage(r.Page); err != nil {
		return SearchResponse{}, err
	}
	if r.ConversationID == "" {
		// A global search must be implemented by a store query that applies
		// current membership to every hit. Until that port exists, fail closed.
		return SearchResponse{}, ErrInvalidArgument
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return SearchResponse{}, err
	}
	return s.store.Search(ctx, r)
}
func (s *Service) GetReadState(ctx context.Context, r GetReadStateRequest) (ReadState, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" {
		return ReadState{}, errOr(err, ErrInvalidArgument)
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return ReadState{}, err
	}
	return s.store.GetReadState(ctx, r.TenantID, r.ConversationID, r.Principal.TenantID, r.Principal.SubjectID)
}
func (s *Service) UpdateReadState(ctx context.Context, r UpdateReadStateRequest) (ReadState, error) {
	x := r.ReadState
	if err := validatePrincipal(r.Principal, x.TenantID); err != nil || x.ConversationID == "" || r.ExpectedRevision == 0 {
		return ReadState{}, errOr(err, ErrInvalidArgument)
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: x.TenantID, ConversationID: x.ConversationID}); err != nil {
		return ReadState{}, err
	}
	x.SubjectID = r.Principal.SubjectID
	x.HomeTenantID = r.Principal.TenantID
	return s.store.PutReadState(ctx, x, r.ExpectedRevision)
}
func (s *Service) GetPreferences(ctx context.Context, r GetPreferencesRequest) (NotificationPreferences, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" {
		return NotificationPreferences{}, errOr(err, ErrInvalidArgument)
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return NotificationPreferences{}, err
	}
	return s.store.GetPreferences(ctx, r.TenantID, r.ConversationID, r.Principal.TenantID, r.Principal.SubjectID)
}
func (s *Service) UpdatePreferences(ctx context.Context, r UpdatePreferencesRequest) (NotificationPreferences, error) {
	x := r.Preferences
	if err := validatePrincipal(r.Principal, x.TenantID); err != nil || x.ConversationID == "" || r.ExpectedRevision == 0 {
		return NotificationPreferences{}, errOr(err, ErrInvalidArgument)
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: x.TenantID, ConversationID: x.ConversationID}); err != nil {
		return NotificationPreferences{}, err
	}
	x.SubjectID = r.Principal.SubjectID
	x.HomeTenantID = r.Principal.TenantID
	return s.store.PutPreferences(ctx, x, r.ExpectedRevision)
}
func (s *Service) AddReaction(ctx context.Context, r AddReactionRequest) (Reaction, error) {
	x := r.Reaction
	if err := validatePrincipal(r.Principal, x.TenantID); err != nil || x.ConversationID == "" || x.PostID == "" || x.Emoji == "" || len(x.Emoji) > 64 {
		return Reaction{}, errOr(err, ErrInvalidArgument)
	}
	if err := s.requireVisiblePost(ctx, r.Principal, x.TenantID, x.ConversationID, x.PostID); err != nil {
		return Reaction{}, err
	}
	x.SubjectID = r.Principal.SubjectID
	x.HomeTenantID = r.Principal.TenantID
	x.CreatedAt = s.now()
	return s.store.PutReaction(ctx, x)
}
func (s *Service) RemoveReaction(ctx context.Context, r RemoveReactionRequest) error {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" || r.PostID == "" || r.Emoji == "" {
		return errOr(err, ErrInvalidArgument)
	}
	if err := s.requireVisiblePost(ctx, r.Principal, r.TenantID, r.ConversationID, r.PostID); err != nil {
		return err
	}
	return s.store.RemoveReaction(ctx, r.TenantID, r.ConversationID, r.PostID, r.Principal.TenantID, r.Principal.SubjectID, r.Emoji)
}
func (s *Service) ListReactions(ctx context.Context, r ListReactionsRequest) (ListReactionsResponse, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" || r.PostID == "" || len(r.Page.Cursor) > 1024 {
		return ListReactionsResponse{}, errOr(err, ErrInvalidArgument)
	}
	if err := validatePage(r.Page); err != nil {
		return ListReactionsResponse{}, err
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return ListReactionsResponse{}, err
	}
	return s.store.ListReactions(ctx, r.Principal, r.TenantID, r.ConversationID, r.PostID, r.Page)
}
func (s *Service) requireVisiblePost(ctx context.Context, p Principal, tenantID, conversationID, postID string) error {
	c, err := s.GetConversation(ctx, GetConversationRequest{Principal: p, TenantID: tenantID, ConversationID: conversationID})
	if err != nil {
		return err
	}
	post, err := s.store.GetPost(ctx, tenantID, conversationID, postID)
	if err != nil {
		return err
	}
	if post.Deleted || !s.postVisibleTo(ctx, p, c, post) {
		return ErrPermissionDenied
	}
	return nil
}
func (s *Service) PinPost(ctx context.Context, r PinPostRequest) (Pin, error) {
	x := r.Pin
	if err := validatePrincipal(r.Principal, x.TenantID); err != nil || x.ConversationID == "" || x.PostID == "" {
		return Pin{}, errOr(err, ErrInvalidArgument)
	}
	c, err := s.store.GetConversation(ctx, x.TenantID, x.ConversationID)
	if err != nil {
		return Pin{}, err
	}
	if c.OwnerID != r.Principal.SubjectID || r.Principal.TenantID != c.TenantID {
		return Pin{}, ErrPermissionDenied
	}
	if err := s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead); err != nil {
		return Pin{}, err
	}
	x.PinnedBy = r.Principal.SubjectID
	x.PinnedByHomeTenantID = r.Principal.TenantID
	x.CreatedAt = s.now()
	return s.store.PutPin(ctx, x)
}
func (s *Service) UnpinPost(ctx context.Context, r UnpinPostRequest) error {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" || r.PostID == "" || r.ExpectedRevision == 0 {
		return errOr(err, ErrInvalidArgument)
	}
	c, e := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if e != nil {
		return e
	}
	if c.OwnerID != r.Principal.SubjectID || r.Principal.TenantID != c.TenantID {
		return ErrPermissionDenied
	}
	if err := s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead); err != nil {
		return err
	}
	return s.store.RemovePin(ctx, r.Principal, r.TenantID, r.ConversationID, r.PostID, r.Principal.TenantID, r.ExpectedRevision)
}
func (s *Service) ListPins(ctx context.Context, r ListPinsRequest) ([]Pin, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" {
		return nil, errOr(err, ErrInvalidArgument)
	}
	c, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID})
	if err != nil {
		return nil, err
	}
	pins, err := s.store.ListPins(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return nil, err
	}
	visible := make([]Pin, 0, len(pins))
	for _, pin := range pins {
		post, getErr := s.store.GetPost(ctx, r.TenantID, r.ConversationID, pin.PostID)
		if getErr != nil {
			return nil, getErr
		}
		if !post.Deleted && s.postVisibleTo(ctx, r.Principal, c, post) {
			visible = append(visible, pin)
		}
	}
	return visible, nil
}
func (s *Service) WatchConversation(ctx context.Context, r WatchConversationRequest) (<-chan WatchEvent, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" {
		return nil, errOr(err, ErrInvalidArgument)
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return nil, err
	}
	return s.store.Watch(ctx, r)
}

// storeErrorWatcher is the store's reporting watch. chatstore implements it; a
// store that does not keeps the single-channel behaviour.
type storeErrorWatcher interface {
	WatchWithErrors(context.Context, WatchConversationRequest) (<-chan WatchEvent, <-chan error, error)
}

// WatchConversationWithErrors carries the store's terminal cause out to the
// transport. Without it the store's error channel was dropped on the floor and a
// failed poll reached the caller as a closed channel — an end of stream
// indistinguishable from a healthy one, which is what a browser saw milliseconds
// after opening a watch.
func (s *Service) WatchConversationWithErrors(ctx context.Context, r WatchConversationRequest) (<-chan WatchEvent, <-chan error, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" {
		return nil, nil, errOr(err, ErrInvalidArgument)
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return nil, nil, err
	}
	reporting, ok := s.store.(storeErrorWatcher)
	if !ok {
		events, err := s.store.Watch(ctx, r)
		return events, nil, err
	}
	return reporting.WatchWithErrors(ctx, r)
}

func (s *Service) authorize(ctx context.Context, p Principal, c Conversation, action chatpolicy.Action) error {
	if s.authority != nil {
		in, err := s.authority.Authorize(ctx, p, c, action, s.now())
		if err != nil {
			return ErrPermissionDenied
		}
		if _, err = chatpolicy.Evaluate(action, in); err != nil {
			return ErrPermissionDenied
		}
		return nil
	}
	// Runtime composition must provide a current-authority source. Request
	// claims and store membership rows are insufficient after revocation.
	return ErrUnavailable
}

func validatePrincipal(p Principal, tenant string) error {
	if strings.TrimSpace(p.SubjectID) == "" || strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(tenant) == "" {
		return ErrUnauthenticated
	}
	return nil
}
func validatePage(p Page) error {
	if p.PageSize > 200 {
		return ErrInvalidArgument
	}
	return nil
}
func validKind(k ConversationKind) bool {
	return k == PublicChannel || k == PrivateChannel || k == Direct || k == Group
}
func errOr(got, want error) error {
	if got != nil {
		return got
	}
	return want
}
func timePtr(t time.Time) *time.Time { return &t }

var _ ConversationService = (*Service)(nil)
