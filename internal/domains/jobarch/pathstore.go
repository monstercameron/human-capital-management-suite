package jobarch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrOverlappingPath refuses a second published edge over live history.
	ErrOverlappingPath = errors.New("jobarch: overlapping published promotion path")
	// ErrUnknownPathRevision refuses pins to revisions the store never saw.
	ErrUnknownPathRevision = errors.New("jobarch: unknown promotion path revision")
	// ErrInvalidPin refuses pins without pinned profile revisions.
	ErrInvalidPin = errors.New("jobarch: invalid promotion pin")
	// ErrPathNotFound reports a missing path or pin inside a tenant.
	ErrPathNotFound = errors.New("jobarch: promotion path not found")
)

// AssignmentPin is the durable snapshot of one worker assignment: the path
// revision it means plus both pinned profile revisions. JobCode and
// GradeCode are checked display projections, never identities.
type AssignmentPin struct {
	AssignmentID  string
	PathID        string
	PathRevision  string
	SourceProfile ProfileRevisionRef
	TargetProfile ProfileRevisionRef
	JobCode       string
	GradeCode     string
	At            time.Time
}

func (p AssignmentPin) Validate() error {
	if strings.TrimSpace(p.AssignmentID) == "" || strings.TrimSpace(p.PathID) == "" || strings.TrimSpace(p.PathRevision) == "" {
		return fmt.Errorf("%w: assignment, path and revision are required", ErrInvalidPin)
	}
	if err := p.SourceProfile.Validate("assignment.source"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPin, err)
	}
	if err := p.TargetProfile.Validate("assignment.target"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPin, err)
	}
	if p.At.IsZero() {
		return fmt.Errorf("%w: pin time is required", ErrInvalidPin)
	}
	return nil
}

// ProposalPin is the durable snapshot a proposal simulates and commits
// against. Immutability is what keeps simulation and commit meaning the
// same revision.
type ProposalPin struct {
	ProposalID    string
	PathID        string
	PathRevision  string
	SourceProfile ProfileRevisionRef
	TargetProfile ProfileRevisionRef
	At            time.Time
}

func (p ProposalPin) Validate() error {
	if strings.TrimSpace(p.ProposalID) == "" || strings.TrimSpace(p.PathID) == "" || strings.TrimSpace(p.PathRevision) == "" {
		return fmt.Errorf("%w: proposal, path and revision are required", ErrInvalidPin)
	}
	if err := p.SourceProfile.Validate("proposal.source"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPin, err)
	}
	if err := p.TargetProfile.Validate("proposal.target"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPin, err)
	}
	if p.At.IsZero() {
		return fmt.Errorf("%w: pin time is required", ErrInvalidPin)
	}
	return nil
}

// MemoryPathStore is the tenant-scoped append-only promotion-path store. Rows
// carry effective/known coordinates; a path is never edited in place.
type MemoryPathStore struct {
	mu          sync.RWMutex
	paths       map[string]map[string][]PromotionPathRevision
	assignments map[string]map[string]AssignmentPin
	proposals   map[string]map[string]ProposalPin
}

// NewMemoryPathStore builds an empty path store.
func NewMemoryPathStore() *MemoryPathStore {
	return &MemoryPathStore{
		paths:       map[string]map[string][]PromotionPathRevision{},
		assignments: map[string]map[string]AssignmentPin{},
		proposals:   map[string]map[string]ProposalPin{},
	}
}

func pathWindowEnd(p PromotionPathRevision) time.Time {
	if p.EffectiveTo.IsZero() {
		return time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	}
	return p.EffectiveTo
}

func windowsOverlap(a, b PromotionPathRevision) bool {
	return a.EffectiveFrom.Before(pathWindowEnd(b)) && b.EffectiveFrom.Before(pathWindowEnd(a))
}

// SavePath appends one path revision. Identical re-saves are idempotent;
// overlapping PUBLISHED edges over the same path are refused so proposals
// can never straddle two live meanings.
func (s *MemoryPathStore) SavePath(ctx context.Context, tenantID string, path PromotionPathRevision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenant is required", ErrInvalidRevision)
	}
	if err := path.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byTenant, ok := s.paths[tenantID]
	if !ok {
		byTenant = map[string][]PromotionPathRevision{}
		s.paths[tenantID] = byTenant
	}
	rows := byTenant[path.PathIDOrID()]
	for _, row := range rows {
		if row.Revision == path.Revision {
			existing, err := row.Digest()
			if err != nil {
				return err
			}
			candidate, err := path.Digest()
			if err != nil {
				return err
			}
			if existing == candidate {
				return nil
			}
			return fmt.Errorf("%w: revision %q already exists with other content", ErrDuplicateIdentity, path.Revision)
		}
	}
	if path.Lifecycle == LifecyclePublished {
		for _, row := range rows {
			if row.Lifecycle == LifecyclePublished && windowsOverlap(row, path) {
				return fmt.Errorf("%w: path %q revision %q overlaps published revision %q", ErrOverlappingPath, path.PathIDOrID(), path.Revision, row.Revision)
			}
		}
	}
	byTenant[path.PathIDOrID()] = append(rows, path)
	return nil
}

// ListPaths returns every stored revision for one path, oldest first.
func (s *MemoryPathStore) ListPaths(ctx context.Context, tenantID, pathID string) ([]PromotionPathRevision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := append([]PromotionPathRevision(nil), s.paths[tenantID][pathID]...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Revision < rows[j].Revision })
	return rows, nil
}

// CurrentPath resolves the published revision covering at.
func (s *MemoryPathStore) CurrentPath(ctx context.Context, tenantID, pathID string, at time.Time) (PromotionPathRevision, error) {
	rows, err := s.ListPaths(ctx, tenantID, pathID)
	if err != nil {
		return PromotionPathRevision{}, err
	}
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if row.Lifecycle != LifecyclePublished {
			continue
		}
		if !at.Before(row.EffectiveFrom) && at.Before(pathWindowEnd(row)) {
			return row, nil
		}
	}
	return PromotionPathRevision{}, fmt.Errorf("%w: path %q has no published revision at %s", ErrPathNotFound, pathID, at.UTC().Format(time.RFC3339))
}

func (s *MemoryPathStore) findRevision(tenantID, pathID, revision string) (PromotionPathRevision, bool) {
	for _, row := range s.paths[tenantID][pathID] {
		if row.Revision == revision {
			return row, true
		}
	}
	return PromotionPathRevision{}, false
}

// PinAssignment snapshots one assignment against a stored path revision.
func (s *MemoryPathStore) PinAssignment(ctx context.Context, tenantID string, pin AssignmentPin) (AssignmentPin, error) {
	if err := ctx.Err(); err != nil {
		return AssignmentPin{}, err
	}
	if err := pin.Validate(); err != nil {
		return AssignmentPin{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.findRevision(tenantID, pin.PathID, pin.PathRevision)
	if !ok {
		return AssignmentPin{}, fmt.Errorf("%w: path %q revision %q", ErrUnknownPathRevision, pin.PathID, pin.PathRevision)
	}
	if stored.From != pin.SourceProfile || stored.To != pin.TargetProfile {
		return AssignmentPin{}, fmt.Errorf("%w: pin profiles do not match stored path revision", ErrInvalidPin)
	}
	byTenant, ok := s.assignments[tenantID]
	if !ok {
		byTenant = map[string]AssignmentPin{}
		s.assignments[tenantID] = byTenant
	}
	byTenant[pin.AssignmentID] = pin
	return pin, nil
}

// LoadAssignment reloads one assignment snapshot.
func (s *MemoryPathStore) LoadAssignment(ctx context.Context, tenantID, assignmentID string) (AssignmentPin, error) {
	if err := ctx.Err(); err != nil {
		return AssignmentPin{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	pin, ok := s.assignments[tenantID][assignmentID]
	if !ok {
		return AssignmentPin{}, fmt.Errorf("%w: assignment %q", ErrPathNotFound, assignmentID)
	}
	return pin, nil
}

// PinProposal snapshots one proposal against a stored path revision.
func (s *MemoryPathStore) PinProposal(ctx context.Context, tenantID string, pin ProposalPin) (ProposalPin, error) {
	if err := ctx.Err(); err != nil {
		return ProposalPin{}, err
	}
	if err := pin.Validate(); err != nil {
		return ProposalPin{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.findRevision(tenantID, pin.PathID, pin.PathRevision)
	if !ok {
		return ProposalPin{}, fmt.Errorf("%w: path %q revision %q", ErrUnknownPathRevision, pin.PathID, pin.PathRevision)
	}
	if stored.From != pin.SourceProfile || stored.To != pin.TargetProfile {
		return ProposalPin{}, fmt.Errorf("%w: pin profiles do not match stored path revision", ErrInvalidPin)
	}
	byTenant, ok := s.proposals[tenantID]
	if !ok {
		byTenant = map[string]ProposalPin{}
		s.proposals[tenantID] = byTenant
	}
	byTenant[pin.ProposalID] = pin
	return pin, nil
}

// LoadProposal reloads one proposal snapshot.
func (s *MemoryPathStore) LoadProposal(ctx context.Context, tenantID, proposalID string) (ProposalPin, error) {
	if err := ctx.Err(); err != nil {
		return ProposalPin{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	pin, ok := s.proposals[tenantID][proposalID]
	if !ok {
		return ProposalPin{}, fmt.Errorf("%w: proposal %q", ErrPathNotFound, proposalID)
	}
	return pin, nil
}
