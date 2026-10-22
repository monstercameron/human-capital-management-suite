//go:build !(js && wasm)

// CHATBUG-014: server-side only. Planned writes belong to the intent engine,
// which the browser client must not carry.

package org

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	ManagerRelationshipIDField = "org.manager_relationship.relationship_id"
	ManagerWorkerIDField       = "org.manager_relationship.manager_id"
)

var (
	ErrManagerMutationInvalid  = errors.New("org: manager mutation is invalid")
	ErrManagerMutationCycle    = errors.New("org: manager mutation would create a cycle")
	ErrManagerMutationNoChange = errors.New("org: manager mutation has no change")
)

// ManagerMutation is the Organization-owned writeback for one direct manager
// relationship. It does not mutate Person or copy manager identifiers into an
// unrelated aggregate.
type ManagerMutation struct {
	Tenant             values.TenantId
	ProposalRevisionID string
	ProposalDigest     string
	ActorPrincipalID   string
	AuthorityDecision  string
	WorkerID           string
	AssignmentID       string
	ResourceKey        values.ResourceKey
	Subject            intent.SubjectReference
	Effective          values.EffectiveInterval
	ExpectedRevision   values.RevisionToken

	CurrentRelationshipID string
	TargetRelationshipID  string
	CurrentManagerID      string
	TargetManagerID       string
	ManagerAncestors      []string
}

func (m ManagerMutation) Validate() error {
	if err := m.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrManagerMutationInvalid, err)
	}
	for name, value := range map[string]string{"proposal_revision_id": m.ProposalRevisionID, "proposal_digest": m.ProposalDigest,
		"actor_principal_id": m.ActorPrincipalID, "authority_decision": m.AuthorityDecision, "worker_id": m.WorkerID,
		"assignment_id": m.AssignmentID, "target_relationship_id": m.TargetRelationshipID, "target_manager_id": m.TargetManagerID} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrManagerMutationInvalid, name)
		}
	}
	if m.TargetManagerID == m.WorkerID || slices.Contains(m.ManagerAncestors, m.WorkerID) {
		return ErrManagerMutationCycle
	}
	if err := m.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrManagerMutationInvalid, err)
	}
	if m.Subject.Kind != "worker" {
		return fmt.Errorf("%w: subject kind %q is not a worker", ErrManagerMutationInvalid, m.Subject.Kind)
	}
	if m.Subject.SubjectID != m.WorkerID {
		return fmt.Errorf("%w: subject is not the worker", ErrManagerMutationInvalid)
	}
	if err := m.ResourceKey.Validate(); err != nil || m.ResourceKey.Tenant != m.Tenant {
		return fmt.Errorf("%w: resource key is outside tenant", ErrManagerMutationInvalid)
	}
	if err := m.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrManagerMutationInvalid, err)
	}
	if !m.ExpectedRevision.IsSpecified() {
		return fmt.Errorf("%w: expected revision is required", ErrManagerMutationInvalid)
	}
	if m.CurrentRelationshipID == m.TargetRelationshipID && m.CurrentManagerID == m.TargetManagerID {
		return ErrManagerMutationNoChange
	}
	return nil
}

func (m ManagerMutation) PlannedWrites() ([]intent.PlannedWrite, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	writes := make([]intent.PlannedWrite, 0, 2)
	if m.CurrentRelationshipID != m.TargetRelationshipID {
		writes = append(writes, intent.PlannedWrite{Subject: m.Subject, ResourceKey: m.ResourceKey, FieldPath: ManagerRelationshipIDField,
			CurrentCanonicalText: m.CurrentRelationshipID, ProposedCanonicalText: m.TargetRelationshipID,
			SourceAuthorityDecision: m.AuthorityDecision, ExpectedRevision: m.ExpectedRevision})
	}
	if m.CurrentManagerID != m.TargetManagerID {
		writes = append(writes, intent.PlannedWrite{Subject: m.Subject, ResourceKey: m.ResourceKey, FieldPath: ManagerWorkerIDField,
			CurrentCanonicalText: m.CurrentManagerID, ProposedCanonicalText: m.TargetManagerID,
			SourceAuthorityDecision: m.AuthorityDecision, ExpectedRevision: m.ExpectedRevision})
	}
	return writes, nil
}

func (m ManagerMutation) Explain() string {
	return fmt.Sprintf("organization manager proposal=%s worker=%s assignment=%s relationship=%s manager=%s",
		m.ProposalRevisionID, m.WorkerID, m.AssignmentID, m.TargetRelationshipID, m.TargetManagerID)
}

func ExplainManagerMutation(m ManagerMutation) string { return m.Explain() }
