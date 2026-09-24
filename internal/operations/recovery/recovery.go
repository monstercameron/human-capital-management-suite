// Package recovery owns the pure recovery contracts for authoritative and
// rebuildable platform planes.
//
// The package deliberately keeps planning separate from storage and process
// orchestration. A matrix is useful only when every entry has an owner,
// numeric recovery objectives, an ordering, and semantic checks that can be
// run after bytes have been restored or rebuilt.
package recovery

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Version is the recovery contract version.
func Version() int { return 1 }

// AuthorityClass says whether a plane contains the canonical facts or can be
// derived again from an authoritative source.
type AuthorityClass string

const (
	Authoritative AuthorityClass = "AUTHORITATIVE"
	Rebuildable   AuthorityClass = "REBUILDABLE"
)

// Method is the recovery operation required for a plane.
type Method string

const (
	ModeRestore Method = "RESTORE"
	ModeReplay  Method = "REPLAY"
	ModeRebuild Method = "REBUILD"
)

// RPOClass maps an entry to the recovery classes defined by the platform
// recovery plan. Class targets are evaluated separately from actual minutes.
type RPOClass string

const (
	RPOA    RPOClass = "RPO-A"
	RPOB    RPOClass = "RPO-B"
	RPOC    RPOClass = "RPO-C"
	RPONone RPOClass = "NONE"
)

// Plane identifies one recovery boundary in the production data plane.
type Plane string

const (
	Ledger     Plane = "ledger"
	Artifacts  Plane = "artifacts"
	Config     Plane = "config"
	Keys       Plane = "keys"
	Runtime    Plane = "runtime"
	Outbox     Plane = "outbox"
	Projection Plane = "projection"
	Search     Plane = "search"
	Analytics  Plane = "analytics"
	Cache      Plane = "cache"
)

// Target is a numeric recovery objective in minutes. NotApplicable is only
// valid for an RPO of a demonstrably replayable plane; RTO is always numeric.
type Target struct {
	Minutes       int  `json:"minutes"`
	NotApplicable bool `json:"not_applicable,omitempty"`
}

// Contract is the complete recovery contract for one plane.
type Contract struct {
	Plane           Plane          `json:"plane"`
	Store           string         `json:"store"`
	Owner           string         `json:"owner"`
	Authority       AuthorityClass `json:"authority"`
	Method          Method         `json:"method"`
	RPOClass        RPOClass       `json:"rpo_class"`
	RPO             Target         `json:"rpo"`
	RTO             Target         `json:"rto"`
	Replayable      bool           `json:"replayable"`
	DependencyOrder int            `json:"dependency_order"`
	Dependencies    []Plane        `json:"dependencies"`
	SemanticChecks  []string       `json:"semantic_checks"`
}

// Matrix is an immutable-by-convention collection of recovery contracts.
// NewMatrix copies all slices before validation; callers can therefore keep
// their input data without allowing later edits to change a validated matrix.
type Matrix struct {
	Contracts []Contract
}

var (
	ErrInvalidMatrix   = errors.New("recovery: invalid matrix")
	ErrUnknownPlane    = errors.New("recovery: unknown plane")
	ErrInvalidContract = errors.New("recovery: invalid contract")
)

// DefaultMatrix returns the Gate A matrix for the platform planes. Numeric
// objectives are intentionally conservative pilot targets, not a claim that
// the runtime has already measured those objectives in production.
func DefaultMatrix() Matrix {
	return Matrix{Contracts: []Contract{
		{Plane: Keys, Store: "key-reference-store", Owner: "platform-security", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 30}, DependencyOrder: 1, SemanticChecks: []string{"key references resolve to the pinned version", "decryptability check passes"}},
		{Plane: Config, Store: "configuration-store", Owner: "platform-configuration", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 60}, DependencyOrder: 2, Dependencies: []Plane{Keys}, SemanticChecks: []string{"schema and policy versions are pinned", "configuration digest matches the approved revision"}},
		{Plane: Ledger, Store: "canonical-ledger", Owner: "data-ledger", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 60}, DependencyOrder: 3, Dependencies: []Plane{Keys, Config}, SemanticChecks: []string{"event chain and stream heads are contiguous", "ledger invariants and tenant boundaries pass"}},
		{Plane: Artifacts, Store: "content-addressed-artifacts", Owner: "data-artifacts", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOB, RPO: Target{Minutes: 15}, RTO: Target{Minutes: 120}, DependencyOrder: 4, Dependencies: []Plane{Keys, Config}, SemanticChecks: []string{"content digests and retention metadata match", "classification and tenant references resolve"}},
		{Plane: Runtime, Store: "workflow-runtime-state", Owner: "workflow-runtime", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 60}, DependencyOrder: 5, Dependencies: []Plane{Keys, Config, Ledger}, SemanticChecks: []string{"frontiers, timers and leases satisfy epoch fences", "workflow definitions and instance references resolve"}},
		{Plane: Outbox, Store: "transactional-outbox", Owner: "event-delivery", Authority: Authoritative, Method: ModeRestore, RPOClass: RPOA, RPO: Target{Minutes: 0}, RTO: Target{Minutes: 30}, DependencyOrder: 6, Dependencies: []Plane{Keys, Config, Ledger}, SemanticChecks: []string{"outbox rows remain idempotent against ledger events", "delivery leases are safe to resume"}},
		{Plane: Projection, Store: "critical-projections", Owner: "projection-platform", Authority: Rebuildable, Method: ModeReplay, RPOClass: RPOC, RPO: Target{NotApplicable: true}, RTO: Target{Minutes: 90}, Replayable: true, DependencyOrder: 7, Dependencies: []Plane{Ledger, Config}, SemanticChecks: []string{"replay watermark reaches the ledger head", "projection digest matches the reference conformance check"}},
		{Plane: Search, Store: "search-index", Owner: "search-platform", Authority: Rebuildable, Method: ModeRebuild, RPOClass: RPOC, RPO: Target{NotApplicable: true}, RTO: Target{Minutes: 180}, Replayable: true, DependencyOrder: 8, Dependencies: []Plane{Ledger, Artifacts, Config}, SemanticChecks: []string{"index is rebuilt only from authorized source rows", "document and field digests match the source snapshot"}},
		{Plane: Analytics, Store: "analytics-views", Owner: "analytics-platform", Authority: Rebuildable, Method: ModeRebuild, RPOClass: RPOC, RPO: Target{NotApplicable: true}, RTO: Target{Minutes: 240}, Replayable: true, DependencyOrder: 9, Dependencies: []Plane{Ledger, Projection}, SemanticChecks: []string{"source watermark and schema version are recorded", "aggregate counts and sample values reconcile"}},
		{Plane: Cache, Store: "runtime-cache", Owner: "runtime-platform", Authority: Rebuildable, Method: ModeRebuild, RPOClass: RPONone, RPO: Target{NotApplicable: true}, RTO: Target{Minutes: 30}, Replayable: false, DependencyOrder: 10, Dependencies: []Plane{Projection}, SemanticChecks: []string{"cache is empty or derived after the source is healthy", "tenant and authorization keys are scoped"}},
	}}
}

// NewMatrix copies contracts and validates their graph and objectives.
func NewMatrix(contracts []Contract) (Matrix, error) {
	m := Matrix{Contracts: cloneContracts(contracts)}
	if err := m.Validate(); err != nil {
		return Matrix{}, err
	}
	return m, nil
}

// Validate checks that every recovery entry is complete and that the
// dependency order is a valid topological order.
func (m Matrix) Validate() error {
	if len(m.Contracts) == 0 {
		return fmt.Errorf("%w: no contracts", ErrInvalidMatrix)
	}
	byPlane := make(map[Plane]Contract, len(m.Contracts))
	byOrder := make(map[int]Plane, len(m.Contracts))
	for _, c := range m.Contracts {
		if _, exists := byPlane[c.Plane]; exists {
			return fmt.Errorf("%w: duplicate plane %q", ErrInvalidMatrix, c.Plane)
		}
		if c.Plane == "" || strings.TrimSpace(c.Store) == "" || strings.TrimSpace(c.Owner) == "" {
			return fmt.Errorf("%w: plane, store and owner are required", ErrInvalidContract)
		}
		if c.Authority != Authoritative && c.Authority != Rebuildable {
			return fmt.Errorf("%w: plane %q has authority %q", ErrInvalidContract, c.Plane, c.Authority)
		}
		if c.Method != ModeRestore && c.Method != ModeReplay && c.Method != ModeRebuild {
			return fmt.Errorf("%w: plane %q has method %q", ErrInvalidContract, c.Plane, c.Method)
		}
		if c.RPOClass != RPOA && c.RPOClass != RPOB && c.RPOClass != RPOC && c.RPOClass != RPONone {
			return fmt.Errorf("%w: plane %q has RPO class %q", ErrInvalidContract, c.Plane, c.RPOClass)
		}
		if c.RTO.NotApplicable || c.RTO.Minutes <= 0 {
			return fmt.Errorf("%w: plane %q needs a positive numeric RTO", ErrInvalidContract, c.Plane)
		}
		if c.RPO.NotApplicable {
			if c.RPO.Minutes != 0 || c.Authority != Rebuildable || (c.Method != ModeReplay && c.Method != ModeRebuild) ||
				(c.RPOClass == RPOC && !c.Replayable) || (c.RPOClass == RPONone && c.Replayable) ||
				(c.RPOClass != RPOC && c.RPOClass != RPONone) {
				return fmt.Errorf("%w: plane %q marks a non-replayable RPO as N/A", ErrInvalidContract, c.Plane)
			}
		} else if c.RPO.Minutes < 0 {
			return fmt.Errorf("%w: plane %q has a negative RPO", ErrInvalidContract, c.Plane)
		}
		if (c.RPOClass == RPOA && (c.RPO.NotApplicable || c.RPO.Minutes != 0)) ||
			(c.RPOClass == RPOB && (c.RPO.NotApplicable || c.RPO.Minutes <= 0)) ||
			(c.RPOClass == RPOC && (!c.RPO.NotApplicable || !c.Replayable)) ||
			(c.RPOClass == RPONone && !c.RPO.NotApplicable) {
			return fmt.Errorf("%w: plane %q RPO target does not satisfy class %q", ErrInvalidContract, c.Plane, c.RPOClass)
		}
		if c.Authority == Authoritative && c.Method != ModeRestore {
			return fmt.Errorf("%w: authoritative plane %q must be restored", ErrInvalidContract, c.Plane)
		}
		if c.Authority == Rebuildable && c.Method == ModeRestore {
			return fmt.Errorf("%w: rebuildable plane %q cannot use RESTORE", ErrInvalidContract, c.Plane)
		}
		if c.DependencyOrder <= 0 {
			return fmt.Errorf("%w: plane %q has no dependency order", ErrInvalidContract, c.Plane)
		}
		if _, exists := byOrder[c.DependencyOrder]; exists {
			return fmt.Errorf("%w: duplicate dependency order %d", ErrInvalidMatrix, c.DependencyOrder)
		}
		if len(c.SemanticChecks) == 0 {
			return fmt.Errorf("%w: plane %q has no semantic verification", ErrInvalidContract, c.Plane)
		}
		for _, check := range c.SemanticChecks {
			if strings.TrimSpace(check) == "" {
				return fmt.Errorf("%w: plane %q has an empty semantic check", ErrInvalidContract, c.Plane)
			}
		}
		byPlane[c.Plane] = c
		byOrder[c.DependencyOrder] = c.Plane
	}
	for _, c := range m.Contracts {
		for _, dependency := range c.Dependencies {
			dep, ok := byPlane[dependency]
			if !ok {
				return fmt.Errorf("%w: plane %q depends on %q", ErrUnknownPlane, c.Plane, dependency)
			}
			if dep.DependencyOrder >= c.DependencyOrder {
				return fmt.Errorf("%w: plane %q depends on later plane %q", ErrInvalidMatrix, c.Plane, dependency)
			}
		}
	}
	return nil
}

// Contract returns a copy of the contract for plane.
func (m Matrix) Contract(plane Plane) (Contract, bool) {
	for _, c := range m.Contracts {
		if c.Plane == plane {
			return cloneContract(c), true
		}
	}
	return Contract{}, false
}

// Ordered returns contracts in recovery order without exposing the matrix's
// backing slice.
func (m Matrix) Ordered() []Contract {
	contracts := cloneContracts(m.Contracts)
	sort.Slice(contracts, func(i, j int) bool { return contracts[i].DependencyOrder < contracts[j].DependencyOrder })
	return contracts
}

// Explain renders a bounded, deterministic matrix summary without payloads.
func (m Matrix) Explain() string {
	ordered := m.Ordered()
	parts := make([]string, 0, len(ordered))
	for _, c := range ordered {
		rpo := fmt.Sprintf("%dm", c.RPO.Minutes)
		if c.RPO.NotApplicable {
			rpo = "N/A"
		}
		parts = append(parts, fmt.Sprintf("%d:%s[%s/%s class=%s rpo=%s rto=%dm owner=%s checks=%d]", c.DependencyOrder, c.Plane, c.Authority, c.Method, c.RPOClass, rpo, c.RTO.Minutes, c.Owner, len(c.SemanticChecks)))
	}
	return "recovery matrix v1 " + strings.Join(parts, "; ")
}

func cloneContracts(in []Contract) []Contract {
	out := make([]Contract, len(in))
	for i, c := range in {
		out[i] = cloneContract(c)
	}
	return out
}

func cloneContract(c Contract) Contract {
	c.Dependencies = append([]Plane(nil), c.Dependencies...)
	c.SemanticChecks = append([]string(nil), c.SemanticChecks...)
	return c
}
