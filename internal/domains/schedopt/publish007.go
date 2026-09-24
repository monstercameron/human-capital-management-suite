// SCHED-OPT-007: publish and reconcile schedules.
//
// PublishSchedule turns one approved schedule into governed assignment,
// message and integration effects exactly once under an idempotency key.
// ReconcileSchedule compares observed schedule and demand coverage
// against the publication across dimensions (worker, window, demand,
// channel) and returns repairable failures. Both functions are
// kernel-pure: effects are described, never executed.
package schedopt

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// PublishVersion is the rejection version for schedule publication.
const PublishVersion = "schedopt-publish/v1"

var (
	// ErrPublishRejected is the SCHED-OPT-007 sentinel. A publication
	// that would duplicate effects, or observations that cannot be
	// reconciled, fail with this error.
	ErrPublishRejected = errors.New("SCHED_OPT_007_REJECTED")
)

// PublishRejection is the stable SCHED-OPT-007 failure shape.
type PublishRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *PublishRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrPublishRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the SCHED_OPT_007_REJECTED sentinel to errors.Is.
func (r *PublishRejection) Unwrap() error { return ErrPublishRejected }

func publishReject(field, state, reason string) error {
	return &PublishRejection{Field: field, State: state, Version: PublishVersion, Reason: reason}
}

// Publication is the governed once-only effect description for one
// approved schedule.
type Publication struct {
	Tenant             string
	Revision           string
	BoundDigest        string
	IdempotencyKey     string
	Assignments        []ReviewAssignment
	Messages           []string
	IntegrationEffects []string
	PublishedAt        time.Time
	Digest             string
}

func (p Publication) computedDigest() string {
	assignments := make([]string, 0, len(p.Assignments))
	for _, a := range p.Assignments {
		assignments = append(assignments, strings.Join([]string{a.AssignmentID, a.WorkerRef, a.DemandRef, a.WindowRef}, "\x00"))
	}
	sort.Strings(assignments)
	w := canonicalbytes.New("hcmnext.domains.schedopt.Publication", 1).
		String("tenant", p.Tenant).
		String("revision", p.Revision).
		String("bound", p.BoundDigest).
		String("idempotency", p.IdempotencyKey).
		String("published_at", p.PublishedAt.UTC().Format(time.RFC3339Nano)).
		SortedStrings("assignments", assignments).
		SortedStrings("messages", append([]string(nil), p.Messages...)).
		SortedStrings("effects", append([]string(nil), p.IntegrationEffects...))
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// PublishSchedule publishes one approved schedule once. The idempotency
// key binds the exact approval digest: republishing the same approval
// under the same key returns the identical publication, while a changed
// approval under a used key is refused.
func PublishSchedule(approved ApprovedSchedule, idempotencyKey string, seen map[string]string, now time.Time) (Publication, error) {
	if strings.TrimSpace(approved.Tenant) == "" || strings.TrimSpace(approved.Revision) == "" {
		return Publication{}, publishReject("publish.approval", "MISSING", "approved schedule revision is required")
	}
	if approved.BoundDigest == "" || approved.BoundDigest != approved.computedDigest() {
		return Publication{}, publishReject("publish.approval", "UNBOUND", "approval digest does not bind the schedule")
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return Publication{}, publishReject("publish.idempotency_key", "MISSING", "idempotency key is required")
	}
	if now.IsZero() {
		return Publication{}, publishReject("publish.published_at", "MISSING", "publication instant is required")
	}
	if prior, used := seen[idempotencyKey]; used {
		if prior != approved.BoundDigest {
			return Publication{}, publishReject("publish.idempotency_key", "REUSED", "idempotency key already bound another approval")
		}
	}
	if len(approved.Assignments) == 0 {
		return Publication{}, publishReject("publish.assignments", "EMPTY", "an empty schedule publishes nothing")
	}
	messages := make([]string, 0, len(approved.Assignments))
	effects := make([]string, 0, len(approved.Assignments))
	for _, a := range approved.Assignments {
		messages = append(messages, "notify:"+a.WorkerRef+":"+a.AssignmentID)
		effects = append(effects, "sync:"+a.DemandRef+":"+a.WindowRef)
	}
	sort.Strings(messages)
	sort.Strings(effects)
	pub := Publication{
		Tenant: approved.Tenant, Revision: approved.Revision, BoundDigest: approved.BoundDigest,
		IdempotencyKey: idempotencyKey, Assignments: append([]ReviewAssignment(nil), approved.Assignments...),
		Messages: messages, IntegrationEffects: effects, PublishedAt: now.UTC(),
	}
	pub.Digest = pub.computedDigest()
	return pub, nil
}

// ObservedCoverage is one multidimensional coverage reading.
type ObservedCoverage struct {
	WorkerRef string
	WindowRef string
	DemandRef string
	Channel   string
	State     string
}

// CoverageFailure is one repairable dimensional failure.
type CoverageFailure struct {
	Dimension string
	Ref       string
	State     string
	Repair    string
}

// ScheduleReconResult keeps observed coverage multidimensional and
// repairable.
type ScheduleReconResult struct {
	Matched  int
	Failures []CoverageFailure
	Repair   []string
	Digest   string
}

// ReconcileSchedule compares observed coverage against the publication.
// Unknown, missing or failed dimensions become failures with a named
// repair; nothing is auto-healed.
func ReconcileSchedule(pub Publication, observed []ObservedCoverage) (ScheduleReconResult, error) {
	if pub.Digest == "" {
		return ScheduleReconResult{}, publishReject("reconcile.publication", "UNBOUND", "publication digest is required")
	}
	if len(observed) == 0 {
		return ScheduleReconResult{}, publishReject("reconcile.observed", "MISSING", "at least one observation is required")
	}
	covered := make(map[string]bool, len(observed))
	for i, o := range observed {
		if strings.TrimSpace(o.WorkerRef) == "" || strings.TrimSpace(o.WindowRef) == "" ||
			strings.TrimSpace(o.DemandRef) == "" || strings.TrimSpace(o.Channel) == "" {
			return ScheduleReconResult{}, publishReject(fmt.Sprintf("reconcile.observed[%d]", i), "INCOMPLETE", "observations carry worker, window, demand and channel")
		}
		switch o.State {
		case "COVERED", "MISSING", "FAILED", "UNKNOWN":
		default:
			return ScheduleReconResult{}, publishReject(fmt.Sprintf("reconcile.observed[%d].state", i), "UNDECLARED", fmt.Sprintf("state %q is not declared", o.State))
		}
		if o.State == "COVERED" {
			covered[o.WorkerRef+"\x00"+o.WindowRef+"\x00"+o.DemandRef] = true
		}
	}
	res := ScheduleReconResult{}
	for _, a := range pub.Assignments {
		if covered[a.WorkerRef+"\x00"+a.WindowRef+"\x00"+a.DemandRef] {
			res.Matched++
			continue
		}
		res.Failures = append(res.Failures, CoverageFailure{
			Dimension: "assignment", Ref: a.AssignmentID, State: "UNOBSERVED",
			Repair: "re-observe assignment " + a.AssignmentID + " before reissuing effects",
		})
	}
	// Channel failures stay dimensional: one bad channel never fails the
	// whole schedule.
	channels := map[string]int{}
	for _, o := range observed {
		if o.State == "FAILED" || o.State == "UNKNOWN" {
			channels[o.Channel]++
		}
	}
	for channel, n := range channels {
		res.Failures = append(res.Failures, CoverageFailure{
			Dimension: "channel", Ref: channel, State: "DEGRADED",
			Repair: fmt.Sprintf("replay %d %s observations through the integration effect", n, channel),
		})
	}
	sort.Slice(res.Failures, func(i, j int) bool {
		if res.Failures[i].Dimension != res.Failures[j].Dimension {
			return res.Failures[i].Dimension < res.Failures[j].Dimension
		}
		return res.Failures[i].Ref < res.Failures[j].Ref
	})
	for _, f := range res.Failures {
		res.Repair = append(res.Repair, f.Dimension+":"+f.Ref+":"+f.Repair)
	}
	repairs := append([]string(nil), res.Repair...)
	sort.Strings(repairs)
	w := canonicalbytes.New("hcmnext.domains.schedopt.ScheduleRecon", 1).
		String("publication", pub.Digest).
		Int("matched", int64(res.Matched)).
		SortedStrings("repairs", repairs)
	digest, err := w.Digest()
	if err != nil {
		return ScheduleReconResult{}, publishReject("reconcile.digest", "UNENCODABLE", "result is not digestible")
	}
	res.Digest = digest
	return res, nil
}
