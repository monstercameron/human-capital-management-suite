// Package agentapproval owns the human gate between an on-behalf-of agent and
// a governed submission or external write. It records exactly what the user
// saw, binds the decision to a server-computed digest, and leaves normal
// BusinessIntent approvals to their existing workflow.
package agentapproval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

const (
	// MaxBatchItems is the largest list a user can approve as one exact card.
	MaxBatchItems = 25
	// ApprovalLifetime is the maximum lifetime independent of the task expiry.
	ApprovalLifetime = 24 * time.Hour
	maxTaskLifetime  = 7 * 24 * time.Hour
)

var (
	ErrInvalid             = errors.New("agentapproval: invalid request")
	ErrNotFound            = errors.New("agentapproval: approval not found")
	ErrExpired             = errors.New("agentapproval: approval expired")
	ErrTaskExpired         = errors.New("agentapproval: task expired")
	ErrRevoked             = errors.New("agentapproval: approval revoked")
	ErrDigestMismatch      = errors.New("agentapproval: exact digest does not match")
	ErrUnauthorized        = errors.New("agentapproval: user is not the assigned approver")
	ErrAgentCannotApprove  = errors.New("agentapproval: an agent cannot decide an approval")
	ErrWrongSurface        = errors.New("agentapproval: approval must come from the product surface")
	ErrStepUpRequired      = errors.New("agentapproval: step-up proof is required")
	ErrApprovalAlreadyMade = errors.New("agentapproval: approval has already been decided")
	ErrNotApproved         = errors.New("agentapproval: approval is not approved")
	ErrRevalidation        = errors.New("agentapproval: final revalidation failed")
	ErrSubmission          = errors.New("agentapproval: submission failed")
	ErrSubmissionInFlight  = errors.New("agentapproval: submission is already in flight")
	ErrAlreadySubmitted    = errors.New("agentapproval: approval was already submitted")
	ErrIdempotencyConflict = errors.New("agentapproval: idempotency key is bound to another submission")
)

// Tier is the side-effect tier from AGENT2-001.
type Tier uint8

const (
	TierCommunicate    Tier = 2
	TierSubmitGoverned Tier = 3
	TierExternalWrite  Tier = 4
)

func (t Tier) valid() bool { return t == TierSubmitGoverned || t == TierExternalWrite }

// ItemKind describes the governed operation represented by one card item.
type ItemKind string

const (
	ItemGovernedIntent  ItemKind = "GOVERNED_INTENT"
	ItemConnectionWrite ItemKind = "CONNECTION_WRITE"
)

// ActorKind is deliberately closed. An actor chain containing Agent is never
// accepted as the origin of a human approval decision.
type ActorKind string

const (
	ActorHuman ActorKind = "HUMAN"
	ActorAgent ActorKind = "AGENT"
)

// ActorRef is server-derived actor-chain evidence. It is not an authority
// grant and is never accepted from a chat reaction or model output.
type ActorRef struct {
	Kind ActorKind
	ID   string
}

// Subject identifies one affected record without carrying hidden fields.
type Subject struct {
	ID   string
	Kind string
}

// MaterialField is the before/after material value shown to the user.
type MaterialField struct {
	Path   string
	Before string
	After  string
}

// Source is evidence shown on the approval card. Taint is mandatory so an
// untrusted source can never be rendered as an unattributed fact.
type Source struct {
	Ref   string
	Taint string
}

// ActionItem is the complete material description of one T3/T4 effect.
// Exactly one of intent definition or connection operation is required based
// on Kind; the service does not infer an operation from a display name.
type ActionItem struct {
	ID                      string
	Kind                    ItemKind
	Tier                    Tier
	IntentDefinitionID      string
	IntentDefinitionVersion string
	ConnectionID            string
	ConnectionOperation     string
	Subjects                []Subject
	MaterialFields          []MaterialField
	Sources                 []Source
	Uncertainty             string
	RiskClass               string
}

// AgentActionApproval is the immutable card plus its lifecycle fact. Items
// and Digest are server-owned; callers can only refer to the digest they were
// shown when deciding or submitting.
type AgentActionApproval struct {
	ID             string
	TenantID       string
	TaskID         string
	AssignedUserID string
	AgentID        string
	OriginChain    []ActorRef
	Items          []ActionItem
	Digest         string
	TaskExpiresAt  time.Time
	ExpiresAt      time.Time
	CreatedAt      time.Time
	State          State
	RevokeReason   string
}

// State is the approval lifecycle. Executing is a durable in-process claim;
// it fences concurrent submitters before the external port is called.
type State string

const (
	StatePending   State = "PENDING"
	StateApproved  State = "APPROVED"
	StateRevoked   State = "REVOKED"
	StateExecuting State = "EXECUTING"
	StateSubmitted State = "SUBMITTED"
	StateFailed    State = "FAILED"
)

// CreateRequest contains server-resolved action data. It intentionally has no
// free-form prompt or model consent field.
type CreateRequest struct {
	ID             string
	TenantID       string
	TaskID         string
	AssignedUserID string
	AgentID        string
	OriginChain    []ActorRef
	Items          []ActionItem
	TaskExpiresAt  time.Time
	Now            time.Time
}

// ApprovalSurface identifies the only supported decision route. Chat text,
// reactions, model messages and API callbacks are not product approval.
type ApprovalSurface string

const (
	SurfaceProductTaskView ApprovalSurface = "PRODUCT_TASK_VIEW"
)

// ApproveRequest is the user's decision assertion. PresentedItemIDs must list
// every item in the same server-rendered card, so a batch cannot hide an item.
type ApproveRequest struct {
	ApprovalID       string
	UserID           string
	Surface          ApprovalSurface
	ExpectedDigest   string
	PresentedItemIDs []string
	DecisionOrigin   []ActorRef
	StepUpProof      stepup.Proof
}

// StepUpRequest binds the existing signed step-up proof record to this exact
// approval. The verifier is supplied by the trust/authn composition root.
type StepUpRequest struct {
	ApprovalID string
	UserID     string
	Digest     string
	Proof      stepup.Proof
}

// StepUpVerifier is deliberately local: agentapproval does not mint or
// consume credentials, and callers can adapt trust/stepup's proof store and
// current principal without a second authority path.
type StepUpVerifier interface {
	Verify(context.Context, StepUpRequest) error
}

// StepUpVerifierFunc adapts a function to StepUpVerifier.
type StepUpVerifierFunc func(context.Context, StepUpRequest) error

func (f StepUpVerifierFunc) Verify(ctx context.Context, req StepUpRequest) error {
	return f(ctx, req)
}

// Revalidator rechecks the current grant, user authority, proposal/connection
// freshness and material digest immediately before the effect boundary.
type Revalidator interface {
	Revalidate(context.Context, AgentActionApproval) (RevalidationResult, error)
}

// RevalidationResult makes the final checks explicit at the effect boundary.
// A caller cannot report one generic "still valid" bit and accidentally omit
// the current grant, user authority, or freshness check.
type RevalidationResult struct {
	GrantValid     bool
	AuthorityValid bool
	Fresh          bool
}

// RevalidatorFunc adapts a function to Revalidator.
type RevalidatorFunc func(context.Context, AgentActionApproval) (RevalidationResult, error)

func (f RevalidatorFunc) Revalidate(ctx context.Context, approval AgentActionApproval) (RevalidationResult, error) {
	return f(ctx, approval)
}

// SubmissionRequest is the one idempotent handoff to a governed intent or
// connection adapter. The adapter owns the normal business workflow.
type SubmissionRequest struct {
	ApprovalID     string
	TenantID       string
	UserID         string
	Digest         string
	IdempotencyKey string
	Items          []ActionItem
}

// Submitter performs the already approved operation exactly once for the
// supplied idempotency key.
type Submitter interface {
	Submit(context.Context, SubmissionRequest) error
}

// SubmitterFunc adapts a function to Submitter.
type SubmitterFunc func(context.Context, SubmissionRequest) error

func (f SubmitterFunc) Submit(ctx context.Context, req SubmissionRequest) error {
	return f(ctx, req)
}

// Receipt is the durable result returned after an idempotent handoff.
type Receipt struct {
	ApprovalID     string
	Digest         string
	IdempotencyKey string
	SubmittedAt    time.Time
	Replayed       bool
}

type record struct {
	approval AgentActionApproval
	receipt  Receipt
}

// Service is a concurrency-safe approval coordinator. A durable adapter may
// persist the same immutable approval and state transitions; no caller-facing
// contract changes when that adapter replaces this memory implementation.
type Service struct {
	mu    sync.Mutex
	now   func() time.Time
	seq   uint64
	items map[string]*record
}

// New returns an in-memory coordinator with the supplied clock. The clock is
// mandatory so expiry and race tests cannot accidentally use wall time.
func New(now func() time.Time) (*Service, error) {
	if now == nil {
		return nil, fmt.Errorf("%w: clock is required", ErrInvalid)
	}
	return &Service{now: now, items: make(map[string]*record)}, nil
}

// Create builds the exact approval card and computes its digest once. The
// server sorts and deep-copies all material rows before hashing them.
func (s *Service) Create(req CreateRequest) (AgentActionApproval, error) {
	if s == nil {
		return AgentActionApproval{}, ErrInvalid
	}
	now := req.Now.UTC()
	if now.IsZero() {
		now = s.now().UTC()
	}
	if err := validateCreate(req, now); err != nil {
		return AgentActionApproval{}, err
	}
	id := req.ID
	if id == "" {
		s.mu.Lock()
		s.seq++
		id = fmt.Sprintf("agent-approval-%08d", s.seq)
		s.mu.Unlock()
	}
	items := normalizeItems(req.Items)
	approval := AgentActionApproval{
		ID: id, TenantID: req.TenantID, TaskID: req.TaskID, AssignedUserID: req.AssignedUserID,
		AgentID: req.AgentID, OriginChain: slices.Clone(req.OriginChain), Items: items,
		TaskExpiresAt: req.TaskExpiresAt.UTC(), ExpiresAt: minTime(req.TaskExpiresAt.UTC(), now.Add(ApprovalLifetime)),
		CreatedAt: now, State: StatePending,
	}
	approval.Digest = Digest(approval)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.items[id]; exists {
		return AgentActionApproval{}, fmt.Errorf("%w: duplicate id %q", ErrInvalid, id)
	}
	s.items[id] = &record{approval: cloneApproval(approval)}
	return cloneApproval(approval), nil
}

// Get returns a copy of the server-held approval card.
func (s *Service) Get(id string) (AgentActionApproval, error) {
	if s == nil || strings.TrimSpace(id) == "" {
		return AgentActionApproval{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	if !ok {
		return AgentActionApproval{}, ErrNotFound
	}
	return cloneApproval(r.approval), nil
}

// Approve accepts only the assigned user's product-surface decision. It
// verifies step-up before the atomic state transition, then rechecks digest,
// expiry and revocation so a concurrent revoke wins or loses as one CAS.
func (s *Service) Approve(ctx context.Context, req ApproveRequest, verifier StepUpVerifier) error {
	if ctx == nil || s == nil || strings.TrimSpace(req.ApprovalID) == "" || strings.TrimSpace(req.UserID) == "" {
		return ErrInvalid
	}
	if req.Surface != SurfaceProductTaskView {
		return ErrWrongSurface
	}
	s.mu.Lock()
	r, ok := s.items[req.ApprovalID]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	approval := cloneApproval(r.approval)
	if err := s.checkOpenLocked(approval); err != nil {
		s.mu.Unlock()
		return err
	}
	if approval.State != StatePending {
		s.mu.Unlock()
		return ErrApprovalAlreadyMade
	}
	if req.UserID != approval.AssignedUserID {
		s.mu.Unlock()
		return ErrUnauthorized
	}
	if req.ExpectedDigest != approval.Digest || Digest(approval) != approval.Digest || !sameIDs(req.PresentedItemIDs, approval.Items) {
		s.mu.Unlock()
		return ErrDigestMismatch
	}
	if err := validateHumanDecision(req.UserID, req.DecisionOrigin); err != nil {
		s.mu.Unlock()
		return err
	}
	needsStepUp := approvalNeedsStepUp(approval)
	s.mu.Unlock()

	if needsStepUp {
		if verifier == nil || req.StepUpProof.ID == "" {
			return ErrStepUpRequired
		}
		if err := verifier.Verify(ctx, StepUpRequest{ApprovalID: approval.ID, UserID: req.UserID, Digest: approval.Digest, Proof: req.StepUpProof}); err != nil {
			return fmt.Errorf("%w: %v", ErrStepUpRequired, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok = s.items[req.ApprovalID]
	if !ok {
		return ErrNotFound
	}
	if err := s.checkOpenLocked(r.approval); err != nil {
		return err
	}
	if r.approval.State != StatePending {
		return ErrApprovalAlreadyMade
	}
	if r.approval.Digest != approval.Digest || Digest(r.approval) != r.approval.Digest {
		return ErrDigestMismatch
	}
	r.approval.State = StateApproved
	return nil
}

// Revoke fences an approval before it reaches the submission claim. A revoke
// after execution begins cannot create a false promise that an external call
// was undone.
func (s *Service) Revoke(id, reason string) error {
	if s == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(reason) == "" {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	if !ok {
		return ErrNotFound
	}
	switch r.approval.State {
	case StatePending, StateApproved:
		r.approval.State = StateRevoked
		r.approval.RevokeReason = reason
		return nil
	case StateRevoked:
		return ErrRevoked
	default:
		return ErrApprovalAlreadyMade
	}
}

// Submit rechecks the exact digest and all current authority/freshness facts,
// claims execution once, and calls the adapter with one idempotency key. A
// duplicate successful call returns the original receipt without invoking the
// adapter again.
func (s *Service) Submit(ctx context.Context, id, userID, expectedDigest, idempotencyKey string, revalidator Revalidator, submitter Submitter) (Receipt, error) {
	if ctx == nil || s == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(expectedDigest) == "" || strings.TrimSpace(idempotencyKey) == "" || revalidator == nil || submitter == nil {
		return Receipt{}, ErrInvalid
	}
	s.mu.Lock()
	r, ok := s.items[id]
	if !ok {
		s.mu.Unlock()
		return Receipt{}, ErrNotFound
	}
	if r.approval.AssignedUserID != userID {
		s.mu.Unlock()
		return Receipt{}, ErrUnauthorized
	}
	if r.approval.State == StateSubmitted {
		if r.receipt.IdempotencyKey != idempotencyKey {
			s.mu.Unlock()
			return Receipt{}, ErrIdempotencyConflict
		}
		if expectedDigest != r.receipt.Digest || r.approval.Digest != r.receipt.Digest || Digest(r.approval) != r.receipt.Digest {
			s.mu.Unlock()
			return Receipt{}, ErrDigestMismatch
		}
		receipt := r.receipt
		receipt.Replayed = true
		s.mu.Unlock()
		return receipt, nil
	}
	if r.approval.State == StateExecuting {
		s.mu.Unlock()
		return Receipt{}, ErrSubmissionInFlight
	}
	if r.approval.State == StateRevoked {
		s.mu.Unlock()
		return Receipt{}, ErrRevoked
	}
	if r.approval.State != StateApproved {
		s.mu.Unlock()
		return Receipt{}, ErrNotApproved
	}
	if err := s.checkApprovedLocked(r.approval, expectedDigest); err != nil {
		s.mu.Unlock()
		return Receipt{}, err
	}
	approval := cloneApproval(r.approval)
	// Keep the lock while calling the final revalidator: revocation and the
	// execution claim must be one serialized decision at this boundary.
	validation, err := revalidator.Revalidate(ctx, approval)
	if err != nil {
		s.mu.Unlock()
		return Receipt{}, fmt.Errorf("%w: %v", ErrRevalidation, err)
	}
	if !validation.GrantValid || !validation.AuthorityValid || !validation.Fresh {
		s.mu.Unlock()
		return Receipt{}, fmt.Errorf("%w: current grant, authority and freshness are required", ErrRevalidation)
	}
	r.approval.State = StateExecuting
	s.mu.Unlock()

	request := SubmissionRequest{ApprovalID: approval.ID, TenantID: approval.TenantID, UserID: approval.AssignedUserID, Digest: approval.Digest, IdempotencyKey: idempotencyKey, Items: cloneItems(approval.Items)}
	if err := submitter.Submit(ctx, request); err != nil {
		s.mu.Lock()
		r.approval.State = StateFailed
		s.mu.Unlock()
		return Receipt{}, fmt.Errorf("%w: %v", ErrSubmission, err)
	}
	receipt := Receipt{ApprovalID: approval.ID, Digest: approval.Digest, IdempotencyKey: idempotencyKey, SubmittedAt: s.now().UTC()}
	s.mu.Lock()
	r.approval.State = StateSubmitted
	r.receipt = receipt
	s.mu.Unlock()
	return receipt, nil
}

// Digest returns the canonical material digest bound to an approval.
func Digest(approval AgentActionApproval) string {
	canonical := canonicalApproval{TenantID: approval.TenantID, TaskID: approval.TaskID, AssignedUserID: approval.AssignedUserID, AgentID: approval.AgentID, TaskExpiresAt: approval.TaskExpiresAt.UTC().UnixNano(), ExpiresAt: approval.ExpiresAt.UTC().UnixNano(), Items: canonicalItems(approval.Items)}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte("hcm-next-agent-action-approval/v1\x00"), raw...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

type canonicalApproval struct {
	TenantID       string          `json:"tenant_id"`
	TaskID         string          `json:"task_id"`
	AssignedUserID string          `json:"assigned_user_id"`
	AgentID        string          `json:"agent_id"`
	TaskExpiresAt  int64           `json:"task_expires_at"`
	ExpiresAt      int64           `json:"expires_at"`
	Items          []canonicalItem `json:"items"`
}

type canonicalItem struct {
	ID                      string          `json:"id"`
	Kind                    ItemKind        `json:"kind"`
	Tier                    Tier            `json:"tier"`
	IntentDefinitionID      string          `json:"intent_definition_id,omitempty"`
	IntentDefinitionVersion string          `json:"intent_definition_version,omitempty"`
	ConnectionID            string          `json:"connection_id,omitempty"`
	ConnectionOperation     string          `json:"connection_operation,omitempty"`
	Subjects                []Subject       `json:"subjects"`
	MaterialFields          []MaterialField `json:"material_fields"`
	Sources                 []Source        `json:"sources"`
	Uncertainty             string          `json:"uncertainty"`
	RiskClass               string          `json:"risk_class"`
}

func canonicalItems(items []ActionItem) []canonicalItem {
	out := make([]canonicalItem, 0, len(items))
	for _, item := range normalizeItems(items) {
		out = append(out, canonicalItem{ID: item.ID, Kind: item.Kind, Tier: item.Tier, IntentDefinitionID: item.IntentDefinitionID, IntentDefinitionVersion: item.IntentDefinitionVersion, ConnectionID: item.ConnectionID, ConnectionOperation: item.ConnectionOperation, Subjects: item.Subjects, MaterialFields: item.MaterialFields, Sources: item.Sources, Uncertainty: item.Uncertainty, RiskClass: item.RiskClass})
	}
	return out
}

func (s *Service) checkOpenLocked(a AgentActionApproval) error {
	now := s.now().UTC()
	if a.State == StateRevoked {
		return ErrRevoked
	}
	if !now.Before(a.TaskExpiresAt) {
		return ErrTaskExpired
	}
	if !now.Before(a.ExpiresAt) {
		return ErrExpired
	}
	return nil
}

func (s *Service) checkApprovedLocked(a AgentActionApproval, digest string) error {
	if err := s.checkOpenLocked(a); err != nil {
		return err
	}
	if a.State != StateApproved {
		return ErrNotApproved
	}
	if digest != a.Digest || Digest(a) != a.Digest {
		return ErrDigestMismatch
	}
	return nil
}

func validateCreate(req CreateRequest, now time.Time) error {
	for field, value := range map[string]string{"id": req.ID, "tenant_id": req.TenantID, "task_id": req.TaskID, "assigned_user_id": req.AssignedUserID, "agent_id": req.AgentID} {
		if value != "" && (strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value) {
			return fmt.Errorf("%w: %s is not canonical", ErrInvalid, field)
		}
	}
	if req.TenantID == "" || req.TaskID == "" || req.AssignedUserID == "" || req.AgentID == "" || len(req.Items) == 0 || len(req.Items) > MaxBatchItems {
		return fmt.Errorf("%w: tenant, task, user, agent and one to twenty-five items are required", ErrInvalid)
	}
	if req.TaskExpiresAt.IsZero() || !req.TaskExpiresAt.After(now) || req.TaskExpiresAt.Sub(now) > maxTaskLifetime {
		return fmt.Errorf("%w: task expiry must be in the next seven days", ErrInvalid)
	}
	seen := make(map[string]bool, len(req.Items))
	for _, item := range req.Items {
		if err := validateItem(item); err != nil {
			return err
		}
		if seen[item.ID] {
			return fmt.Errorf("%w: duplicate item %q", ErrInvalid, item.ID)
		}
		seen[item.ID] = true
	}
	return nil
}

func validateItem(item ActionItem) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.ID) != item.ID || !item.Tier.valid() || len(item.Subjects) == 0 || len(item.MaterialFields) == 0 || len(item.Sources) == 0 || strings.TrimSpace(item.Uncertainty) == "" || strings.TrimSpace(item.RiskClass) == "" {
		return fmt.Errorf("%w: item %q is incomplete", ErrInvalid, item.ID)
	}
	if item.Kind != ItemGovernedIntent && item.Kind != ItemConnectionWrite {
		return fmt.Errorf("%w: item %q has unknown kind", ErrInvalid, item.ID)
	}
	if item.Kind == ItemGovernedIntent && (strings.TrimSpace(item.IntentDefinitionID) == "" || strings.TrimSpace(item.IntentDefinitionVersion) == "" || item.ConnectionID != "" || item.ConnectionOperation != "") {
		return fmt.Errorf("%w: governed item %q must name only its definition and version", ErrInvalid, item.ID)
	}
	if item.Kind == ItemConnectionWrite && (strings.TrimSpace(item.ConnectionID) == "" || strings.TrimSpace(item.ConnectionOperation) == "" || item.IntentDefinitionID != "" || item.IntentDefinitionVersion != "") {
		return fmt.Errorf("%w: connection item %q must name only its operation", ErrInvalid, item.ID)
	}
	for _, subject := range item.Subjects {
		if strings.TrimSpace(subject.ID) == "" || strings.TrimSpace(subject.Kind) == "" {
			return fmt.Errorf("%w: item %q has an incomplete subject", ErrInvalid, item.ID)
		}
	}
	for _, field := range item.MaterialFields {
		if strings.TrimSpace(field.Path) == "" {
			return fmt.Errorf("%w: item %q has an unnamed material field", ErrInvalid, item.ID)
		}
	}
	for _, source := range item.Sources {
		if strings.TrimSpace(source.Ref) == "" || strings.TrimSpace(source.Taint) == "" {
			return fmt.Errorf("%w: item %q has an untainted source", ErrInvalid, item.ID)
		}
	}
	return nil
}

func validateHumanDecision(userID string, chain []ActorRef) error {
	if len(chain) == 0 {
		return ErrUnauthorized
	}
	human := false
	for _, actor := range chain {
		if actor.Kind == ActorAgent {
			return ErrAgentCannotApprove
		}
		if actor.Kind == ActorHuman && actor.ID == userID {
			human = true
		}
	}
	if !human {
		return ErrUnauthorized
	}
	return nil
}

func approvalNeedsStepUp(a AgentActionApproval) bool {
	for _, item := range a.Items {
		switch strings.ToUpper(strings.TrimSpace(item.RiskClass)) {
		case "HIGH", "CRITICAL":
			return true
		}
	}
	return false
}

func sameIDs(ids []string, items []ActionItem) bool {
	if len(ids) != len(items) {
		return false
	}
	for i, item := range items {
		if ids[i] != item.ID {
			return false
		}
	}
	return true
}

func normalizeItems(items []ActionItem) []ActionItem {
	out := cloneItems(items)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	for i := range out {
		sort.Slice(out[i].Subjects, func(a, b int) bool {
			left, right := out[i].Subjects[a], out[i].Subjects[b]
			if left.ID != right.ID {
				return left.ID < right.ID
			}
			return left.Kind < right.Kind
		})
		sort.Slice(out[i].MaterialFields, func(a, b int) bool {
			left, right := out[i].MaterialFields[a], out[i].MaterialFields[b]
			if left.Path != right.Path {
				return left.Path < right.Path
			}
			if left.Before != right.Before {
				return left.Before < right.Before
			}
			return left.After < right.After
		})
		sort.Slice(out[i].Sources, func(a, b int) bool {
			return out[i].Sources[a].Ref+"\x00"+out[i].Sources[a].Taint < out[i].Sources[b].Ref+"\x00"+out[i].Sources[b].Taint
		})
	}
	return out
}

func cloneItems(items []ActionItem) []ActionItem {
	out := make([]ActionItem, len(items))
	for i, item := range items {
		out[i] = item
		out[i].Subjects = slices.Clone(item.Subjects)
		out[i].MaterialFields = slices.Clone(item.MaterialFields)
		out[i].Sources = slices.Clone(item.Sources)
	}
	return out
}

func cloneApproval(a AgentActionApproval) AgentActionApproval {
	a.OriginChain = slices.Clone(a.OriginChain)
	a.Items = cloneItems(a.Items)
	return a
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
