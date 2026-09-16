// Package operator is the single governed door for operator, support,
// recovery and break-glass actions (INTENT-022).
//
// A material operational change -- database repair, workflow node
// intervention, connector redrive, projection rebuild, failover, quarantine,
// tenant suspension, key rotation -- is never a privileged side door. Each one
// is submitted to a [Gateway] as a typed [Request] and either resolves to an
// operational IntentInstance with a JIT (or break-glass) authority, the dual
// control and simulation its [Policy] demands, an idempotency key and an
// evidence [Receipt], or it is refused before any effect runs.
//
// The effect itself is performed by an [Executor] registered for the kind.
// An executor receives an [Authorization] that only the gateway can mint, and
// must call [Authorization.Require] before touching anything, so an executor
// invoked outside the gateway refuses. A request carries a typed scope and an
// expected version, never an arbitrary target state: an emergency action can
// bypass dual control and simulation, but it records its declared bypass
// reason and demands a post-use review, and it cannot rewrite business truth
// the executor does not itself own.
//
// # Bypass obligations and separated repair authority
//
// A bypass is not free (WF-RUN-039). Every bypassing action records a durable
// [Obligation] naming what it skipped, who acted, who approved it and by when
// a distinct person must review it; a gateway with nowhere to record that
// refuses the bypass outright. Each kind belongs to one authority [Family],
// and an obligation past its due review suspends its whole family at this
// gateway and at the capability gateway ([CapabilitySuspensions]) until a
// reviewer who is neither the operator nor the approver discharges it. The
// repair families carry one more rule: an approver of record over a scope may
// not come back as its repair operator.
package operator

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
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/breakglass"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Kind is the closed vocabulary of operator actions.
type Kind string

// Operator action kinds.
const (
	KindDatabaseRepair           Kind = "DATABASE_REPAIR"
	KindWorkflowNodeIntervention Kind = "WORKFLOW_NODE_INTERVENTION"
	KindConnectorRedrive         Kind = "CONNECTOR_REDRIVE"
	KindProjectionRebuild        Kind = "PROJECTION_REBUILD"
	KindFailover                 Kind = "FAILOVER"
	KindQuarantine               Kind = "QUARANTINE"
	KindTenantSuspension         Kind = "TENANT_SUSPENSION"
	KindKeyRotation              Kind = "KEY_ROTATION"
	// The four governed workflow controls EP-WF-002 exposes.
	KindWorkflowPause     Kind = "WORKFLOW_PAUSE"
	KindWorkflowResume    Kind = "WORKFLOW_RESUME"
	KindWorkflowCancel    Kind = "WORKFLOW_CANCEL"
	KindWorkflowRetryNode Kind = "WORKFLOW_RETRY_NODE"
	// The typed workflow interventions WF-RUN-015 adds beyond retry and
	// resume, each its own capability so a grant for one never authorizes
	// another.
	KindWorkflowSkip       Kind = "WORKFLOW_SKIP"
	KindWorkflowSatisfy    Kind = "WORKFLOW_SATISFY"
	KindWorkflowOverride   Kind = "WORKFLOW_OVERRIDE"
	KindWorkflowRewind     Kind = "WORKFLOW_REWIND"
	KindWorkflowCompensate Kind = "WORKFLOW_COMPENSATE"
	KindWorkflowSupersede  Kind = "WORKFLOW_SUPERSEDE"
	KindWorkflowReconcile  Kind = "WORKFLOW_RECONCILE"
	// KindDiagnosticRead is the one non-material kind: it still needs a JIT
	// grant and still leaves a receipt, but no dual control or simulation.
	KindDiagnosticRead Kind = "DIAGNOSTIC_READ"
)

// Kinds lists every kind in a stable order.
func Kinds() []Kind {
	return append([]Kind{
		KindDatabaseRepair, KindWorkflowNodeIntervention, KindConnectorRedrive, KindProjectionRebuild,
		KindFailover, KindQuarantine, KindTenantSuspension, KindKeyRotation,
		KindWorkflowPause, KindWorkflowResume, KindWorkflowCancel, KindWorkflowRetryNode,
		KindWorkflowSkip, KindWorkflowSatisfy, KindWorkflowOverride, KindWorkflowRewind,
		KindWorkflowCompensate, KindWorkflowSupersede, KindWorkflowReconcile, KindDiagnosticRead,
	}, authorityKinds()...)
}

// Policy is what one kind demands before its effect may run.
type Policy struct {
	// IntentType is the registered operational intent the action resolves to.
	IntentType string
	// Material marks a change or effect; only DIAGNOSTIC_READ is not.
	Material bool
	// Roles are the JIT roles whose grant may authorize the kind.
	Roles []jit.Role
	// DualControl requires a second approver distinct from the operator and
	// the grant's principal.
	DualControl bool
	// SimulationRequired requires a simulation over exactly the request scope.
	SimulationRequired bool
}

// PolicyFor returns the policy of a kind.
func PolicyFor(k Kind) (Policy, bool) {
	// The repair, override and migrate families keep their policies with
	// their definitions in authority.go (WF-RUN-039).
	if p, ok := authorityPolicyFor(k); ok {
		return p, true
	}
	p := Policy{IntentType: "hcmnext.operations." + strings.ToLower(string(k)) + ".v1", Material: true}
	switch k {
	case KindDatabaseRepair, KindProjectionRebuild:
		p.Roles, p.DualControl, p.SimulationRequired = []jit.Role{jit.RoleIntegrityRepair}, k == KindDatabaseRepair, true
	case KindWorkflowNodeIntervention:
		p.Roles, p.DualControl, p.SimulationRequired = []jit.Role{jit.RoleIntegrityRepair, jit.RoleIncidentResponder}, true, true
	case KindConnectorRedrive:
		p.Roles, p.SimulationRequired = []jit.Role{jit.RoleIncidentResponder, jit.RoleIntegrityRepair}, true
	case KindFailover, KindKeyRotation:
		p.Roles, p.DualControl = []jit.Role{jit.RoleIncidentResponder}, true
	case KindQuarantine:
		// Containment must be fast: one responder, no simulation, but still a
		// JIT grant, an intent and a receipt.
		p.Roles = []jit.Role{jit.RoleIncidentResponder}
	case KindTenantSuspension:
		p.Roles, p.DualControl = []jit.Role{jit.RoleIncidentResponder, jit.RoleAccessRevocation}, true
	case KindWorkflowPause, KindWorkflowResume:
		// Pausing contains; resuming is revalidated by the runtime against the
		// context the instance resumes into. Neither needs a second person.
		p.Roles = []jit.Role{jit.RoleIncidentResponder, jit.RoleIntegrityRepair}
	case KindWorkflowCancel:
		p.Roles, p.DualControl = []jit.Role{jit.RoleIncidentResponder, jit.RoleIntegrityRepair}, true
	case KindWorkflowRetryNode:
		p.Roles, p.SimulationRequired = []jit.Role{jit.RoleIntegrityRepair, jit.RoleIncidentResponder}, true
	case KindWorkflowSkip, KindWorkflowSatisfy, KindWorkflowRewind, KindWorkflowCompensate,
		KindWorkflowSupersede, KindWorkflowReconcile:
		// Repair authority is its own family: an integrity-repair grant, a
		// second person and a dry run of exactly this instance.
		p.Roles, p.DualControl, p.SimulationRequired = []jit.Role{jit.RoleIntegrityRepair}, true, true
	case KindWorkflowOverride:
		// Override is exceptional authority, distinct from repair: an
		// incident responder's grant, never an integrity-repair one.
		p.Roles, p.DualControl, p.SimulationRequired = []jit.Role{jit.RoleIncidentResponder}, true, true
	case KindDiagnosticRead:
		p.Roles, p.Material = []jit.Role{jit.RoleSupportReadOnly, jit.RoleIncidentResponder}, false
	default:
		return Policy{}, false
	}
	return p, true
}

// Sentinels. Classify with errors.Is or [CodeOf].
var (
	ErrOperator = errors.New("operator: action refused")
	// ErrNoEffect is what an executor wraps when it failed before changing
	// anything (its transaction rolled back). The gateway then aborts the
	// pending receipt so the same key may be retried, instead of recording a
	// repair.
	ErrNoEffect = errors.New("operator: executor failed without effect")
)

// Stable refusal codes.
const (
	CodeInvalidRequest      = "OPERATOR_INVALID_REQUEST"
	CodeUnknownKind         = "OPERATOR_UNKNOWN_KIND"
	CodeAuthorityRequired   = "OPERATOR_AUTHORITY_REQUIRED"
	CodeAuthorityMismatch   = "OPERATOR_AUTHORITY_MISMATCH"
	CodeAuthorityInactive   = "OPERATOR_AUTHORITY_INACTIVE"
	CodeDualControlRequired = "OPERATOR_DUAL_CONTROL_REQUIRED"
	CodeSimulationRequired  = "OPERATOR_SIMULATION_REQUIRED"
	CodeBypassReason        = "OPERATOR_BYPASS_REASON_REQUIRED"
	CodeIdempotencyConflict = "OPERATOR_IDEMPOTENCY_CONFLICT"
	CodeRepairRequired      = "OPERATOR_REPAIR_REQUIRED"
	CodeUnauthorizedEffect  = "OPERATOR_UNAUTHORIZED_EFFECT"
	CodeNoExecutor          = "OPERATOR_NO_EXECUTOR"
	CodeJournal             = "OPERATOR_JOURNAL_FAILED"
)

// Error is one typed refusal.
type Error struct {
	Code   string
	Kind   Kind
	Detail string
	Err    error
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("operator: %s", e.Code)
	if e.Kind != "" {
		msg += " for " + string(e.Kind)
	}
	msg += ": " + e.Detail
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the package sentinel and any cause.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrOperator, e.Err}
	}
	return []error{ErrOperator}
}

// ErrorCode reports the refusal's stable code.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

// CodeOf returns the refusal code err carries, or "".
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func refuse(code string, k Kind, format string, args ...any) *Error {
	return &Error{Code: code, Kind: k, Detail: fmt.Sprintf(format, args...)}
}

// Scope is the typed target of an action: a resource class and the exact
// identifiers it touches.
type Scope struct {
	Resource string   `json:"resource"`
	IDs      []string `json:"ids"`
}

func (s Scope) normalized() Scope {
	ids := slices.Clone(s.IDs)
	sort.Strings(ids)
	return Scope{Resource: s.Resource, IDs: ids}
}

func (s Scope) equal(o Scope) bool {
	a, b := s.normalized(), o.normalized()
	return a.Resource == b.Resource && slices.Equal(a.IDs, b.IDs)
}

// Simulation is the dry-run evidence a kind with [Policy.SimulationRequired]
// must present over exactly its request scope.
type Simulation struct {
	Digest string
	Scope  Scope
	At     time.Time
}

// Emergency is break-glass authority. It substitutes for the JIT grant and
// bypasses dual control and simulation, and the receipt it produces always
// requires review.
type Emergency struct {
	Grant        *breakglass.Grant
	BypassReason string
}

// Request is one operator action.
type Request struct {
	Kind            Kind
	Tenant          values.TenantId
	Operator        string
	Scope           Scope
	ExpectedVersion string
	// PayloadDigest pins the typed parameters the executor will apply.
	PayloadDigest  string
	IdempotencyKey string
	Reason         string
	TicketRef      string

	JIT            *jit.Grant
	SecondApprover string
	Simulation     *Simulation
	Emergency      *Emergency
}

// maxSimulationAge bounds how stale a simulation may be.
const maxSimulationAge = 24 * time.Hour

func (r Request) validate() error {
	if _, ok := PolicyFor(r.Kind); !ok {
		return refuse(CodeUnknownKind, r.Kind, "kind is not in the closed operator vocabulary")
	}
	if r.Tenant.Validate() != nil {
		return refuse(CodeInvalidRequest, r.Kind, "tenant is required")
	}
	for _, f := range []struct{ name, value string }{
		{"operator", r.Operator}, {"idempotency key", r.IdempotencyKey}, {"reason", r.Reason},
		{"ticket", r.TicketRef}, {"scope resource", r.Scope.Resource}, {"payload digest", r.PayloadDigest},
	} {
		if strings.TrimSpace(f.value) == "" {
			return refuse(CodeInvalidRequest, r.Kind, "%s is required", f.name)
		}
	}
	if len(r.Scope.IDs) == 0 {
		return refuse(CodeInvalidRequest, r.Kind, "scope names no target ids; an operator action is never tenant-wide by omission")
	}
	return nil
}

// digest is the request identity an idempotency key is bound to. Authority
// objects are excluded: the same action re-presented under a renewed grant
// is the same action.
func (r Request) digest() string {
	body, _ := json.Marshal(struct {
		Kind            Kind   `json:"kind"`
		Tenant          string `json:"tenant"`
		Operator        string `json:"operator"`
		Scope           Scope  `json:"scope"`
		ExpectedVersion string `json:"expected_version"`
		PayloadDigest   string `json:"payload_digest"`
	}{r.Kind, string(r.Tenant), strings.ToLower(strings.TrimSpace(r.Operator)), r.Scope.normalized(), r.ExpectedVersion, r.PayloadDigest})
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Outcome is how one submission ended.
type Outcome string

// Outcomes.
const (
	OutcomePending        Outcome = "PENDING"
	OutcomeApplied        Outcome = "APPLIED"
	OutcomeRepairRequired Outcome = "REPAIR_REQUIRED"
	// OutcomeDuplicate is an idempotent replay of an already recorded action:
	// the original receipt is returned and nothing runs again.
	OutcomeDuplicate Outcome = "DUPLICATE"
)

// Authority kinds on a receipt.
const (
	AuthorityJIT        = "JIT"
	AuthorityBreakGlass = "BREAK_GLASS"
)

// Receipt is the evidence of one operator action.
type Receipt struct {
	IntentInstanceID string          `json:"intent_instance_id"`
	IntentType       string          `json:"intent_type"`
	Kind             Kind            `json:"kind"`
	Tenant           values.TenantId `json:"tenant"`
	Operator         string          `json:"operator"`
	Scope            Scope           `json:"scope"`
	ExpectedVersion  string          `json:"expected_version,omitempty"`
	IdempotencyKey   string          `json:"idempotency_key"`
	RequestDigest    string          `json:"request_digest"`
	Reason           string          `json:"reason"`
	TicketRef        string          `json:"ticket_ref"`
	AuthorityKind    string          `json:"authority_kind"`
	AuthorityRef     string          `json:"authority_ref"`
	SecondApprover   string          `json:"second_approver,omitempty"`
	SimulationDigest string          `json:"simulation_digest,omitempty"`
	BypassReason     string          `json:"bypass_reason,omitempty"`
	// Bypassed names every requirement the authority path skipped
	// (WF-RUN-039). A non-empty list is what obliges a due review, so a
	// receipt can never record a bypass without also owing one.
	Bypassed       []string  `json:"bypassed,omitempty"`
	ReviewRequired bool      `json:"review_required"`
	Outcome        Outcome   `json:"outcome"`
	EffectRef      string    `json:"effect_ref,omitempty"`
	FailureCode    string    `json:"failure_code,omitempty"`
	RecordedAt     time.Time `json:"recorded_at"`
	Digest         string    `json:"digest"`
}

func (r Receipt) sealed() Receipt {
	r.Digest = ""
	body, _ := json.Marshal(r)
	sum := sha256.Sum256(body)
	r.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return r
}

// Verify reports whether the receipt's digest matches its content.
func (r Receipt) Verify() error {
	if r.sealed().Digest != r.Digest {
		return refuse(CodeInvalidRequest, r.Kind, "receipt digest does not match its content")
	}
	return nil
}

// IntentInstanceID derives the operational IntentInstance identity for one
// tenant-scoped idempotency key.
func IntentInstanceID(tenant values.TenantId, key string) string {
	sum := sha256.Sum256([]byte(string(tenant) + "\x1f" + key))
	return "intent:operator:" + hex.EncodeToString(sum[:16])
}

// Authorization is the gateway-minted permission an executor must require.
// Its fields are unexported: a zero Authorization -- the only one code
// outside this package can build -- authorizes nothing.
type Authorization struct {
	kind     Kind
	tenant   values.TenantId
	scope    Scope
	instance string
}

// Kind, Tenant, Scope and IntentInstanceID describe what was authorized.
func (a Authorization) Kind() Kind               { return a.kind }
func (a Authorization) Tenant() values.TenantId  { return a.tenant }
func (a Authorization) Scope() Scope             { return a.scope.normalized() }
func (a Authorization) IntentInstanceID() string { return a.instance }

// Require refuses unless a authorizes kind in tenant.
func (a Authorization) Require(kind Kind, tenant values.TenantId) error {
	if a.instance == "" || a.kind != kind || a.tenant != tenant {
		return refuse(CodeUnauthorizedEffect, kind, "no gateway authorization for this effect")
	}
	return nil
}

// Executor performs one kind's effect.
type Executor interface {
	Apply(ctx context.Context, auth Authorization, req Request) (effectRef string, err error)
}

// ExecutorFunc adapts a function to [Executor].
type ExecutorFunc func(ctx context.Context, auth Authorization, req Request) (string, error)

// Apply implements Executor.
func (f ExecutorFunc) Apply(ctx context.Context, auth Authorization, req Request) (string, error) {
	return f(ctx, auth, req)
}

// Journal durably records receipts keyed by tenant and idempotency key.
type Journal interface {
	// Lookup returns the receipt recorded for key, if any.
	Lookup(ctx context.Context, tenant values.TenantId, key string) (Receipt, bool, error)
	// Begin records a PENDING receipt unless one exists for its key, in which
	// case it records nothing and returns the existing one with true.
	Begin(ctx context.Context, pending Receipt) (Receipt, bool, error)
	// Complete replaces the PENDING receipt for its key with a final one.
	Complete(ctx context.Context, final Receipt) error
	// Abort removes the PENDING receipt for its key when the action provably
	// had no effect.
	Abort(ctx context.Context, pending Receipt) error
}

// Gateway is the governed operator door.
type Gateway struct {
	journal   Journal
	executors map[Kind]Executor
	clock     func() time.Time
	policy    func(Kind) (Policy, bool)
	// obligations records and reads the bypass obligations of this gateway's
	// actions (WF-RUN-039). It defaults to the journal when the journal is
	// also an [ObligationStore], so one wiring covers receipts and the debts
	// they leave behind.
	obligations ObligationStore
}

// GatewayOption configures a [Gateway].
type GatewayOption func(*Gateway)

// WithObligations replaces the gateway's bypass-obligation store. Without it
// the gateway uses the journal when the journal implements [ObligationStore],
// and refuses every bypass when it does not.
func WithObligations(store ObligationStore) GatewayOption {
	return func(g *Gateway) { g.obligations = store }
}

// NewGateway builds a gateway. Every executor must be registered for a known
// kind; clock defaults to time.Now.
func NewGateway(journal Journal, executors map[Kind]Executor, clock func() time.Time, opts ...GatewayOption) (*Gateway, error) {
	if journal == nil {
		return nil, refuse(CodeInvalidRequest, "", "a journal is required")
	}
	registered := make(map[Kind]Executor, len(executors))
	for k, e := range executors {
		if _, ok := PolicyFor(k); !ok || e == nil {
			return nil, refuse(CodeUnknownKind, k, "executor registered for an unknown kind or nil")
		}
		registered[k] = e
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	g := &Gateway{journal: journal, executors: registered, clock: clock, policy: PolicyFor}
	if store, ok := journal.(ObligationStore); ok {
		g.obligations = store
	}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	return g, nil
}

// ReviewObligation discharges one outstanding bypass obligation. The reviewer
// must differ from both the operator who incurred it and the approver who
// authorized the bypass, and the review lifts the suspension its family is
// under.
func (g *Gateway) ReviewObligation(ctx context.Context, tenant values.TenantId, id string, review ObligationReview) (ret0 Obligation, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "operator.review_obligation", tenant, id)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if g.obligations == nil {
		return Obligation{}, refuse(CodeObligationRequired, "", "this gateway has no obligation store to review against")
	}
	if tenant.Validate() != nil || strings.TrimSpace(id) == "" {
		return Obligation{}, refuse(CodeInvalidRequest, "", "a review names a tenant and an obligation id")
	}
	o, err := g.obligations.DischargeObligation(ctx, tenant, id, review)
	if err != nil {
		if CodeOf(err) != "" {
			return Obligation{}, err
		}
		return Obligation{}, &Error{Code: CodeObligationFailed, Detail: "discharge", Err: err}
	}
	return o, nil
}

// OutstandingObligations returns every undischarged bypass obligation of
// tenant, oldest due date first.
func (g *Gateway) OutstandingObligations(ctx context.Context, tenant values.TenantId) (ret0 []Obligation, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "operator.outstanding_obligations", tenant)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if g.obligations == nil {
		return nil, nil
	}
	out, err := g.obligations.OutstandingObligations(ctx, tenant)
	if err != nil {
		return nil, &Error{Code: CodeObligationFailed, Detail: "load outstanding obligations", Err: err}
	}
	return out, nil
}

// Submit evaluates and, when every requirement holds, performs one operator
// action exactly once.
func (g *Gateway) Submit(ctx context.Context, req Request) (ret0 Receipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "operator.submit", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return Receipt{}, err
	}
	policy, ok := g.policy(req.Kind)
	if !ok {
		return Receipt{}, refuse(CodeUnknownKind, req.Kind, "kind has no policy")
	}
	digest := req.digest()
	if prior, found, err := g.journal.Lookup(ctx, req.Tenant, req.IdempotencyKey); err != nil {
		return Receipt{}, &Error{Code: CodeJournal, Kind: req.Kind, Detail: "lookup", Err: err}
	} else if found {
		return replay(prior, digest, req.Kind)
	}
	executor, ok := g.executors[req.Kind]
	if !ok {
		return Receipt{}, refuse(CodeNoExecutor, req.Kind, "no executor is registered for this kind")
	}
	now := g.clock().UTC()
	// Outstanding obligations gate the action before any authority is spent:
	// an overdue review suspends its whole family, and a repair-family action
	// is refused to an approver of record over the same scope.
	outstanding, err := g.outstanding(ctx, req.Tenant)
	if err != nil {
		return Receipt{}, err
	}
	if err := gateObligations(req, outstanding, now); err != nil {
		return Receipt{}, err
	}
	receipt, approver, err := g.authorize(req, policy, now)
	if err != nil {
		return Receipt{}, err
	}
	if len(receipt.Bypassed) > 0 && g.obligations == nil {
		return Receipt{}, refuse(CodeObligationRequired, req.Kind, "a bypass needs an obligation store to stay accountable; this gateway has none")
	}
	receipt.RequestDigest, receipt.Outcome, receipt.RecordedAt = digest, OutcomePending, now
	receipt = receipt.sealed()
	if existing, found, err := g.journal.Begin(ctx, receipt); err != nil {
		return Receipt{}, &Error{Code: CodeJournal, Kind: req.Kind, Detail: "begin", Err: err}
	} else if found {
		return replay(existing, digest, req.Kind)
	}
	// The obligation is recorded before the effect runs: a bypass whose debt
	// cannot be written down does not happen at all.
	if len(receipt.Bypassed) > 0 {
		if err := g.obligations.RecordObligation(ctx, ObligationFor(receipt, approver, now)); err != nil {
			abortErr := g.journal.Abort(ctx, receipt)
			return Receipt{}, &Error{Code: CodeObligationFailed, Kind: req.Kind, Detail: "record bypass obligation", Err: errors.Join(err, abortErr)}
		}
	}

	auth := Authorization{kind: req.Kind, tenant: req.Tenant, scope: req.Scope.normalized(), instance: receipt.IntentInstanceID}
	effectRef, applyErr := executor.Apply(ctx, auth, req)
	if applyErr != nil && errors.Is(applyErr, ErrNoEffect) {
		if err := g.journal.Abort(ctx, receipt); err != nil {
			return Receipt{}, &Error{Code: CodeJournal, Kind: req.Kind, Detail: "abort", Err: errors.Join(applyErr, err)}
		}
		return Receipt{}, applyErr
	}
	final := receipt
	final.RecordedAt = g.clock().UTC()
	if applyErr != nil {
		final.Outcome, final.FailureCode = OutcomeRepairRequired, failureCode(applyErr)
	} else {
		final.Outcome, final.EffectRef = OutcomeApplied, effectRef
	}
	final = final.sealed()
	if err := g.journal.Complete(ctx, final); err != nil {
		return final, &Error{Code: CodeJournal, Kind: req.Kind, Detail: "complete", Err: err}
	}
	if applyErr != nil {
		return final, &Error{Code: CodeRepairRequired, Kind: req.Kind, Detail: "the effect failed after authorization; it is recorded for repair, never retried blindly", Err: applyErr}
	}
	return final, nil
}

func failureCode(err error) string {
	var c interface{ ErrorCode() string }
	if errors.As(err, &c) && c.ErrorCode() != "" {
		return c.ErrorCode()
	}
	return "EFFECT_FAILED"
}

// replay resolves a submission whose key already has a receipt.
func replay(prior Receipt, digest string, k Kind) (Receipt, error) {
	if prior.RequestDigest != digest {
		return Receipt{}, refuse(CodeIdempotencyConflict, k, "idempotency key is bound to a different action")
	}
	switch prior.Outcome {
	case OutcomePending, OutcomeRepairRequired:
		return prior, refuse(CodeRepairRequired, k, "a prior attempt under this key did not record a clean outcome; it needs repair, not a re-run")
	}
	dup := prior
	dup.Outcome = OutcomeDuplicate
	return dup, nil
}

// outstanding loads the tenant's undischarged obligations. A store that
// cannot answer fails the submission closed: the gateway cannot tell whether
// the family is suspended, so it does not act.
func (g *Gateway) outstanding(ctx context.Context, tenant values.TenantId) ([]Obligation, error) {
	if g.obligations == nil {
		return nil, nil
	}
	out, err := g.obligations.OutstandingObligations(ctx, tenant)
	if err != nil {
		return nil, &Error{Code: CodeObligationFailed, Detail: "load outstanding obligations", Err: err}
	}
	return out, nil
}

// gateObligations refuses a request its tenant's outstanding obligations do
// not admit: an overdue review suspends the whole authority family, and a
// repair-family action is refused to an approver of record over the same
// scope.
func gateObligations(req Request, outstanding []Obligation, now time.Time) error {
	if o, suspended := SuspendedFamilies(outstanding, now)[req.Kind.Family()]; suspended {
		return refuse(CodeObligationOverdue, req.Kind,
			"authority family %s is suspended: obligation %s from %s was due for review at %s",
			req.Kind.Family(), o.ID, o.Operator, o.DueAt.UTC().Format(time.RFC3339))
	}
	if req.Kind.SeparatesRepairFromApproval() {
		if o, conflict := ApproverOverScope(outstanding, req.Operator, req.Scope); conflict {
			return refuse(CodeRepairSeparation, req.Kind,
				"%s approved obligation %s over this scope and may not also be its repair operator", req.Operator, o.ID)
		}
	}
	return nil
}

// authorize checks authority, dual control and simulation, records the use
// on the grant, and returns the receipt skeleton together with the approver of
// record on the authority that admitted the action.
func (g *Gateway) authorize(req Request, p Policy, now time.Time) (Receipt, string, error) {
	r := Receipt{
		IntentInstanceID: IntentInstanceID(req.Tenant, req.IdempotencyKey), IntentType: p.IntentType,
		Kind: req.Kind, Tenant: req.Tenant, Operator: req.Operator, Scope: req.Scope.normalized(),
		ExpectedVersion: req.ExpectedVersion, IdempotencyKey: req.IdempotencyKey,
		Reason: req.Reason, TicketRef: req.TicketRef,
	}
	action := fmt.Sprintf("%s %s ticket=%s key=%s", req.Kind, req.Scope.Resource, req.TicketRef, req.IdempotencyKey)

	if req.Emergency != nil {
		if !p.Material {
			return Receipt{}, "", refuse(CodeAuthorityMismatch, req.Kind, "break-glass authority is for material emergencies, not diagnostic reads")
		}
		grant := req.Emergency.Grant
		if grant == nil {
			return Receipt{}, "", refuse(CodeAuthorityRequired, req.Kind, "emergency action names no break-glass grant")
		}
		if strings.TrimSpace(req.Emergency.BypassReason) == "" {
			return Receipt{}, "", refuse(CodeBypassReason, req.Kind, "emergency execution must declare why dual control and simulation are bypassed")
		}
		if !strings.EqualFold(grant.User, req.Operator) {
			return Receipt{}, "", refuse(CodeAuthorityMismatch, req.Kind, "break-glass grant belongs to another user")
		}
		if err := grant.Use(string(req.Kind), action, now); err != nil {
			code := CodeAuthorityInactive
			if errors.Is(err, breakglass.ErrCapabilityNotGranted) {
				code = CodeAuthorityMismatch
			}
			return Receipt{}, "", &Error{Code: code, Kind: req.Kind, Detail: "break-glass grant refused the use", Err: err}
		}
		r.AuthorityKind, r.AuthorityRef = AuthorityBreakGlass, grant.ID
		r.BypassReason, r.ReviewRequired = req.Emergency.BypassReason, true
		// Break-glass always replaces the JIT authority path, and additionally
		// skips whatever this kind's policy demanded of it.
		r.Bypassed = []string{BypassJITAuthority}
		if p.DualControl {
			r.Bypassed = append(r.Bypassed, BypassDualControl)
		}
		if p.SimulationRequired {
			r.Bypassed = append(r.Bypassed, BypassSimulation)
		}
		// The break-glass approver is the approver of record: the grant store
		// already requires them to differ from the user who broke the glass.
		return r, grant.Approver, nil
	}

	grant := req.JIT
	if grant == nil {
		return Receipt{}, "", refuse(CodeAuthorityRequired, req.Kind, "operator actions require a JIT grant; there is no standing operator authority")
	}
	switch {
	case !strings.EqualFold(grant.Principal, req.Operator):
		return Receipt{}, "", refuse(CodeAuthorityMismatch, req.Kind, "JIT grant belongs to another principal")
	case grant.Tenant != req.Tenant:
		return Receipt{}, "", refuse(CodeAuthorityMismatch, req.Kind, "JIT grant is scoped to another tenant")
	case !slices.Contains(p.Roles, grant.Role):
		return Receipt{}, "", refuse(CodeAuthorityMismatch, req.Kind, "JIT role %s does not authorize this kind", grant.Role)
	case !slices.Contains(grant.Capabilities, string(req.Kind)):
		return Receipt{}, "", refuse(CodeAuthorityMismatch, req.Kind, "JIT grant does not name this capability")
	}
	if p.DualControl {
		second := strings.TrimSpace(req.SecondApprover)
		if second == "" || strings.EqualFold(second, req.Operator) || strings.EqualFold(second, grant.Principal) {
			return Receipt{}, "", refuse(CodeDualControlRequired, req.Kind, "a second approver distinct from the operator is required")
		}
		r.SecondApprover = second
	}
	if p.SimulationRequired {
		sim := req.Simulation
		switch {
		case sim == nil || strings.TrimSpace(sim.Digest) == "":
			return Receipt{}, "", refuse(CodeSimulationRequired, req.Kind, "a simulation of this action is required first")
		case !sim.Scope.equal(req.Scope):
			return Receipt{}, "", refuse(CodeSimulationRequired, req.Kind, "the simulation covered a different scope")
		case sim.At.IsZero() || sim.At.After(now) || now.Sub(sim.At) > maxSimulationAge:
			return Receipt{}, "", refuse(CodeSimulationRequired, req.Kind, "the simulation is missing its instant, from the future or stale")
		}
		r.SimulationDigest = sim.Digest
	}
	if err := grant.Use(action, now); err != nil {
		return Receipt{}, "", &Error{Code: CodeAuthorityInactive, Kind: req.Kind, Detail: "JIT grant refused the use", Err: err}
	}
	r.AuthorityKind, r.AuthorityRef = AuthorityJIT, grant.ID
	// The approver of record is the second approver when the policy demanded
	// one, and otherwise the approver who granted the JIT authority.
	approver := r.SecondApprover
	if approver == "" {
		approver = grant.Approver
	}
	return r, approver, nil
}
