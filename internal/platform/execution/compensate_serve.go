package execution

// WF-REV-002: the served compensation composition.
//
// The compensate executor (internal/workflow/steps/compensate) and
// execute.Driver.CancelGoverned's Compensator callback had no production
// caller: compensations never ran on the served path. This file composes
// them into the served cell behind one shared executor instance:
//
//   - COMPENSATE nodes: promotionStepPorts.ReleaseHold delegates to
//     [ServedCompensation.ReleaseHoldViaExecutor] when a served compensation
//     is composed (NewPromotionExecution always composes one). The side
//     effect is unchanged -- the same promotionbudget hold release the
//     legacy path runs, inside the same advance transaction -- but it now
//     runs as a governed compensation: bound authorization, an idempotent
//     capability, postcondition observation and one ledger event.
//   - Cancellation-driven compensation: [ServedCompensation.Compensate]
//     implements cancellation.Compensator over the same executor, so
//     WF-REV-001's discharge driver drains recorded obligations through it.
//     [ServedCompensation.Discharge] is the served entry: it carries the
//     tenant and intent the Compensator interface does not, the same shape
//     promotionbudget.ReleaseForIntent documents ("the shape a cancellation
//     has").
//   - Governed cancel: [ServedCompensation.GovernedCompensator] adapts the
//     same executor to transactioncancel.Compensator, and
//     [PromotionExecution.CancelGoverned] passes it to the served driver's
//     CancelGoverned. A cancel before the commit point returns CANCELLED
//     with no writes and never invokes the compensator; a cancel after the
//     commit point releases the hold through the executor.
//
// Bounds, stated not implied. The executor's operation owner and ledger are
// process-scoped: the owner scopes reserve/effect/complete to one execution,
// and a repeated execution re-presents safely because the release itself is
// idempotent (each execution records its own ledger event). Crash-resume
// authority stays where WF-REV-001 put it -- the node's recorded
// COMPENSATED transition inside the governing transaction. A recorded ledger
// event is provisional until its governing transaction commits; readers join
// it against node outcomes. The served
// capability implements the one production compensation that exists today,
// the budget-hold release; any other compensation ref fails closed into
// REPAIR_REQUIRED (its inverse capability is WF-REV-006/016 territory,
// Gate C). The governed compensator resolves the cancelled intent through
// IntentForPlan when the caller does not already know it; with neither it
// fails closed rather than guessing whose hold to release.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	transactioncancel "github.com/monstercameron/human-capital-management-suite/internal/transaction/cancel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/compensate"
)

const (
	// ServedHoldReleaseCapability is the production compensation behind
	// compensate_budget_hold: releasing the unit's compensation-pool hold
	// under the proposal's original idempotency identity. It is the same
	// action the reversibility declaration names for the budget reservation
	// effect (budget.ReleaseCompensationBudgetIntentType), so the hold the
	// COMPENSATE node releases and the hold cancellation discharges are
	// one thing, not two.
	ServedHoldReleaseCapability = "hcmnext.rewards.release_compensation_budget/v1"
	// ServedCompensationPath is the governed TX-007 path reference the
	// served cell files on its CancelGoverned requests.
	ServedCompensationPath = "promotion.compensation.hold_release/v1"
	// ServedCompensationObservation is the verification observation every
	// served compensation request names.
	ServedCompensationObservation = "observe.promotion.compensation_effective/v1"
	// servedObservationMaxAge bounds how stale a compensation observation
	// may be before the executor refuses it.
	servedObservationMaxAge = 5 * time.Minute
	// servedDischargeActor is the actor every cancellation-driven
	// compensation runs as. Authority comes from the recorded obligation,
	// not from this label: the request binds the obligation's decision id
	// as its approval ref.
	servedDischargeActor = "principal:workflow-cancellation"
)

var (
	// ErrCompensationUnsupported reports a compensation ref the served cell
	// has no inverse capability for. The call fails closed; nothing is
	// guessed.
	ErrCompensationUnsupported = errors.New("platform execution: served compensation supports no such capability")
	// ErrCompensationIdentity reports a compensation call that names no
	// tenant, no intent or no transaction. Identity is never invented.
	ErrCompensationIdentity = errors.New("platform execution: compensation needs tenant, intent and transaction")
	// ErrCompensationIntentResolver reports a governed post-commit cancel
	// whose plan resolves to no known intent with no resolver bound.
	ErrCompensationIntentResolver = errors.New("platform execution: no intent resolver for the governed compensation")
)

// servedAuthorityFingerprint binds an executor request to this cell's
// authority. An unsigned cell binds to an explicit marker rather than an
// empty string, so the executor's required-binding check still runs.
func servedAuthorityFingerprint(configured string) string {
	if strings.TrimSpace(configured) == "" {
		return "cell-local:unsigned-authority"
	}
	return configured
}

// ServedCompensationOptions carries the compensate executor's ports.
// Every port is injectable so tests can deterministically double the
// provider edge; nil ports default to the served implementations, which
// need a transaction in the call context (the advance, discharge or
// governed transaction the entry point opens).
type ServedCompensationOptions struct {
	Capability      compensate.Capability
	Authorizer      compensate.Authorizer
	Observer        compensate.Observer
	Operations      compensate.OperationOwner
	Ledger          compensate.Ledger
	Clock           func() time.Time
	AuthorityDigest string
	// DB opens the governed compensator's own transaction. Nil is fine
	// until GovernedCompensator actually runs post-commit.
	DB execute.Beginner
	// IntentForPlan resolves a committed transaction plan to the intent
	// whose hold the governed compensator releases. Nil fails closed.
	IntentForPlan func(context.Context, uuid.UUID, string) (uuid.UUID, error)
}

// ServedCompensation is the served cell's single compensate executor and
// its three entry points. One instance serves the COMPENSATE node path,
// the cancellation discharge path and the governed cancel path.
type ServedCompensation struct {
	exec            *compensate.Executor
	ledger          *recordingLedger
	capability      *servedHoldCapability
	clock           func() time.Time
	authorityDigest string
	db              execute.Beginner
	intentForPlan   func(context.Context, uuid.UUID, string) (uuid.UUID, error)
}

// ComposeServedCompensation builds the served compensation over the given
// ports, defaulting nil ones to the served implementations.
func ComposeServedCompensation(opts ServedCompensationOptions) (*ServedCompensation, error) {
	clock := opts.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	ledger := &recordingLedger{}
	s := &ServedCompensation{
		ledger:          ledger,
		capability:      &servedHoldCapability{clock: clock},
		clock:           clock,
		authorityDigest: opts.AuthorityDigest,
		db:              opts.DB,
		intentForPlan:   opts.IntentForPlan,
	}
	operations := opts.Operations
	if operations == nil {
		operations = &servedOperations{}
	}
	ledgerPort := opts.Ledger
	if ledgerPort == nil {
		ledgerPort = ledger
	}
	capPort := opts.Capability
	if capPort == nil {
		capPort = s.capability
	}
	authPort := opts.Authorizer
	if authPort == nil {
		authPort = &servedAuthorizer{authorityDigest: opts.AuthorityDigest}
	}
	obsPort := opts.Observer
	if obsPort == nil {
		obsPort = &servedHoldObserver{clock: clock}
	}
	s.exec = &compensate.Executor{
		Capability: capPort, Authorizer: authPort, Observer: obsPort,
		Operations: operations, Ledger: ledgerPort, Now: clock,
	}
	return s, nil
}

// Executor returns the one executor instance every served compensation
// path shares. The REFACTOR clause is this accessor: callers cannot hold
// two.
func (s *ServedCompensation) Executor() *compensate.Executor {
	if s == nil {
		return nil
	}
	return s.exec
}

// LedgerEvents returns the compensation events the shared executor has
// recorded, in recording order.
func (s *ServedCompensation) LedgerEvents() []compensate.Event {
	if s == nil || s.ledger == nil {
		return nil
	}
	return s.ledger.snapshot()
}

// CapabilityCalls reports how many times the served hold capability ran.
func (s *ServedCompensation) CapabilityCalls() int {
	if s == nil || s.capability == nil {
		return -1
	}
	return s.capability.callCount()
}

// holdReleaseRequest is the pure COMPENSATE-node mapping: a step request
// becomes the executor's governed compensation request. Pure so the GOLDEN
// test pins it without a database.
func holdReleaseRequest(tenant, intent uuid.UUID, actor, nodeID string, attempt int, proposalDigest, manifestDigest, fingerprint string) compensate.Request {
	return compensate.Request{
		TenantID: tenant.String(), ActorID: actor,
		TargetExecutionRef:         nodeID + "#" + strconv.Itoa(attempt),
		TargetEffectRef:            nodeID + "#" + strconv.Itoa(attempt),
		CompensationCapabilityRef:  ServedHoldReleaseCapability,
		VerificationObservationRef: ServedCompensationObservation,
		Reason:                     "compensate-node:" + nodeID,
		ApprovalPolicy:             "promotion.execute/v1",
		ApprovalRef:                proposalDigest,
		AuthorityPolicyFingerprint: fingerprint,
		CapabilityManifestDigest:   manifestDigest,
		PayloadDigest:              proposalDigest,
		IdempotencyKey:             proposalDigest + ":compensate:" + nodeID,
		OriginalHistoryRef:         "proposal-hold:" + proposalDigest,
		Strategy:                   compensate.StrategyCorrection,
		ObservationMaxAge:          servedObservationMaxAge,
	}
}

// dischargeCompensateRequest is the pure discharge mapping: one recorded
// obligation item becomes the executor's request. The idempotency key binds
// (obligation, effect) exactly as the discharge contract demands, and the
// history ref names the countered effect execution.
func dischargeCompensateRequest(tenant uuid.UUID, item cancellation.CompensationItem, manifestDigest, fingerprint string) compensate.Request {
	capabilityID := stripCompensationVersion(item.Compensation)
	strategy := compensate.StrategyCorrection
	if strings.Contains(capabilityID, ".supersede") {
		strategy = compensate.StrategySupersedingRevision
	}
	payload := sha256.Sum256([]byte(strings.Join([]string{tenant.String(), item.EffectID, item.Compensation, item.ObligationID.String()}, "\x00")))
	return compensate.Request{
		TenantID: tenant.String(), ActorID: servedDischargeActor,
		TargetExecutionRef: item.EffectID, TargetEffectRef: item.EffectID,
		CompensationCapabilityRef:  capabilityID,
		VerificationObservationRef: ServedCompensationObservation,
		Reason:                     "discharge:" + item.ObligationID.String(),
		ApprovalPolicy:             "workflow-cancellation/v1",
		ApprovalRef:                item.ObligationID.String(),
		AuthorityPolicyFingerprint: fingerprint,
		CapabilityManifestDigest:   manifestDigest,
		PayloadDigest:              hex.EncodeToString(payload[:]),
		IdempotencyKey:             item.ObligationID.String() + ":" + item.EffectID,
		OriginalHistoryRef:         "effect:" + item.EffectID,
		Strategy:                   strategy,
		ObservationMaxAge:          servedObservationMaxAge,
	}
}

// governedCompensateRequest is the pure governed-cancel mapping: one
// post-commit cancellation becomes the executor's request, bound to the
// committed transaction it counters.
func governedCompensateRequest(tenant uuid.UUID, commitIdentity, planID, manifestDigest, fingerprint string) compensate.Request {
	payload := sha256.Sum256([]byte(strings.Join([]string{tenant.String(), commitIdentity, planID}, "\x00")))
	return compensate.Request{
		TenantID: tenant.String(), ActorID: servedDischargeActor,
		TargetExecutionRef: planID, TargetEffectRef: planID,
		CompensationCapabilityRef:  ServedHoldReleaseCapability,
		VerificationObservationRef: ServedCompensationObservation,
		Reason:                     "governed-cancel:" + planID,
		ApprovalPolicy:             "transaction-cancel/v1",
		ApprovalRef:                commitIdentity,
		AuthorityPolicyFingerprint: fingerprint,
		CapabilityManifestDigest:   manifestDigest,
		PayloadDigest:              hex.EncodeToString(payload[:]),
		IdempotencyKey:             commitIdentity + ":governed",
		OriginalHistoryRef:         "commit:" + commitIdentity,
		Strategy:                   compensate.StrategyCorrection,
		ObservationMaxAge:          servedObservationMaxAge,
	}
}

// stripCompensationVersion drops the "@version" evidence suffix a recorded
// compensation carries, recovering the published action id.
func stripCompensationVersion(compensation string) string {
	if i := strings.LastIndex(compensation, "@"); i >= 0 {
		return compensation[:i]
	}
	return compensation
}

// servedStatusRoute maps an executor status onto its COMPENSATE route. The
// vocabularies are identical by construction; the map keeps the identity
// explicit and pinned by GOLDEN.
func servedStatusRoute(status compensate.Status) string {
	switch status {
	case compensate.StatusCompensated:
		return "COMPENSATED"
	case compensate.StatusPartial:
		return "PARTIAL"
	case compensate.StatusFailed:
		return "FAILED"
	default:
		return "REPAIR_REQUIRED"
	}
}

// CancelGoverned resolves a cancellation against the served driver's local
// commit boundary, passing the served governed compensator. It is the
// served cell's production caller for execute.Driver.CancelGoverned: the
// RED this todo closes.
func (p *PromotionExecution) CancelGoverned(ctx context.Context, req transactioncancel.Request) (execute.GovernedCommitResult, error) {
	if p == nil || p.driver == nil || p.Compensation == nil {
		return execute.GovernedCommitResult{}, fmt.Errorf("platform execution: governed cancellation needs a composed driver and compensation")
	}
	if strings.TrimSpace(req.CompensationPath) == "" {
		req.CompensationPath = ServedCompensationPath
	}
	return p.driver.CancelGoverned(ctx, req, p.Compensation.GovernedCompensator())
}

// ReleaseHoldViaExecutor runs the compensate_budget_hold node through the
// shared executor. The release itself is the legacy one -- the unit's
// compensation-pool hold under the proposal's original idempotency
// identity, inside the advance transaction -- now wrapped in bound
// authorization, exactly-once operation scope, postcondition observation
// and a ledger event carrying the countered hold as its history ref.
func (s *ServedCompensation) ReleaseHoldViaExecutor(ctx context.Context, req execute.StepRequest) (promotionsteps.HoldReleaseResult, error) {
	tx, ok := stepTx(ctx)
	if !ok {
		return promotionsteps.HoldReleaseResult{}, fmt.Errorf("platform execution: compensate_budget_hold runs only inside the advance transaction")
	}
	proposalDigest := req.Proposal.Revision.MaterialDigest.Digest
	if strings.TrimSpace(proposalDigest) == "" {
		return promotionsteps.HoldReleaseResult{}, fmt.Errorf("platform execution: compensate_budget_hold needs the proposal material digest")
	}
	intentID, err := uuid.Parse(req.Proposal.Revision.IntentID)
	if err != nil {
		return promotionsteps.HoldReleaseResult{}, fmt.Errorf("platform execution: compensate_budget_hold needs a UUID intent identity: %w", err)
	}
	delegation, found, err := runtime.LoadExecutionDelegation(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return promotionsteps.HoldReleaseResult{}, err
	}
	if !found {
		return promotionsteps.HoldReleaseResult{}, errNoDelegation
	}
	manifest, err := s.exec.Capability.Manifest(ctx)
	if err != nil {
		return promotionsteps.HoldReleaseResult{}, fmt.Errorf("platform execution: served compensation manifest: %w", err)
	}
	compensateReq := holdReleaseRequest(req.TenantID, intentID, delegation.Subject,
		req.Node.ID, req.Attempt, proposalDigest, manifest.Digest, servedAuthorityFingerprint(s.authorityDigest))
	ctx = withCallOwner(withCompensationIdentity(ctx, req.TenantID, intentID))
	result, err := s.exec.Execute(ctx, compensateReq)
	if err != nil {
		return promotionsteps.HoldReleaseResult{}, err
	}
	return promotionsteps.HoldReleaseResult{
		Artifact: promotionsteps.Artifact{
			OutputDigest: result.Event.Digest,
			Refs: runtime.GovernanceRefs{
				ProposalRef:             proposalDigest,
				AuthorizationDecisionID: delegation.Subject,
				EffectRefs:              []string{result.Event.Digest},
			},
		},
		Status: servedStatusRoute(result.Status),
	}, nil
}

// Compensate implements cancellation.Compensator over the shared executor:
// one compensation event per presented effect, each naming the countered
// effect execution as its history ref. Unknown compensation refs fail
// closed (the executor records REPAIR_REQUIRED and the effect remains),
// never invented. Storage failures are the only errors; anything else is a
// remaining effect, exactly as the discharge contract requires.
func (s *ServedCompensation) Compensate(ctx context.Context, ex cancellation.Executor, item cancellation.CompensationItem) (cancellation.CompensationResult, error) {
	tx, ok := ex.(dbport.Tx)
	if !ok || tx == nil {
		return cancellation.CompensationResult{}, fmt.Errorf("%w: discharge compensator needs the discharge transaction", ErrCompensationIdentity)
	}
	tenant, _, ok := compensationIdentityFrom(ctx)
	if !ok {
		return cancellation.CompensationResult{}, fmt.Errorf("%w: discharge carries no tenant or intent", ErrCompensationIdentity)
	}
	// The executor's manifest binds the port to the hold release. A recorded
	// compensation naming any other inverse is not presented at all -- no
	// event, no invention -- so the effect stays remaining and the instance
	// parks REPAIR_REQUIRED. (Publishing further inverses is WF-REV-006/016,
	// Gate C.)
	if stripped := stripCompensationVersion(item.Compensation); stripped != ServedHoldReleaseCapability {
		return cancellation.CompensationResult{Compensated: false}, nil
	}
	manifest, err := s.exec.Capability.Manifest(ctx)
	if err != nil {
		return cancellation.CompensationResult{}, fmt.Errorf("platform execution: served compensation manifest: %w", err)
	}
	compensateReq := dischargeCompensateRequest(tenant, item, manifest.Digest, servedAuthorityFingerprint(s.authorityDigest))
	ctx = withCallOwner(withStepTx(ctx, tx))
	result, err := s.exec.Execute(ctx, compensateReq)
	if err != nil {
		return cancellation.CompensationResult{}, err
	}
	if result.Status != compensate.StatusCompensated {
		return cancellation.CompensationResult{Compensated: false}, nil
	}
	evidence := result.Event.ObservationEvidenceRef
	if strings.TrimSpace(evidence) == "" {
		evidence = result.Event.CapabilityEvidenceRef
	}
	if strings.TrimSpace(evidence) == "" {
		return cancellation.CompensationResult{Compensated: false}, nil
	}
	return cancellation.CompensationResult{Compensated: true, EvidenceRef: evidence}, nil
}

// Discharge drains one recorded compensation obligation through the shared
// executor. Tenant and intent travel in the call context because the
// Compensator interface does not carry them; the discharge transaction
// carries the durability.
func (s *ServedCompensation) Discharge(ctx context.Context, ex cancellation.Executor, tenant, intent uuid.UUID, req cancellation.DischargeRequest) (cancellation.Outcome, error) {
	if tenant == uuid.Nil || intent == uuid.Nil {
		return cancellation.Outcome{}, fmt.Errorf("%w: discharge names no tenant or intent", ErrCompensationIdentity)
	}
	tx, ok := ex.(dbport.Tx)
	if !ok || tx == nil {
		return cancellation.Outcome{}, fmt.Errorf("%w: discharge needs the discharge transaction", ErrCompensationIdentity)
	}
	ctx = withCompensationIdentity(withStepTx(ctx, tx), tenant, intent)
	return cancellation.Discharge(ctx, ex, req, s)
}

// GovernedCompensator adapts the shared executor to the governed cancel
// boundary: after an AFTER_COMMIT cancel it releases the cancelled plan's
// hold through the executor, in its own transaction. The intent travels in
// the call context when the caller knows it (WithCompensationIntent) and
// otherwise resolves through IntentForPlan; with neither the compensator
// fails closed so the boundary reports the launch failure instead of
// releasing the wrong hold.
func (s *ServedCompensation) GovernedCompensator() transactioncancel.Compensator {
	return func(ctx context.Context, req transactioncancel.CompensationRequest) error {
		tenant := req.Request.Tenant
		intent, ok := compensationIntentFrom(ctx)
		if !ok && s.intentForPlan != nil {
			var err error
			intent, err = s.intentForPlan(ctx, tenant, req.Request.PlanID)
			if err != nil {
				return fmt.Errorf("%w: resolve intent for plan %s: %v", ErrCompensationIntentResolver, req.Request.PlanID, err)
			}
			ok = intent != uuid.Nil
		}
		if tenant == uuid.Nil || !ok {
			return fmt.Errorf("%w: governed compensation of plan %s", ErrCompensationIntentResolver, req.Request.PlanID)
		}
		if s.db == nil {
			return fmt.Errorf("%w: governed compensation needs a database", ErrCompensationIdentity)
		}
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("platform execution: begin governed compensation: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
			return err
		}
		manifest, err := s.exec.Capability.Manifest(ctx)
		if err != nil {
			return fmt.Errorf("platform execution: served compensation manifest: %w", err)
		}
		compensateReq := governedCompensateRequest(tenant, req.CommitIdentity, req.Request.PlanID,
			manifest.Digest, servedAuthorityFingerprint(s.authorityDigest))
		ctx = withCallOwner(withCompensationIdentity(withStepTx(ctx, tx), tenant, intent))
		result, err := s.exec.Execute(ctx, compensateReq)
		if err != nil {
			return err
		}
		if result.Status != compensate.StatusCompensated {
			return fmt.Errorf("%w: governed compensation of plan %s ended %s",
				transactioncancel.ErrCompensation, req.Request.PlanID, result.Status)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("platform execution: commit governed compensation: %w", err)
		}
		return nil
	}
}

// WithCompensationIntent carries a known intent into the governed
// compensator, skipping plan resolution.
func WithCompensationIntent(ctx context.Context, intent uuid.UUID) context.Context {
	return context.WithValue(ctx, compensationIntentKey{}, intent)
}

// compensationIdentityKey carries the discharge tenant and intent.
type compensationIdentityKey struct{}

type compensationIdentity struct {
	tenant uuid.UUID
	intent uuid.UUID
}

func withCompensationIdentity(ctx context.Context, tenant, intent uuid.UUID) context.Context {
	return context.WithValue(ctx, compensationIdentityKey{}, compensationIdentity{tenant: tenant, intent: intent})
}

func compensationIdentityFrom(ctx context.Context) (uuid.UUID, uuid.UUID, bool) {
	identity, ok := ctx.Value(compensationIdentityKey{}).(compensationIdentity)
	if !ok || identity.tenant == uuid.Nil || identity.intent == uuid.Nil {
		return uuid.Nil, uuid.Nil, false
	}
	return identity.tenant, identity.intent, true
}

// compensationIntentKey carries a caller-known intent for the governed
// compensator.
type compensationIntentKey struct{}

func compensationIntentFrom(ctx context.Context) (uuid.UUID, bool) {
	intent, ok := ctx.Value(compensationIntentKey{}).(uuid.UUID)
	if !ok || intent == uuid.Nil {
		return uuid.Nil, false
	}
	return intent, true
}

// callOwnerKey carries the executor's per-call operation owner: exactly-once
// within the governing transaction. Calls without one share a process-wide
// fallback, which is still exact but not crash-safe; the governing
// transaction's recorded outcome is the crash-resume authority either way.
type callOwnerKey struct{}

func withCallOwner(ctx context.Context) context.Context {
	return context.WithValue(ctx, callOwnerKey{}, &callOwner{m: make(map[compensate.OperationKey]compensate.OperationRecord)})
}

// servedHoldCapability is the served Capability port: the budget-hold
// release, idempotent by the proposal's original identity. Any other
// capability ref fails closed; the served cell implements no other inverse.
type servedHoldCapability struct {
	clock func() time.Time

	mu    sync.Mutex
	calls int
}

func (c *servedHoldCapability) Manifest(context.Context) (compensate.CapabilityManifest, error) {
	return compensate.CapabilityManifest{
		CapabilityRef: ServedHoldReleaseCapability,
		Digest:        digestOf("promotion.compensation.hold_capability/v1", ServedHoldReleaseCapability),
		Idempotent:    true,
	}, nil
}

func (c *servedHoldCapability) Compensate(ctx context.Context, req compensate.CapabilityRequest) (compensate.CapabilityReceipt, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if req.Request.CompensationCapabilityRef != ServedHoldReleaseCapability {
		return compensate.CapabilityReceipt{}, fmt.Errorf("%w: %q", ErrCompensationUnsupported, req.Request.CompensationCapabilityRef)
	}
	tx, ok := stepTx(ctx)
	if !ok || tx == nil {
		return compensate.CapabilityReceipt{}, fmt.Errorf("%w: hold release runs inside its governing transaction", ErrCompensationIdentity)
	}
	tenant, intent, ok := compensationIdentityFrom(ctx)
	if !ok {
		return compensate.CapabilityReceipt{}, fmt.Errorf("%w: hold release names no tenant or intent", ErrCompensationIdentity)
	}
	at := c.clock().UTC()
	released, err := promotionbudget.ReleaseForIntent(ctx, tx, tenant, intent, at)
	if err != nil {
		return compensate.CapabilityReceipt{}, err
	}
	held := "held=false"
	if released {
		held = "held=true"
	}
	return compensate.CapabilityReceipt{
		Accepted: true, Applied: true,
		EvidenceRef: digestOf("promotion.compensation.hold_release_evidence/v1",
			tenant.String(), intent.String(), held, at.Format(time.RFC3339Nano)),
		Detail: "no outstanding hold for the intent; " + held,
	}, nil
}

func (c *servedHoldCapability) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// servedAuthorizer binds an executor call to this cell's authority. The
// upstream governance -- the delegation behind a COMPENSATE node, the
// recorded obligation behind a discharge -- already authorized the undo;
// this port refuses any request that does not exactly carry the cell's
// authority fingerprint alongside its own bindings.
type servedAuthorizer struct {
	authorityDigest string
}

func (a *servedAuthorizer) Authorize(_ context.Context, r compensate.Request) (compensate.AuthorizationDecision, error) {
	if r.AuthorityPolicyFingerprint != servedAuthorityFingerprint(a.authorityDigest) {
		return compensate.AuthorizationDecision{}, nil
	}
	return compensate.AuthorizationDecision{
		Allowed: true, TenantID: r.TenantID, ActorID: r.ActorID,
		TargetEffectRef: r.TargetEffectRef, CapabilityRef: r.CompensationCapabilityRef,
		PayloadDigest: r.PayloadDigest, PolicyFingerprint: r.AuthorityPolicyFingerprint,
		ApprovalRef: r.ApprovalRef,
		EvidenceRef: digestOf("promotion.compensation.authorization/v1",
			r.TenantID, r.ActorID, r.TargetEffectRef, r.CompensationCapabilityRef,
			r.PayloadDigest, r.AuthorityPolicyFingerprint, r.ApprovalRef),
	}, nil
}

// servedHoldObserver verifies the hold release's postcondition by
// re-presenting it: the release is idempotent, so a second call that frees
// nothing proves no hold is outstanding, while one that frees something
// proves the capability did not apply and fails closed.
type servedHoldObserver struct {
	clock func() time.Time
}

func (o *servedHoldObserver) ObserveCompensation(ctx context.Context, req compensate.ObservationRequest) (compensate.Observation, error) {
	if req.Request.CompensationCapabilityRef != ServedHoldReleaseCapability {
		return compensate.Observation{}, fmt.Errorf("%w: %q", ErrCompensationUnsupported, req.Request.CompensationCapabilityRef)
	}
	tx, ok := stepTx(ctx)
	if !ok || tx == nil {
		return compensate.Observation{}, fmt.Errorf("%w: observation runs inside its governing transaction", ErrCompensationIdentity)
	}
	tenant, intent, ok := compensationIdentityFrom(ctx)
	if !ok {
		return compensate.Observation{}, fmt.Errorf("%w: observation names no tenant or intent", ErrCompensationIdentity)
	}
	at := o.clock().UTC()
	again, err := promotionbudget.ReleaseForIntent(ctx, tx, tenant, intent, at)
	if err != nil {
		return compensate.Observation{}, err
	}
	if again {
		return compensate.Observation{}, fmt.Errorf("platform execution: hold outstanding after the compensation")
	}
	if strings.TrimSpace(req.CorrectionEvidenceRef) == "" {
		return compensate.Observation{}, fmt.Errorf("platform execution: compensation carries no correction evidence")
	}
	return compensate.Observation{
		Match: true, ObservedAt: at,
		TenantID: req.Request.TenantID, TargetEffectRef: req.Request.TargetEffectRef,
		CorrectionEvidenceRef: req.CorrectionEvidenceRef,
		EvidenceRef: digestOf("promotion.compensation.observation/v1",
			req.Request.TenantID, req.Request.TargetEffectRef,
			req.CorrectionEvidenceRef, at.Format(time.RFC3339Nano)),
		Detail: "no outstanding hold for the intent",
	}, nil
}

// recordingLedger is the executor's compensation-event record: one entry
// per executed compensation, in recording order. It is process-scoped (see
// the package bound above); the governing transaction's recorded node
// outcome is the durable authority it indexes.
type recordingLedger struct {
	mu     sync.Mutex
	events []compensate.Event
}

func (l *recordingLedger) AppendCompensation(_ context.Context, e compensate.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
	return nil
}

func (l *recordingLedger) snapshot() []compensate.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]compensate.Event(nil), l.events...)
}

// callOwner is one governing transaction's operation scope for the
// executor: reserve, effect and completion records that live and die with
// the call. The recorded node outcome beside it is the crash-resume
// authority.
type callOwner struct {
	mu sync.Mutex
	m  map[compensate.OperationKey]compensate.OperationRecord
}

// servedOperations is the executor's operation owner: a per-call scope from
// the call context with a process-wide fallback. Reserve, effect and
// completion records live for one execution; a repeated execution
// re-presents safely because the hold release itself is idempotent.
type servedOperations struct {
	mu     sync.Mutex
	shared map[compensate.OperationKey]compensate.OperationRecord
}

func (o *servedOperations) scope(ctx context.Context) *callOwner {
	if owner, ok := ctx.Value(callOwnerKey{}).(*callOwner); ok && owner != nil {
		return owner
	}
	return nil
}

func (o *servedOperations) Reserve(ctx context.Context, key compensate.OperationKey, digest string) (compensate.OperationRecord, bool, error) {
	if owner := o.scope(ctx); owner != nil {
		return owner.reserve(key, digest)
	}
	return o.reserveShared(key, digest)
}

func (o *servedOperations) RecordEffect(ctx context.Context, key compensate.OperationKey, digest string, receipt compensate.CapabilityReceipt) error {
	if owner := o.scope(ctx); owner != nil {
		return owner.recordEffect(key, digest, receipt)
	}
	return o.recordShared(key, digest, receipt)
}

func (o *servedOperations) Complete(ctx context.Context, key compensate.OperationKey, digest string, result compensate.Result) error {
	if owner := o.scope(ctx); owner != nil {
		return owner.complete(key, digest, result)
	}
	return o.completeShared(key, digest, result)
}

func (o *callOwner) reserve(key compensate.OperationKey, digest string) (compensate.OperationRecord, bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if rec, ok := o.m[key]; ok {
		return rec, false, nil
	}
	rec := compensate.OperationRecord{Key: key, RequestDigest: digest, State: compensate.OperationReserved}
	o.m[key] = rec
	return rec, true, nil
}

func (o *callOwner) recordEffect(key compensate.OperationKey, digest string, receipt compensate.CapabilityReceipt) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec := o.m[key]
	if rec.RequestDigest != digest {
		return fmt.Errorf("%w: reservation for another request", ErrCompensationIdentity)
	}
	rec.State = compensate.OperationEffectRecorded
	rec.Receipt = receipt
	o.m[key] = rec
	return nil
}

func (o *callOwner) complete(key compensate.OperationKey, digest string, result compensate.Result) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec := o.m[key]
	if rec.RequestDigest != digest {
		return fmt.Errorf("%w: completion for another request", ErrCompensationIdentity)
	}
	rec.State = compensate.OperationCompleted
	rec.Result = result
	o.m[key] = rec
	return nil
}

func (o *servedOperations) reserveShared(key compensate.OperationKey, digest string) (compensate.OperationRecord, bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.shared == nil {
		o.shared = make(map[compensate.OperationKey]compensate.OperationRecord)
	}
	if rec, ok := o.shared[key]; ok {
		return rec, false, nil
	}
	rec := compensate.OperationRecord{Key: key, RequestDigest: digest, State: compensate.OperationReserved}
	o.shared[key] = rec
	return rec, true, nil
}

func (o *servedOperations) recordShared(key compensate.OperationKey, digest string, receipt compensate.CapabilityReceipt) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec := o.shared[key]
	if rec.RequestDigest != digest {
		return fmt.Errorf("%w: reservation for another request", ErrCompensationIdentity)
	}
	rec.State = compensate.OperationEffectRecorded
	rec.Receipt = receipt
	o.shared[key] = rec
	return nil
}

func (o *servedOperations) completeShared(key compensate.OperationKey, digest string, result compensate.Result) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec := o.shared[key]
	if rec.RequestDigest != digest {
		return fmt.Errorf("%w: completion for another request", ErrCompensationIdentity)
	}
	rec.State = compensate.OperationCompleted
	rec.Result = result
	o.shared[key] = rec
	return nil
}
