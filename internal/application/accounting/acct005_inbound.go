// Package accounting owns the SMB inbound accounting adapter (SMB-ACCT-005):
// external invoice/payment observations reach local receivable and payable
// balances only through admitted sources, explicit version ordering and
// owner commands. It reuses the connectivity webhook ingress, the observe
// normalization/store, the syncjob page lifecycle, the workorderbilling
// receivable ledger and the payable registry; durable commercial receipt
// persistence stays with the providerreceipts store once BILL007 extends its
// schema, and this adapter consumes it through the ReceiptDedup port.
package accounting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/syncjob"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payable"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrAcctValidation = errors.New("accounting: inbound request is invalid")
	ErrAcctDenied     = errors.New("accounting: not found or not permitted")
	ErrAcctConflict   = errors.New("accounting: idempotency conflict")
	ErrAcctStale      = errors.New("accounting: observation version is stale")
)

// Admitted inbound source kinds. Payroll-owned kinds are never admitted:
// PAYGL input stays payroll-owned and quarantines here.
const (
	SourceARInvoice     = "AR_INVOICE"
	SourceARReceipt     = "AR_RECEIPT"
	SourceAPBill        = "AP_BILL"
	SourceAPPayment     = "AP_PAYMENT"
	SourceAPCredit      = "AP_CREDIT"
	SourceReimbursement = "EXPENSE_REIMBURSEMENT"
	SourceStockCost     = "STOCK_COST"
)

// acctInboundPolicyVersion identifies the approved inbound adapter policy that
// every externally observed invoice snapshot cites. Manual invoices must carry
// an explicit policy version (workorderbilling ManualSource validation).
const acctInboundPolicyVersion = "acct005-inbound/v1"

// Ingest outcome statuses.
const (
	OutcomeApplied     = "applied"
	OutcomeDuplicate   = "duplicate"
	OutcomeQuarantined = "quarantined"
	OutcomeExternal    = "externally_reported"
)

// AcctOp carries the tenant, legal entity, actor and capabilities for one
// inbound call. Reads that disclose data and every mutation authorize it.
type AcctOp struct {
	TenantID      string
	LegalEntityID string
	ActorID       string
	IngestAllowed bool
	ReviewAllowed bool
}

func (op AcctOp) validate() error {
	if strings.TrimSpace(op.TenantID) == "" || strings.TrimSpace(op.LegalEntityID) == "" || strings.TrimSpace(op.ActorID) == "" {
		return fmt.Errorf("%w: operation needs tenant, legal entity and actor", ErrAcctValidation)
	}
	return nil
}

// SourceProfile admits one (connection, account, kind) triple. SettlesCash is
// the approved-profile statement that this source's settlement references
// confirm platform cash; without it paid status stays externally reported.
type SourceProfile struct {
	ConnectionID string
	AccountID    string
	Kind         string
	Module       string
	Enabled      bool
	SettlesCash  bool
}

// SourceAdmission confirms an admitted source.
type SourceAdmission struct {
	SourceID string
	Key      string
}

// AccountingObservation is one normalized external invoice/payment fact with
// its provenance. External truth is recorded; it never bypasses authority.
type AccountingObservation struct {
	ConnectionID      string
	AccountID         string
	Kind              string
	ObjectID          string
	ExternalTextID    string
	EventID           string
	Version           uint64
	Digest            string
	ObservedAt        string
	EffectiveAt       string
	Status            string
	Amount            string
	SettlementRef     string
	Authority         string
	AuthorityAt       string
	AuthorityDecision string
	SupplierID        string
	SupplierName      string
	InvoiceNumber     string
	Category          string
	TaxEvidence       string
	DueDate           string
	Terms             string
	LocalNotes        string
	Aliases           []string
}

// IngestOutcome is the durable receipt for one ingest call.
type IngestOutcome struct {
	ReceiptID string
	Status    string
	FamilyID  string
	Reason    string
	Key       string
}

// FamilyView explains one obligation family.
type FamilyView struct {
	FamilyID       string
	Kind           string
	ObjectID       string
	ExternalTextID string
	Version        uint64
	Status         string
	Settled        string
	Remaining      string
	Notes          string
	Aliases        []string
}

// Discrepancy records a contradiction with a proposed repair.
type Discrepancy struct {
	FamilyID       string
	Reason         string
	ProposedRepair string
	At             string
}

// DenialReceipt is the authorized telemetry for one security refusal. The
// caller-visible response stays neutral; the receipt is tenant-scoped.
type DenialReceipt struct {
	ReceiptID string
	Reason    string
	At        string
}

// CheckpointOutcome reports one page-commit attempt.
type CheckpointOutcome struct {
	Advanced   bool
	Writes     int
	Candidates int
	Failures   int
	Watermark  int64
}

// RunRow explains one ingested object for the run detail.
type RunRow struct {
	ObjectID   string
	Kind       string
	Outcome    string
	Reason     string
	ReviewLink string
}

// RunDetail aggregates one connection run for the operations surface.
type RunDetail struct {
	ConnectionID string
	Fetched      int
	Applied      int
	Duplicates   int
	Quarantined  int
	Externally   int
	Watermark    int64
	Complete     bool
	Rows         []RunRow
}

type acctSource struct {
	profile SourceProfile
	key     string
}

type acctFamily struct {
	id             string
	tenant         string
	entity         string
	connection     string
	account        string
	kind           string
	object         string
	textID         string
	version        uint64
	digest         string
	status         string
	amount         string
	settled        string
	notes          string
	aliases        []string
	payableID      string
	receiptID      string
	externalStatus string
}

type acctVersion struct {
	version   uint64
	digest    string
	receiptID string
	familyID  string
}

type acctIdem struct {
	fingerprint string
	outcome     IngestOutcome
	tenant      string
	entity      string
}

type acctRun struct {
	fetched     int
	applied     int
	duplicates  int
	quarantined int
	externally  int
	rows        []RunRow
}

type acctCheckpoint struct {
	watermark int64
	complete  bool
	known     map[string]string
	at        string
}

// ReceiptDedup is the narrow port this adapter needs from durable receipt
// storage. Production binds the providerreceipts store once BILL007 admits
// commercial families; tests bind the memory double below.
type ReceiptDedup interface {
	RecordDomestic(ctxTenant, key, digest string) (duplicate bool, prior string, err error)
}

// MemoryDedup is a durable-emulating ReceiptDedup for tests. It persists
// through Export/Import so restart replay is proven, not assumed.
type MemoryDedup struct {
	mu      sync.Mutex
	records map[string]string
}

// NewMemoryDedup builds an empty double.
func NewMemoryDedup() *MemoryDedup {
	return &MemoryDedup{records: make(map[string]string)}
}

func (m *MemoryDedup) RecordDomestic(ctxTenant, key, digest string) (bool, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	slot := ctxTenant + "\x00" + strings.TrimSpace(key)
	if prior, ok := m.records[slot]; ok {
		return true, prior, nil
	}
	m.records[slot] = digest
	return false, "", nil
}

// InboundRegistry routes external accounting observations to their owning
// balances under explicit version and authority rules.
type InboundRegistry struct {
	mu            sync.Mutex
	clock         func() time.Time
	ap            *payable.PayableRegistry
	ledgers       map[string]*workorderbilling.ReceivableLedger
	sources       map[string]*acctSource
	families      map[string]*acctFamily
	versions      map[string]*acctVersion
	idem          map[string]*acctIdem
	runs          map[string]*acctRun
	checkpoints   map[string]*acctCheckpoint
	denials       map[string][]DenialReceipt
	discrepancies map[string][]Discrepancy
	dedup         *MemoryDedup
	seq           uint64
}

// NewInboundRegistry builds a registry sharing one multi-tenant payable
// owner. Receivable ledgers bind per tenant before AR ingest.
func NewInboundRegistry(clock func() time.Time, ap *payable.PayableRegistry) *InboundRegistry {
	return &InboundRegistry{
		clock:         clock,
		ap:            ap,
		ledgers:       make(map[string]*workorderbilling.ReceivableLedger),
		sources:       make(map[string]*acctSource),
		families:      make(map[string]*acctFamily),
		versions:      make(map[string]*acctVersion),
		idem:          make(map[string]*acctIdem),
		runs:          make(map[string]*acctRun),
		checkpoints:   make(map[string]*acctCheckpoint),
		denials:       make(map[string][]DenialReceipt),
		discrepancies: make(map[string][]Discrepancy),
		dedup:         NewMemoryDedup(),
	}
}

func acctKey(parts ...string) string { return strings.Join(parts, "\x00") }

func acctNow(r *InboundRegistry) time.Time {
	if r.clock != nil {
		return r.clock()
	}
	return time.Now().UTC()
}

func acctStamp(r *InboundRegistry) string { return acctNow(r).Format(time.RFC3339) }

// BindLedger binds the tenant's receivable balance owner.
func (r *InboundRegistry) BindLedger(op AcctOp, ledger *workorderbilling.ReceivableLedger) error {
	if err := op.validate(); err != nil {
		return err
	}
	if !op.IngestAllowed {
		r.recordDenial(op, "bind ledger without ingest capability")
		return ErrAcctDenied
	}
	if ledger == nil {
		return fmt.Errorf("%w: ledger is required", ErrAcctValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ledgers[acctKey(op.TenantID, op.LegalEntityID)] = ledger
	return nil
}

func acctKnownKind(kind string) bool {
	switch kind {
	case SourceARInvoice, SourceARReceipt, SourceAPBill, SourceAPPayment, SourceAPCredit, SourceReimbursement, SourceStockCost:
		return true
	default:
		return false
	}
}

// AdmitSource admits one source triple. Unknown kinds (including
// payroll-owned ones) are refused; nothing routes without admission.
func (r *InboundRegistry) AdmitSource(op AcctOp, profile SourceProfile, key string) (SourceAdmission, error) {
	if err := op.validate(); err != nil {
		return SourceAdmission{}, err
	}
	if !op.IngestAllowed {
		r.recordDenial(op, "admit source without ingest capability")
		return SourceAdmission{}, ErrAcctDenied
	}
	if strings.TrimSpace(profile.ConnectionID) == "" || strings.TrimSpace(profile.AccountID) == "" {
		return SourceAdmission{}, fmt.Errorf("%w: source needs connection and account", ErrAcctValidation)
	}
	if !acctKnownKind(profile.Kind) {
		return SourceAdmission{}, fmt.Errorf("%w: source kind %q is not admitted", ErrAcctValidation, profile.Kind)
	}
	if strings.TrimSpace(key) == "" {
		return SourceAdmission{}, fmt.Errorf("%w: idempotency key is required", ErrAcctValidation)
	}
	if strings.TrimSpace(profile.Module) == "" {
		return SourceAdmission{}, fmt.Errorf("%w: source needs its owning module", ErrAcctValidation)
	}
	id := acctKey(op.TenantID, op.LegalEntityID, profile.ConnectionID, profile.AccountID, profile.Kind)
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.sources[id]; ok {
		return SourceAdmission{SourceID: id, Key: existing.key}, nil
	}
	r.sources[id] = &acctSource{profile: profile, key: strings.TrimSpace(key)}
	return SourceAdmission{SourceID: id, Key: strings.TrimSpace(key)}, nil
}

func (r *InboundRegistry) recordDenial(op AcctOp, reason string) DenialReceipt {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	receipt := DenialReceipt{ReceiptID: fmt.Sprintf("deny-%d", r.seq), Reason: reason, At: acctStamp(r)}
	slot := acctKey(op.TenantID, op.LegalEntityID)
	r.denials[slot] = append(r.denials[slot], receipt)
	return receipt
}

// Denials lists the caller's tenant-scoped denial telemetry.
func (r *InboundRegistry) Denials(op AcctOp) ([]DenialReceipt, error) {
	if err := op.validate(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]DenialReceipt(nil), r.denials[acctKey(op.TenantID, op.LegalEntityID)]...)
	return out, nil
}

func acctParseMoney(raw string) (values.Money, error) {
	trimmed := strings.TrimSpace(raw)
	parts := strings.Split(trimmed, " ")
	if len(parts) != 2 {
		return values.Money{}, fmt.Errorf("amount %q must read like 60.00 USD", raw)
	}
	return values.NewMoney(parts[0], parts[1], 2, values.RoundingExactRequired)
}

func acctFingerprint(obs AccountingObservation) string {
	aliases := strings.Join(append([]string(nil), obs.Aliases...), ",")
	sum := sha256.Sum256([]byte(strings.Join([]string{
		obs.ConnectionID, obs.AccountID, obs.Kind, obs.ObjectID, obs.ExternalTextID,
		obs.EventID, fmt.Sprintf("%d", obs.Version), obs.Digest, obs.ObservedAt,
		obs.EffectiveAt, obs.Status, obs.Amount, obs.SettlementRef, obs.Authority,
		obs.AuthorityAt, obs.AuthorityDecision, obs.SupplierID, obs.SupplierName,
		obs.InvoiceNumber, obs.Category, obs.TaxEvidence, obs.DueDate, obs.Terms,
		obs.LocalNotes, aliases,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func acctValidateObs(obs AccountingObservation) error {
	if strings.TrimSpace(obs.ConnectionID) == "" || strings.TrimSpace(obs.AccountID) == "" {
		return fmt.Errorf("%w: observation needs connection and account", ErrAcctValidation)
	}
	if !acctKnownKind(obs.Kind) {
		return fmt.Errorf("%w: observation kind %q is not admitted", ErrAcctValidation, obs.Kind)
	}
	if strings.TrimSpace(obs.ObjectID) == "" || strings.TrimSpace(obs.EventID) == "" {
		return fmt.Errorf("%w: observation needs object and event identity", ErrAcctValidation)
	}
	if obs.Version == 0 {
		return fmt.Errorf("%w: observation version starts at 1", ErrAcctValidation)
	}
	if strings.TrimSpace(obs.Digest) == "" {
		return fmt.Errorf("%w: observation needs its content digest", ErrAcctValidation)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(obs.ObservedAt)); err != nil {
		return fmt.Errorf("%w: observed_at %v", ErrAcctValidation, err)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(obs.EffectiveAt)); err != nil {
		return fmt.Errorf("%w: effective_at %v", ErrAcctValidation, err)
	}
	if _, err := acctParseMoney(obs.Amount); err != nil {
		return fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	return nil
}

// Ingest routes one observation to its owning balance.

func acctReceiptID(tenant, entity, fingerprint string) string {
	sum := sha256.Sum256([]byte(tenant + "\x00" + entity + "\x00" + fingerprint))
	return "rcpt-" + hex.EncodeToString(sum[:])[:16]
}

func (r *InboundRegistry) payOp(op AcctOp) payable.PayOp {
	return payable.PayOp{TenantID: op.TenantID, LegalEntityID: op.LegalEntityID, ActorID: op.ActorID, PayableAllowed: true, ApproveAllowed: op.ReviewAllowed}
}

// Ingest admits one observation under version, authority and idempotency
// rules. Owner commands run before the local commit so a crash replays to
// one converged effect through owner idempotency; the local commit pins the
// receipt for every later replay.
func (r *InboundRegistry) Ingest(op AcctOp, obs AccountingObservation, key string) (IngestOutcome, error) {
	if err := op.validate(); err != nil {
		return IngestOutcome{}, err
	}
	if !op.IngestAllowed {
		r.recordDenial(op, "ingest without ingest capability")
		return IngestOutcome{}, ErrAcctDenied
	}
	if strings.TrimSpace(key) == "" {
		return IngestOutcome{}, fmt.Errorf("%w: idempotency key is required", ErrAcctValidation)
	}
	if err := acctValidateObs(obs); err != nil {
		return IngestOutcome{}, err
	}
	tenant, entity := op.TenantID, op.LegalEntityID
	fingerprint := acctFingerprint(obs)
	receiptID := acctReceiptID(tenant, entity, fingerprint)

	r.mu.Lock()
	source, admitted := r.sources[acctKey(tenant, entity, obs.ConnectionID, obs.AccountID, obs.Kind)]
	if prior, ok := r.idem[acctKey(tenant, strings.TrimSpace(key))]; ok {
		r.mu.Unlock()
		if prior.tenant != tenant || prior.entity != entity {
			r.recordDenial(op, "idempotency key crosses tenant scope")
			return IngestOutcome{}, ErrAcctDenied
		}
		if prior.fingerprint != fingerprint {
			return IngestOutcome{}, fmt.Errorf("%w: key is already used", ErrAcctConflict)
		}
		return prior.outcome, nil
	}
	r.mu.Unlock()

	if !admitted {
		return r.quarantine(op, obs, key, receiptID, "", "source is not admitted")
	}
	if !source.profile.Enabled {
		return r.quarantine(op, obs, key, receiptID, "", "source module is disabled")
	}

	vslot := acctKey(tenant, entity, obs.ConnectionID, obs.AccountID, obs.Kind, obs.ObjectID)
	r.mu.Lock()
	prior, ok := r.versions[vslot]
	r.mu.Unlock()
	if ok {
		if obs.Version < prior.version {
			r.recordRow(op, obs, prior.familyID, "stale", fmt.Sprintf("version %d is older than accepted %d", obs.Version, prior.version))
			return IngestOutcome{}, fmt.Errorf("%w: version %d is older than accepted %d", ErrAcctStale, obs.Version, prior.version)
		}
		if obs.Version == prior.version {
			if obs.Digest == prior.digest {
				outcome := IngestOutcome{ReceiptID: prior.receiptID, Status: OutcomeDuplicate, FamilyID: prior.familyID, Key: strings.TrimSpace(key)}
				r.commitOutcome(op, key, fingerprint, outcome)
				r.recordRow(op, obs, prior.familyID, OutcomeDuplicate, "same version and digest as accepted observation")
				return outcome, nil
			}
			r.recordDiscrepancy(op, prior.familyID, fmt.Sprintf("version %d restated with a new digest", obs.Version), fmt.Sprintf("review restated facts amount %s event %s", obs.Amount, obs.EventID))
			return r.quarantine(op, obs, key, receiptID, prior.familyID, "contradictory restatement opens a discrepancy")
		}
	}

	switch obs.Kind {
	case SourceARInvoice:
		return r.applyARInvoice(op, source.profile, obs, key, fingerprint, receiptID)
	case SourceARReceipt:
		return r.applyARReceipt(op, source.profile, obs, key, fingerprint, receiptID)
	case SourceAPBill:
		return r.applyAPBill(op, source.profile, obs, key, fingerprint, receiptID)
	case SourceAPPayment:
		return r.applyAPPayment(op, source.profile, obs, key, fingerprint, receiptID)
	case SourceAPCredit:
		return r.applyAPCredit(op, source.profile, obs, key, fingerprint, receiptID)
	case SourceReimbursement:
		return r.applyReimbursement(op, source.profile, obs, key, fingerprint, receiptID)
	case SourceStockCost:
		return r.applyStockCost(op, source.profile, obs, key, fingerprint, receiptID)
	default:
		return IngestOutcome{}, fmt.Errorf("%w: observation kind %q is not admitted", ErrAcctValidation, obs.Kind)
	}
}

func (r *InboundRegistry) commitOutcome(op AcctOp, key, fingerprint string, outcome IngestOutcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.idem[acctKey(op.TenantID, strings.TrimSpace(key))] = &acctIdem{fingerprint: fingerprint, outcome: outcome, tenant: op.TenantID, entity: op.LegalEntityID}
}

func (r *InboundRegistry) recordRow(op AcctOp, obs AccountingObservation, familyID, outcome, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot := acctKey(op.TenantID, op.LegalEntityID, obs.ConnectionID)
	run, ok := r.runs[slot]
	if !ok {
		run = &acctRun{}
		r.runs[slot] = run
	}
	run.fetched++
	link := "/workspace/app/accounting/inbound/events/" + obs.EventID
	if familyID != "" {
		link = "/workspace/app/accounting/inbound/families/" + familyID
	}
	switch outcome {
	case OutcomeApplied:
		run.applied++
	case OutcomeDuplicate:
		run.duplicates++
	case OutcomeQuarantined:
		run.quarantined++
	case OutcomeExternal:
		run.externally++
	}
	run.rows = append(run.rows, RunRow{ObjectID: obs.ObjectID, Kind: obs.Kind, Outcome: outcome, Reason: reason, ReviewLink: link})
}

func (r *InboundRegistry) recordDiscrepancy(op AcctOp, familyID, reason, repair string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot := acctKey(op.TenantID, op.LegalEntityID)
	r.discrepancies[slot] = append(r.discrepancies[slot], Discrepancy{FamilyID: familyID, Reason: reason, ProposedRepair: repair, At: acctStamp(r)})
}

// Discrepancies lists the tenant's open contradictions with proposed repairs.
func (r *InboundRegistry) Discrepancies(op AcctOp) ([]Discrepancy, error) {
	if err := op.validate(); err != nil {
		return nil, err
	}
	if !op.IngestAllowed && !op.ReviewAllowed {
		r.recordDenial(op, "read discrepancies without capability")
		return nil, ErrAcctDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]Discrepancy(nil), r.discrepancies[acctKey(op.TenantID, op.LegalEntityID)]...)
	return out, nil
}

func (r *InboundRegistry) quarantine(op AcctOp, obs AccountingObservation, key, receiptID, familyID, reason string) (IngestOutcome, error) {
	outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeQuarantined, FamilyID: familyID, Reason: reason, Key: strings.TrimSpace(key)}
	r.commitOutcome(op, key, acctFingerprint(obs), outcome)
	r.recordRow(op, obs, familyID, OutcomeQuarantined, reason)
	return outcome, nil
}

func (r *InboundRegistry) familySlot(op AcctOp, obs AccountingObservation) string {
	return acctKey(op.TenantID, op.LegalEntityID, obs.ConnectionID, obs.AccountID, obs.ObjectID)
}

func (r *InboundRegistry) versionSlot(op AcctOp, obs AccountingObservation) string {
	return acctKey(op.TenantID, op.LegalEntityID, obs.ConnectionID, obs.AccountID, obs.Kind, obs.ObjectID)
}

// applyARInvoice projects an external invoice onto the receivable balance
// owner. Later versions update only permitted fields; amount restatements
// become discrepancies and local notes are retained.
func (r *InboundRegistry) applyARInvoice(op AcctOp, profile SourceProfile, obs AccountingObservation, key, fingerprint, receiptID string) (IngestOutcome, error) {
	money, err := acctParseMoney(obs.Amount)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	if obs.AuthorityDecision != "approved" {
		return r.quarantine(op, obs, key, receiptID, "", "invoice authority decision is not approved")
	}
	r.mu.Lock()
	ledger, ok := r.ledgers[acctKey(op.TenantID, op.LegalEntityID)]
	family, seen := r.families[r.familySlot(op, obs)]
	r.mu.Unlock()
	if !ok || ledger == nil {
		return IngestOutcome{}, fmt.Errorf("%w: receivable owner is not bound", ErrAcctValidation)
	}
	if seen && family.kind != SourceARInvoice {
		return r.quarantine(op, obs, key, receiptID, family.id, "object is already owned by another family kind")
	}
	if seen && family.amount != obs.Amount {
		r.recordDiscrepancy(op, family.id, "external master restated the obligated amount", fmt.Sprintf("review restated obligation %s against accepted %s", obs.Amount, family.amount))
		return r.quarantine(op, obs, key, receiptID, family.id, "restated obligation opens a discrepancy")
	}
	invoiceID := "ar-" + strings.TrimSpace(obs.ObjectID)
	effective, err := time.Parse(time.RFC3339, strings.TrimSpace(obs.EffectiveAt))
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: effective_at %v", ErrAcctValidation, err)
	}
	quantity, err := values.NewDecimal("1", 0, values.RoundingExactRequired)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	invoice := workorderbilling.CustomerInvoice{
		ID: invoiceID, TenantID: op.TenantID, IssuerEntity: op.LegalEntityID, IssuerSeries: "EXT",
		InvoiceNumber: strings.TrimSpace(obs.ExternalTextID), CustomerID: strings.TrimSpace(obs.AccountID),
		Authority: "external:" + strings.TrimSpace(obs.ConnectionID) + ":" + strings.TrimSpace(obs.AccountID),
		Source:    workorderbilling.InvoiceSource{Kind: workorderbilling.InvoiceSourceManual, Manual: workorderbilling.ManualSource{RateRef: "external-observe", PolicyRef: "acct005-inbound", PolicyVersion: acctInboundPolicyVersion, Evidence: strings.TrimSpace(obs.EventID)}},
		Currency:  money.Currency(),
		Lines: []workorderbilling.InvoiceLine{{ID: "ext-1", Description: "externally observed " + strings.TrimSpace(obs.ExternalTextID),
			Quantity: quantity, UnitPrice: money, Amount: money, RateRef: "external-observe", PolicyRef: "acct005-inbound"}},
		Total: money, IssueDate: effective.Format("2006-01-02"), Status: workorderbilling.InvoiceIssued,
		Revision: obs.Version, Digest: strings.TrimSpace(obs.Digest),
	}
	_ = profile
	if err := ledger.Register(invoice); err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	r.mu.Lock()
	if !seen {
		r.seq++
		family = &acctFamily{id: fmt.Sprintf("fam-%d", r.seq), tenant: op.TenantID, entity: op.LegalEntityID,
			connection: obs.ConnectionID, account: obs.AccountID, kind: SourceARInvoice,
			object: obs.ObjectID, textID: obs.ExternalTextID, notes: obs.LocalNotes}
		r.families[r.familySlot(op, obs)] = family
	}
	family.version = obs.Version
	family.digest = obs.Digest
	family.status = "open"
	family.amount = obs.Amount
	family.receiptID = receiptID
	r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
	r.mu.Unlock()
	outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeApplied, FamilyID: family.id, Key: strings.TrimSpace(key)}
	r.commitOutcome(op, key, fingerprint, outcome)
	r.recordRow(op, obs, family.id, OutcomeApplied, "invoice projected to the receivable owner")
	return outcome, nil
}

// applyARReceipt allocates an externally observed receipt. Paid status
// without qualifying settlement evidence stays externally reported: it never
// becomes platform-confirmed cash.
func (r *InboundRegistry) applyARReceipt(op AcctOp, profile SourceProfile, obs AccountingObservation, key, fingerprint, receiptID string) (IngestOutcome, error) {
	money, err := acctParseMoney(obs.Amount)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	r.mu.Lock()
	ledger, ok := r.ledgers[acctKey(op.TenantID, op.LegalEntityID)]
	family, seen := r.families[r.familySlot(op, obs)]
	r.mu.Unlock()
	if !seen || family.kind != SourceARInvoice {
		return r.quarantine(op, obs, key, receiptID, "", "receipt names an unknown invoice")
	}
	if !ok || ledger == nil {
		return IngestOutcome{}, fmt.Errorf("%w: receivable owner is not bound", ErrAcctValidation)
	}
	if obs.Status == "paid" && obs.AuthorityDecision == "approved" && profile.SettlesCash && strings.TrimSpace(obs.SettlementRef) != "" {
		event := workorderbilling.ReceivableEvent{
			EventID:  "acct5:" + op.TenantID + ":" + obs.ConnectionID + ":" + obs.AccountID + ":AR_RECEIPT:" + obs.ObjectID + ":" + fmt.Sprintf("%d", obs.Version) + ":" + receiptID,
			TenantID: op.TenantID, InvoiceID: "ar-" + strings.TrimSpace(obs.ObjectID),
			Kind: workorderbilling.ReceivableReceiptAllocated, Amount: money,
			At: strings.TrimSpace(obs.ObservedAt), Actor: strings.TrimSpace(obs.Authority), Ref: strings.TrimSpace(obs.SettlementRef),
		}
		if err := ledger.Post(event); err != nil {
			return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
		}
		r.mu.Lock()
		family.version = obs.Version
		family.status = "platform_confirmed"
		family.receiptID = receiptID
		r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
		r.mu.Unlock()
		outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeApplied, FamilyID: family.id, Key: strings.TrimSpace(key)}
		r.commitOutcome(op, key, fingerprint, outcome)
		r.recordRow(op, obs, family.id, OutcomeApplied, "receipt allocated against the invoice obligation")
		return outcome, nil
	}
	r.mu.Lock()
	family.externalStatus = obs.Status
	r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
	r.mu.Unlock()
	outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeExternal, FamilyID: family.id, Reason: "paid status without qualifying settlement evidence", Key: strings.TrimSpace(key)}
	r.commitOutcome(op, key, fingerprint, outcome)
	r.recordRow(op, obs, family.id, OutcomeExternal, "externally reported; not platform-confirmed cash")
	return outcome, nil
}

// applyAPBill admits an approved no-PO bill into the BILL012 payable owner.
// Supplier payment obligations never settle the customer receivable.
func (r *InboundRegistry) applyAPBill(op AcctOp, profile SourceProfile, obs AccountingObservation, key, fingerprint, receiptID string) (IngestOutcome, error) {
	_ = profile
	if obs.AuthorityDecision != "approved" {
		return r.quarantine(op, obs, key, receiptID, "", "bill authority decision is not approved")
	}
	for _, field := range []struct {
		name, value string
	}{
		{"invoice number", obs.InvoiceNumber},
		{"approver", obs.Authority},
		{"approval time", obs.AuthorityAt},
		{"due date", obs.DueDate},
		{"category", obs.Category},
		{"tax evidence", obs.TaxEvidence},
		{"supplier", obs.SupplierID},
	} {
		if strings.TrimSpace(field.value) == "" {
			return IngestOutcome{}, fmt.Errorf("%w: AP bill needs its %s", ErrAcctValidation, field.name)
		}
	}
	money, err := acctParseMoney(obs.Amount)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	r.mu.Lock()
	family, seen := r.families[r.familySlot(op, obs)]
	r.mu.Unlock()
	if seen && family.kind != SourceAPBill {
		return r.quarantine(op, obs, key, receiptID, family.id, "object is already owned by another family kind")
	}
	if seen && family.amount != obs.Amount {
		r.recordDiscrepancy(op, family.id, "external master restated the billed amount", fmt.Sprintf("review restated bill %s against accepted %s", obs.Amount, family.amount))
		return r.quarantine(op, obs, key, receiptID, family.id, "restated bill opens a discrepancy")
	}
	admitted, err := r.ap.AdmitNonPOBill(r.payOp(op), payable.NonPOBillInput{
		ApprovalID: "acct5:" + strings.TrimSpace(obs.EventID), ApprovalRevision: obs.Version,
		SupplierID: strings.TrimSpace(obs.SupplierID), SupplierName: strings.TrimSpace(obs.SupplierName),
		InvoiceNumber: strings.TrimSpace(obs.InvoiceNumber), Amount: money.String(), Currency: money.Currency(),
		Category: strings.TrimSpace(obs.Category), Terms: strings.TrimSpace(obs.Terms),
		TaxEvidence: strings.TrimSpace(obs.TaxEvidence), DueDate: strings.TrimSpace(obs.DueDate),
		ApprovedBy: strings.TrimSpace(obs.Authority), ApprovedAt: strings.TrimSpace(obs.AuthorityAt),
	}, "acct5:"+strings.TrimSpace(key))
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	if admitted.Status == payable.PayableQuarantined {
		r.mu.Lock()
		if !seen {
			r.seq++
			family = &acctFamily{id: fmt.Sprintf("fam-%d", r.seq), tenant: op.TenantID, entity: op.LegalEntityID,
				connection: obs.ConnectionID, account: obs.AccountID, kind: SourceAPBill,
				object: obs.ObjectID, textID: obs.ExternalTextID, notes: obs.LocalNotes}
			r.families[r.familySlot(op, obs)] = family
		}
		family.version = obs.Version
		family.payableID = admitted.PayableID
		family.status = "quarantined"
		r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
		r.mu.Unlock()
		return r.quarantine(op, obs, key, receiptID, family.id, "payable owner quarantined the bill")
	}
	r.mu.Lock()
	if !seen {
		r.seq++
		family = &acctFamily{id: fmt.Sprintf("fam-%d", r.seq), tenant: op.TenantID, entity: op.LegalEntityID,
			connection: obs.ConnectionID, account: obs.AccountID, kind: SourceAPBill,
			object: obs.ObjectID, textID: obs.ExternalTextID, notes: obs.LocalNotes}
		r.families[r.familySlot(op, obs)] = family
	}
	family.version = obs.Version
	family.digest = obs.Digest
	family.status = "open"
	family.amount = obs.Amount
	family.payableID = admitted.PayableID
	family.receiptID = receiptID
	r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
	r.mu.Unlock()
	outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeApplied, FamilyID: family.id, Key: strings.TrimSpace(key)}
	r.commitOutcome(op, key, fingerprint, outcome)
	r.recordRow(op, obs, family.id, OutcomeApplied, "bill admitted to the payable owner")
	return outcome, nil
}

// applyAPPayment allocates a supplier payment through the BILL012 owner.
// Bank net amounts need source-profile reconciliation like customer receipts.
func (r *InboundRegistry) applyAPPayment(op AcctOp, profile SourceProfile, obs AccountingObservation, key, fingerprint, receiptID string) (IngestOutcome, error) {
	money, err := acctParseMoney(obs.Amount)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	r.mu.Lock()
	family, seen := r.families[r.familySlot(op, obs)]
	r.mu.Unlock()
	if !seen || family.kind != SourceAPBill || family.payableID == "" {
		return r.quarantine(op, obs, key, receiptID, "", "payment names an unknown bill")
	}
	if obs.AuthorityDecision != "approved" || !profile.SettlesCash || strings.TrimSpace(obs.SettlementRef) == "" {
		r.mu.Lock()
		family.externalStatus = obs.Status
		r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
		r.mu.Unlock()
		outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeExternal, FamilyID: family.id, Reason: "supplier-paid net amount without qualifying settlement evidence", Key: strings.TrimSpace(key)}
		r.commitOutcome(op, key, fingerprint, outcome)
		r.recordRow(op, obs, family.id, OutcomeExternal, "externally reported; not platform-confirmed cash")
		return outcome, nil
	}
	if _, err := r.ap.AllocatePayment(r.payOp(op), payable.PaymentInput{
		PayableID: family.payableID, Amount: money.String(),
		ManualEvidence: "external:" + strings.TrimSpace(obs.SettlementRef), Label: "external settlement",
	}, "acct5:"+strings.TrimSpace(key)); err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	r.mu.Lock()
	family.version = obs.Version
	family.status = "platform_confirmed"
	family.receiptID = receiptID
	r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
	r.mu.Unlock()
	outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeApplied, FamilyID: family.id, Key: strings.TrimSpace(key)}
	r.commitOutcome(op, key, fingerprint, outcome)
	r.recordRow(op, obs, family.id, OutcomeApplied, "supplier payment allocated against the bill")
	return outcome, nil
}

// applyAPCredit records a supplier credit through the BILL012 owner. It
// reduces only its AP obligation, never a sharing-text AR invoice.
func (r *InboundRegistry) applyAPCredit(op AcctOp, profile SourceProfile, obs AccountingObservation, key, fingerprint, receiptID string) (IngestOutcome, error) {
	_ = profile
	money, err := acctParseMoney(obs.Amount)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	r.mu.Lock()
	family, seen := r.families[r.familySlot(op, obs)]
	r.mu.Unlock()
	if !seen || family.kind != SourceAPBill || family.payableID == "" {
		return r.quarantine(op, obs, key, receiptID, "", "credit names an unknown bill")
	}
	if obs.AuthorityDecision != "approved" {
		return r.quarantine(op, obs, key, receiptID, family.id, "credit authority decision is not approved")
	}
	if _, err := r.ap.RecordCredit(r.payOp(op), payable.CreditInput{
		PayableID: family.payableID, Amount: money.String(),
		SourceRef: strings.TrimSpace(obs.EventID), Reason: "external credit " + strings.TrimSpace(obs.Status),
	}, "acct5:"+strings.TrimSpace(key)); err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	r.mu.Lock()
	family.version = obs.Version
	family.receiptID = receiptID
	r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
	r.mu.Unlock()
	outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeApplied, FamilyID: family.id, Key: strings.TrimSpace(key)}
	r.commitOutcome(op, key, fingerprint, outcome)
	r.recordRow(op, obs, family.id, OutcomeApplied, "supplier credit applied to the bill")
	return outcome, nil
}

// applyReimbursement settles one expense-obligation family. Payroll and
// accounting aliases resolve to the same family: the obligation settles
// once, never once per channel.
func (r *InboundRegistry) applyReimbursement(op AcctOp, profile SourceProfile, obs AccountingObservation, key, fingerprint, receiptID string) (IngestOutcome, error) {
	_ = profile
	if obs.AuthorityDecision != "approved" {
		return r.quarantine(op, obs, key, receiptID, "", "reimbursement authority decision is not approved")
	}
	if len(obs.Aliases) == 0 {
		return IngestOutcome{}, fmt.Errorf("%w: reimbursement needs its channel alias", ErrAcctValidation)
	}
	money, err := acctParseMoney(obs.Amount)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	slot := r.familySlot(op, obs)
	r.mu.Lock()
	family, seen := r.families[slot]
	if seen && family.kind != SourceReimbursement {
		r.mu.Unlock()
		return r.quarantine(op, obs, key, receiptID, family.id, "object is already owned by another family kind")
	}
	if seen && family.settled != "" && family.settled != money.String() {
		r.mu.Unlock()
		r.recordDiscrepancy(op, family.id, "reimbursement restated across channels", fmt.Sprintf("review restated reimbursement %s against settled %s", money.String(), family.settled))
		return r.quarantine(op, obs, key, receiptID, family.id, "restated reimbursement opens a discrepancy")
	}
	if !seen {
		r.seq++
		family = &acctFamily{id: fmt.Sprintf("fam-%d", r.seq), tenant: op.TenantID, entity: op.LegalEntityID,
			connection: obs.ConnectionID, account: obs.AccountID, kind: SourceReimbursement,
			object: obs.ObjectID, textID: obs.ExternalTextID, notes: obs.LocalNotes}
		r.families[slot] = family
	}
	known := make(map[string]bool, len(family.aliases)+len(obs.Aliases))
	for _, alias := range family.aliases {
		known[alias] = true
	}
	for _, alias := range obs.Aliases {
		if strings.TrimSpace(alias) == "" {
			continue
		}
		if !known[alias] {
			known[alias] = true
			family.aliases = append(family.aliases, alias)
		}
	}
	family.version = obs.Version
	family.digest = obs.Digest
	family.status = "reimbursed"
	family.amount = money.String()
	family.settled = money.String()
	family.receiptID = receiptID
	r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
	r.mu.Unlock()
	outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeApplied, FamilyID: family.id, Key: strings.TrimSpace(key)}
	r.commitOutcome(op, key, fingerprint, outcome)
	r.recordRow(op, obs, family.id, OutcomeApplied, "reimbursement settled once for the obligation")
	return outcome, nil
}

// applyStockCost records a stock valuation observation in stock authority
// scope. It never creates invoice or expense effects.
func (r *InboundRegistry) applyStockCost(op AcctOp, profile SourceProfile, obs AccountingObservation, key, fingerprint, receiptID string) (IngestOutcome, error) {
	_ = profile
	money, err := acctParseMoney(obs.Amount)
	if err != nil {
		return IngestOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	r.mu.Lock()
	slot := r.familySlot(op, obs)
	family, seen := r.families[slot]
	if seen && family.kind != SourceStockCost {
		r.mu.Unlock()
		return r.quarantine(op, obs, key, receiptID, family.id, "object is already owned by another family kind")
	}
	if !seen {
		r.seq++
		family = &acctFamily{id: fmt.Sprintf("fam-%d", r.seq), tenant: op.TenantID, entity: op.LegalEntityID,
			connection: obs.ConnectionID, account: obs.AccountID, kind: SourceStockCost,
			object: obs.ObjectID, textID: obs.ExternalTextID, notes: obs.LocalNotes}
		r.families[slot] = family
	}
	family.version = obs.Version
	family.digest = obs.Digest
	family.status = "recorded"
	family.amount = money.String()
	family.receiptID = receiptID
	r.versions[r.versionSlot(op, obs)] = &acctVersion{version: obs.Version, digest: obs.Digest, receiptID: receiptID, familyID: family.id}
	r.mu.Unlock()
	outcome := IngestOutcome{ReceiptID: receiptID, Status: OutcomeApplied, FamilyID: family.id, Key: strings.TrimSpace(key)}
	r.commitOutcome(op, key, fingerprint, outcome)
	r.recordRow(op, obs, family.id, OutcomeApplied, "stock valuation recorded in stock authority scope")
	return outcome, nil
}

func acctZero(currency string) string {
	zero, err := values.NewMoney("0", currency, 2, values.RoundingExactRequired)
	if err != nil {
		return "0.00 " + currency
	}
	return zero.String()
}

func acctCurrency(amount string) string {
	parts := strings.Split(strings.TrimSpace(amount), " ")
	if len(parts) == 2 {
		return parts[1]
	}
	return "USD"
}

// ExplainFamily explains one obligation family. Foreign scopes read as
// neutral denied, never an empty success.
func (r *InboundRegistry) ExplainFamily(op AcctOp, familyID string) (FamilyView, error) {
	if err := op.validate(); err != nil {
		return FamilyView{}, err
	}
	if !op.IngestAllowed && !op.ReviewAllowed {
		r.recordDenial(op, "read family without capability")
		return FamilyView{}, ErrAcctDenied
	}
	r.mu.Lock()
	var family *acctFamily
	for _, candidate := range r.families {
		if candidate.id == familyID {
			family = candidate
			break
		}
	}
	r.mu.Unlock()
	if family == nil || family.tenant != op.TenantID || family.entity != op.LegalEntityID {
		r.recordDenial(op, "read family outside scope")
		return FamilyView{}, ErrAcctDenied
	}
	view := FamilyView{
		FamilyID: family.id, Kind: family.kind, ObjectID: family.object,
		ExternalTextID: family.textID, Version: family.version, Status: family.status,
		Notes: family.notes, Aliases: append([]string(nil), family.aliases...),
	}
	switch family.kind {
	case SourceARInvoice:
		r.mu.Lock()
		ledger := r.ledgers[acctKey(op.TenantID, op.LegalEntityID)]
		r.mu.Unlock()
		if ledger == nil {
			return FamilyView{}, fmt.Errorf("%w: receivable owner is not bound", ErrAcctValidation)
		}
		projection, err := ledger.Explain("ar-"+family.object, acctStamp(r))
		if err != nil {
			return FamilyView{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
		}
		view.Settled = projection.Receipts.String()
		view.Remaining = projection.Remaining.String()
	case SourceAPBill:
		if family.payableID == "" {
			view.Settled = acctZero(acctCurrency(family.amount))
			view.Remaining = family.amount
			return view, nil
		}
		projection, err := r.ap.Explain(r.payOp(op), family.payableID)
		if err != nil {
			return FamilyView{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
		}
		view.Settled = projection.Paid
		view.Remaining = projection.Remaining
	case SourceReimbursement:
		view.Settled = family.settled
		view.Remaining = acctZero(acctCurrency(family.amount))
	case SourceStockCost:
		view.Settled = acctZero(acctCurrency(family.amount))
		view.Remaining = family.amount
	}
	return view, nil
}

// AdvanceCheckpoint plans one page through the syncjob lifecycle and
// persists the cursor only after durable ingest with explicit complete-page
// handling. Inbound never deletes: absence yields candidates, and the
// inferred-delete policy is refused at this boundary. Inbound job IDs match
// connection IDs so the run detail can show the checkpoint.
func (r *InboundRegistry) AdvanceCheckpoint(op AcctOp, job syncjob.Job, cursor syncjob.Cursor, items []syncjob.SourceItem, complete bool, key string) (CheckpointOutcome, error) {
	if err := op.validate(); err != nil {
		return CheckpointOutcome{}, err
	}
	if !op.IngestAllowed {
		r.recordDenial(op, "advance checkpoint without ingest capability")
		return CheckpointOutcome{}, ErrAcctDenied
	}
	if strings.TrimSpace(key) == "" {
		return CheckpointOutcome{}, fmt.Errorf("%w: idempotency key is required", ErrAcctValidation)
	}
	if job.AllowInferredDelete {
		return CheckpointOutcome{}, fmt.Errorf("%w: inbound pages never imply deletion", ErrAcctValidation)
	}
	batch, err := syncjob.PlanBatch(job, cursor, items)
	if err != nil {
		return CheckpointOutcome{}, fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	outcome := CheckpointOutcome{
		Writes: len(batch.Writes), Candidates: len(batch.Candidates),
		Failures: len(batch.Failures), Watermark: batch.Cursor.Watermark,
	}
	if !complete || len(batch.Failures) > 0 {
		return outcome, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	slot := acctKey(op.TenantID, op.LegalEntityID, job.ID)
	known := make(map[string]string, len(batch.Cursor.Known))
	for id, fingerprint := range batch.Cursor.Known {
		known[id] = fingerprint
	}
	r.checkpoints[slot] = &acctCheckpoint{watermark: batch.Cursor.Watermark, complete: true, known: known, at: acctStamp(r)}
	outcome.Advanced = true
	return outcome, nil
}

// DescribeRun aggregates one connection run for the operations surface.
func (r *InboundRegistry) DescribeRun(op AcctOp, connectionID string) (RunDetail, error) {
	if err := op.validate(); err != nil {
		return RunDetail{}, err
	}
	if !op.IngestAllowed && !op.ReviewAllowed {
		r.recordDenial(op, "read run without capability")
		return RunDetail{}, ErrAcctDenied
	}
	if strings.TrimSpace(connectionID) == "" {
		return RunDetail{}, fmt.Errorf("%w: connection is required", ErrAcctValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	detail := RunDetail{ConnectionID: strings.TrimSpace(connectionID)}
	if run, ok := r.runs[acctKey(op.TenantID, op.LegalEntityID, strings.TrimSpace(connectionID))]; ok {
		detail.Fetched = run.fetched
		detail.Applied = run.applied
		detail.Duplicates = run.duplicates
		detail.Quarantined = run.quarantined
		detail.Externally = run.externally
		detail.Rows = append([]RunRow(nil), run.rows...)
	}
	if checkpoint, ok := r.checkpoints[acctKey(op.TenantID, op.LegalEntityID, strings.TrimSpace(connectionID))]; ok {
		detail.Watermark = checkpoint.watermark
		detail.Complete = checkpoint.complete
	}
	return detail, nil
}

type acctSnapshotSource struct {
	Profile SourceProfile
	Key     string
}

type acctSnapshotFamily struct {
	ID             string
	Tenant         string
	Entity         string
	Connection     string
	Account        string
	Kind           string
	Object         string
	TextID         string
	Version        uint64
	Digest         string
	Status         string
	Amount         string
	Settled        string
	Notes          string
	Aliases        []string
	PayableID      string
	ReceiptID      string
	ExternalStatus string
}

type acctSnapshotVersion struct {
	Version   uint64
	Digest    string
	ReceiptID string
	FamilyID  string
}

type acctSnapshotIdem struct {
	Fingerprint string
	Outcome     IngestOutcome
	Tenant      string
	Entity      string
}

type acctSnapshotRun struct {
	Fetched     int
	Applied     int
	Duplicates  int
	Quarantined int
	Externally  int
	Rows        []RunRow
}

type acctSnapshotCheckpoint struct {
	Watermark int64
	Complete  bool
	Known     map[string]string
	At        string
}

type acctSnapshot struct {
	Seq           uint64
	Sources       map[string]acctSnapshotSource
	Families      map[string]acctSnapshotFamily
	Versions      map[string]acctSnapshotVersion
	Idem          map[string]acctSnapshotIdem
	Runs          map[string]acctSnapshotRun
	Checkpoints   map[string]acctSnapshotCheckpoint
	Denials       map[string][]DenialReceipt
	Discrepancies map[string][]Discrepancy
	Dedup         map[string]string
	Payable       json.RawMessage
	Ledgers       map[string]json.RawMessage
}

// Export snapshots the adapter with its owners for restart recovery.
func (r *InboundRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := acctSnapshot{
		Seq: r.seq, Sources: make(map[string]acctSnapshotSource, len(r.sources)),
		Families: make(map[string]acctSnapshotFamily, len(r.families)),
		Versions: make(map[string]acctSnapshotVersion, len(r.versions)),
		Idem:     make(map[string]acctSnapshotIdem, len(r.idem)), Runs: make(map[string]acctSnapshotRun, len(r.runs)),
		Checkpoints:   make(map[string]acctSnapshotCheckpoint, len(r.checkpoints)),
		Denials:       make(map[string][]DenialReceipt, len(r.denials)),
		Discrepancies: make(map[string][]Discrepancy, len(r.discrepancies)),
		Dedup:         make(map[string]string), Ledgers: make(map[string]json.RawMessage),
	}
	for id, source := range r.sources {
		snap.Sources[id] = acctSnapshotSource{Profile: source.profile, Key: source.key}
	}
	for id, family := range r.families {
		snap.Families[id] = acctSnapshotFamily{
			ID: family.id, Tenant: family.tenant, Entity: family.entity,
			Connection: family.connection, Account: family.account, Kind: family.kind,
			Object: family.object, TextID: family.textID, Version: family.version,
			Digest: family.digest, Status: family.status, Amount: family.amount,
			Settled: family.settled, Notes: family.notes, Aliases: append([]string(nil), family.aliases...),
			PayableID: family.payableID, ReceiptID: family.receiptID, ExternalStatus: family.externalStatus,
		}
	}
	for id, version := range r.versions {
		snap.Versions[id] = acctSnapshotVersion{Version: version.version, Digest: version.digest, ReceiptID: version.receiptID, FamilyID: version.familyID}
	}
	for id, record := range r.idem {
		snap.Idem[id] = acctSnapshotIdem{Fingerprint: record.fingerprint, Outcome: record.outcome, Tenant: record.tenant, Entity: record.entity}
	}
	for id, run := range r.runs {
		snap.Runs[id] = acctSnapshotRun{Fetched: run.fetched, Applied: run.applied, Duplicates: run.duplicates,
			Quarantined: run.quarantined, Externally: run.externally, Rows: append([]RunRow(nil), run.rows...)}
	}
	for id, checkpoint := range r.checkpoints {
		known := make(map[string]string, len(checkpoint.known))
		for item, fingerprint := range checkpoint.known {
			known[item] = fingerprint
		}
		snap.Checkpoints[id] = acctSnapshotCheckpoint{Watermark: checkpoint.watermark, Complete: checkpoint.complete, Known: known, At: checkpoint.at}
	}
	for id, receipts := range r.denials {
		snap.Denials[id] = append([]DenialReceipt(nil), receipts...)
	}
	for id, items := range r.discrepancies {
		snap.Discrepancies[id] = append([]Discrepancy(nil), items...)
	}
	r.dedup.mu.Lock()
	for slot, digest := range r.dedup.records {
		snap.Dedup[slot] = digest
	}
	r.dedup.mu.Unlock()
	payableSnap, err := r.ap.Export()
	if err != nil {
		return nil, err
	}
	snap.Payable = payableSnap
	for tenant, ledger := range r.ledgers {
		raw, err := ledger.Export()
		if err != nil {
			return nil, err
		}
		snap.Ledgers[tenant] = raw
	}
	return json.Marshal(snap)
}

// Import restores a snapshot into this registry and its bound owners. Bind
// the owners first; a restart binds fresh owners, imports, and reruns to the
// same receipts.
func (r *InboundRegistry) Import(data []byte) error {
	var snap acctSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("%w: %v", ErrAcctValidation, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq = snap.Seq
	r.sources = make(map[string]*acctSource, len(snap.Sources))
	for id, source := range snap.Sources {
		r.sources[id] = &acctSource{profile: source.Profile, key: source.Key}
	}
	r.families = make(map[string]*acctFamily, len(snap.Families))
	for id, family := range snap.Families {
		r.families[id] = &acctFamily{
			id: family.ID, tenant: family.Tenant, entity: family.Entity,
			connection: family.Connection, account: family.Account, kind: family.Kind,
			object: family.Object, textID: family.TextID, version: family.Version,
			digest: family.Digest, status: family.Status, amount: family.Amount,
			settled: family.Settled, notes: family.Notes, aliases: append([]string(nil), family.Aliases...),
			payableID: family.PayableID, receiptID: family.ReceiptID, externalStatus: family.ExternalStatus,
		}
	}
	r.versions = make(map[string]*acctVersion, len(snap.Versions))
	for id, version := range snap.Versions {
		r.versions[id] = &acctVersion{version: version.Version, digest: version.Digest, receiptID: version.ReceiptID, familyID: version.FamilyID}
	}
	r.idem = make(map[string]*acctIdem, len(snap.Idem))
	for id, record := range snap.Idem {
		r.idem[id] = &acctIdem{fingerprint: record.Fingerprint, outcome: record.Outcome, tenant: record.Tenant, entity: record.Entity}
	}
	r.runs = make(map[string]*acctRun, len(snap.Runs))
	for id, run := range snap.Runs {
		r.runs[id] = &acctRun{fetched: run.Fetched, applied: run.Applied, duplicates: run.Duplicates,
			quarantined: run.Quarantined, externally: run.Externally, rows: append([]RunRow(nil), run.Rows...)}
	}
	r.checkpoints = make(map[string]*acctCheckpoint, len(snap.Checkpoints))
	for id, checkpoint := range snap.Checkpoints {
		known := make(map[string]string, len(checkpoint.Known))
		for item, fingerprint := range checkpoint.Known {
			known[item] = fingerprint
		}
		r.checkpoints[id] = &acctCheckpoint{watermark: checkpoint.Watermark, complete: checkpoint.Complete, known: known, at: checkpoint.At}
	}
	r.denials = make(map[string][]DenialReceipt, len(snap.Denials))
	for id, receipts := range snap.Denials {
		r.denials[id] = append([]DenialReceipt(nil), receipts...)
	}
	r.discrepancies = make(map[string][]Discrepancy, len(snap.Discrepancies))
	for id, items := range snap.Discrepancies {
		r.discrepancies[id] = append([]Discrepancy(nil), items...)
	}
	r.dedup.mu.Lock()
	r.dedup.records = make(map[string]string, len(snap.Dedup))
	for slot, digest := range snap.Dedup {
		r.dedup.records[slot] = digest
	}
	r.dedup.mu.Unlock()
	if len(snap.Payable) > 0 {
		if err := r.ap.Import(snap.Payable); err != nil {
			return err
		}
	}
	for tenant, raw := range snap.Ledgers {
		ledger, ok := r.ledgers[tenant]
		if !ok || ledger == nil {
			return fmt.Errorf("%w: receivable owner for %q is not bound", ErrAcctValidation, tenant)
		}
		if err := ledger.Import(raw); err != nil {
			return err
		}
	}
	return nil
}
