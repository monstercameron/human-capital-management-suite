package agentportable

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

const (
	// CurrentFormatVersion identifies the version-one portable format.
	CurrentFormatVersion uint32 = 1
	maxPortableBytes            = 1 << 20
)

var (
	// ErrInvalidDefinition identifies malformed, noncanonical, or unsupported input.
	ErrInvalidDefinition = errors.New("agentportable: invalid definition")
	// ErrMappingRequired identifies an import without a destination resolver.
	ErrMappingRequired = errors.New("agentportable: reference mapping is required")
)

// ReferenceKind identifies the destination registry used to resolve a reference.
type ReferenceKind string

const (
	// SourceReference names tenant-owned knowledge sources.
	SourceReference ReferenceKind = "source"
	// CapabilityReference names an available tool or capability.
	CapabilityReference ReferenceKind = "capability"
	// ModelPolicyReference names a destination model policy.
	ModelPolicyReference ReferenceKind = "model_policy"
	// OutputSchemaReference names a destination output schema.
	OutputSchemaReference ReferenceKind = "output_schema"
	// EvaluationReference names a destination evaluation suite.
	EvaluationReference ReferenceKind = "evaluation_suite"
)

// Definition is the allowlisted, authority-free transfer representation. It
// cannot represent tenant IDs, identities, credentials, grants, connections,
// triggers, schedules, memory, installations, or run history.
type Definition struct {
	FormatVersion      uint32                    `json:"format_version"`
	ManifestSchema     uint32                    `json:"manifest_schema_version"`
	Purpose            string                    `json:"purpose"`
	InstructionsDigest string                    `json:"instructions_digest"`
	SourceCeiling      []agentmanifest.Reference `json:"source_ceiling"`
	ToolCeiling        []agentmanifest.Reference `json:"tool_ceiling"`
	ModelPolicy        agentmanifest.Reference   `json:"model_policy"`
	AutonomyCeiling    string                    `json:"autonomy_ceiling"`
	Budget             agentmanifest.Budget      `json:"budget"`
	OutputSchema       agentmanifest.Reference   `json:"output_schema"`
	EvaluationRefs     []agentmanifest.Reference `json:"evaluation_refs"`
	Instructions       string                    `json:"instructions,omitempty"`
}

// ReferenceMapper resolves each portable reference to an explicitly approved
// destination reference. Returning an error fails the entire import.
type ReferenceMapper func(kind ReferenceKind, source agentmanifest.Reference) (agentmanifest.Reference, error)

// ImportRequest provides fresh destination identity for an inert draft.
type ImportRequest struct {
	TenantID string
	AgentID  string
	OwnerID  string
}

// Draft is an imported, reviewable definition. DRAFT has no execution or
// installation semantics; a separate publication flow must approve it.
type Draft struct {
	TenantID     string                 `json:"tenant_id"`
	State        string                 `json:"state"`
	Manifest     agentmanifest.Manifest `json:"manifest"`
	Instructions string                 `json:"instructions,omitempty"`
}

// Export strips identity and all authority-bearing fields from a manifest.
func Export(manifest agentmanifest.Manifest) ([]byte, error) {
	return ExportWithInstructions(manifest, "")
}

// ExportWithInstructions carries the exact instruction bytes and verifies
// them against the manifest digest.
func ExportWithInstructions(manifest agentmanifest.Manifest, instructions string) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("%w: source manifest: %v", ErrInvalidDefinition, err)
	}
	definition := Definition{
		FormatVersion: CurrentFormatVersion, ManifestSchema: manifest.SchemaVersion,
		Purpose: manifest.Purpose, InstructionsDigest: manifest.InstructionsDigest,
		SourceCeiling: copyReferences(manifest.SourceCeiling), ToolCeiling: copyReferences(manifest.ToolCeiling),
		ModelPolicy: manifest.ModelPolicy, AutonomyCeiling: manifest.AutonomyCeiling,
		Budget: manifest.Budget, OutputSchema: manifest.OutputSchema,
		EvaluationRefs: copyReferences(manifest.EvaluationRefs),
		Instructions:   instructions,
	}
	if instructions != "" && !instructionDigestMatches(instructions, manifest.InstructionsDigest) {
		return nil, fmt.Errorf("%w: instructions digest mismatch", ErrInvalidDefinition)
	}
	return definition.CanonicalJSON()
}

// Parse validates the closed schema and requires byte-for-byte canonical JSON.
func Parse(data []byte) (Definition, error) {
	var definition Definition
	if len(data) == 0 || len(data) > maxPortableBytes || !utf8.Valid(data) {
		return definition, fmt.Errorf("%w: input size or encoding", ErrInvalidDefinition)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return definition, fmt.Errorf("%w: %v", ErrInvalidDefinition, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return definition, fmt.Errorf("%w: decode: %v", ErrInvalidDefinition, err)
	}
	if decoder.More() {
		return definition, fmt.Errorf("%w: trailing JSON", ErrInvalidDefinition)
	}
	canonical, err := definition.CanonicalJSON()
	if err != nil {
		return definition, err
	}
	if !bytes.Equal(bytes.TrimSpace(data), canonical) {
		return definition, fmt.Errorf("%w: noncanonical JSON", ErrInvalidDefinition)
	}
	return definition, nil
}

// CanonicalJSON returns the only accepted JSON encoding of a valid definition.
func (d Definition) CanonicalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("%w: encode: %v", ErrInvalidDefinition, err)
	}
	return data, nil
}

// Digest returns the SHA-256 digest of the canonical portable bytes.
func (d Definition) Digest() (string, error) {
	data, err := d.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Validate enforces the exact format and delegates reference validation to
// agentmanifest without permitting nil required collections.
func (d Definition) Validate() error {
	if d.FormatVersion != CurrentFormatVersion || d.ManifestSchema == 0 {
		return fmt.Errorf("%w: unsupported format or manifest schema", ErrInvalidDefinition)
	}
	if strings.TrimSpace(d.Purpose) == "" || strings.TrimSpace(d.Purpose) != d.Purpose || !utf8.ValidString(d.Purpose) {
		return fmt.Errorf("%w: purpose is required", ErrInvalidDefinition)
	}
	if !validDigest(d.InstructionsDigest) {
		return fmt.Errorf("%w: instructions_digest is invalid", ErrInvalidDefinition)
	}
	if d.Instructions != "" && !instructionDigestMatches(d.Instructions, d.InstructionsDigest) {
		return fmt.Errorf("%w: instruction content digest mismatch", ErrInvalidDefinition)
	}
	if d.SourceCeiling == nil || d.ToolCeiling == nil || d.EvaluationRefs == nil || len(d.EvaluationRefs) == 0 {
		return fmt.Errorf("%w: required reference arrays must be explicit and evaluations non-empty", ErrInvalidDefinition)
	}
	if err := validateRefs(d.SourceCeiling, true); err != nil {
		return fmt.Errorf("%w: source_ceiling: %v", ErrInvalidDefinition, err)
	}
	if err := validateRefs(d.ToolCeiling, true); err != nil {
		return fmt.Errorf("%w: tool_ceiling: %v", ErrInvalidDefinition, err)
	}
	if err := validateRefs(d.EvaluationRefs, true); err != nil {
		return fmt.Errorf("%w: evaluation_refs: %v", ErrInvalidDefinition, err)
	}
	if err := validateRef(d.ModelPolicy); err != nil {
		return fmt.Errorf("%w: model_policy: %v", ErrInvalidDefinition, err)
	}
	if err := validateRef(d.OutputSchema); err != nil {
		return fmt.Errorf("%w: output_schema: %v", ErrInvalidDefinition, err)
	}
	if strings.TrimSpace(d.AutonomyCeiling) == "" {
		return fmt.Errorf("%w: autonomy ceiling is required", ErrInvalidDefinition)
	}
	if d.Budget.MaxCostMicros == 0 || d.Budget.MaxInputTokens == 0 || d.Budget.MaxOutputTokens == 0 || d.Budget.MaxConcurrentRuns == 0 {
		return fmt.Errorf("%w: budget bounds must be positive", ErrInvalidDefinition)
	}
	return nil
}

// Import validates, maps, and returns an inert DRAFT with empty context grants.
func Import(data []byte, request ImportRequest, mapper ReferenceMapper) (Draft, error) {
	d, err := Parse(data)
	if err != nil {
		return Draft{}, err
	}
	if !validIdentity(request.TenantID) || !validIdentity(request.AgentID) || !validIdentity(request.OwnerID) {
		return Draft{}, fmt.Errorf("%w: destination identity is required", ErrInvalidDefinition)
	}
	if mapper == nil {
		return Draft{}, ErrMappingRequired
	}
	mapRefs := func(kind ReferenceKind, refs []agentmanifest.Reference) ([]agentmanifest.Reference, error) {
		out := make([]agentmanifest.Reference, 0, len(refs))
		for _, ref := range refs {
			mapped, e := mapper(kind, ref)
			if e != nil {
				return nil, fmt.Errorf("%w: %s %q: %v", ErrInvalidDefinition, kind, ref.ID, e)
			}
			if e = validateRef(mapped); e != nil {
				return nil, fmt.Errorf("%w: mapped %s %q: %v", ErrInvalidDefinition, kind, ref.ID, e)
			}
			out = append(out, mapped)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	sources, err := mapRefs(SourceReference, d.SourceCeiling)
	if err != nil {
		return Draft{}, err
	}
	tools, err := mapRefs(CapabilityReference, d.ToolCeiling)
	if err != nil {
		return Draft{}, err
	}
	evals, err := mapRefs(EvaluationReference, d.EvaluationRefs)
	if err != nil {
		return Draft{}, err
	}
	model, err := mapper(ModelPolicyReference, d.ModelPolicy)
	if err != nil {
		return Draft{}, fmt.Errorf("%w: model policy: %v", ErrInvalidDefinition, err)
	}
	if err = validateRef(model); err != nil {
		return Draft{}, fmt.Errorf("%w: mapped model policy: %v", ErrInvalidDefinition, err)
	}
	output, err := mapper(OutputSchemaReference, d.OutputSchema)
	if err != nil {
		return Draft{}, fmt.Errorf("%w: output schema: %v", ErrInvalidDefinition, err)
	}
	if err = validateRef(output); err != nil {
		return Draft{}, fmt.Errorf("%w: mapped output schema: %v", ErrInvalidDefinition, err)
	}
	manifest := agentmanifest.Manifest{SchemaVersion: d.ManifestSchema, ID: request.AgentID, Version: 1, OwnerID: request.OwnerID, Purpose: d.Purpose, InstructionsDigest: d.InstructionsDigest, SourceCeiling: sources, ToolCeiling: tools, ModelPolicy: model, AutonomyCeiling: d.AutonomyCeiling, Budget: d.Budget, OutputSchema: output, ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: evals}
	if err := manifest.Validate(); err != nil {
		return Draft{}, fmt.Errorf("%w: mapped manifest: %v", ErrInvalidDefinition, err)
	}
	return Draft{TenantID: request.TenantID, State: "DRAFT", Manifest: manifest, Instructions: d.Instructions}, nil
}

func instructionDigestMatches(content, digest string) bool {
	if content == "" || len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	sum := sha256.Sum256([]byte(content))
	return "sha256:"+hex.EncodeToString(sum[:]) == digest
}

func validateRefs(refs []agentmanifest.Reference, required bool) error {
	if refs == nil && required {
		return errors.New("required")
	}
	seen := map[string]bool{}
	prev := ""
	for i, ref := range refs {
		if err := validateRef(ref); err != nil {
			return fmt.Errorf("[%d]: %v", i, err)
		}
		if seen[ref.ID] || (i > 0 && ref.ID <= prev) {
			return errors.New("references must be unique and sorted")
		}
		seen[ref.ID] = true
		prev = ref.ID
	}
	return nil
}
func validateRef(ref agentmanifest.Reference) error {
	if strings.TrimSpace(ref.ID) == "" || ref.Version == 0 || ref.SchemaVersion == 0 || !validDigest(ref.Digest) {
		return errors.New("invalid reference")
	}
	return nil
}
func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil && value == strings.ToLower(value)
}
func validIdentity(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) == value && strings.TrimSpace(value) != "" && len(value) <= 256
}
func copyReferences(refs []agentmanifest.Reference) []agentmanifest.Reference {
	return append([]agentmanifest.Reference{}, refs...)
}
func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := consumeJSONValue(dec); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return errors.New("trailing JSON")
	}
	return nil
}

func consumeJSONValue(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate JSON field %q", name)
			}
			seen[name] = true
			if err := consumeJSONValue(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	}
	if delim == '[' {
		for dec.More() {
			if err := consumeJSONValue(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	}
	return errors.New("unexpected JSON delimiter")
}
