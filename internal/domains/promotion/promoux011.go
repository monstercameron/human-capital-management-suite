package promotion

// PROMOUX-011: "Invalidate promotion counts and timestamps after every
// durable transition."
//
// RED: after creating or approving a promotion the shell still announces
// zero journeys, `Updated` remains older than the newest transition, or
// views converge only after unrelated navigation.
//
// GREEN: each committed promotion transition emits one authority-filtered
// invalidation with sequence; shell count, Journeys, My Work, Person and
// detail refresh only their affected regions; displayed last-updated time
// equals the latest business transition; reconnect catch-up converges
// without duplicate renders or lost scroll/focus.
//
// # The clause this file exists to get right: sequence numbers and the
// authority filter interact
//
// tools/uxqual/invalidation's client (processing.go's handle) admits any
// message whose SourceSequence is strictly greater than the last one it
// admitted -- it does not require the next number to be exactly one more.
// That is correct for its own contract (a client should not have to reject a
// legitimate jump forward), but it means whatever mints the SourceSequence
// value decides what a viewer can infer from the numbers they see. If that
// value were the shared, per-tenant commit position -- the same number for
// every subscriber -- then a viewer authorized for transitions 1, 2 and 4 but
// not 3 would receive messages numbered 1, 2, 4. Nothing about the payload
// would name transition 3, but the gap itself would: the viewer can count
// promotions they have no authority over and infer activity from the holes.
// A filter that removes content but leaves a hole in the numbering is not a
// filter.
//
// The scheme chosen here is gap-free renumbering per subscriber, not a
// per-viewer sequence space computed from the global position. Concretely:
// [SubscriberSequencer] never reads or forwards the tenant-wide commit
// position produced by internal/data/promotioninvalidation.NextSequence (that
// value is used only as an invalidation item's Revision, protecting against
// stale or reordered application -- see that package's doc comment). Instead
// it hands each subscriber its own private counter that advances by exactly
// one, and only, when a message is actually about to be delivered to that
// subscriber. A transition that authorizes zero items for a given subscriber
// never touches that subscriber's counter at all, so the numbers that
// subscriber ever sees are contiguous by construction: there is no
// arithmetic a viewer can perform on 1, 2, 3, ... that reveals how many
// transitions were filtered out between any two of them, because none were
// skipped in their own numbering -- they were never numbered in the first
// place. TestTodo_PROMOUX_011 proves this by contrasting the two schemes
// directly: feeding the same three transitions (with the middle one denied)
// through the naive shared-position scheme reproduces the gap, and through
// [SubscriberSequencer] does not.
//
// The alternative considered and rejected: a single per-viewer sequence
// space seeded from the global position (e.g. "skip publishing the number,
// but still reserve it for this viewer"). That scheme still requires the
// viewer's client to be told the gap is expected -- which is exactly the
// side channel this clause exists to close -- or it requires the server to
// track, per viewer, exactly which global positions that viewer has been
// told about, which is strictly more state than a private monotonically
// increasing counter for no additional guarantee. Gap-free per-subscriber
// renumbering gives the same client-side contract (strictly increasing,
// no client-visible gap ever) with less server state and no risk of a
// half-applied "reserve but don't disclose" bookkeeping bug.
import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Region is one of the exactly five independently invalidated presentation
// areas GREEN names. It is a closed set: [Region.Projection] and
// [Transition.RegionSubject] switch over every member explicitly and refuse
// an unrecognized value rather than falling through to a default, so adding
// a sixth region without updating both call sites fails loudly instead of
// silently reusing another region's wiring.
type Region string

const (
	// RegionShellCount is the application shell's promotion/journey count
	// badge: it changes when the number of the viewer's actionable or
	// tracked promotions changes, never when unrelated journey detail does.
	RegionShellCount Region = "SHELL_COUNT"
	// RegionJourneys is the Journeys list page.
	RegionJourneys Region = "JOURNEYS"
	// RegionMyWork is the My Work list of assigned decisions and tasks.
	RegionMyWork Region = "MY_WORK"
	// RegionPerson is one worker's Person page summary of active and past
	// promotions.
	RegionPerson Region = "PERSON"
	// RegionDetail is one promotion journey's own detail page.
	RegionDetail Region = "DETAIL"
)

// Regions returns every region in a stable order, freshly copied on every
// call. It is the single source callers use to iterate "every region" rather
// than re-listing the five constants, so a region added to the const block
// without being added here is caught by TestTodo_PROMOUX_011's
// exhaustiveness table rather than silently skipped by every future caller
// that iterates this slice.
func Regions() []Region {
	return []Region{RegionShellCount, RegionJourneys, RegionMyWork, RegionPerson, RegionDetail}
}

// ErrUnknownRegion means a Region value outside the five declared constants
// reached a function that must handle every region explicitly.
var ErrUnknownRegion = errors.New("promotion: unknown invalidation region")

// Projection returns the wire projection name a region's invalidation
// subscribers filter on (tools/uxqual/invalidation.Scope.Projection). Each
// region gets its own name so that a message meant for one region's
// subscribers is never accidentally admitted by another region's client:
// tools/uxqual/invalidation's handle() rejects any message whose Projection
// does not equal the client's own scope, which is the entire mechanism
// REFACTOR's "no page triggers a full-shell reload" and RED's "refresh only
// their affected regions" rely on -- this file adds no new filtering logic
// on top of it.
func (r Region) Projection() (string, error) {
	switch r {
	case RegionShellCount:
		return "promotion_shell_count", nil
	case RegionJourneys:
		return "promotion_journeys", nil
	case RegionMyWork:
		return "promotion_my_work", nil
	case RegionPerson:
		return "promotion_person", nil
	case RegionDetail:
		return "promotion_detail", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownRegion, r)
	}
}

// ErrInvalidTransition means a Transition is missing a required field or
// carries an invalid one. A zero-value Transition always fails this check:
// an all-defaults transition (no tenant, no timestamp, no revision) must
// never be treated as a legitimate "nothing changed" event.
var ErrInvalidTransition = errors.New("promotion: invalid transition")

// Transition is one durable promotion workflow transition the workflow
// engine has already committed. It is the only input this file's
// invalidation mapping trusts.
//
// OccurredAt is the transition's own business timestamp -- when the
// workflow actually recorded the transition -- and is never derived from
// time.Now() by any function in this file. That is what lets
// TransitionLastUpdated (internal/humanwork/productui) prove displayed
// "Updated" equals the transition's own timestamp rather than "roughly now".
type Transition struct {
	Tenant     values.TenantId
	JourneyRef values.EntityRef
	WorkerRef  values.EntityRef
	OccurredAt time.Time
	// Revision is the new value of the affected records' revision counter,
	// used as the emitted InvalidationItem's Revision. It must be the
	// transition's own durable ordering position (internal/data/
	// promotioninvalidation.NextSequence is the production source), never a
	// value derived from OccurredAt or from a subscriber-local counter.
	Revision uint64
}

// Validate reports whether t is complete enough to invalidate anything. A
// zero-value Transition fails every one of these checks, so "no transition
// was actually supplied" can never be mistaken for "a transition happened
// with default values" -- the fail-closed shape the todo's proof standards
// require of any type whose zero value could otherwise be read as
// permissive.
func (t Transition) Validate() error {
	if err := t.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidTransition, err)
	}
	if err := t.JourneyRef.Validate(); err != nil || t.JourneyRef.Tenant != t.Tenant {
		return fmt.Errorf("%w: journey reference", ErrInvalidTransition)
	}
	if err := t.WorkerRef.Validate(); err != nil || t.WorkerRef.Tenant != t.Tenant {
		return fmt.Errorf("%w: worker reference", ErrInvalidTransition)
	}
	if t.OccurredAt.IsZero() {
		return fmt.Errorf("%w: occurred_at is unset", ErrInvalidTransition)
	}
	if t.Revision == 0 {
		return fmt.Errorf("%w: revision is unset", ErrInvalidTransition)
	}
	return nil
}

// RegionSubject returns the entity reference a region's invalidation
// targets for this transition. Journeys and My Work both key off the
// journey (a list entry changing), Person keys off the worker, Detail keys
// off the journey's own detail record, and Shell Count keys off one
// synthetic, deterministic per-tenant subject (see [ShellCountSubject])
// rather than any real business entity, because the shell badge has no
// individual record of its own to be invalidated -- it is a computed
// aggregate whose only "identity" is the tenant it summarizes.
func (t Transition) RegionSubject(region Region) (values.EntityRef, error) {
	switch region {
	case RegionShellCount:
		return ShellCountSubject(t.Tenant), nil
	case RegionJourneys, RegionMyWork:
		return t.JourneyRef, nil
	case RegionPerson:
		return t.WorkerRef, nil
	case RegionDetail:
		return t.JourneyRef, nil
	default:
		return values.EntityRef{}, fmt.Errorf("%w: %q", ErrUnknownRegion, region)
	}
}

// ShellCountSubject is the deterministic, tenant-scoped synthetic subject
// the shell count region invalidates. It is derived rather than randomly
// minted so that every emitter for one tenant agrees on the same subject
// without having to share mutable state, and it is a version-5 UUID (not an
// arbitrary string) because values.EntityId requires an opaque id shaped
// like a UUID or ULID.
func ShellCountSubject(tenant values.TenantId) values.EntityRef {
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("promotion-shell-count:"+string(tenant))).String()
	return values.EntityRef{Tenant: tenant, Kind: values.Kind("promotion_shell_count"), Id: id}
}

// RegionCacheKey is the one cache-key format every region consumer shares
// (REFACTOR: "consumers share cache keys and sequence handling"). Two
// independent call sites deriving a key for the same (region, subject) must
// always agree, which TestTodo_PROMOUX_011 proves by calling this from two
// different fixtures and comparing the result.
func RegionCacheKey(region Region, subject values.EntityRef) string {
	return string(region) + "\x00" + subject.String()
}

// RegionCursor is a consumer's last-known delivered position for one region.
// The zero value (Known: false) means "this consumer has never received an
// authoritative baseline for this region" and must never be treated as
// "caught up to sequence 0": [NeedsRefresh] fails closed on it regardless of
// the incoming sequence, including an incoming sequence of 0.
type RegionCursor struct {
	Known    bool
	Sequence uint64
}

// NeedsRefresh reports whether a newly observed sequence number for this
// region should cause a refresh. An unknown cursor always needs a refresh:
// a consumer that has never recorded a position has no basis for concluding
// it is already current, so the zero value here means "stale", not
// "permissive".
func (c RegionCursor) NeedsRefresh(incoming uint64) bool {
	if !c.Known {
		return true
	}
	return incoming > c.Sequence
}

// Advance returns the cursor after observing incoming, refusing to move
// backwards or to accept a zero incoming sequence as meaningful progress:
// an invalidation with no sequence must never be read as "current".
func (c RegionCursor) Advance(incoming uint64) (RegionCursor, error) {
	if incoming == 0 {
		return c, fmt.Errorf("%w: incoming sequence is zero", ErrInvalidTransition)
	}
	if c.Known && incoming <= c.Sequence {
		return c, fmt.Errorf("%w: incoming sequence %d does not advance past %d", ErrInvalidTransition, incoming, c.Sequence)
	}
	return RegionCursor{Known: true, Sequence: incoming}, nil
}

// SubscriberSequencer mints each subscriber's own gap-free, contiguous
// delivery sequence. See the file doc comment for the full disclosure
// argument this type exists to close.
//
// It holds only bounded, per-process, in-memory state -- one uint64 per
// (subscriber, projection) pair that has ever received a message -- and is
// safe for concurrent use by multiple goroutines emitting to different or
// the same subscribers.
type SubscriberSequencer struct {
	mu   sync.Mutex
	next map[string]uint64
}

// NewSubscriberSequencer returns an empty sequencer. A fresh sequencer (one
// per live connection, in production use) starts every subscriber's counter
// at zero known state, matching a freshly (re)established subscription that
// has not yet been told the current baseline by its own authoritative load.
func NewSubscriberSequencer() *SubscriberSequencer {
	return &SubscriberSequencer{next: make(map[string]uint64)}
}

// Advance mints the next private sequence only after the caller has admitted
// an item for this subscriber. The transport owns authorization and message
// assembly; this domain type never imports the transport layer.
func (s *SubscriberSequencer) Advance(subscriberKey string, region Region) (local, watermark uint64, err error) {
	if s == nil {
		return 0, 0, fmt.Errorf("%w: nil sequencer", ErrInvalidTransition)
	}
	if subscriberKey == "" {
		return 0, 0, fmt.Errorf("%w: empty subscriber key", ErrInvalidTransition)
	}
	projection, err := region.Projection()
	if err != nil {
		return 0, 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	key := subscriberKey + "\x00" + projection
	watermark = s.next[key]
	local = watermark + 1
	s.next[key] = local
	return local, watermark, nil
}

// Snapshot returns the current per-(subscriber, projection) local sequence
// state, keyed by "subscriberKey\x00projection". It exists for tests and
// observability only; production delivery never needs to read it back.
func (s *SubscriberSequencer) Snapshot() map[string]uint64 {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]uint64, len(s.next))
	for k, v := range s.next {
		out[k] = v
	}
	return out
}
