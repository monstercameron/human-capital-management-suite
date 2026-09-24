package app

// WF-RUN-034: the promotion workflow's governed step services.
//
// Every capability a served promotion step invokes passes the same capability
// gateway the interactive surface uses, with a principal, a purpose, a
// deadline, an idempotency key and a declared effect set. Steps run without an
// authenticated caller (approval resumes, the effective-date timer), so the
// principal is the delegation runtime.Start pinned from the verified principal
// that executed the proposal, re-authorized against the current role
// assignment before every invocation: a revoked role fails the step closed.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Delegation refusals. Both are matchable with errors.Is and both fail the
// step closed: no capability is invoked behind either.
var (
	// ErrDelegationInvalid means the pinned delegation cannot be turned into
	// a principal: it names no subject, an unknown subject kind, method or
	// assurance, or a validity window that has already closed.
	ErrDelegationInvalid = errors.New("app: execution delegation is invalid")
	// ErrDelegationRevoked means the delegated subject no longer holds the
	// execution role under the current role assignment, or the cell names no
	// execution role to check.
	ErrDelegationRevoked = errors.New("app: execution delegation no longer carries the execution role")
	// ErrCommitNotAdmitted means the governed preflight at the commit boundary
	// reports findings that block the promotion as approved.
	ErrCommitNotAdmitted = errors.New("app: the promotion preflight blocks the commit")
)

// PromotionStepCall is one governed promotion step invocation.
type PromotionStepCall struct {
	// Delegation is the authority runtime.Start pinned for the instance.
	Delegation runtime.ExecutionDelegation
	// IntentID is the promotion intent the approved proposal revision binds.
	IntentID string
	// NodeID names the step, for evidence.
	NodeID string
	// IdempotencyKey names this node attempt; each invocation derives its own
	// key from it and the capability it calls.
	IdempotencyKey string
	// Deadline bounds every invocation the step makes.
	Deadline time.Time
	// DeclaredEffects is the node's permitted effect set.
	DeclaredEffects []capability.EffectClass
}

// PromotionStepAnswer is a governed step's typed result.
type PromotionStepAnswer struct {
	// Digest is the domain result digest the node records as its output.
	Digest string
	// EvidenceIDs are the gateway evidence records, one per invocation, in
	// invocation order.
	EvidenceIDs []string
	// Subject is the delegated principal every invocation was authorized as.
	Subject string
}

// PromotionStepServices runs the promotion workflow's capability invocations
// through the cell's gateway as the delegated principal.
type PromotionStepServices struct {
	svc          *IntentService
	roleAccess   roleaccess.Store
	requiredRole string
	// marketRates is the market-rate source the variant's fetch_market_rate
	// node reads through [PromotionStepServices.FetchMarketRate], bound by
	// [PromotionStepServices.SetMarketRateSource]. Nil fails the fetch
	// closed; nothing is fabricated in the meantime.
	marketRates rewards.MarketRateSource
	// reads is the gateway over the four read-only graph capabilities the
	// bootstrap table does not publish (revalidation and the observations).
	// It shares the cell's evidence sink and clock, so its decisions land in
	// the same chronology.
	reads *capability.Gateway
	now   func() time.Time
}

// governedRead is the payload a graph read capability's handler runs once
// the gateway has admitted the invocation.
type governedRead func(context.Context) (any, error)

// NewPromotionStepServices composes the step services over a cell composed
// with an execution authority.
func NewPromotionStepServices(cell *Cell) (*PromotionStepServices, error) {
	if cell == nil || cell.Service == nil || cell.Evidence == nil {
		return nil, errors.New("app: promotion step services need a composed cell")
	}
	if cell.Service.executionAuthority == nil || cell.Service.executionAuthority.RequiredRole == "" {
		return nil, errors.New("app: promotion step services need an execution authority naming its role")
	}
	svc := cell.Service
	now := func() time.Time { return svc.clock().Time() }
	return &PromotionStepServices{
		svc: svc, roleAccess: cell.RoleAccess, requiredRole: svc.executionAuthority.RequiredRole,
		reads: svc.gateway, now: now,
		marketRates: cell.MarketRateSource,
	}, nil
}

// executionDelegation pins the verified executing principal and the purpose
// the execution was authorized under. A principal with no purpose of
// processing pins nothing: its steps then find no delegation and fail closed
// rather than act under an unstated purpose.
func executionDelegation(principal *trust.Principal, purpose string) *runtime.ExecutionDelegation {
	if strings.TrimSpace(purpose) == "" {
		return nil
	}
	return &runtime.ExecutionDelegation{
		Subject: principal.Subject(), SubjectKind: principal.SubjectKind().String(),
		TenantKey: principal.Tenant().String(), OrganizationScopeID: principal.OrganizationScopeID(),
		Roles: principal.Roles(), Purposes: []string{purpose},
		AuthenticationMethod: principal.AuthenticationMethod().String(), Assurance: principal.Assurance().String(),
		SessionRef: principal.SessionRef(), EvidenceRef: principal.EvidenceID(),
	}
}

var (
	delegatedSubjectKinds = map[string]trust.SubjectKind{
		"human": trust.SubjectKindHuman, "service": trust.SubjectKindService,
		"agent": trust.SubjectKindAgent, "integration": trust.SubjectKindIntegration,
	}
	delegatedMethods = map[string]trust.AuthenticationMethod{
		"bearer_token": trust.AuthenticationMethodBearerToken, "mutual_tls": trust.AuthenticationMethodMutualTLS,
	}
	delegatedAssurance = map[string]trust.Assurance{
		"low": trust.AssuranceLow, "substantial": trust.AssuranceSubstantial, "high": trust.AssuranceHigh,
	}
)

// principal rebuilds the delegated principal for one step, bounded to the
// step's deadline, with its roles narrowed to the current role assignment.
func (p *PromotionStepServices) principal(ctx context.Context, call PromotionStepCall) (*trust.Principal, string, error) {
	d := call.Delegation
	if err := d.Validate(); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrDelegationInvalid, err)
	}
	kind, kindOK := delegatedSubjectKinds[d.SubjectKind]
	method, methodOK := delegatedMethods[d.AuthenticationMethod]
	assurance, assuranceOK := delegatedAssurance[d.Assurance]
	if !kindOK || !methodOK || !assuranceOK {
		return nil, "", fmt.Errorf("%w: unknown subject kind, authentication method or assurance", ErrDelegationInvalid)
	}
	roles := slices.Clone(d.Roles)
	if p.roleAccess != nil {
		snapshot, err := p.roleAccess.Load(ctx, values.TenantId(d.TenantKey), d.OrganizationScopeID)
		if err != nil {
			return nil, "", fmt.Errorf("%w: current role assignment is unavailable: %v", ErrDelegationRevoked, err)
		}
		current := roleaccess.AssignedRoles(snapshot, d.Subject, d.Roles)
		roles = slices.DeleteFunc(roles, func(role string) bool {
			normalized := roleaccess.NormalizeRoleIDs([]string{role})
			return len(normalized) != 1 || !slices.Contains(current, normalized[0])
		})
	}
	if p.requiredRole == "" || !slices.Contains(roles, p.requiredRole) {
		return nil, "", fmt.Errorf("%w: %s does not hold %q", ErrDelegationRevoked, d.Subject, p.requiredRole)
	}
	issued := d.RecordedAt
	if issued.IsZero() {
		issued = p.now()
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(d.TenantKey), Subject: d.Subject, SubjectKind: kind,
		OrganizationScopeID: d.OrganizationScopeID, Roles: roles, Purposes: d.Purposes,
		AuthenticationMethod: method, Assurance: assurance, SessionRef: d.SessionRef,
		DelegationRefs: []string{"delegation:workflow_execution:" + d.EvidenceRef},
		IssuedAt:       issued, ExpiresAt: call.Deadline, CredentialDigest: d.EvidenceRef,
	})
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v (node_id=%s issued_at=%s expires_at=%s)", ErrDelegationInvalid, err,
			call.NodeID, issued.UTC().Format(time.RFC3339Nano), call.Deadline.UTC().Format(time.RFC3339Nano))
	}
	return principal, d.Purposes[0], nil
}

// envelope is the governed invocation envelope one call presents for def.
func envelopeFor(call PromotionStepCall, def capability.Definition, purpose string) *capability.Invocation {
	return &capability.Invocation{
		Purpose: purpose, Deadline: call.Deadline, IdempotencyKey: call.IdempotencyKey + "/" + def.ID,
		DeclaredEffects: slices.Clone(call.DeclaredEffects),
	}
}

// gatewayRequest records the evidence id of one gateway decision on answer.
func gatewayRequest(ctx context.Context, gateway *capability.Gateway, req capability.InvokeRequest, answer *PromotionStepAnswer) (any, error) {
	result, err := gateway.Invoke(ctx, req)
	var gwErr *capability.GatewayError
	switch {
	case err == nil:
		answer.EvidenceIDs = append(answer.EvidenceIDs, result.EvidenceID)
	case errors.As(err, &gwErr):
		answer.EvidenceIDs = append(answer.EvidenceIDs, gwErr.EvidenceID)
	}
	return result.Response, err
}

// invoke calls one bound capability through the gateway with the envelope.
func (p *PromotionStepServices) invoke(ctx context.Context, gateway *capability.Gateway, def capability.Definition, principal *trust.Principal, purpose string, payload any, call PromotionStepCall, answer *PromotionStepAnswer) (any, error) {
	return gatewayRequest(ctx, gateway, capability.InvokeRequest{
		Capability: def.Key(), Payload: payload, Authorization: authorize(principal, purpose, def),
		Invocation: envelopeFor(call, def, purpose),
	}, answer)
}

// refuse records a delegation that no longer authorizes the step as a
// gateway refusal on the capability the step would have invoked, and returns
// the cause. The handler never runs.
func refuse(ctx context.Context, gateway *capability.Gateway, def capability.Definition, call PromotionStepCall, cause error, answer *PromotionStepAnswer) error {
	purpose := ""
	if len(call.Delegation.Purposes) > 0 {
		purpose = call.Delegation.Purposes[0]
	}
	_, _ = gatewayRequest(ctx, gateway, capability.InvokeRequest{
		Capability:    def.Key(),
		Authorization: capability.Authorization{Decision: capability.Deny, SubjectRef: call.Delegation.Subject, Tenant: call.Delegation.TenantKey, Reason: cause.Error()},
		Invocation:    envelopeFor(call, def, purpose),
	}, answer)
	return cause
}

// boundDefinition resolves a capability the cell's own registry publishes.
func (p *PromotionStepServices) boundDefinition(id string) (capability.Definition, error) {
	rec, found := p.svc.caps.Lookup(capabilityKeyFor(intent.Ref{TypeID: id, Version: 1}))
	if !found {
		return capability.Definition{}, fmt.Errorf("app: capability %s is not published in this cell", id)
	}
	return rec.Definition, nil
}

// bound invokes one capability the cell's own registry publishes.
func (p *PromotionStepServices) bound(ctx context.Context, id string, principal *trust.Principal, purpose string, payload any, call PromotionStepCall, answer *PromotionStepAnswer) (any, error) {
	def, err := p.boundDefinition(id)
	if err != nil {
		return nil, err
	}
	return p.invoke(ctx, p.svc.gateway, def, principal, purpose, payload, call, answer)
}

// promotionSession is one step's authorized view of the promotion request.
type promotionSession struct {
	principal *trust.Principal
	purpose   string
	call      DomainCall
	answer    PromotionStepAnswer
}

// open authorizes the delegation and resolves the intent's promotion request
// under it. A delegation that no longer authorizes the step is recorded as a
// refusal of first, the capability the step would invoke first.
func (p *PromotionStepServices) open(ctx context.Context, call PromotionStepCall, first string) (*promotionSession, error) {
	principal, purpose, err := p.principal(ctx, call)
	if err != nil {
		if def, defErr := p.boundDefinition(first); defErr == nil {
			answer := PromotionStepAnswer{Subject: call.Delegation.Subject}
			return nil, refuse(ctx, p.svc.gateway, def, call, err, &answer)
		}
		return nil, err
	}
	inst, _, ownedErr := p.svc.loadInstance(ctx, call.Delegation.TenantKey, call.IntentID)
	if ownedErr != nil {
		return nil, fmt.Errorf("app: load promotion intent %s: %w", call.IntentID, ownedErr)
	}
	def, err := p.svc.defs.Resolve(inst.Definition)
	if err != nil {
		return nil, fmt.Errorf("app: resolve promotion definition: %w", err)
	}
	if def.Ref.TypeID != promotion.IntentType {
		return nil, fmt.Errorf("app: intent %s is %s, not a promotion", call.IntentID, def.Ref.TypeID)
	}
	resolved, err := p.svc.inputs.Resolve(ctx, ResolveRequest{Instance: inst, Definition: def, Principal: principal, Purpose: purpose})
	if err != nil {
		return nil, fmt.Errorf("app: resolve the promotion request as %s: %w", principal.Subject(), err)
	}
	if resolved.Explain == nil || resolved.Promotion == nil {
		return nil, fmt.Errorf("app: intent %s resolved no promotion request", call.IntentID)
	}
	return &promotionSession{principal: principal, purpose: purpose, call: resolved, answer: PromotionStepAnswer{Subject: principal.Subject()}}, nil
}

func (p *PromotionStepServices) explain(ctx context.Context, s *promotionSession, call PromotionStepCall) (people.Explanation, error) {
	got, err := p.bound(ctx, people.ExplainWorkerStateIntentType, s.principal, s.purpose, *s.call.Explain, call, &s.answer)
	if err != nil {
		return people.Explanation{}, err
	}
	explanation, ok := got.(people.Explanation)
	if !ok {
		return people.Explanation{}, fmt.Errorf("app: explain_worker_state returned %T", got)
	}
	return explanation, nil
}

func (p *PromotionStepServices) preflight(ctx context.Context, s *promotionSession, call PromotionStepCall) (promotion.PreflightResult, error) {
	explanation, err := p.explain(ctx, s, call)
	if err != nil {
		return promotion.PreflightResult{}, err
	}
	request := *s.call.Promotion
	request.WorkerState = explanation
	got, err := p.bound(ctx, promotion.IntentType, s.principal, s.purpose, promotionCall{Mode: promotionModePreflight, Request: request}, call, &s.answer)
	if err != nil {
		return promotion.PreflightResult{}, err
	}
	answer, ok := got.(promotionAnswer)
	if !ok {
		return promotion.PreflightResult{}, fmt.Errorf("app: promote_worker returned %T", got)
	}
	return answer.Preflight, nil
}

func (p *PromotionStepServices) simulate(ctx context.Context, s *promotionSession, call PromotionStepCall) (rewards.SimulateCompensationResult, error) {
	req := s.call.Promotion
	got, err := p.bound(ctx, rewards.SimulateCompensationIntentType, s.principal, s.purpose, rewards.SimulateCompensationInput{
		Tenant: req.Tenant, Subject: req.Subject, Current: req.Current, Proposed: req.Proposed,
		Annualization: req.Annualization, EffectiveDate: req.EffectiveDate,
	}, call, &s.answer)
	if err != nil {
		return rewards.SimulateCompensationResult{}, err
	}
	result, ok := compensationSimulation(got)
	if !ok {
		return rewards.SimulateCompensationResult{}, fmt.Errorf("app: simulate_compensation returned %T", got)
	}
	return result, nil
}

// SnapshotWorker invokes explain_worker_state for the promotion subject.
func (p *PromotionStepServices) SnapshotWorker(ctx context.Context, call PromotionStepCall) (ret0 PromotionStepAnswer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_services.snapshot_worker", call.NodeID, call.IntentID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	s, err := p.open(ctx, call, people.ExplainWorkerStateIntentType)
	if err != nil {
		return PromotionStepAnswer{}, err
	}
	explanation, err := p.explain(ctx, s, call)
	if err != nil {
		return s.answer, err
	}
	s.answer.Digest = explanation.ResultDigest
	return s.answer, nil
}

// SimulateCompensation invokes simulate_compensation for the approved pay.
func (p *PromotionStepServices) SimulateCompensation(ctx context.Context, call PromotionStepCall) (ret0 PromotionStepAnswer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_services.simulate_compensation", call.NodeID, call.IntentID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	s, err := p.open(ctx, call, rewards.SimulateCompensationIntentType)
	if err != nil {
		return PromotionStepAnswer{}, err
	}
	result, err := p.simulate(ctx, s, call)
	if err != nil {
		return s.answer, err
	}
	s.answer.Digest = result.ResultDigest
	return s.answer, nil
}

// EvaluateBand resolves the target band question through the governed
// preflight and invokes evaluate_pay_band_position for it.
func (p *PromotionStepServices) EvaluateBand(ctx context.Context, call PromotionStepCall) (ret0 PromotionStepAnswer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_services.evaluate_band", call.NodeID, call.IntentID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	s, err := p.open(ctx, call, rewards.EvaluatePayBandIntentType)
	if err != nil {
		return PromotionStepAnswer{}, err
	}
	preflight, err := p.preflight(ctx, s, call)
	if err != nil {
		return s.answer, err
	}
	if preflight.Band.State != rewards.BandResultEvaluated {
		return s.answer, fmt.Errorf("app: the promotion names no evaluable target band: %s", preflight.Band.Reason)
	}
	evaluated := preflight.Band.Evaluation
	got, err := p.bound(ctx, rewards.EvaluatePayBandIntentType, s.principal, s.purpose, PayBandInputs{Query: evaluated.Query, Amount: evaluated.Position.Amount}, call, &s.answer)
	if err != nil {
		return s.answer, err
	}
	band, ok := got.(rewards.PayBandEvaluation)
	if !ok {
		return s.answer, fmt.Errorf("app: evaluate_pay_band_position returned %T", got)
	}
	s.answer.Digest = band.ResultDigest
	return s.answer, nil
}

// ThresholdInputs sources rules.compensation.raise_threshold/v3's inputs from
// governed reads: the raise from simulate_compensation, the band position,
// budget authority and grade change from the promotion preflight. An input a
// read cannot establish is the table's explicit UNKNOWN, never a default.
func (p *PromotionStepServices) ThresholdInputs(ctx context.Context, call PromotionStepCall) (ret0 rules.PromotionApprovalInput, ret1 PromotionStepAnswer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_services.threshold_inputs", call.NodeID, call.IntentID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0, ret1) }()
	s, err := p.open(ctx, call, promotion.IntentType)
	if err != nil {
		return rules.PromotionApprovalInput{}, PromotionStepAnswer{}, err
	}
	preflight, err := p.preflight(ctx, s, call)
	if err != nil {
		return rules.PromotionApprovalInput{}, s.answer, err
	}
	simulated, err := p.simulate(ctx, s, call)
	if err != nil {
		return rules.PromotionApprovalInput{}, s.answer, err
	}
	in := rules.PromotionApprovalInput{
		IncreasePercent: simulated.Delta.IncreasePercent,
		BandPosition:    rules.BandPositionUnknown,
		BudgetAuthority: rules.BudgetAuthorityUnknown,
		GradeChange:     !strings.EqualFold(strings.TrimSpace(preflight.Input.Baseline.Grade), strings.TrimSpace(preflight.Input.Target.Grade)),
	}
	if preflight.Band.State == rewards.BandResultEvaluated {
		if position := rules.BandPosition(preflight.Band.Evaluation.Position.Placement.String()); position.Valid() {
			in.BandPosition = position
		}
	}
	if budget := preflight.Input.Budget; budget != nil {
		if available, ok := budget.AvailableAmount.Get(); ok {
			if cmp, cmpErr := available.Cmp(simulated.Delta.AnnualizedBase); cmpErr == nil {
				in.BudgetAuthority = rules.BudgetAuthorityInsufficient
				if cmp >= 0 {
					in.BudgetAuthority = rules.BudgetAuthoritySufficient
				}
			}
		}
	}
	s.answer.Digest = simulated.ResultDigest
	return in, s.answer, nil
}

// AuthorizeCommit re-runs the governed promotion preflight at the commit
// boundary, as the delegated principal, and refuses a promotion whose
// preflight now blocks. It performs no write: the commit itself is the
// caller's transactional writer.
func (p *PromotionStepServices) AuthorizeCommit(ctx context.Context, call PromotionStepCall) (ret0 PromotionStepAnswer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_services.authorize_commit", call.NodeID, call.IntentID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	s, err := p.open(ctx, call, promotion.IntentType)
	if err != nil {
		return PromotionStepAnswer{}, err
	}
	preflight, err := p.preflight(ctx, s, call)
	if err != nil {
		return s.answer, err
	}
	if blocking := preflight.Blocking(); len(blocking) > 0 {
		return s.answer, fmt.Errorf("%w: %s", ErrCommitNotAdmitted, blocking[0].Code)
	}
	s.answer.Digest = preflight.ResultDigest
	return s.answer, nil
}

// GovernedRead invokes one of the graph's local-store read capabilities
// (revalidation or an observation) through the gateway as the delegated
// principal; read runs only once the gateway admits the invocation.
func (p *PromotionStepServices) GovernedRead(ctx context.Context, call PromotionStepCall, capabilityID string, read func(context.Context) (any, error)) (ret0 any, ret1 PromotionStepAnswer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_services.governed_read", call.NodeID, capabilityID)
	defer func() { observe.DoneWith(obsOp, retErr, ret1) }()
	answer := PromotionStepAnswer{Subject: call.Delegation.Subject}
	var def capability.Definition
	for _, candidate := range promotionexec.GovernedReadDefinitions() {
		if candidate.ID == capabilityID {
			def = candidate
		}
	}
	if def.ID == "" {
		return nil, answer, fmt.Errorf("app: %s is not a governed promotion read", capabilityID)
	}
	if read == nil {
		return nil, answer, fmt.Errorf("app: governed read %s has no reader", capabilityID)
	}
	principal, purpose, err := p.principal(ctx, call)
	if err != nil {
		return nil, answer, refuse(ctx, p.reads, def, call, err, &answer)
	}
	got, err := p.invoke(ctx, p.reads, def, principal, purpose, governedRead(read), call, &answer)
	return got, answer, err
}
