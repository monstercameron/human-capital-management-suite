package fixtures

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// positionCatalogRecord is one corpus position: the governed revision of a
// hiring slot the fixture company declares. Only catalogued codes resolve;
// a code the catalog does not name is a non-existent position, not a read
// failure.
type positionCatalogRecord struct {
	Code             string `json:"code"`
	ID               string `json:"id"`
	JobCode          string `json:"job_code"`
	OrgUnit          string `json:"org_unit"`
	LegalEntity      string `json:"legal_entity"`
	Lifecycle        string `json:"lifecycle"`
	CapacityFTE      string `json:"capacity_fte"`
	CapacityHeads    int64  `json:"capacity_heads"`
	EffectiveFrom    string `json:"effective_from"`
	KnownAt          string `json:"known_at"`
	RecordedAt       string `json:"recorded_at"`
	RevisionStream   string `json:"revision_stream"`
	RevisionSequence uint64 `json:"revision_sequence"`
	EvidenceRef      string `json:"evidence_ref"`
}

// positionCatalogFile is the on-disk shape of the corpus position catalog.
type positionCatalogFile struct {
	PolicyVersion   string                  `json:"policy_version"`
	SourceSystem    string                  `json:"source_system"`
	AuthorityPolicy string                  `json:"authority_policy"`
	Positions       []positionCatalogRecord `json:"positions"`
}

// PositionCatalog returns the parsed corpus position catalog.
func PositionCatalog() (positionCatalogFile, error) {
	if err := load(); err != nil {
		return positionCatalogFile{}, err
	}
	return positionCatalog, nil
}

// MemoryPositionCatalog is an in-memory position.PositionFacts
// implementation over the fixture corpus. It answers the catalogued
// revisions effective and known at the query coordinate and nothing wider.
type MemoryPositionCatalog struct {
	byID     map[string]position.PositionRevision
	byCode   map[string]string
	known    map[string]values.KnownAt
	calendar values.CalendarRef
}

// NewMemoryPositionCatalog builds the in-memory reader over the fixture
// corpus.
func NewMemoryPositionCatalog() (*MemoryPositionCatalog, error) {
	if err := load(); err != nil {
		return nil, err
	}
	cal, err := Calendar()
	if err != nil {
		return nil, err
	}
	out := &MemoryPositionCatalog{
		byID:     make(map[string]position.PositionRevision, len(positionCatalog.Positions)),
		byCode:   make(map[string]string, len(positionCatalog.Positions)),
		known:    make(map[string]values.KnownAt, len(positionCatalog.Positions)),
		calendar: cal,
	}
	for _, record := range positionCatalog.Positions {
		if _, dup := out.byCode[record.Code]; dup {
			return nil, fmt.Errorf("fixtures: position catalog names %q twice", record.Code)
		}
		if _, dup := out.byID[record.ID]; dup {
			return nil, fmt.Errorf("fixtures: position catalog names id %q twice", record.ID)
		}
		ref := values.EntityRef{Tenant: Tenant, Kind: position.KindPosition, Id: record.ID}
		if err := ref.Validate(); err != nil {
			return nil, fmt.Errorf("fixtures: position %s id: %w", record.Code, err)
		}
		lifecycle := position.Lifecycle(record.Lifecycle)
		if !lifecycle.Valid() {
			return nil, fmt.Errorf("fixtures: position %s has unknown lifecycle %q", record.Code, record.Lifecycle)
		}
		fte, err := values.NewDecimal(record.CapacityFTE, 4, MoneyRounding)
		if err != nil {
			return nil, fmt.Errorf("fixtures: position %s capacity: %w", record.Code, err)
		}
		effectiveFrom, err := values.ParseLocalDate(record.EffectiveFrom)
		if err != nil {
			return nil, fmt.Errorf("fixtures: position %s: %w", record.Code, err)
		}
		effective, err := values.NewOpenLocalDateInterval(effectiveFrom, cal)
		if err != nil {
			return nil, fmt.Errorf("fixtures: position %s: %w", record.Code, err)
		}
		knownInstant, err := instant(record.KnownAt)
		if err != nil {
			return nil, fmt.Errorf("fixtures: position %s: %w", record.Code, err)
		}
		knownAt, err := values.NewKnownAt(knownInstant)
		if err != nil {
			return nil, fmt.Errorf("fixtures: position %s: %w", record.Code, err)
		}
		recordedInstant, err := instant(record.RecordedAt)
		if err != nil {
			return nil, fmt.Errorf("fixtures: position %s: %w", record.Code, err)
		}
		recordedAt, err := values.NewRecordedAt(recordedInstant)
		if err != nil {
			return nil, fmt.Errorf("fixtures: position %s: %w", record.Code, err)
		}
		revision, err := values.NewSequenceRevision(record.RevisionStream, record.RevisionSequence)
		if err != nil {
			return nil, fmt.Errorf("fixtures: position %s: %w", record.Code, err)
		}
		if err := values.ValidateKnowledgeOrder(knownAt, recordedAt, false); err != nil {
			return nil, fmt.Errorf("fixtures: position %s: %w", record.Code, err)
		}
		revisionState := position.PositionRevision{
			Position:    ref,
			Revision:    revision,
			Effective:   effective,
			Lifecycle:   lifecycle,
			JobCode:     record.JobCode,
			OrgUnit:     record.OrgUnit,
			LegalEntity: record.LegalEntity,
			Capacity: position.CapacityPolicy{
				CapacityFTE:   fte,
				CapacityHeads: record.CapacityHeads,
			},
			Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: positionCatalog.SourceSystem, PolicyRef: positionCatalog.AuthorityPolicy},
			Provenance: evidence.Provenance{Source: positionCatalog.SourceSystem, EvidenceRef: record.EvidenceRef, RecordedAt: recordedAt},
		}
		if err := revisionState.Validate(); err != nil {
			return nil, fmt.Errorf("fixtures: position %s: %w", record.Code, err)
		}
		out.byID[record.ID] = revisionState
		out.byCode[record.Code] = record.ID
		out.known[record.ID] = knownAt
	}
	return out, nil
}

// PositionRefForCode resolves a catalogued position code to its entity
// reference. A code the catalog does not name reports false: callers turn
// that into a governed absence, never a guessed identity.
func (m *MemoryPositionCatalog) PositionRefForCode(code string) (values.EntityRef, bool) {
	id, ok := m.byCode[code]
	if !ok {
		return values.EntityRef{}, false
	}
	return values.EntityRef{Tenant: Tenant, Kind: position.KindPosition, Id: id}, true
}

// PositionRevisionAt implements position.PositionFacts.
func (m *MemoryPositionCatalog) PositionRevisionAt(_ context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	if err := q.Validate(); err != nil {
		return position.PositionRevision{}, false, err
	}
	if q.Tenant != Tenant {
		return position.PositionRevision{}, false, nil
	}
	revision, ok := m.byID[q.Position.Id]
	if !ok {
		return position.PositionRevision{}, false, nil
	}
	if err := q.AsOf.EffectiveOn.Validate(); err != nil {
		return position.PositionRevision{}, false, nil
	}
	contains, err := revision.Effective.ContainsDate(q.AsOf.EffectiveOn)
	if err != nil || !contains {
		return position.PositionRevision{}, false, nil
	}
	if m.known[q.Position.Id].Instant().After(q.AsOf.KnownAt.Instant()) {
		return position.PositionRevision{}, false, nil
	}
	return revision, true, nil
}
