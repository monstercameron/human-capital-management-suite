// Package agentskills publishes immutable, versioned skills assembled from
// the platform capability registry.
package agentskills

import (
	"encoding/json"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// SideEffectTier is the agent write ladder from AGENT2-001.
type SideEffectTier uint8

const (
	TierRead SideEffectTier = iota
	TierPrivateDraft
	TierCommunicate
	TierSubmitGoverned
	TierExternalWrite
)

// TierT0 through TierT4 are protocol-friendly aliases for callers that use
// the governance labels directly.
const (
	TierT0 = TierRead
	TierT1 = TierPrivateDraft
	TierT2 = TierCommunicate
	TierT3 = TierSubmitGoverned
	TierT4 = TierExternalWrite
)

func (t SideEffectTier) String() string {
	switch t {
	case TierRead:
		return "T0"
	case TierPrivateDraft:
		return "T1"
	case TierCommunicate:
		return "T2"
	case TierSubmitGoverned:
		return "T3"
	case TierExternalWrite:
		return "T4"
	default:
		return fmt.Sprintf("T%d", uint8(t))
	}
}

// Valid reports whether t is one of the five published tiers.
func (t SideEffectTier) Valid() bool { return t <= TierExternalWrite }

// MinimumTierForEffect translates the capability effect ladder to the
// narrowest agent tier that can safely contain it. A capability can never be
// exposed through a skill that declares a lower tier.
func MinimumTierForEffect(effect capability.EffectClass) SideEffectTier {
	switch effect {
	case capability.EffectPure, capability.EffectReadOnly:
		return TierRead
	case capability.EffectInternalMutation:
		return TierSubmitGoverned
	case capability.EffectExternalMutation, capability.EffectIrreversibleExternalMutation:
		return TierExternalWrite
	default:
		return TierExternalWrite
	}
}

// RequiredTier is the minimum tier needed for a resolved capability record.
func RequiredTier(record capability.Record) SideEffectTier {
	return MinimumTierForEffect(record.Definition.EffectClass)
}

// OperationKind identifies the registry consulted for an operation reference.
type OperationKind string

const (
	OperationCapability OperationKind = "CAPABILITY"
	OperationConnection OperationKind = "CONNECTION"
)

// OperationRef is either an exact capability version or an operation exposed
// by an administrator-granted connection. Capability references are resolved
// at publication and retained in the published record; connection references
// remain explicit and are validated by the connection layer at invocation.
type OperationRef struct {
	Kind         OperationKind
	Capability   capability.Key
	ConnectionID string
	Operation    string
}

// Deprecation records an intentional retirement path without changing the
// meaning of an already-published version.
type Deprecation struct {
	Reason     string
	ReplacedBy *SkillKey
}

// SkillKey identifies one exact immutable skill version.
type SkillKey struct {
	ID      string
	Version uint32
}

func (k SkillKey) String() string { return fmt.Sprintf("%s/v%d", k.ID, k.Version) }

// SkillPin is the version and content digest a task stores. The digest makes
// a stale or tampered task fail closed even if a caller supplies the same ID
// and version with different schemas.
type SkillPin struct {
	ID      string `json:"id"`
	Version uint32 `json:"version"`
	Digest  string `json:"digest"`
}

// SkillVersionPin is a descriptive alias used by task/run callers.
type SkillVersionPin = SkillPin

func (p SkillPin) Key() SkillKey { return SkillKey{ID: p.ID, Version: p.Version} }

// SkillDefinition is an immutable, typed skill manifest. JSON schemas are
// retained as JSON objects because MCP transports schemas without imposing a
// particular Go request type on the skill registry.
type SkillDefinition struct {
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
}

// ResolvedOperation keeps the source reference and, for capabilities, the
// exact registry record that justified publication. Skill metadata is derived
// from this record rather than copied into SkillDefinition.
type ResolvedOperation struct {
	Reference     OperationRef
	Capability    capability.Record
	HasCapability bool
}

// SkillRecord is the published immutable value returned by lookups.
type SkillRecord struct {
	Definition            SkillDefinition
	Digest                string
	Status                Status
	ResolvedOperations    []ResolvedOperation
	HighestCapabilityTier SideEffectTier
}

// Status is the skill lifecycle state. Deprecated versions remain resolvable
// for pinned tasks; retired versions remain historical but are not projected.
type Status string

const (
	StatusActive     Status = "ACTIVE"
	StatusDeprecated Status = "DEPRECATED"
	StatusRetired    Status = "RETIRED"
)

// MCPProtocolRevision is the pinned protocol revision used by this package's
// conformance checker. The projection itself only uses the stable tools/list
// fields required by that revision.
const MCPProtocolRevision = "2025-06-18"

func (d SkillDefinition) Key() SkillKey { return SkillKey{ID: d.ID, Version: d.Version} }

func (d SkillDefinition) clone() SkillDefinition {
	c := d
	c.InputSchema = append(json.RawMessage(nil), d.InputSchema...)
	c.OutputSchema = append(json.RawMessage(nil), d.OutputSchema...)
	c.Operations = append([]OperationRef(nil), d.Operations...)
	c.RequiredPurposes = append([]string(nil), d.RequiredPurposes...)
	c.DataClassesRead = append([]string(nil), d.DataClassesRead...)
	c.DataClassesWritten = append([]string(nil), d.DataClassesWritten...)
	c.EvalRefs = append([]string(nil), d.EvalRefs...)
	if d.Deprecation != nil {
		dep := *d.Deprecation
		if d.Deprecation.ReplacedBy != nil {
			key := *d.Deprecation.ReplacedBy
			dep.ReplacedBy = &key
		}
		c.Deprecation = &dep
	}
	return c
}

func (r SkillRecord) clone() SkillRecord {
	c := r
	c.Definition = r.Definition.clone()
	c.ResolvedOperations = append([]ResolvedOperation(nil), r.ResolvedOperations...)
	for i := range c.ResolvedOperations {
		definition := r.ResolvedOperations[i].Capability.Definition
		definition.ReadData.DataDomains = append([]string(nil), definition.ReadData.DataDomains...)
		definition.ReadData.FieldPaths = append([]string(nil), definition.ReadData.FieldPaths...)
		definition.WriteData.DataDomains = append([]string(nil), definition.WriteData.DataDomains...)
		definition.WriteData.FieldPaths = append([]string(nil), definition.WriteData.FieldPaths...)
		c.ResolvedOperations[i].Capability.Definition = definition
	}
	return c
}
