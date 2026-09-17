package productdurability

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ALIGN-040: projections rebuild from the authoritative chronology. The
// journal is the source of truth; a projection is a fold over one tenant
// stream. Rebuild replays the stream in order and must arrive at exactly
// the digest the live projection holds — otherwise the projection is not a
// function of its chronology and cannot be trusted after a restart.

// Rebuild errors.
var (
	ErrRebuildInvalid = errors.New("productdurability: rebuild input is invalid")
	ErrRebuildGap     = errors.New("productdurability: chronology has a sequence gap")
)

// ChronologyProjection is a projection folded from one tenant stream.
type ChronologyProjection struct {
	Tenant         values.TenantId `json:"tenant"`
	Stream         string          `json:"stream"`
	AppliedThrough uint64          `json:"applied_through"`
	Entries        int             `json:"entries"`
	Digest         string          `json:"digest"`
}

func foldEntry(running, tenant, stream string, entry HistoryEntry) string {
	raw := strings.Join([]string{running, tenant, stream, fmt.Sprint(entry.Sequence), entry.PayloadDigest}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Rebuild folds one tenant stream from its chronology entries. Entries
// must be in sequence order starting at 1 with no gaps, and every entry
// must belong to the named tenant stream. The result digest is fully
// determined by the chronology: the same history always rebuilds the same
// projection.
func Rebuild(tenant values.TenantId, stream string, entries []HistoryEntry) (ChronologyProjection, error) {
	if err := tenant.Validate(); err != nil {
		return ChronologyProjection{}, fmt.Errorf("%w: tenant: %v", ErrRebuildInvalid, err)
	}
	if strings.TrimSpace(stream) == "" {
		return ChronologyProjection{}, fmt.Errorf("%w: stream is required", ErrRebuildInvalid)
	}
	if len(entries) == 0 {
		return ChronologyProjection{}, fmt.Errorf("%w: no chronology entries", ErrRebuildInvalid)
	}
	projection := ChronologyProjection{Tenant: tenant, Stream: stream}
	running := "sha256:genesis"
	for i, entry := range entries {
		if entry.Tenant != tenant || entry.Stream != stream {
			return ChronologyProjection{}, fmt.Errorf("%w: entry %d is outside %s", ErrRebuildInvalid, i, stream)
		}
		if entry.Sequence != uint64(i+1) {
			return ChronologyProjection{}, fmt.Errorf("%w: position %d carries sequence %d", ErrRebuildGap, i, entry.Sequence)
		}
		if entry.Digest == "" || entry.Digest != entry.computeDigest() {
			return ChronologyProjection{}, fmt.Errorf("%w: entry %d digest does not match", ErrRebuildInvalid, entry.Sequence)
		}
		running = foldEntry(running, tenant.String(), stream, entry)
		projection.AppliedThrough = entry.Sequence
	}
	projection.Entries = len(entries)
	projection.Digest = running
	return projection, nil
}

// Projector folds entries incrementally, the way a live projection
// follows its journal. Rebuild(journal) must equal the Projector state
// after the same appends.
type Projector struct {
	Tenant     values.TenantId
	Stream     string
	projection ChronologyProjection
	running    string
}

// NewProjector starts a live projection over one tenant stream.
func NewProjector(tenant values.TenantId, stream string) *Projector {
	return &Projector{
		Tenant: tenant, Stream: stream,
		projection: ChronologyProjection{Tenant: tenant, Stream: stream},
		running:    "sha256:genesis",
	}
}

// Apply folds one journal entry. Entries must arrive in order with no
// gaps, from the projector's own tenant stream.
func (p *Projector) Apply(entry HistoryEntry) error {
	if entry.Tenant != p.Tenant || entry.Stream != p.Stream {
		return fmt.Errorf("%w: entry is outside %s", ErrRebuildInvalid, p.Stream)
	}
	if entry.Sequence != p.projection.AppliedThrough+1 {
		return fmt.Errorf("%w: live position %d, entry sequence %d", ErrRebuildGap, p.projection.AppliedThrough, entry.Sequence)
	}
	p.running = foldEntry(p.running, p.Tenant.String(), p.Stream, entry)
	p.projection.AppliedThrough = entry.Sequence
	p.projection.Entries++
	p.projection.Digest = p.running
	return nil
}

// State returns the live projection.
func (p *Projector) State() ChronologyProjection {
	return p.projection
}
