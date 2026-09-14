// Package progress detects stuck workflow instances from expected progress
// (WF-RUN-020).
//
// A workflow is not stuck because it is old. A promotion legitimately waits
// weeks for its effective date, and an approval legitimately waits for a
// person. What makes an instance stuck is that something the runtime itself
// promised would move it has not: a timer that was due and never fired, a
// lease whose holder stopped heartbeating and nobody reclaimed, ready work
// eligible long ago and never dispatched, a signal subscription that expired
// and was never closed, a human task past its escalation point and never
// escalated, a node that keeps failing, or a live instance with no mechanism
// at all that could advance it.
//
// Detect is a pure function over a [Snapshot] read from the runtime's own
// durable rows, so the verdict for one instance depends only on what those
// rows say and the instant it is evaluated at. Nothing in this package writes
// a runtime or business row: [LoadSnapshots] only reads, and [RaiseIncident]
// writes only the operations store's operational_incident row.
package progress

import (
	"fmt"
	"sort"
	"time"
)

// Kind names one missed expectation.
type Kind string

// The expectations a live instance can miss.
const (
	// KindTimerOverdue: a PENDING timer is past fires_at plus grace.
	KindTimerOverdue Kind = "TIMER_OVERDUE"
	// KindLeaseAbandoned: a HELD lease is past expires_at plus grace, so its
	// holder is gone and nothing reclaimed the work it fenced.
	KindLeaseAbandoned Kind = "LEASE_ABANDONED"
	// KindReadyWorkUndispatched: READY work is past eligible_at plus grace.
	KindReadyWorkUndispatched Kind = "READY_WORK_UNDISPATCHED"
	// KindSignalExpiredUnclosed: an OPEN subscription is past expires_at plus
	// grace and was never marked EXPIRED, so its expiry path never ran.
	KindSignalExpiredUnclosed Kind = "SIGNAL_EXPIRED_UNCLOSED"
	// KindSLAEscalationMissed: an open human task is past its SLA
	// escalate_at plus grace and was never escalated.
	KindSLAEscalationMissed Kind = "SLA_ESCALATION_MISSED"
	// KindPoisonNode: one node has failed at least PoisonAttempts times.
	KindPoisonNode Kind = "POISON_NODE"
	// KindNoProgressMechanism: a live, unpaused instance holds no pending
	// timer, held lease, ready work, open subscription or open human task, so
	// nothing in the runtime can ever advance it.
	KindNoProgressMechanism Kind = "NO_PROGRESS_MECHANISM"
)

// Instance is one workflow instance's runtime row.
type Instance struct {
	TenantID       string
	InstanceID     string
	WorkflowID     string
	RuntimeStatus  string
	CorrelationID  string
	CreatedAt      time.Time
	StartedAt      time.Time
	LastRecordedAt time.Time
}

// Timer is one workflow_timer row.
type Timer struct {
	TimerID, NodeID, Kind, State string
	FiresAt                      time.Time
}

// Lease is one workflow_lease row fencing this instance or one of its nodes.
type Lease struct {
	LeaseID, ResourceKind, ResourceID, State, HolderID string
	ExpiresAt, HeartbeatAt                             time.Time
}

// ReadyWork is one workflow_ready_work row.
type ReadyWork struct {
	ReadyWorkID, NodeID, State string
	Attempt                    int
	EligibleAt                 time.Time
}

// Subscription is one workflow_signal_subscription row.
type Subscription struct {
	SubscriptionID, NodeID, SignalName, State string
	ExpiresAt                                 time.Time
}

// HumanTask is one open work_item row of this instance, with its SLA row when
// one exists.
type HumanTask struct {
	WorkItemID, NodeID, Status string
	// Open is whether the work item is still awaiting a person.
	Open bool
	// SLA fields are zero when the item carries no SLA row.
	HasSLA      bool
	BreachState string
	EscalateAt  time.Time
}

// NodeAttempt is one workflow_node_execution row.
type NodeAttempt struct {
	NodeID     string
	Attempt    int
	Status     string
	ErrorClass string
}

// Snapshot is everything the runtime durably recorded about one instance.
type Snapshot struct {
	Instance      Instance
	Timers        []Timer
	Leases        []Lease
	ReadyWork     []ReadyWork
	Subscriptions []Subscription
	HumanTasks    []HumanTask
	Attempts      []NodeAttempt
}

// Policy is the tolerance each expectation is granted before it counts as
// missed. A zero grace is allowed and means "the instant it is due".
type Policy struct {
	TimerGrace      time.Duration
	LeaseGrace      time.Duration
	DispatchGrace   time.Duration
	SignalGrace     time.Duration
	EscalationGrace time.Duration
	// PoisonAttempts is how many FAILED attempts of one node make it poison.
	PoisonAttempts int
}

// DefaultPolicy is the runtime's default tolerance.
func DefaultPolicy() Policy {
	return Policy{
		TimerGrace: 5 * time.Minute, LeaseGrace: 2 * time.Minute, DispatchGrace: 10 * time.Minute,
		SignalGrace: 5 * time.Minute, EscalationGrace: 15 * time.Minute, PoisonAttempts: 3,
	}
}

// Validate refuses a policy that could never or always flag.
func (p Policy) Validate() error {
	for name, d := range map[string]time.Duration{
		"timer": p.TimerGrace, "lease": p.LeaseGrace, "dispatch": p.DispatchGrace,
		"signal": p.SignalGrace, "escalation": p.EscalationGrace,
	} {
		if d < 0 {
			return fmt.Errorf("progress: %s grace %s is negative", name, d)
		}
	}
	if p.PoisonAttempts < 1 {
		return fmt.Errorf("progress: poison attempts %d must be at least 1", p.PoisonAttempts)
	}
	return nil
}

// Finding is one missed expectation with the evidence that proves it.
type Finding struct {
	Kind Kind
	// NodeID is the node the expectation belongs to, when it has one.
	NodeID string
	// Ref identifies the runtime row that carries the expectation.
	Ref string
	// DueAt is when the expectation was due (before grace). It is zero for
	// expectations with no due instant (poison, no mechanism).
	DueAt time.Time
	// Detail is a bounded, human-readable explanation.
	Detail string
}

// Identity is the stable identity of the finding, independent of detail
// wording and of when it was evaluated.
func (f Finding) Identity() string { return string(f.Kind) + "|" + f.NodeID + "|" + f.Ref }

// live reports whether an instance status can still make progress.
func live(status string) bool {
	switch status {
	case "CREATED", "RUNNING", "WAITING", "PAUSE_REQUESTED", "CANCELLING", "BLOCKED":
		return true
	}
	return false
}

// Detect returns every missed expectation of the snapshot's instance at now,
// sorted by identity. A terminal or PAUSED instance has no expectations: a
// pause is an operator's decision, not a stall.
func Detect(s Snapshot, p Policy, now time.Time) ([]Finding, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		return nil, fmt.Errorf("progress: evaluation instant is required")
	}
	if !live(s.Instance.RuntimeStatus) {
		return nil, nil
	}
	var out []Finding
	mechanisms := 0

	for _, t := range s.Timers {
		if t.State != "PENDING" {
			continue
		}
		mechanisms++
		if now.After(t.FiresAt.Add(p.TimerGrace)) {
			out = append(out, Finding{Kind: KindTimerOverdue, NodeID: t.NodeID, Ref: "workflow_timer:" + t.TimerID, DueAt: t.FiresAt,
				Detail: fmt.Sprintf("%s timer was due at %s and has not fired", t.Kind, t.FiresAt.UTC().Format(time.RFC3339))})
		}
	}
	for _, l := range s.Leases {
		if l.State != "HELD" {
			continue
		}
		mechanisms++
		if now.After(l.ExpiresAt.Add(p.LeaseGrace)) {
			out = append(out, Finding{Kind: KindLeaseAbandoned, NodeID: "", Ref: "workflow_lease:" + l.LeaseID, DueAt: l.ExpiresAt,
				Detail: fmt.Sprintf("%s lease on %s expired at %s (last heartbeat %s) and was never reclaimed", l.ResourceKind, l.ResourceID,
					l.ExpiresAt.UTC().Format(time.RFC3339), l.HeartbeatAt.UTC().Format(time.RFC3339))})
		}
	}
	for _, r := range s.ReadyWork {
		if r.State != "READY" && r.State != "DISPATCHED" {
			continue
		}
		mechanisms++
		if r.State == "READY" && now.After(r.EligibleAt.Add(p.DispatchGrace)) {
			out = append(out, Finding{Kind: KindReadyWorkUndispatched, NodeID: r.NodeID, Ref: "workflow_ready_work:" + r.ReadyWorkID, DueAt: r.EligibleAt,
				Detail: fmt.Sprintf("attempt %d was eligible at %s and was never dispatched", r.Attempt, r.EligibleAt.UTC().Format(time.RFC3339))})
		}
	}
	for _, sub := range s.Subscriptions {
		if sub.State != "OPEN" {
			continue
		}
		mechanisms++
		if !sub.ExpiresAt.IsZero() && now.After(sub.ExpiresAt.Add(p.SignalGrace)) {
			out = append(out, Finding{Kind: KindSignalExpiredUnclosed, NodeID: sub.NodeID, Ref: "workflow_signal_subscription:" + sub.SubscriptionID, DueAt: sub.ExpiresAt,
				Detail: fmt.Sprintf("subscription to %s expired at %s and was never closed", sub.SignalName, sub.ExpiresAt.UTC().Format(time.RFC3339))})
		}
	}
	for _, task := range s.HumanTasks {
		if !task.Open {
			continue
		}
		mechanisms++
		if task.HasSLA && task.BreachState != "ESCALATED" && task.BreachState != "SATISFIED" && now.After(task.EscalateAt.Add(p.EscalationGrace)) {
			out = append(out, Finding{Kind: KindSLAEscalationMissed, NodeID: task.NodeID, Ref: "work_item:" + task.WorkItemID, DueAt: task.EscalateAt,
				Detail: fmt.Sprintf("%s task passed its escalation point %s and was never escalated", task.Status, task.EscalateAt.UTC().Format(time.RFC3339))})
		}
	}

	failures := map[string]int{}
	for _, a := range s.Attempts {
		if a.Status == "FAILED" {
			failures[a.NodeID]++
		}
	}
	for node, n := range failures {
		if n >= p.PoisonAttempts {
			out = append(out, Finding{Kind: KindPoisonNode, NodeID: node, Ref: "workflow_node_execution:" + node,
				Detail: fmt.Sprintf("node failed %d times (poison threshold %d)", n, p.PoisonAttempts)})
		}
	}

	// A parked or blocked instance may legitimately hold no mechanism of its
	// own (BLOCKED waits on repair; CANCELLING drains), so only instances the
	// runtime says are actively executing or waiting are expected to hold one.
	if mechanisms == 0 && (s.Instance.RuntimeStatus == "RUNNING" || s.Instance.RuntimeStatus == "WAITING" || s.Instance.RuntimeStatus == "CREATED") {
		out = append(out, Finding{Kind: KindNoProgressMechanism, Ref: "workflow_instance:" + s.Instance.InstanceID,
			Detail: fmt.Sprintf("%s instance holds no pending timer, held lease, ready work, open subscription or open human task", s.Instance.RuntimeStatus)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out, nil
}
