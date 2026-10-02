package chat

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrChannelStatus = errors.New("chat: action refused by channel status")
var ErrChannelHeld = errors.New("chat: channel is under record hold")
var ErrLastReopener = errors.New("chat: last channel reopener must retain access")

type ChannelStatus struct {
	TenantID       string                   `json:"tenant_id"`
	ConversationID string                   `json:"conversation_id"`
	Name           string                   `json:"channel_name"`
	Status         chatpolicy.ChannelStatus `json:"status"`
	Revision       uint64                   `json:"revision"`
	ChangedBy      string                   `json:"changed_by"`
	ChangedAt      *time.Time               `json:"changed_at,omitempty"`
	Reason         string                   `json:"reason"`
	Until          *time.Time               `json:"until,omitempty"`
}

func (s ChannelStatus) Effective(at time.Time) chatpolicy.ChannelStatus {
	if s.Status == chatpolicy.StatusLocked && s.Until != nil && !at.Before(*s.Until) {
		return chatpolicy.StatusOpen
	}
	return chatpolicy.NormalizeChannelStatus(s.Status)
}

type ChangeChannelStatusRequest struct {
	Principal        Principal                `json:"-"`
	TenantID         string                   `json:"tenant_id"`
	ConversationID   string                   `json:"conversation_id"`
	Status           chatpolicy.ChannelStatus `json:"status"`
	ExpectedRevision uint64                   `json:"expected_revision"`
	Reason           string                   `json:"reason"`
	Until            *time.Time               `json:"until,omitempty"`
}

type StatusTransition struct {
	Status     chatpolicy.ChannelStatus `json:"status"`
	Permission string                   `json:"permission"`
	Restricted bool                     `json:"restricted"`
}

// ChannelStatusStore changes status and emits its event in the same transaction.
// Recheck must run while the conversation fence is held, before any mutation.
type ChannelStatusStore interface {
	ReadChannelStatus(context.Context, string, string) (ChannelStatus, error)
	CommitChannelStatus(context.Context, ChangeChannelStatusRequest, time.Time, func(context.Context, ChannelStatus) error) (ChannelStatus, error)
	SweepChannelStatuses(context.Context, string, time.Time) (int, error)
}

// ChannelStatusAuthority resolves assignable workspace and channel permissions.
// Implementations also prove current public delivery grants for installed agents.
type ChannelStatusAuthority interface {
	ChannelStatusPermissions(context.Context, Principal, Conversation, time.Time) (chatpolicy.StatusPermissions, error)
}

type ChannelStatusService interface {
	GetChannelStatus(context.Context, GetConversationRequest) (ChannelStatus, error)
	AllowedStatusTransitions(context.Context, GetConversationRequest) ([]StatusTransition, error)
	ChangeChannelStatus(context.Context, ChangeChannelStatusRequest) (ChannelStatus, error)
}

type ChannelStatusSnapshot struct {
	Status         ChannelStatus      `json:"status"`
	CanPost        bool               `json:"can_post"`
	AllowedActions [7]bool            `json:"allowed_actions"`
	Transitions    []StatusTransition `json:"transitions"`
}

type ChannelStatusSnapshotService interface {
	GetChannelStatusSnapshot(context.Context, GetConversationRequest) (ChannelStatusSnapshot, error)
}

func (s *Service) GetChannelStatusSnapshot(ctx context.Context, r GetConversationRequest) (ChannelStatusSnapshot, error) {
	c, err := s.statusConversation(ctx, r)
	if err != nil {
		return ChannelStatusSnapshot{}, err
	}
	status, err := s.GetChannelStatus(ctx, r)
	if err != nil {
		return ChannelStatusSnapshot{}, err
	}
	permissions, err := s.statusPermissions(ctx, r.Principal, c)
	if err != nil {
		return ChannelStatusSnapshot{}, err
	}
	snapshot := ChannelStatusSnapshot{Status: status, Transitions: []StatusTransition{}}
	for action := chatpolicy.StatusPost; action <= chatpolicy.StatusRename; action++ {
		snapshot.AllowedActions[action] = chatpolicy.StatusAllows(status.Status, action, permissions.PostAnnouncements, permissions.PublicDelivery)
	}
	snapshot.CanPost = snapshot.AllowedActions[chatpolicy.StatusPost]
	if snapshot.CanPost {
		err = s.authorize(ctx, r.Principal, c, chatpolicy.ActionPost)
		if errors.Is(err, ErrPermissionDenied) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrChannelStatus) {
			snapshot.CanPost = false
		} else if err != nil {
			return ChannelStatusSnapshot{}, err
		}
	}
	for _, rule := range chatpolicy.StatusRegistry() {
		if chatpolicy.CanChangeChannelStatus(status.Status, rule.Status, permissions) {
			snapshot.Transitions = append(snapshot.Transitions, StatusTransition{Status: rule.Status, Permission: chatpolicy.PermissionChangeChannelStatus, Restricted: rule.Status == chatpolicy.StatusLocked || rule.Status == chatpolicy.StatusArchived})
		}
	}
	return snapshot, nil
}

func (s *Service) statusStore() (ChannelStatusStore, error) {
	store, ok := s.store.(ChannelStatusStore)
	if !ok {
		return nil, ErrUnavailable
	}
	return store, nil
}

func (s *Service) statusConversation(ctx context.Context, r GetConversationRequest) (Conversation, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil {
		return Conversation{}, err
	}
	if r.ConversationID == "" {
		return Conversation{}, ErrInvalidArgument
	}
	c, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return c, err
	}
	if c.Kind != PublicChannel && c.Kind != PrivateChannel {
		return c, ErrInvalidArgument
	}
	// Archived channels remain readable; lifecycle is enforced separately.
	c.Archived = false
	return c, s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead)
}

func (s *Service) statusPermissions(ctx context.Context, p Principal, c Conversation) (chatpolicy.StatusPermissions, error) {
	if a, ok := s.authority.(ChannelStatusAuthority); ok {
		return a.ChannelStatusPermissions(ctx, p, c, s.now())
	}
	if s.authority == nil {
		return chatpolicy.StatusPermissions{}, ErrUnavailable
	}
	in, err := s.authority.Authorize(ctx, p, c, chatpolicy.ActionRead, s.now())
	if err != nil {
		return chatpolicy.StatusPermissions{}, err
	}
	if _, err = chatpolicy.Evaluate(chatpolicy.ActionRead, in); err != nil {
		return chatpolicy.StatusPermissions{}, ErrPermissionDenied
	}
	permissions := chatpolicy.StatusPermissions{}
	if p.TenantID != c.TenantID {
		return permissions, nil
	}
	for _, role := range in.Principal.Roles {
		switch role {
		case chatpolicy.WorkspaceAdministratorRole:
			permissions.WorkspaceAdmin = true
		case chatpolicy.PermissionChangeChannelStatus:
			permissions.ChangeOpen, permissions.ChangeRestricted = true, true
		case chatpolicy.PermissionPostAnnouncements:
			permissions.PostAnnouncements = true
		}
	}
	m, err := s.store.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return permissions, err
	}
	permissions.ChannelAdmin = err == nil && strings.EqualFold(string(m.Role), string(Manager)) && m.JoinedAt != nil && m.LeftAt == nil
	if store, ok := s.store.(ChannelStatusPermissionStore); ok {
		assigned, err := store.ChannelStatusRolePermissions(ctx, c.TenantID, c.ID, in.Principal.Roles)
		if err != nil {
			return permissions, err
		}
		permissions.ChangeOpen = permissions.ChangeOpen || assigned.ChangeOpen
		permissions.ChangeRestricted = permissions.ChangeRestricted || assigned.ChangeRestricted
		permissions.PostAnnouncements = permissions.PostAnnouncements || assigned.PostAnnouncements
		if permissions.ChannelAdmin {
			active, err := store.ChannelStatusMembershipCurrent(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
			if err != nil {
				return permissions, err
			}
			permissions.ChannelAdmin = active
		}
	}
	return permissions, nil
}

func (s *Service) GetChannelStatus(ctx context.Context, r GetConversationRequest) (ChannelStatus, error) {
	if _, err := s.statusConversation(ctx, r); err != nil {
		return ChannelStatus{}, err
	}
	store, err := s.statusStore()
	if err != nil {
		return ChannelStatus{}, err
	}
	status, err := store.ReadChannelStatus(ctx, r.TenantID, r.ConversationID)
	status.Status = status.Effective(s.now())
	return status, err
}

func (s *Service) AllowedStatusTransitions(ctx context.Context, r GetConversationRequest) ([]StatusTransition, error) {
	c, err := s.statusConversation(ctx, r)
	if err != nil {
		return nil, err
	}
	store, err := s.statusStore()
	if err != nil {
		return nil, err
	}
	current, err := store.ReadChannelStatus(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return nil, err
	}
	p, err := s.statusPermissions(ctx, r.Principal, c)
	if err != nil {
		return nil, err
	}
	transitions := []StatusTransition{}
	for _, rule := range chatpolicy.StatusRegistry() {
		if chatpolicy.CanChangeChannelStatus(current.Effective(s.now()), rule.Status, p) {
			transitions = append(transitions, StatusTransition{rule.Status, chatpolicy.PermissionChangeChannelStatus, rule.Status == chatpolicy.StatusLocked || rule.Status == chatpolicy.StatusArchived})
		}
	}
	return transitions, nil
}

func (s *Service) ChangeChannelStatus(ctx context.Context, r ChangeChannelStatusRequest) (ChannelStatus, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.ExpectedRevision == 0 || r.Reason == "" || len(r.Reason) > 2000 || (r.Until != nil && (r.Status != chatpolicy.StatusLocked || !r.Until.After(s.now()))) {
		return ChannelStatus{}, ErrInvalidArgument
	}
	c, err := s.statusConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID})
	if err != nil {
		return ChannelStatus{}, err
	}
	store, err := s.statusStore()
	if err != nil {
		return ChannelStatus{}, err
	}
	return store.CommitChannelStatus(ctx, r, s.now(), func(ctx context.Context, current ChannelStatus) error {
		p, err := s.statusPermissions(ctx, r.Principal, c)
		if err != nil {
			return err
		}
		if !chatpolicy.CanChangeChannelStatus(current.Effective(s.now()), r.Status, p) {
			return ErrPermissionDenied
		}
		return nil
	})
}

// CheckChannelStatusAction is shared by posts, thread replies and mutations.
func (s *Service) CheckChannelStatusAction(ctx context.Context, p Principal, c Conversation, action chatpolicy.StatusAction) error {
	return s.authorize(WithChannelStatusAction(ctx, action), p, c, chatpolicy.ActionRead)
}

type channelStatusActionKey struct{}

// WithChannelStatusAction is set by the server-side command dispatch, never
// from a browser claim. The single authorize hook then checks the precise
// mutation alongside current membership rather than treating it as a read.
func WithChannelStatusAction(ctx context.Context, action chatpolicy.StatusAction) context.Context {
	return context.WithValue(ctx, channelStatusActionKey{}, action)
}

func (s *Service) authorizeChannelStatus(ctx context.Context, p Principal, c Conversation, action chatpolicy.Action, in *chatpolicy.Input) error {
	store, ok := s.store.(ChannelStatusStore)
	if c.Kind != PublicChannel && c.Kind != PrivateChannel {
		return nil
	}
	if !ok {
		return ErrUnavailable
	}
	status, err := store.ReadChannelStatus(ctx, c.TenantID, c.ID)
	if err != nil {
		return err
	}
	// Legacy adapters use Archived for every non-ACTIVE lifecycle. A registered
	// channel status preserves read/search authority instead of disabling it.
	if c.Archived {
		for _, rule := range chatpolicy.StatusRegistry() {
			if rule.Status == status.Status {
				in.Channel.Enabled = true
			}
		}
	}
	if members, ok := s.store.(ChannelStatusPermissionStore); ok && in.HasMembership && !machineConversationPrincipal(ctx, p) {
		active, err := members.ChannelStatusMembershipCurrent(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
		if err != nil {
			return err
		}
		if !active {
			in.Membership.State = chatpolicy.MembershipSuspended
		}
	}
	if _, err = chatpolicy.Evaluate(action, *in); err != nil {
		return conversationDenial(ctx, p)
	}
	statusAction, mutation := ctx.Value(channelStatusActionKey{}).(chatpolicy.StatusAction)
	if !mutation && action == chatpolicy.ActionPost {
		statusAction, mutation = chatpolicy.StatusPost, true
	}
	if !mutation {
		return nil
	}
	if !(action == chatpolicy.ActionJoin && statusAction == chatpolicy.StatusManageMembers) && (!in.HasMembership || !in.Membership.CurrentAt(c.ID, p.SubjectID, p.TenantID, s.now())) {
		return conversationDenial(ctx, p)
	}
	c.Archived = false
	permissions := chatpolicy.StatusPermissions{}
	if status.Effective(s.now()) == chatpolicy.StatusAnnouncements {
		permissions, err = s.statusPermissions(ctx, p, c)
		if err != nil {
			return err
		}
	}
	if !chatpolicy.StatusAllows(status.Effective(s.now()), statusAction, permissions.PostAnnouncements, permissions.PublicDelivery) {
		return ErrChannelStatus
	}
	return nil
}

// PostAllowed requires a verified identity of the declared kind. Scheduled
// callers hold refused deliveries and notify their owner instead of dropping them.
func (s *Service) PostAllowed(ctx context.Context, c Conversation, kind trust.SubjectKind) error {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != kind || !s.now().Before(p.ExpiresAt()) {
		return ErrUnauthenticated
	}
	return s.authorize(ctx, Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}, c, chatpolicy.ActionPost)
}

// WithChannelStatusRead keeps a fence recheck's read separate from its mutation.
func WithChannelStatusRead(ctx context.Context) context.Context {
	return context.WithValue(ctx, channelStatusActionKey{}, false)
}

func (s *Service) CheckChannelMembershipAdmission(ctx context.Context, p Principal, c Conversation) error {
	return s.authorize(WithChannelStatusAction(ctx, chatpolicy.StatusManageMembers), p, c, chatpolicy.ActionJoin)
}

func (s *Service) CheckChannelMembershipAdmissionByID(ctx context.Context, p Principal, tenant, conversation string) error {
	if err := validatePrincipal(p, tenant); err != nil {
		return err
	}
	if conversation == "" {
		return ErrInvalidArgument
	}
	channel, err := s.store.GetConversation(ctx, tenant, conversation)
	if err != nil {
		return err
	}
	return s.CheckChannelMembershipAdmission(ctx, p, channel)
}

// CheckPersonaReplyChannelStatus checks a server-owned delivery capability, not a human session impersonating its author.
func (s *Service) CheckPersonaReplyChannelStatus(ctx context.Context, r PersonaReplyCommitRequest) error {
	if r.Proof == nil || !r.Proof.ValidFor(r.TenantID, r.ConversationID, r.AuthorID, r.ExpectedAudienceRevision, r.OutputDigest, r.Body, r.ParentID) {
		return ErrPermissionDenied
	}
	c, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return err
	}
	if c.Kind != PublicChannel && c.Kind != PrivateChannel {
		return ErrPermissionDenied
	}
	store, ok := s.store.(ChannelStatusStore)
	if !ok {
		return ErrUnavailable
	}
	status, err := store.ReadChannelStatus(ctx, c.TenantID, c.ID)
	if err != nil {
		return err
	}
	action := chatpolicy.StatusPost
	if r.ParentID != "" {
		action = chatpolicy.StatusReply
	}
	if !chatpolicy.StatusAllows(status.Effective(s.now()), action, false, true) {
		return ErrChannelStatus
	}
	return nil
}
