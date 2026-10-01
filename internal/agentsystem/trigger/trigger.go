package trigger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/authority"
)

var (
	ErrInvalid     = errors.New("agent trigger: invalid request")
	ErrUnavailable = errors.New("agent trigger: required owner or ledger unavailable")
	ErrDenied      = errors.New("agent trigger: admission denied")
	ErrDisabled    = errors.New("agent trigger: subscription is not opted in")
	ErrPaused      = errors.New("agent trigger: subscription is paused")
	ErrLoop        = errors.New("agent trigger: cause chain would recurse")
	ErrReplay      = errors.New("agent trigger: event was already admitted")
	ErrBudget      = errors.New("agent trigger: budget or concurrency ceiling reached")
	ErrConflict    = errors.New("agent trigger: subscription revision changed")
)

const hardMaxCauseDepth = 8

// EventClass is an owner-defined committed chat event category.
type EventClass string

const (
	// EventPostCreated identifies a committed post-created outbox event.
	EventPostCreated EventClass = "POST_CREATED"
)

// OriginKind identifies the authenticated author type stored by Chat.
type OriginKind string

const (
	// OriginHuman identifies a human-authored committed post.
	OriginHuman OriginKind = "HUMAN"
	// OriginAgent identifies an agent-authored committed post.
	OriginAgent OriginKind = "AGENT"
)

// Budget contains one atomic admission ceiling. FairShare is applied per
// installation; Channel is shared by every subscription in the channel.
// Implementations must reserve all dimensions atomically before model work.
type Budget struct {
	EventsPerMinute     uint64
	EventsPerDay        uint64
	CostMicrosPerMinute uint64
	CostMicrosPerDay    uint64
	MaxConcurrent       uint32
	Cooldown            time.Duration
}

// Subscription is a current Chat-owned opt-in and its immutable run ceiling.
// ResolveSubscription must load current state and must not trust request data.
type Subscription struct {
	TenantID       string
	ChannelID      string
	InstallationID string
	AgentID        string
	AgentVersion   string
	SponsorID      string
	Purpose        string
	AudienceID     string
	Revision       uint64
	Enabled        bool
	Paused         bool
	EventClasses   []EventClass
	MaxCauseDepth  uint8
	ChannelBudget  Budget
	FairShare      Budget
}

// CommittedEvent is resolved by the Chat outbox owner from an opaque outbox
// ID. CauseChain contains installation IDs from causal ancestors and, for an
// agent-authored event, ends with AuthorInstallationID.
type CommittedEvent struct {
	OutboxID             string
	PostID               string
	TenantID             string
	ChannelID            string
	Class                EventClass
	Origin               OriginKind
	AuthorInstallationID string
	CauseChain           []string
	EstimatedCostMicros  uint64
}

// SubscriptionResolver reads the current opt-in, revision, and policy from
// Chat-owned state.
type SubscriptionResolver interface {
	ResolveSubscription(context.Context, string, string, string) (Subscription, error)
}

// OutboxReader resolves only committed events in the requested tenant.
type OutboxReader interface {
	ResolveCommittedEvent(context.Context, string, string) (CommittedEvent, error)
}

// AccessChecker rechecks current channel membership, classification, and
// audience eligibility against owner state immediately before reservation.
type AccessChecker interface {
	CheckTriggerAccess(context.Context, Subscription, CommittedEvent) error
}

// Reservation is the complete input to one atomic dedupe, cooldown, fair
// share, channel budget, and concurrency reservation.
type Reservation struct {
	SourceKey            string
	SourceKind           agentrun.SourceKind
	CauseID              string
	TenantID             string
	ChannelID            string
	InstallationID       string
	AgentID              string
	AgentVersion         string
	SponsorID            string
	Purpose              string
	AudienceID           string
	SubscriptionRevision uint64
	CauseDepth           uint8
	EstimatedCostMicros  uint64
	OccurredAt           time.Time
	ChannelBudget        Budget
	FairShare            Budget
	Mode                 authority.RunMode
	TierCeiling          authority.Tier
}

// AdmissionLedger reserves dedupe, rate, cost, cooldown, and concurrency in
// one atomic operation. Production adapters must durably enforce every field.
type AdmissionLedger interface {
	ReserveTrigger(context.Context, Reservation) error
}

// Actor is the authenticated control-plane principal requesting a pause.
type Actor struct {
	TenantID    string
	PrincipalID string
}

// PauseAuthorizer resolves current manager authority for the channel.
type PauseAuthorizer interface {
	AuthorizeTriggerPause(context.Context, Actor, Subscription) error
}

// PauseWriter persists a pause change with compare-and-swap on Subscription.Revision
// and atomically rechecks the actor's current manager grant.
type PauseWriter interface {
	SetTriggerPaused(context.Context, Actor, Subscription, bool) error
}

// Service validates owner-resolved trigger evidence and delegates atomic
// admission to the shared trigger ledger. It has no direct model or chat writer.
type Service struct {
	subscriptions SubscriptionResolver
	outbox        OutboxReader
	access        AccessChecker
	ledger        AdmissionLedger
	pauseAuth     PauseAuthorizer
	pauseWriter   PauseWriter
	now           func() time.Time
}

// New requires every owner and admission boundary; missing boundaries fail closed.
func New(subscriptions SubscriptionResolver, outbox OutboxReader, access AccessChecker, ledger AdmissionLedger, pauseAuth PauseAuthorizer, pauseWriter PauseWriter, now func() time.Time) (*Service, error) {
	if subscriptions == nil || outbox == nil || access == nil || ledger == nil || pauseAuth == nil || pauseWriter == nil || now == nil {
		return nil, fmt.Errorf("%w: subscription, outbox, access, ledger, pause, and clock ports are required", ErrUnavailable)
	}
	return &Service{subscriptions: subscriptions, outbox: outbox, access: access, ledger: ledger, pauseAuth: pauseAuth, pauseWriter: pauseWriter, now: now}, nil
}

// Admit resolves committed owner state, rejects disabled, stale, over-budget,
// or recursive events, then atomically reserves them before inference starts.
func (s *Service) Admit(ctx context.Context, tenantID, channelID, installationID, outboxID string) (Reservation, error) {
	if s == nil || s.subscriptions == nil || s.outbox == nil || s.access == nil || s.ledger == nil || s.now == nil {
		return Reservation{}, ErrUnavailable
	}
	if ctx == nil {
		return Reservation{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Reservation{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if !validID(tenantID, 128) || !validID(channelID, 128) || !validID(installationID, 128) || !validID(outboxID, 256) {
		return Reservation{}, ErrInvalid
	}
	sub, err := s.subscriptions.ResolveSubscription(ctx, tenantID, channelID, installationID)
	if err != nil {
		return Reservation{}, fmt.Errorf("%w: resolve subscription: %w", ErrUnavailable, err)
	}
	if err := validateSubscription(sub, tenantID, channelID, installationID); err != nil {
		return Reservation{}, err
	}
	event, err := s.outbox.ResolveCommittedEvent(ctx, tenantID, outboxID)
	if err != nil {
		return Reservation{}, fmt.Errorf("%w: resolve committed event: %w", ErrUnavailable, err)
	}
	if err := validateEvent(event, tenantID, channelID, outboxID, sub); err != nil {
		return Reservation{}, err
	}
	if err := s.access.CheckTriggerAccess(ctx, sub, event); err != nil {
		return Reservation{}, fmt.Errorf("%w: current channel access: %w", ErrDenied, err)
	}
	now := s.now()
	if now.IsZero() {
		return Reservation{}, fmt.Errorf("%w: clock returned zero time", ErrUnavailable)
	}
	source, err := (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, agentrun.Request{
		Source: agentrun.SourceIdentity{TenantID: tenantID, Kind: agentrun.SourceChat, Ref: event.OutboxID},
		Agent:  agentrun.VersionRef{AgentID: sub.AgentID}, InstallationID: sub.InstallationID,
	})
	if err != nil {
		return Reservation{}, fmt.Errorf("%w: canonical chat source: %w", ErrDenied, err)
	}
	request := reservationFor(sub, event, now.UTC(), source.Key)
	if err := s.ledger.ReserveTrigger(ctx, request); err != nil {
		if errors.Is(err, ErrReplay) || errors.Is(err, ErrBudget) || errors.Is(err, ErrConflict) {
			return Reservation{}, err
		}
		return Reservation{}, fmt.Errorf("%w: atomic reservation: %w", ErrUnavailable, err)
	}
	return request, nil
}

// SetPaused requires current subscription revision and manager authorization;
// the owner store must compare-and-swap the revision before writing.
func (s *Service) SetPaused(ctx context.Context, actor Actor, tenantID, channelID, installationID string, expectedRevision uint64, paused bool) error {
	if s == nil || s.subscriptions == nil || s.pauseAuth == nil || s.pauseWriter == nil {
		return ErrUnavailable
	}
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if !validID(actor.TenantID, 128) || !validID(actor.PrincipalID, 128) || actor.TenantID != tenantID || !validID(channelID, 128) || !validID(installationID, 128) || expectedRevision == 0 {
		return ErrDenied
	}
	sub, err := s.subscriptions.ResolveSubscription(ctx, tenantID, channelID, installationID)
	if err != nil {
		return fmt.Errorf("%w: resolve subscription: %w", ErrUnavailable, err)
	}
	if err := validateSubscriptionIdentity(sub, tenantID, channelID, installationID); err != nil {
		return err
	}
	if sub.Revision != expectedRevision {
		return ErrConflict
	}
	if err := s.pauseAuth.AuthorizeTriggerPause(ctx, actor, sub); err != nil {
		return fmt.Errorf("%w: pause authority: %w", ErrDenied, err)
	}
	if err := s.pauseWriter.SetTriggerPaused(ctx, actor, sub, paused); err != nil {
		if errors.Is(err, ErrDenied) || errors.Is(err, ErrConflict) {
			return err
		}
		return fmt.Errorf("%w: persist pause with revision: %w", ErrUnavailable, err)
	}
	return nil
}

func validateSubscription(sub Subscription, tenantID, channelID, installationID string) error {
	if err := validateSubscriptionIdentity(sub, tenantID, channelID, installationID); err != nil {
		return err
	}
	if !sub.Enabled {
		return ErrDisabled
	}
	if sub.Paused {
		return ErrPaused
	}
	if !validID(sub.AgentID, 128) || !validID(sub.AgentVersion, 128) || !validID(sub.SponsorID, 128) || !validID(sub.Purpose, 128) || !validID(sub.AudienceID, 128) || sub.MaxCauseDepth == 0 || sub.MaxCauseDepth > hardMaxCauseDepth || !validBudget(sub.ChannelBudget) || !validBudget(sub.FairShare) || !shareWithinChannel(sub.FairShare, sub.ChannelBudget) {
		return fmt.Errorf("%w: incomplete or unsafe current subscription", ErrDenied)
	}
	if len(sub.EventClasses) == 0 {
		return fmt.Errorf("%w: no event classes are explicitly subscribed", ErrDenied)
	}
	seen := make(map[EventClass]struct{}, len(sub.EventClasses))
	for _, class := range sub.EventClasses {
		if class != EventPostCreated {
			return fmt.Errorf("%w: unsupported event class %q", ErrDenied, class)
		}
		if _, duplicate := seen[class]; duplicate {
			return fmt.Errorf("%w: duplicate event class", ErrDenied)
		}
		seen[class] = struct{}{}
	}
	return nil
}

func validateSubscriptionIdentity(sub Subscription, tenantID, channelID, installationID string) error {
	if !validID(sub.TenantID, 128) || !validID(sub.ChannelID, 128) || !validID(sub.InstallationID, 128) || sub.TenantID != tenantID || sub.ChannelID != channelID || sub.InstallationID != installationID || sub.Revision == 0 {
		return fmt.Errorf("%w: subscription identity or revision mismatch", ErrDenied)
	}
	return nil
}

func validateEvent(event CommittedEvent, tenantID, channelID, outboxID string, sub Subscription) error {
	if !validID(event.OutboxID, 256) || !validID(event.PostID, 256) || event.OutboxID != outboxID || event.TenantID != tenantID || event.ChannelID != channelID || event.EstimatedCostMicros == 0 || event.EstimatedCostMicros > sub.FairShare.CostMicrosPerMinute || event.EstimatedCostMicros > sub.FairShare.CostMicrosPerDay || event.EstimatedCostMicros > sub.ChannelBudget.CostMicrosPerMinute || event.EstimatedCostMicros > sub.ChannelBudget.CostMicrosPerDay {
		return fmt.Errorf("%w: committed event scope or cost estimate is invalid", ErrDenied)
	}
	if !containsClass(sub.EventClasses, event.Class) {
		return fmt.Errorf("%w: event class is not subscribed", ErrDenied)
	}
	if len(event.CauseChain) > hardMaxCauseDepth {
		return ErrLoop
	}
	seen := make(map[string]struct{}, len(event.CauseChain))
	for _, id := range event.CauseChain {
		if !validID(id, 128) {
			return fmt.Errorf("%w: invalid cause-chain installation", ErrLoop)
		}
		if _, duplicate := seen[id]; duplicate {
			return ErrLoop
		}
		if id == sub.InstallationID {
			return ErrLoop
		}
		seen[id] = struct{}{}
	}
	if uint8(len(event.CauseChain)) >= sub.MaxCauseDepth {
		return ErrLoop
	}
	switch event.Origin {
	case OriginHuman:
		if event.AuthorInstallationID != "" {
			return fmt.Errorf("%w: human outbox event carries an agent installation", ErrDenied)
		}
	case OriginAgent:
		if !validID(event.AuthorInstallationID, 128) || len(event.CauseChain) == 0 || event.CauseChain[len(event.CauseChain)-1] != event.AuthorInstallationID {
			return fmt.Errorf("%w: agent outbox event lacks its authenticated cause-chain origin", ErrDenied)
		}
	default:
		return fmt.Errorf("%w: only human and agent posts can trigger", ErrDenied)
	}
	return nil
}

func reservationFor(sub Subscription, event CommittedEvent, now time.Time, sourceKey string) Reservation {
	return Reservation{
		SourceKey: sourceKey, SourceKind: agentrun.SourceChat, CauseID: event.OutboxID,
		TenantID: sub.TenantID, ChannelID: sub.ChannelID, InstallationID: sub.InstallationID,
		AgentID: sub.AgentID, AgentVersion: sub.AgentVersion, SponsorID: sub.SponsorID,
		Purpose: sub.Purpose, AudienceID: sub.AudienceID, SubscriptionRevision: sub.Revision,
		CauseDepth: uint8(len(event.CauseChain)), EstimatedCostMicros: event.EstimatedCostMicros,
		OccurredAt: now, ChannelBudget: sub.ChannelBudget, FairShare: sub.FairShare,
		Mode: authority.ModeSponsored, TierCeiling: authority.TierCommunicate,
	}
}

func validBudget(b Budget) bool {
	return b.EventsPerMinute > 0 && b.EventsPerDay > 0 && b.CostMicrosPerMinute > 0 && b.CostMicrosPerDay > 0 && b.MaxConcurrent > 0 && b.Cooldown > 0 && b.Cooldown <= 24*time.Hour
}

func shareWithinChannel(share, channel Budget) bool {
	return share.EventsPerMinute <= channel.EventsPerMinute && share.EventsPerDay <= channel.EventsPerDay && share.CostMicrosPerMinute <= channel.CostMicrosPerMinute && share.CostMicrosPerDay <= channel.CostMicrosPerDay && share.MaxConcurrent <= channel.MaxConcurrent && share.Cooldown >= channel.Cooldown
}

func containsClass(classes []EventClass, wanted EventClass) bool {
	for _, class := range classes {
		if class == wanted {
			return true
		}
	}
	return false
}

func validID(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value
}
