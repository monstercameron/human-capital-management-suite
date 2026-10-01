// Package install owns the server-side admission boundary for personas in
// conversations. It keeps persona placement, mention budgets, and lifecycle
// fencing together so a caller cannot check one of those ceilings and then
// start work against a stale installation.
package install

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

var (
	ErrInvalid          = errors.New("persona install: invalid request")
	ErrDenied           = errors.New("persona install: denied")
	ErrUnavailable      = errors.New("persona install: unavailable")
	ErrLimit            = errors.New("persona install: admission limit exceeded")
	ErrFenced           = errors.New("persona install: fenced")
	ErrNotFound         = errors.New("persona install: not found")
	ErrAlreadyInstalled = errors.New("persona install: already installed")
)

// Tier aliases the published agent skill side-effect ladder. A persona never
// raises the tier of one of its pinned skills.
type Tier = agentskills.SideEffectTier

const (
	TierT0 = agentskills.TierT0
	TierT1 = agentskills.TierT1
	TierT2 = agentskills.TierT2
	TierT3 = agentskills.TierT3
	TierT4 = agentskills.TierT4
)

// ChannelClass is the chat-owned classification used by placement policy.
type ChannelClass string

const (
	ChannelPublic       ChannelClass = "PUBLIC"
	ChannelPrivate      ChannelClass = "PRIVATE"
	ChannelGroupDM      ChannelClass = "GROUP_DM"
	ChannelOneToOne     ChannelClass = "ONE_TO_ONE"
	ChannelExternal     ChannelClass = "EXTERNAL"
	ChannelCrossCompany ChannelClass = "CROSS_COMPANY"
)

// Lifecycle is the persona publication state relevant to invocation.
type Lifecycle string

const (
	LifecycleDraft     Lifecycle = "DRAFT"
	LifecyclePublished Lifecycle = "PUBLISHED"
	LifecycleSuspended Lifecycle = "SUSPENDED"
	LifecycleRetired   Lifecycle = "RETIRED"
)

// PersonaVersion is an immutable, published persona projection. The profile
// and skill registry own its construction; this package only consumes the
// resulting ceilings.
type PersonaVersion struct {
	TenantID              string
	PersonaID             string
	Version               uint64
	Lifecycle             Lifecycle
	OwnerID               string
	StewardID             string
	AllowedChannelClasses []ChannelClass
	TierCeiling           Tier
	DataClasses           []string
	AlwaysPrivate         bool
	ConversationSearch    bool
	DailySpendCeiling     int64
}

// Conversation is a membership snapshot supplied by chat. Membership is not
// stored or mutated by this package.
type Conversation struct {
	TenantID            string
	ConversationID      string
	Class               ChannelClass
	ExternalMembers     bool
	CrossCompanyMembers bool
	MembershipRevision  uint64
}

// ChannelPersonaPolicy is the channel-owned outer bound. Empty allowlists
// mean no data class or channel class is allowed.
type ChannelPersonaPolicy struct {
	MaxTier                   Tier
	AllowedDataClasses        []string
	AlwaysPrivate             bool
	ConversationSearchAllowed bool
	AllowedChannelClasses     []ChannelClass
	AllowExternalMembers      bool
	AllowCrossCompanyMembers  bool
}

// MembershipSnapshot lets chat recompute the policy before each invocation.
// AllowedDataClasses is an optional narrower audience floor for the current
// membership; it is particularly useful when a guest joins a conversation.
type MembershipSnapshot struct {
	Revision            uint64
	ExternalMembers     bool
	CrossCompanyMembers bool
	AllowedDataClasses  []string
}

type MembershipProvider interface {
	Snapshot(context.Context, string, string) (MembershipSnapshot, error)
}

type ChannelManagerAuthorizer interface {
	IsChannelManager(context.Context, string, string, string) (bool, error)
}

// BilateralPolicy is the AGENT-037 seam. External and cross-company
// placement is denied unless this policy explicitly allows that pair.
type BilateralPolicy interface {
	AllowsPersona(context.Context, string, string, string) (bool, error)
}

// InstallRequest is evaluated atomically against the service's installation
// set. Installation IDs are generated when omitted.
type InstallRequest struct {
	Persona        PersonaVersion
	Conversation   Conversation
	Policy         ChannelPersonaPolicy
	InstallerID    string
	ChannelManager bool
}

type InstallationStatus string

const (
	InstallationActive    InstallationStatus = "ACTIVE"
	InstallationSuspended InstallationStatus = "SUSPENDED"
	InstallationKilled    InstallationStatus = "KILLED"
)

// Installation is a revisioned grant. Policy is copied at installation time,
// while EffectivePolicy re-evaluates it against current chat membership.
type Installation struct {
	ID                string
	TenantID          string
	PersonaID         string
	PersonaVersion    uint64
	ConversationID    string
	ConversationClass ChannelClass
	InstallerID       string
	Policy            ChannelPersonaPolicy
	Status            InstallationStatus
	Revision          uint64
	RevocationEpoch   uint64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type PolicyDecision struct {
	Allowed            bool
	Reason             string
	Policy             ChannelPersonaPolicy
	MembershipRevision uint64
	RevocationEpoch    uint64
}

// Limits are persona-specific ceilings nested inside the wider agent budget.
// The default values are the AGENTP-001 release defaults.
type Limits struct {
	PerInvokerPerHour      int
	PerInvokerConcurrent   int
	PerConversationPerHour int
	DailySpendMicros       int64
}

func DefaultLimits() Limits {
	return Limits{PerInvokerPerHour: 30, PerInvokerConcurrent: 3, PerConversationPerHour: 120}
}

type BudgetRequest struct {
	TenantID       string
	InvokerID      string
	PersonaID      string
	ConversationID string
	EstimatedSpend int64
}

type BudgetReservation interface {
	Release() error
	Settle(int64) error
}

// OuterBudget is the AGENT2-012/agentbudget seam. Persona admission is
// refused if the wider task, user, or tenant ledger cannot reserve first.
type OuterBudget interface {
	Reserve(context.Context, BudgetRequest) (BudgetReservation, error)
}

type MentionRequest struct {
	TenantID       string
	InvokerID      string
	PersonaID      string
	ConversationID string
	InstallationID string
	EstimatedSpend int64
	At             time.Time
}

type LimitReason string

const (
	ReasonInvokerRate        LimitReason = "PER_INVOKER_RATE"
	ReasonInvokerConcurrency LimitReason = "PER_INVOKER_CONCURRENCY"
	ReasonConversationRate   LimitReason = "PER_CONVERSATION_RATE"
	ReasonPersonaSpend       LimitReason = "PER_PERSONA_DAILY_SPEND"
	ReasonOuterBudget        LimitReason = "OUTER_AGENT_BUDGET"
)

// LimitError is safe to render as an ephemeral typed denial. It intentionally
// contains no prompt, model, or channel content.
type LimitError struct {
	Reason     LimitReason
	RetryAfter time.Duration
	Ephemeral  bool
}

func (e *LimitError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s (retry after %s)", e.Reason, e.RetryAfter.Round(time.Second))
}

func (e *LimitError) Unwrap() error { return ErrLimit }

// MentionLease reserves all persona-local ceilings before model work. Commit
// is the linearization point for starting the run and checks the lifecycle
// epoch again; Release is safe to call after a failed commit.
type MentionLease struct {
	service        *Service
	ID             string
	request        MentionRequest
	installation   string
	epoch          uint64
	estimatedSpend int64
	outer          BudgetReservation
	closed         bool
}

func (l *MentionLease) Commit() error {
	if l == nil || l.service == nil {
		return ErrFenced
	}
	return l.service.commitMention(l)
}

func (l *MentionLease) Release() error {
	if l == nil || l.service == nil {
		return ErrFenced
	}
	return l.service.closeMention(l, 0, false)
}

func (l *MentionLease) Settle(actualSpend int64) error {
	if l == nil || l.service == nil {
		return ErrFenced
	}
	return l.service.closeMention(l, actualSpend, true)
}

// TaskState is the small lifecycle projection this package owns. Durable
// agentrun adapters can mirror these transitions through LifecycleProjector.
type TaskState string

const (
	TaskRunning TaskState = "RUNNING"
	TaskWaiting TaskState = "WAITING"
	TaskPaused  TaskState = "PAUSED"
)

type Task struct {
	ID             string
	TenantID       string
	PersonaID      string
	InstallationID string
	State          TaskState
	PauseReason    string
	ApprovalOpen   bool
	CardExpiresAt  time.Time
}

type PauseRequest struct {
	TenantID        string
	PersonaID       string
	InstallationID  string
	Reason          string
	RevocationEpoch uint64
}

type LifecycleProjector interface {
	Pause(context.Context, PauseRequest) error
	VoidApprovals(context.Context, PauseRequest) error
	ExpireCards(context.Context, PauseRequest) error
}

type OwnerStatus struct {
	Active       bool
	HasOwnerRole bool
	InvalidSince time.Time
}

type OwnerStatusResolver interface {
	ResolveOwner(context.Context, string, string, string) (OwnerStatus, error)
}

type Notification struct {
	TenantID  string
	PersonaID string
	OwnerID   string
	Reason    string
}

type NotificationSink interface {
	Notify(context.Context, Notification) error
}

// RevocationEpochStore is the AGENT2-020 seam. Implementations persist the
// tenant/persona/version/install epoch with the identity change event.
type RevocationEpochStore interface {
	Bump(context.Context, string, string, uint64, string, string) (uint64, error)
}

type Config struct {
	Clock       func() time.Time
	Limits      Limits
	Membership  MembershipProvider
	Manager     ChannelManagerAuthorizer
	Bilateral   BilateralPolicy
	OuterBudget OuterBudget
	Projector   LifecycleProjector
	OwnerStatus OwnerStatusResolver
	Notifier    NotificationSink
	Revocations RevocationEpochStore
}

// Service is safe for concurrent installs, mentions, membership refreshes,
// and lifecycle changes. The mutex is also the fence commit boundary.
type Service struct {
	mu               sync.Mutex
	now              func() time.Time
	limits           Limits
	membership       MembershipProvider
	manager          ChannelManagerAuthorizer
	bilateral        BilateralPolicy
	outerBudget      OuterBudget
	projector        LifecycleProjector
	ownerStatus      OwnerStatusResolver
	notifier         NotificationSink
	revocations      RevocationEpochStore
	sequence         uint64
	installations    map[string]*installationState
	personas         map[string]PersonaVersion
	tasks            map[string]*Task
	invokerHour      map[string]*windowCounter
	conversationHour map[string]*windowCounter
	spend            map[string]*spendCounter
	leases           map[string]*MentionLease
	membershipState  map[string]MembershipSnapshot
}

type installationState struct {
	installation Installation
	persona      PersonaVersion
}

type windowCounter struct {
	period time.Time
	count  int
}
type spendCounter struct {
	period         time.Time
	used, reserved int64
}

func New(cfg Config) (*Service, error) {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	limits := cfg.Limits
	defaults := DefaultLimits()
	if limits.PerInvokerPerHour == 0 {
		limits.PerInvokerPerHour = defaults.PerInvokerPerHour
	}
	if limits.PerInvokerConcurrent == 0 {
		limits.PerInvokerConcurrent = defaults.PerInvokerConcurrent
	}
	if limits.PerConversationPerHour == 0 {
		limits.PerConversationPerHour = defaults.PerConversationPerHour
	}
	if limits.PerInvokerPerHour < 1 || limits.PerInvokerConcurrent < 1 || limits.PerConversationPerHour < 1 || limits.DailySpendMicros < 0 {
		return nil, fmt.Errorf("%w: invalid persona limits", ErrInvalid)
	}
	return &Service{now: clock, limits: limits, membership: cfg.Membership, manager: cfg.Manager, bilateral: cfg.Bilateral, outerBudget: cfg.OuterBudget, projector: cfg.Projector, ownerStatus: cfg.OwnerStatus, notifier: cfg.Notifier, revocations: cfg.Revocations,
		installations: make(map[string]*installationState), personas: make(map[string]PersonaVersion), tasks: make(map[string]*Task), invokerHour: make(map[string]*windowCounter), conversationHour: make(map[string]*windowCounter), spend: make(map[string]*spendCounter), leases: make(map[string]*MentionLease), membershipState: make(map[string]MembershipSnapshot)}, nil
}

func (s *Service) Install(ctx context.Context, req InstallRequest) (Installation, error) {
	if s == nil {
		return Installation{}, ErrInvalid
	}
	if err := validateInstallRequest(req); err != nil {
		return Installation{}, err
	}
	// Manager status must come from chat's authority source. A request field is
	// caller-controlled and cannot prove that the installer manages this room.
	if s.manager == nil {
		return Installation{}, ErrDenied
	}
	manager, err := s.manager.IsChannelManager(ctx, req.Conversation.TenantID, req.Conversation.ConversationID, req.InstallerID)
	if err != nil {
		return Installation{}, err
	}
	if !manager {
		return Installation{}, ErrDenied
	}
	var currentMembership MembershipSnapshot
	hasCurrentMembership := false
	if s.membership != nil {
		currentMembership, err = s.membership.Snapshot(ctx, req.Conversation.TenantID, req.Conversation.ConversationID)
		if err != nil {
			return Installation{}, err
		}
		if currentMembership.Revision == 0 {
			return Installation{}, ErrUnavailable
		}
		currentMembership.ExternalMembers = currentMembership.ExternalMembers || req.Conversation.ExternalMembers || req.Conversation.Class == ChannelExternal
		currentMembership.CrossCompanyMembers = currentMembership.CrossCompanyMembers || req.Conversation.CrossCompanyMembers || req.Conversation.Class == ChannelCrossCompany
		req.Conversation.ExternalMembers = currentMembership.ExternalMembers
		req.Conversation.CrossCompanyMembers = currentMembership.CrossCompanyMembers
		hasCurrentMembership = true
	}
	if err := validatePlacement(ctx, req, s.bilateral); err != nil {
		return Installation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	conversationKey := req.Conversation.TenantID + "\x00" + req.Conversation.ConversationID
	if current, exists := s.membershipState[conversationKey]; hasCurrentMembership {
		if exists && (current.Revision > currentMembership.Revision || current.Revision == currentMembership.Revision && !sameMembership(current, currentMembership)) {
			return Installation{}, ErrFenced
		}
	}
	active := 0
	for _, existing := range s.installations {
		if existing.installation.TenantID == req.Persona.TenantID && existing.installation.ConversationID == req.Conversation.ConversationID && existing.installation.Status == InstallationActive {
			active++
		}
	}
	if active >= 5 {
		return Installation{}, fmt.Errorf("%w: at most five personas per conversation", ErrDenied)
	}
	s.sequence++
	now := s.now().UTC()
	id := fmt.Sprintf("persona-install-%d", s.sequence)
	installation := Installation{ID: id, TenantID: req.Persona.TenantID, PersonaID: req.Persona.PersonaID, PersonaVersion: req.Persona.Version, ConversationID: req.Conversation.ConversationID, ConversationClass: req.Conversation.Class, InstallerID: req.InstallerID, Policy: clonePolicy(req.Policy), Status: InstallationActive, Revision: 1, RevocationEpoch: 1, CreatedAt: now, UpdatedAt: now}
	s.installations[id] = &installationState{installation: installation, persona: clonePersona(req.Persona)}
	s.personas[personaKey(req.Persona.TenantID, req.Persona.PersonaID)] = clonePersona(req.Persona)
	external := req.Conversation.ExternalMembers || req.Conversation.Class == ChannelExternal
	crossCompany := req.Conversation.CrossCompanyMembers || req.Conversation.Class == ChannelCrossCompany
	if current, exists := s.membershipState[conversationKey]; hasCurrentMembership && (!exists || currentMembership.Revision >= current.Revision) {
		s.membershipState[conversationKey] = cloneMembership(currentMembership)
	} else if !exists && (req.Conversation.MembershipRevision > 0 || external || crossCompany) {
		s.membershipState[conversationKey] = MembershipSnapshot{Revision: req.Conversation.MembershipRevision, ExternalMembers: external, CrossCompanyMembers: crossCompany}
	}
	return cloneInstallation(installation), nil
}

func (s *Service) InstallPersona(ctx context.Context, req InstallRequest) (Installation, error) {
	return s.Install(ctx, req)
}

func validateInstallRequest(req InstallRequest) error {
	if strings.TrimSpace(req.Persona.TenantID) == "" || strings.TrimSpace(req.Persona.PersonaID) == "" || req.Persona.Version == 0 || req.Persona.Lifecycle != LifecyclePublished || strings.TrimSpace(req.Conversation.TenantID) == "" || req.Conversation.TenantID != req.Persona.TenantID || strings.TrimSpace(req.Conversation.ConversationID) == "" || strings.TrimSpace(req.InstallerID) == "" {
		return fmt.Errorf("%w: published persona, tenant, conversation and installer are required", ErrInvalid)
	}
	if !req.Persona.TierCeiling.Valid() || !req.Policy.MaxTier.Valid() || req.Policy.MaxTier > req.Persona.TierCeiling {
		return fmt.Errorf("%w: channel tier exceeds persona ceiling", ErrDenied)
	}
	if !containsClass(req.Persona.AllowedChannelClasses, req.Conversation.Class) || len(req.Policy.AllowedChannelClasses) == 0 || !containsClass(req.Policy.AllowedChannelClasses, req.Conversation.Class) {
		return fmt.Errorf("%w: conversation class is outside the persona channel policy", ErrDenied)
	}
	if !subset(req.Policy.AllowedDataClasses, req.Persona.DataClasses) {
		return fmt.Errorf("%w: channel data classes exceed persona data reach", ErrDenied)
	}
	return nil
}

func validatePlacement(ctx context.Context, req InstallRequest, bilateral BilateralPolicy) error {
	external := req.Conversation.ExternalMembers || req.Conversation.Class == ChannelExternal
	crossCompany := req.Conversation.CrossCompanyMembers || req.Conversation.Class == ChannelCrossCompany
	needsBilateral := external || crossCompany
	if !needsBilateral {
		return nil
	}
	if external && !req.Policy.AllowExternalMembers {
		return fmt.Errorf("%w: external membership is disabled", ErrDenied)
	}
	if crossCompany && !req.Policy.AllowCrossCompanyMembers {
		return fmt.Errorf("%w: cross-company membership is disabled", ErrDenied)
	}
	if bilateral == nil {
		return fmt.Errorf("%w: bilateral policy is required", ErrDenied)
	}
	ok, err := bilateral.AllowsPersona(ctx, req.Persona.TenantID, req.Conversation.ConversationID, req.Persona.PersonaID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: AGENT-037 bilateral policy refused placement", ErrDenied)
	}
	return nil
}

func (s *Service) Installation(_ context.Context, id string) (Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.installations[id]
	if !ok {
		return Installation{}, ErrNotFound
	}
	return cloneInstallation(state.installation), nil
}

// UpdateMembership is the commit callback from chat. It records the
// membership revision used to fence an invocation; chat remains the source of
// truth for membership itself.
func (s *Service) UpdateMembership(tenant, conversation string, snapshot MembershipSnapshot) error {
	if s == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(conversation) == "" || snapshot.Revision == 0 {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenant + "\x00" + conversation
	if current, ok := s.membershipState[key]; ok {
		if snapshot.Revision < current.Revision || snapshot.Revision == current.Revision && !sameMembership(current, snapshot) {
			return ErrFenced
		}
	}
	s.membershipState[key] = cloneMembership(snapshot)
	return nil
}

func (s *Service) EffectivePolicy(ctx context.Context, id string) (PolicyDecision, error) {
	if s == nil {
		return PolicyDecision{}, ErrInvalid
	}
	s.mu.Lock()
	state, ok := s.installations[id]
	if !ok {
		s.mu.Unlock()
		return PolicyDecision{}, ErrNotFound
	}
	snapshot := state.installation
	persona := clonePersona(state.persona)
	membership, hasMembership := s.membershipState[snapshot.TenantID+"\x00"+snapshot.ConversationID]
	s.mu.Unlock()
	if snapshot.Status != InstallationActive || persona.Lifecycle != LifecyclePublished {
		return PolicyDecision{Allowed: false, Reason: "persona unavailable", RevocationEpoch: snapshot.RevocationEpoch}, ErrUnavailable
	}
	policy := clonePolicy(snapshot.Policy)
	if !hasMembership {
		membership = MembershipSnapshot{Revision: 0, ExternalMembers: false, CrossCompanyMembers: false, AllowedDataClasses: append([]string(nil), persona.DataClasses...)}
	}
	if s.membership != nil {
		var err error
		fresh, err := s.membership.Snapshot(ctx, snapshot.TenantID, snapshot.ConversationID)
		if err != nil {
			return PolicyDecision{}, err
		}
		if fresh.Revision == 0 {
			return PolicyDecision{}, ErrUnavailable
		}
		fresh.ExternalMembers = fresh.ExternalMembers || snapshot.ConversationClass == ChannelExternal
		fresh.CrossCompanyMembers = fresh.CrossCompanyMembers || snapshot.ConversationClass == ChannelCrossCompany
		// Keep a newer chat callback if it raced the read, and cache a newer
		// provider snapshot so every later invocation sees the recomputed floor.
		s.mu.Lock()
		key := snapshot.TenantID + "\x00" + snapshot.ConversationID
		current, exists := s.membershipState[key]
		if exists && current.Revision > fresh.Revision {
			membership = current
			hasMembership = true
		} else if exists && current.Revision == fresh.Revision && !sameMembership(current, fresh) {
			s.mu.Unlock()
			return PolicyDecision{}, ErrFenced
		} else {
			membership = cloneMembership(fresh)
			hasMembership = true
			s.membershipState[key] = cloneMembership(fresh)
		}
		s.mu.Unlock()
	}
	if !hasMembership {
		membership = MembershipSnapshot{Revision: 0, ExternalMembers: false, CrossCompanyMembers: false, AllowedDataClasses: append([]string(nil), persona.DataClasses...)}
	}
	policy.AlwaysPrivate = policy.AlwaysPrivate || persona.AlwaysPrivate
	policy.ConversationSearchAllowed = policy.ConversationSearchAllowed && persona.ConversationSearch
	externalMembership := membership.ExternalMembers || snapshot.ConversationClass == ChannelExternal
	crossCompanyMembership := membership.CrossCompanyMembers || snapshot.ConversationClass == ChannelCrossCompany
	if externalMembership && !policy.AllowExternalMembers {
		return PolicyDecision{Allowed: false, Reason: "external membership is outside channel policy", MembershipRevision: membership.Revision, RevocationEpoch: snapshot.RevocationEpoch}, ErrUnavailable
	}
	if crossCompanyMembership && !policy.AllowCrossCompanyMembers {
		return PolicyDecision{Allowed: false, Reason: "cross-company membership is outside channel policy", MembershipRevision: membership.Revision, RevocationEpoch: snapshot.RevocationEpoch}, ErrUnavailable
	}
	if externalMembership || crossCompanyMembership {
		if s.bilateral == nil {
			return PolicyDecision{Allowed: false, Reason: "bilateral policy is required for current membership", MembershipRevision: membership.Revision, RevocationEpoch: snapshot.RevocationEpoch}, ErrUnavailable
		}
		allowed, err := s.bilateral.AllowsPersona(ctx, snapshot.TenantID, snapshot.ConversationID, persona.PersonaID)
		if err != nil {
			return PolicyDecision{}, err
		}
		if !allowed {
			return PolicyDecision{Allowed: false, Reason: "bilateral policy refused current membership", MembershipRevision: membership.Revision, RevocationEpoch: snapshot.RevocationEpoch}, ErrUnavailable
		}
	}
	if (externalMembership || crossCompanyMembership) && len(membership.AllowedDataClasses) == 0 {
		policy.AllowedDataClasses = nil
	} else if len(membership.AllowedDataClasses) > 0 {
		policy.AllowedDataClasses = intersect(policy.AllowedDataClasses, membership.AllowedDataClasses)
	}
	return PolicyDecision{Allowed: true, Policy: policy, MembershipRevision: membership.Revision, RevocationEpoch: snapshot.RevocationEpoch}, nil
}

func (s *Service) AdmitMention(ctx context.Context, req MentionRequest) (*MentionLease, error) {
	if s == nil || req.At.IsZero() {
		if s == nil {
			return nil, ErrInvalid
		}
		req.At = s.now()
	}
	decision, err := s.EffectivePolicy(ctx, req.InstallationID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	state, ok := s.installations[req.InstallationID]
	if !ok {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	latestMembership, hasMembership := s.membershipState[req.TenantID+"\x00"+req.ConversationID]
	if req.TenantID != state.installation.TenantID || req.PersonaID != state.installation.PersonaID || req.ConversationID != state.installation.ConversationID || !decision.Allowed || state.installation.RevocationEpoch != decision.RevocationEpoch || (hasMembership && latestMembership.Revision != decision.MembershipRevision) {
		s.mu.Unlock()
		return nil, ErrFenced
	}
	if req.EstimatedSpend < 0 {
		s.mu.Unlock()
		return nil, ErrInvalid
	}
	period := req.At.UTC().Truncate(time.Hour)
	ikey := req.TenantID + "\x00" + req.InvokerID + "\x00" + req.PersonaID
	count := s.invokerHour[ikey]
	if count == nil || !count.period.Equal(period) {
		count = &windowCounter{period: period}
		s.invokerHour[ikey] = count
	}
	if count.count >= s.limits.PerInvokerPerHour {
		retry := period.Add(time.Hour).Sub(req.At.UTC())
		s.mu.Unlock()
		return nil, &LimitError{Reason: ReasonInvokerRate, RetryAfter: retry, Ephemeral: true}
	}
	concurrent := 0
	for _, lease := range s.leases {
		if !lease.closed && lease.request.TenantID == req.TenantID && lease.request.InvokerID == req.InvokerID && lease.request.PersonaID == req.PersonaID {
			concurrent++
		}
	}
	if concurrent >= s.limits.PerInvokerConcurrent {
		s.mu.Unlock()
		return nil, &LimitError{Reason: ReasonInvokerConcurrency, RetryAfter: time.Minute, Ephemeral: true}
	}
	ckey := req.TenantID + "\x00" + req.ConversationID
	conv := s.conversationHour[ckey]
	if conv == nil || !conv.period.Equal(period) {
		conv = &windowCounter{period: period}
		s.conversationHour[ckey] = conv
	}
	if conv.count >= s.limits.PerConversationPerHour {
		retry := period.Add(time.Hour).Sub(req.At.UTC())
		s.mu.Unlock()
		return nil, &LimitError{Reason: ReasonConversationRate, RetryAfter: retry, Ephemeral: true}
	}
	spendLimit := s.limits.DailySpendMicros
	if state.persona.DailySpendCeiling > 0 && (spendLimit == 0 || state.persona.DailySpendCeiling < spendLimit) {
		spendLimit = state.persona.DailySpendCeiling
	}
	spendKey := req.TenantID + "\x00" + req.PersonaID
	day := req.At.UTC().Truncate(24 * time.Hour)
	spend := s.spend[spendKey]
	if spend == nil || !spend.period.Equal(day) {
		spend = &spendCounter{period: day}
		s.spend[spendKey] = spend
	}
	if spendLimit > 0 && spend.used+spend.reserved+req.EstimatedSpend > spendLimit {
		retry := day.Add(24 * time.Hour).Sub(req.At.UTC())
		s.mu.Unlock()
		return nil, &LimitError{Reason: ReasonPersonaSpend, RetryAfter: retry, Ephemeral: true}
	}
	count.count++
	conv.count++
	spend.reserved += req.EstimatedSpend
	s.sequence++
	lease := &MentionLease{service: s, ID: fmt.Sprintf("mention-%d", s.sequence), request: req, installation: req.InstallationID, epoch: state.installation.RevocationEpoch, estimatedSpend: req.EstimatedSpend}
	s.leases[lease.ID] = lease
	s.mu.Unlock()
	if s.outerBudget != nil {
		reservation, reserveErr := s.outerBudget.Reserve(ctx, BudgetRequest{TenantID: req.TenantID, InvokerID: req.InvokerID, PersonaID: req.PersonaID, ConversationID: req.ConversationID, EstimatedSpend: req.EstimatedSpend})
		if reserveErr != nil {
			_ = lease.Release()
			return nil, &LimitError{Reason: ReasonOuterBudget, RetryAfter: time.Minute, Ephemeral: true}
		}
		lease.outer = reservation
	}
	return lease, nil
}

func (s *Service) commitMention(lease *MentionLease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lease.closed {
		return ErrFenced
	}
	state, ok := s.installations[lease.installation]
	if !ok || state.installation.Status != InstallationActive || state.installation.RevocationEpoch != lease.epoch || state.persona.Lifecycle != LifecyclePublished {
		return ErrFenced
	}
	if _, ok := s.leases[lease.ID]; !ok {
		return ErrFenced
	}
	return nil
}

func (s *Service) closeMention(lease *MentionLease, actual int64, settle bool) error {
	s.mu.Lock()
	if lease.closed {
		s.mu.Unlock()
		return ErrFenced
	}
	if settle && (actual < 0 || actual > lease.estimatedSpend) {
		s.mu.Unlock()
		return fmt.Errorf("%w: actual spend exceeds reservation", ErrInvalid)
	}
	delete(s.leases, lease.ID)
	lease.closed = true
	period := lease.request.At.UTC().Truncate(time.Hour)
	ikey := lease.request.TenantID + "\x00" + lease.request.InvokerID + "\x00" + lease.request.PersonaID
	if counter := s.invokerHour[ikey]; counter != nil && counter.period.Equal(period) { /* rate count remains consumed */
	}
	ckey := lease.request.TenantID + "\x00" + lease.request.ConversationID
	if counter := s.conversationHour[ckey]; counter != nil && counter.period.Equal(period) { /* rate count remains consumed */
	}
	day := lease.request.At.UTC().Truncate(24 * time.Hour)
	skey := lease.request.TenantID + "\x00" + lease.request.PersonaID
	if spend := s.spend[skey]; spend != nil && spend.period.Equal(day) {
		spend.reserved -= lease.estimatedSpend
		if settle {
			spend.used += actual
		}
	}
	outer := lease.outer
	s.mu.Unlock()
	if outer != nil {
		if settle {
			return outer.Settle(actual)
		}
		return outer.Release()
	}
	return nil
}

// RegisterTask lets lifecycle fencing pause local projections as well as
// notifying a durable agentrun adapter.
func (s *Service) RegisterTask(task Task) error {
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.TenantID) == "" || task.State == "" {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[task.ID]; exists {
		return ErrInvalid
	}
	copy := task
	s.tasks[task.ID] = &copy
	return nil
}

func (s *Service) Task(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	return *task, nil
}

type SuspendScope string

const (
	SuspendVersion      SuspendScope = "VERSION"
	SuspendPersona      SuspendScope = "PERSONA"
	SuspendInstallation SuspendScope = "INSTALLATION"
	SuspendTenant       SuspendScope = "TENANT"
)

type SuspendRequest struct {
	Scope          SuspendScope
	TenantID       string
	PersonaID      string
	PersonaVersion uint64
	InstallationID string
	Reason         string
	At             time.Time
}

func (s *Service) Suspend(ctx context.Context, req SuspendRequest) error {
	if s == nil || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.Reason) == "" {
		return ErrInvalid
	}
	if req.At.IsZero() {
		req.At = s.now()
	}
	if s.revocations != nil {
		if _, err := s.revocations.Bump(ctx, req.TenantID, req.PersonaID, req.PersonaVersion, req.InstallationID, req.Reason); err != nil {
			return err
		}
	}
	s.mu.Lock()
	var affected []PauseRequest
	for id, state := range s.installations {
		if !matchesScope(id, state.installation, req) || state.installation.Status != InstallationActive {
			continue
		}
		state.installation.Status = InstallationSuspended
		state.installation.Revision++
		state.installation.RevocationEpoch++
		state.installation.UpdatedAt = req.At.UTC()
		if req.Scope != SuspendInstallation {
			state.persona.Lifecycle = LifecycleSuspended
		}
		affected = append(affected, PauseRequest{TenantID: state.installation.TenantID, PersonaID: state.installation.PersonaID, InstallationID: id, Reason: req.Reason, RevocationEpoch: state.installation.RevocationEpoch})
	}
	if req.Scope == SuspendPersona || req.Scope == SuspendVersion || req.Scope == SuspendTenant {
		for key, persona := range s.personas {
			if persona.TenantID != req.TenantID || !matchesPersona(persona, req) {
				continue
			}
			persona.Lifecycle = LifecycleSuspended
			s.personas[key] = persona
		}
	}
	for _, pause := range affected {
		for _, task := range s.tasks {
			if task.TenantID == pause.TenantID && (pause.InstallationID == task.InstallationID || (pause.PersonaID == task.PersonaID && req.Scope != SuspendInstallation)) && task.State != TaskPaused {
				task.State = TaskPaused
				task.PauseReason = req.Reason
				task.ApprovalOpen = false
				task.CardExpiresAt = req.At.UTC()
			}
		}
	}
	projector, notifier := s.projector, s.notifier
	s.mu.Unlock()
	for _, pause := range affected {
		if projector != nil {
			_ = projector.Pause(ctx, pause)
			_ = projector.VoidApprovals(ctx, pause)
			_ = projector.ExpireCards(ctx, pause)
		}
	}
	_ = notifier
	return nil
}

func (s *Service) SuspendPersona(ctx context.Context, tenant, persona string, reason string) error {
	return s.Suspend(ctx, SuspendRequest{Scope: SuspendPersona, TenantID: tenant, PersonaID: persona, Reason: reason})
}
func (s *Service) SuspendInstallation(ctx context.Context, id, reason string) error {
	s.mu.Lock()
	state, ok := s.installations[id]
	s.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	return s.Suspend(ctx, SuspendRequest{Scope: SuspendInstallation, TenantID: state.installation.TenantID, InstallationID: id, Reason: reason})
}
func (s *Service) KillTenant(ctx context.Context, tenant, reason string) error {
	return s.Suspend(ctx, SuspendRequest{Scope: SuspendTenant, TenantID: tenant, Reason: reason})
}

// RemoveInstallation is an explicit uninstall. It keeps the installation's
// history and fences its tasks, rather than deleting state keyed by the
// persona or conversation and accidentally removing a sibling placement.
func (s *Service) RemoveInstallation(ctx context.Context, id, installerID string) error {
	if s == nil {
		return ErrInvalid
	}
	s.mu.Lock()
	state, ok := s.installations[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	installation := state.installation
	s.mu.Unlock()
	if installerID == "" {
		return ErrDenied
	}
	if s.manager == nil {
		return ErrDenied
	}
	manager, err := s.manager.IsChannelManager(ctx, installation.TenantID, installation.ConversationID, installerID)
	if err != nil {
		return err
	}
	if !manager {
		return ErrDenied
	}
	return s.removeInstallation(ctx, installation, "INSTALLATION_REMOVED")
}

func (s *Service) Uninstall(ctx context.Context, id, installerID string) error {
	return s.RemoveInstallation(ctx, id, installerID)
}

func (s *Service) removeInstallation(ctx context.Context, installation Installation, reason string) error {
	if s.revocations != nil {
		if _, err := s.revocations.Bump(ctx, installation.TenantID, installation.PersonaID, installation.PersonaVersion, installation.ID, reason); err != nil {
			return err
		}
	}
	s.mu.Lock()
	state, ok := s.installations[installation.ID]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	state.installation.Status = InstallationKilled
	state.installation.Revision++
	state.installation.RevocationEpoch++
	state.installation.UpdatedAt = s.now().UTC()
	pause := PauseRequest{TenantID: state.installation.TenantID, PersonaID: state.installation.PersonaID, InstallationID: state.installation.ID, Reason: reason, RevocationEpoch: state.installation.RevocationEpoch}
	for _, task := range s.tasks {
		if task.InstallationID == state.installation.ID {
			task.State = TaskPaused
			task.PauseReason = reason
			task.ApprovalOpen = false
			task.CardExpiresAt = state.installation.UpdatedAt
		}
	}
	projector := s.projector
	s.mu.Unlock()
	if projector != nil {
		_ = projector.Pause(ctx, pause)
		_ = projector.VoidApprovals(ctx, pause)
		_ = projector.ExpireCards(ctx, pause)
	}
	return nil
}

func matchesScope(id string, installation Installation, req SuspendRequest) bool {
	if installation.TenantID != req.TenantID {
		return false
	}
	switch req.Scope {
	case SuspendTenant:
		return true
	case SuspendInstallation:
		return id == req.InstallationID
	case SuspendPersona, SuspendVersion:
		return installation.PersonaID == req.PersonaID && (req.Scope == SuspendPersona || installation.PersonaVersion == req.PersonaVersion)
	default:
		return false
	}
}
func matchesPersona(persona PersonaVersion, req SuspendRequest) bool {
	return persona.PersonaID == req.PersonaID && (req.Scope == SuspendPersona || persona.Version == req.PersonaVersion)
}

// BeginStep is the invocation fence used immediately before every task step.
// It performs no model or owner call; callers must receive nil before doing
// any work.
func (s *Service) BeginStep(ctx context.Context, installationID string) (PolicyDecision, error) {
	decision, err := s.EffectivePolicy(ctx, installationID)
	if err != nil {
		return PolicyDecision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.installations[installationID]
	latestMembership, hasMembership := s.membershipState[state.installation.TenantID+"\x00"+state.installation.ConversationID]
	if !ok || state.installation.Status != InstallationActive || state.installation.RevocationEpoch != decision.RevocationEpoch || (hasMembership && latestMembership.Revision != decision.MembershipRevision) {
		return PolicyDecision{}, ErrFenced
	}
	return decision, nil
}

type ReassignmentWindow struct {
	TenantID, PersonaID, OwnerID string
	Deadline                     time.Time
}

func (s *Service) OwnerChanged(ctx context.Context, tenant, persona, owner string, at time.Time) (ReassignmentWindow, error) {
	if s == nil || tenant == "" || persona == "" || owner == "" {
		return ReassignmentWindow{}, ErrInvalid
	}
	if at.IsZero() {
		at = s.now()
	}
	window := ReassignmentWindow{TenantID: tenant, PersonaID: persona, OwnerID: owner, Deadline: at.UTC().Add(14 * 24 * time.Hour)}
	return window, nil
}

// SweepOrphans is deliberately status-driven, so a lost owner-change event
// cannot keep a persona alive indefinitely. Once the persisted invalid-since
// time reaches fourteen days, the persona is suspended and a notification is
// sent to the steward/tenant administrators by the adapter.
func (s *Service) SweepOrphans(ctx context.Context, now time.Time) error {
	if s == nil || s.ownerStatus == nil {
		return nil
	}
	if now.IsZero() {
		now = s.now()
	}
	s.mu.Lock()
	personas := make([]PersonaVersion, 0, len(s.personas))
	for _, persona := range s.personas {
		personas = append(personas, clonePersona(persona))
	}
	s.mu.Unlock()
	for _, persona := range personas {
		status, err := s.ownerStatus.ResolveOwner(ctx, persona.TenantID, persona.PersonaID, persona.OwnerID)
		if err != nil {
			continue
		}
		if status.Active && status.HasOwnerRole {
			continue
		}
		invalidSince := status.InvalidSince
		if invalidSince.IsZero() {
			invalidSince = now
		}
		if now.Before(invalidSince.Add(14 * 24 * time.Hour)) {
			continue
		}
		if err := s.SuspendPersona(ctx, persona.TenantID, persona.PersonaID, "OWNER_REASSIGNMENT_EXPIRED"); err != nil {
			return err
		}
		if s.notifier != nil {
			_ = s.notifier.Notify(ctx, Notification{TenantID: persona.TenantID, PersonaID: persona.PersonaID, OwnerID: persona.OwnerID, Reason: "OWNER_REASSIGNMENT_EXPIRED"})
		}
	}
	return nil
}

func personaKey(tenant, persona string) string { return tenant + "\x00" + persona }
func containsClass(values []ChannelClass, value ChannelClass) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func subset(values, allowed []string) bool {
	set := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		set[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}
func intersect(left, right []string) []string {
	set := make(map[string]struct{}, len(right))
	for _, value := range right {
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(left))
	for _, value := range left {
		if _, ok := set[value]; ok && !containsString(out, value) {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func clonePersona(persona PersonaVersion) PersonaVersion {
	persona.AllowedChannelClasses = append([]ChannelClass(nil), persona.AllowedChannelClasses...)
	persona.DataClasses = append([]string(nil), persona.DataClasses...)
	return persona
}
func clonePolicy(policy ChannelPersonaPolicy) ChannelPersonaPolicy {
	policy.AllowedDataClasses = append([]string(nil), policy.AllowedDataClasses...)
	policy.AllowedChannelClasses = append([]ChannelClass(nil), policy.AllowedChannelClasses...)
	return policy
}
func cloneMembership(snapshot MembershipSnapshot) MembershipSnapshot {
	snapshot.AllowedDataClasses = append([]string(nil), snapshot.AllowedDataClasses...)
	return snapshot
}

func sameMembership(left, right MembershipSnapshot) bool {
	if left.Revision != right.Revision || left.ExternalMembers != right.ExternalMembers || left.CrossCompanyMembers != right.CrossCompanyMembers || len(left.AllowedDataClasses) != len(right.AllowedDataClasses) {
		return false
	}
	leftClasses := append([]string(nil), left.AllowedDataClasses...)
	rightClasses := append([]string(nil), right.AllowedDataClasses...)
	sort.Strings(leftClasses)
	sort.Strings(rightClasses)
	for i := range leftClasses {
		if leftClasses[i] != rightClasses[i] {
			return false
		}
	}
	return true
}
func cloneInstallation(installation Installation) Installation {
	installation.Policy = clonePolicy(installation.Policy)
	return installation
}
