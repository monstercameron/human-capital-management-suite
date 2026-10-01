package agentskills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

var (
	ErrInvalidDefinition = errors.New("agentskills: invalid skill definition")
	ErrAlreadyPublished  = errors.New("agentskills: skill version already published")
	ErrUnknownSkill      = errors.New("agentskills: unknown skill version")
	ErrDigestMismatch    = errors.New("agentskills: skill pin digest mismatch")
	ErrRetiredSkill      = errors.New("agentskills: skill version is retired")
	ErrDuplicateSkillPin = errors.New("agentskills: duplicate skill pin")
)

// CapabilityCatalog is the read-only part of capability.Registry used by
// publication. Keeping this seam small permits bootstrap and conformance
// fixtures without duplicating the capability registry.
type CapabilityCatalog interface {
	Lookup(capability.Key) (capability.Record, bool)
}

type skillEntry struct {
	record SkillRecord
}

// Registry publishes immutable skill versions against exact capability
// versions. It is safe for concurrent publication and lookup.
type Registry struct {
	mu           sync.RWMutex
	capabilities CapabilityCatalog
	entries      map[SkillKey]skillEntry
}

func NewRegistry(capabilities CapabilityCatalog) *Registry {
	return &Registry{capabilities: capabilities, entries: make(map[SkillKey]skillEntry)}
}

// Publish validates and publishes one skill version. A published key can
// never be replaced; changing schemas or operations requires a new version.
func (r *Registry) Publish(def SkillDefinition) error {
	resolved, highest, err := r.resolve(def)
	if err != nil {
		return err
	}
	digest, err := digestDefinition(def)
	if err != nil {
		return err
	}
	key := def.Key()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[key]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyPublished, key)
	}
	status := StatusActive
	if def.Deprecation != nil {
		status = StatusDeprecated
	}
	r.entries[key] = skillEntry{record: SkillRecord{
		Definition:            def.clone(),
		Digest:                digest,
		Status:                status,
		ResolvedOperations:    resolved,
		HighestCapabilityTier: highest,
	}}
	return nil
}

// Register is a semantic alias for Publish, useful to callers that register
// skills alongside capability definitions.
func (r *Registry) Register(def SkillDefinition) error { return r.Publish(def) }

func (r *Registry) resolve(def SkillDefinition) ([]ResolvedOperation, SideEffectTier, error) {
	if err := validateDefinition(def); err != nil {
		return nil, 0, err
	}
	resolved := make([]ResolvedOperation, 0, len(def.Operations))
	highest := TierRead
	for _, operation := range def.Operations {
		switch operation.Kind {
		case OperationCapability:
			if r.capabilities == nil {
				return nil, 0, fmt.Errorf("%w: capability catalog is required", ErrInvalidDefinition)
			}
			record, ok := r.capabilities.Lookup(operation.Capability)
			if !ok {
				return nil, 0, fmt.Errorf("%w: capability %s", ErrInvalidDefinition, operation.Capability.String())
			}
			if !record.Definition.AgentEligible {
				return nil, 0, fmt.Errorf("%w: capability %s is not agent eligible", ErrInvalidDefinition, operation.Capability.String())
			}
			if record.Status == capability.StatusRetired {
				return nil, 0, fmt.Errorf("%w: capability %s is retired", ErrInvalidDefinition, operation.Capability.String())
			}
			minimum := MinimumTierForEffect(record.Definition.EffectClass)
			if minimum > highest {
				highest = minimum
			}
			resolved = append(resolved, ResolvedOperation{Reference: operation, Capability: record, HasCapability: true})
		case OperationConnection:
			// Connection operations are governed by their connection grant. The
			// skill's declared tier is the ceiling for this operation.
			resolved = append(resolved, ResolvedOperation{Reference: operation})
		default:
			return nil, 0, fmt.Errorf("%w: operation kind %q", ErrInvalidDefinition, operation.Kind)
		}
	}
	if def.SideEffectTier < highest {
		return nil, 0, fmt.Errorf("%w: declared tier %s is below capability requirement %s", ErrInvalidDefinition, def.SideEffectTier, highest)
	}
	return resolved, highest, nil
}

func validateDefinition(def SkillDefinition) error {
	if strings.TrimSpace(def.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidDefinition)
	}
	if def.Version == 0 {
		return fmt.Errorf("%w: version must be >= 1", ErrInvalidDefinition)
	}
	if strings.TrimSpace(def.Owner) == "" {
		return fmt.Errorf("%w: owner is required", ErrInvalidDefinition)
	}
	if strings.TrimSpace(def.Description) == "" {
		return fmt.Errorf("%w: description is required", ErrInvalidDefinition)
	}
	if !def.SideEffectTier.Valid() {
		return fmt.Errorf("%w: invalid side-effect tier %d", ErrInvalidDefinition, def.SideEffectTier)
	}
	if len(def.Operations) == 0 {
		return fmt.Errorf("%w: at least one operation is required", ErrInvalidDefinition)
	}
	if strings.TrimSpace(def.IdempotencyRule) == "" {
		return fmt.Errorf("%w: idempotency rule is required", ErrInvalidDefinition)
	}
	if strings.TrimSpace(def.CostClass) == "" {
		return fmt.Errorf("%w: cost class is required", ErrInvalidDefinition)
	}
	for field, schema := range map[string]json.RawMessage{"inputSchema": def.InputSchema, "outputSchema": def.OutputSchema} {
		if !isMCPObjectSchema(schema) {
			return fmt.Errorf("%w: %s must be an object JSON schema", ErrInvalidDefinition, field)
		}
	}
	for _, operation := range def.Operations {
		switch operation.Kind {
		case OperationCapability:
			if strings.TrimSpace(operation.Capability.ID) == "" || operation.Capability.Version == 0 {
				return fmt.Errorf("%w: capability operation needs an exact id and version", ErrInvalidDefinition)
			}
			if operation.ConnectionID != "" || operation.Operation != "" {
				return fmt.Errorf("%w: capability operation cannot include connection fields", ErrInvalidDefinition)
			}
		case OperationConnection:
			if strings.TrimSpace(operation.ConnectionID) == "" || strings.TrimSpace(operation.Operation) == "" {
				return fmt.Errorf("%w: connection operation needs connection id and operation", ErrInvalidDefinition)
			}
			if operation.Capability != (capability.Key{}) {
				return fmt.Errorf("%w: connection operation cannot include a capability", ErrInvalidDefinition)
			}
		default:
			return fmt.Errorf("%w: unsupported operation kind %q", ErrInvalidDefinition, operation.Kind)
		}
	}
	if def.Deprecation != nil && strings.TrimSpace(def.Deprecation.Reason) == "" {
		return fmt.Errorf("%w: deprecation reason is required", ErrInvalidDefinition)
	}
	return nil
}

func isMCPObjectSchema(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return false
	}
	typeValue, ok := value["type"]
	if !ok {
		return true
	}
	var schemaType string
	return json.Unmarshal(typeValue, &schemaType) == nil && schemaType == "object"
}

func digestDefinition(def SkillDefinition) (string, error) {
	canonical := struct {
		ID                 string
		Version            uint32
		Owner              string
		Description        string
		InputSchema        json.RawMessage
		OutputSchema       json.RawMessage
		Operations         []OperationRef
		SideEffectTier     SideEffectTier
		RequiredPurposes   []string
		DataClassesRead    []string
		DataClassesWritten []string
		IdempotencyRule    string
		CostClass          string
		EvalRefs           []string
		Deprecation        *Deprecation
	}{
		ID: def.ID, Version: def.Version, Owner: def.Owner, Description: def.Description,
		InputSchema: def.InputSchema, OutputSchema: def.OutputSchema, Operations: def.Operations,
		SideEffectTier: def.SideEffectTier, RequiredPurposes: def.RequiredPurposes,
		DataClassesRead: def.DataClassesRead, DataClassesWritten: def.DataClassesWritten,
		IdempotencyRule: def.IdempotencyRule, CostClass: def.CostClass, EvalRefs: def.EvalRefs,
		Deprecation: def.Deprecation,
	}
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: digest manifest: %v", ErrInvalidDefinition, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (r *Registry) Lookup(key SkillKey) (SkillRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[key]
	if !ok {
		return SkillRecord{}, false
	}
	return entry.record.clone(), true
}

func (r *Registry) ResolvePin(pin SkillPin) (SkillRecord, error) {
	record, ok := r.Lookup(pin.Key())
	if !ok {
		return SkillRecord{}, fmt.Errorf("%w: %s", ErrUnknownSkill, pin.Key())
	}
	if pin.Digest == "" || pin.Digest != record.Digest {
		return SkillRecord{}, fmt.Errorf("%w: %s", ErrDigestMismatch, pin.Key())
	}
	if record.Status == StatusRetired {
		return SkillRecord{}, fmt.Errorf("%w: %s", ErrRetiredSkill, pin.Key())
	}
	return record, nil
}

func (r *Registry) Pin(key SkillKey) (SkillPin, error) {
	record, ok := r.Lookup(key)
	if !ok {
		return SkillPin{}, fmt.Errorf("%w: %s", ErrUnknownSkill, key)
	}
	if record.Status == StatusRetired {
		return SkillPin{}, fmt.Errorf("%w: %s", ErrRetiredSkill, key)
	}
	return SkillPin{ID: key.ID, Version: key.Version, Digest: record.Digest}, nil
}

func (r *Registry) List() []SkillRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]SkillRecord, 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, entry.record.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Definition.ID != out[j].Definition.ID {
			return out[i].Definition.ID < out[j].Definition.ID
		}
		return out[i].Definition.Version < out[j].Definition.Version
	})
	return out
}

func (r *Registry) Deprecate(key SkillKey) error { return r.setStatus(key, StatusDeprecated) }

func (r *Registry) Retire(key SkillKey) error { return r.setStatus(key, StatusRetired) }

func (r *Registry) setStatus(key SkillKey, status Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[key]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownSkill, key)
	}
	entry.record.Status = status
	r.entries[key] = entry
	return nil
}
