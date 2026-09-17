// Package releaseevidence records version-bound product-slice release
// evidence (ALIGN-054) and detects binary, migration and definition skew
// against it (ALIGN-055).
//
// A product slice is released as one immutable record binding the slice
// version to exactly what shipped: the binary revision, the schema artifact
// digest and target migration version, the digest of every workflow and
// intent definition the slice runs, and the evidence refs of the tests that
// admitted it. A record is sealed by its own digest; re-recording the same
// slice version with different content is refused, so release evidence can
// never be rewritten after the fact.
package releaseevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sentinels.
var (
	ErrInvalid   = errors.New("releaseevidence: invalid release record")
	ErrImmutable = errors.New("releaseevidence: release version already recorded with different content")
	ErrNotFound  = errors.New("releaseevidence: no release recorded")
)

// Release is one product-slice release.
type Release struct {
	Slice          string            `json:"slice"`
	Version        string            `json:"version"`
	BinaryRevision string            `json:"binary_revision"`
	SchemaDigest   string            `json:"schema_digest"`
	SchemaVersion  int64             `json:"schema_version"`
	Definitions    map[string]string `json:"definitions"`
	EvidenceRefs   []string          `json:"evidence_refs"`
	RecordedBy     string            `json:"recorded_by"`
	RecordedAt     time.Time         `json:"recorded_at"`
	Digest         string            `json:"digest"`
}

func (r Release) validate() error {
	for name, v := range map[string]string{
		"slice": r.Slice, "version": r.Version, "binary revision": r.BinaryRevision,
		"schema digest": r.SchemaDigest, "recorded by": r.RecordedBy,
	} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalid, name)
		}
	}
	switch {
	case r.SchemaVersion <= 0:
		return fmt.Errorf("%w: schema version must be positive", ErrInvalid)
	case len(r.Definitions) == 0:
		return fmt.Errorf("%w: a release binds at least one definition digest", ErrInvalid)
	case len(r.EvidenceRefs) == 0:
		return fmt.Errorf("%w: a release cites the evidence that admitted it", ErrInvalid)
	case r.RecordedAt.IsZero():
		return fmt.Errorf("%w: recorded_at is required", ErrInvalid)
	}
	for name, digest := range r.Definitions {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(digest) == "" {
			return fmt.Errorf("%w: definition %q has no digest", ErrInvalid, name)
		}
	}
	return nil
}

func (r Release) canonical() Release {
	c := r
	c.Definitions = maps.Clone(r.Definitions)
	c.EvidenceRefs = slices.Clone(r.EvidenceRefs)
	sort.Strings(c.EvidenceRefs)
	c.RecordedAt = r.RecordedAt.UTC()
	c.Digest = ""
	return c
}

// Seal returns the release with its content digest.
func (r Release) Seal() (Release, error) {
	if err := r.validate(); err != nil {
		return Release{}, err
	}
	c := r.canonical()
	body, _ := json.Marshal(c)
	sum := sha256.Sum256(body)
	c.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return c, nil
}

// Verify reports whether the release still matches its digest.
func (r Release) Verify() error {
	sealed, err := r.Seal()
	if err != nil {
		return err
	}
	if sealed.Digest != r.Digest {
		return fmt.Errorf("%w: digest does not match content", ErrInvalid)
	}
	return nil
}

// Journal is the append-only release log.
type Journal struct {
	mu       sync.Mutex
	releases map[string]Release
	order    []string
}

// NewJournal returns an empty journal.
func NewJournal() *Journal { return &Journal{releases: map[string]Release{}} }

// Record seals and appends a release. Recording the identical release again
// is a no-op returning the stored record; different content under the same
// slice version is refused.
func (j *Journal) Record(r Release) (Release, error) {
	sealed, err := r.Seal()
	if err != nil {
		return Release{}, err
	}
	key := sealed.Slice + "\x1f" + sealed.Version
	j.mu.Lock()
	defer j.mu.Unlock()
	if prior, ok := j.releases[key]; ok {
		if prior.Digest != sealed.Digest {
			return Release{}, fmt.Errorf("%w: %s %s", ErrImmutable, sealed.Slice, sealed.Version)
		}
		return prior, nil
	}
	j.releases[key] = sealed
	j.order = append(j.order, key)
	return sealed, nil
}

// Get returns the recorded release for one slice version.
func (j *Journal) Get(slice, version string) (Release, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.releases[slice+"\x1f"+version]
	if !ok {
		return Release{}, fmt.Errorf("%w: %s %s", ErrNotFound, slice, version)
	}
	c := r.canonical()
	c.Digest = r.Digest
	return c, nil
}

// Latest returns the most recently recorded release of a slice.
func (j *Journal) Latest(slice string) (Release, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.order) - 1; i >= 0; i-- {
		if r := j.releases[j.order[i]]; r.Slice == slice {
			c := r.canonical()
			c.Digest = r.Digest
			return c, nil
		}
	}
	return Release{}, ErrNotFound
}
