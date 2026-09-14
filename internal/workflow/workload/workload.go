// Package workload enforces workflow workload limits (WF-RUN-021).
//
// A workflow start is refused before any unsafe work when what it asks for
// exceeds what the control plane allows: more parallel branches, more child
// workflows, a larger input payload or a higher cost than its limits declare,
// or more concurrently live instances than its tenant may run. A request that
// is too large on its own is [OutcomeOverloaded] -- waiting will not make it
// fit. A request that only arrives while the tenant is at its concurrency
// limit is [OutcomeAdmissionDeferred] -- the same request will fit once
// running work finishes, so its durable intent stays visible for the caller's
// retry or queue policy.
//
// Limits are resolved, never hard-coded at a call site: a [ControlSnapshot]
// carries a default and rules scoped by tenant, capability and criticality,
// and the most specific matching rule wins.
package workload

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Outcome is the admission verdict for one workflow start.
type Outcome string

// Outcomes.
const (
	OutcomeAdmitted          Outcome = "ADMITTED"
	OutcomeOverloaded        Outcome = "OVERLOADED"
	OutcomeAdmissionDeferred Outcome = "ADMISSION_DEFERRED"
)

// Dimension names one limited quantity.
type Dimension string

// Dimensions.
const (
	DimensionBranches           Dimension = "BRANCHES"
	DimensionChildren           Dimension = "CHILDREN"
	DimensionPayloadBytes       Dimension = "PAYLOAD_BYTES"
	DimensionCostUnits          Dimension = "COST_UNITS"
	DimensionConcurrentInstance Dimension = "CONCURRENT_INSTANCES"
)

// Limits are the maximum quantities one workflow start may demand. Every
// limit must be positive: a zero limit would silently refuse everything, and
// "unlimited" is not a value this control plane offers.
type Limits struct {
	MaxBranches            int
	MaxChildren            int
	MaxPayloadBytes        int
	MaxCostUnits           int
	MaxConcurrentInstances int
}

// Validate refuses a non-positive limit.
func (l Limits) Validate() error {
	for d, v := range map[Dimension]int{
		DimensionBranches: l.MaxBranches, DimensionChildren: l.MaxChildren, DimensionPayloadBytes: l.MaxPayloadBytes,
		DimensionCostUnits: l.MaxCostUnits, DimensionConcurrentInstance: l.MaxConcurrentInstances,
	} {
		if v < 1 {
			return fmt.Errorf("workload: %s limit %d must be positive", d, v)
		}
	}
	return nil
}

// Rule scopes Limits to a tenant, a capability (the workflow id a start
// runs) and a criticality. An empty scope field matches anything.
type Rule struct {
	TenantID     string
	CapabilityID string
	Criticality  string
	Limits       Limits
}

func (r Rule) specificity() int {
	n := 0
	for _, s := range []string{r.TenantID, r.CapabilityID, r.Criticality} {
		if s != "" {
			n++
		}
	}
	return n
}

func (r Rule) matches(tenant, capability, criticality string) bool {
	return (r.TenantID == "" || r.TenantID == tenant) &&
		(r.CapabilityID == "" || r.CapabilityID == capability) &&
		(r.Criticality == "" || r.Criticality == criticality)
}

// ControlSnapshot is one immutable version of the control plane's limits.
type ControlSnapshot struct {
	Version string
	Default Limits
	Rules   []Rule
}

// ErrAmbiguousLimits reports two equally specific rules that both match a
// start and disagree, so no single limit set can be chosen deterministically.
var ErrAmbiguousLimits = errors.New("workload: ambiguous limit rules")

// Resolved is the limit set a start is judged against and where it came from.
type Resolved struct {
	Limits          Limits
	SnapshotVersion string
	// Source is "default" or the matching rule's scope.
	Source string
}

// DefaultSnapshot is the platform's conservative default control snapshot.
func DefaultSnapshot() ControlSnapshot {
	return ControlSnapshot{
		Version: "workload.limits/default/1",
		Default: Limits{MaxBranches: 8, MaxChildren: 4, MaxPayloadBytes: 256 * 1024, MaxCostUnits: 200, MaxConcurrentInstances: 500},
	}
}

// Resolve returns the limits for one start: the most specific matching rule,
// else the default. Two equally specific matching rules with different limits
// are [ErrAmbiguousLimits].
func (c ControlSnapshot) Resolve(tenant, capability, criticality string) (Resolved, error) {
	if strings.TrimSpace(c.Version) == "" {
		return Resolved{}, fmt.Errorf("workload: control snapshot has no version")
	}
	if err := c.Default.Validate(); err != nil {
		return Resolved{}, fmt.Errorf("workload: default limits: %w", err)
	}
	// Ambiguity is judged only among the most specific matching rules: two
	// equally general rules that disagree are harmless when a more specific
	// rule overrides both.
	bestSpec := -1
	for i, r := range c.Rules {
		if err := r.Limits.Validate(); err != nil {
			return Resolved{}, fmt.Errorf("workload: rule %d: %w", i, err)
		}
		if r.matches(tenant, capability, criticality) && r.specificity() > bestSpec {
			bestSpec = r.specificity()
		}
	}
	best := -1
	for i, r := range c.Rules {
		if !r.matches(tenant, capability, criticality) || r.specificity() != bestSpec {
			continue
		}
		if best >= 0 && c.Rules[best].Limits != r.Limits {
			return Resolved{}, fmt.Errorf("%w: rules %d and %d both match %s/%s/%s", ErrAmbiguousLimits, best, i, tenant, capability, criticality)
		}
		if best < 0 {
			best = i
		}
	}
	if best < 0 {
		return Resolved{Limits: c.Default, SnapshotVersion: c.Version, Source: "default"}, nil
	}
	r := c.Rules[best]
	return Resolved{Limits: r.Limits, SnapshotVersion: c.Version,
		Source: "tenant=" + r.TenantID + ";capability=" + r.CapabilityID + ";criticality=" + r.Criticality}, nil
}

// Demand is what one start asks for.
type Demand struct {
	Branches  int
	Children  int
	CostUnits int
	// PayloadBytes is the size of the start's input snapshot.
	PayloadBytes int
	// ActiveInstances is how many other instances of this workflow the tenant
	// already runs, excluding the one this start would create or replay.
	ActiveInstances int
}

// Cost weights: an external capability invocation is the most expensive
// thing a node does, a human task or approval holds a person and durable
// state, and every other node is bookkeeping.
const (
	costCapability  = 3
	costHumanWork   = 2
	costOtherNode   = 1
	costSubworkflow = 5
)

// DemandFor derives a start's structural demand from its compiled plan:
// branches are the widest fan-out any node declares (outgoing edges, or a
// PARALLEL node's branch count), children are SUBWORKFLOW nodes, and cost is
// the weighted node count.
func DemandFor(plan *workflow.CompiledWorkflow, payloadBytes, activeInstances int) (Demand, error) {
	if plan == nil {
		return Demand{}, fmt.Errorf("workload: no compiled plan")
	}
	if payloadBytes < 0 || activeInstances < 0 {
		return Demand{}, fmt.Errorf("workload: negative payload %d or active count %d", payloadBytes, activeInstances)
	}
	d := Demand{PayloadBytes: payloadBytes, ActiveInstances: activeInstances, Branches: 1}
	out := map[string]int{}
	for _, e := range plan.Edges {
		out[e.From]++
	}
	for _, n := range plan.Nodes {
		if out[n.ID] > d.Branches {
			d.Branches = out[n.ID]
		}
		switch n.Type {
		case workflow.StepCapability:
			d.CostUnits += costCapability
		case workflow.StepApproval, workflow.StepTask:
			d.CostUnits += costHumanWork
		case workflow.StepSubworkflow:
			d.Children++
			d.CostUnits += costSubworkflow
		default:
			d.CostUnits += costOtherNode
		}
	}
	return d, nil
}

// Violation is one limit a start exceeds.
type Violation struct {
	Dimension Dimension
	Limit     int
	Demand    int
}

// Verdict is the admission decision and its evidence.
type Verdict struct {
	Outcome         Outcome
	Violations      []Violation
	SnapshotVersion string
	Source          string
}

// Reason renders the violations as one bounded, stable sentence.
func (v Verdict) Reason() string {
	if len(v.Violations) == 0 {
		return string(v.Outcome)
	}
	parts := make([]string, 0, len(v.Violations))
	for _, x := range v.Violations {
		parts = append(parts, string(x.Dimension)+" "+strconv.Itoa(x.Demand)+">"+strconv.Itoa(x.Limit))
	}
	return string(v.Outcome) + ": " + strings.Join(parts, ", ")
}

// Evaluate judges a demand against resolved limits. Any structural violation
// makes the start [OutcomeOverloaded]; only a concurrency violation makes it
// [OutcomeAdmissionDeferred]; otherwise it is [OutcomeAdmitted].
func Evaluate(r Resolved, d Demand) Verdict {
	v := Verdict{Outcome: OutcomeAdmitted, SnapshotVersion: r.SnapshotVersion, Source: r.Source}
	structural := false
	for _, check := range []struct {
		dim           Dimension
		demand, limit int
	}{
		{DimensionBranches, d.Branches, r.Limits.MaxBranches},
		{DimensionChildren, d.Children, r.Limits.MaxChildren},
		{DimensionPayloadBytes, d.PayloadBytes, r.Limits.MaxPayloadBytes},
		{DimensionCostUnits, d.CostUnits, r.Limits.MaxCostUnits},
	} {
		if check.demand > check.limit {
			structural = true
			v.Violations = append(v.Violations, Violation{Dimension: check.dim, Limit: check.limit, Demand: check.demand})
		}
	}
	if d.ActiveInstances+1 > r.Limits.MaxConcurrentInstances {
		v.Violations = append(v.Violations, Violation{Dimension: DimensionConcurrentInstance, Limit: r.Limits.MaxConcurrentInstances, Demand: d.ActiveInstances + 1})
	}
	sort.Slice(v.Violations, func(i, j int) bool { return v.Violations[i].Dimension < v.Violations[j].Dimension })
	switch {
	case structural:
		v.Outcome = OutcomeOverloaded
	case len(v.Violations) > 0:
		v.Outcome = OutcomeAdmissionDeferred
	}
	return v
}
