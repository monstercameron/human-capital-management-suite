package agentsecurity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// QuarantineModel is the narrow seam for internal/agentmodel. Its
// implementation is expected to call SchemaFlux and return a typed value;
// this package never parses model prose or imports a provider SDK.
type QuarantineModel interface {
	Extract(context.Context, QuarantineRequest) (QuarantineModelOutput, error)
}

// QuarantineRequest deliberately contains no skills, tools, plan instructions,
// or delegated credentials. The raw content is visible only during this
// extraction call and is not copied into QuarantineExtraction.
type QuarantineRequest struct {
	Source   SourceKind
	SourceID string
	Content  string
	Schema   ExtractionSchema
	Skills   []string
}

type ExtractionField struct {
	Name     string
	Type     string
	Required bool
}

type ExtractionSchema struct {
	ID      string
	Version string
	Fields  []ExtractionField
}

// QuarantineModelOutput is the only model response admitted by the boundary.
// Values are JSON-typed fragments, never a free-form model message.
type QuarantineModelOutput struct {
	SchemaID      string
	SchemaVersion string
	Values        []ExtractedValue
	Skills        []string
}

type ExtractedValue struct {
	Name       string
	Value      json.RawMessage
	Taint      []TaintLabel
	Provenance []string
	Citations  []Citation
}

// QuarantineExtraction is safe to pass to planning. It contains the declared
// typed values and their evidence, but never the raw source content.
type QuarantineExtraction struct {
	Source        SourceKind
	SourceID      string
	SourceDigest  string
	SchemaID      string
	SchemaVersion string
	Values        []ExtractedValue
}

type QuarantinedExtractor struct {
	model QuarantineModel
}

func NewQuarantinedExtractor(model QuarantineModel) (*QuarantinedExtractor, error) {
	if model == nil {
		return nil, refusal(RefusalInvalid, "model", "quarantine extraction requires a model gateway")
	}
	return &QuarantinedExtractor{model: model}, nil
}

// Extract invokes the model with an empty skill set and validates every
// returned field, citation, and schema binding before exposing it to a plan.
func (e *QuarantinedExtractor) Extract(ctx context.Context, req QuarantineRequest) (QuarantineExtraction, error) {
	if e == nil || e.model == nil {
		return QuarantineExtraction{}, refusal(RefusalInvalid, "model", "quarantine extractor is not configured")
	}
	if ctx == nil {
		return QuarantineExtraction{}, refusal(RefusalInvalid, "context", "quarantine extraction requires a context")
	}
	if err := ctx.Err(); err != nil {
		return QuarantineExtraction{}, err
	}
	req.Schema.Fields = append([]ExtractionField(nil), req.Schema.Fields...)
	if err := validateQuarantineRequest(req); err != nil {
		return QuarantineExtraction{}, err
	}
	req.Skills = nil
	modelRequest := req
	modelRequest.Schema.Fields = append([]ExtractionField(nil), req.Schema.Fields...)
	output, err := e.model.Extract(ctx, modelRequest)
	if err != nil {
		return QuarantineExtraction{}, fmt.Errorf("quarantine extraction: %w", err)
	}
	if len(output.Skills) != 0 {
		return QuarantineExtraction{}, refusal(RefusalCapability, "skills", "quarantine extraction cannot invoke skills")
	}
	if output.SchemaID != req.Schema.ID || output.SchemaVersion != req.Schema.Version {
		return QuarantineExtraction{}, refusal(RefusalOutput, "schema", "extraction schema does not match the declared schema")
	}
	values, err := validateExtractedValues(req, output.Values)
	if err != nil {
		return QuarantineExtraction{}, err
	}
	return QuarantineExtraction{
		Source: req.Source, SourceID: req.SourceID, SourceDigest: digestContent(req.Content),
		SchemaID: req.Schema.ID, SchemaVersion: req.Schema.Version, Values: values,
	}, nil
}

// ExtractQuarantined is a convenience for callers that do not need to retain
// an extractor. It has the same no-skills and typed-output guarantees.
func ExtractQuarantined(ctx context.Context, model QuarantineModel, req QuarantineRequest) (QuarantineExtraction, error) {
	extractor, err := NewQuarantinedExtractor(model)
	if err != nil {
		return QuarantineExtraction{}, err
	}
	return extractor.Extract(ctx, req)
}

func validateQuarantineRequest(req QuarantineRequest) error {
	if !req.Source.valid() || strings.TrimSpace(req.SourceID) == "" || strings.TrimSpace(req.Content) == "" {
		return refusal(RefusalInvalid, "source", "source, source id and content are required")
	}
	if len(req.Skills) != 0 {
		return refusal(RefusalCapability, "skills", "quarantine extraction cannot invoke skills")
	}
	if err := req.Schema.validate(); err != nil {
		return err
	}
	return nil
}

func (s ExtractionSchema) validate() error {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Version) == "" || len(s.Fields) == 0 {
		return refusal(RefusalOutput, "schema", "schema id, version and fields are required")
	}
	seen := make(map[string]struct{}, len(s.Fields))
	for _, field := range s.Fields {
		if strings.TrimSpace(field.Name) == "" || !validExtractionType(field.Type) {
			return refusal(RefusalOutput, "schema", "schema field name and supported type are required")
		}
		if _, exists := seen[field.Name]; exists {
			return refusal(RefusalOutput, "schema", "schema fields must be unique")
		}
		seen[field.Name] = struct{}{}
	}
	return nil
}

func validateExtractedValues(req QuarantineRequest, values []ExtractedValue) ([]ExtractedValue, error) {
	fields := make(map[string]ExtractionField, len(req.Schema.Fields))
	for _, field := range req.Schema.Fields {
		fields[field.Name] = field
	}
	seen := make(map[string]struct{}, len(values))
	validated := make([]ExtractedValue, 0, len(values))
	for _, value := range values {
		field, ok := fields[value.Name]
		if !ok {
			return nil, refusal(RefusalOutput, "values", "extraction contains an undeclared field")
		}
		if _, exists := seen[value.Name]; exists {
			return nil, refusal(RefusalOutput, "values", "extraction fields must be unique")
		}
		seen[value.Name] = struct{}{}
		if !validJSONType(value.Value, field.Type) {
			return nil, refusal(RefusalOutput, "values."+value.Name, "extracted value does not match its schema type")
		}
		if len(value.Citations) == 0 {
			return nil, refusal(RefusalOutput, "values."+value.Name, "extracted value requires a citation")
		}
		for _, citation := range value.Citations {
			if citation.SourceID != req.SourceID || strings.TrimSpace(citation.Location) == "" || citation.Digest != digestContent(req.Content) {
				return nil, refusal(RefusalOutput, "values."+value.Name, "citation is not bound to the quarantined source")
			}
		}
		value.Value = append(json.RawMessage(nil), value.Value...)
		value.Taint = joinTaint([]TaintLabel{TaintExternal}, value.Taint...)
		value.Provenance = joinStrings([]string{req.SourceID}, value.Provenance...)
		value.Citations = append([]Citation(nil), value.Citations...)
		validated = append(validated, value)
	}
	for _, field := range req.Schema.Fields {
		if field.Required {
			if _, ok := seen[field.Name]; !ok {
				return nil, refusal(RefusalOutput, "values."+field.Name, "required extraction field is missing")
			}
		}
	}
	sort.Slice(validated, func(i, j int) bool { return validated[i].Name < validated[j].Name })
	return validated, nil
}

func validExtractionType(kind string) bool {
	switch kind {
	case "string", "integer", "number", "boolean", "object", "array":
		return true
	default:
		return false
	}
}

func validJSONType(raw json.RawMessage, kind string) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		return ok && !strings.ContainsAny(number.String(), ".eE")
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	default:
		return false
	}
}

// PlanningContext is the only context accepted by a planner. Raw source
// content and model narratives are intentionally absent from this type.
type PlanningContext struct {
	UserGoal            string
	ConfirmedPlanDigest string
	Extractions         []QuarantineExtraction
}

func NewPlanningContext(userGoal, confirmedPlanDigest string, extractions ...QuarantineExtraction) (PlanningContext, error) {
	if strings.TrimSpace(userGoal) == "" || strings.TrimSpace(confirmedPlanDigest) == "" {
		return PlanningContext{}, refusal(RefusalInvalid, "plan", "user goal and confirmed plan digest are required")
	}
	copyExtractions := make([]QuarantineExtraction, len(extractions))
	for i, extraction := range extractions {
		if err := validatePlanningExtraction(extraction); err != nil {
			return PlanningContext{}, err
		}
		copyExtractions[i] = extraction
		copyExtractions[i].Values = cloneExtractedValues(extraction.Values)
	}
	return PlanningContext{UserGoal: userGoal, ConfirmedPlanDigest: confirmedPlanDigest, Extractions: copyExtractions}, nil
}

func validatePlanningExtraction(extraction QuarantineExtraction) error {
	if !extraction.Source.valid() || strings.TrimSpace(extraction.SourceID) == "" || !validContentDigest(extraction.SourceDigest) || strings.TrimSpace(extraction.SchemaID) == "" || strings.TrimSpace(extraction.SchemaVersion) == "" {
		return refusal(RefusalOutput, "extractions", "planning context contains an invalid extraction")
	}
	for _, value := range extraction.Values {
		if strings.TrimSpace(value.Name) == "" || !containsTaintLabel(value.Taint, TaintExternal) || len(value.Provenance) == 0 || !containsString(value.Provenance, extraction.SourceID) || len(value.Citations) == 0 {
			return refusal(RefusalOutput, "extractions."+value.Name, "quarantined values must retain external taint and source provenance")
		}
		if !validTaintLabels(value.Taint) {
			return refusal(RefusalOutput, "extractions."+value.Name, "extraction contains an unknown taint label")
		}
		for _, citation := range value.Citations {
			if !validBoundCitation(citation, extraction.SourceID, extraction.SourceDigest) {
				return refusal(RefusalOutput, "extractions."+value.Name, "extraction citation is not bound to its source")
			}
		}
	}
	return nil
}

type WriteArgumentRole string

const (
	WriteSubject     WriteArgumentRole = "subject"
	WriteRecipient   WriteArgumentRole = "recipient"
	WriteAmount      WriteArgumentRole = "amount"
	WriteDestination WriteArgumentRole = "destination"
	WriteOther       WriteArgumentRole = "other"
)

type WriteArgument struct {
	Name       string
	Value      string
	Role       WriteArgumentRole
	Taint      []TaintLabel
	Provenance []string
	Citations  []Citation
}

type WriteApprovalEvidence struct {
	Name      string
	Value     string
	Role      WriteArgumentRole
	Taint     []TaintLabel
	Citations []Citation
}

type WriteApprovalCard struct {
	Digest    string
	Arguments []WriteApprovalEvidence
}

// WriteOwner is the AGENT-003 owner validator seam. It must resolve current
// authorization for the exact subject, recipient, or amount, not a name-only
// lookup cached when the task started.
type WriteOwner interface {
	AuthorizeWriteArgument(context.Context, WriteArgumentRole, string) (bool, error)
}

// BuildWriteApprovalCard creates the exact evidence card a user must approve.
// It is deterministic and includes the source citations and taint on every
// argument, so an approval cannot silently cover a changed value.
func BuildWriteApprovalCard(args []WriteArgument) (WriteApprovalCard, error) {
	if len(args) == 0 {
		return WriteApprovalCard{}, refusal(RefusalInvalid, "arguments", "at least one write argument is required")
	}
	evidence, err := normalizeApprovalEvidence(args)
	if err != nil {
		return WriteApprovalCard{}, err
	}
	return WriteApprovalCard{Digest: writeEvidenceDigest(evidence), Arguments: evidence}, nil
}

// BindWriteArguments permits a T2-T4 operation to receive only arguments that
// remain bound to deterministic owner checks and the user's exact approval.
// It never returns an egress destination derived from tainted content.
func BindWriteArguments(ctx context.Context, tier SideEffectTier, args []WriteArgument, approval *WriteApprovalCard, owner WriteOwner) ([]WriteArgument, error) {
	if tier != TierCommunicate && tier != TierSubmitGoverned && tier != TierExternalWrite {
		return nil, refusal(RefusalEffectClass, "tier", "write arguments require a T2, T3 or T4 effect tier")
	}
	if len(args) == 0 {
		return nil, refusal(RefusalInvalid, "arguments", "at least one write argument is required")
	}
	for _, arg := range args {
		if strings.TrimSpace(arg.Name) == "" || strings.TrimSpace(arg.Value) == "" || !validWriteRole(arg.Role) {
			return nil, refusal(RefusalOutput, "arguments", "write arguments require a name, value and role")
		}
		if len(arg.Taint) == 0 || !validTaintLabels(arg.Taint) {
			return nil, refusal(RefusalOutput, "arguments."+arg.Name, "write arguments require known taint labels")
		}
		if arg.Role == WriteDestination && taintedWriteValue(arg.Taint) {
			return nil, refusal(RefusalAuthorityExpansion, "arguments."+arg.Name, "egress destination cannot derive from tainted content")
		}
		if isProtectedWriteRole(arg.Role) {
			if owner == nil {
				return nil, refusal(RefusalOutput, "owner", "subject, recipient and amount require an owner validator")
			}
			allowed, err := owner.AuthorizeWriteArgument(ctx, arg.Role, arg.Value)
			if err != nil || !allowed {
				return nil, refusal(RefusalAuthorityExpansion, "arguments."+arg.Name, "write argument is outside current owner authorization")
			}
		}
	}
	needsApproval := tier == TierSubmitGoverned || tier == TierExternalWrite
	for _, arg := range args {
		if arg.Role == WriteDestination || taintedWriteValue(arg.Taint) {
			needsApproval = true
		}
	}
	if needsApproval {
		if approval == nil {
			return nil, refusal(RefusalEffectClass, "approval", "write arguments require an exact approval card")
		}
		want, err := BuildWriteApprovalCard(args)
		if err != nil {
			return nil, err
		}
		if approval.Digest != want.Digest || !sameApprovalEvidence(approval.Arguments, want.Arguments) {
			return nil, refusal(RefusalArgsDigest, "approval", "approval card does not match exact write arguments")
		}
	}
	return cloneWriteArguments(args), nil
}

type SideEffectTier string

const (
	TierRead           SideEffectTier = "T0"
	TierPrivateDraft   SideEffectTier = "T1"
	TierCommunicate    SideEffectTier = "T2"
	TierSubmitGoverned SideEffectTier = "T3"
	TierExternalWrite  SideEffectTier = "T4"
)

func validWriteRole(role WriteArgumentRole) bool {
	return role == WriteSubject || role == WriteRecipient || role == WriteAmount || role == WriteDestination || role == WriteOther
}

func isProtectedWriteRole(role WriteArgumentRole) bool {
	return role == WriteSubject || role == WriteRecipient || role == WriteAmount
}

func taintedWriteValue(labels []TaintLabel) bool {
	for _, label := range labels {
		if label != TaintCanonical && label != TaintHuman {
			return true
		}
	}
	return false
}

func normalizeApprovalEvidence(args []WriteArgument) ([]WriteApprovalEvidence, error) {
	evidence := make([]WriteApprovalEvidence, 0, len(args))
	seen := make(map[string]struct{}, len(args))
	for _, arg := range args {
		if strings.TrimSpace(arg.Name) == "" || strings.TrimSpace(arg.Value) == "" || !validWriteRole(arg.Role) {
			return nil, refusal(RefusalOutput, "arguments", "write arguments require a name, value and role")
		}
		if len(arg.Taint) == 0 || !validTaintLabels(arg.Taint) {
			return nil, refusal(RefusalOutput, "arguments."+arg.Name, "write arguments require known taint labels")
		}
		if _, exists := seen[arg.Name]; exists {
			return nil, refusal(RefusalOutput, "arguments", "write argument names must be unique")
		}
		seen[arg.Name] = struct{}{}
		if taintedWriteValue(arg.Taint) && len(arg.Citations) == 0 {
			return nil, refusal(RefusalOutput, "arguments."+arg.Name, "tainted write arguments require source citations")
		}
		for _, citation := range arg.Citations {
			if !validBoundCitation(citation, "", "") {
				return nil, refusal(RefusalOutput, "arguments."+arg.Name, "write argument citation is incomplete")
			}
		}
		evidence = append(evidence, WriteApprovalEvidence{Name: arg.Name, Value: arg.Value, Role: arg.Role, Taint: joinTaint(nil, arg.Taint...), Citations: append([]Citation(nil), arg.Citations...)})
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].Name < evidence[j].Name })
	return evidence, nil
}

func validBoundCitation(citation Citation, sourceID, digest string) bool {
	if strings.TrimSpace(citation.SourceID) == "" || strings.TrimSpace(citation.Location) == "" || strings.TrimSpace(citation.Digest) == "" {
		return false
	}
	return (sourceID == "" || citation.SourceID == sourceID) && (digest == "" || citation.Digest == digest)
}

func validContentDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validTaintLabels(labels []TaintLabel) bool {
	for _, label := range labels {
		switch label {
		case TaintCanonical, TaintHuman, TaintExternal, TaintDerived, TaintTool:
		default:
			return false
		}
	}
	return true
}

func containsTaintLabel(labels []TaintLabel, want TaintLabel) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func writeEvidenceDigest(evidence []WriteApprovalEvidence) string {
	encoded, _ := json.Marshal(evidence)
	sum := sha256.Sum256(append([]byte("hcm-next-agent-write-approval/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sameApprovalEvidence(left, right []WriteApprovalEvidence) bool {
	leftCopy := append([]WriteApprovalEvidence(nil), left...)
	rightCopy := append([]WriteApprovalEvidence(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool { return leftCopy[i].Name < leftCopy[j].Name })
	sort.Slice(rightCopy, func(i, j int) bool { return rightCopy[i].Name < rightCopy[j].Name })
	leftBytes, _ := json.Marshal(leftCopy)
	rightBytes, _ := json.Marshal(rightCopy)
	return string(leftBytes) == string(rightBytes)
}

func cloneExtractedValues(values []ExtractedValue) []ExtractedValue {
	result := make([]ExtractedValue, len(values))
	for i, value := range values {
		result[i] = value
		result[i].Value = append(json.RawMessage(nil), value.Value...)
		result[i].Taint = append([]TaintLabel(nil), value.Taint...)
		result[i].Provenance = append([]string(nil), value.Provenance...)
		result[i].Citations = append([]Citation(nil), value.Citations...)
	}
	return result
}

func cloneWriteArguments(args []WriteArgument) []WriteArgument {
	result := make([]WriteArgument, len(args))
	for i, arg := range args {
		result[i] = arg
		result[i].Taint = append([]TaintLabel(nil), arg.Taint...)
		result[i].Provenance = append([]string(nil), arg.Provenance...)
		result[i].Citations = append([]Citation(nil), arg.Citations...)
	}
	return result
}
