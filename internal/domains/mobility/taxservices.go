// Mobility tax services (WF-CAP-018): the DELEGATED contracts binding the
// mobility tax vendor. Shadow payroll, hypothetical tax, equalization
// settlement and gross-up are computed by the tax provider; this package owns
// the contract vocabulary, the recurring hypothetical-run schedule trigger,
// and the annual settlement observation and reconciliation. Vendor transport
// arrives with WF-EXT-022's connector bindings; until then VendorPort is the
// seam a test or adapter implements.
package mobility

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidTaxContract  = errors.New("mobility: invalid delegated tax contract")
	ErrInvalidTaxSchedule  = errors.New("mobility: invalid hypothetical-run schedule")
	ErrTaxDateUnscheduled  = errors.New("mobility: date is not a scheduled hypothetical-run due date")
	ErrInvalidSettlement   = errors.New("mobility: invalid settlement statement")
	ErrTaxCurrencyMismatch = errors.New("mobility: settlement currency does not match posted shadow amounts")
)

// maxScheduledDues bounds schedule expansion: a trigger that cannot enumerate
// its dues cannot be reviewed, so an absurd schedule fails closed.
const maxScheduledDues = 120

// TaxService is the closed mobility-tax-service vocabulary. Each service is
// one DELEGATED contract: the vendor computes, this side requests, observes
// and reconciles.
type TaxService string

const (
	TaxShadowPayroll TaxService = "SHADOW_PAYROLL"
	TaxHypothetical  TaxService = "HYPOTHETICAL_TAX"
	TaxEqualization  TaxService = "EQUALIZATION_SETTLEMENT"
	TaxGrossUp       TaxService = "GROSS_UP"
)

func (s TaxService) Valid() bool {
	switch s {
	case TaxShadowPayroll, TaxHypothetical, TaxEqualization, TaxGrossUp:
		return true
	default:
		return false
	}
}

// TaxContractID is the capability namespace identity of one tax service.
func TaxContractID(s TaxService) string {
	return "hcmnext.mobility.tax." + strings.ToLower(string(s))
}

// DelegatedContract is one DELEGATED tax-service contract: the vendor named
// by VendorRef performs the computation under the pinned request/response
// schemas. VendorComputes is always true; there is deliberately no local
// compute flag to misread.
type DelegatedContract struct {
	Service        TaxService
	VendorRef      string
	RequestSchema  string
	ResponseSchema string
	DataDomains    []string
}

func (c DelegatedContract) Validate() error {
	if !c.Service.Valid() {
		return fieldError(ErrInvalidTaxContract, "service", "is not declared")
	}
	if strings.TrimSpace(c.VendorRef) == "" {
		return fieldError(ErrInvalidTaxContract, "vendor_ref", "is required")
	}
	if strings.TrimSpace(c.RequestSchema) == "" || strings.TrimSpace(c.ResponseSchema) == "" {
		return fieldError(ErrInvalidTaxContract, "schemas", "request and response schemas are required")
	}
	if len(c.DataDomains) == 0 {
		return fieldError(ErrInvalidTaxContract, "data_domains", "at least one data domain is required")
	}
	return nil
}

// VendorComputes reports the delegation direction: the tax provider computes.
func (c DelegatedContract) VendorComputes() bool { return true }

// Digest pins the contract bytes.
func (c DelegatedContract) Digest() string {
	b, err := canonicalbytes.New("hcmnext.domains.mobility.DelegatedTaxContract", schemaVersion).
		String("service", string(c.Service)).String("vendor_ref", c.VendorRef).
		String("request_schema", c.RequestSchema).String("response_schema", c.ResponseSchema).
		SortedStrings("data_domains", append([]string(nil), c.DataDomains...)).Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// DelegatedContracts returns the four mobility tax contracts bound to one
// tax-provider vendor.
func DelegatedContracts(vendorRef string) []DelegatedContract {
	contract := func(service TaxService, slot string) DelegatedContract {
		return DelegatedContract{
			Service: service, VendorRef: vendorRef,
			RequestSchema: "mobility.tax." + slot + ".request/v1", ResponseSchema: "mobility.tax." + slot + ".result/v1",
			DataDomains: []string{"mobility.tax"},
		}
	}
	return []DelegatedContract{
		contract(TaxShadowPayroll, "shadow_payroll"),
		contract(TaxHypothetical, "hypothetical_tax"),
		contract(TaxEqualization, "equalization_settlement"),
		contract(TaxGrossUp, "gross_up"),
	}
}

// TaxCadence is the closed recurring-run cadence vocabulary.
type TaxCadence string

const (
	TaxMonthly   TaxCadence = "MONTHLY"
	TaxQuarterly TaxCadence = "QUARTERLY"
)

func (c TaxCadence) Valid() bool { return c == TaxMonthly || c == TaxQuarterly }

func (c TaxCadence) months() int {
	if c == TaxQuarterly {
		return 3
	}
	return 1
}

// HypotheticalRunSchedule is the schedule trigger recurring hypothetical tax
// runs expand from: dues fall on the anchor's day-of-month (clamped to the
// month's last day) within [PeriodStart, PeriodEnd].
type HypotheticalRunSchedule struct {
	AssignmentRef string
	Cadence       TaxCadence
	Anchor        values.LocalDate
	PeriodStart   values.LocalDate
	PeriodEnd     values.LocalDate
	VendorRef     string
}

func (s HypotheticalRunSchedule) Validate() error {
	if strings.TrimSpace(s.AssignmentRef) == "" {
		return fieldError(ErrInvalidTaxSchedule, "assignment_ref", "is required")
	}
	if !s.Cadence.Valid() {
		return fieldError(ErrInvalidTaxSchedule, "cadence", "is not declared")
	}
	if s.Anchor.Validate() != nil || s.PeriodStart.Validate() != nil || s.PeriodEnd.Validate() != nil {
		return fieldError(ErrInvalidTaxSchedule, "dates", "anchor and period bounds must be valid dates")
	}
	if s.PeriodEnd.Compare(s.PeriodStart) < 0 {
		return fieldError(ErrInvalidTaxSchedule, "period", "ends before it starts")
	}
	if strings.TrimSpace(s.VendorRef) == "" {
		return fieldError(ErrInvalidTaxSchedule, "vendor_ref", "is required")
	}
	return nil
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func addMonths(d values.LocalDate, months int) values.LocalDate {
	total := int(d.Year())*12 + int(d.Month()) - 1 + months
	year, month0 := total/12, total%12
	day := int(d.Day())
	if last := daysInMonth(year, time.Month(month0+1)); day > last {
		day = last
	}
	out, err := values.NewLocalDate(year, time.Month(month0+1), day)
	if err != nil {
		return d
	}
	return out
}

// DueDates expands the scheduled dues in ascending order. Expansion is pure:
// the same schedule always yields the same dues.
func (s HypotheticalRunSchedule) DueDates() ([]values.LocalDate, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var dues []values.LocalDate
	due := s.Anchor
	for due.Compare(s.PeriodEnd) <= 0 {
		if due.Compare(s.PeriodStart) >= 0 {
			dues = append(dues, due)
		}
		if len(dues) > maxScheduledDues {
			return nil, fieldError(ErrInvalidTaxSchedule, "period", "expands beyond the reviewable due bound")
		}
		next := addMonths(due, s.Cadence.months())
		if next.Compare(due) <= 0 {
			return nil, fieldError(ErrInvalidTaxSchedule, "cadence", "does not advance")
		}
		due = next
	}
	return dues, nil
}

// NextDue returns the first due strictly after the given date.
func (s HypotheticalRunSchedule) NextDue(after values.LocalDate) (values.LocalDate, bool) {
	dues, err := s.DueDates()
	if err != nil {
		return values.LocalDate{}, false
	}
	for _, due := range dues {
		if due.Compare(after) > 0 {
			return due, true
		}
	}
	return values.LocalDate{}, false
}

// HypotheticalRunRequest is one scheduled vendor run. The idempotency key
// derives from the assignment and due date, so a retried trigger submits one
// run.
type HypotheticalRunRequest struct {
	AssignmentRef  string
	Period         string
	DueDate        values.LocalDate
	VendorRef      string
	IdempotencyKey string
}

// RunRequest binds a scheduled due date to its vendor run request. An
// off-schedule date is refused: the vendor never runs outside the trigger.
func (s HypotheticalRunSchedule) RunRequest(due values.LocalDate) (HypotheticalRunRequest, error) {
	dues, err := s.DueDates()
	if err != nil {
		return HypotheticalRunRequest{}, err
	}
	scheduled := false
	for _, d := range dues {
		if d.Compare(due) == 0 {
			scheduled = true
			break
		}
	}
	if !scheduled {
		return HypotheticalRunRequest{}, fmt.Errorf("%w: %s", ErrTaxDateUnscheduled, due.String())
	}
	return HypotheticalRunRequest{
		AssignmentRef: s.AssignmentRef, Period: fmt.Sprintf("%04d-%02d", due.Year(), due.Month()),
		DueDate: due, VendorRef: s.VendorRef,
		IdempotencyKey: "hypotax:" + s.AssignmentRef + ":" + due.String(),
	}, nil
}

// SettlementLineKind is the closed settlement-line vocabulary.
type SettlementLineKind string

const (
	SettlementWithheld     SettlementLineKind = "WITHHELD"
	SettlementHypothetical SettlementLineKind = "HYPOTHETICAL"
	SettlementActual       SettlementLineKind = "ACTUAL"
)

func (k SettlementLineKind) Valid() bool {
	switch k {
	case SettlementWithheld, SettlementHypothetical, SettlementActual:
		return true
	default:
		return false
	}
}

// SettlementLine is one observed vendor settlement line in one jurisdiction.
type SettlementLine struct {
	Jurisdiction string
	Kind         SettlementLineKind
	Amount       values.Money
}

// SettlementStatement is the observed annual vendor settlement: evidence,
// never a local computation.
type SettlementStatement struct {
	AssignmentRef string
	TaxYear       int
	VendorRef     string
	StatementRef  string
	Lines         []SettlementLine
}

func (s SettlementStatement) Validate() error {
	if strings.TrimSpace(s.AssignmentRef) == "" {
		return fieldError(ErrInvalidSettlement, "assignment_ref", "is required")
	}
	if s.TaxYear < 1900 || s.TaxYear > 9999 {
		return fieldError(ErrInvalidSettlement, "tax_year", "is not a calendar year")
	}
	if strings.TrimSpace(s.VendorRef) == "" || strings.TrimSpace(s.StatementRef) == "" {
		return fieldError(ErrInvalidSettlement, "refs", "vendor and statement refs are required")
	}
	if len(s.Lines) == 0 {
		return fieldError(ErrInvalidSettlement, "lines", "at least one settlement line is required")
	}
	seen := map[string]bool{}
	for _, line := range s.Lines {
		if strings.TrimSpace(line.Jurisdiction) == "" || !line.Kind.Valid() || line.Amount.Validate() != nil {
			return fieldError(ErrInvalidSettlement, "lines", "each line needs jurisdiction, kind and valid money")
		}
		key := line.Jurisdiction + "\x00" + string(line.Kind)
		if seen[key] {
			return fieldError(ErrInvalidSettlement, "lines", "duplicate jurisdiction and kind")
		}
		seen[key] = true
	}
	return nil
}

// PostedShadowAmount is one locally posted shadow-payroll amount the annual
// settlement reconciles against.
type PostedShadowAmount struct {
	AssignmentRef string
	TaxYear       int
	Jurisdiction  string
	Amount        values.Money
	PostingRef    string
}

func (p PostedShadowAmount) Validate() error {
	if strings.TrimSpace(p.AssignmentRef) == "" || strings.TrimSpace(p.Jurisdiction) == "" || strings.TrimSpace(p.PostingRef) == "" {
		return fieldError(ErrInvalidSettlement, "posting", "assignment, jurisdiction and posting refs are required")
	}
	if p.Amount.Validate() != nil {
		return fieldError(ErrInvalidSettlement, "posting", "amount must be valid money")
	}
	return nil
}

// SettlementDelta is one jurisdiction where posted shadow amounts and the
// vendor settlement disagree. Difference is posted minus settled.
type SettlementDelta struct {
	Jurisdiction string
	Posted       values.Money
	Settled      values.Money
	Difference   values.Money
}

// SettlementReconciliation is the annual settlement verdict.
type SettlementReconciliation struct {
	Balanced bool
	Covered  []string
	Deltas   []SettlementDelta
}

// ReconcileSettlement compares posted shadow totals per jurisdiction against
// the vendor statement's ACTUAL lines. A currency mismatch fails closed: no
// FX rate is ever invented here.
func ReconcileSettlement(posted []PostedShadowAmount, statement SettlementStatement) (SettlementReconciliation, error) {
	if err := statement.Validate(); err != nil {
		return SettlementReconciliation{}, err
	}
	totals := map[string]values.Money{}
	seenPostings := map[string]bool{}
	for _, p := range posted {
		if err := p.Validate(); err != nil {
			return SettlementReconciliation{}, err
		}
		if p.AssignmentRef != statement.AssignmentRef || p.TaxYear != statement.TaxYear {
			return SettlementReconciliation{}, fieldError(ErrInvalidSettlement, "posting", "assignment or tax year does not match the statement")
		}
		if seenPostings[p.PostingRef] {
			return SettlementReconciliation{}, fieldError(ErrInvalidSettlement, "posting", "duplicate posting ref")
		}
		seenPostings[p.PostingRef] = true
		running, ok := totals[p.Jurisdiction]
		if !ok {
			totals[p.Jurisdiction] = p.Amount
			continue
		}
		sum, err := running.Add(p.Amount)
		if err != nil {
			return SettlementReconciliation{}, fmt.Errorf("%w: %s: %v", ErrTaxCurrencyMismatch, p.Jurisdiction, err)
		}
		totals[p.Jurisdiction] = sum
	}
	var covered []string
	var deltas []SettlementDelta
	for _, line := range statement.Lines {
		if line.Kind != SettlementActual {
			continue
		}
		covered = append(covered, line.Jurisdiction)
		postedTotal, ok := totals[line.Jurisdiction]
		if !ok {
			zero, err := values.NewMoney("0", line.Amount.Currency(), 2, values.RoundingExactRequired)
			if err != nil {
				return SettlementReconciliation{}, err
			}
			postedTotal = zero
		}
		cmp, err := postedTotal.Cmp(line.Amount)
		if err != nil {
			return SettlementReconciliation{}, fmt.Errorf("%w: %s: %v", ErrTaxCurrencyMismatch, line.Jurisdiction, err)
		}
		if cmp == 0 {
			continue
		}
		diff, err := postedTotal.Sub(line.Amount)
		if err != nil {
			return SettlementReconciliation{}, fmt.Errorf("%w: %s: %v", ErrTaxCurrencyMismatch, line.Jurisdiction, err)
		}
		deltas = append(deltas, SettlementDelta{Jurisdiction: line.Jurisdiction, Posted: postedTotal, Settled: line.Amount, Difference: diff})
	}
	return SettlementReconciliation{Balanced: len(deltas) == 0, Covered: covered, Deltas: deltas}, nil
}

// VendorRunReceipt is the vendor's acknowledgement of one submitted run.
type VendorRunReceipt struct {
	RequestKey string
	RunRef     string
}

// VendorPort is the seam WF-EXT-022's connector bindings implement: submit a
// scheduled run, observe the annual settlement. It carries references and
// digests, never credentials.
type VendorPort interface {
	SubmitHypotheticalRun(ctx context.Context, req HypotheticalRunRequest) (VendorRunReceipt, error)
	ObserveSettlement(ctx context.Context, assignmentRef string, taxYear int) (SettlementStatement, error)
}
