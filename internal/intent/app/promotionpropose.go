package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// The intent-only `promotion.propose` request contract (PROMO-007).
//
// hcmnext.journey.v1.JourneyService.ProposePromotion is the same governed act
// ProposeJourney performs, stated as a closed contract rather than as one
// page's form. What a caller may say is exactly an intention: who the subject
// is, what is desired for them, when it takes effect, why, which subject
// revision the intention was formed against, and which client request this is.
//
// The two halves of "intent-only" are enforced here, and they are different
// claims. The first is structural: the generated message has no field a caller
// could use to assert a server-owned fact, and [promotionProposeContractError]
// refuses to serve at all if one is ever added, so the absence is a property of
// the build rather than of somebody's memory. The second is per-request: a
// caller who appends such a fact as an unknown or extension field on the wire
// is refused, because a smuggled assertion that is silently dropped is
// indistinguishable, from the caller's side, from one that was accepted.
//
// Everything the request does not say, this cell reads. The current placement
// comes from the governed worker read through the capability gateway, exactly
// as [journeyEngine.Propose]'s does; the compensation baseline, the
// annualization and the budget authority come from the corpus
// internal/domains/promotion certifies. Propose and its preflight simulation
// write no domain state: what they record is chronology, and what they compute
// is a proposal.

// promotionProposeContractFields is the closed allowlist: every field
// hcmnext.journey.v1.ProposePromotionRequest is permitted to have, in the
// order the canonical request digest encodes them.
//
// It is a hand-written list on purpose. Deriving it from the generated message
// would make it agree with the message by construction and therefore assert
// nothing; written out, it is a second, independent statement of the contract
// that a new field has to be reconciled with before this cell will serve.
var promotionProposeContractFields = []string{
	"subject_worker_ref",
	"desired_job_code",
	"desired_grade",
	"desired_position_id",
	"desired_org_unit",
	"desired_manager_ref",
	"desired_base_pay",
	"desired_pay_currency",
	"effective_date",
	"reason",
	"expected_subject_revision",
	"client_request_id",
}

// promotionProposeRequiredFields are the fields a request must actually carry.
// desired_position_id, desired_org_unit, desired_manager_ref and
// desired_pay_currency are optional: an empty position makes no claim on a
// vacant slot, while the other empty values mean current organization,
// unchanged manager and the subject's own currency. A supplied position is
// still checked against the governed position store before execution.
var promotionProposeRequiredFields = []string{
	"subject_worker_ref",
	"desired_job_code",
	"desired_grade",
	"desired_base_pay",
	"effective_date",
	"reason",
	"expected_subject_revision",
	"client_request_id",
}

// PromotionProposeContractFields returns a copy of the closed allowlist, so a
// conformance test, an operator report or a client generator can read the
// contract from the one place that enforces it.
func PromotionProposeContractFields() []string {
	out := make([]string, len(promotionProposeContractFields))
	copy(out, promotionProposeContractFields)
	return out
}

// Reason references this contract owns.
const (
	reasonPromotionContractDrift  = "promotion.propose.contract_drift"
	reasonPromotionSmuggledFields = "promotion.propose.smuggled_fields"
	reasonPromotionStaleSubject   = "promotion.propose.stale_subject_revision"
)

// promotionProposeMessageFields reads the wire field names off the generated
// request message by reflection over its Go struct tags.
//
// It reflects over the type rather than a value: protoc-gen-go's message state
// carries a do-not-copy marker, and a composite literal of the message here
// would be a value that must never be copied.
func promotionProposeMessageFields() []string {
	t := reflect.TypeOf((*journeyv1.ProposePromotionRequest)(nil)).Elem()
	out := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("protobuf")
		if tag == "" {
			continue
		}
		for _, part := range strings.Split(tag, ",") {
			if name, ok := strings.CutPrefix(part, "name="); ok {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// promotionProposeContractError is the structural half of "intent-only",
// evaluated once per process: the generated message's field set has to be
// exactly the allowlist. A field added to the .proto that nobody added here -
// which is how a server-owned fact would arrive - makes every ProposePromotion
// call fail closed rather than quietly accepting the new field.
var promotionProposeContractError = sync.OnceValue(checkPromotionProposeContract)

func checkPromotionProposeContract() *envelope.Error {
	allowed := map[string]bool{}
	for _, name := range promotionProposeContractFields {
		allowed[name] = true
	}
	present := map[string]bool{}
	var unexpected []string
	for _, name := range promotionProposeMessageFields() {
		present[name] = true
		if !allowed[name] {
			unexpected = append(unexpected, name)
		}
	}
	var missing []string
	for _, name := range promotionProposeContractFields {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	if len(unexpected) == 0 && len(missing) == 0 {
		return nil
	}
	owned := envelope.New(envelope.CodeFailedPrecondition, reasonPromotionContractDrift,
		"a precondition for the operation is not met")
	for _, name := range unexpected {
		owned = owned.WithViolation(name,
			"the promotion.propose request message carries a field the intent-only contract does not allow",
			reasonPromotionContractDrift)
	}
	for _, name := range missing {
		owned = owned.WithViolation(name,
			"the intent-only contract names a field the promotion.propose request message does not carry",
			reasonPromotionContractDrift)
	}
	return owned
}

// promotionProposeValues projects one request onto the allowlist, in
// allowlist order, with every value trimmed. It is the one place the contract
// is turned into data: validation, the canonical digest and the request
// payload all read this rather than reaching for getters of their own, so a
// field can never be validated under one name and digested under another.
func promotionProposeValues(req *journeyv1.ProposePromotionRequest) map[string]string {
	return map[string]string{
		"subject_worker_ref":        strings.TrimSpace(req.GetSubjectWorkerRef()),
		"desired_job_code":          strings.TrimSpace(req.GetDesiredJobCode()),
		"desired_grade":             strings.TrimSpace(req.GetDesiredGrade()),
		"desired_position_id":       strings.TrimSpace(req.GetDesiredPositionId()),
		"desired_org_unit":          strings.TrimSpace(req.GetDesiredOrgUnit()),
		"desired_manager_ref":       strings.TrimSpace(req.GetDesiredManagerRef()),
		"desired_base_pay":          strings.TrimSpace(req.GetDesiredBasePay()),
		"desired_pay_currency":      strings.TrimSpace(req.GetDesiredPayCurrency()),
		"effective_date":            strings.TrimSpace(req.GetEffectiveDate()),
		"reason":                    strings.TrimSpace(req.GetReason()),
		"expected_subject_revision": strings.TrimSpace(req.GetExpectedSubjectRevision()),
		"client_request_id":         strings.TrimSpace(req.GetClientRequestId()),
	}
}

// promotionProposeUnknownBytes is how many bytes of unknown (or extension)
// fields the decoded message is holding.
//
// Proto3 preserves what it could not interpret rather than discarding it, so
// this is the exact wire evidence that a caller appended something the
// contract does not define - the only remaining way to try to assert a
// server-owned fact once the message itself has nowhere to put one. It reads
// the message's own reflection rather than a copy, and names no Protobuf
// runtime type, so this package still imports no Protobuf runtime package.
func promotionProposeUnknownBytes(req *journeyv1.ProposePromotionRequest) int {
	return len(req.ProtoReflect().GetUnknown())
}

// validatePromotionPropose is the whole request-side gate: the contract has
// not drifted, nothing was smuggled onto the wire, every required field is
// present, and the effective date is a date.
func validatePromotionPropose(req *journeyv1.ProposePromotionRequest) error {
	if req == nil {
		return journeyInputError("(request)", "is required")
	}
	if owned := promotionProposeContractError(); owned != nil {
		return owned
	}
	if n := promotionProposeUnknownBytes(req); n > 0 {
		return envelope.New(envelope.CodeInvalidArgument, reasonPromotionSmuggledFields,
			"the request is malformed or structurally invalid").
			WithViolation("(unknown_fields)",
				"the request carries "+strconv.Itoa(n)+
					" bytes of fields the intent-only promotion.propose contract does not define; "+
					"current pay, current manager, position vacancy, budget authority and the "+
					"security context are resolved by the server and cannot be asserted",
				reasonPromotionSmuggledFields)
	}
	values := promotionProposeValues(req)
	for _, field := range promotionProposeRequiredFields {
		if values[field] == "" {
			return journeyInputError(field, "is required")
		}
	}
	if _, err := parsePromotionEffectiveDate(values["effective_date"]); err != nil {
		return journeyInputError("effective_date", "is not an ISO-8601 date (YYYY-MM-DD)")
	}
	return nil
}

// parsePromotionEffectiveDate is the one date parse this contract performs,
// named so the refusal and the payload cannot disagree about what was read.
func parsePromotionEffectiveDate(text string) (values.LocalDate, error) {
	return values.ParseLocalDate(text)
}

// PromotionSubjectRevision is the fixed-corpus revision coordinate a
// promotion.propose request must state as expected_subject_revision.
//
// It is the subject's compensation revision coordinate,
// "<revision_stream>@<revision_sequence>", and it is derived from the worker
// key. Created workers instead publish their durable row's actual revision in
// ListWorkers and are checked against that value. Callers should use the
// listed SubjectRevision, which keeps later worker revisions visible.
func PromotionSubjectRevision(workerKey string) string {
	return promotionRevisionStream(workerKey) + "@" + promotionRevisionSequence
}

func promotionSubjectRevision(subject WorkerLocation) string {
	if subject.Created != nil {
		return fmt.Sprintf("%s@%d", subject.Created.RevisionStream, subject.Created.RevisionSequence)
	}
	return PromotionSubjectRevision(subject.Key)
}

// promotionRevisionStream and promotionRevisionSequence are the fixed-corpus
// stream and sequence [journeyRequestPayload] writes into the request's
// compensation sides. Created workers use the revision stored on their row.
const promotionRevisionSequence = "1"

func promotionRevisionStream(workerKey string) string {
	return "rewards.package." + journeySanitize(workerKey)
}

// promotionRequestExtras are the three things the intent-only contract can
// say that the older page form cannot, carried into [journeyRequestPayload].
//
// They travel as one optional value rather than as three new parameters so
// that the page form's own call is unchanged and its payload - and therefore
// its digest - is byte-for-byte what it has always been.
type promotionRequestExtras struct {
	// targetOrgUnit is the desired organizational unit. Empty means the one
	// the governed read disclosed, which is what a promotion that does not
	// move somebody between units means.
	targetOrgUnit string
	// managerRef is the desired manager. Empty means unchanged.
	managerRef string
	// subjectRevision is the revision the proposal was formed against,
	// recorded in the request so the proposal carries its own precondition.
	subjectRevision string
}

// PromotionProposeRequestDigest is the canonical digest of one
// promotion.propose request: "sha256:" followed by lowercase hex over the
// length-prefixed, allowlist-ordered encoding of every contract field except
// client_request_id.
//
// It is length-prefixed rather than delimited because a delimiter is a value
// some field could contain, and two different requests that encoded to one
// string would have one digest. client_request_id is excluded because it says
// which call this is and nothing about what is being proposed: two callers
// proposing the same promotion on two different transports must agree here,
// and they would not if the dedup coordinate participated.
func PromotionProposeRequestDigest(req *journeyv1.ProposePromotionRequest) string {
	values := promotionProposeValues(req)
	h := sha256.New()
	for _, field := range promotionProposeContractFields {
		if field == "client_request_id" {
			continue
		}
		value := values[field]
		fmt.Fprintf(h, "%d:%s=%d:%s\n", len(field), field, len(value), value)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// promotionProposeIdempotencyKey is the idempotency coordinate the recorded
// intent carries. Two calls presenting the same client_request_id are one
// intent, whichever transport each arrived on, and the store answers the
// second with the intent the first actually recorded.
func promotionProposeIdempotencyKey(clientRequestID string) string {
	return "promotion.propose:" + clientRequestID
}

// ---------------------------------------------------------------------------
// The governed act
// ---------------------------------------------------------------------------

// ProposePromotion implements the intent-only `promotion.propose` contract.
//
// It runs the same path [journeyEngine.Propose] does and adds nothing beside
// it: the reference is resolved against the corpus and this tenant's created
// population, the declared subject revision is checked against the one this
// cell holds, the current placement is read through the capability gateway,
// the promote_worker intent is minted by IntentService, and the preflight
// simulation is the engine's own read-only SimulateIntent. No domain row is
// written on this path; the only durable effect is the intent chronology
// CreateIntent records.
//
// It is a method on the journey engine rather than a second service because
// there is exactly one Promotion application service on this cell, and a
// second entry point that reached the domains another way would be the thing
// this contract exists to prevent.
func (e *journeyEngine) ProposePromotion(
	ctx context.Context, req *journeyv1.ProposePromotionRequest,
) (*journeyv1.ProposePromotionResponse, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := validatePromotionPropose(req); err != nil {
		return nil, err
	}
	fields := promotionProposeValues(req)

	subject, resolved, err := e.locate(ctx, principal.Tenant(), fields["subject_worker_ref"])
	if err != nil {
		return nil, err
	}
	if !resolved {
		return nil, journeyInputError("subject_worker_ref", "no such worker in this workforce")
	}
	if want := promotionSubjectRevision(subject); fields["expected_subject_revision"] != want {
		// A refusal rather than a silent re-base: the caller formed this
		// intention against a view of the subject this cell does not hold, and
		// promoting somebody from a state that is not theirs is the lost
		// update this field exists to make impossible.
		return nil, envelope.New(envelope.CodeFailedPrecondition, reasonPromotionStaleSubject,
			"a precondition for the operation is not met").
			WithViolation("expected_subject_revision",
				"the proposal was formed against a subject revision this cell does not hold",
				reasonPromotionStaleSubject)
	}

	// The older page form is the shape journeyBaseline and journeyRequestPayload
	// already read. Projecting onto it here rather than duplicating them is
	// what makes "the browser form and this contract are one capability" true
	// in the code and not only in the doc comment.
	in := workspace.ProposalInput{
		WorkerRef:        subject.Key,
		TargetJobCode:    fields["desired_job_code"],
		TargetGrade:      fields["desired_grade"],
		TargetPositionID: fields["desired_position_id"],
		ProposedBase:     fields["desired_base_pay"],
		EffectiveDate:    fields["effective_date"],
		BusinessReason:   fields["reason"],
	}
	baseline, err := journeyBaseline(in, subject)
	if err != nil {
		return nil, err
	}
	if declared := fields["desired_pay_currency"]; declared != "" && !strings.EqualFold(declared, baseline.currency) {
		return nil, journeyInputError("desired_pay_currency",
			"does not match the subject's own currency; this contract proposes a promotion, not a redenomination")
	}

	current, err := e.currentPlacement(ctx, principal, subject.Ref, baseline.effective)
	if err != nil {
		return nil, err
	}
	if err := validatePublishedPromotionPath(current, in, baseline); err != nil {
		return nil, err
	}

	def, ownedErr := e.svc.defs.Resolve(intent.Ref{TypeID: promotion.IntentType, Version: 1})
	if ownedErr != nil {
		return nil, journeyError(envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").WithDiagnostic(ownedErr))
	}
	payload, err := journeyRequestPayload(in, subject.Key, current, baseline, promotionRequestExtras{
		targetOrgUnit:   fields["desired_org_unit"],
		managerRef:      fields["desired_manager_ref"],
		subjectRevision: fields["expected_subject_revision"],
	})
	if err != nil {
		return nil, err
	}
	proposeIdempotencyKey := promotionProposeIdempotencyKey(fields["client_request_id"])

	// PROMOUX-002: the same admission boundary [journeyEngine.Propose] runs,
	// so the intent-only contract cannot be used to route around the guard
	// the page's own form is subject to. See admitPromotionWindow's doc.
	// PROMOUX-017: the shared helper abandons the reservation this call
	// opened when CreateIntent refuses it (a changed request reusing a
	// client request id on a different day must not leave a second ACTIVE
	// reservation behind).
	var created *intentsv1.CreateIntentResponse
	if _, err := e.createGuardedIntent(ctx, principal, subject.Ref.String(), fields["effective_date"], proposeIdempotencyKey,
		func() (string, error) {
			res, err := e.svc.CreateIntent(ctx, &intentsv1.CreateIntentRequest{
				IdempotencyKey: proposeIdempotencyKey,
				Definition:     &intentsv1.DefinitionReference{IntentTypeId: def.Ref.TypeID, Version: def.Ref.Version},
				// Server-derived, exactly as [journeyEngine.Propose]'s is. There is no
				// initiator field on the wire contract for a caller to fill.
				Initiator: &intentsv1.PrincipalReference{
					PrincipalId:          principal.Subject(),
					Kind:                 journeyInitiatorKind(principal.SubjectKind()),
					IdentityAssuranceRef: principal.EvidenceID(),
				},
				Subjects: journeySubjects(subject.Ref.Id, fields["desired_position_id"]),
				Request: &intentsv1.TypedPayload{
					Schema: &intentsv1.SchemaReference{
						SchemaId:         def.InputSchema.SchemaID,
						Version:          def.InputSchema.Version,
						ProtobufFullName: def.InputSchema.ProtobufFullName,
					},
					ProtobufWireBytes: payload,
				},
				ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
			})
			if err != nil {
				return "", err
			}
			created = res
			return res.GetIntent().GetIntentId(), nil
		}); err != nil {
		return nil, err
	}

	instance := created.GetIntent()
	simulated, simErr := e.resimulateDetailed(ctx, instance.GetIntentId())
	if simErr != nil {
		return nil, simErr
	}
	artifact := simulated.Artifact
	// The durable half of the contract: the snapshot the simulation read, the
	// proposal revision it minted and the result that binds them all survive
	// this process (journey_candidates.go).
	stored, convErr := protomap.InstanceFromProto(instance)
	if convErr != nil {
		return nil, journeyError(convErr)
	}
	if persistErr := e.recordProposalCandidates(ctx, principal, stored, simulated); persistErr != nil {
		return nil, journeyError(persistErr)
	}
	stage := journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED
	if artifact.GetProposalRevisionId() == "" {
		stage = journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED
	}
	return &journeyv1.ProposePromotionResponse{
		IntentId:               instance.GetIntentId(),
		CorrelationId:          instance.GetCorrelationId(),
		ProposalRevisionId:     artifact.GetProposalRevisionId(),
		MaterialDigest:         artifact.GetMaterialProposalDigest().GetDigest(),
		CanonicalRequestDigest: instance.GetCanonicalRequestDigest().GetDigest(),
		RequestDigest:          PromotionProposeRequestDigest(req),
		Stage:                  stage,
	}, nil
}
