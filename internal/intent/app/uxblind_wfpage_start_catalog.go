package app

import (
	"context"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// WorkflowStartAvailability is deliberately a small, display-safe vocabulary.
// The launcher never receives capability names, subject facts, or policy
// details when a start is unavailable.
type WorkflowStartAvailability string

const (
	WorkflowStartAvailable           WorkflowStartAvailability = "available"
	WorkflowStartNoCapability        WorkflowStartAvailability = "no_capability"
	WorkflowStartMissingAuthority    WorkflowStartAvailability = "missing_authority"
	WorkflowStartMissingPrerequisite WorkflowStartAvailability = "missing_prerequisite"
	WorkflowStartQuarantined         WorkflowStartAvailability = "quarantined"
)

// WorkflowStartCandidate is the definition-owned metadata needed to build the
// viewer's launcher projection. It is not an authorization decision.
type WorkflowStartCandidate struct {
	WorkflowID      string
	Version         uint32
	SemanticVersion string
	Name            string
	Description     string
	Category        string
	Keywords        []string
	Icon            string
	Owner           string
	Status          string
	Hidden          bool
	Quarantined     bool
}

// WorkflowStartDecision is returned by the same start-authority check used by
// the start intent. Only this bounded result crosses into the catalog.
type WorkflowStartDecision struct {
	Availability WorkflowStartAvailability
}

// WorkflowStartAuthority is the application boundary for the shared start
// authorization check. Implementations may inspect capabilities, authority,
// and prerequisite facts, but must return only the safe availability class.
type WorkflowStartAuthority func(context.Context, values.TenantId, WorkflowStartCandidate) WorkflowStartDecision

// WorkflowStartCatalogEntry is the server projection consumed by product
// clients. Hidden definitions are never represented by an entry.
type WorkflowStartCatalogEntry struct {
	WorkflowStartCandidate
	Availability WorkflowStartAvailability
}

// BuildWorkflowStartCatalog returns active published versions that the viewer
// may discover, with a reason when the shared start check says they cannot be
// started. Hidden and malformed definitions fail closed and are omitted.
func BuildWorkflowStartCatalog(ctx context.Context, tenant values.TenantId, candidates []WorkflowStartCandidate, authority WorkflowStartAuthority) []WorkflowStartCatalogEntry {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil
		}
	}
	// The version store normally supplies one active version per workflow. Keep
	// this boundary deterministic anyway: a retry or a mixed read must not let
	// input order decide which display metadata reaches the viewer.
	latest := make(map[string]WorkflowStartCandidate, len(candidates))
	for _, candidate := range candidates {
		id := strings.TrimSpace(candidate.WorkflowID)
		if id == "" || candidate.Version == 0 || strings.TrimSpace(candidate.Name) == "" || candidate.Hidden || !strings.EqualFold(strings.TrimSpace(candidate.Status), "ACTIVE") {
			continue
		}
		candidate.WorkflowID = id
		candidate.Keywords = append([]string(nil), candidate.Keywords...)
		candidate.Name = strings.TrimSpace(candidate.Name)
		candidate.Description = strings.TrimSpace(candidate.Description)
		candidate.Category = strings.TrimSpace(candidate.Category)
		candidate.Icon = strings.TrimSpace(candidate.Icon)
		candidate.Owner = strings.TrimSpace(candidate.Owner)
		candidate.SemanticVersion = strings.TrimSpace(candidate.SemanticVersion)
		candidate.Status = strings.TrimSpace(candidate.Status)
		if previous, ok := latest[id]; ok && previous.Version >= candidate.Version {
			continue
		}
		latest[id] = candidate
	}
	result := make([]WorkflowStartCatalogEntry, 0, len(latest))
	for _, candidate := range latest {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return nil
			}
		}
		decision := WorkflowStartDecision{Availability: WorkflowStartMissingAuthority}
		if candidate.Quarantined {
			decision.Availability = WorkflowStartQuarantined
		} else if authority != nil {
			decision = authority(ctx, tenant, candidate)
		}
		if !validWorkflowStartAvailability(decision.Availability) {
			decision.Availability = WorkflowStartMissingAuthority
		}
		result = append(result, WorkflowStartCatalogEntry{WorkflowStartCandidate: candidate, Availability: decision.Availability})
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := strings.ToLower(strings.TrimSpace(result[i].Name)), strings.ToLower(strings.TrimSpace(result[j].Name))
		if left == right {
			return result[i].WorkflowID < result[j].WorkflowID
		}
		return left < right
	})
	return result
}

func validWorkflowStartAvailability(value WorkflowStartAvailability) bool {
	switch value {
	case WorkflowStartAvailable, WorkflowStartNoCapability, WorkflowStartMissingAuthority, WorkflowStartMissingPrerequisite, WorkflowStartQuarantined:
		return true
	default:
		return false
	}
}
