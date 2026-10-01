package portable

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

const (
	// CurrentFormatVersion is the first portable agent definition format.
	CurrentFormatVersion uint32 = 1
	maxPortableBytes            = 1 << 20
)

var (
	// ErrInvalidDefinition identifies an unsupported or malformed portable definition.
	ErrInvalidDefinition = errors.New("portable agent definition is invalid")
	// ErrMappingRequired identifies an import without a destination reference resolver.
	ErrMappingRequired = errors.New("portable reference mapping is required")
)

// ReferenceKind identifies the registry whose reference must be resolved at import.
type ReferenceKind string

const (
	// SourceReference names a source that must be selected in the destination tenant.
	SourceReference ReferenceKind = "source"
	// CapabilityReference names a capability that must be available at destination.
	CapabilityReference ReferenceKind = "capability"
	// ModelPolicyReference identifies a destination-approved model policy.
	ModelPolicyReference ReferenceKind = "model_policy"
	// OutputSchemaReference identifies a destination-supported output schema.
	OutputSchemaReference ReferenceKind = "output_schema"
	// EvaluationReference identifies a destination-supported evaluation suite.
	EvaluationReference ReferenceKind = "evaluation_suite"
)

// Definition is the versioned, authority-free transfer format. Its fields are
// an allowlist: tenant identity, owners, context grants, credentials,
// installations, memory, and run history are not representable.
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
}

// ReferenceMapper resolves a source reference to a destination-owned or
// destination-approved reference. Returning an error refuses the import.
type ReferenceMapper func(kind ReferenceKind, source agentmanifest.Reference) (agentmanifest.Reference, error)

// ImportRequest supplies identity that is local to the destination tenant.
type ImportRequest struct {
	TenantID string
	AgentID  string
	OwnerID  string
}

// Draft is a reviewable imported definition. DRAFT is descriptive state only;
// this package exposes no operation that validates, publishes, installs, or grants it.
type Draft struct {
	TenantID string                 `json:"tenant_id"`
	State    string                 `json:"state"`
	Manifest agentmanifest.Manifest `json:"manifest"`
}

// Export converts a valid tenant manifest to the versioned portable allowlist.
func Export(manifest agentmanifest.Manifest) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("%w: manifest: %v", ErrInvalidDefinition, err)
	}
	definition := Definition{
		FormatVersion: CurrentFormatVersion, ManifestSchema: manifest.SchemaVersion,
		Purpose: manifest.Purpose, InstructionsDigest: manifest.InstructionsDigest,
		SourceCeiling: copyReferences(manifest.SourceCeiling),
		ToolCeiling:   copyReferences(manifest.ToolCeiling), ModelPolicy: manifest.ModelPolicy,
		AutonomyCeiling: manifest.AutonomyCeiling, Budget: manifest.Budget,
		OutputSchema: manifest.OutputSchema, EvaluationRefs: copyReferences(manifest.EvaluationRefs),
	}
	data, err := definition.CanonicalJSON()
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Import validates a portable definition, resolves each reference in the
// destination, and creates a version-one DRAFT with empty context grants.
func Import(data []byte, request ImportRequest, mapReference ReferenceMapper) (Draft, error) {
	definition, err := Parse(data)
	if err != nil {
		return Draft{}, err
	}
	if !validIdentity(request.TenantID) || !validIdentity(request.AgentID) || !validIdentity(request.OwnerID) {
		return Draft{}, fmt.Errorf("%w: destination tenant, agent, and owner IDs are required", ErrInvalidDefinition)
	}
	if mapReference == nil {
		return Draft{}, ErrMappingRequired
	}
	manifest, err := mapDefinition(definition, request, mapReference)
	if err != nil {
		return Draft{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Draft{}, fmt.Errorf("%w: mapped manifest: %v", ErrInvalidDefinition, err)
	}
	return Draft{TenantID: request.TenantID, State: "DRAFT", Manifest: manifest}, nil
}

func mapDefinition(definition Definition, request ImportRequest, mapper ReferenceMapper) (agentmanifest.Manifest, error) {
	sources, err := mapReferences(SourceReference, definition.SourceCeiling, mapper)
	if err != nil {
		return agentmanifest.Manifest{}, err
	}
	tools, err := mapReferences(CapabilityReference, definition.ToolCeiling, mapper)
	if err != nil {
		return agentmanifest.Manifest{}, err
	}
	model, err := mapReference(ModelPolicyReference, definition.ModelPolicy, mapper)
	if err != nil {
		return agentmanifest.Manifest{}, err
	}
	output, err := mapReference(OutputSchemaReference, definition.OutputSchema, mapper)
	if err != nil {
		return agentmanifest.Manifest{}, err
	}
	evaluations, err := mapReferences(EvaluationReference, definition.EvaluationRefs, mapper)
	if err != nil {
		return agentmanifest.Manifest{}, err
	}
	return agentmanifest.Manifest{
		SchemaVersion: definition.ManifestSchema, ID: request.AgentID, Version: 1,
		OwnerID: request.OwnerID, Purpose: definition.Purpose,
		InstructionsDigest: definition.InstructionsDigest, SourceCeiling: sources,
		ToolCeiling: tools, ModelPolicy: model, AutonomyCeiling: definition.AutonomyCeiling,
		Budget: definition.Budget, OutputSchema: output,
		ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: evaluations,
	}, nil
}

func mapReferences(kind ReferenceKind, refs []agentmanifest.Reference, mapper ReferenceMapper) ([]agentmanifest.Reference, error) {
	mapped := make([]agentmanifest.Reference, 0, len(refs))
	for _, ref := range refs {
		value, err := mapReference(kind, ref, mapper)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, value)
	}
	sort.Slice(mapped, func(i, j int) bool { return mapped[i].ID < mapped[j].ID })
	return mapped, nil
}

func mapReference(kind ReferenceKind, ref agentmanifest.Reference, mapper ReferenceMapper) (agentmanifest.Reference, error) {
	mapped, err := mapper(kind, ref)
	if err != nil {
		return agentmanifest.Reference{}, fmt.Errorf("%w: %s %q: %v", ErrInvalidDefinition, kind, ref.ID, err)
	}
	return mapped, nil
}

func validIdentity(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && strings.TrimSpace(value) == value && len(value) <= 256
}

func copyReferences(refs []agentmanifest.Reference) []agentmanifest.Reference {
	return append([]agentmanifest.Reference{}, refs...)
}
