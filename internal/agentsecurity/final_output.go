package agentsecurity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

// ErrIncompleteOutput marks a provider stream that has not reached a final response.
var ErrIncompleteOutput = errors.New("agent output is incomplete")

// FinalOutputCandidate contains the full response envelope. Partial provider
// streams are represented explicitly and cannot be promoted to a final result.
type FinalOutputCandidate struct {
	Complete bool
	Draft    AgentOutput
	Answer   []Datum
}

// FinalOutput is an immutable, gateway-sealed result suitable for a delivery
// owner to inspect and reauthorize. Its contents are private to prevent callers
// from replacing validated data between validation and the audience check.
type FinalOutput struct {
	gateway          *ToolGateway
	admissionReceipt string
	draft            DraftOutput
	answer           Answer
	receipt          string
}

// FinalOutputIdentity binds a persistence projection to the invocation and
// delivery context that produced it. Every component is required and is
// checked against the admitted tenant before a projection is issued.
type FinalOutputIdentity struct {
	TenantID       string
	OutputID       string
	InvocationID   string
	AdmissionID    string
	RunID          string
	InvokerID      string
	ConversationID string
	ThreadID       string
	PostID         string
	PersonaID      string
	PersonaVersion string
	InstallationID string
}

// FinalOutputPersistence is an opaque, detached projection of a validated
// final output. Its payload, materials, citations, and digests cannot be
// supplied or replaced by a persistence caller.
type FinalOutputPersistence struct {
	identity        FinalOutputIdentity
	payload         DraftOutput
	answer          Answer
	materials       []OutputMaterial
	citations       []Citation
	semanticDigest  string
	admissionDigest string
	digest          string
}

// FinalOutputRecipient is the tenant-scoped identity whose current access must
// be checked before the result is committed to a destination.
type FinalOutputRecipient struct {
	TenantID  string
	SubjectID string
}

// OutputMaterialKind identifies the governed output element being rechecked.
type OutputMaterialKind string

const (
	OutputRecord OutputMaterialKind = "RECORD"
	OutputField  OutputMaterialKind = "FIELD"
	OutputClaim  OutputMaterialKind = "CLAIM"
	OutputSource OutputMaterialKind = "SOURCE"
)

// OutputMaterial binds an authorized identifier and, for answer sources, the
// exact validated text that the delivery adapter associates with it. Values
// are transient and must not be logged.
type OutputMaterial struct {
	Kind  OutputMaterialKind
	ID    string
	Value string
}

// OutputAudienceAuthorizer is the current AuthZ owner for result disclosure.
// It must resolve grants at call time and fail closed when that decision is
// unavailable or the recipient no longer has access.
type OutputAudienceAuthorizer interface {
	AuthorizeOutput(context.Context, string, string, FinalOutputRecipient, OutputMaterial) error
}

// ValidateFinalOutput validates the complete typed result and semantic answer
// with the same gateway that issued admission. The returned value is opaque and
// cannot be delivered until ReauthorizeFinalOutput succeeds for its audience.
func (g *ToolGateway) ValidateFinalOutput(ctx context.Context, admission Admission, toolName string, candidate FinalOutputCandidate, refs OutputReferenceOwner, fields OutputFieldAuthorizer, claims OutputClaimOwner) (FinalOutput, error) {
	if !candidate.Complete {
		return FinalOutput{}, ErrIncompleteOutput
	}
	if len(candidate.Answer) == 0 {
		return FinalOutput{}, refusal(RefusalOutput, "answer", "a final answer requires at least one validated semantic part")
	}
	draft, err := g.ValidateDraftOutput(ctx, admission, toolName, candidate.Draft, refs, fields, claims)
	if err != nil {
		return FinalOutput{}, err
	}
	answer, err := g.BuildAnswer(candidate.Answer)
	if err != nil {
		return FinalOutput{}, err
	}
	if !supportedNarrative(draft.Narrative, answer) {
		return FinalOutput{}, refusal(RefusalOutput, "narrative", "unvalidated narrative cannot enter a final delivery")
	}
	receipt, err := finalOutputDigest(draft, answer)
	if err != nil {
		return FinalOutput{}, refusal(RefusalOutput, "output", "final output could not be bound to canonical material")
	}
	return FinalOutput{gateway: g, admissionReceipt: admission.admissionReceipt, draft: draft, answer: answer, receipt: receipt}, nil
}

// IssueFinalOutputPersistence rechecks the gateway seal and admission, then
// mints a detached persistence projection from the validated output. Payload,
// materials, citations, and digests are always derived from private validated
// fields; callers cannot provide any of them.
func (g *ToolGateway) IssueFinalOutputPersistence(ctx context.Context, admission Admission, output FinalOutput, identity FinalOutputIdentity) (FinalOutputPersistence, error) {
	if err := validateFinalOutputSeal(g, admission, output); err != nil {
		return FinalOutputPersistence{}, err
	}
	if identity.TenantID == "" || identity.TenantID != admission.Tenant || identity.OutputID == "" || identity.InvocationID == "" || identity.AdmissionID == "" || identity.RunID == "" || identity.InvokerID == "" || identity.ConversationID == "" || identity.ThreadID == "" || identity.PostID == "" || identity.PersonaID == "" || identity.PersonaVersion == "" || identity.InstallationID == "" {
		return FinalOutputPersistence{}, refusal(RefusalOutput, "identity", "complete identity binding in the admitted tenant is required")
	}
	payload, answer, err := output.Output()
	if err != nil {
		return FinalOutputPersistence{}, err
	}
	materials := cloneOutputMaterials(finalOutputMaterials(output))
	citations := citationsFromAnswer(answer)
	semanticDigest := output.receipt
	admissionDigest := output.admissionReceipt
	digest, err := persistenceDigest(identity, payload, answer, materials, citations, semanticDigest, admissionDigest)
	if err != nil {
		return FinalOutputPersistence{}, refusal(RefusalOutput, "persistence", "final output persistence projection could not be sealed")
	}
	return FinalOutputPersistence{
		identity: identity, payload: payload, answer: answer, materials: materials,
		citations: citations, semanticDigest: semanticDigest, admissionDigest: admissionDigest, digest: digest,
	}, nil
}

// Identity returns the detached invocation and delivery identity.
func (p FinalOutputPersistence) Identity() FinalOutputIdentity { return p.identity }

// Payload returns detached validated payload and semantic answer data.
func (p FinalOutputPersistence) Payload() (DraftOutput, Answer, error) {
	if p.digest == "" {
		return DraftOutput{}, Answer{}, refusal(RefusalInvalid, "persistence", "unsealed final output persistence projection")
	}
	payload := cloneDraftOutput(p.payload)
	answer := cloneAnswer(p.answer)
	if digest, err := persistenceDigest(p.identity, payload, answer, p.materials, p.citations, p.semanticDigest, p.admissionDigest); err != nil || digest != p.digest {
		return DraftOutput{}, Answer{}, refusal(RefusalOutput, "persistence", "final output persistence projection changed after issuance")
	}
	return payload, answer, nil
}

// Materials returns a detached copy of the validated output materials.
func (p FinalOutputPersistence) Materials() []OutputMaterial {
	return cloneOutputMaterials(p.materials)
}

// Citations returns a detached copy of the validated answer citations.
func (p FinalOutputPersistence) Citations() []Citation {
	return append([]Citation(nil), p.citations...)
}

// SemanticDigest returns the gateway-issued digest of the validated output.
func (p FinalOutputPersistence) SemanticDigest() string { return p.semanticDigest }

// AdmissionDigest returns the gateway-issued admission receipt bound to the
// validated output. It is never accepted as caller-supplied persistence data.
func (p FinalOutputPersistence) AdmissionDigest() string { return p.admissionDigest }

// Digest returns the projection digest bound to identity and all persisted data.
func (p FinalOutputPersistence) Digest() string { return p.digest }

// ReauthorizeFinalOutput checks every validated output material for every
// supplied current recipient. The delivery owner must pair this with its atomic
// audience revision fence when it commits the post or workflow result.
func (g *ToolGateway) ReauthorizeFinalOutput(ctx context.Context, admission Admission, output FinalOutput, recipients []FinalOutputRecipient, authorizer OutputAudienceAuthorizer) error {
	if err := validateFinalOutputSeal(g, admission, output); err != nil {
		return err
	}
	if authorizer == nil || len(recipients) == 0 {
		return refusal(RefusalOutput, "audience", "current recipients and an authorization owner are required")
	}
	ordered := append([]FinalOutputRecipient(nil), recipients...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].TenantID == ordered[j].TenantID {
			return ordered[i].SubjectID < ordered[j].SubjectID
		}
		return ordered[i].TenantID < ordered[j].TenantID
	})
	for i, recipient := range ordered {
		if recipient.TenantID != admission.Tenant || recipient.SubjectID == "" || (i > 0 && recipient == ordered[i-1]) {
			return refusal(RefusalOutput, "audience", "recipient set is invalid or crosses tenant scope")
		}
	}
	materials := finalOutputMaterials(output)
	if len(materials) == 0 {
		return refusal(RefusalOutput, "materials", "final output has no reauthorizable material")
	}
	for _, recipient := range ordered {
		for _, material := range materials {
			if err := g.ReauthorizeFinalOutputMaterial(ctx, admission, output, recipient, material, authorizer); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReauthorizeFinalOutputMaterial is the adapter seam for delivery owners that
// recheck one material at a time. It refuses materials that are not part of the
// sealed validated output, so a delivery adapter cannot authorize added content
// merely because the recipient can read the requested identifier.
func (g *ToolGateway) ReauthorizeFinalOutputMaterial(ctx context.Context, admission Admission, output FinalOutput, recipient FinalOutputRecipient, material OutputMaterial, authorizer OutputAudienceAuthorizer) error {
	if err := validateFinalOutputSeal(g, admission, output); err != nil {
		return err
	}
	if authorizer == nil || recipient.TenantID != admission.Tenant || recipient.SubjectID == "" {
		return refusal(RefusalOutput, "audience", "recipient and current authorization owner are required")
	}
	if !containsOutputMaterial(finalOutputMaterials(output), material) {
		return refusal(RefusalOutput, "materials", "requested material is not part of the validated output")
	}
	if err := authorizer.AuthorizeOutput(ctx, admission.Tenant, admission.Purpose, recipient, material); err != nil {
		return refusal(RefusalOutput, "audience", "a current recipient cannot read this output material")
	}
	return nil
}

// Output returns detached validated data for a delivery adapter. Callers must
// retain the opaque FinalOutput and pass that value to the reauthorization gate
// immediately before the destination's revision-fenced commit.
func (o FinalOutput) Output() (DraftOutput, Answer, error) {
	if o.gateway == nil || o.receipt == "" {
		return DraftOutput{}, Answer{}, refusal(RefusalInvalid, "output", "unsealed final output")
	}
	if receipt, err := finalOutputDigest(o.draft, o.answer); err != nil || receipt != o.receipt {
		return DraftOutput{}, Answer{}, refusal(RefusalOutput, "output", "final output changed after validation")
	}
	draft := o.draft
	draft.References = cloneStrings(o.draft.References)
	draft.Fields = cloneStrings(o.draft.Fields)
	draft.Claims = cloneStrings(o.draft.Claims)
	if shape, ok := o.draft.Result.Value.(DraftValue); ok {
		detached, err := shape.DetachDraft()
		if err != nil || nilValue(detached) {
			return DraftOutput{}, Answer{}, refusal(RefusalOutput, "output", "final draft could not be detached")
		}
		draft.Result.Value = detached
	}
	return draft, cloneAnswer(o.answer), nil
}

func finalOutputMaterials(output FinalOutput) []OutputMaterial {
	byKey := make(map[string]OutputMaterial)
	add := func(kind OutputMaterialKind, id string) {
		if id != "" {
			material := OutputMaterial{Kind: kind, ID: id}
			byKey[string(material.Kind)+"\x00"+material.ID+"\x00"+material.Value] = material
		}
	}
	for _, id := range output.draft.References {
		add(OutputRecord, id)
	}
	for _, id := range output.draft.Fields {
		add(OutputField, id)
	}
	for _, id := range output.draft.Claims {
		add(OutputClaim, id)
	}
	for _, part := range output.answer.Parts {
		for _, citation := range part.Citations {
			if citation.SourceID != "" {
				material := OutputMaterial{Kind: OutputSource, ID: citation.SourceID, Value: part.Text}
				key := string(material.Kind) + "\x00" + material.ID + "\x00" + material.Value
				byKey[key] = material
			}
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]OutputMaterial, 0, len(keys))
	for _, key := range keys {
		result = append(result, byKey[key])
	}
	return result
}

func validateFinalOutputSeal(g *ToolGateway, admission Admission, output FinalOutput) error {
	if g == nil || output.gateway != g || admission.admissionSeal != g || output.admissionReceipt == "" || output.admissionReceipt != admission.admissionReceipt || admission.admissionReceipt != digestAdmission(admission) {
		return refusal(RefusalInvalid, "output", "final output is not bound to this current admission")
	}
	receipt, err := finalOutputDigest(output.draft, output.answer)
	if err != nil || receipt != output.receipt {
		return refusal(RefusalOutput, "output", "final output changed after validation")
	}
	return nil
}

func containsOutputMaterial(materials []OutputMaterial, target OutputMaterial) bool {
	for _, material := range materials {
		if material == target {
			return true
		}
	}
	return false
}

func supportedNarrative(narrative string, answer Answer) bool {
	if narrative == "" {
		return true
	}
	for _, part := range answer.Parts {
		if part.Text == narrative {
			return true
		}
	}
	return false
}

func finalOutputDigest(draft DraftOutput, answer Answer) (string, error) {
	draftDigest, err := digestValidatedResult(draft.Result)
	if err != nil || draftDigest != draft.Result.semanticReceipt {
		return "", errors.New("draft receipt invalid")
	}
	encoded, err := json.Marshal(struct {
		DraftReceipt string   `json:"draft_receipt"`
		References   []string `json:"references"`
		Fields       []string `json:"fields"`
		Claims       []string `json:"claims"`
		Narrative    string   `json:"narrative"`
		Answer       Answer   `json:"answer"`
	}{draftDigest, draft.References, draft.Fields, draft.Claims, draft.Narrative, answer})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("hcm-next-agent-final-output/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func cloneDraftOutput(draft DraftOutput) DraftOutput {
	copy := draft
	copy.References = cloneStrings(draft.References)
	copy.Fields = cloneStrings(draft.Fields)
	copy.Claims = cloneStrings(draft.Claims)
	if shape, ok := draft.Result.Value.(DraftValue); ok && !nilValue(shape) {
		if detached, err := shape.DetachDraft(); err == nil && !nilValue(detached) {
			copy.Result.Value = detached
		}
	}
	return copy
}

func cloneOutputMaterials(materials []OutputMaterial) []OutputMaterial {
	return append([]OutputMaterial(nil), materials...)
}

func citationsFromAnswer(answer Answer) []Citation {
	var citations []Citation
	for _, part := range answer.Parts {
		citations = append(citations, part.Citations...)
	}
	return citations
}

func persistenceDigest(identity FinalOutputIdentity, payload DraftOutput, answer Answer, materials []OutputMaterial, citations []Citation, semanticDigest, admissionDigest string) (string, error) {
	encoded, err := json.Marshal(struct {
		Identity        FinalOutputIdentity `json:"identity"`
		Payload         DraftOutput         `json:"payload"`
		Answer          Answer              `json:"answer"`
		Materials       []OutputMaterial    `json:"materials"`
		Citations       []Citation          `json:"citations"`
		SemanticDigest  string              `json:"semantic_digest"`
		AdmissionDigest string              `json:"admission_digest"`
	}{identity, payload, answer, materials, citations, semanticDigest, admissionDigest})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("hcm-next-agent-final-output-persistence/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func cloneAnswer(answer Answer) Answer {
	out := Answer{Parts: make([]AnswerPart, len(answer.Parts))}
	for i, part := range answer.Parts {
		out.Parts[i] = part
		out.Parts[i].Taint = append([]TaintLabel(nil), part.Taint...)
		out.Parts[i].Citations = append([]Citation(nil), part.Citations...)
	}
	return out
}
