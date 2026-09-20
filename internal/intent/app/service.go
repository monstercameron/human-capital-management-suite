package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// EnvelopeSchemaRef is the registered payload schema of the bytes a created
// intent's ledger event and outbox message carry: the marshalled
// hcmnext.intents.v1.IntentInstance.
const EnvelopeSchemaRef = "hcmnext.intents.v1.IntentInstance@1"

// Reason references this service owns. They are stable identifiers an operator
// reads in telemetry; the caller-visible condition is the envelope code.
const (
	reasonNoGovernedWrite     = "p1a.no_governed_write"
	reasonIntentNotFound      = "intent.not_found"
	reasonDefinitionUnknown   = "definition.not_found"
	reasonCapabilityUnknown   = "capability.not_found"
	reasonRequestRejected     = "intent.request_rejected"
	reasonStaleRevision       = "intent.stale_revision"
	reasonDomainUnavailable   = "intent.domain_unavailable"
	reasonAuthorizationDenied = "authorization.denied"

	rulePhaseCeiling = "release.p1a_zero_effect_ceiling"
)

// defaultPageSize bounds a list answer a caller did not size itself.
const defaultPageSize int32 = 50

// simulationRevision is the revision number a P1A simulation mints.
//
// P1A revises nothing: it simulates a stored request and answers. Revision 2
// arrives with the first governed edit, which is a P1B contract.
const simulationRevision uint64 = 1

// artifactNamespace derives the identifiers a simulation's artifacts carry.
var artifactNamespace = uuid.MustParse("2c9a5f60-6c1b-4a3c-9c0e-1c9f6a3d2b41")

// derivedIDs mints the proposal-revision and plan identifiers for one
// simulation, from the intent's own identity and canonical request digest.
//
// They are derived rather than freshly minted because a P1A simulation is a
// pure function of the stored request: simulating the same intent twice is the
// same artifact, not two. Minting a fresh UUID would put a new identifier
// inside the material proposal payload - the revision identifier is the first
// field the PROPOSAL profile hashes - and make the material digest, the thing
// an approval binds, differ on every call for reasons that have nothing to do
// with the proposal's content.
func derivedIDs(inst intent.Instance, revision uint64) intent.IDSource {
	seed := fmt.Sprintf("%s|%s|%d", inst.IntentID, inst.CanonicalRequestDigest.Digest, revision)
	ordinal := 0
	return func() (string, error) {
		ordinal++
		return uuid.NewSHA1(artifactNamespace, fmt.Appendf(nil, "%s|%d", seed, ordinal)).String(), nil
	}
}

// Options configures [NewIntentService].
type Options struct {
	// Definitions is the compiled-in intent registry (fourteen definitions).
	Definitions *intent.Registry
	// Capabilities is the published capability table this cell serves and
	// invokes through.
	Capabilities *capability.Registry
	// Gateway is the governed capability gateway. Required: there is no path
	// to a domain answer that does not go through it.
	Gateway *capability.Gateway
	// Store is the persistence port.
	Store Store
	// Inputs resolves the governed reads a P1A intent needs.
	Inputs DomainInputs
	// Digester mints the canonical request and proposal digests.
	Digester intent.Digester
	// Controls is the pinned control context.
	Controls Controls
	// IDs mints identifiers. Nil means intent.UUIDv7Source.
	IDs intent.IDSource
	// Clock supplies the recording time. Nil means time.Now in UTC.
	Clock intent.Clock
	// ProposalExecutor is the caller-driven workflow driver ExecuteIntent runs
	// an approved promotion proposal through. Nil leaves simulation and every
	// other intent operation available while EXECUTE fails closed exactly as
	// it does today (see [ExecutionAuthority]).
	ProposalExecutor ProposalExecutor
	// ExecutionAuthority is the explicit P1B gate ExecuteIntent requires
	// before it will run ProposalExecutor at all. Nil means this cell is
	// byte-for-byte the P1A cell of today: ExecuteIntent refuses under the
	// same envelope every other governed write already does, regardless of
	// whether ProposalExecutor is also set.
	ExecutionAuthority *ExecutionAuthority
	// ExecutionResolver and ExecutionVersions are the two composition-time
	// values a wire ExecuteIntent request cannot itself supply (a
	// runtime.StartRequest's Resolver and Versions are Go values, not wire
	// data): the workflow this cell resolves EXECUTE requests to, and the
	// version store it resolves them against. Either being nil leaves
	// ExecuteIntent refusing as executionUnavailable once past the
	// authority gate, exactly like a nil ProposalExecutor.
	ExecutionResolver runtime.WorkflowResolver
	ExecutionVersions workflowversion.Store
	// ExecutionCellID names the cell runtime.StartRequest.CellID records.
	// Empty means "cell-local".
	ExecutionCellID string
	// TenantUUID maps this cell's tenant key onto the uuid its composed
	// Store's own tenant table uses. It is required for ExecuteIntent to run
	// (nil leaves it refusing as executionUnavailable) because
	// internal/intent/app must not import internal/intent/app/pgstore (that
	// package already imports this one): only the composition root that
	// built the real Store knows the exact derivation its tenant rows use.
	TenantUUID func(values.TenantId) uuid.UUID
	// ExecutionFacts resolves a bound proposal revision's approval decisions
	// and supersession from stored facts, which is what lets
	// [IntentService.ExecuteIntent] stop asserting
	// runtime.ProposalBinding.Approved on its caller's word (WF-RUN-027).
	// [NewCell] supplies [DurableProposalFacts] whenever the cell was
	// composed with the execution database those facts live in.
	//
	// Nil is an incomplete composition: ExecuteIntent cannot start because
	// internal/workflow/runtime.Start requires both durable fact ports. A
	// composition that can read the facts must set this.
	ExecutionFacts ExecutionFacts
	// PromotionPlan names the promotion workflow this cell executes:
	// PromotionPlanExecute runs the executable graph, anything else (zero
	// included) is the prototype posture and never routes to the
	// high-performer variant (HIPERF-004). [NewCell] threads the
	// composition root's plan flag through; a cell composed without one
	// never pins the variant.
	PromotionPlan string
	// PerformanceRatings resolves a promotion subject's calibrated rating
	// for high-performer routing. Nil leaves every start on the plan's own
	// digest: with no rating source no subject can prove the top band.
	PerformanceRatings CalibratedRatingLookup
	// HighPerformerVariantDigest is the compiled high-performer variant
	// digest a top-band subject's start pins (HIPERF-005). Empty pins
	// nothing: the resolver serves the plan's own digest.
	HighPerformerVariantDigest string
	// Evidence is where [IntentService.ExecuteIntent] records its
	// OBS-024 GATE_REFUSED/GATE_ADMITTED evidence, through the same
	// capability evidence sink mechanism CAP-002's gateway already writes
	// through. Nil means a fresh [MemoryEvidenceSink] private to this
	// service; [NewCell] instead passes the cell's own gateway sink, so a
	// caller reads capability-invocation and authority-gate evidence back
	// from one place ([Cell.Evidence]).
	Evidence      capability.EvidenceSink
	LegalEvidence LegalEvidenceVerifier
	// Idempotency composes ENDPOINT-004's Coordinator for the three governed
	// writes beyond creation: SubmitIntent, CancelIntent and SupersedeIntent
	// each carry an idempotency_key, and exact replay of the same key and
	// payload must return the original result without re-running the
	// transition. Nil is treated as those three write surfaces being
	// unavailable ([ErrLifecycleWritesUnavailable]) rather than silently
	// skipping idempotency, exactly like a nil Coordinator in
	// internal/transport/humanwork.
	Idempotency *endpoint.Coordinator
	// SafePoints resolves the safe-point question CancelIntent asks only for
	// an EXECUTING intent. Nil leaves every such intent answered as
	// [intent.CancellationPointUnknown] (CANCELLATION_PENDING): a missing
	// port never allows a clean cancel it cannot prove.
	SafePoints SafePoints
	// WorkflowCancellation makes CancelIntent on an EXECUTING intent the
	// governed workflow cancellation (WF-RUN-010): the bound workflow
	// instance is decided and acted on durably before the intent's own
	// disposition is derived. Nil keeps the SafePoints answer.
	WorkflowCancellation WorkflowCancellation
	// AdmissionRelease frees a cleanly cancelled intent's admission
	// reservation. Nil releases nothing.
	AdmissionRelease AdmissionRelease
}

// IntentService is the application service behind both transports.
//
// It implements transport.IntentHandler and transport.RegistryHandler and
// nothing else: the transports own protocol, this owns ordering, and the
// kernel and the domain packages own every rule.
type IntentService struct {
	defs     *intent.Registry
	caps     *capability.Registry
	gateway  *capability.Gateway
	store    Store
	inputs   DomainInputs
	digester intent.Digester
	controls Controls
	ids      intent.IDSource
	clock    intent.Clock
	executor ProposalExecutor
	// proposalDecisioner is present only when the service is composed with
	// the durable workflow database. It is the one shared implementation used
	// by the public decision operations and the journey page.
	proposalDecisioner proposalDecisioner
	// executionAuthority is nil for every P1A cell composed today.
	// [IntentService.ExecuteIntent] is the only reader.
	executionAuthority *ExecutionAuthority
	executionResolver  runtime.WorkflowResolver
	executionVersions  workflowversion.Store
	executionCellID    string
	// executionPlan, performanceRatings and highPerformerVariantDigest are
	// HIPERF-004/005's rating-driven routing: only a top-band subject under
	// the execute plan pins the variant digest (see highPerformerPin).
	executionPlan              string
	performanceRatings         CalibratedRatingLookup
	highPerformerVariantDigest string
	tenantUUID                 func(values.TenantId) uuid.UUID
	// executionFacts is WF-RUN-027's durable approval/supersession reader.
	// Nil makes [IntentService.executionStart] produce a request that
	// runtime.Start refuses because no durable facts source is available.
	executionFacts ExecutionFacts
	// evidence is OBS-024's GATE_REFUSED/GATE_ADMITTED recorder.
	// [IntentService.ExecuteIntent] is the only reader.
	evidence      capability.EvidenceSink
	legalEvidence LegalEvidenceVerifier
	// idempotency is ENDPOINT-004's Coordinator, shared by SubmitIntent,
	// CancelIntent and SupersedeIntent. Nil leaves all three unavailable.
	idempotency *endpoint.Coordinator
	// promotionAdmissionMu closes the scan/append window for promotion
	// creation within one service. The store's idempotency constraint remains
	// the cross-process replay fence; this lock makes the active-worker guard
	// itself atomic for concurrent callers of this service.
	promotionAdmissionMu sync.Mutex
	// safePoints is CancelIntent's safe-point resolver. Nil means "unknown"
	// for every EXECUTING intent.
	safePoints SafePoints
	// workflowCancel is CancelIntent's governed workflow cancellation; nil
	// keeps the safePoints answer.
	workflowCancel WorkflowCancellation
	// releaseAdmission frees a cleanly cancelled intent's admission
	// reservation; nil releases nothing.
	releaseAdmission AdmissionRelease
}

var (
	_ transport.IntentHandler   = (*IntentService)(nil)
	_ transport.RegistryHandler = (*IntentService)(nil)
)

// NewIntentService validates the wiring and returns the service.
func NewIntentService(opts Options) (*IntentService, error) {
	switch {
	case opts.Definitions == nil:
		return nil, errors.New("app: an intent definition registry is required")
	case opts.Capabilities == nil:
		return nil, errors.New("app: a capability registry is required")
	case opts.Gateway == nil:
		return nil, errors.New("app: a capability gateway is required; no domain answer bypasses it")
	case opts.Store == nil:
		return nil, errors.New("app: a Store is required")
	case opts.Inputs == nil:
		return nil, errors.New("app: a DomainInputs port is required")
	case opts.Digester == nil:
		return nil, errors.New("app: a digester is required")
	}
	svc := &IntentService{
		defs:     opts.Definitions,
		caps:     opts.Capabilities,
		gateway:  opts.Gateway,
		store:    opts.Store,
		inputs:   opts.Inputs,
		digester: opts.Digester,
		controls: opts.Controls,
		ids:      opts.IDs,
		clock:    opts.Clock,
		executor: opts.ProposalExecutor,

		executionAuthority: opts.ExecutionAuthority,
		executionResolver:  opts.ExecutionResolver,
		executionVersions:  opts.ExecutionVersions,
		executionCellID:    opts.ExecutionCellID,

		executionPlan:              opts.PromotionPlan,
		performanceRatings:         opts.PerformanceRatings,
		highPerformerVariantDigest: opts.HighPerformerVariantDigest,
		tenantUUID:                 opts.TenantUUID,
		executionFacts:             opts.ExecutionFacts,
		evidence:                   opts.Evidence,
		legalEvidence:              opts.LegalEvidence,
		idempotency:                opts.Idempotency,
		safePoints:                 opts.SafePoints,
		workflowCancel:             opts.WorkflowCancellation,
		releaseAdmission:           opts.AdmissionRelease,
	}
	if svc.ids == nil {
		svc.ids = intent.UUIDv7Source
	}
	if svc.clock == nil {
		svc.clock = func() values.Instant { return values.NewInstant(time.Now().UTC()) }
	}
	if svc.evidence == nil {
		svc.evidence = NewMemoryEvidenceSink()
	}
	return svc, nil
}

// ---------------------------------------------------------------------------
// Shared plumbing
// ---------------------------------------------------------------------------

// caller returns the verified principal and the resolved invocation, or the
// typed refusal. Admission has already run; a missing principal here is a
// wiring fault, not a caller mistake, and it fails closed.
func caller(ctx context.Context) (*trust.Principal, *transport.Invocation, *envelope.Error) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return nil, nil, envelope.New(envelope.CodeUnauthenticated, "authentication.no_principal",
			"the request carries no valid authentication")
	}
	inv, _ := transport.InvocationFromContext(ctx)
	return principal, inv, nil
}

// purposeOf returns the resolved purpose of processing for this invocation.
func purposeOf(principal *trust.Principal, inv *transport.Invocation) string {
	if inv != nil && inv.Purpose() != "" {
		return inv.Purpose()
	}
	return principal.DefaultPurpose()
}

// resolveDefinition maps a wire definition reference onto a published
// definition.
func (s *IntentService) resolveDefinition(ref *intentsv1.DefinitionReference, forInstantiation bool) (intent.Definition, *envelope.Error) {
	kernelRef, err := protomap.DefinitionRefFromProto(ref)
	if err != nil {
		return intent.Definition{}, envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").
			WithViolation("definition", "the definition reference is not a valid (intent_type_id, version) pair", rulePhaseCeiling).
			WithDiagnostic(err)
	}
	resolve := s.defs.Resolve
	if forInstantiation {
		resolve = s.defs.ResolveForInstantiation
	}
	def, err := resolve(kernelRef)
	if err != nil {
		return intent.Definition{}, envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").
			WithViolation("definition.intent_type_id", "no definition is published at this identifier and version", rulePhaseCeiling).
			WithDiagnostic(err)
	}
	return def, nil
}

// invoke runs one domain answer through the governed capability gateway.
//
// There is deliberately no second path. Every domain result this service
// returns was produced by a handler the gateway resolved, authorized, checked
// for effect class and recorded evidence for; a refusal is projected with the
// gateway's own stable code rather than re-derived from message text.
func (s *IntentService) invoke(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	key capability.Key,
	payload any,
) (any, string, *envelope.Error) {
	rec, found := s.caps.Lookup(key)
	if !found {
		return nil, "", envelope.New(envelope.CodeNotFound, reasonCapabilityUnknown,
			"the resource does not exist or is not visible").
			WithViolation("capability_id", "no capability is published at this identifier and version", rulePhaseCeiling)
	}
	result, err := s.gateway.Invoke(ctx, capability.InvokeRequest{
		Capability:    key,
		Payload:       payload,
		Authorization: authorize(principal, purpose, rec.Definition),
	})
	if err != nil {
		return nil, "", gatewayError(err)
	}
	return result.Response, result.EvidenceID, nil
}

// gatewayError projects a capability refusal onto the owned error model.
func gatewayError(err error) *envelope.Error {
	var gwErr *capability.GatewayError
	if !errors.As(err, &gwErr) {
		return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
	code := envelope.CodeUnavailable
	switch gwErr.Code {
	case capability.CodeUnauthorized:
		code = envelope.CodePermissionDenied
	case capability.CodeUnknownCapability:
		code = envelope.CodeNotFound
	case capability.CodeCapabilityDisabled, capability.CodeWriteEffectRefusedP1A:
		code = envelope.CodeFailedPrecondition
	case capability.CodeHandlerFailed:
		code = envelope.CodeFailedPrecondition
	}
	owned := envelope.New(code, "capability."+strings.ToLower(gwErr.Code),
		"the capability invocation was refused").
		WithViolation("capability_id", gwErr.Reason, "capability_gateway."+strings.ToLower(gwErr.Code)).
		WithDiagnostic(err)
	if gwErr.EvidenceID != "" {
		owned = owned.WithEvidence(envelope.Evidence{ID: gwErr.EvidenceID, Kind: "capability_invocation"})
	}
	return owned
}

// loadInstance reads one stored intent back as the typed kernel envelope.
func (s *IntentService) loadInstance(ctx context.Context, tenant, intentID string) (intent.Instance, IntentRecord, *envelope.Error) {
	rec, err := s.store.LoadIntent(ctx, tenant, intentID)
	if err != nil {
		if errors.Is(err, ErrIntentNotFound) {
			return intent.Instance{}, IntentRecord{}, notFound()
		}
		return intent.Instance{}, IntentRecord{}, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
	inst, decodeErr := decodeEnvelope(rec.Envelope)
	if decodeErr != nil {
		return intent.Instance{}, IntentRecord{}, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(decodeErr)
	}
	mergeCurrentProjection(&inst, rec)
	return inst, rec, nil
}

// mergeCurrentProjection lays a record's current projection over its decoded
// envelope. The ledger envelope is the immutable creation fact. Lifecycle and
// version are the projection that terminal outcome consumers advance, so a
// read merges the current projection back onto that envelope. ListIntents
// once skipped this and reported every intent at its creation-time
// lifecycle, so a withdrawn proposal still listed as open.
func mergeCurrentProjection(inst *intent.Instance, rec IntentRecord) {
	inst.Lifecycle = rec.Lifecycle
	inst.CommitReceiptRef = rec.CommitReceiptRef
	inst.RepairRef = rec.RepairRef
	inst.InstanceVersion = rec.InstanceVersion
	if !rec.RecordedAt.IsZero() {
		inst.RecordedAt = values.NewInstant(rec.RecordedAt)
	}
	if !rec.LastTransitionAt.IsZero() {
		inst.LastTransitionAt = values.NewInstant(rec.LastTransitionAt)
	}
}

// notFound is the one visibility answer. It never distinguishes "does not
// exist" from "not visible to you".
func notFound() *envelope.Error {
	return envelope.New(envelope.CodeNotFound, reasonIntentNotFound,
		"the resource does not exist or is not visible").
		WithViolation("intent_id", "no intent is visible at this identifier", "intent.visibility")
}

// encodeEnvelope marshals the typed instance for the ledger.
func encodeEnvelope(inst intent.Instance) ([]byte, error) {
	msg, err := protomap.InstanceToProto(inst)
	if err != nil {
		return nil, err
	}
	return protomap.MarshalDeterministic(msg)
}

// decodeEnvelope reads a stored envelope back into the kernel type.
func decodeEnvelope(wire []byte) (intent.Instance, error) {
	var msg intentsv1.IntentInstance
	if err := protomap.Unmarshal(wire, &msg); err != nil {
		return intent.Instance{}, fmt.Errorf("app: stored intent envelope does not decode: %w", err)
	}
	return protomap.InstanceFromProto(&msg)
}

// ---------------------------------------------------------------------------
// IntentHandler
// ---------------------------------------------------------------------------

// CreateIntent drafts one typed intent and records it as chronology.
//
// This is the only write path in P1A, and what it writes is not workforce
// state: one ledger event carrying the typed envelope, one projection advance
// and one outbox message, in one transaction. A replay of the same idempotency
// key returns the original intent and writes nothing.
func (s *IntentService) CreateIntent(ctx context.Context, req *intentsv1.CreateIntentRequest) (*intentsv1.CreateIntentResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	def, ownedErr := s.resolveDefinition(req.GetDefinition(), true)
	if ownedErr != nil {
		return nil, ownedErr
	}

	spec, ownedErr := s.specFor(req, def, principal, inv)
	if ownedErr != nil {
		return nil, ownedErr
	}
	// Promotion admission is the integrity boundary for overlapping starts.
	// Hold the service-local lock across the active scan and append below: a
	// UI suppression or a read-only availability projection cannot prevent two
	// callers from racing here.
	promotionAdmission := def.Ref.TypeID == promotion.IntentType
	promotionPreGuarded := promotionAdmissionAlreadyGuarded(req.GetIdempotencyKey())
	if promotionAdmission && !promotionPreGuarded {
		s.promotionAdmissionMu.Lock()
		defer s.promotionAdmissionMu.Unlock()
	}

	var (
		inst intent.Instance
		err  error
	)
	if def.Family == intent.FamilyChangeRequest {
		inst, _, err = intent.Draft(spec, def, s.digester, s.ids, s.clock)
	} else {
		inst, _, err = intent.NewInstance(spec, def, s.digester, s.ids, s.clock)
	}
	if err != nil {
		return nil, kernelRejection(err)
	}
	if promotionAdmission && !promotionPreGuarded {
		workerID := promotionEmploymentSubject(inst.Subjects)
		if workerID != "" {
			active, found, scanErr := s.findActivePromotion(ctx, string(inst.Tenant), workerID)
			if scanErr != nil {
				return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
					"the operation could not be completed").WithDiagnostic(scanErr)
			}
			if found {
				// Exact replays resolve to the original intent, preserving the
				// normal idempotency contract even though the active guard runs
				// before the store's append path.
				if active.IdempotencyKey == inst.IdempotencyKey {
					// The digest reference is scoped to its intent ID. A newly
					// drafted replay has a fresh ID even though its material is
					// identical, so comparing the two references directly rejects
					// every legitimate replay. Recompute the candidate under the
					// stored intent's scope before deciding whether it is exact.
					candidate := inst
					candidate.IntentID = active.IntentID
					replayDigest, digestErr := s.digester.RequestDigest(candidate)
					if digestErr != nil {
						return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
							"the operation could not be completed").WithDiagnostic(digestErr)
					}
					if active.CanonicalRequestDigest.AlgorithmID == replayDigest.AlgorithmID &&
						active.CanonicalRequestDigest.Digest == replayDigest.Digest {
						msg, convertErr := protomap.InstanceToProto(active)
						if convertErr != nil {
							return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
								"the operation could not be completed").WithDiagnostic(convertErr)
						}
						return &intentsv1.CreateIntentResponse{Intent: msg}, nil
					}
				}
				return nil, envelope.New(envelope.CodeAlreadyExists, reasonPromotionActive,
					"an active promotion already exists for this employee").
					WithViolation("subjects", "open or complete the existing promotion before starting another", reasonPromotionActive)
			}
		}
	}

	envelopeBytes, err := encodeEnvelope(inst)
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}

	result, err := s.store.AppendIntent(ctx, IntentRecord{
		Tenant:            string(inst.Tenant),
		IntentID:          inst.IntentID,
		Definition:        inst.Definition,
		IdempotencyKey:    inst.IdempotencyKey,
		CorrelationID:     inst.CorrelationID,
		Lifecycle:         inst.Lifecycle,
		InstanceVersion:   inst.InstanceVersion,
		RequestDigest:     inst.CanonicalRequestDigest,
		CreatedAt:         inst.CreatedAt.Time(),
		RecordedAt:        inst.RecordedAt.Time(),
		LastTransitionAt:  inst.LastTransitionAt.Time(),
		Envelope:          envelopeBytes,
		EnvelopeSchemaRef: EnvelopeSchemaRef,
	})
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}

	// A replay returns the intent that was actually recorded, not the one this
	// call happened to mint: two calls with one idempotency key are one intent.
	stored, decodeErr := decodeEnvelope(result.Record.Envelope)
	if decodeErr != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(decodeErr)
	}
	msg, err := protomap.InstanceToProto(stored)
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
	return &intentsv1.CreateIntentResponse{Intent: msg}, nil
}

// promotionAdmissionAlreadyGuarded identifies the two owned callers that
// reserve the worker/effective-date window in promotion_active_intent_guard
// before creating the intent. Re-running the legacy worker-wide scan for those
// calls would erase the effective-date distinction the database guard owns.
func promotionAdmissionAlreadyGuarded(idempotencyKey string) bool {
	return strings.HasPrefix(idempotencyKey, "journey:propose:") ||
		strings.HasPrefix(idempotencyKey, "promotion.propose:")
}

// specFor projects the wire request plus the server-derived trusted context
// onto the kernel's creation request.
//
// Every field the kernel calls trusted comes from the principal or from the
// definition, never from the request body: the tenant, the organization scope,
// the purpose, the initiator, the classification floor and the retention class
// are not caller-selectable, and admission has already rejected a request that
// tried.
func (s *IntentService) specFor(
	req *intentsv1.CreateIntentRequest,
	def intent.Definition,
	principal *trust.Principal,
	inv *transport.Invocation,
) (intent.InstanceSpec, *envelope.Error) {
	payload, err := protomap.PayloadFromProto(req.GetRequest())
	if err != nil {
		return intent.InstanceSpec{}, envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").
			WithViolation("request", "the typed payload is not a valid schema-bearing payload", rulePhaseCeiling).
			WithDiagnostic(err)
	}
	initiator, err := protomap.PrincipalFromProto(req.GetInitiator())
	if err != nil {
		return intent.InstanceSpec{}, envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").
			WithViolation("initiator", "the server-derived initiator is not a valid principal reference", rulePhaseCeiling).
			WithDiagnostic(err)
	}
	mode, err := protomap.ModeFromProto(req.GetExecutionMode())
	if err != nil {
		return intent.InstanceSpec{}, envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").
			WithViolation("execution_mode", "the request declares no execution mode", rulePhaseCeiling).
			WithDiagnostic(err)
	}

	subjects := make([]intent.SubjectReference, 0, len(req.GetSubjects()))
	for _, s := range req.GetSubjects() {
		subjects = append(subjects, intent.SubjectReference{
			Kind:            s.GetSubjectKind(),
			SubjectID:       s.GetSubjectId(),
			AuthorityDomain: s.GetAuthorityDomain(),
		})
	}

	correlation := principal.EvidenceID()
	trace := principal.EvidenceID()
	if inv != nil {
		correlation = inv.RequestID()
		trace = inv.TrustedFingerprint()
	}

	// The trusted origin is derived here and nowhere else. A request that
	// cannot produce one is refused rather than recorded without it: an intent
	// whose origin is unknown cannot be investigated later, and "unknown"
	// would be indistinguishable from "not recorded yet".
	origin, err := deriveOrigin(principal, inv)
	if err != nil {
		return intent.InstanceSpec{}, envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").
			WithViolation("(origin)", "the trusted origin of the request could not be established", rulePhaseCeiling).
			WithDiagnostic(err)
	}

	spec := intent.InstanceSpec{
		Tenant:                        principal.Tenant(),
		OrganizationScopeID:           principal.OrganizationScopeID(),
		Initiator:                     initiator,
		Purpose:                       purposeOf(principal, inv),
		Subjects:                      subjects,
		Request:                       payload,
		IdempotencyKey:                req.GetIdempotencyKey(),
		CorrelationID:                 correlation,
		TraceID:                       trace,
		Classification:                def.DataClassificationFloor,
		RetentionClass:                def.RetentionClass,
		ControlSnapshots:              s.controls.Snapshots,
		ExecutionMode:                 mode,
		SourceAuthoritySnapshotDigest: s.controls.SourceAuthorityDigest,
		RiskContextDigest:             s.controls.RiskContextDigest,
		Origin:                        origin,
	}
	if ref := req.GetOriginEventRef(); ref != "" {
		spec.OriginEventRef = &ref
	}
	if at, effErr := requestedEffectiveAt(payload); effErr == nil && at != nil {
		spec.RequestedEffectiveAt = at
	}
	return spec, nil
}

// requestedEffectiveAt reads the effective time out of the P1A request payload.
//
// The kernel refuses to guess an effective time (changerequest.go reports
// INVALID_EFFECTIVE_DATE rather than defaulting one), so this reads what the
// caller actually asked for and passes nothing along when the caller said
// nothing.
func requestedEffectiveAt(payload intent.TypedPayload) (*values.Instant, error) {
	fields, err := decodeStruct(payload.WireBytes)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{"effective_date", "effective_on", "as_of"} {
		date, dateErr := localDate(fields, path)
		if dateErr != nil {
			continue
		}
		at := values.NewInstant(time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC))
		return &at, nil
	}
	return nil, nil
}

// kernelRejection projects a kernel validation failure onto the owned model.
// A malformed or inadmissible request is the caller's to fix; anything else is
// this cell's fault and is reported as such.
func kernelRejection(err error) *envelope.Error {
	switch {
	case errors.Is(err, intent.ErrInvalidInstance),
		errors.Is(err, intent.ErrUntypedPayload),
		errors.Is(err, intent.ErrInvalidReference),
		errors.Is(err, intent.ErrInitiatorNotAllowed),
		errors.Is(err, intent.ErrModeNotAllowed),
		errors.Is(err, intent.ErrInvalidDefinition):
		return envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").
			WithViolation("(request)", "the request is not admissible under its definition", rulePhaseCeiling).
			WithDiagnostic(err)
	case errors.Is(err, intent.ErrEffectInPreflight):
		return envelope.New(envelope.CodeFailedPrecondition, reasonNoGovernedWrite,
			"a precondition for the operation is not met").
			WithViolation("definition", "the definition is not zero-effect in its scheduled release", rulePhaseCeiling).
			WithDiagnostic(err)
	default:
		return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
}

// GetIntent returns one stored intent.
func (s *IntentService) GetIntent(ctx context.Context, req *intentsv1.GetIntentRequest) (*intentsv1.GetIntentResponse, error) {
	principal, _, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	inst, _, ownedErr := s.loadInstance(ctx, principal.Tenant().String(), req.GetIntentId())
	if ownedErr != nil {
		return nil, ownedErr
	}
	msg, err := protomap.InstanceToProto(inst)
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
	return &intentsv1.GetIntentResponse{Intent: msg}, nil
}

// ListIntents returns one bounded page of the tenant's intents.
func (s *IntentService) ListIntents(ctx context.Context, req *intentsv1.ListIntentsRequest) (*intentsv1.ListIntentsResponse, error) {
	principal, _, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	size := req.GetPage().GetPageSize()
	if size <= 0 {
		size = defaultPageSize
	}
	page, err := s.store.ListIntents(ctx, principal.Tenant().String(), size, req.GetPage().GetCursor())
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
	out := &intentsv1.ListIntentsResponse{
		Intents: make([]*intentsv1.IntentInstance, 0, len(page.Records)),
		Page:    &commonv1.PageResponse{NextCursor: page.NextCursor},
	}
	for _, rec := range page.Records {
		inst, decodeErr := decodeEnvelope(rec.Envelope)
		if decodeErr != nil {
			return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
				"the operation could not be completed").WithDiagnostic(decodeErr)
		}
		mergeCurrentProjection(&inst, rec)
		msg, convErr := protomap.InstanceToProto(inst)
		if convErr != nil {
			return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
				"the operation could not be completed").WithDiagnostic(convErr)
		}
		out.Intents = append(out.Intents, msg)
	}
	return out, nil
}

// SimulateIntent preflights and simulates a stored intent and returns the
// immutable artifact, with the zero-effect receipt that proves it changed
// nothing.
//
// It writes nothing at all. The proposal revision it mints and the plan it
// compiles are values in the answer; P1A binds no approval and executes no
// plan, so persisting them would record authority this release does not have.
func (s *IntentService) SimulateIntent(ctx context.Context, req *intentsv1.SimulateIntentRequest) (*intentsv1.SimulateIntentResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	inst, rec, ownedErr := s.loadInstance(ctx, principal.Tenant().String(), req.GetIntentId())
	if ownedErr != nil {
		return nil, ownedErr
	}
	if want := req.GetExpectedInstanceVersion(); want != 0 && want != rec.InstanceVersion {
		return nil, envelope.New(envelope.CodeFailedPrecondition, reasonStaleRevision,
			"a precondition for the operation is not met").
			WithViolation("expected_instance_version", "the expected instance version is stale", "intent.expected_revision")
	}
	def, err := s.defs.Resolve(inst.Definition)
	if err != nil {
		return nil, envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").WithDiagnostic(err)
	}

	artifact, ownedErr := s.simulate(ctx, principal, purposeOf(principal, inv), inst, def)
	if ownedErr != nil {
		return nil, ownedErr
	}
	return &intentsv1.SimulateIntentResponse{Simulation: artifact}, nil
}

// SubmitIntent, CancelIntent and SupersedeIntent are implemented in
// lifecycle_endpoints.go (EP-INTENT-003). They are declared there rather than
// here only because this file was already at the size where one more RPC
// family belonged in its own file; they are exactly as much a part of
// IntentHandler as every method above.

// p1aRefusal is the one typed answer for a method whose semantics are a
// governed write.
//
// It is a refusal, not an omission: the method is published, it authenticates,
// it validates, and then it states plainly that this release grants no write
// authority. Returning UNIMPLEMENTED would say the contract is missing;
// returning FAILED_PRECONDITION says the contract exists and the precondition
// (a released authority topology) is not met, which is the truth
// (planning/next-steps.md, "P1A - paid observation, preflight and simulation").
func p1aRefusal(method, description string) *envelope.Error {
	return envelope.Newf(envelope.CodeFailedPrecondition, reasonNoGovernedWrite,
		"a precondition for the operation is not met").
		WithViolation(strings.ToLower(method),
			"P1A grants no write authority, so "+description+" has no governed implementation in this release",
			rulePhaseCeiling)
}

// ExplainIntent returns the intent's lifecycle history and the digests that
// make it auditable.
func (s *IntentService) ExplainIntent(ctx context.Context, req *intentsv1.ExplainIntentRequest) (*intentsv1.ExplainIntentResponse, error) {
	principal, _, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	tenant := principal.Tenant().String()
	inst, rec, ownedErr := s.loadInstance(ctx, tenant, req.GetIntentId())
	if ownedErr != nil {
		return nil, ownedErr
	}
	entries, err := s.store.Timeline(ctx, tenant, req.GetIntentId())
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}

	refs := []*commonv1.EvidenceRef{{
		EvidenceId:   inst.CanonicalRequestDigest.AlgorithmID + ":" + inst.CanonicalRequestDigest.Digest,
		EvidenceKind: "canonical_request_digest",
	}}
	var lines []string
	lines = append(lines, fmt.Sprintf("intent %s (%s) is at %s",
		inst.IntentID, inst.Definition, inst.Lifecycle))
	for _, entry := range entries {
		refs = append(refs, &commonv1.EvidenceRef{
			EvidenceId:   entry.DigestAlgorithm + ":" + entry.Digest,
			EvidenceKind: "ledger_event",
		})
		lines = append(lines, fmt.Sprintf("seq %d %s recorded %s",
			entry.Sequence, entry.Kind, entry.RecordedAt.UTC().Format(time.RFC3339)))
	}
	for _, rev := range inst.ProposalRevisions {
		refs = append(refs, &commonv1.EvidenceRef{
			EvidenceId:   rev.MaterialDigest.AlgorithmID + ":" + rev.MaterialDigest.Digest,
			EvidenceKind: "material_proposal_digest",
		})
	}
	lines = append(lines, fmt.Sprintf("instance version %d, request digest %s",
		rec.InstanceVersion, inst.CanonicalRequestDigest.Digest))

	return &intentsv1.ExplainIntentResponse{
		IntentId:        inst.IntentID,
		EvidenceRefs:    refs,
		ExplanationText: strings.Join(lines, "\n"),
	}, nil
}

// ListIntentTimeline returns the intent's chronology, read from the
// authoritative ledger rather than from a status column.
func (s *IntentService) ListIntentTimeline(ctx context.Context, req *intentsv1.ListIntentTimelineRequest) (*intentsv1.ListIntentTimelineResponse, error) {
	principal, _, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	tenant := principal.Tenant().String()
	if _, _, ownedErr := s.loadInstance(ctx, tenant, req.GetIntentId()); ownedErr != nil {
		return nil, ownedErr
	}
	entries, err := s.store.Timeline(ctx, tenant, req.GetIntentId())
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
	out := &intentsv1.ListIntentTimelineResponse{
		Events: make([]*intentsv1.TimelineEvent, 0, len(entries)),
		Page:   &commonv1.PageResponse{},
	}
	for _, entry := range entries {
		out.Events = append(out.Events, &intentsv1.TimelineEvent{
			EventId:     entry.EventID,
			Kind:        entry.Kind,
			OccurredAt:  protomap.InstantToProto(values.NewInstant(entry.OccurredAt)),
			Description: fmt.Sprintf("%s at sequence %d under schema %s", entry.Kind, entry.Sequence, entry.SchemaRef),
			EvidenceRef: &commonv1.EvidenceRef{
				EvidenceId:   entry.DigestAlgorithm + ":" + entry.Digest,
				EvidenceKind: "ledger_event",
			},
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Simulation
// ---------------------------------------------------------------------------

// simulate runs the P1A read/preflight/simulate path for one stored intent.
type simulationResult struct {
	Artifact *intentsv1.SimulationArtifact
	Revision *intent.ProposalRevision
}

func (s *IntentService) simulate(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	inst intent.Instance,
	def intent.Definition,
) (*intentsv1.SimulationArtifact, *envelope.Error) {
	result, ownedErr := s.simulateDetailed(ctx, principal, purpose, inst, def)
	return result.Artifact, ownedErr
}

func (s *IntentService) simulateDetailed(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	inst intent.Instance,
	def intent.Definition,
) (simulationResult, *envelope.Error) {
	return s.simulateDetailedWithRelationships(ctx, principal, purpose, inst, def, nil)
}

// simulateDetailedWithRelationships is the narrow journey-inspection variant
// of simulateDetailed. The public simulation API always calls the wrapper
// above with no additional scope facts; only the current owner of a durable
// approval work item receives the one assignment-derived relationship used to
// review that proposal.
func (s *IntentService) simulateDetailedWithRelationships(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	inst intent.Instance,
	def intent.Definition,
	relationships []authz.RelationshipFact,
) (simulationResult, *envelope.Error) {
	call, err := s.inputs.Resolve(ctx, ResolveRequest{
		Instance:      inst,
		Definition:    def,
		Principal:     principal,
		Purpose:       purpose,
		Relationships: relationships,
	})
	if err != nil {
		if errors.Is(err, ErrAuthorizationDenied) {
			return simulationResult{}, authorizationRefusal(err)
		}
		return simulationResult{}, envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").
			WithViolation("request", "the typed request payload could not be resolved into a governed domain read", rulePhaseCeiling).
			WithDiagnostic(err)
	}

	// The governed read runs first and is the only source of baseline facts.
	var explanation people.Explanation
	if call.Explain != nil {
		answer, _, ownedErr := s.invoke(ctx, principal, purpose,
			capabilityKeyFor(intent.Ref{TypeID: people.ExplainWorkerStateIntentType, Version: 1}), *call.Explain)
		if ownedErr != nil {
			return simulationResult{}, ownedErr
		}
		got, ok := answer.(people.Explanation)
		if !ok {
			return simulationResult{}, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
				"the operation could not be completed").
				WithDiagnostic(fmt.Errorf("app: explain_worker_state returned %T", answer))
		}
		explanation = got
	}

	switch {
	case call.Promotion != nil:
		return s.simulatePromotion(ctx, principal, purpose, inst, def, call, explanation)
	case call.Compensation != nil:
		artifact, err := s.simulateCompensation(ctx, principal, purpose, inst, def, call)
		return simulationResult{Artifact: artifact}, err
	case call.PayBand != nil:
		artifact, err := s.simulatePayBand(ctx, principal, purpose, inst, def, call)
		return simulationResult{Artifact: artifact}, err
	case call.Drift != nil:
		artifact, err := s.simulateDrift(ctx, principal, purpose, inst, def, call)
		return simulationResult{Artifact: artifact}, err
	case call.Repair != nil:
		artifact, err := s.simulateRepair(ctx, principal, purpose, inst, def, call)
		return simulationResult{Artifact: artifact}, err
	case call.Transaction != nil:
		artifact, err := s.simulateTransaction(ctx, principal, purpose, inst, def, call)
		return simulationResult{Artifact: artifact}, err
	default:
		artifact, err := s.simulateExplanation(ctx, inst, def, call, explanation)
		return simulationResult{Artifact: artifact}, err
	}
}

// authorizationRefusal projects a policy denial onto the owned error model.
//
// It is PERMISSION_DENIED rather than NOT_FOUND because the caller is known,
// the resource is known to exist to somebody, and what failed is the ruling.
// The violation carries the policy's own reason token and never the value or
// the field the caller was refused, so a refusal cannot be read as an oracle
// for what would have been returned.
func authorizationRefusal(err error) *envelope.Error {
	return envelope.New(envelope.CodePermissionDenied, reasonAuthorizationDenied,
		"the caller is not authorized to read this under the resolved purpose").
		WithViolation("purpose", "the evaluated authorization policy denies this read", AuthorizationPolicyVersion).
		WithDiagnostic(err)
}

// simulatePromotion runs the complete P1A promotion path: governed read,
// kernel plus domain preflight, deterministic simulation, immutable proposal
// revision, compiled non-executable plan, zero-effect receipt.
func (s *IntentService) simulatePromotion(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	inst intent.Instance,
	def intent.Definition,
	call DomainCall,
	explanation people.Explanation,
) (simulationResult, *envelope.Error) {
	request := *call.Promotion
	request.WorkerState = explanation

	key := capabilityKeyFor(def.Ref)
	preflighter := &gatewayPreflighter{svc: s, principal: principal, purpose: purpose, key: key, request: request}

	kernelResult, err := intent.Preflight(ctx, intent.PreflightRequest{
		Instance:   inst,
		Definition: def,
		Baseline:   call.Baseline,
	}, s.defs, preflighter)
	if err != nil {
		return simulationResult{}, kernelRejection(err)
	}

	answer, _, ownedErr := s.invoke(ctx, principal, purpose, key,
		promotionCall{Mode: promotionModeSimulate, Request: request})
	if ownedErr != nil {
		return simulationResult{}, ownedErr
	}
	simulated, ok := answer.(promotionAnswer)
	if !ok {
		return simulationResult{}, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").
			WithDiagnostic(fmt.Errorf("app: promote_worker returned %T", answer))
	}

	artifact := &intentsv1.SimulationArtifact{
		IntentId:          inst.IntentID,
		PlannedWrites:     plannedWritesProto(simulated.Simulation),
		Findings:          findingsProto(kernelResult, simulated.Preflight),
		Uncertainty:       uncertaintyProto(simulated.Simulation),
		ZeroEffectReceipt: receiptProto(simulated.Simulation.Receipt, simulated.Simulation.Effects),
	}
	var minted *intent.ProposalRevision

	// A proposal is minted only from a READY preflight. A blocked promotion is
	// still simulated and still answered - that is the product - but it never
	// produces an artifact an approval could bind.
	if kernelResult.Ready() && simulated.Simulation.Executable {
		ledger := intent.NewProposalLedger(inst.IntentID)
		spec, specErr := proposalFor(inst, def, request, simulated.Simulation, call.Baseline, s.controls.Snapshots, simulationRevision, call.ManagerWorkerID)
		if specErr != nil {
			return simulationResult{}, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
				"the operation could not be completed").WithDiagnostic(specErr)
		}
		artifactIDs := derivedIDs(inst, simulationRevision)
		rev, revErr := intent.NewProposalRevision(spec, def, s.digester, artifactIDs, s.clock)
		if revErr != nil {
			return simulationResult{}, kernelRejection(revErr)
		}
		if appendErr := ledger.Append(rev); appendErr != nil {
			return simulationResult{}, kernelRejection(appendErr)
		}
		planInput, planErr := planFor(inst, def, rev, request, call.Baseline, s.controls, true)
		if planErr != nil {
			return simulationResult{}, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
				"the operation could not be completed").WithDiagnostic(planErr)
		}
		plan, compileErr := intent.CompilePlan(planInput, artifactIDs)
		if compileErr != nil {
			return simulationResult{}, kernelRejection(compileErr)
		}
		if verifyErr := plan.VerifyDigest(); verifyErr != nil {
			return simulationResult{}, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
				"the operation could not be completed").WithDiagnostic(verifyErr)
		}

		artifact.ProposalRevisionId = rev.ProposalRevisionID
		artifact.MaterialProposalDigest = rev.MaterialDigest.ToProto()
		artifact.PlannedEffects = plannedEffectsProto(plan)
		minted = &rev
	}
	return simulationResult{Artifact: artifact, Revision: minted}, nil
}

// simulateCompensation answers a simulate_compensation intent.
func (s *IntentService) simulateCompensation(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	inst intent.Instance,
	def intent.Definition,
	call DomainCall,
) (*intentsv1.SimulationArtifact, *envelope.Error) {
	kernelResult, err := intent.Preflight(ctx, intent.PreflightRequest{
		Instance: inst, Definition: def, Baseline: call.Baseline,
	}, s.defs, nil)
	if err != nil {
		return nil, kernelRejection(err)
	}
	answer, _, ownedErr := s.invoke(ctx, principal, purpose, capabilityKeyFor(def.Ref), *call.Compensation)
	if ownedErr != nil {
		return nil, ownedErr
	}
	result, ok := answer.(rewards.SimulateCompensationResult)
	if !ok {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").
			WithDiagnostic(fmt.Errorf("app: simulate_compensation returned %T", answer))
	}
	return &intentsv1.SimulationArtifact{
		IntentId:          inst.IntentID,
		Findings:          findingsProto(kernelResult, promotion.PreflightResult{}),
		ZeroEffectReceipt: receiptProto(result.Receipt, result.Effects),
	}, nil
}

// simulatePayBand answers an evaluate_pay_band_position intent.
func (s *IntentService) simulatePayBand(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	inst intent.Instance,
	def intent.Definition,
	call DomainCall,
) (*intentsv1.SimulationArtifact, *envelope.Error) {
	kernelResult, err := intent.Preflight(ctx, intent.PreflightRequest{
		Instance: inst, Definition: def, Baseline: call.Baseline,
	}, s.defs, nil)
	if err != nil {
		return nil, kernelRejection(err)
	}
	answer, _, ownedErr := s.invoke(ctx, principal, purpose, capabilityKeyFor(def.Ref), *call.PayBand)
	if ownedErr != nil {
		return nil, ownedErr
	}
	result, ok := answer.(rewards.PayBandEvaluation)
	if !ok {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").
			WithDiagnostic(fmt.Errorf("app: evaluate_pay_band_position returned %T", answer))
	}
	receipt, receiptErr := evidence.NewZeroEffectReceipt(
		result.IntentType, result.IntentVersion, evidence.ModeSimulate,
		evidence.RequestStateSimulated,
		[]evidence.ControlVersion{
			{Name: "catalog", Version: result.CatalogVersion},
			{Name: "rule_pack", Version: result.RulePackVersion},
		},
		result.InputsDigest, result.ResultDigest, result.Effects)
	if receiptErr != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(receiptErr)
	}
	return &intentsv1.SimulationArtifact{
		IntentId:          inst.IntentID,
		Findings:          findingsProto(kernelResult, promotion.PreflightResult{}),
		ZeroEffectReceipt: receiptProto(receipt, result.Effects),
	}, nil
}

// simulateExplanation answers an explain_worker_state intent.
func (s *IntentService) simulateExplanation(
	ctx context.Context,
	inst intent.Instance,
	def intent.Definition,
	call DomainCall,
	explanation people.Explanation,
) (*intentsv1.SimulationArtifact, *envelope.Error) {
	kernelResult, err := intent.Preflight(ctx, intent.PreflightRequest{
		Instance: inst, Definition: def, Baseline: call.Baseline,
	}, s.defs, nil)
	if err != nil {
		return nil, kernelRejection(err)
	}
	return &intentsv1.SimulationArtifact{
		IntentId:          inst.IntentID,
		Findings:          findingsProto(kernelResult, promotion.PreflightResult{}),
		ZeroEffectReceipt: receiptProto(explanation.Receipt, explanation.Effects),
	}, nil
}

// gatewayPreflighter is the intent.Preflighter adapter over the promotion
// domain. Every call goes through the capability gateway, so a domain preflight
// is authorized, effect-class checked and evidenced exactly like every other
// invocation.
type gatewayPreflighter struct {
	svc       *IntentService
	principal *trust.Principal
	purpose   string
	key       capability.Key
	request   promotion.PreflightRequest
}

// Preflight implements intent.Preflighter.
//
// It never populates DeclaredEffects. That field exists so that a domain which
// mistakenly declares an effect is rejected rather than obeyed, and the
// promotion package has no channel to declare one: it returns counters that
// must be zero and a receipt whose constructor refuses a non-zero count.
func (p *gatewayPreflighter) Preflight(ctx context.Context, req intent.PreflightRequest) (intent.DomainPreflightResult, error) {
	answer, _, ownedErr := p.svc.invoke(ctx, p.principal, p.purpose, p.key,
		promotionCall{Mode: promotionModePreflight, Request: p.request})
	if ownedErr != nil {
		return intent.DomainPreflightResult{}, ownedErr
	}
	result, ok := answer.(promotionAnswer)
	if !ok {
		return intent.DomainPreflightResult{}, fmt.Errorf("app: promote_worker preflight returned %T", answer)
	}
	if !result.Preflight.Effects.IsZero() {
		return intent.DomainPreflightResult{}, fmt.Errorf(
			"app: %s preflight counted effects %v; P1A preflight is zero-effect",
			req.Definition.Ref, result.Preflight.Effects.NonZero())
	}
	return intent.DomainPreflightResult{
		Status:   preflightStatus(result.Preflight.Status),
		Findings: domainFindings(result.Preflight),
	}, nil
}

// preflightStatus maps the promotion verdict onto the kernel's.
func preflightStatus(s promotion.Status) intent.PreflightStatus {
	switch s {
	case promotion.StatusReady:
		return intent.PreflightReady
	case promotion.StatusNeedsData:
		return intent.PreflightNeedsData
	case promotion.StatusBlocked:
		return intent.PreflightBlocked
	case promotion.StatusDenied:
		return intent.PreflightDenied
	default:
		return intent.PreflightUnspecified
	}
}

// domainFindings maps the promotion findings onto the kernel's finding shape,
// preserving the domain's own stable code in the detail so nothing is lost.
func domainFindings(result promotion.PreflightResult) []intent.Finding {
	out := make([]intent.Finding, 0, len(result.Findings))
	for _, f := range result.Findings {
		out = append(out, domainFindingKernelProjection(f))
	}
	return out
}

// domainFindingKernelProjection is the single typed bridge used both when a
// promotion finding participates in the kernel preflight status and when the
// wire projection identifies that bridge row again. Keeping the comparison on
// typed source values prevents presentation wording or locale from becoming a
// deduplication key.
func domainFindingKernelProjection(f promotion.Finding) intent.Finding {
	code := intent.FindingUnknownReference
	status := intent.PreflightBlocked
	switch f.Severity {
	case promotion.SeverityNeedsData:
		code, status = intent.FindingMissingRequired, intent.PreflightNeedsData
	case promotion.SeverityDenied:
		code, status = intent.FindingAuthorityDenied, intent.PreflightDenied
	case promotion.SeverityBlocking:
		code, status = intent.FindingUnknownReference, intent.PreflightBlocked
	case promotion.SeverityAdvisory:
		code, status = intent.FindingUnknownReference, intent.PreflightReady
	}
	return intent.Finding{
		Code:      code,
		FieldPath: f.Field,
		Detail:    f.Code + ": " + f.Message,
		Status:    status,
	}
}

// ---------------------------------------------------------------------------
// Projection onto the wire artifact
// ---------------------------------------------------------------------------

func plannedWritesProto(sim promotion.SimulationResult) []*intentsv1.PlannedWrite {
	out := make([]*intentsv1.PlannedWrite, 0, len(sim.Projected.Changes))
	for _, change := range sim.Projected.Changes {
		if !change.Changed {
			continue
		}
		out = append(out, &intentsv1.PlannedWrite{
			TargetRef: sim.Projected.Worker.String() + "#" + change.Field,
			Operation: "SET",
			EvidenceRef: &commonv1.EvidenceRef{
				EvidenceId:   sim.ResultDigest,
				EvidenceKind: "promotion_simulation",
			},
		})
	}
	return out
}

// plannedEffectsProto reports what the compiled plan says would happen. A
// zero-effect release compiles a plan with no outbox effect at all, so this is
// the plan's participants and appends rendered as the caller-visible "what
// would happen" list, never a queue of things about to happen.
func plannedEffectsProto(plan intent.TransactionPlan) []*intentsv1.PlannedEffect {
	out := make([]*intentsv1.PlannedEffect, 0, len(plan.Appends))
	for _, append_ := range plan.Appends {
		out = append(out, &intentsv1.PlannedEffect{
			EffectKind:  "LEDGER_APPEND",
			TargetRef:   append_.StreamID,
			Description: fmt.Sprintf("would append %s at sequence %d (plan %s, not executable)", append_.EventType, append_.ExpectedSequence, plan.PlanID),
		})
	}
	return out
}

func findingsProto(kernelResult intent.PreflightResult, domain promotion.PreflightResult) []*intentsv1.Finding {
	out := make([]*intentsv1.Finding, 0, len(kernelResult.Findings)+len(domain.Findings))
	for _, f := range kernelResult.Findings {
		if mirrorsPromotionFinding(f, domain.Findings) {
			continue
		}
		out = append(out, &intentsv1.Finding{
			Code:     string(f.Code),
			Message:  f.Detail,
			Severity: f.Status.String(),
		})
	}
	for _, f := range domain.Findings {
		out = append(out, &intentsv1.Finding{
			Code:     f.Code,
			Message:  promotionFindingMessage(f),
			Severity: f.Severity.String(),
		})
	}
	return out
}

func mirrorsPromotionFinding(kernel intent.Finding, domain []promotion.Finding) bool {
	for _, finding := range domain {
		if kernel == domainFindingKernelProjection(finding) {
			return true
		}
	}
	return false
}

func promotionFindingMessage(f promotion.Finding) string {
	if f.Code == promotion.CodeBudgetObservationOnly {
		return "Finance confirmed the current budget baseline. Funds are reserved only when the promotion is recorded."
	}
	return f.Message
}

func uncertaintyProto(sim promotion.SimulationResult) []*intentsv1.UncertaintyNote {
	out := make([]*intentsv1.UncertaintyNote, 0, len(sim.Compensation.Assumptions))
	for _, a := range sim.Compensation.Assumptions {
		out = append(out, &intentsv1.UncertaintyNote{
			Dimension:   a.Key,
			Description: a.Value + ": " + a.Reason,
		})
	}
	if sim.CompensationReason != "" {
		out = append(out, &intentsv1.UncertaintyNote{
			Dimension:   "compensation_state",
			Description: sim.CompensationReason,
		})
	}
	return out
}

// receiptProto projects the domain's zero-effect receipt onto the wire.
//
// zero_effect is computed from the counters rather than asserted: a receipt
// that could claim zero without the counters agreeing would be the one thing
// this whole release is selling.
func receiptProto(receipt evidence.ZeroEffectReceipt, counters evidence.EffectCounters) *intentsv1.ZeroEffectReceipt {
	if counters.IsZero() && receipt.Validate() == nil {
		return &intentsv1.ZeroEffectReceipt{
			ZeroEffect: true,
			ReasonRef: fmt.Sprintf("%s/%s:%s:%s",
				receipt.IntentType, receipt.IntentVersion, receipt.Mode, receipt.ResultDigest),
		}
	}
	return &intentsv1.ZeroEffectReceipt{
		ZeroEffect: false,
		ReasonRef:  "effects.observed:" + strings.Join(counters.NonZero(), ","),
	}
}
