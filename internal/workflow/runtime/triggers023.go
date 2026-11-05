// Trigger registry and open-obligation tracker (WF-EXT-023): runs start
// from dates, schedules, typed events and post-completion obligations,
// not only from intent proposals.
//
// A [TriggerSource] is a registry entry naming the workflow and typed
// intent a fire starts. [TriggerRegistry.Sweep] fires due date and
// schedule occurrences, [TriggerRegistry.FireEvent] fires typed-event
// matches, and [ObligationTracker] carries the typed open obligations
// an END step emits until a sweep drains them into follow-up runs.
//
// Every fire carries an occurrence key (source + occurrence identity)
// and fires at most once per key: the registry records each fired key,
// so a repeated sweep, a redelivered event or a re-drained obligation
// is a no-op. The fired key is also the follow-up run's
// [StartRequest.StartIdempotencyKey], so [derivedStartInstanceID] maps
// a retried fire to the same instance. Recovery falls out of the same
// design: [TriggerRegistry.Sweep] fires every occurrence after the
// last sweep watermark, so missed triggers fire after downtime.
//
// The registry is in-memory. The scheduler persists the source rows,
// the fired keys and the sweep watermark; this package only computes
// which occurrences fire. Like the rest of this package, nothing here
// reads a wall clock: every method takes its now.
//
// Schedule sources are anchor-plus-interval expressions. WF-EXT-012's
// unified time-expression model supersedes them when it lands; the
// occurrence-key contract survives that migration unchanged.
package runtime

import (
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CodeDuplicateTrigger reports a trigger source id, event redelivery key
// or obligation key that is already registered.
const CodeDuplicateTrigger = "DUPLICATE_TRIGGER"

// MaxTriggerCatchUp bounds one sweep's schedule catch-up so a trigger
// whose anchor is years in the past cannot wedge the sweep. Remaining
// occurrences fire on later sweeps.
const MaxTriggerCatchUp = 1024

// TriggerSourceKind names the four trigger source families.
type TriggerSourceKind string

const (
	// TriggerSourceDate fires once at an absolute time.
	TriggerSourceDate TriggerSourceKind = "DATE"
	// TriggerSourceSchedule fires at anchor plus every positive
	// multiple of its interval.
	TriggerSourceSchedule TriggerSourceKind = "SCHEDULE"
	// TriggerSourceEvent fires when a typed event arrives.
	TriggerSourceEvent TriggerSourceKind = "EVENT"
	// TriggerSourceObligation fires when the obligation tracker
	// holds an open obligation of its type.
	TriggerSourceObligation TriggerSourceKind = "OBLIGATION"
)

// TriggerSource is one registry entry: the durable cause of every run
// its fires start. Exactly one of At, Every/Anchor, EventType or
// ObligationType must be set, matching Kind.
type TriggerSource struct {
	ID             string
	Kind           TriggerSourceKind
	WorkflowID     string
	IntentType     string
	TenantID       uuid.UUID
	At             time.Time
	Anchor         time.Time
	Every          time.Duration
	EventType      string
	ObligationType string
}

// TriggerFire is one occurrence that fired: the authority to start one
// follow-up run. Key is the at-most-once occurrence identity and the
// run's start idempotency key; FiredAt is the occurrence time, never
// the sweep time.
type TriggerFire struct {
	SourceID   string
	Kind       TriggerSourceKind
	Key        string
	WorkflowID string
	IntentType string
	Facts      map[string]string
	FiredAt    time.Time
}

// StartSource converts the fire to the TRIGGER start cause the run
// starts under. It always validates: Register required every field
// [StartSource.validate] needs.
func (f TriggerFire) StartSource() StartSource {
	return StartSource{
		Kind:       StartSourceTrigger,
		IntentType: f.IntentType,
		Trigger: &TriggerStartSource{
			TriggerID:   f.SourceID,
			TriggerType: string(f.Kind),
			Key:         f.Key,
			Facts:       copyFacts023(f.Facts),
		},
	}
}

func copyFacts023(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// TriggerRegistry holds trigger sources and the keys they already
// fired. It is safe for concurrent use.
type TriggerRegistry struct {
	mu        sync.Mutex
	sources   map[string]TriggerSource
	fired     map[string]bool
	watermark time.Time
}

// NewTriggerRegistry returns an empty registry.
func NewTriggerRegistry() *TriggerRegistry {
	return &TriggerRegistry{sources: map[string]TriggerSource{}, fired: map[string]bool{}}
}

// Register records one trigger source. Times are normalized to UTC.
func (r *TriggerRegistry) Register(src TriggerSource) error {
	if src.ID == "" {
		return refuse(CodeInvalidRecord, "", "", "trigger source names no id")
	}
	if src.WorkflowID == "" {
		return refuse(CodeInvalidRecord, "", "", "trigger source %q names no workflow", src.ID)
	}
	if src.IntentType == "" {
		return refuse(CodeInvalidRecord, "", "", "trigger source %q names no intent type", src.ID)
	}
	if src.TenantID == uuid.Nil {
		return refuse(CodeInvalidRecord, "", "", "trigger source %q names no tenant", src.ID)
	}
	switch src.Kind {
	case TriggerSourceDate:
		if src.At.IsZero() {
			return refuse(CodeInvalidRecord, "", "", "date trigger %q names no fire time", src.ID)
		}
		src.At = src.At.UTC()
	case TriggerSourceSchedule:
		if src.Every <= 0 {
			return refuse(CodeInvalidRecord, "", "", "schedule trigger %q names no positive interval", src.ID)
		}
		if src.Anchor.IsZero() {
			return refuse(CodeInvalidRecord, "", "", "schedule trigger %q names no anchor", src.ID)
		}
		src.Anchor = src.Anchor.UTC()
	case TriggerSourceEvent:
		if src.EventType == "" {
			return refuse(CodeInvalidRecord, "", "", "event trigger %q names no event type", src.ID)
		}
	case TriggerSourceObligation:
		if src.ObligationType == "" {
			return refuse(CodeInvalidRecord, "", "", "obligation trigger %q names no obligation type", src.ID)
		}
	default:
		return refuse(CodeInvalidRecord, "", "", "trigger source %q has unknown kind %q", src.ID, string(src.Kind))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.sources[src.ID]; dup {
		return refuse(CodeDuplicateTrigger, "", "", "trigger source %q is already registered", src.ID)
	}
	r.sources[src.ID] = src
	return nil
}

func occurrenceKey023(id string, at time.Time) string {
	return id + "@" + at.UTC().Format(time.RFC3339Nano)
}

// Sweep fires every due date occurrence, every schedule occurrence
// after the last sweep watermark up to now, and every tracked open
// obligation matching an obligation source. It returns fires oldest
// first (ties by source id, then key) and advances the watermark to
// now, or only to the earliest capped source's last fired occurrence
// when a schedule hit MaxTriggerCatchUp, so the remaining occurrences
// fire on later sweeps. A nil tracker skips the obligation drain.
func (r *TriggerRegistry) Sweep(tenant uuid.UUID, now time.Time, tracker *ObligationTracker) []TriggerFire {
	now = now.UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	var fires []TriggerFire
	cappedAt := time.Time{}
	for _, src := range r.sortedSources023(tenant) {
		switch src.Kind {
		case TriggerSourceDate:
			if src.At.After(now) {
				continue
			}
			key := occurrenceKey023(src.ID, src.At)
			if r.fired[key] {
				continue
			}
			r.fired[key] = true
			fires = append(fires, TriggerFire{
				SourceID: src.ID, Kind: src.Kind, Key: key,
				WorkflowID: src.WorkflowID, IntentType: src.IntentType,
				FiredAt: src.At,
			})
		case TriggerSourceSchedule:
			sched, capped, last := r.sweepSchedule023(src, now)
			fires = append(fires, sched...)
			if capped && (cappedAt.IsZero() || last.Before(cappedAt)) {
				cappedAt = last
			}
		case TriggerSourceObligation:
			if tracker == nil {
				continue
			}
			for _, ob := range tracker.pendingSorted023(src.ObligationType) {
				key := src.ID + "/obligation/" + ob.Key
				if r.fired[key] {
					continue
				}
				if !tracker.claim023(src.ObligationType, ob.Key) {
					continue
				}
				r.fired[key] = true
				fires = append(fires, TriggerFire{
					SourceID: src.ID, Kind: src.Kind, Key: key,
					WorkflowID: src.WorkflowID, IntentType: src.IntentType,
					Facts: copyFacts023(ob.Facts), FiredAt: ob.EmittedAt.UTC(),
				})
			}
		}
	}
	next := now
	if !cappedAt.IsZero() {
		next = cappedAt
	}
	if next.After(r.watermark) {
		r.watermark = next
	}
	sortTriggerFires023(fires)
	return fires
}

// sweepSchedule023 fires the source's occurrences after the watermark up to
// now, oldest first, stopping after MaxTriggerCatchUp new fires. It reports
// whether the cap cut the enumeration short and, when it did, the last fired
// occurrence, so Sweep can resume the next sweep there instead of at now.
func (r *TriggerRegistry) sweepSchedule023(src TriggerSource, now time.Time) ([]TriggerFire, bool, time.Time) {
	// Occurrences are anchor + n*every for n >= 1: the anchor opens
	// the first interval, it is not itself a fire.
	n := int64(1)
	if r.watermark.After(src.Anchor) {
		n = int64(r.watermark.Sub(src.Anchor)/src.Every) + 1
		if n < 1 {
			n = 1
		}
	}
	var fires []TriggerFire
	for emitted := 0; emitted < MaxTriggerCatchUp; n++ {
		occ := src.Anchor.Add(time.Duration(n) * src.Every)
		if occ.After(now) {
			return fires, false, time.Time{}
		}
		key := occurrenceKey023(src.ID, occ)
		if r.fired[key] {
			continue
		}
		r.fired[key] = true
		emitted++
		fires = append(fires, TriggerFire{
			SourceID: src.ID, Kind: src.Kind, Key: key,
			WorkflowID: src.WorkflowID, IntentType: src.IntentType,
			FiredAt: occ,
		})
	}
	return fires, true, fires[len(fires)-1].FiredAt
}

// FireEvent fires every event source of this tenant subscribed to
// eventType. eventID is the at-most-once identity: redelivering the
// same event fires nothing.
func (r *TriggerRegistry) FireEvent(tenant uuid.UUID, eventType, eventID string, facts map[string]string, now time.Time) []TriggerFire {
	if eventID == "" {
		return nil
	}
	now = now.UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	var fires []TriggerFire
	for _, src := range r.sortedSources023(tenant) {
		if src.Kind != TriggerSourceEvent || src.EventType != eventType {
			continue
		}
		key := src.ID + "/event/" + eventID
		if r.fired[key] {
			continue
		}
		r.fired[key] = true
		fires = append(fires, TriggerFire{
			SourceID: src.ID, Kind: src.Kind, Key: key,
			WorkflowID: src.WorkflowID, IntentType: src.IntentType,
			Facts: copyFacts023(facts), FiredAt: now,
		})
	}
	sortTriggerFires023(fires)
	return fires
}

func (r *TriggerRegistry) sortedSources023(tenant uuid.UUID) []TriggerSource {
	var out []TriggerSource
	for _, src := range r.sources {
		if src.TenantID != tenant {
			continue
		}
		out = append(out, src)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortTriggerFires023(fires []TriggerFire) {
	sort.Slice(fires, func(i, j int) bool {
		if !fires[i].FiredAt.Equal(fires[j].FiredAt) {
			return fires[i].FiredAt.Before(fires[j].FiredAt)
		}
		if fires[i].SourceID != fires[j].SourceID {
			return fires[i].SourceID < fires[j].SourceID
		}
		return fires[i].Key < fires[j].Key
	})
}

// OpenObligation is one typed open obligation an END step emitted:
// work a completed run hands to a follow-up run. Key is the
// at-most-once identity within Type.
type OpenObligation struct {
	Type      string
	Key       string
	Facts     map[string]string
	EmittedBy string
	EmittedAt time.Time
}

// ObligationTracker holds open obligations until a sweep drains them
// into obligation-trigger fires. It is safe for concurrent use.
type ObligationTracker struct {
	mu      sync.Mutex
	pending map[string]map[string]OpenObligation
}

// NewObligationTracker returns an empty tracker.
func NewObligationTracker() *ObligationTracker {
	return &ObligationTracker{pending: map[string]map[string]OpenObligation{}}
}

// Emit records one open obligation. Emitting the same type and key
// twice is refused: the first emission already owns the follow-up.
func (t *ObligationTracker) Emit(ob OpenObligation) error {
	if ob.Type == "" {
		return refuse(CodeInvalidRecord, "", "", "obligation names no type")
	}
	if ob.Key == "" {
		return refuse(CodeInvalidRecord, "", "", "obligation of type %q names no key", ob.Type)
	}
	if ob.EmittedBy == "" {
		return refuse(CodeInvalidRecord, "", "", "obligation %s/%s names no emitting run", ob.Type, ob.Key)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	byKey, ok := t.pending[ob.Type]
	if !ok {
		byKey = map[string]OpenObligation{}
		t.pending[ob.Type] = byKey
	}
	if _, dup := byKey[ob.Key]; dup {
		return refuse(CodeDuplicateTrigger, "", "", "obligation %s/%s is already open", ob.Type, ob.Key)
	}
	ob.EmittedAt = ob.EmittedAt.UTC()
	ob.Facts = copyFacts023(ob.Facts)
	byKey[ob.Key] = ob
	return nil
}

// Pending returns the open obligations of one type, oldest first.
func (t *ObligationTracker) Pending(obligationType string) []OpenObligation {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pendingSorted023(obligationType)
}

func (t *ObligationTracker) pendingSorted023(obligationType string) []OpenObligation {
	var out []OpenObligation
	for _, ob := range t.pending[obligationType] {
		out = append(out, ob)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].EmittedAt.Equal(out[j].EmittedAt) {
			return out[i].EmittedAt.Before(out[j].EmittedAt)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Claim consumes one open obligation, reporting whether it was open.
func (t *ObligationTracker) Claim(obligationType, key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.claim023(obligationType, key)
}

func (t *ObligationTracker) claim023(obligationType, key string) bool {
	byKey, ok := t.pending[obligationType]
	if !ok {
		return false
	}
	if _, ok := byKey[key]; !ok {
		return false
	}
	delete(byKey, key)
	return true
}
