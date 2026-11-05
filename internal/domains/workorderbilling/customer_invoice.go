// Customer invoices and conserved receivables for SMB-BILL-001.
//
// BillingDraft artifacts stay drafts and status events: they never become
// customer invoices on their own. This file adds the explicit
// CustomerInvoice lifecycle (DRAFT/APPROVAL_PENDING/READY/ISSUED/VOIDED)
// with per-issuer/series numbering consumed at issue, and a
// ReceivableLedger that derives the OPEN/PARTIAL/PAID/OVERDUE/DISPUTED
// collection projection from signed, idempotent receivable events.
// Over-application settles into explicit unapplied customer credit; it is
// never silently dropped or netted away.
package workorderbilling

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Invoice errors. Content refusals reuse ErrRejected through fail(); the
// errors below discriminate lookup, concurrency and authorization outcomes.
var (
	// ErrInvoiceNotFound is the neutral response for unknown IDs, guessed
	// IDs and cross-tenant references alike.
	ErrInvoiceNotFound = errors.New("work order billing: invoice not found or denied")
	// ErrInvoiceConflict reports stale expected revisions and idempotency
	// keys reused with changed payloads.
	ErrInvoiceConflict = errors.New("work order billing: invoice revision or key conflict")
	// ErrInvoiceDenied reports missing capability grants, illegal lifecycle
	// transitions and non-local issuance attempts.
	ErrInvoiceDenied = errors.New("work order billing: invoice action denied")
)

// InvoiceStatus is the conserved invoice lifecycle. Payment, delivery,
// invoice validity and accounting-sync states stay independent: no UI may
// equate workorderbilling Issued with settled customer payment.
type InvoiceStatus string

const (
	InvoiceDraft           InvoiceStatus = "DRAFT"
	InvoiceApprovalPending InvoiceStatus = "APPROVAL_PENDING"
	InvoiceReady           InvoiceStatus = "READY"
	InvoiceIssued          InvoiceStatus = "ISSUED"
	InvoiceVoided          InvoiceStatus = "VOIDED"
)

// InvoiceSourceKind is the typed source union. Standalone manual invoices
// carry an explicit commercial snapshot plus authorized evidence and never
// a fabricated work order reference.
type InvoiceSourceKind string

const (
	InvoiceSourceWorkOrder InvoiceSourceKind = "work-order"
	InvoiceSourceManual    InvoiceSourceKind = "manual"
)

// ManualSource is the explicit commercial snapshot for a standalone
// customer invoice with no operational source.
type ManualSource struct {
	RateRef       string `json:"rate_ref"`
	PolicyRef     string `json:"policy_ref"`
	PolicyVersion string `json:"policy_version"`
	Evidence      string `json:"evidence"`
}

// InvoiceSource binds an invoice to its billable origin. A BillingDraft
// marked Issued creates no invoice unless an explicit migrated link with
// evidence exists.
type InvoiceSource struct {
	Kind              InvoiceSourceKind `json:"kind"`
	WorkOrderID       string            `json:"work_order_id,omitempty"`
	MigratedDraftID   string            `json:"migrated_draft_id,omitempty"`
	MigrationEvidence string            `json:"migration_evidence,omitempty"`
	Manual            ManualSource      `json:"manual,omitempty"`
}

// InvoiceLine is one frozen commercial line. Amounts are payable values in
// Money and cannot be patched after issue.
type InvoiceLine struct {
	ID          string
	Description string
	Quantity    values.Decimal
	UnitPrice   values.Money
	Amount      values.Money
	RateRef     string
	PolicyRef   string
}

// InvoiceEvent is one idempotent lifecycle decision.
type InvoiceEvent struct{ Type, Actor, At, Key, Detail string }

// CustomerInvoice is the conserved customer receivable record. Total is
// always the exact line sum; the digest freezes content at issue.
type CustomerInvoice struct {
	ID            string
	TenantID      string
	IssuerEntity  string
	IssuerSeries  string
	InvoiceNumber string
	CustomerID    string
	Authority     string
	Source        InvoiceSource
	Currency      string
	Lines         []InvoiceLine
	Total         values.Money
	TaxPolicyRef  string
	IssueDate     string
	DueDate       string
	Status        InvoiceStatus
	Revision      uint64
	Events        []InvoiceEvent
	Digest        string
}

// ActorCaps are the explicit grants a command checks before mutating. The
// domain takes them as parameters; role resolution stays with callers.
type ActorCaps struct {
	CanSubmit  bool
	CanApprove bool
	CanIssue   bool
	CanVoid    bool
}

// InvoiceReceipt is the idempotent result of one command. Replaying the
// same tenant-scoped key with the same payload returns the original
// receipt; a reused key with a changed payload is a conflict.
type InvoiceReceipt struct {
	Key       string `json:"key"`
	TenantID  string `json:"tenant_id"`
	InvoiceID string `json:"invoice_id"`
	Revision  uint64 `json:"revision"`
	Number    string `json:"number"`
	Action    string `json:"action"`
	Payload   string `json:"payload"`
}

// DraftInvoiceRequest drafts one invoice. The tenant is taken from the
// caller, never from the request body.
type DraftInvoiceRequest struct {
	InvoiceID    string
	IssuerEntity string
	IssuerSeries string
	CustomerID   string
	Authority    string
	Source       InvoiceSource
	Currency     string
	Lines        []InvoiceLine
	TaxPolicyRef string
	IssueDate    string
	DueDate      string
}

// LocalInvoiceAuthority is the only authority that may issue locally.
// External masters are observations with origin/version.
const LocalInvoiceAuthority = "local"

func validInvoiceAuthority(authority string) bool {
	if authority == LocalInvoiceAuthority {
		return true
	}
	parts := strings.Split(authority, ":")
	return len(parts) == 3 && parts[0] == "external" && parts[1] != "" && parts[2] != ""
}

func moneyText(m values.Money) string {
	raw, err := m.MarshalText()
	if err != nil {
		return ""
	}
	return string(raw)
}

func zeroMoneyLike(obligation values.Money) (values.Money, error) {
	return values.NewMoney("0", obligation.Currency(), obligation.Amount().Scale(), obligation.Amount().Rounding())
}

// Validate verifies identity, source union, line amounts, lifecycle shape,
// dates and the content digest. When a line carries both a valid quantity
// and a valid unit price, the extended amount must match exactly; lines
// without them are manual-evidence amounts carried as recorded. Exported
// fields allow serialization, so a previously valid digest never excuses a
// mutated value.
func (inv CustomerInvoice) Validate() error {
	if strings.TrimSpace(inv.ID) == "" || strings.TrimSpace(inv.TenantID) == "" {
		return fail("invoice identity and tenant are required")
	}
	if strings.TrimSpace(inv.IssuerEntity) == "" || strings.TrimSpace(inv.IssuerSeries) == "" {
		return fail("issuer legal entity and series are required")
	}
	if strings.TrimSpace(inv.CustomerID) == "" {
		return fail("invoice customer is required")
	}
	if !validInvoiceAuthority(inv.Authority) {
		return fail("unknown invoicing authority %q", inv.Authority)
	}
	if _, err := values.NewMoney("0", inv.Currency, 2, values.RoundingExactRequired); err != nil {
		return fail("invoice currency: %v", err)
	}
	switch inv.Status {
	case InvoiceDraft, InvoiceApprovalPending, InvoiceReady, InvoiceIssued, InvoiceVoided:
	default:
		return fail("unknown invoice status %q", inv.Status)
	}
	if err := inv.Source.validate(); err != nil {
		return err
	}
	if len(inv.Lines) == 0 {
		return fail("invoice has no lines")
	}
	scale := inv.Lines[0].Amount.Amount().Scale()
	total, err := values.NewMoneyFromDecimal(mustZeroDecimal(scale), inv.Currency)
	if err != nil {
		return fail("initialize invoice total: %v", err)
	}
	for _, line := range inv.Lines {
		if strings.TrimSpace(line.ID) == "" || strings.TrimSpace(line.RateRef) == "" || strings.TrimSpace(line.PolicyRef) == "" {
			return fail("line identity and rate/policy refs are required")
		}
		if err := line.Amount.Validate(); err != nil || line.Amount.Currency() != inv.Currency {
			return fail("invalid line amount or currency")
		}
		if line.Amount.Amount().Scale() != scale {
			return fail("line scales must match the invoice scale")
		}
		if line.Amount.Amount().Sign() <= 0 {
			return fail("line amounts must be positive")
		}
		if line.Quantity.Validate() == nil && line.UnitPrice.Validate() == nil && line.UnitPrice.Currency() == inv.Currency {
			extended, err := line.Quantity.Mul(line.UnitPrice.Amount(), line.Amount.Amount().Scale(), line.Amount.Amount().Rounding())
			if err != nil || !extended.Equal(line.Amount.Amount()) {
				return fail("line amount does not match its quantity and unit price")
			}
		}
		total, err = total.Add(line.Amount)
		if err != nil {
			return fail("sum invoice lines: %v", err)
		}
	}
	if err := inv.Total.Validate(); err != nil || inv.Total.Currency() != inv.Currency {
		return fail("invalid invoice total or currency")
	}
	if !total.Amount().Equal(inv.Total.Amount()) {
		return fail("invoice total does not equal line sum")
	}
	if inv.Total.Amount().Sign() <= 0 {
		return fail("invoice must obligate a positive amount")
	}
	if inv.Revision == 0 {
		return fail("invoice revision is required")
	}
	if inv.IssueDate != "" {
		if _, err := time.Parse("2006-01-02", inv.IssueDate); err != nil {
			return fail("issue date is not a calendar date")
		}
	}
	if inv.DueDate != "" {
		due, err := time.Parse("2006-01-02", inv.DueDate)
		if err != nil {
			return fail("due date is not a calendar date")
		}
		if inv.IssueDate != "" {
			issued, _ := time.Parse("2006-01-02", inv.IssueDate)
			if due.Before(issued) {
				return fail("due date precedes the issue date")
			}
		}
	}
	switch inv.Status {
	case InvoiceDraft, InvoiceApprovalPending, InvoiceReady:
		if inv.InvoiceNumber != "" {
			return fail("unissued invoice must not carry a number")
		}
	case InvoiceIssued, InvoiceVoided:
		if _, err := parseInvoiceNumber(inv.IssuerSeries, inv.InvoiceNumber); err != nil {
			return fail("issued invoice number is malformed: %v", err)
		}
	}
	if inv.Digest == "" || inv.Digest != digestInvoice(inv) {
		return fail("invoice content digest mismatch")
	}
	return nil
}

// parseInvoiceNumber splits a consumed number into its series sequence. The
// number must be exactly the series plus a zero-padded six-digit sequence,
// so a forged or misfiled number never validates.
func parseInvoiceNumber(series, number string) (uint64, error) {
	rest, ok := strings.CutPrefix(number, series+"-")
	if !ok || len(rest) != 6 {
		return 0, fmt.Errorf("want %q plus six digits", series+"-")
	}
	seq, err := strconv.ParseUint(rest, 10, 64)
	if err != nil || seq == 0 || fmt.Sprintf("%s-%06d", series, seq) != number {
		return 0, fmt.Errorf("sequence is not a positive six-digit counter")
	}
	return seq, nil
}

func mustZeroDecimal(scale int32) values.Decimal {
	zero, err := values.NewDecimal("0", scale, values.RoundingExactRequired)
	if err != nil {
		panic("workorderbilling: zero decimal must always parse: " + err.Error())
	}
	return zero
}

func (src InvoiceSource) validate() error {
	switch src.Kind {
	case InvoiceSourceManual:
		if src.WorkOrderID != "" || src.MigratedDraftID != "" || src.MigrationEvidence != "" {
			return fail("manual invoice must not reference operational sources")
		}
		manual := src.Manual
		if strings.TrimSpace(manual.RateRef) == "" || strings.TrimSpace(manual.PolicyRef) == "" || strings.TrimSpace(manual.PolicyVersion) == "" {
			return fail("manual invoice requires an explicit line/rate/policy snapshot")
		}
		if strings.TrimSpace(manual.Evidence) == "" {
			return fail("manual invoice requires authorized evidence")
		}
	case InvoiceSourceWorkOrder:
		if strings.TrimSpace(src.WorkOrderID) == "" {
			return fail("work-order invoice requires its work order reference")
		}
		if src.Manual != (ManualSource{}) {
			return fail("work-order invoice must not carry a manual snapshot")
		}
		if src.MigratedDraftID != "" && strings.TrimSpace(src.MigrationEvidence) == "" {
			return fail("migrated draft link requires evidence")
		}
	default:
		return fail("unknown invoice source kind %q", src.Kind)
	}
	return nil
}

func digestInvoice(inv CustomerInvoice) string {
	fields := []string{inv.ID, inv.TenantID, inv.IssuerEntity, inv.IssuerSeries, inv.InvoiceNumber,
		inv.CustomerID, inv.Authority, string(inv.Source.Kind), inv.Source.WorkOrderID,
		inv.Source.MigratedDraftID, inv.Source.MigrationEvidence,
		inv.Source.Manual.RateRef, inv.Source.Manual.PolicyRef, inv.Source.Manual.PolicyVersion,
		inv.Source.Manual.Evidence, inv.Currency, string(inv.Status), fmt.Sprint(inv.Revision)}
	for _, line := range inv.Lines {
		fields = append(fields, line.ID, line.Description, line.Quantity.String(),
			moneyText(line.UnitPrice), moneyText(line.Amount), line.RateRef, line.PolicyRef)
	}
	fields = append(fields, moneyText(inv.Total), inv.TaxPolicyRef, inv.IssueDate, inv.DueDate)
	var b strings.Builder
	for _, field := range fields {
		fmt.Fprintf(&b, "%d:%s", len(field), field)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func hashPayload(fields ...string) string {
	var b strings.Builder
	for _, field := range fields {
		fmt.Fprintf(&b, "%d:%s", len(field), field)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// InvoiceStore is the concurrency-safe in-memory invoice port. State leaves
// through Export snapshots; durable revision storage stays with the
// existing workorder persistence boundary.
type InvoiceStore struct {
	mu       sync.Mutex
	invoices map[string]*CustomerInvoice
	numbers  map[string]uint64
	receipts map[string]storedReceipt
	taxGate  TaxIssueGate
}

type storedReceipt struct {
	Receipt InvoiceReceipt
	Result  CustomerInvoice
}

// NewInvoiceStore returns an empty store.
func NewInvoiceStore() *InvoiceStore {
	return &InvoiceStore{
		invoices: make(map[string]*CustomerInvoice),
		numbers:  make(map[string]uint64),
		receipts: make(map[string]storedReceipt),
	}
}

func invoiceKey(tenant, id string) string { return tenant + "\x00" + id }

func (s *InvoiceStore) replayOrConflict(tenant, key, payload string) (CustomerInvoice, InvoiceReceipt, bool, error) {
	stored, ok := s.receipts[tenant+"\x00"+key]
	if !ok {
		return CustomerInvoice{}, InvoiceReceipt{}, false, nil
	}
	if stored.Receipt.Payload != payload {
		return CustomerInvoice{}, InvoiceReceipt{}, true, fmt.Errorf("%w: idempotency key reused with a changed payload", ErrInvoiceConflict)
	}
	return stored.Result, stored.Receipt, true, nil
}

func (s *InvoiceStore) record(tenant, key string, receipt InvoiceReceipt, result CustomerInvoice) {
	s.receipts[tenant+"\x00"+key] = storedReceipt{Receipt: receipt, Result: result}
}

func copyInvoice(inv CustomerInvoice) CustomerInvoice {
	inv.Lines = append([]InvoiceLine(nil), inv.Lines...)
	inv.Events = append([]InvoiceEvent(nil), inv.Events...)
	return inv
}

// Draft records a DRAFT invoice. Unknown authorities are refused; only the
// local authority may later issue.
func (s *InvoiceStore) Draft(tenant, actor, key string, req DraftInvoiceRequest) (CustomerInvoice, InvoiceReceipt, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(actor) == "" || strings.TrimSpace(key) == "" {
		return CustomerInvoice{}, InvoiceReceipt{}, fail("tenant, actor, and idempotency key are required")
	}
	if strings.TrimSpace(req.InvoiceID) == "" {
		return CustomerInvoice{}, InvoiceReceipt{}, fail("invoice id is required")
	}
	var lineFields []string
	for _, line := range req.Lines {
		lineFields = append(lineFields, line.ID, line.Description, line.Quantity.String(),
			moneyText(line.UnitPrice), moneyText(line.Amount), line.RateRef, line.PolicyRef)
	}
	payload := hashPayload("draft", tenant, req.InvoiceID, req.IssuerEntity, req.IssuerSeries,
		req.CustomerID, req.Authority, string(req.Source.Kind), req.Source.WorkOrderID,
		req.Source.MigratedDraftID, req.Source.MigrationEvidence, req.Source.Manual.RateRef,
		req.Source.Manual.PolicyRef, req.Source.Manual.PolicyVersion, req.Source.Manual.Evidence,
		req.Currency, req.TaxPolicyRef, req.IssueDate, req.DueDate, actor, strings.Join(lineFields, "\x01"))
	s.mu.Lock()
	defer s.mu.Unlock()
	if inv, receipt, done, err := s.replayOrConflict(tenant, key, payload); done || err != nil {
		return inv, receipt, err
	}
	if _, exists := s.invoices[invoiceKey(tenant, req.InvoiceID)]; exists {
		return CustomerInvoice{}, InvoiceReceipt{}, fmt.Errorf("%w: invoice %q already exists", ErrInvoiceConflict, req.InvoiceID)
	}
	inv := CustomerInvoice{
		ID: req.InvoiceID, TenantID: tenant, IssuerEntity: strings.TrimSpace(req.IssuerEntity),
		IssuerSeries: strings.TrimSpace(req.IssuerSeries), CustomerID: strings.TrimSpace(req.CustomerID),
		Authority: req.Authority, Source: req.Source, Currency: req.Currency,
		Lines:        append([]InvoiceLine(nil), req.Lines...),
		TaxPolicyRef: req.TaxPolicyRef, IssueDate: req.IssueDate, DueDate: req.DueDate,
		Status: InvoiceDraft, Revision: 1,
		Events: []InvoiceEvent{{Type: "draft", Actor: actor, Key: key}},
	}
	if len(inv.Lines) == 0 {
		return CustomerInvoice{}, InvoiceReceipt{}, fail("invoice has no lines")
	}
	scale := inv.Lines[0].Amount.Amount().Scale()
	total, err := values.NewMoneyFromDecimal(mustZeroDecimal(scale), inv.Currency)
	if err != nil {
		return CustomerInvoice{}, InvoiceReceipt{}, fail("initialize invoice total: %v", err)
	}
	for _, line := range inv.Lines {
		total, err = total.Add(line.Amount)
		if err != nil {
			return CustomerInvoice{}, InvoiceReceipt{}, fail("sum invoice lines: %v", err)
		}
	}
	inv.Total = total
	inv.Digest = digestInvoice(inv)
	if err := inv.Validate(); err != nil {
		return CustomerInvoice{}, InvoiceReceipt{}, err
	}
	stored := copyInvoice(inv)
	s.invoices[invoiceKey(tenant, inv.ID)] = &stored
	receipt := InvoiceReceipt{Key: key, TenantID: tenant, InvoiceID: inv.ID, Revision: inv.Revision, Action: "draft", Payload: payload}
	s.record(tenant, key, receipt, copyInvoice(inv))
	return copyInvoice(inv), receipt, nil
}

// transition runs one lifecycle command: neutral lookup, revision
// compare-and-swap, capability check, then the state-specific mutation.
func (s *InvoiceStore) transition(tenant, actor string, allowed bool, action string, id string, expected uint64, key, at, detail string, mutate func(*CustomerInvoice) error) (CustomerInvoice, InvoiceReceipt, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(actor) == "" || strings.TrimSpace(key) == "" {
		return CustomerInvoice{}, InvoiceReceipt{}, fail("tenant, actor, and idempotency key are required")
	}
	payload := hashPayload(action, tenant, id, fmt.Sprint(expected), actor, at, detail)
	s.mu.Lock()
	defer s.mu.Unlock()
	if inv, receipt, done, err := s.replayOrConflict(tenant, key, payload); done || err != nil {
		return inv, receipt, err
	}
	stored, ok := s.invoices[invoiceKey(tenant, id)]
	if !ok {
		return CustomerInvoice{}, InvoiceReceipt{}, ErrInvoiceNotFound
	}
	if expected != stored.Revision {
		return CustomerInvoice{}, InvoiceReceipt{}, fmt.Errorf("%w: expected revision %d, current is %d", ErrInvoiceConflict, expected, stored.Revision)
	}
	if !allowed {
		return CustomerInvoice{}, InvoiceReceipt{}, fmt.Errorf("%w: actor lacks the %s grant", ErrInvoiceDenied, action)
	}
	next := copyInvoice(*stored)
	if err := mutate(&next); err != nil {
		return CustomerInvoice{}, InvoiceReceipt{}, err
	}
	next.Revision++
	next.Events = append(next.Events, InvoiceEvent{Type: action, Actor: actor, At: at, Key: key, Detail: detail})
	next.Digest = digestInvoice(next)
	if err := next.Validate(); err != nil {
		return CustomerInvoice{}, InvoiceReceipt{}, err
	}
	*s.invoices[invoiceKey(tenant, id)] = copyInvoice(next)
	receipt := InvoiceReceipt{Key: key, TenantID: tenant, InvoiceID: id, Revision: next.Revision, Number: next.InvoiceNumber, Action: action, Payload: payload}
	s.record(tenant, key, receipt, copyInvoice(next))
	return copyInvoice(next), receipt, nil
}

// SubmitForApproval moves DRAFT to APPROVAL_PENDING.
func (s *InvoiceStore) SubmitForApproval(tenant, actor string, caps ActorCaps, id string, expected uint64, key, at string) (CustomerInvoice, InvoiceReceipt, error) {
	return s.transition(tenant, actor, caps.CanSubmit, "submit", id, expected, key, at, "", func(next *CustomerInvoice) error {
		if next.Status != InvoiceDraft {
			return fmt.Errorf("%w: only DRAFT invoices can be submitted", ErrInvoiceDenied)
		}
		next.Status = InvoiceApprovalPending
		return nil
	})
}

// Approve moves APPROVAL_PENDING to READY.
func (s *InvoiceStore) Approve(tenant, actor string, caps ActorCaps, id string, expected uint64, key, at string) (CustomerInvoice, InvoiceReceipt, error) {
	return s.transition(tenant, actor, caps.CanApprove, "approve", id, expected, key, at, "", func(next *CustomerInvoice) error {
		if next.Status != InvoiceApprovalPending {
			return fmt.Errorf("%w: only APPROVAL_PENDING invoices can be approved", ErrInvoiceDenied)
		}
		next.Status = InvoiceReady
		return nil
	})
}

// Issue moves READY to ISSUED and consumes one per-issuer/series number.
// Only the local authority issues; external masters stay observations.
// Numbers are never reused: rollback leaves explained gaps.
func (s *InvoiceStore) Issue(tenant, actor string, caps ActorCaps, id string, expected uint64, key, at string) (CustomerInvoice, InvoiceReceipt, error) {
	return s.transition(tenant, actor, caps.CanIssue, "issue", id, expected, key, at, "", func(next *CustomerInvoice) error {
		if next.Status != InvoiceReady {
			return fmt.Errorf("%w: only READY invoices can be issued", ErrInvoiceDenied)
		}
		if next.Authority != LocalInvoiceAuthority {
			return fmt.Errorf("%w: authority %q cannot issue locally", ErrInvoiceDenied, next.Authority)
		}
		seriesKey := tenant + "\x00" + next.IssuerEntity + "\x00" + next.IssuerSeries
		if s.taxGate != nil {
			if err := s.taxGate.ValidateForIssue(tenant, *next, at); err != nil {
				return err
			}
		}
		s.numbers[seriesKey]++
		next.InvoiceNumber = fmt.Sprintf("%s-%06d", next.IssuerSeries, s.numbers[seriesKey])
		next.Status = InvoiceIssued
		return nil
	})
}

// Void moves ISSUED to VOIDED with a mandatory reason. The numbered artifact
// and its amounts stay conserved for history.
func (s *InvoiceStore) Void(tenant, actor string, caps ActorCaps, id string, expected uint64, key, at, reason string) (CustomerInvoice, InvoiceReceipt, error) {
	if strings.TrimSpace(reason) == "" {
		return CustomerInvoice{}, InvoiceReceipt{}, fail("void reason is required")
	}
	return s.transition(tenant, actor, caps.CanVoid, "void", id, expected, key, at, reason, func(next *CustomerInvoice) error {
		if next.Status != InvoiceIssued {
			return fmt.Errorf("%w: only ISSUED invoices can be voided", ErrInvoiceDenied)
		}
		next.Status = InvoiceVoided
		return nil
	})
}

// Get returns the conserved record or the neutral not-found response.
func (s *InvoiceStore) Get(tenant, id string) (CustomerInvoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.invoices[invoiceKey(tenant, id)]
	if !ok {
		return CustomerInvoice{}, ErrInvoiceNotFound
	}
	return copyInvoice(*stored), nil
}

// FindByDraft locates the invoice carrying an explicit migrated-draft link.
// Drafts alone never satisfy this: only evidence-backed links resolve.
func (s *InvoiceStore) FindByDraft(tenant, draftID string) (CustomerInvoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, stored := range s.invoices {
		if !strings.HasPrefix(key, tenant+"\x00") {
			continue
		}
		if stored.Source.MigratedDraftID == draftID && draftID != "" {
			return copyInvoice(*stored), nil
		}
	}
	return CustomerInvoice{}, ErrInvoiceNotFound
}

// MarshalJSON renders Money fields as canonical text so snapshots stay
// compatible with ordinary JSON persistence.
func (l InvoiceLine) MarshalJSON() ([]byte, error) {
	type wire InvoiceLine
	raw := struct {
		wire
		UnitPrice string `json:"UnitPrice"`
		Amount    string `json:"Amount"`
	}{wire: wire(l), UnitPrice: moneyText(l.UnitPrice), Amount: moneyText(l.Amount)}
	return json.Marshal(raw)
}

// UnmarshalJSON restores Money fields through the shared money-text reader.
func (l *InvoiceLine) UnmarshalJSON(data []byte) error {
	type wire InvoiceLine
	var raw struct {
		wire
		UnitPrice json.RawMessage `json:"UnitPrice"`
		Amount    json.RawMessage `json:"Amount"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*l = InvoiceLine(raw.wire)
	var err error
	if l.UnitPrice, err = parseMoneyJSON(raw.UnitPrice); err != nil {
		return err
	}
	if l.Amount, err = parseMoneyJSON(raw.Amount); err != nil {
		return err
	}
	return nil
}

// MarshalJSON renders the invoice total as canonical money text.
func (inv CustomerInvoice) MarshalJSON() ([]byte, error) {
	type wire CustomerInvoice
	raw := struct {
		wire
		Total string `json:"Total"`
	}{wire: wire(inv), Total: moneyText(inv.Total)}
	return json.Marshal(raw)
}

// UnmarshalJSON restores the invoice total through the shared reader.
func (inv *CustomerInvoice) UnmarshalJSON(data []byte) error {
	type wire CustomerInvoice
	var raw struct {
		wire
		Total json.RawMessage `json:"Total"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*inv = CustomerInvoice(raw.wire)
	total, err := parseMoneyJSON(raw.Total)
	if err != nil {
		return err
	}
	inv.Total = total
	return nil
}

// MarshalJSON renders the event amount as canonical money text.
func (ev ReceivableEvent) MarshalJSON() ([]byte, error) {
	type wire ReceivableEvent
	raw := struct {
		wire
		Amount string `json:"Amount"`
	}{wire: wire(ev), Amount: moneyText(ev.Amount)}
	return json.Marshal(raw)
}

// UnmarshalJSON restores the event amount through the shared reader.
func (ev *ReceivableEvent) UnmarshalJSON(data []byte) error {
	type wire ReceivableEvent
	var raw struct {
		wire
		Amount json.RawMessage `json:"Amount"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*ev = ReceivableEvent(raw.wire)
	amount, err := parseMoneyJSON(raw.Amount)
	if err != nil {
		return err
	}
	ev.Amount = amount
	return nil
}

type invoiceSnapshot struct {
	Invoices []CustomerInvoice `json:"invoices"`
	Numbers  map[string]uint64 `json:"numbers"`
	Receipts []snapshotReceipt `json:"receipts"`
}

type snapshotReceipt struct {
	Key     string          `json:"key"`
	Receipt InvoiceReceipt  `json:"receipt"`
	Result  CustomerInvoice `json:"result"`
}

// Export captures the store for restart recovery with deterministic order.
func (s *InvoiceStore) Export() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := invoiceSnapshot{Numbers: make(map[string]uint64, len(s.numbers))}
	ids := make([]string, 0, len(s.invoices))
	for id := range s.invoices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		snapshot.Invoices = append(snapshot.Invoices, copyInvoice(*s.invoices[id]))
	}
	for series, seq := range s.numbers {
		snapshot.Numbers[series] = seq
	}
	keys := make([]string, 0, len(s.receipts))
	for key := range s.receipts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		stored := s.receipts[key]
		snapshot.Receipts = append(snapshot.Receipts, snapshotReceipt{Key: key, Receipt: stored.Receipt, Result: copyInvoice(stored.Result)})
	}
	return json.Marshal(snapshot)
}

// Import restores a snapshot. Every imported field revalidates fail-closed:
// invoices (identity, arithmetic, lifecycle, digest), per-series counters
// floored by consumed numbers (a regressed counter would reuse an issued
// number), receipt map-key bindings against receipt contents, receipt
// agreement with stored invoices, and event-key uniqueness. Tampered
// snapshots refuse instead of resurrecting forged obligations.
func (s *InvoiceStore) Import(data []byte) error {
	var snapshot invoiceSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fail("invoice snapshot is not valid: %v", err)
	}
	byID := make(map[string]*CustomerInvoice, len(snapshot.Invoices))
	for i := range snapshot.Invoices {
		inv := &snapshot.Invoices[i]
		if err := inv.Validate(); err != nil {
			return err
		}
		key := invoiceKey(inv.TenantID, inv.ID)
		if _, dup := byID[key]; dup {
			return fail("snapshot invoice %q is recorded twice", inv.ID)
		}
		byID[key] = inv
		seenEvents := make(map[string]bool, len(inv.Events))
		for _, event := range inv.Events {
			if strings.TrimSpace(event.Key) == "" {
				return fail("snapshot invoice %q carries an event without an idempotency key", inv.ID)
			}
			if seenEvents[event.Key] {
				return fail("snapshot invoice %q repeats event key %q", inv.ID, event.Key)
			}
			seenEvents[event.Key] = true
		}
	}
	for seriesKey := range snapshot.Numbers {
		parts := strings.Split(seriesKey, "\x00")
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return fail("snapshot series key is malformed")
		}
	}
	for _, inv := range snapshot.Invoices {
		if inv.InvoiceNumber == "" {
			continue
		}
		consumed, err := parseInvoiceNumber(inv.IssuerSeries, inv.InvoiceNumber)
		if err != nil {
			return fail("snapshot invoice %q carries a malformed number: %v", inv.ID, err)
		}
		seriesKey := inv.TenantID + "\x00" + inv.IssuerEntity + "\x00" + inv.IssuerSeries
		if snapshot.Numbers[seriesKey] < consumed {
			return fail("snapshot series counter regressed below consumed number %q", inv.InvoiceNumber)
		}
	}
	for _, entry := range snapshot.Receipts {
		receipt := entry.Receipt
		if strings.TrimSpace(receipt.TenantID) == "" || strings.TrimSpace(receipt.Key) == "" {
			return fail("snapshot receipt lacks its tenant or key")
		}
		if entry.Key != receipt.TenantID+"\x00"+receipt.Key {
			return fail("snapshot receipt is filed under the wrong key")
		}
		switch receipt.Action {
		case "draft", "submit", "approve", "issue", "void":
		default:
			return fail("snapshot receipt action %q is unknown", receipt.Action)
		}
		inv, ok := byID[invoiceKey(receipt.TenantID, receipt.InvoiceID)]
		if !ok {
			return fail("snapshot receipt references unknown invoice %q", receipt.InvoiceID)
		}
		if receipt.Revision == 0 || receipt.Revision > inv.Revision {
			return fail("snapshot receipt revision disagrees with invoice %q", receipt.InvoiceID)
		}
		if receipt.Number != "" && receipt.Number != inv.InvoiceNumber {
			return fail("snapshot receipt number disagrees with invoice %q", receipt.InvoiceID)
		}
		if err := entry.Result.Validate(); err != nil {
			return fail("snapshot receipt result is invalid: %v", err)
		}
		if entry.Result.ID != receipt.InvoiceID || entry.Result.TenantID != receipt.TenantID || entry.Result.Revision != receipt.Revision {
			return fail("snapshot receipt result disagrees with its receipt")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoices = make(map[string]*CustomerInvoice, len(snapshot.Invoices))
	for _, inv := range snapshot.Invoices {
		stored := copyInvoice(inv)
		s.invoices[invoiceKey(inv.TenantID, inv.ID)] = &stored
	}
	s.numbers = make(map[string]uint64, len(snapshot.Numbers))
	for series, seq := range snapshot.Numbers {
		s.numbers[series] = seq
	}
	s.receipts = make(map[string]storedReceipt, len(snapshot.Receipts))
	for _, entry := range snapshot.Receipts {
		s.receipts[entry.Key] = storedReceipt{Receipt: entry.Receipt, Result: copyInvoice(entry.Result)}
	}
	return nil
}

// ReceivableEventKind names the signed receivable events. Money moves only
// through the first four kinds; disputes carry no amount.
type ReceivableEventKind string

const (
	ReceivableCreditPosted     ReceivableEventKind = "credit-posted"
	ReceivableReceiptAllocated ReceivableEventKind = "receipt-allocated"
	ReceivableRefundAllocated  ReceivableEventKind = "refund-allocated"
	ReceivableReversalPosted   ReceivableEventKind = "reversal-posted"
	ReceivableDisputeOpened    ReceivableEventKind = "dispute-opened"
	ReceivableDisputeResolved  ReceivableEventKind = "dispute-resolved"
)

// ReceivableEvent is one signed allocation fact. EventID is the
// tenant-scoped idempotency identity: repeats are no-ops, and a reused ID
// with a changed payload is a conflict.
type ReceivableEvent struct {
	EventID   string
	TenantID  string
	InvoiceID string
	Kind      ReceivableEventKind
	Amount    values.Money
	At        string
	Actor     string
	Ref       string
}

// CollectionStatus is derived separately from the conserved invoice
// lifecycle; it never rewrites invoice state.
type CollectionStatus string

const (
	CollectionOpen     CollectionStatus = "OPEN"
	CollectionPartial  CollectionStatus = "PARTIAL"
	CollectionPaid     CollectionStatus = "PAID"
	CollectionOverdue  CollectionStatus = "OVERDUE"
	CollectionDisputed CollectionStatus = "DISPUTED"
)

// ReceivableProjection explains one invoice: obligation, credit,
// settled receipt, refund and reversal balances, the remainder and any
// unapplied customer credit.
type ReceivableProjection struct {
	InvoiceID       string
	Obligation      values.Money
	Credits         values.Money
	Receipts        values.Money
	Refunds         values.Money
	Reversals       values.Money
	Remaining       values.Money
	UnappliedCredit values.Money
	Status          CollectionStatus
	Disputed        bool
}

type invoiceAccount struct {
	obligation values.Money
	dueDate    string
	credits    values.Money
	receipts   values.Money
	refunds    values.Money
	reversals  values.Money
	disputed   bool
}

// ReceivableLedger is the concurrency-safe in-memory allocation port for
// one tenant. Guards are arrival-ordered like a real ledger: a refund or
// reversal against not-yet-settled money is refused (never silently held),
// and reposting after prerequisites arrive converges to identical balances.
type ReceivableLedger struct {
	mu       sync.Mutex
	tenant   string
	accounts map[string]*invoiceAccount
	seen     map[string]string
}

// NewReceivableLedger returns an empty ledger for one tenant.
func NewReceivableLedger(tenant string) *ReceivableLedger {
	return &ReceivableLedger{tenant: tenant, accounts: make(map[string]*invoiceAccount), seen: make(map[string]string)}
}

// Register binds an invoice obligation to the ledger. The invoice must
// validate and belong to the ledger tenant.
func (l *ReceivableLedger) Register(inv CustomerInvoice) error {
	if err := inv.Validate(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if inv.TenantID != l.tenant {
		return ErrInvoiceNotFound
	}
	if existing, ok := l.accounts[inv.ID]; ok {
		if existing.obligation.Amount().Equal(inv.Total.Amount()) {
			return nil
		}
		return fmt.Errorf("%w: invoice %q already registered with a different obligation", ErrInvoiceConflict, inv.ID)
	}
	zero, err := zeroMoneyLike(inv.Total)
	if err != nil {
		return fail("initialize receivable totals: %v", err)
	}
	l.accounts[inv.ID] = &invoiceAccount{
		obligation: inv.Total, dueDate: inv.DueDate,
		credits: zero, receipts: zero, refunds: zero, reversals: zero,
	}
	return nil
}

// Post applies one signed event idempotently. Unknown invoices and foreign
// tenants share the neutral not-found response.
func (l *ReceivableLedger) Post(ev ReceivableEvent) error {
	if strings.TrimSpace(ev.EventID) == "" || strings.TrimSpace(ev.InvoiceID) == "" {
		return fail("receivable event identity is required")
	}
	payload := hashPayload("receivable", ev.EventID, ev.TenantID, ev.InvoiceID, string(ev.Kind), moneyText(ev.Amount), ev.At, ev.Actor, ev.Ref)
	l.mu.Lock()
	defer l.mu.Unlock()
	if prior, ok := l.seen[ev.EventID]; ok {
		if prior != payload {
			return fmt.Errorf("%w: receivable event reused with a changed payload", ErrInvoiceConflict)
		}
		return nil
	}
	account, ok := l.accounts[ev.InvoiceID]
	if !ok || ev.TenantID != l.tenant {
		return ErrInvoiceNotFound
	}
	if err := ev.Amount.Validate(); err != nil || ev.Amount.Currency() != account.obligation.Currency() {
		return fail("receivable event has an invalid amount or currency")
	}
	if ev.Amount.Amount().Scale() != account.obligation.Amount().Scale() {
		return fail("receivable event scale must match the invoice scale")
	}
	switch ev.Kind {
	case ReceivableDisputeOpened, ReceivableDisputeResolved:
		if ev.Amount.Amount().Sign() != 0 {
			return fail("dispute events must not carry money")
		}
		account.disputed = ev.Kind == ReceivableDisputeOpened
	case ReceivableCreditPosted, ReceivableReceiptAllocated, ReceivableRefundAllocated, ReceivableReversalPosted:
		if ev.Amount.Amount().Sign() <= 0 {
			return fail("receivable money events must carry a positive amount")
		}
		if err := account.apply(ev.Kind, ev.Amount); err != nil {
			return err
		}
	default:
		return fail("unknown receivable event kind %q", ev.Kind)
	}
	l.seen[ev.EventID] = payload
	return nil
}

// apply enforces cumulative per-kind caps: credits cannot exceed the
// obligation, and refunds plus reversals cannot exceed settled receipts in
// any arrival order (each is capped by receipts net of the other).
// Receipts may exceed the net obligation; the surplus becomes explicit
// unapplied customer credit.
func (a *invoiceAccount) apply(kind ReceivableEventKind, amount values.Money) error {
	add := func(total values.Money) (values.Money, error) { return total.Add(amount) }
	over := func(total, cap values.Money) bool {
		cmp, err := total.Cmp(cap)
		return err == nil && cmp > 0
	}
	switch kind {
	case ReceivableCreditPosted:
		next, err := add(a.credits)
		if err != nil || over(next, a.obligation) {
			return fail("credit exceeds the invoice obligation")
		}
		a.credits = next
	case ReceivableReceiptAllocated:
		next, err := add(a.receipts)
		if err != nil {
			return fail("allocate receipt: %v", err)
		}
		a.receipts = next
	case ReceivableRefundAllocated:
		next, err := add(a.refunds)
		if err != nil {
			return fail("allocate refund: %v", err)
		}
		net, err := a.receipts.Sub(a.reversals)
		if err != nil || over(next, net) {
			return fail("refund exceeds settled receipts net of reversals")
		}
		a.refunds = next
	case ReceivableReversalPosted:
		next, err := add(a.reversals)
		if err != nil {
			return fail("post reversal: %v", err)
		}
		net, err := a.receipts.Sub(a.refunds)
		if err != nil || over(next, net) {
			return fail("reversal exceeds net settled receipts")
		}
		a.reversals = next
	}
	return nil
}

// Explain derives the collection projection. asOf is an ISO YYYY-MM-DD
// date supplied by the caller; the ledger keeps no clock.
func (l *ReceivableLedger) Explain(invoiceID, asOf string) (ReceivableProjection, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	account, ok := l.accounts[invoiceID]
	if !ok {
		return ReceivableProjection{}, ErrInvoiceNotFound
	}
	net, err := account.obligation.Sub(account.credits)
	if err != nil {
		return ReceivableProjection{}, fail("explain obligation: %v", err)
	}
	net, err = net.Sub(account.receipts)
	if err != nil {
		return ReceivableProjection{}, fail("explain receipts: %v", err)
	}
	net, err = net.Add(account.refunds)
	if err != nil {
		return ReceivableProjection{}, fail("explain refunds: %v", err)
	}
	net, err = net.Add(account.reversals)
	if err != nil {
		return ReceivableProjection{}, fail("explain reversals: %v", err)
	}
	zero, err := zeroMoneyLike(account.obligation)
	if err != nil {
		return ReceivableProjection{}, fail("explain zero: %v", err)
	}
	projection := ReceivableProjection{
		InvoiceID: invoiceID, Obligation: account.obligation, Credits: account.credits,
		Receipts: account.receipts, Refunds: account.refunds, Reversals: account.reversals,
		Disputed: account.disputed,
	}
	cmp, err := net.Cmp(zero)
	if err != nil {
		return ReceivableProjection{}, fail("explain remainder: %v", err)
	}
	if cmp >= 0 {
		projection.Remaining, projection.UnappliedCredit = net, zero
	} else {
		negated, err := net.Neg()
		if err != nil {
			return ReceivableProjection{}, fail("explain unapplied credit: %v", err)
		}
		projection.Remaining, projection.UnappliedCredit = zero, negated
	}
	applied, err := account.credits.Add(account.receipts)
	if err != nil {
		return ReceivableProjection{}, fail("explain applied: %v", err)
	}
	applied, err = applied.Sub(account.refunds)
	if err != nil {
		return ReceivableProjection{}, fail("explain applied: %v", err)
	}
	applied, err = applied.Sub(account.reversals)
	if err != nil {
		return ReceivableProjection{}, fail("explain applied: %v", err)
	}
	switch {
	case account.disputed:
		projection.Status = CollectionDisputed
	case cmp <= 0:
		projection.Status = CollectionPaid
	case account.dueDate != "" && asOf > account.dueDate:
		projection.Status = CollectionOverdue
	case applied.Amount().Sign() > 0:
		projection.Status = CollectionPartial
	default:
		projection.Status = CollectionOpen
	}
	return projection, nil
}

type ledgerSnapshot struct {
	Tenant   string                   `json:"tenant"`
	Accounts map[string]ledgerAccount `json:"accounts"`
	Seen     map[string]string        `json:"seen"`
}

type ledgerAccount struct {
	Obligation string `json:"obligation"`
	DueDate    string `json:"due_date"`
	Credits    string `json:"credits"`
	Receipts   string `json:"receipts"`
	Refunds    string `json:"refunds"`
	Reversals  string `json:"reversals"`
	Disputed   bool   `json:"disputed"`
}

// Export captures the ledger for restart recovery.
func (l *ReceivableLedger) Export() ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := ledgerSnapshot{Tenant: l.tenant, Accounts: make(map[string]ledgerAccount, len(l.accounts)), Seen: make(map[string]string, len(l.seen))}
	for id, account := range l.accounts {
		snapshot.Accounts[id] = ledgerAccount{
			Obligation: moneyText(account.obligation), DueDate: account.dueDate,
			Credits: moneyText(account.credits), Receipts: moneyText(account.receipts),
			Refunds: moneyText(account.refunds), Reversals: moneyText(account.reversals),
			Disputed: account.disputed,
		}
	}
	for id, hash := range l.seen {
		snapshot.Seen[id] = hash
	}
	return json.Marshal(snapshot)
}

// Import restores a snapshot previously captured by Export. Tenant
// mismatches and malformed balances fail closed.
func (l *ReceivableLedger) Import(data []byte) error {
	var snapshot ledgerSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fail("receivable snapshot is not valid: %v", err)
	}
	if snapshot.Tenant != l.tenant {
		return fmt.Errorf("%w: receivable snapshot belongs to another tenant", ErrInvoiceNotFound)
	}
	accounts := make(map[string]*invoiceAccount, len(snapshot.Accounts))
	for id, raw := range snapshot.Accounts {
		read := func(text string) (values.Money, error) {
			return parseMoneyJSON(json.RawMessage("\"" + text + "\""))
		}
		obligation, err := read(raw.Obligation)
		if err != nil {
			return fail("snapshot obligation invalid: %v", err)
		}
		credits, err := read(raw.Credits)
		if err != nil {
			return fail("snapshot credits invalid: %v", err)
		}
		receipts, err := read(raw.Receipts)
		if err != nil {
			return fail("snapshot receipts invalid: %v", err)
		}
		refunds, err := read(raw.Refunds)
		if err != nil {
			return fail("snapshot refunds invalid: %v", err)
		}
		reversals, err := read(raw.Reversals)
		if err != nil {
			return fail("snapshot reversals invalid: %v", err)
		}
		accounts[id] = &invoiceAccount{
			obligation: obligation, dueDate: raw.DueDate, credits: credits,
			receipts: receipts, refunds: refunds, reversals: reversals, disputed: raw.Disputed,
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.accounts = accounts
	l.seen = make(map[string]string, len(snapshot.Seen))
	for id, hash := range snapshot.Seen {
		l.seen[id] = hash
	}
	return nil
}

// InvoiceBadges keeps invoice, delivery, collection and accounting-sync
// states separately labeled. Absent observations render honestly instead
// of inheriting a stronger status.
type InvoiceBadges struct {
	Invoice        string `json:"invoice"`
	Delivery       string `json:"delivery"`
	Collection     string `json:"collection"`
	AccountingSync string `json:"accounting_sync"`
}

// InvoiceProjection is the consuming-surface contract for one invoice:
// conserved identity, independent badges and the authority call to action.
type InvoiceProjection struct {
	InvoiceID    string        `json:"invoice_id"`
	Number       string        `json:"number"`
	CustomerID   string        `json:"customer_id"`
	Total        string        `json:"total"`
	Badges       InvoiceBadges `json:"badges"`
	AuthorityCTA string        `json:"authority_cta,omitempty"`
	SettingsLink string        `json:"settings_link,omitempty"`
}

// ProjectInvoice builds the consuming projection. Delivery and accounting
// observations are explicit inputs: empty means unobserved, never inferred.
func ProjectInvoice(inv CustomerInvoice, collection CollectionStatus, delivery, accountingSync string) InvoiceProjection {
	view := InvoiceProjection{
		InvoiceID: inv.ID, Number: inv.InvoiceNumber, CustomerID: inv.CustomerID, Total: inv.Total.String(),
		Badges: InvoiceBadges{Invoice: string(inv.Status), Delivery: delivery, Collection: string(collection), AccountingSync: accountingSync},
	}
	if view.Badges.Delivery == "" {
		view.Badges.Delivery = "not-observed"
	}
	if view.Badges.AccountingSync == "" {
		view.Badges.AccountingSync = "not-synced"
	}
	if inv.Authority != LocalInvoiceAuthority {
		view.AuthorityCTA = "Configure invoicing authority"
		view.SettingsLink = "/workspace/app/billing/settings"
	}
	return view
}

// SetTaxIssueGate installs the optional SMB-BILL-004 tax treatment gate.
// A nil gate preserves the exact BILL-001 issuance behavior.
func (s *InvoiceStore) SetTaxIssueGate(gate TaxIssueGate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taxGate = gate
}
