package agenttemplate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

var (
	// ErrInvalidTemplate identifies an incomplete or unsupported template.
	ErrInvalidTemplate = errors.New("agenttemplate: invalid template")
	// ErrAlreadyPublished means an immutable template version already exists.
	ErrAlreadyPublished = errors.New("agenttemplate: template version already published")
	// ErrUnknownTemplate means the requested immutable template version is absent.
	ErrUnknownTemplate = errors.New("agenttemplate: unknown template version")
	// ErrIncompatibleSchema means the target manifest schema is not supported.
	ErrIncompatibleSchema = errors.New("agenttemplate: incompatible manifest schema")
)

// Definition is a platform-authored, immutable starting point. It contains
// prose and schema compatibility only; it cannot name sources, tools, grants,
// model policies, or evaluation evidence.
type Definition struct {
	ID                    string `json:"id"`
	Version               uint32 `json:"version"`
	TemplateSchemaVersion uint32 `json:"template_schema_version"`
	ManifestSchemaVersion uint32 `json:"manifest_schema_version"`
	DisplayName           string `json:"display_name"`
	Summary               string `json:"summary"`
	Purpose               string `json:"purpose"`
	Instructions          string `json:"instructions"`
}

// Pin identifies one exact immutable template version and its content digest.
type Pin struct {
	ID                    string `json:"id"`
	Version               uint32 `json:"version"`
	TemplateSchemaVersion uint32 `json:"template_schema_version"`
	Digest                string `json:"digest"`
}

// Record is a published immutable template definition and its digest.
type Record struct {
	Definition Definition `json:"definition"`
	Digest     string     `json:"digest"`
}

// TenantDraft is a tenant-owned, unpublished copy of a template. Empty
// ceilings are explicit and convey no source or tool authority.
type TenantDraft struct {
	ID            string                    `json:"id"`
	TenantID      string                    `json:"tenant_id"`
	OwnerID       string                    `json:"owner_id"`
	Version       uint64                    `json:"version"`
	State         string                    `json:"state"`
	DisplayName   string                    `json:"display_name"`
	Purpose       string                    `json:"purpose"`
	Instructions  string                    `json:"instructions"`
	SourceCeiling []agentmanifest.Reference `json:"source_ceiling"`
	ToolCeiling   []agentmanifest.Reference `json:"tool_ceiling"`
	Template      Pin                       `json:"template_provenance"`
}

// InstallRequest supplies the tenant-owned identity for a new draft.
type InstallRequest struct {
	DraftID               string
	TenantID              string
	OwnerID               string
	ManifestSchemaVersion uint32
}

// Registry owns published template versions. Published entries cannot be
// replaced; changes require a new version.
type Registry struct {
	mu      sync.RWMutex
	entries map[key]Record
}

type key struct {
	id      string
	version uint32
}

// NewRegistry returns an empty versioned template registry.
func NewRegistry() *Registry { return &Registry{entries: make(map[key]Record)} }

// Publish validates and publishes one immutable template version.
func (r *Registry) Publish(def Definition) (Pin, error) {
	if r == nil {
		return Pin{}, ErrInvalidTemplate
	}
	if err := validate(def); err != nil {
		return Pin{}, err
	}
	digest, err := digestDefinition(def)
	if err != nil {
		return Pin{}, err
	}
	k := key{id: def.ID, version: def.Version}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[k]; exists {
		return Pin{}, fmt.Errorf("%w: %s/v%d", ErrAlreadyPublished, def.ID, def.Version)
	}
	r.entries[k] = Record{Definition: def, Digest: digest}
	return Pin{ID: def.ID, Version: def.Version, TemplateSchemaVersion: def.TemplateSchemaVersion, Digest: digest}, nil
}

// ResolvePin returns the exact published record named by pin, rejecting a
// stale or altered digest.
func (r *Registry) ResolvePin(pin Pin) (Record, error) {
	if r == nil {
		return Record{}, ErrUnknownTemplate
	}
	r.mu.RLock()
	record, ok := r.entries[key{id: pin.ID, version: pin.Version}]
	r.mu.RUnlock()
	if !ok {
		return Record{}, fmt.Errorf("%w: %s/v%d", ErrUnknownTemplate, pin.ID, pin.Version)
	}
	if pin.TemplateSchemaVersion != record.Definition.TemplateSchemaVersion || pin.Digest == "" || pin.Digest != record.Digest {
		return Record{}, fmt.Errorf("%w: template pin does not match published content", ErrInvalidTemplate)
	}
	return record, nil
}

// Install copies the exact pinned template into a new tenant-owned draft.
// The returned draft always has empty source and tool ceilings and remains in
// DRAFT state; this method does not validate, evaluate, review, publish, or
// grant access to anything.
func (r *Registry) Install(pin Pin, request InstallRequest) (TenantDraft, error) {
	record, err := r.ResolvePin(pin)
	if err != nil {
		return TenantDraft{}, err
	}
	if strings.TrimSpace(request.DraftID) == "" || strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.OwnerID) == "" {
		return TenantDraft{}, fmt.Errorf("%w: draft, tenant, and owner IDs are required", ErrInvalidTemplate)
	}
	if request.ManifestSchemaVersion != record.Definition.ManifestSchemaVersion {
		return TenantDraft{}, fmt.Errorf("%w: got %d, want %d", ErrIncompatibleSchema, request.ManifestSchemaVersion, record.Definition.ManifestSchemaVersion)
	}
	return TenantDraft{
		ID: request.DraftID, TenantID: request.TenantID, OwnerID: request.OwnerID,
		Version: 1, State: "DRAFT", DisplayName: record.Definition.DisplayName,
		Purpose: record.Definition.Purpose, Instructions: record.Definition.Instructions,
		SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{}, Template: pin,
	}, nil
}

// Lookup returns one exact template version without exposing registry state.
func (r *Registry) Lookup(id string, version uint32) (Record, bool) {
	if r == nil {
		return Record{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.entries[key{id: id, version: version}]
	return record, ok
}

// PlatformStarters publishes capability-neutral Policy Guide and Project
// Assistant starting points. Their lack of grants means they require the
// normal manifest, authority, evaluation, review, and publication flow before
// they can become useful or available to users.
func PlatformStarters() (*Registry, error) {
	registry := NewRegistry()
	for _, definition := range []Definition{
		{
			ID: "hcmnext.agent_template.policy_guide", Version: 1,
			TemplateSchemaVersion: 1, ManifestSchemaVersion: agentmanifest.CurrentSchemaVersion,
			DisplayName: "Policy Guide", Summary: "A starting point for policy questions.",
			Purpose:      "Help the tenant answer policy questions using approved sources.",
			Instructions: "Answer only from sources and capabilities explicitly approved during validation. Cite supporting sources. If no approved source supports an answer, say that the answer is unavailable.",
		},
		{
			ID: "hcmnext.agent_template.project_assistant", Version: 1,
			TemplateSchemaVersion: 1, ManifestSchemaVersion: agentmanifest.CurrentSchemaVersion,
			DisplayName: "Project Assistant", Summary: "A starting point for ordinary project work.",
			Purpose:      "Help the tenant coordinate ordinary project work using explicitly approved project context.",
			Instructions: "Use only project context and capabilities explicitly approved during validation. Do not claim to have changed project state. Ask for review before proposing a consequential action.",
		},
	} {
		if _, err := registry.Publish(definition); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func validate(def Definition) error {
	for field, value := range map[string]string{"id": def.ID, "display_name": def.DisplayName, "summary": def.Summary, "purpose": def.Purpose, "instructions": def.Instructions} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
			return fmt.Errorf("%w: %s is required and must be valid UTF-8 without surrounding whitespace", ErrInvalidTemplate, field)
		}
	}
	if len(def.ID) > 256 || len(def.DisplayName) > 256 || len(def.Summary) > 2000 || len(def.Purpose) > 4000 || len(def.Instructions) > 16000 {
		return fmt.Errorf("%w: text exceeds its limit", ErrInvalidTemplate)
	}
	if def.Version == 0 || def.TemplateSchemaVersion != 1 || def.ManifestSchemaVersion == 0 {
		return fmt.Errorf("%w: version or schema version is unsupported", ErrInvalidTemplate)
	}
	return nil
}

func digestDefinition(def Definition) (string, error) {
	encoded, err := json.Marshal(def)
	if err != nil {
		return "", fmt.Errorf("%w: encode definition: %v", ErrInvalidTemplate, err)
	}
	digest := sha256.Sum256(append([]byte("hcm-next-agent-template/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
