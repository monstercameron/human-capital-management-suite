package workspace

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
)

// Receipt is one zero-effect receipt as this workspace shows it: the domain's
// own receipt fields, the digests that identify what produced them, and the
// effect counters that make "this changed nothing" checkable.
type Receipt struct {
	Digest string

	WorkerID   string
	WorkerName string

	IntentType     string
	IntentVersion  string
	Mode           string
	RequestState   string
	ExecutionState string
	InputsDigest   string
	ResultDigest   string
	Controls       []string
	Counters       evidence.EffectCounters

	CapabilityID      string
	CapabilityVersion string

	// PlanDigest, WorkflowReceipt and Terminal cite the workflow simulation
	// the approval timeline came from, so the two halves of the page can be
	// replayed together rather than separately.
	PlanDigest      string
	WorkflowReceipt string
	Terminal        string

	GeneratedAt time.Time
}

// maxReceipts bounds the receipt store.
//
// The store is a read cache, not a record: the authoritative artifact is the
// simulation, which is a pure function of the request and can always be
// produced again by re-submitting the form. Bounding it is what keeps a
// long-lived process from turning a page view into unbounded memory.
const maxReceipts = 256

// receiptStore keeps recently rendered receipts addressable by digest.
type receiptStore struct {
	mu    sync.Mutex
	byID  map[string]Receipt
	order []string
}

func newReceiptStore() *receiptStore {
	return &receiptStore{byID: make(map[string]Receipt, maxReceipts)}
}

// put records a receipt, evicting the oldest when the bound is reached.
func (s *receiptStore) put(r Receipt) {
	if r.Digest == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byID[r.Digest]; !exists {
		s.order = append(s.order, r.Digest)
		for len(s.order) > maxReceipts {
			delete(s.byID, s.order[0])
			s.order = s.order[1:]
		}
	}
	s.byID[r.Digest] = r
}

// get returns a recorded receipt.
func (s *receiptStore) get(digest string) (Receipt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byID[digest]
	return r, ok
}

// receiptOf projects a rendered page into the receipt the receipt route
// addresses. The second result is false when the compensation half was never
// disclosed and therefore never simulated: there is no receipt for a
// simulation that did not run, and minting an empty one would be a receipt
// for nothing.
func receiptOf(p Page) (Receipt, bool) {
	if !p.Reading.CompensationDisclosed || p.ReceiptDigest == "" {
		return Receipt{}, false
	}
	domainReceipt := p.Reading.Simulation.Receipt
	controls := make([]string, 0, len(domainReceipt.Controls))
	for _, c := range domainReceipt.Controls {
		controls = append(controls, c.Name+"@"+c.Version)
	}
	return Receipt{
		Digest:            p.ReceiptDigest,
		WorkerID:          p.Reading.Worker.Id,
		WorkerName:        p.Contract.Request.WorkerName,
		IntentType:        domainReceipt.IntentType,
		IntentVersion:     domainReceipt.IntentVersion,
		Mode:              string(domainReceipt.Mode),
		RequestState:      domainReceipt.RequestState,
		ExecutionState:    domainReceipt.ExecutionState,
		InputsDigest:      domainReceipt.InputsDigest,
		ResultDigest:      domainReceipt.ResultDigest,
		Controls:          controls,
		Counters:          p.Reading.Simulation.Effects,
		CapabilityID:      p.Reading.CapabilityID,
		CapabilityVersion: p.Reading.CapabilityVersion,
		PlanDigest:        p.Approvals.PlanDigest,
		WorkflowReceipt:   p.Approvals.ReceiptDigest,
		Terminal:          p.Approvals.Terminal,
		GeneratedAt:       p.Reading.AsOf,
	}, true
}

// ReceiptContract renders one receipt through the same frozen contract the
// workspace itself uses, so the receipt page is the same accessible,
// token-styled document by construction rather than a second surface with its
// own markup and its own accessibility story.
func ReceiptContract(r Receipt) contract.WorkspaceContract {
	readonly := func(id, label, value string) contract.RequestField {
		return contract.RequestField{ID: id, Label: label, Kind: contract.FieldKindReadOnly, Value: value}
	}
	counters := r.Counters
	source := contract.SourceRecord{
		WorkspaceID: "receipt/" + r.Digest,
		Title:       "Zero-effect receipt",
		WorkerID:    r.WorkerID,
		WorkerName:  r.WorkerName,
		AllFields: []contract.RequestField{
			readonly("receiptDigest", "Receipt digest", r.Digest),
			readonly("receiptIntent", "Intent", r.IntentType+"@"+r.IntentVersion),
			readonly("receiptCapability", "Capability", r.CapabilityID+"@"+r.CapabilityVersion),
			readonly("receiptMode", "Mode", r.Mode),
			readonly("receiptRequestState", "Request state", r.RequestState),
			readonly("receiptExecutionState", "Execution state", r.ExecutionState),
			readonly("receiptInputsDigest", "Inputs digest", r.InputsDigest),
			readonly("receiptControls", "Pinned controls", strings.Join(r.Controls, ", ")),
			readonly("receiptWorkflowPlan", "Workflow plan digest", r.PlanDigest),
			readonly("receiptWorkflowReceipt", "Workflow receipt digest", r.WorkflowReceipt),
			readonly("receiptTerminal", "Workflow terminal", r.Terminal),
		},
		Preflight: []contract.PreflightFinding{{
			ID:       "receipt-effects",
			Severity: contract.SeveritySuccess,
			Label:    "Effect counters",
			Detail: fmt.Sprintf(
				"domain writes %d, reservations %d, work items %d, timers %d, messages %d, outbox %d, provider calls %d, approval bindings %d",
				counters.DomainWrites, counters.Reservations, counters.WorkItems, counters.Timers,
				counters.Messages, counters.OutboxEntries, counters.ProviderCalls, counters.ApprovalBindings),
		}},
		Simulation: contract.SimulationResult{
			Status:      contract.SimulationReady,
			Summary:     "This simulation produced no effect of any kind.",
			GeneratedAt: r.GeneratedAt,
			Checks: []contract.SimulationCheck{{
				Label:  "Result digest",
				Status: contract.SeverityInfo,
				Detail: r.ResultDigest,
			}},
		},
		Provenance: contract.Provenance{
			CapabilityID:      r.CapabilityID,
			CapabilityVersion: r.CapabilityVersion,
			SourceSystem:      "hcm-next",
			AsOf:              r.GeneratedAt,
		},
	}
	visible := make(contract.FieldVisibility, len(source.AllFields))
	for _, f := range source.AllFields {
		visible[f.ID] = true
	}
	return contract.NewWorkspaceContract(source, visible, contract.ActionVisibility{})
}
