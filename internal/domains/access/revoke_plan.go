// WF-CAP-003: plan revocation of everything a worker holds.
//
// access.plan_revoke_all enumerates one worker's holdings — sessions,
// credentials, entitlements and badges — and emits an ordered revocation
// plan whose steps dispatch through connector bindings. The access graph
// contributes entitlements and account credentials natively; sessions and
// badges (and any further credential stores) arrive through HoldingSource,
// implemented by their owning domains so this package imports nothing
// outside its kernel. Execution belongs to the capability gateway
// (WF-EXT-005): a step carries its connector binding and operation, and the
// gateway dispatches it; this file plans and verifies, it never calls a
// provider.
//
// Verification reuses the entitlement edge set — the same input the drift
// reconciler will read — by re-enumerating after execution: every planned
// holding must be gone or terminal, and survivors fail by name.
package access

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// HoldingKind is the closed vocabulary of revocable holding kinds.
type HoldingKind string

const (
	HoldingSession     HoldingKind = "SESSION"
	HoldingCredential  HoldingKind = "CREDENTIAL"
	HoldingEntitlement HoldingKind = "ENTITLEMENT"
	HoldingBadge       HoldingKind = "BADGE"
)

func (k HoldingKind) valid() bool {
	switch k {
	case HoldingSession, HoldingCredential, HoldingEntitlement, HoldingBadge:
		return true
	default:
		return false
	}
}

// RevocationOrder is the fixed termination order: live sessions first so
// access stops, then credentials so it cannot resume, then entitlements,
// then physical badges.
func RevocationOrder() []HoldingKind {
	return []HoldingKind{HoldingSession, HoldingCredential, HoldingEntitlement, HoldingBadge}
}

func kindRank(k HoldingKind) int {
	for i, kind := range RevocationOrder() {
		if kind == k {
			return i
		}
	}
	return len(RevocationOrder())
}

// RevokeOperation is the single operation a revocation step performs.
const RevokeOperation = "REVOKE"

// Holding is one revocable thing a worker holds. RecordID names the native
// graph record and is empty for external holdings; Key is the dispatch key
// the connector acts on. ConnectorRef is the binding the step dispatches
// through; GraphOnly holdings revoke by graph state alone because nothing
// was ever provisioned at a provider.
type Holding struct {
	Kind         HoldingKind
	RecordID     string
	Key          string
	System       string
	ConnectorRef string
	GraphOnly    bool
	Lifecycle    Lifecycle
}

func (h Holding) terminal() bool {
	return h.Lifecycle == LifecycleRevoked || h.Lifecycle == LifecycleRetired
}

// HoldingSource enumerates one kind of holdings for one worker. Sources are
// tenant-scoped by their inputs: a source must list only the requested
// tenant and worker, and only its own kind.
type HoldingSource interface {
	Kind() HoldingKind
	ListHoldings(tenant values.TenantId, workerRef string) ([]Holding, error)
}

// RevocationStep is one ordered, dispatchable revocation.
type RevocationStep struct {
	Sequence  int
	Operation string
	Holding   Holding
}

// RevocationPlan is one worker's ordered revocation. GraphDigest binds the
// graph version planned against: a caller that re-plans onto a different
// digest re-plans onto different truth. AlreadyRevoked reports terminal
// holdings so the plan accounts for everything and omits nothing silently.
type RevocationPlan struct {
	Tenant         values.TenantId
	WorkerRef      string
	GraphDigest    string
	Steps          []RevocationStep
	AlreadyRevoked []Holding
	Digest         string
}

// PlanRevokeAll enumerates the worker's holdings from the native graph and
// the external sources and emits the ordered plan. Unknown workers,
// tenant mismatches, duplicated holdings, cross-kind listings and
// connector steps without a binding all fail closed.
func PlanRevokeAll(tenant values.TenantId, workerRef string, graph Graph, external []HoldingSource) (RevocationPlan, error) {
	if strings.TrimSpace(workerRef) == "" {
		return RevocationPlan{}, invalid(RecordWorkforceIdentity, "", "worker_ref", "worker ref is required", ErrInvalidGraph)
	}
	if err := graph.Validate(); err != nil {
		return RevocationPlan{}, err
	}
	if graph.Tenant != tenant {
		return RevocationPlan{}, invalid(RecordGraph, "", "tenant", "graph belongs to another tenant", ErrTenantMismatch)
	}
	var identities []WorkforceIdentity
	for _, ident := range graph.Identities {
		if ident.WorkerRef.Id == workerRef {
			identities = append(identities, ident)
		}
	}
	if len(identities) == 0 {
		return RevocationPlan{}, invalid(RecordWorkforceIdentity, workerRef, "worker_ref", "no identity for worker", ErrUnownedReference)
	}
	owned := make(map[string]struct{}, len(identities))
	for _, ident := range identities {
		owned[ident.ID] = struct{}{}
	}
	accountSource := map[string]string{}
	for _, account := range graph.Accounts {
		if _, ok := owned[account.WorkforceIdentityID]; ok {
			accountSource[account.ID] = account.Source
		}
	}
	var actionable, terminal []Holding
	for _, account := range graph.Accounts {
		if _, ok := owned[account.WorkforceIdentityID]; !ok {
			continue
		}
		// An account link gates authentication to its application: killing
		// it kills re-auth, so native accounts enumerate as credentials.
		h := Holding{Kind: HoldingCredential, RecordID: account.ID, Key: account.AccountID, System: account.System, ConnectorRef: account.Source, Lifecycle: account.Lifecycle}
		if h.terminal() {
			terminal = append(terminal, h)
			continue
		}
		if strings.TrimSpace(h.ConnectorRef) == "" {
			return RevocationPlan{}, invalid(RecordAccountLink, account.ID, "source", "revocable account names no connector binding", ErrInvalidGraph)
		}
		actionable = append(actionable, h)
	}
	for _, edge := range graph.Expected {
		if _, ok := owned[edge.WorkforceIdentityID]; !ok {
			continue
		}
		h := Holding{Kind: HoldingEntitlement, RecordID: edge.ID, Key: edge.ID, System: edge.System, Lifecycle: edge.Lifecycle}
		if h.terminal() {
			terminal = append(terminal, h)
			continue
		}
		if edge.AccountLinkID == "" {
			// Expected but never provisioned: no provider holds it, so
			// revocation is graph state alone.
			h.GraphOnly = true
		} else {
			source, ok := accountSource[edge.AccountLinkID]
			if !ok || strings.TrimSpace(source) == "" {
				return RevocationPlan{}, invalid(RecordExpectedEntitlement, edge.ID, "account_link_id", "provisioned entitlement names no connector binding", ErrInvalidGraph)
			}
			h.ConnectorRef = source
		}
		actionable = append(actionable, h)
	}
	for _, source := range external {
		if source == nil {
			return RevocationPlan{}, invalid(RecordGraph, "", "holding_source", "holding source is nil", ErrInvalidGraph)
		}
		kind := source.Kind()
		if !kind.valid() {
			return RevocationPlan{}, invalid(RecordGraph, "", "holding_source", "holding source names no declared kind", ErrInvalidGraph)
		}
		listed, err := source.ListHoldings(tenant, workerRef)
		if err != nil {
			return RevocationPlan{}, err
		}
		for _, h := range listed {
			if h.Kind != kind {
				return RevocationPlan{}, invalid(RecordGraph, "", "holding_source", fmt.Sprintf("source lists %s outside its kind %s", h.Kind, kind), ErrInvalidGraph)
			}
			if strings.TrimSpace(h.Key) == "" || !h.Lifecycle.Valid() {
				return RevocationPlan{}, invalid(RecordGraph, "", "holding_source", "listed holding is not identified", ErrInvalidGraph)
			}
			if h.terminal() {
				terminal = append(terminal, h)
				continue
			}
			if !h.GraphOnly && strings.TrimSpace(h.ConnectorRef) == "" {
				return RevocationPlan{}, invalid(RecordGraph, "", "holding_source", fmt.Sprintf("holding %s names no connector binding", h.Key), ErrInvalidGraph)
			}
			actionable = append(actionable, h)
		}
	}
	seen := make(map[string]struct{}, len(actionable)+len(terminal))
	for _, h := range actionable {
		key := string(h.Kind) + "\x00" + h.Key
		if _, dup := seen[key]; dup {
			return RevocationPlan{}, invalid(RecordGraph, "", "holding", fmt.Sprintf("holding %s %s is listed twice", h.Kind, h.Key), ErrDuplicateRecord)
		}
		seen[key] = struct{}{}
	}
	for _, h := range terminal {
		key := string(h.Kind) + "\x00" + h.Key
		if _, dup := seen[key]; dup {
			return RevocationPlan{}, invalid(RecordGraph, "", "holding", fmt.Sprintf("holding %s %s is both actionable and revoked", h.Kind, h.Key), ErrDuplicateRecord)
		}
		seen[key] = struct{}{}
	}
	sort.SliceStable(actionable, func(i, j int) bool {
		if actionable[i].Kind != actionable[j].Kind {
			return kindRank(actionable[i].Kind) < kindRank(actionable[j].Kind)
		}
		if actionable[i].System != actionable[j].System {
			return actionable[i].System < actionable[j].System
		}
		return actionable[i].Key < actionable[j].Key
	})
	sort.SliceStable(terminal, func(i, j int) bool {
		if terminal[i].Kind != terminal[j].Kind {
			return kindRank(terminal[i].Kind) < kindRank(terminal[j].Kind)
		}
		return terminal[i].Key < terminal[j].Key
	})
	plan := RevocationPlan{
		Tenant: tenant, WorkerRef: workerRef, GraphDigest: graph.Digest(),
		AlreadyRevoked: terminal,
	}
	for i, h := range actionable {
		plan.Steps = append(plan.Steps, RevocationStep{Sequence: i + 1, Operation: RevokeOperation, Holding: h})
	}
	plan.Digest = plan.computeDigest()
	return plan, nil
}

func (p RevocationPlan) computeDigest() string {
	w := canonicalbytes.New("hcmnext.domains.access.RevocationPlan", 1).
		String("tenant", string(p.Tenant)).
		String("worker_ref", p.WorkerRef).
		String("graph_digest", p.GraphDigest)
	for _, step := range p.Steps {
		w.String("step", fmt.Sprintf("%d|%s|%s|%s|%s|%s|%t|%s",
			step.Sequence, step.Operation, step.Holding.Kind, step.Holding.Key,
			step.Holding.System, step.Holding.ConnectorRef, step.Holding.GraphOnly, step.Holding.Lifecycle))
	}
	for _, h := range p.AlreadyRevoked {
		w.String("revoked", fmt.Sprintf("%s|%s|%s", h.Kind, h.Key, h.Lifecycle))
	}
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Verify reconciles the plan against a fresh enumeration: every planned
// holding must be gone or terminal, and nothing already revoked may have
// resurrected. Survivors fail by name.
func (p RevocationPlan) Verify(current []Holding) error {
	live := make(map[string]Holding, len(current))
	for _, h := range current {
		live[string(h.Kind)+"\x00"+h.Key] = h
	}
	var survivors []string
	for _, step := range p.Steps {
		key := string(step.Holding.Kind) + "\x00" + step.Holding.Key
		if h, ok := live[key]; ok && !h.terminal() {
			survivors = append(survivors, string(step.Holding.Kind)+" "+step.Holding.Key)
		}
	}
	for _, h := range p.AlreadyRevoked {
		key := string(h.Kind) + "\x00" + h.Key
		if live, ok := live[key]; ok && !live.terminal() {
			survivors = append(survivors, "resurrected "+string(h.Kind)+" "+h.Key)
		}
	}
	if len(survivors) > 0 {
		sort.Strings(survivors)
		return invalid(RecordGraph, p.WorkerRef, "holding", fmt.Sprintf("holdings survive revocation: %s", strings.Join(survivors, ", ")), ErrInvalidGraph)
	}
	return nil
}
