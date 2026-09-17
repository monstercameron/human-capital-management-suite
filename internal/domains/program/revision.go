// PROGRAM-002: the effective ProgramRevision resolver. Exactly one
// revision governs a tenant/org/jurisdiction/effective-known context;
// overlapping or ambiguous revisions and ambient-latest fallbacks fail.
package program

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrInvalidRevision     = errors.New("program: invalid program revision")
	ErrOverlappingRevision = errors.New("program: revision overlaps a recorded interval")
	ErrNoEffectiveRevision = errors.New("program: no effective revision")
	ErrAmbiguousRevision   = errors.New("program: ambiguous effective revision")
	ErrRevisionStore       = errors.New("program: revision store failure")
)

// Revision is one immutable effective revision of a program definition.
type Revision struct {
	ProgramID    string
	Version      uint64
	Tenant       string
	Org          string
	Jurisdiction string
	From         time.Time
	To           time.Time
	Digest       string
}

// ResolveContext is the exact context a revision resolves for.
type ResolveContext struct {
	Tenant       string
	Org          string
	Jurisdiction string
	At           time.Time
}

// RevisionStore is the revision backing port. The catalog ships an
// in-memory store; fault tests inject failures behind this interface.
type RevisionStore interface {
	List(programID string) ([]Revision, error)
	Append(rev Revision) error
}

type memoryRevisionStore struct {
	records []Revision
}

func (s *memoryRevisionStore) List(programID string) ([]Revision, error) {
	out := []Revision{}
	for _, r := range s.records {
		if programID == "" || r.ProgramID == programID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *memoryRevisionStore) Append(rev Revision) error {
	s.records = append(s.records, rev)
	return nil
}

// failStore is the fault-injection store: every operation fails.
type failStore struct{}

func (failStore) List(string) ([]Revision, error) { return nil, errors.New("store unavailable") }
func (failStore) Append(Revision) error           { return errors.New("store unavailable") }

// SetRevisionStore swaps the revision backing port.
func (c *Catalog) SetRevisionStore(s RevisionStore) {
	c.store = s
}

func revisionDigest(rev Revision) (string, error) {
	w := canonicalbytes.New("program-revision", 1)
	w.String("program_id", rev.ProgramID)
	w.Int("version", int64(rev.Version))
	w.String("tenant", rev.Tenant)
	w.String("org", rev.Org)
	w.String("jurisdiction", rev.Jurisdiction)
	w.String("from", rev.From.UTC().Format(time.RFC3339))
	w.String("to", rev.To.UTC().Format(time.RFC3339))
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// ValidateRevision checks a revision without recording it.
func ValidateRevision(rev Revision) error {
	if strings.TrimSpace(rev.ProgramID) == "" || rev.Version == 0 ||
		strings.TrimSpace(rev.Tenant) == "" || strings.TrimSpace(rev.Org) == "" ||
		strings.TrimSpace(rev.Jurisdiction) == "" {
		return fmt.Errorf("%w: program, version, tenant, org and jurisdiction are required", ErrInvalidRevision)
	}
	if rev.From.IsZero() || rev.To.IsZero() {
		return fmt.Errorf("%w: effective interval must be closed", ErrInvalidRevision)
	}
	if !rev.To.After(rev.From) {
		return fmt.Errorf("%w: effective interval is inverted", ErrInvalidRevision)
	}
	return nil
}

func intervalsOverlap(aFrom, aTo, bFrom, bTo time.Time) bool {
	return aFrom.Before(bTo) && bFrom.Before(aTo)
}

// AppendRevision records an immutable revision. Same-scope overlaps and
// caller-supplied digests that do not reseal are refused.
func (c *Catalog) AppendRevision(caller Caller, rev Revision) (Revision, error) {
	if err := ValidateRevision(rev); err != nil {
		return Revision{}, err
	}
	if err := c.authorize(caller, rev.Tenant); err != nil {
		return Revision{}, err
	}
	existing, err := c.store.List(rev.ProgramID)
	if err != nil {
		return Revision{}, fmt.Errorf("%w: %v", ErrRevisionStore, err)
	}
	for _, e := range existing {
		if e.Tenant == rev.Tenant && e.Org == rev.Org && e.Jurisdiction == rev.Jurisdiction &&
			intervalsOverlap(e.From, e.To, rev.From, rev.To) {
			return Revision{}, fmt.Errorf("%w: version %d", ErrOverlappingRevision, e.Version)
		}
		if e.Version == rev.Version {
			return Revision{}, fmt.Errorf("%w: version %d", ErrOverlappingRevision, e.Version)
		}
	}
	supplied := rev.Digest
	rev.Digest = ""
	digest, err := revisionDigest(rev)
	if err != nil {
		return Revision{}, err
	}
	if supplied != "" && supplied != digest {
		return Revision{}, fmt.Errorf("%w: digest does not reseal", ErrInvalidRevision)
	}
	rev.Digest = digest
	if err := c.store.Append(rev); err != nil {
		return Revision{}, fmt.Errorf("%w: %v", ErrRevisionStore, err)
	}
	c.revisions = append(c.revisions, rev)
	c.revDigests[rev.Digest] = true
	c.journal = append(c.journal, JournalEntry{
		Op: "append-revision", Ref: rev.ProgramID,
		Detail: fmt.Sprintf("version=%d tenant=%s org=%s jurisdiction=%s", rev.Version, rev.Tenant, rev.Org, rev.Jurisdiction),
	})
	return rev, nil
}

// ResolveRevision returns the exact revision for ctx. Zero or ambiguous
// matches fail: there is no ambient latest.
func (c *Catalog) ResolveRevision(ctx ResolveContext) (Revision, error) {
	if strings.TrimSpace(ctx.Tenant) == "" || strings.TrimSpace(ctx.Org) == "" ||
		strings.TrimSpace(ctx.Jurisdiction) == "" || ctx.At.IsZero() {
		return Revision{}, fmt.Errorf("%w: tenant, org, jurisdiction and instant are required", ErrInvalidRevision)
	}
	all, err := c.store.List("")
	if err != nil {
		return Revision{}, fmt.Errorf("%w: %v", ErrRevisionStore, err)
	}
	matches := []Revision{}
	for _, rev := range all {
		if rev.Tenant == ctx.Tenant && rev.Org == ctx.Org && rev.Jurisdiction == ctx.Jurisdiction &&
			!ctx.At.Before(rev.From) && ctx.At.Before(rev.To) {
			matches = append(matches, rev)
		}
	}
	switch len(matches) {
	case 0:
		return Revision{}, fmt.Errorf("%w: %s/%s/%s at %s",
			ErrNoEffectiveRevision, ctx.Tenant, ctx.Org, ctx.Jurisdiction, ctx.At.UTC().Format(time.RFC3339))
	case 1:
		return matches[0], nil
	default:
		return Revision{}, fmt.Errorf("%w: %d revisions cover %s",
			ErrAmbiguousRevision, len(matches), ctx.At.UTC().Format(time.RFC3339))
	}
}
