package workerlifecycle

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrTrackingRejected is the WORKER-LIFE-003 refusal boundary. Retries never
// duplicate a child, task completion never substitutes for observation, and
// closure needs exact readiness.
var ErrTrackingRejected = errors.New("WORKER_LIFE_003_REJECTED")

// ChildState is the closed child lifecycle vocabulary.
type ChildState string

const (
	StatusChildPending  ChildState = "PENDING"
	StatusChildEmitted  ChildState = "EMITTED"
	StatusChildObserved ChildState = "OBSERVED"
	StatusChildFailed   ChildState = "FAILED"
)

// ChildOutcome is the closed observation-outcome vocabulary.
type ChildOutcome string

const (
	ChildObserved ChildOutcome = "OBSERVED"
	ChildFailed   ChildOutcome = "FAILED"
)

// TrackedChild is one bounded child intent and its lifecycle state.
type TrackedChild struct {
	ChildID        string
	IntentType     string
	IntentVersion  string
	DependsOn      []string
	ScopeDigest    string
	State          ChildState
	IntentID       string
	ObservationRef values.EntityRef
}

// ChildIntent is the emitted bounded intent. It carries digests and
// references only, never protected payloads.
type ChildIntent struct {
	ID            string
	ChildID       string
	IntentType    string
	IntentVersion string
	ScopeDigest   string
	PlanDigest    string
	Revision      uint64
}

// ChildRepair is a scoped repair obligation for one failed child.
type ChildRepair struct {
	ChildID string
	Reason  string
}

// OnboardingTracker coordinates one plan's bounded children. The parent
// stores references and typed aggregates, never protected payloads.
type OnboardingTracker struct {
	PlanDigest string
	EventDate  values.LocalDate
	Revision   uint64
	Completion CompletionPolicy
	Children   []TrackedChild
	Repairs    []ChildRepair
	Closed     bool
	Digest     string
}

// ScopeForTemplate binds one child template to its plan: type, version and
// identity, nothing broader.
func ScopeForTemplate(planDigest string, tmpl ChildTemplate) (string, error) {
	if strings.TrimSpace(planDigest) == "" || strings.TrimSpace(tmpl.ID) == "" ||
		strings.TrimSpace(tmpl.IntentType) == "" || strings.TrimSpace(tmpl.IntentVersion) == "" {
		return "", errors.Join(ErrTrackingRejected, errors.New("plan digest and template identity are required"))
	}
	w := canonicalbytes.New("hcmnext.domains.workerlifecycle.ChildScope", 1).
		String("plan_digest", planDigest).String("child_id", tmpl.ID).
		String("intent_type", tmpl.IntentType).String("intent_version", tmpl.IntentVersion)
	raw, err := w.Bytes()
	if err != nil {
		return "", errors.Join(ErrTrackingRejected, err)
	}
	return canonicalbytes.Digest(raw), nil
}

func intentID(planDigest, scope, childID string, revision uint64) string {
	w := canonicalbytes.New("hcmnext.domains.workerlifecycle.ChildIntent", 1).
		String("plan_digest", planDigest).String("scope_digest", scope).
		String("child_id", childID).Int("revision", int64(revision))
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// NewOnboardingTracker binds one sealed plan and its readiness into a
// tracker. Emission and closure are separate governed steps.
func NewOnboardingTracker(plan WorkerLifecyclePlan, readiness ReadinessResolution) (OnboardingTracker, error) {
	fail := func(format string, args ...any) (OnboardingTracker, error) {
		return OnboardingTracker{}, errors.Join(ErrTrackingRejected, fmt.Errorf(format, args...))
	}
	if err := plan.Validate(); err != nil {
		return fail("plan: %v", err)
	}
	if err := readiness.Validate(); err != nil {
		return fail("readiness: %v", err)
	}
	if readiness.PlanDigest != plan.CanonicalDigest {
		return fail("readiness binds another plan")
	}
	tracker := OnboardingTracker{
		PlanDigest: plan.CanonicalDigest,
		EventDate:  plan.EventDate,
		Revision:   1,
		Completion: plan.Completion,
	}
	ordered := append([]ChildTemplate(nil), plan.Children...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Ordinal < ordered[j].Ordinal })
	for _, tmpl := range ordered {
		scope, err := ScopeForTemplate(tracker.PlanDigest, tmpl)
		if err != nil {
			return fail("scope: %v", err)
		}
		tracker.Children = append(tracker.Children, TrackedChild{
			ChildID: tmpl.ID, IntentType: tmpl.IntentType, IntentVersion: tmpl.IntentVersion,
			DependsOn:   append([]string(nil), tmpl.DependsOn...),
			ScopeDigest: scope, State: StatusChildPending,
		})
	}
	tracker.Digest = canonicalbytes.Digest(tracker.body())
	return tracker, nil
}

// EmitDueChildren emits every pending child whose dependencies are observed.
// Emission is idempotent: an emitted child is never emitted twice, and a
// blocked or unknown readiness emits nothing.
func EmitDueChildren(tracker OnboardingTracker, readiness ReadinessResolution) (OnboardingTracker, []ChildIntent, error) {
	fail := func(format string, args ...any) (OnboardingTracker, []ChildIntent, error) {
		return OnboardingTracker{}, nil, errors.Join(ErrTrackingRejected, fmt.Errorf(format, args...))
	}
	if readiness.PlanDigest != tracker.PlanDigest {
		return fail("readiness binds another plan")
	}
	if readiness.Aggregate == AggregateBlocked || readiness.Aggregate == AggregateUnknown {
		return fail("readiness %s emits no children", readiness.Aggregate)
	}
	if tracker.Closed {
		return fail("tracker is closed")
	}
	observed := map[string]bool{}
	for _, child := range tracker.Children {
		if child.State == StatusChildObserved {
			observed[child.ChildID] = true
		}
	}
	next := tracker
	next.Children = append([]TrackedChild(nil), tracker.Children...)
	emitted := []ChildIntent{}
	for i, child := range next.Children {
		if child.State != StatusChildPending {
			continue
		}
		ready := true
		for _, dep := range child.DependsOn {
			if !observed[dep] {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		id := intentID(next.PlanDigest, child.ScopeDigest, child.ChildID, next.Revision)
		if id == "" {
			return fail("child %q identity failed", child.ChildID)
		}
		next.Children[i].State = StatusChildEmitted
		next.Children[i].IntentID = id
		emitted = append(emitted, ChildIntent{
			ID: id, ChildID: child.ChildID, IntentType: child.IntentType,
			IntentVersion: child.IntentVersion, ScopeDigest: child.ScopeDigest,
			PlanDigest: next.PlanDigest, Revision: next.Revision,
		})
	}
	next.Digest = canonicalbytes.Digest(next.body())
	return next, emitted, nil
}

// ObserveChild records a governed observation for one emitted child. Only
// an observation advances a child; task completion alone is refused by
// CompleteChildTask.
func ObserveChild(tracker OnboardingTracker, childID string, observation values.EntityRef, outcome ChildOutcome) (OnboardingTracker, error) {
	fail := func(format string, args ...any) (OnboardingTracker, error) {
		return OnboardingTracker{}, errors.Join(ErrTrackingRejected, fmt.Errorf(format, args...))
	}
	if err := observation.Validate(); err != nil {
		return fail("observation: %v", err)
	}
	if outcome != ChildObserved && outcome != ChildFailed {
		return fail("outcome %q is not declared", outcome)
	}
	next := tracker
	next.Children = append([]TrackedChild(nil), tracker.Children...)
	for i, child := range next.Children {
		if child.ChildID != childID {
			continue
		}
		if child.State != StatusChildEmitted {
			return fail("child %q is %s, not emitted", childID, child.State)
		}
		if outcome == ChildObserved {
			next.Children[i].State = StatusChildObserved
			next.Children[i].ObservationRef = observation
		} else {
			next.Children[i].State = StatusChildFailed
			next.Repairs = append(append([]ChildRepair(nil), tracker.Repairs...), ChildRepair{ChildID: childID, Reason: "observation failed"})
		}
		next.Digest = canonicalbytes.Digest(next.body())
		return next, nil
	}
	return fail("child %q is not tracked", childID)
}

// CompleteChildTask refuses task completion as a substitute for the required
// governed observation.
func CompleteChildTask(tracker OnboardingTracker, childID string) (OnboardingTracker, error) {
	for _, child := range tracker.Children {
		if child.ChildID == childID {
			return OnboardingTracker{}, errors.Join(ErrTrackingRejected, fmt.Errorf("child %q: task completion never substitutes for observation", childID))
		}
	}
	return OnboardingTracker{}, errors.Join(ErrTrackingRejected, fmt.Errorf("child %q is not tracked", childID))
}

// RetryChild returns a failed child to emitted under its original
// deterministic identity: a retry never mints a duplicate.
func RetryChild(tracker OnboardingTracker, childID string) (OnboardingTracker, error) {
	fail := func(format string, args ...any) (OnboardingTracker, error) {
		return OnboardingTracker{}, errors.Join(ErrTrackingRejected, fmt.Errorf(format, args...))
	}
	next := tracker
	next.Children = append([]TrackedChild(nil), tracker.Children...)
	for i, child := range next.Children {
		if child.ChildID != childID {
			continue
		}
		if child.State != StatusChildFailed {
			return fail("child %q is %s, not failed", childID, child.State)
		}
		next.Children[i].State = StatusChildEmitted
		next.Digest = canonicalbytes.Digest(next.body())
		return next, nil
	}
	return fail("child %q is not tracked", childID)
}

// ReplanTracker records a material change such as a late start date. A new
// revision invalidates every unobserved child instead of preserving stale
// identities; observed evidence stands.
func ReplanTracker(tracker OnboardingTracker, eventDate values.LocalDate, reason string) (OnboardingTracker, error) {
	fail := func(format string, args ...any) (OnboardingTracker, error) {
		return OnboardingTracker{}, errors.Join(ErrTrackingRejected, fmt.Errorf(format, args...))
	}
	if err := eventDate.Validate(); err != nil {
		return fail("event date: %v", err)
	}
	if strings.TrimSpace(reason) == "" {
		return fail("replan reason is required")
	}
	if tracker.Closed {
		return fail("tracker is closed")
	}
	next := tracker
	next.EventDate = eventDate
	next.Revision++
	next.Children = append([]TrackedChild(nil), tracker.Children...)
	for i, child := range next.Children {
		if child.State == StatusChildObserved {
			continue
		}
		next.Children[i].State = StatusChildPending
		next.Children[i].IntentID = ""
	}
	next.Digest = canonicalbytes.Digest(next.body())
	return next, nil
}

// CloseTracker closes the lifecycle under its exact policy: every child
// observed and readiness READY, or CONDITIONAL for business-only completion.
func CloseTracker(tracker OnboardingTracker, readiness ReadinessResolution) (OnboardingTracker, error) {
	fail := func(format string, args ...any) (OnboardingTracker, error) {
		return OnboardingTracker{}, errors.Join(ErrTrackingRejected, fmt.Errorf(format, args...))
	}
	if tracker.Closed {
		return fail("tracker is already closed")
	}
	if readiness.PlanDigest != tracker.PlanDigest {
		return fail("readiness binds another plan")
	}
	for _, child := range tracker.Children {
		if child.State != StatusChildObserved {
			return fail("child %q is %s, not observed", child.ChildID, child.State)
		}
	}
	switch tracker.Completion {
	case CompleteAllRequired:
		if readiness.Aggregate != AggregateReady {
			return fail("readiness %s does not satisfy ALL_REQUIRED", readiness.Aggregate)
		}
	case CompleteBusinessOnly:
		if readiness.Aggregate != AggregateReady && readiness.Aggregate != AggregateConditional {
			return fail("readiness %s does not satisfy BUSINESS_ONLY", readiness.Aggregate)
		}
	default:
		return fail("completion policy %q is not declared", tracker.Completion)
	}
	next := tracker
	next.Closed = true
	next.Digest = canonicalbytes.Digest(next.body())
	return next, nil
}

func (t OnboardingTracker) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.workerlifecycle.OnboardingTracker", 1).
		String("plan_digest", t.PlanDigest).Value("event_date", t.EventDate).
		Int("revision", int64(t.Revision)).String("completion", string(t.Completion)).
		Bool("closed", t.Closed).Count("children", len(t.Children))
	for _, child := range t.Children {
		w.String("child_id", child.ChildID).String("intent_type", child.IntentType).
			String("intent_version", child.IntentVersion).String("scope_digest", child.ScopeDigest).
			String("state", string(child.State)).String("intent_id", child.IntentID)
		w.SortedStrings("depends_on", child.DependsOn)
		w.Optional("observation", child.State == StatusChildObserved, child.ObservationRef)
	}
	w.Count("repairs", len(t.Repairs))
	for _, repair := range t.Repairs {
		w.String("repair_child", repair.ChildID).String("repair_reason", repair.Reason)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate rechecks child identities, scope bindings, observation linkage,
// closure preconditions and the digest.
func (t OnboardingTracker) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrTrackingRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(t.PlanDigest) == "" || t.Revision == 0 {
		return fail("plan lineage is required")
	}
	seen := map[string]struct{}{}
	for i, child := range t.Children {
		if strings.TrimSpace(child.ChildID) == "" || strings.TrimSpace(child.IntentType) == "" || strings.TrimSpace(child.IntentVersion) == "" {
			return fail("child %d identity is required", i)
		}
		if _, ok := seen[child.ChildID]; ok {
			return fail("duplicate child %q", child.ChildID)
		}
		seen[child.ChildID] = struct{}{}
		scope, err := ScopeForTemplate(t.PlanDigest, ChildTemplate{
			ID: child.ChildID, IntentType: child.IntentType, IntentVersion: child.IntentVersion,
		})
		if err != nil || scope != child.ScopeDigest {
			return fail("child %q scope is not bounded", child.ChildID)
		}
		switch child.State {
		case StatusChildPending:
			if child.IntentID != "" {
				return fail("child %q pending with an intent identity", child.ChildID)
			}
		case StatusChildEmitted, StatusChildFailed:
			if child.IntentID != intentID(t.PlanDigest, child.ScopeDigest, child.ChildID, t.Revision) {
				return fail("child %q intent identity mismatch", child.ChildID)
			}
		case StatusChildObserved:
			if child.IntentID != intentID(t.PlanDigest, child.ScopeDigest, child.ChildID, t.Revision) {
				return fail("child %q intent identity mismatch", child.ChildID)
			}
			if err := child.ObservationRef.Validate(); err != nil {
				return fail("child %q observation: %v", child.ChildID, err)
			}
		default:
			return fail("child %q state %q is not declared", child.ChildID, child.State)
		}
		if t.Closed && child.State != StatusChildObserved {
			return fail("closed with child %q %s", child.ChildID, child.State)
		}
	}
	if t.Digest != canonicalbytes.Digest(t.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}
