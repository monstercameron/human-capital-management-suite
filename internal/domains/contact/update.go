package contact

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrInvalidUpdate reports an update request that cannot name a
	// change: a missing tenant, subject, actor, endpoint, value, purpose
	// or effective date.
	ErrInvalidUpdate = errors.New("contact: invalid contact update")

	// ErrProxyAuthorityMissing reports a proxy submission without its
	// authority: subject and actor evidence must never collapse.
	ErrProxyAuthorityMissing = errors.New("contact: proxy submission needs authority")

	// ErrTenantMismatch reports a subject outside the request tenant.
	ErrTenantMismatch = errors.New("contact: subject outside request tenant")

	// ErrDuplicateEndpoint reports an ADD for an endpoint that exists.
	ErrDuplicateEndpoint = errors.New("contact: endpoint already exists")

	// ErrUnknownEndpoint reports a REVISE, REMOVE or CORRECT for an
	// endpoint that does not exist.
	ErrUnknownEndpoint = errors.New("contact: unknown endpoint")

	// ErrStaleRevision reports an expected revision behind the head:
	// revalidate and reapprove instead of overwriting.
	ErrStaleRevision = errors.New("contact: expected revision is stale")

	// ErrApprovalRequired reports a policy-gated update without its
	// proposal-bound decision.
	ErrApprovalRequired = errors.New("contact: policy approval is required")

	// ErrUnknownVerification reports a receipt naming no tracked challenge.
	ErrUnknownVerification = errors.New("contact: unknown verification challenge")

	// ErrInvalidVerification reports a receipt that fails its challenge:
	// wrong subject, endpoint, purpose or token.
	ErrInvalidVerification = errors.New("contact: invalid verification receipt")

	// ErrLateVerification reports a receipt past its challenge expiry.
	ErrLateVerification = errors.New("contact: verification arrived late")

	// ErrDuplicateVerification reports a receipt for a consumed challenge.
	ErrDuplicateVerification = errors.New("contact: verification already consumed")
)

// UpdateOp is one typed contact operation.
type UpdateOp string

// Typed operations.
const (
	OpAdd     UpdateOp = "ADD"
	OpRevise  UpdateOp = "REVISE"
	OpRemove  UpdateOp = "REMOVE"
	OpCorrect UpdateOp = "CORRECT"
)

// SyncState is one external-consistency state. Queued provider work never
// closes consistency: only reconciliation does.
type SyncState string

// External-sync states.
const (
	SyncPending    SyncState = "PENDING_SYNC"
	SyncReconciled SyncState = "RECONCILED"
	SyncRepair     SyncState = "REPAIR_REQUIRED"
	SyncNone       SyncState = "NO_SYNC"
)

// UpdateRequest is one typed contact change.
type UpdateRequest struct {
	Tenant           values.TenantId
	Subject          values.EntityRef
	Actor            values.EntityRef
	ProxyAuthority   string
	EndpointID       string
	Kind             EndpointType
	Raw              string
	Purpose          string
	Priority         int
	Source           string
	Op               UpdateOp
	ExpectedRevision uint64
	EffectiveDate    string
	ChallengeID      string
	ChallengeToken   string
	RequireApproval  bool
	ApprovalRef      string
	GuardedFacts     map[string]string
	Simulate         bool
	Now              time.Time
}

// OutboxOp is one atomic outbox write bound to its revision.
type OutboxOp struct {
	EndpointID string
	Revision   uint64
	Digest     string
}

// Signal is one verified endpoint signal.
type Signal struct {
	ID         string
	EndpointID string
	Digest     string
}

// UpdateOutcome is one applied update: subject and actor stay separate,
// guarded facts echo byte-identical, and external sync stays pending
// until reconciliation.
type UpdateOutcome struct {
	Subject       values.EntityRef
	Actor         values.EntityRef
	Proxy         bool
	Revision      ContactEndpointRevision
	Verified      bool
	Outbox        []OutboxOp
	EffectiveDate string
	Deferred      bool
	TimerRef      string
	GuardedFacts  map[string]string
	ExternalSync  SyncState
	Signals       []Signal
}

// RepairTicket bounds one endpoint repair.
type RepairTicket struct {
	EndpointID  string
	Reason      string
	Attempts    int
	MaxAttempts int
}

// UpdateService applies typed contact updates over a Store. Signals
// buffer before subscription so signal-before-subscription never
// disappears; verification challenges track durably in memory.
type UpdateService struct {
	mu         sync.Mutex
	store      Store
	challenges map[string]*VerificationChallenge
	signals    map[string][]Signal
	subscribed map[string]bool
	consumed   map[string]bool
	repairs    map[string]*RepairTicket
}

// NewUpdateService returns a service over one store.
func NewUpdateService(store Store) *UpdateService {
	return &UpdateService{
		store:      store,
		challenges: make(map[string]*VerificationChallenge),
		signals:    make(map[string][]Signal),
		subscribed: make(map[string]bool),
		consumed:   make(map[string]bool),
		repairs:    make(map[string]*RepairTicket),
	}
}

// TrackChallenge registers one issued verification challenge.
func (s *UpdateService) TrackChallenge(id string, challenge VerificationChallenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	owned := challenge
	s.challenges[id] = &owned
}

// PublishSignal buffers one endpoint signal, subscribed or not.
func (s *UpdateService) PublishSignal(signal Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, buffered := range s.signals[signal.EndpointID] {
		if buffered.ID == signal.ID {
			return
		}
	}
	s.signals[signal.EndpointID] = append(s.signals[signal.EndpointID], signal)
}

// Subscribe drains one endpoint's buffered signals with dedupe.
func (s *UpdateService) Subscribe(endpointID string) []Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribed[endpointID] = true
	buffered := s.signals[endpointID]
	var out []Signal
	for _, signal := range buffered {
		if s.consumed[signal.ID] {
			continue
		}
		s.consumed[signal.ID] = true
		out = append(out, signal)
	}
	return out
}

func guardedCopy(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func headRevision(revisions []ContactEndpointRevision) (ContactEndpointRevision, bool) {
	if len(revisions) == 0 {
		return ContactEndpointRevision{}, false
	}
	head := revisions[0]
	for _, revision := range revisions[1:] {
		if revision.Revision > head.Revision {
			head = revision
		}
	}
	return head, true
}

// Update applies one typed update. Simulation returns the would-be
// revision without persisting anything. A future effective date defers
// with a timer ref instead of applying.
func (s *UpdateService) Update(ctx context.Context, req UpdateRequest) (UpdateOutcome, error) {
	if err := req.Subject.Validate(); err != nil {
		return UpdateOutcome{}, fmt.Errorf("contact: Update subject: %w", ErrInvalidUpdate)
	}
	if err := req.Actor.Validate(); err != nil {
		return UpdateOutcome{}, fmt.Errorf("contact: Update actor: %w", ErrInvalidUpdate)
	}
	if strings.TrimSpace(req.EndpointID) == "" || strings.TrimSpace(req.Raw) == "" ||
		strings.TrimSpace(req.Purpose) == "" || strings.TrimSpace(req.Source) == "" {
		return UpdateOutcome{}, fmt.Errorf("contact: Update: %w", ErrInvalidUpdate)
	}
	if req.Subject.Tenant != req.Tenant || req.Actor.Tenant != req.Tenant {
		return UpdateOutcome{}, fmt.Errorf("contact: Update: %w", ErrTenantMismatch)
	}
	proxy := req.Actor != req.Subject
	if proxy && strings.TrimSpace(req.ProxyAuthority) == "" {
		return UpdateOutcome{}, fmt.Errorf("contact: Update: %w", ErrProxyAuthorityMissing)
	}
	effective, err := values.ParseLocalDate(req.EffectiveDate)
	if err != nil {
		return UpdateOutcome{}, fmt.Errorf("contact: Update effective date: %w", ErrInvalidUpdate)
	}
	if req.RequireApproval && strings.TrimSpace(req.ApprovalRef) == "" {
		return UpdateOutcome{}, fmt.Errorf("contact: Update: %w", ErrApprovalRequired)
	}
	switch req.Op {
	case OpAdd, OpRevise, OpRemove, OpCorrect:
	default:
		return UpdateOutcome{}, fmt.Errorf("contact: Update: %w", ErrInvalidUpdate)
	}
	today, err := values.ParseLocalDate(req.Now.UTC().Format("2006-01-02"))
	if err != nil {
		return UpdateOutcome{}, fmt.Errorf("contact: Update clock: %w", ErrInvalidUpdate)
	}
	outcome := UpdateOutcome{
		Subject: req.Subject, Actor: req.Actor, Proxy: proxy,
		EffectiveDate: req.EffectiveDate, GuardedFacts: guardedCopy(req.GuardedFacts),
		ExternalSync: SyncNone,
	}
	if effective.Compare(today) > 0 {
		outcome.Deferred = true
		outcome.TimerRef = "timer:" + string(req.Tenant) + "/" + req.EndpointID + "/" + req.EffectiveDate
		return outcome, nil
	}
	revisions, err := s.store.ListEndpointRevisions(ctx, req.Tenant, req.EndpointID)
	if err != nil {
		if !errors.Is(err, ErrStoreNotFound) {
			return UpdateOutcome{}, err
		}
		revisions = nil
	}
	head, exists := headRevision(revisions)
	if req.Op == OpAdd && exists {
		return UpdateOutcome{}, fmt.Errorf("contact: Update %s: %w", req.EndpointID, ErrDuplicateEndpoint)
	}
	if req.Op != OpAdd && !exists {
		return UpdateOutcome{}, fmt.Errorf("contact: Update %s: %w", req.EndpointID, ErrUnknownEndpoint)
	}
	if req.Op != OpAdd && req.ExpectedRevision != head.Revision {
		return UpdateOutcome{}, fmt.Errorf("contact: Update %s: %w", req.EndpointID, ErrStaleRevision)
	}
	verified := false
	if req.ChallengeID != "" {
		status, verr := s.consumeChallenge(req, req.Simulate)
		if verr != nil {
			return UpdateOutcome{}, verr
		}
		verified = status == ChallengeVerified
	}
	revision, err := buildRevision(head, exists, req)
	if err != nil {
		return UpdateOutcome{}, err
	}
	chain := []ContactEndpointRevision{revision}
	if verified {
		// Verification appends its own evidence revision: the chain stays
		// gapless and the outbox binds the final digest.
		verifiedRevision, err := revision.MarkVerified()
		if err != nil {
			return UpdateOutcome{}, err
		}
		chain = append(chain, verifiedRevision)
		revision = verifiedRevision
	}
	outcome.Revision = revision
	outcome.Verified = verified
	outcome.Signals = s.Subscribe(req.EndpointID)
	if req.Simulate {
		return outcome, nil
	}
	expected := head.Revision
	for _, link := range chain {
		if err := s.store.PutEndpointRevision(ctx, req.Tenant, link, expected); err != nil {
			// Concurrent writers can both observe an absent (or identical)
			// head before one wins the append. Preserve the service's public
			// conflict vocabulary instead of leaking a store error code.
			if CodeOf(err) == StoreDuplicateCode {
				if req.Op == OpAdd {
					return UpdateOutcome{}, fmt.Errorf("contact: Update %s: %w", req.EndpointID, ErrDuplicateEndpoint)
				}
				return UpdateOutcome{}, fmt.Errorf("contact: Update %s: %w", req.EndpointID, ErrStaleRevision)
			}
			return UpdateOutcome{}, err
		}
		expected = link.Revision
	}
	outcome.Outbox = []OutboxOp{{EndpointID: revision.EndpointID, Revision: revision.Revision, Digest: revision.CanonicalDigest}}
	outcome.ExternalSync = SyncPending
	return outcome, nil
}

// consumeChallenge redeems one tracked receipt against its challenge.
// Simulations peek at a copy: a dry run never burns a one-time token.
func (s *UpdateService) consumeChallenge(req UpdateRequest, peek bool) (ChallengeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	challenge, ok := s.challenges[req.ChallengeID]
	if !ok {
		return "", fmt.Errorf("contact: Update verification: %w", ErrUnknownVerification)
	}
	if challenge.Subject.Tenant != req.Tenant {
		return "", fmt.Errorf("contact: Update verification: %w", ErrTenantMismatch)
	}
	target := challenge
	if peek {
		owned := *challenge
		target = &owned
	}
	status := VerifyChallenge(target, req.Subject, req.EndpointID, req.Purpose, req.ChallengeToken, req.Now)
	switch status {
	case ChallengeVerified:
		return status, nil
	case ChallengeConsumed:
		return status, fmt.Errorf("contact: Update verification: %w", ErrDuplicateVerification)
	case ChallengeExpired:
		return status, fmt.Errorf("contact: Update verification: %w", ErrLateVerification)
	default:
		return status, fmt.Errorf("contact: Update verification: %w", ErrInvalidVerification)
	}
}

// buildRevision folds one typed operation into its successor revision.
func buildRevision(head ContactEndpointRevision, exists bool, req UpdateRequest) (ContactEndpointRevision, error) {
	switch req.Op {
	case OpAdd:
		return NewContactEndpointRevision(req.Subject, req.EndpointID, req.Kind, req.Raw, req.Purpose, req.Priority, req.Source)
	case OpRevise:
		next, err := NewContactEndpointRevision(req.Subject, req.EndpointID, req.Kind, req.Raw, req.Purpose, req.Priority, req.Source)
		if err != nil {
			return ContactEndpointRevision{}, err
		}
		next.Revision = head.Revision + 1
		next.SupersedesRevision = head.Revision
		return head.Successor(next)
	case OpRemove:
		next := head
		next.Revision = head.Revision + 1
		next.SupersedesRevision = head.Revision
		next.Source = "removal:" + req.Source
		return head.Successor(next)
	case OpCorrect:
		next, err := NewContactEndpointRevision(req.Subject, req.EndpointID, req.Kind, req.Raw, req.Purpose, req.Priority, "correction:"+req.Source)
		if err != nil {
			return ContactEndpointRevision{}, err
		}
		next.Revision = head.Revision + 1
		next.SupersedesRevision = head.Revision
		return CorrectEndpointRevision(head, next)
	default:
		return ContactEndpointRevision{}, ErrInvalidUpdate
	}
}

// Reconcile compares one endpoint head against an external observation.
// A match closes consistency; anything else opens bounded repair while
// every unrelated endpoint stays untouched.
func (s *UpdateService) Reconcile(ctx context.Context, tenant values.TenantId, endpointID, observedDigest string) (SyncState, error) {
	revisions, err := s.store.ListEndpointRevisions(ctx, tenant, endpointID)
	if err != nil {
		return SyncRepair, err
	}
	head, exists := headRevision(revisions)
	if !exists {
		return SyncRepair, fmt.Errorf("contact: Reconcile %s: %w", endpointID, ErrUnknownEndpoint)
	}
	if head.CanonicalDigest == observedDigest {
		return SyncReconciled, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(tenant) + "\x00" + endpointID
	ticket, ok := s.repairs[key]
	if !ok {
		ticket = &RepairTicket{EndpointID: endpointID, Reason: "external mismatch", MaxAttempts: 3}
		s.repairs[key] = ticket
	}
	ticket.Attempts++
	return SyncRepair, nil
}

// RepairTickets lists one tenant's open repair tickets in stable order.
func (s *UpdateService) RepairTickets(tenant values.TenantId) []RepairTicket {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []RepairTicket
	for key, ticket := range s.repairs {
		if strings.HasPrefix(key, string(tenant)+"\x00") {
			out = append(out, *ticket)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndpointID < out[j].EndpointID })
	return out
}
