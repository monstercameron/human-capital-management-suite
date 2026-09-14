// Package repairworkbench is the bounded product-slice repair workbench
// (ALIGN-052).
//
// An authenticated operator opens the workbench for one product slice and
// sees a bounded, tenant-scoped queue of repair findings -- a stale
// projection, a stuck outbox entry, a stuck workflow node, a reconciliation
// mismatch -- each with only the typed repair actions its kind admits. A
// finding carries opaque references and an evidence digest, never a payload.
//
// The workbench performs no repair itself. Submitting an action re-checks that
// the finding is still in the operator's slice and tenant and that its
// evidence has not changed, then hands a typed request to the governed
// operator gateway (internal/intent/operator), which applies JIT authority,
// dual control, simulation and idempotency and records the receipt. An action
// the finding's kind does not admit, a finding from another slice or tenant,
// or stale evidence is refused before anything reaches the gateway.
package repairworkbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// MaxItems bounds one workbench view. It is not a request field.
const MaxItems = 50

// FindingKind is the closed vocabulary of repairable findings.
type FindingKind string

// Finding kinds.
const (
	KindProjectionStale        FindingKind = "PROJECTION_STALE"
	KindOutboxStuck            FindingKind = "OUTBOX_STUCK"
	KindWorkflowStuck          FindingKind = "WORKFLOW_STUCK"
	KindReconciliationMismatch FindingKind = "RECONCILIATION_MISMATCH"
)

// Severity orders findings; lower rank is more urgent.
type Severity string

// Severities.
const (
	SeverityCritical Severity = "CRITICAL"
	SeverityMajor    Severity = "MAJOR"
	SeverityMinor    Severity = "MINOR"
)

func (s Severity) rank() int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityMajor:
		return 1
	case SeverityMinor:
		return 2
	}
	return -1
}

// ActionsFor is the closed map from finding kind to admitted operator actions.
func ActionsFor(k FindingKind) []operator.Kind {
	switch k {
	case KindProjectionStale:
		return []operator.Kind{operator.KindProjectionRebuild}
	case KindOutboxStuck:
		return []operator.Kind{operator.KindConnectorRedrive}
	case KindWorkflowStuck:
		return []operator.Kind{operator.KindWorkflowRetryNode, operator.KindWorkflowCancel}
	case KindReconciliationMismatch:
		return []operator.Kind{operator.KindDatabaseRepair}
	}
	return nil
}

// Sentinels.
var (
	ErrInvalid       = errors.New("repairworkbench: invalid input")
	ErrUnauthorized  = errors.New("repairworkbench: operator authorization required")
	ErrNotInView     = errors.New("repairworkbench: finding is not in this workbench")
	ErrActionRefused = errors.New("repairworkbench: action is not admitted for this finding")
	ErrStaleEvidence = errors.New("repairworkbench: finding evidence changed since the workbench was opened")
)

// Finding is one repair finding from an owning package.
type Finding struct {
	ID          string          `json:"id"`
	Slice       string          `json:"slice"`
	Tenant      values.TenantId `json:"tenant"`
	Kind        FindingKind     `json:"kind"`
	Severity    Severity        `json:"severity"`
	Resource    string          `json:"resource"`
	TargetRefs  []string        `json:"target_refs"`
	EvidenceRef string          `json:"evidence_ref"`
	ObservedAt  time.Time       `json:"observed_at"`
}

func (f Finding) validate() error {
	if f.ID == "" || f.Slice == "" || f.Resource == "" || f.EvidenceRef == "" || f.Tenant.Validate() != nil ||
		len(f.TargetRefs) == 0 || f.ObservedAt.IsZero() || f.Severity.rank() < 0 || ActionsFor(f.Kind) == nil {
		return fmt.Errorf("%w: finding %q is incomplete or outside the closed vocabulary", ErrInvalid, f.ID)
	}
	return nil
}

// EvidenceDigest identifies a finding's evidence.
func (f Finding) EvidenceDigest() string {
	refs := slices.Clone(f.TargetRefs)
	sort.Strings(refs)
	body, _ := json.Marshal(struct {
		ID, Slice, Kind, Resource, EvidenceRef string
		Tenant                                 string
		Refs                                   []string
		ObservedAt                             string
	}{f.ID, f.Slice, string(f.Kind), f.Resource, f.EvidenceRef, string(f.Tenant), refs, f.ObservedAt.UTC().Format(time.RFC3339Nano)})
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Item is one workbench row.
type Item struct {
	FindingID      string          `json:"finding_id"`
	Kind           FindingKind     `json:"kind"`
	Severity       Severity        `json:"severity"`
	Resource       string          `json:"resource"`
	TargetCount    int             `json:"target_count"`
	EvidenceDigest string          `json:"evidence_digest"`
	Actions        []operator.Kind `json:"actions"`
}

// View is the bounded workbench for one slice.
type View struct {
	ContractVersion int             `json:"contract_version"`
	Tenant          values.TenantId `json:"tenant"`
	Slice           string          `json:"slice"`
	Items           []Item          `json:"items"`
	Truncated       bool            `json:"truncated"`
}

// Digest identifies the view.
func (v View) Digest() string {
	body, _ := json.Marshal(v)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Open builds the workbench for an operator and slice. Findings from other
// tenants or slices are omitted without a count.
func Open(principal *trust.Principal, slice string, findings []Finding) (View, error) {
	if err := admin.RequireOperator(principal); err != nil {
		return View{}, fmt.Errorf("%w: %w", ErrUnauthorized, err)
	}
	if strings.TrimSpace(slice) == "" {
		return View{}, fmt.Errorf("%w: slice is required", ErrInvalid)
	}
	var in []Finding
	for _, f := range findings {
		if err := f.validate(); err != nil {
			return View{}, err
		}
		if f.Tenant == principal.Tenant() && f.Slice == slice {
			in = append(in, f)
		}
	}
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Severity.rank() != in[j].Severity.rank() {
			return in[i].Severity.rank() < in[j].Severity.rank()
		}
		if !in[i].ObservedAt.Equal(in[j].ObservedAt) {
			return in[i].ObservedAt.Before(in[j].ObservedAt)
		}
		return in[i].ID < in[j].ID
	})
	view := View{ContractVersion: 1, Tenant: principal.Tenant(), Slice: slice, Items: []Item{}}
	if len(in) > MaxItems {
		in, view.Truncated = in[:MaxItems], true
	}
	for _, f := range in {
		view.Items = append(view.Items, Item{FindingID: f.ID, Kind: f.Kind, Severity: f.Severity, Resource: f.Resource,
			TargetCount: len(f.TargetRefs), EvidenceDigest: f.EvidenceDigest(), Actions: ActionsFor(f.Kind)})
	}
	return view, nil
}

// Submitter is the governed operator gateway.
type Submitter interface {
	Submit(ctx context.Context, req operator.Request) (operator.Receipt, error)
}

// Action is one submitted repair.
type Action struct {
	FindingID      string
	EvidenceDigest string
	Kind           operator.Kind
	IdempotencyKey string
	Reason         string
	TicketRef      string
	// Governance carries the JIT grant, second approver, simulation or
	// emergency authority; its scope and identity fields are overwritten from
	// the finding.
	Governance operator.Request
}

// Submit re-checks one action against the operator's current view of the
// findings and forwards it to the gateway.
func Submit(ctx context.Context, gw Submitter, principal *trust.Principal, slice string, current []Finding, a Action) (operator.Receipt, error) {
	if gw == nil {
		return operator.Receipt{}, fmt.Errorf("%w: gateway is required", ErrInvalid)
	}
	view, err := Open(principal, slice, current)
	if err != nil {
		return operator.Receipt{}, err
	}
	var finding *Finding
	for i := range current {
		if current[i].ID == a.FindingID && current[i].Tenant == principal.Tenant() && current[i].Slice == slice {
			finding = &current[i]
		}
	}
	inView := false
	for _, item := range view.Items {
		inView = inView || item.FindingID == a.FindingID
	}
	switch {
	case finding == nil || !inView:
		return operator.Receipt{}, ErrNotInView
	case finding.EvidenceDigest() != a.EvidenceDigest:
		return operator.Receipt{}, ErrStaleEvidence
	case !slices.Contains(ActionsFor(finding.Kind), a.Kind):
		return operator.Receipt{}, fmt.Errorf("%w: %s on %s", ErrActionRefused, a.Kind, finding.Kind)
	}
	req := a.Governance
	req.Kind, req.Tenant, req.Operator = a.Kind, principal.Tenant(), principal.Subject()
	req.Scope = operator.Scope{Resource: finding.Resource, IDs: slices.Clone(finding.TargetRefs)}
	req.ExpectedVersion, req.PayloadDigest = finding.EvidenceRef, a.EvidenceDigest
	req.IdempotencyKey, req.Reason, req.TicketRef = a.IdempotencyKey, a.Reason, a.TicketRef
	return gw.Submit(ctx, req)
}
