package fixtures

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// orgRelationshipRecord is one corpus manager edge: the worker's direct
// manager as the fixture company declares it. manager_key names another
// corpus worker; when it is empty, manager_external_id names the external
// party (such as the board chair) the chain ends at, and no corpus record
// answers for them.
type orgRelationshipRecord struct {
	ID                string `json:"id"`
	WorkerKey         string `json:"worker_key"`
	ManagerKey        string `json:"manager_key"`
	ManagerExternalID string `json:"manager_external_id"`
	AssignmentID      string `json:"assignment_id"`
	Type              string `json:"type"`
	EffectiveFrom     string `json:"effective_from"`
	KnownAt           string `json:"known_at"`
	RecordedAt        string `json:"recorded_at"`
	RevisionStream    string `json:"revision_stream"`
	RevisionSequence  uint64 `json:"revision_sequence"`
	EvidenceRef       string `json:"evidence_ref"`
}

// orgGraphFile is the on-disk shape of the corpus manager graph.
type orgGraphFile struct {
	PolicyVersion     string                  `json:"policy_version"`
	WatermarkStream   string                  `json:"watermark_stream"`
	WatermarkSequence uint64                  `json:"watermark_sequence"`
	SourceSystem      string                  `json:"source_system"`
	AuthorityPolicy   string                  `json:"authority_policy"`
	Relationships     []orgRelationshipRecord `json:"relationships"`
}

// OrgGraph returns the parsed corpus manager graph.
func OrgGraph() (orgGraphFile, error) {
	if err := load(); err != nil {
		return orgGraphFile{}, err
	}
	return orgGraph, nil
}

// MemoryOrgFacts is an in-memory org.WorkerFacts implementation over the
// fixture corpus. It answers the direct-manager edges org-graph.json
// declares and nothing wider. A worker the graph does not name is unknown
// to the graph (Exists false), which ends a resolved chain cleanly rather
// than refusing it; a worker it names with an external manager resolves a
// one-hop chain to that external party.
type MemoryOrgFacts struct {
	byWorkerID map[string]org.ManagerRelationshipFact
	watermark  values.RevisionToken
	policy     string
}

// NewMemoryOrgFacts builds the in-memory reader over the fixture corpus.
func NewMemoryOrgFacts() (*MemoryOrgFacts, error) {
	if err := load(); err != nil {
		return nil, err
	}
	byKey := make(map[string]string, len(workers.Workers))
	for _, w := range workers.Workers {
		byKey[w.Key] = w.ID
	}
	watermark, err := values.NewSequenceRevision(orgGraph.WatermarkStream, orgGraph.WatermarkSequence)
	if err != nil {
		return nil, fmt.Errorf("fixtures: org graph watermark: %w", err)
	}
	out := &MemoryOrgFacts{
		byWorkerID: make(map[string]org.ManagerRelationshipFact, len(orgGraph.Relationships)),
		watermark:  watermark,
		policy:     orgGraph.PolicyVersion,
	}
	seen := make(map[string]struct{}, len(orgGraph.Relationships))
	for _, record := range orgGraph.Relationships {
		workerID, ok := byKey[record.WorkerKey]
		if !ok {
			return nil, fmt.Errorf("fixtures: org graph relationship %s names unknown worker %q", record.ID, record.WorkerKey)
		}
		if _, dup := seen[workerID]; dup {
			return nil, fmt.Errorf("fixtures: org graph names two direct managers for worker %q", record.WorkerKey)
		}
		seen[workerID] = struct{}{}
		managerID := record.ManagerExternalID
		if record.ManagerKey != "" {
			managerID, ok = byKey[record.ManagerKey]
			if !ok {
				return nil, fmt.Errorf("fixtures: org graph relationship %s names unknown manager %q", record.ID, record.ManagerKey)
			}
		}
		relType := org.RelationshipDirectManager
		if record.Type != "" && record.Type != string(org.RelationshipDirectManager) {
			if record.Type != string(org.RelationshipDottedLine) {
				return nil, fmt.Errorf("fixtures: org graph relationship %s has unknown type %q", record.ID, record.Type)
			}
			relType = org.RelationshipDottedLine
		}
		effectiveFrom, err := instant(record.EffectiveFrom)
		if err != nil {
			return nil, fmt.Errorf("fixtures: org graph relationship %s: %w", record.ID, err)
		}
		effective, err := values.NewOpenInstantInterval(effectiveFrom)
		if err != nil {
			return nil, fmt.Errorf("fixtures: org graph relationship %s: %w", record.ID, err)
		}
		knownInstant, err := instant(record.KnownAt)
		if err != nil {
			return nil, fmt.Errorf("fixtures: org graph relationship %s: %w", record.ID, err)
		}
		knownAt, err := values.NewKnownAt(knownInstant)
		if err != nil {
			return nil, fmt.Errorf("fixtures: org graph relationship %s: %w", record.ID, err)
		}
		recordedInstant, err := instant(record.RecordedAt)
		if err != nil {
			return nil, fmt.Errorf("fixtures: org graph relationship %s: %w", record.ID, err)
		}
		recordedAt, err := values.NewRecordedAt(recordedInstant)
		if err != nil {
			return nil, fmt.Errorf("fixtures: org graph relationship %s: %w", record.ID, err)
		}
		revision, err := values.NewSequenceRevision(record.RevisionStream, record.RevisionSequence)
		if err != nil {
			return nil, fmt.Errorf("fixtures: org graph relationship %s: %w", record.ID, err)
		}
		fact := org.ManagerRelationshipFact{
			RelationshipID: record.ID,
			Type:           relType,
			Worker:         values.EntityRef{Tenant: Tenant, Kind: people.KindWorker, Id: workerID},
			Manager:        values.EntityRef{Tenant: Tenant, Kind: people.KindWorker, Id: managerID},
			AssignmentID:   record.AssignmentID,
			Effective:      effective,
			KnownAt:        knownAt,
			Revision:       revision,
			Authority:      evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: orgGraph.SourceSystem, PolicyRef: orgGraph.AuthorityPolicy},
			Provenance:     evidence.Provenance{Source: orgGraph.SourceSystem, EvidenceRef: record.EvidenceRef, RecordedAt: recordedAt},
		}
		if err := fact.Validate(); err != nil {
			return nil, fmt.Errorf("fixtures: org graph relationship %s: %w", record.ID, err)
		}
		out.byWorkerID[workerID] = fact
	}
	return out, nil
}

// WorkerFactsAt implements org.WorkerFacts.
func (m *MemoryOrgFacts) WorkerFactsAt(_ context.Context, q org.WorkerFactsQuery) (org.WorkerFactSet, error) {
	if err := q.Validate(); err != nil {
		return org.WorkerFactSet{}, err
	}
	set := org.WorkerFactSet{Worker: q.Worker, Watermark: m.watermark, PolicyVersion: m.policy}
	fact, ok := m.byWorkerID[q.Worker.Id]
	if !ok || q.Worker.Tenant != Tenant {
		return set, nil
	}
	set.Exists = true
	set.Relationships = []org.ManagerRelationshipFact{fact}
	return set, nil
}
