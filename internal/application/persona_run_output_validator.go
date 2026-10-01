package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaChatReplySchema is the exact registered T0 plain-text response schema ID.
const PersonaChatReplySchema = "persona.chat-reply.v1"

// PersonaChatReplySchemaDigest binds the registered string schema bytes.
const PersonaChatReplySchemaDigest = "sha256:2d381ce0f49738a7040285e57a84c43c3c43793fe3d325e5cb5a4783e7c1290f"

// PersonaChatReplyJSONSchema is the canonical JSON Schema document for reply text.
const PersonaChatReplyJSONSchema = `{"type":"string","minLength":1,"maxLength":16384}`

// PersonaChatReplyProvenance is the fixed schema provenance required by admission.
const PersonaChatReplyProvenance = "persona.chat_reply.schema.v1"

var (
	// ErrPersonaRunOutputValidatorUnavailable marks missing or mismatched output authority.
	ErrPersonaRunOutputValidatorUnavailable = errors.New("application: persona run output validator unavailable")
	errPersonaChatReplyUnsafe               = errors.New("application: persona chat reply does not satisfy the plain-text schema")
	personaReplyLinkPattern                 = regexp.MustCompile(`(?i)(https?://|www\.|\[[^\]]+\]|<\s*a\b|\bsource\s*:)`)
)

// PersonaChatReply is the registered, server-owned draft shape for a T0
// persona response. It contains no caller-declared references, fields or claims.
type PersonaChatReply struct {
	// Text contains the bounded model response text.
	Text string `json:"text"`
}

// DraftFields reports that a chat reply contains no governed data fields.
func (r PersonaChatReply) DraftFields() []string { return nil }

// DraftReferences reports that a chat reply contains no record references.
func (r PersonaChatReply) DraftReferences() []string { return nil }

// DraftClaims reports that a chat reply makes no validated business claims.
func (r PersonaChatReply) DraftClaims() []string { return nil }

// DetachDraft returns an independent copy of the reply value.
func (r PersonaChatReply) DetachDraft() (agentsecurity.DraftValue, error) {
	if strings.TrimSpace(r.Text) == "" {
		return nil, errPersonaChatReplyUnsafe
	}
	return PersonaChatReply{Text: r.Text}, nil
}

// DraftCanonicalBytes returns the stable bytes bound by the gateway seal.
func (r PersonaChatReply) DraftCanonicalBytes() ([]byte, error) {
	if err := validatePersonaChatReply(r.Text); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSpace(r.Text)), nil
}

// PersonaChatReplyToolDescriptor is the sole draft descriptor accepted by the
// T0 persona response path. It must be registered in the same gateway used by
// the admission and output validator.
func PersonaChatReplyToolDescriptor() agentsecurity.ToolDescriptor {
	return agentsecurity.ToolDescriptor{
		Name: "persona.chat_reply", Capability: "persona.reply", Version: 1,
		Class: agentsecurity.ToolDraft, DataScope: []string{"chat.current"}, Cost: 1, Schema: PersonaChatReplySchema,
		Validate: func(value any) (agentsecurity.TypedResult, error) {
			reply, ok := value.(PersonaChatReply)
			if !ok || validatePersonaChatReply(reply.Text) != nil {
				return agentsecurity.TypedResult{}, errPersonaChatReplyUnsafe
			}
			return agentsecurity.TypedResult{
				Schema: PersonaChatReplySchema, Value: reply, Validated: true,
				Taint:      []string{string(agentsecurity.TaintDerived)},
				Provenance: []string{PersonaChatReplyProvenance, "chat.current"},
			}, nil
		},
	}
}

// PersonaRunOutputSchemaRef is the immutable output-schema pin resolved from
// the published persona manifest by the server-side authority source.
type PersonaRunOutputSchemaRef struct {
	ID            string // ID is the schema ID pinned by the published persona manifest.
	Version       uint32 // Version is the pinned schema version.
	Digest        string // Digest is the digest of the canonical schema document.
	PersonaDigest string // PersonaDigest binds the schema pin to the admitted persona profile.
}

// PersonaRunChatReplyAuthority contains gateway-owned authority for exactly
// one admitted persona run. Grounding must be assembled from current source
// owners and sealed by the same Gateway and Admission; model text is never
// accepted as evidence.
type PersonaRunChatReplyAuthority struct {
	Schema      PersonaRunOutputSchemaRef           // Schema is resolved from the exact published persona manifest.
	ModelDigest string                              // ModelDigest is the active route pin resolved by the server.
	Gateway     *agentsecurity.ToolGateway          // Gateway issued Admission and validates the final result.
	Admission   agentsecurity.Admission             // Admission is the opaque gateway-issued authority.
	Grounding   []agentsecurity.Datum               // Grounding holds current source-owner datums sealed by Gateway.
	References  agentsecurity.OutputReferenceOwner  // References rechecks typed record references, if any.
	Fields      agentsecurity.OutputFieldAuthorizer // Fields rechecks typed output field permissions, if any.
	Claims      agentsecurity.OutputClaimOwner      // Claims rechecks typed business claims, if any.
}

// PersonaRunChatReplyAuthoritySource resolves the current, server-owned
// output schema pin, sealed AGENT-026 admission, and grounded source material.
// It must fail closed when the published schema or any source grant is stale.
type PersonaRunChatReplyAuthoritySource interface {
	ResolvePersonaRunChatReplyAuthority(context.Context, agentrun.Record, runstate.Run) (PersonaRunChatReplyAuthority, error)
}

// PersonaRunOutputObservationBinding pins a synthetic validation case to the
// exact run, persona and model under evaluation.
type PersonaRunOutputObservationBinding struct {
	SyntheticTenantID string // SyntheticTenantID identifies the isolated evaluation tenant.
	EvaluationRunID   string // EvaluationRunID identifies the evaluator's run.
	CaseID            string // CaseID identifies one registered case.
	CaseDigest        string // CaseDigest pins the exact case request and expectations.
	PersonaDigest     string // PersonaDigest pins the published persona version.
	ModelDigest       string // ModelDigest pins the routed model version.
	InvocationID      string // InvocationID binds the synthetic persona invocation.
	ConversationID    string // ConversationID binds the private synthetic conversation.
	ThreadID          string // ThreadID binds the private synthetic thread.
	PostID            string // PostID binds the synthetic invoking post.
}

// PersonaRunOutputObservationAuthority is resolved from synthetic-run owners.
// Returned Binding must be independently checked against the registered case,
// route and tenant; it must not echo caller-provided values without checking.
type PersonaRunOutputObservationAuthority struct {
	Binding PersonaRunOutputObservationBinding
	Output  PersonaRunChatReplyAuthority
}

// PersonaRunOutputObservationAuthoritySource resolves current sealed output
// and source authority for a registered synthetic evaluation case.
type PersonaRunOutputObservationAuthoritySource interface {
	ResolvePersonaRunOutputObservationAuthority(context.Context, PersonaRunOutputObservationBinding, agentrun.Record, runstate.Run) (PersonaRunOutputObservationAuthority, error)
}

// PersonaOutputObservationVerdict is a typed safety outcome; it is not a
// caller-supplied boolean and is created only by the sealed validator.
type PersonaOutputObservationVerdict string

const (
	// PersonaOutputObservedSafe means the candidate passed schema and grounding checks.
	PersonaOutputObservedSafe PersonaOutputObservationVerdict = "SAFE"
	// PersonaOutputObservedRejected means a typed safety or grounding check refused the candidate.
	PersonaOutputObservedRejected PersonaOutputObservationVerdict = "REJECTED"
)

// PersonaOutputObservationReason identifies the deterministic output check that decided the verdict.
type PersonaOutputObservationReason string

const (
	// PersonaOutputReasonNone accompanies a safe observation.
	PersonaOutputReasonNone PersonaOutputObservationReason = ""
	// PersonaOutputReasonIncomplete identifies a refusal, partial response or tool proposal.
	PersonaOutputReasonIncomplete PersonaOutputObservationReason = "INCOMPLETE_MODEL_RESULT"
	// PersonaOutputReasonUnsafeText identifies text outside the plain-text schema.
	PersonaOutputReasonUnsafeText PersonaOutputObservationReason = "UNSAFE_TEXT"
	// PersonaOutputReasonUngrounded identifies absent or invalid sealed source evidence.
	PersonaOutputReasonUngrounded PersonaOutputObservationReason = "UNGROUNDED_OUTPUT"
	// PersonaOutputReasonSchema identifies rejection by the registered output validator.
	PersonaOutputReasonSchema PersonaOutputObservationReason = "OUTPUT_SCHEMA_REJECTED"
)

// PersonaOutputGroundingSource is the non-content provenance projection of
// one gateway-validated source citation.
type PersonaOutputGroundingSource struct {
	SourceID       string // SourceID is the tenant-scoped ID validated by the gateway.
	LocationDigest string // LocationDigest binds source location without exposing its title.
	ContentDigest  string // ContentDigest is the verified digest attached to the citation.
}

type personaRunOutputObservationSeal struct{ digest string }

// PersonaRunOutputObservation is a read-only, in-memory evaluation result.
// It contains provenance digests rather than model text or persisted output.
// The private seal makes caller-created or modified observations unverifiable.
type PersonaRunOutputObservation struct {
	Binding          PersonaRunOutputObservationBinding
	Verdict          PersonaOutputObservationVerdict
	Reason           PersonaOutputObservationReason
	Schema           PersonaRunOutputSchemaRef
	CandidateDigest  string
	OutputDigest     string
	EvidenceDigest   string
	GroundingSources []PersonaOutputGroundingSource
	Digest           string
	seal             *personaRunOutputObservationSeal
}

// Verify confirms the observation was issued unchanged by the validator.
func (o PersonaRunOutputObservation) Verify() error {
	if o.seal == nil || o.Digest == "" || o.Digest != o.seal.digest || o.Digest != personaRunOutputObservationDigest(o) {
		return ErrPersonaRunOutputValidatorUnavailable
	}
	if strings.TrimSpace(o.Binding.SyntheticTenantID) == "" || strings.TrimSpace(o.Binding.EvaluationRunID) == "" || strings.TrimSpace(o.Binding.CaseID) == "" ||
		strings.TrimSpace(o.Binding.InvocationID) == "" || strings.TrimSpace(o.Binding.ConversationID) == "" || strings.TrimSpace(o.Binding.ThreadID) == "" || strings.TrimSpace(o.Binding.PostID) == "" ||
		!validPersonaOutputObservationDigest(o.Binding.CaseDigest) || !validPersonaOutputObservationDigest(o.Binding.PersonaDigest) || !validPersonaOutputObservationDigest(o.Binding.ModelDigest) ||
		!validPersonaOutputObservationDigest(o.CandidateDigest) || !validPersonaOutputObservationDigest(o.EvidenceDigest) {
		return ErrPersonaRunOutputValidatorUnavailable
	}
	if o.Verdict == PersonaOutputObservedSafe && (o.Reason != PersonaOutputReasonNone || !validPersonaOutputObservationDigest(o.OutputDigest) || len(o.GroundingSources) == 0) {
		return ErrPersonaRunOutputValidatorUnavailable
	}
	if o.Verdict == PersonaOutputObservedRejected && o.Reason == PersonaOutputReasonNone {
		return ErrPersonaRunOutputValidatorUnavailable
	}
	if o.Verdict != PersonaOutputObservedSafe && o.Verdict != PersonaOutputObservedRejected {
		return ErrPersonaRunOutputValidatorUnavailable
	}
	return nil
}

// BindingValues returns the tenant, evaluator run, case, persona/model pins,
// and invocation context in a stable positional order for package-neutral consumers.
func (o PersonaRunOutputObservation) BindingValues() []string {
	return []string{o.Binding.SyntheticTenantID, o.Binding.EvaluationRunID, o.Binding.CaseID, o.Binding.CaseDigest,
		o.Binding.PersonaDigest, o.Binding.ModelDigest, o.Binding.InvocationID, o.Binding.ConversationID, o.Binding.ThreadID, o.Binding.PostID}
}

// VerdictCode returns the typed verdict's stable wire value.
func (o PersonaRunOutputObservation) VerdictCode() string { return string(o.Verdict) }

// ReasonCode returns the typed reason's stable wire value.
func (o PersonaRunOutputObservation) ReasonCode() string { return string(o.Reason) }

// SourceEvidence returns source ID, location digest, and content digest tuples.
func (o PersonaRunOutputObservation) SourceEvidence() [][3]string {
	result := make([][3]string, 0, len(o.GroundingSources))
	for _, source := range o.GroundingSources {
		result = append(result, [3]string{source.SourceID, source.LocationDigest, source.ContentDigest})
	}
	return result
}

// CandidateHash returns the digest of the complete provider-neutral model result.
func (o PersonaRunOutputObservation) CandidateHash() string { return o.CandidateDigest }

// OutputHash returns the gateway semantic digest when output validation passed.
func (o PersonaRunOutputObservation) OutputHash() string { return o.OutputDigest }

// EvidenceHash returns the digest of sorted, validated grounding references.
func (o PersonaRunOutputObservation) EvidenceHash() string { return o.EvidenceDigest }

// ObservationHash returns the in-memory observation seal digest.
func (o PersonaRunOutputObservation) ObservationHash() string { return o.Digest }

// SchemaValues returns the exact schema ID, version and digest validated for
// this observation.
func (o PersonaRunOutputObservation) SchemaValues() [3]string {
	return [3]string{o.Schema.ID, strconv.FormatUint(uint64(o.Schema.Version), 10), o.Schema.Digest}
}

// PersonaRunFinalOutputPersister persists only an opaque projection produced
// by ToolGateway.IssueFinalOutputPersistence and backed by a recovery receipt.
type PersonaRunFinalOutputPersister interface {
	PersistPersonaRunFinalOutput(context.Context, agentsecurity.FinalOutputPersistence) error
}

// PersonaRunFinalOutputStore is the existing tenant-scoped immutable result
// persistence API used by the concrete persona store.
type PersonaRunFinalOutputStore interface {
	PutFinalOutput(context.Context, agentsecurity.FinalOutputPersistence, *agentsecurity.FinalOutputRecoveryAuthority) error
}

// PersonaRunFinalOutputStoreFactory returns the immutable output store scoped
// to one tenant.
type PersonaRunFinalOutputStoreFactory interface {
	ForTenant(context.Context, values.TenantId) (PersonaRunFinalOutputStore, error)
}

// AgentPersonaRunFinalOutputStoreFactory scopes final-output persistence to
// the projection tenant before using the existing immutable persona store API.
type AgentPersonaRunFinalOutputStoreFactory struct {
	// Store is the root persona store scoped for each final-output write.
	Store *agentpersonastore.Store
}

// ForTenant returns the existing tenant-scoped final-output store.
func (f AgentPersonaRunFinalOutputStoreFactory) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaRunFinalOutputStore, error) {
	if f.Store == nil || ctx == nil || strings.TrimSpace(tenant.String()) == "" {
		return nil, ErrPersonaRunOutputValidatorUnavailable
	}
	return f.Store.Scoped(tenant)
}

// AgentPersonaRunFinalOutputPersister issues a server-owned recovery receipt
// by delegating to agentpersonastore.PutFinalOutput.
type AgentPersonaRunFinalOutputPersister struct {
	Stores   PersonaRunFinalOutputStoreFactory           // Stores scopes the immutable store by tenant.
	Recovery *agentsecurity.FinalOutputRecoveryAuthority // Recovery issues server-owned durable receipts.
}

// PersistPersonaRunFinalOutput writes one sealed projection into its tenant's
// immutable final-output table. The store issues and retains the recovery seal.
func (p AgentPersonaRunFinalOutputPersister) PersistPersonaRunFinalOutput(ctx context.Context, projection agentsecurity.FinalOutputPersistence) error {
	if ctx == nil || isNilPersonaOutputPort(p.Stores) || p.Recovery == nil || projection.Digest() == "" {
		return ErrPersonaRunOutputValidatorUnavailable
	}
	identity := projection.Identity()
	if strings.TrimSpace(identity.TenantID) == "" {
		return ErrPersonaRunOutputValidatorUnavailable
	}
	store, err := p.Stores.ForTenant(ctx, values.TenantId(identity.TenantID))
	if err != nil {
		return fmt.Errorf("scope persona output store: %w", err)
	}
	if isNilPersonaOutputPort(store) {
		return ErrPersonaRunOutputValidatorUnavailable
	}
	return store.PutFinalOutput(ctx, projection, p.Recovery)
}

// PersonaRunOutputValidatorConfig provides all server-owned output authority
// and persistence ports. There is no permissive fallback.
type PersonaRunOutputValidatorConfig struct {
	// Authority resolves current invocation-bound output authority.
	Authority PersonaRunChatReplyAuthoritySource
	// ObservationAuthority resolves registered synthetic fixture authority.
	ObservationAuthority PersonaRunOutputObservationAuthoritySource
	// Persister stores only gateway-sealed output projections.
	Persister PersonaRunFinalOutputPersister
}

// SealedPersonaRunOutputValidator validates a T0 reply and persists its sealed form.
type SealedPersonaRunOutputValidator struct {
	authority    PersonaRunChatReplyAuthoritySource
	observations PersonaRunOutputObservationAuthoritySource
	persister    PersonaRunFinalOutputPersister
}

// NewPersonaRunOutputValidator requires both current authority and durable
// sealed-output persistence.
func NewPersonaRunOutputValidator(config PersonaRunOutputValidatorConfig) (*SealedPersonaRunOutputValidator, error) {
	if isNilPersonaOutputPort(config.Authority) || isNilPersonaOutputPort(config.Persister) {
		return nil, ErrPersonaRunOutputValidatorUnavailable
	}
	return &SealedPersonaRunOutputValidator{authority: config.Authority, observations: config.ObservationAuthority, persister: config.Persister}, nil
}

// ValidateAndPersistPersonaOutput validates model text against the published
// chat-reply schema, grounds it only in current gateway-sealed evidence, issues
// the gateway's opaque persistence projection, and writes that projection.
func (v *SealedPersonaRunOutputValidator) ValidateAndPersistPersonaOutput(ctx context.Context, admission agentrun.Record, run runstate.Run, result agentmodel.ModelResult) (agentsecurity.FinalOutputPersistence, error) {
	if v == nil || ctx == nil || isNilPersonaOutputPort(v.authority) || isNilPersonaOutputPort(v.persister) {
		return agentsecurity.FinalOutputPersistence{}, ErrPersonaRunOutputValidatorUnavailable
	}
	if !validPersonaRunOutputBinding(admission, run, result) {
		return agentsecurity.FinalOutputPersistence{}, agentsecurity.ErrIncompleteOutput
	}
	replyText, err := normalizePersonaChatReply(result.Text)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	authority, err := v.authority.ResolvePersonaRunChatReplyAuthority(ctx, admission, run)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, fmt.Errorf("resolve persona output authority: %w", err)
	}
	if !validPersonaRunChatReplyAuthority(admission, run, authority) {
		return agentsecurity.FinalOutputPersistence{}, ErrPersonaRunOutputValidatorUnavailable
	}
	persisted, err := sealPersonaRunChatReply(ctx, admission, run, authority, replyText)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	if err := v.persister.PersistPersonaRunFinalOutput(ctx, persisted); err != nil {
		return agentsecurity.FinalOutputPersistence{}, fmt.Errorf("persist persona chat reply: %w", err)
	}
	return persisted, nil
}

// ObservePersonaRunOutput applies the same sealed schema and grounding checks
// as production delivery but never calls the persister or returns result text.
// Its synthetic authority source must bind the exact registered fixture and
// current evidence to the returned observation.
func (v *SealedPersonaRunOutputValidator) ObservePersonaRunOutput(ctx context.Context, binding PersonaRunOutputObservationBinding, admission agentrun.Record, run runstate.Run, result agentmodel.ModelResult) (PersonaRunOutputObservation, error) {
	if v == nil || ctx == nil || isNilPersonaOutputPort(v.observations) || !validPersonaRunIdentityBinding(admission, run) || !validPersonaOutputObservationBinding(binding, admission) {
		return PersonaRunOutputObservation{}, ErrPersonaRunOutputValidatorUnavailable
	}
	resolved, err := v.observations.ResolvePersonaRunOutputObservationAuthority(ctx, binding, admission, run)
	if err != nil {
		return PersonaRunOutputObservation{}, fmt.Errorf("resolve synthetic persona output authority: %w", err)
	}
	if resolved.Binding != binding || resolved.Output.ModelDigest != binding.ModelDigest || !validPersonaRunChatReplyObservationAuthority(admission, run, resolved.Output) {
		return PersonaRunOutputObservation{}, ErrPersonaRunOutputValidatorUnavailable
	}
	grounding, groundingErr := resolved.Output.Gateway.BuildAnswer(resolved.Output.Grounding)
	if groundingErr != nil {
		grounding = agentsecurity.Answer{}
	}
	observation := PersonaRunOutputObservation{
		Binding: binding, Schema: resolved.Output.Schema,
		CandidateDigest:  personaModelResultDigest(result),
		GroundingSources: personaOutputGroundingSources(grounding.Parts),
	}
	observation.EvidenceDigest = personaOutputEvidenceDigest(observation.GroundingSources)
	if !validPersonaRunModelResult(result) {
		observation.Verdict, observation.Reason = PersonaOutputObservedRejected, PersonaOutputReasonIncomplete
	} else if text, normalizeErr := normalizePersonaChatReply(result.Text); normalizeErr != nil {
		observation.Verdict, observation.Reason = PersonaOutputObservedRejected, PersonaOutputReasonUnsafeText
	} else if groundingErr != nil || len(resolved.Output.Grounding) == 0 {
		observation.Verdict, observation.Reason = PersonaOutputObservedRejected, PersonaOutputReasonUngrounded
	} else if projection, validationErr := sealPersonaRunChatReply(ctx, admission, run, resolved.Output, text); validationErr != nil {
		if errors.Is(validationErr, agentsecurity.ErrMissingCitation) || errors.Is(validationErr, agentsecurity.ErrInvalidDatum) || errors.Is(validationErr, agentsecurity.ErrQuarantined) {
			observation.Reason = PersonaOutputReasonUngrounded
		} else {
			observation.Reason = PersonaOutputReasonSchema
		}
		observation.Verdict = PersonaOutputObservedRejected
	} else {
		observation.Verdict, observation.Reason = PersonaOutputObservedSafe, PersonaOutputReasonNone
		observation.OutputDigest = projection.SemanticDigest()
	}
	observation.Digest = personaRunOutputObservationDigest(observation)
	observation.seal = &personaRunOutputObservationSeal{digest: observation.Digest}
	if err := observation.Verify(); err != nil {
		return PersonaRunOutputObservation{}, err
	}
	return observation, nil
}

func sealPersonaRunChatReply(ctx context.Context, admission agentrun.Record, run runstate.Run, authority PersonaRunChatReplyAuthority, replyText string) (agentsecurity.FinalOutputPersistence, error) {
	answerDatum, err := authority.Gateway.Infer(replyText, authority.Grounding...)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, fmt.Errorf("ground persona chat reply: %w", err)
	}
	final, err := authority.Gateway.ValidateFinalOutput(ctx, authority.Admission, "persona.chat_reply", agentsecurity.FinalOutputCandidate{
		Complete: true,
		Draft: agentsecurity.AgentOutput{
			Schema: PersonaChatReplySchema, Value: PersonaChatReply{Text: replyText}, Narrative: replyText,
		},
		Answer: []agentsecurity.Datum{answerDatum},
	}, authority.References, authority.Fields, authority.Claims)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, fmt.Errorf("validate persona chat reply: %w", err)
	}
	persisted, err := authority.Gateway.IssueFinalOutputPersistence(ctx, authority.Admission, final, personaRunFinalOutputIdentity(admission, run))
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, fmt.Errorf("seal persona chat reply: %w", err)
	}
	return persisted, nil
}

func validPersonaRunOutputBinding(admission agentrun.Record, run runstate.Run, result agentmodel.ModelResult) bool {
	return validPersonaRunIdentityBinding(admission, run) && validPersonaRunModelResult(result)
}

func validPersonaRunIdentityBinding(admission agentrun.Record, run runstate.Run) bool {
	request := admission.Request
	return admission.Decision == agentrun.DecisionAccepted && admission.ID != "" && admission.ID == run.AdmissionID && admission.ID == run.ID &&
		admission.RequestDigest != "" && admission.RequestDigest == run.RequestDigest &&
		request.Persona != nil && request.Source.TenantID != "" && request.Source.Key != "" && request.Source.Ref != "" &&
		request.Principal.InvokerID != "" && request.Audience.ID != "" && request.Context.ID != "" && request.InstallationID != "" &&
		request.Persona.ID != "" && request.Persona.Version != "" && request.Persona.Digest != "" &&
		request.Agent.AgentID == run.AgentID && request.Agent.Version == run.AgentVersion && request.Agent.Digest == run.AgentDigest &&
		run.TenantID == request.Source.TenantID && run.ActorID == request.Principal.InvokerID
}

func validPersonaRunModelResult(result agentmodel.ModelResult) bool {
	return result.Finish == agentmodel.FinishComplete && result.Failure == nil && result.Refusal == nil &&
		len(result.ToolProposals) == 0 && len(result.Structured) == 0 && strings.TrimSpace(result.Text) != ""
}

func validPersonaRunChatReplyAuthority(admission agentrun.Record, run runstate.Run, authority PersonaRunChatReplyAuthority) bool {
	return validPersonaRunChatReplyObservationAuthority(admission, run, authority) && len(authority.Grounding) > 0
}

func validPersonaRunChatReplyObservationAuthority(admission agentrun.Record, run runstate.Run, authority PersonaRunChatReplyAuthority) bool {
	request := admission.Request
	return authority.Gateway != nil && authority.Schema.ID == PersonaChatReplySchema && authority.Schema.Version == 1 &&
		authority.Schema.Digest == PersonaChatReplySchemaDigest && authority.Schema.PersonaDigest == request.Persona.Digest &&
		authority.Admission.Tenant == request.Source.TenantID && authority.Admission.AgentID == run.AgentID &&
		authority.Admission.Purpose == request.Purpose
}

func personaRunFinalOutputIdentity(admission agentrun.Record, run runstate.Run) agentsecurity.FinalOutputIdentity {
	request := admission.Request
	outputID := personaRunOutputID(request.Source.TenantID, request.Source.Key)
	return agentsecurity.FinalOutputIdentity{
		TenantID: request.Source.TenantID, OutputID: outputID, InvocationID: request.Source.Key,
		AdmissionID: admission.ID, RunID: run.ID,
		InvokerID: request.Principal.InvokerID, ConversationID: request.Audience.ID,
		ThreadID: request.Context.ID, PostID: request.Source.Ref, PersonaID: request.Persona.ID,
		PersonaVersion: request.Persona.Version, InstallationID: request.InstallationID,
	}
}

func personaRunOutputID(tenant, invocation string) string {
	sum := sha256.Sum256([]byte("hcm-next-persona-final-output/v1\x00" + tenant + "\x00" + invocation))
	return "persona-output:" + hex.EncodeToString(sum[:])
}

func normalizePersonaChatReply(text string) (string, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if err := validatePersonaChatReply(text); err != nil {
		return "", err
	}
	return text, nil
}

func validPersonaOutputObservationBinding(binding PersonaRunOutputObservationBinding, admission agentrun.Record) bool {
	return strings.TrimSpace(binding.SyntheticTenantID) != "" && binding.SyntheticTenantID == admission.Request.Source.TenantID &&
		strings.TrimSpace(binding.EvaluationRunID) != "" && strings.TrimSpace(binding.CaseID) != "" &&
		validPersonaOutputObservationDigest(binding.CaseDigest) && binding.PersonaDigest == admission.Request.Persona.Digest &&
		validPersonaOutputObservationDigest(binding.ModelDigest) && binding.InvocationID == admission.Request.Source.Key &&
		binding.ConversationID == admission.Request.Audience.ID && binding.ThreadID == admission.Request.Context.ID && binding.PostID == admission.Request.Source.Ref
}

func personaModelResultDigest(result agentmodel.ModelResult) string {
	encoded, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func personaOutputGroundingSources(parts []agentsecurity.AnswerPart) []PersonaOutputGroundingSource {
	byKey := make(map[string]PersonaOutputGroundingSource)
	for _, part := range parts {
		for _, citation := range part.Citations {
			if citation.SourceID == "" || citation.Location == "" || citation.Digest == "" {
				continue
			}
			location := sha256.Sum256([]byte(citation.Location))
			source := PersonaOutputGroundingSource{
				SourceID: citation.SourceID, LocationDigest: "sha256:" + hex.EncodeToString(location[:]), ContentDigest: citation.Digest,
			}
			byKey[source.SourceID+"\x00"+source.LocationDigest+"\x00"+source.ContentDigest] = source
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sources := make([]PersonaOutputGroundingSource, 0, len(keys))
	for _, key := range keys {
		sources = append(sources, byKey[key])
	}
	return sources
}

func personaOutputEvidenceDigest(sources []PersonaOutputGroundingSource) string {
	encoded, err := json.Marshal(sources)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte("hcm-next-persona-output-grounding/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func personaRunOutputObservationDigest(observation PersonaRunOutputObservation) string {
	encoded, err := json.Marshal(struct {
		Binding          PersonaRunOutputObservationBinding
		Verdict          PersonaOutputObservationVerdict
		Reason           PersonaOutputObservationReason
		Schema           PersonaRunOutputSchemaRef
		CandidateDigest  string
		OutputDigest     string
		EvidenceDigest   string
		GroundingSources []PersonaOutputGroundingSource
	}{observation.Binding, observation.Verdict, observation.Reason, observation.Schema, observation.CandidateDigest, observation.OutputDigest, observation.EvidenceDigest, observation.GroundingSources})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte("hcm-next-persona-output-observation/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validPersonaOutputObservationDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func validatePersonaChatReply(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len(trimmed) > 16*1024 || personaReplyLinkPattern.MatchString(trimmed) {
		return errPersonaChatReplyUnsafe
	}
	for _, char := range trimmed {
		if unicode.IsControl(char) && char != '\n' && char != '\t' {
			return errPersonaChatReplyUnsafe
		}
	}
	return nil
}
