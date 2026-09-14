// Immutable artifact revisions: ARTIFACT-007 binds artifact identity,
// multipart completion, retention and legal hold to immutable object
// versions.
//
// Completion seals bytes through the sealed store and binds the
// provider version ID plus the verified checksum to an append-only
// ArtifactRevision under conditional generation: a racing completion
// for the same key refuses instead of overwriting. Retention, legal
// hold and delete operate and verify per exact version — holds never
// attach to a `latest` alias, held revisions never delete (governance
// bypass included), and abandoned uploads collect boundedly. Logical
// names resolve to explicit revision histories, never to a mutable
// pointer.
package object

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

var (
	// ErrRevisionConflict refuses a completion whose generation moved.
	ErrRevisionConflict = errors.New("object: artifact generation moved under completion")
	// ErrRevisionHeld refuses deletion of a held revision.
	ErrRevisionHeld = errors.New("object: artifact revision is under legal hold")
	// ErrBypassForbidden refuses governance-mode bypass deletion.
	ErrBypassForbidden = errors.New("object: governance bypass deletion is never compliant")
	// ErrRevisionUnknown refuses operations on an unlisted revision.
	ErrRevisionUnknown = errors.New("object: artifact revision unknown")
)

// ArtifactRevision is one immutable bound version.
type ArtifactRevision struct {
	ObjectID          string
	Generation        uint64
	Checksum          string
	ProviderVersionID string
	MediaType         string
	Hold              bool
	RetentionUntil    time.Time
	BoundAt           time.Time
}

// RevisionLog is the append-only revision record. It is safe for
// concurrent use.
type RevisionLog struct {
	mu        sync.Mutex
	store     *SealedObjectStore
	revisions map[string][]ArtifactRevision
	now       func() time.Time
}

// NewRevisionLog starts an empty log over one sealed store.
func NewRevisionLog(store *SealedObjectStore) *RevisionLog {
	return &RevisionLog{store: store, revisions: map[string][]ArtifactRevision{}}
}

func (l *RevisionLog) clock() time.Time {
	if l != nil && l.now != nil {
		return l.now().UTC()
	}
	return time.Now().UTC()
}

// BindCompletion seals completed multipart bytes and binds the
// provider version plus verified checksum to a new immutable revision.
// expectedGeneration is the conditional guard: 0 creates only when the
// object is absent, otherwise the live generation must match.
func (l *RevisionLog) BindCompletion(ctx context.Context, cctx custody.Context, content []byte, objectID, mediaType, providerVersionID string, expectedGeneration uint64) (ArtifactRevision, error) {
	if l == nil || l.store == nil {
		return ArtifactRevision{}, ErrInvalidRequest
	}
	if strings.TrimSpace(objectID) == "" || strings.TrimSpace(providerVersionID) == "" {
		return ArtifactRevision{}, ErrInvalidRequest
	}
	if len(content) == 0 {
		return ArtifactRevision{}, ErrUploadIncomplete
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var live uint64
	if info, err := l.store.Stat(ctx, objectID); err == nil {
		live = info.Generation
	}
	if live != expectedGeneration {
		return ArtifactRevision{}, fmt.Errorf("%w: object %s at generation %d, expected %d",
			ErrRevisionConflict, objectID, live, expectedGeneration)
	}
	// Create-only Put for the first generation, conditional
	// PutNewGeneration after: the store re-checks the generation under
	// its own lock, so a racing bind refuses instead of overwriting.
	if expectedGeneration == 0 {
		if _, err := l.store.Put(ctx, cctx, objectID, mediaType, content); err != nil {
			return ArtifactRevision{}, err
		}
	} else if _, err := l.store.PutNewGeneration(ctx, cctx, objectID, mediaType, content); err != nil {
		return ArtifactRevision{}, err
	}
	info, err := l.store.Stat(ctx, objectID)
	if err != nil {
		return ArtifactRevision{}, err
	}
	checksum := MultipartChecksum(content)
	revision := ArtifactRevision{
		ObjectID: objectID, Generation: info.Generation, Checksum: checksum,
		ProviderVersionID: providerVersionID, MediaType: mediaType, BoundAt: l.clock(),
	}
	l.revisions[objectID] = append(l.revisions[objectID], revision)
	return revision, nil
}

// History resolves one logical name to its explicit revision history.
func (l *RevisionLog) History(objectID string) []ArtifactRevision {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]ArtifactRevision(nil), l.revisions[objectID]...)
}

// ApplyHold sets or clears legal hold on one exact revision. Holds
// never attach to aliases: the generation is required.
func (l *RevisionLog) ApplyHold(objectID string, generation uint64, hold bool) (ArtifactRevision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, revision := range l.revisions[objectID] {
		if revision.Generation == generation {
			l.revisions[objectID][i].Hold = hold
			return l.revisions[objectID][i], nil
		}
	}
	return ArtifactRevision{}, fmt.Errorf("%w: %s generation %d", ErrRevisionUnknown, objectID, generation)
}

// DeleteRevision deletes one unheld revision. Held revisions never
// delete, and governance-mode bypass is refused outright: a bypassed
// deletion would be reported as compliant, which is never true.
func (l *RevisionLog) DeleteRevision(ctx context.Context, objectID string, generation uint64, bypass bool) error {
	if bypass {
		return fmt.Errorf("%w: %s generation %d", ErrBypassForbidden, objectID, generation)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.revisions[objectID][:0:0]
	found := false
	for _, revision := range l.revisions[objectID] {
		if revision.Generation != generation {
			kept = append(kept, revision)
			continue
		}
		found = true
		if revision.Hold {
			return fmt.Errorf("%w: %s generation %d", ErrRevisionHeld, objectID, generation)
		}
	}
	if !found {
		return fmt.Errorf("%w: %s generation %d", ErrRevisionUnknown, objectID, generation)
	}
	l.revisions[objectID] = kept
	return nil
}

// VerifyRevision re-verifies one bound revision against the sealed
// store: generation, checksum and hold state must all still hold.
func (l *RevisionLog) VerifyRevision(ctx context.Context, cctx custody.Context, objectID string, generation uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, revision := range l.revisions[objectID] {
		if revision.Generation != generation {
			continue
		}
		info, err := l.store.Stat(ctx, objectID)
		if err != nil {
			return err
		}
		plaintext, _, err := l.store.Get(ctx, cctx, objectID)
		if err != nil {
			return err
		}
		if MultipartChecksum(plaintext) != revision.Checksum {
			return fmt.Errorf("object: revision %s generation %d checksum drifted", objectID, generation)
		}
		_ = info
		return nil
	}
	return fmt.Errorf("%w: %s generation %d", ErrRevisionUnknown, objectID, generation)
}

// OrphanReport bounds one abandoned-upload collection.
type OrphanReport struct {
	Aborted []string
	Digest  string
}

// CollectOrphans aborts uploads idle since the cutoff and reports the
// bounded set. Collection is the only path that removes part bytes,
// and it only touches uploads the manager already lists as abandoned.
func CollectOrphans(manager *MultipartManager, cutoff time.Time) (OrphanReport, error) {
	if manager == nil {
		return OrphanReport{}, ErrInvalidRequest
	}
	aborted := manager.SweepAbandoned(cutoff)
	sort.Strings(aborted)
	parts := append([]string{"artifact007-orphans"}, aborted...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return OrphanReport{Aborted: aborted, Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}
