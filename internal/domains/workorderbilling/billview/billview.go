// Package billview holds the billing view values the browser client renders:
// adjustment memos, totals previews and customer-safe source families. It
// imports nothing, so the client never links the billing registry or the
// memo template engine behind it.
package billview

// AdjustInvoiceLine is one frozen invoiced line inside a bound snapshot.
type AdjustInvoiceLine struct {
	LineID      string
	Description string
	Qty         string
	UnitPrice   string
	Amount      string
	TaxAmount   string
}

// AdjustInvoice is the frozen invoice snapshot adjustments pin. The server
// adapter resolves it from the invoice owner; the registry never mutates it.
type AdjustInvoice struct {
	InvoiceID  string
	CustomerID string
	Currency   string
	Number     string
	Status     string
	Revision   uint64
	Digest     string
	IssuedAt   string
	Lines      []AdjustInvoiceLine
	Total      string
	TaxTotal   string
}

// AdjustLine is one frozen adjustment line with its original treatment.
type AdjustLine struct {
	LineID         string
	Description    string
	Qty            string
	NetAmount      string
	TaxAmount      string
	TotalAmount    string
	OriginalQty    string
	OriginalAmount string
	OriginalTax    string
}

// Adjustment is one durable credit or debit memo.
type Adjustment struct {
	AdjustmentID    string
	TenantID        string
	LegalEntityID   string
	CustomerID      string
	Currency        string
	Kind            string
	InvoiceID       string
	InvoiceRevision uint64
	InvoiceDigest   string
	Lines           []AdjustLine
	NetAmount       string
	TaxAmount       string
	TotalAmount     string
	Reason          string
	EvidenceRef     string
	TermsRef        string
	CorrectsRef     string
	Status          string
	DraftedBy       string
	SubmittedBy     string
	ApprovedBy      string
	IssuedBy        string
	Number          string
	DocumentDigest  string
	SettledAmount   string
	AllocationID    string
	CreatedAt       string
	IssuedAt        string
	Events          []AdjustEvent
}

// AdjustPreviewLine carries per-line original, consumed, available and
// proposed treatment for the adjustment drawer.
type AdjustPreviewLine struct {
	LineID          string
	Description     string
	OriginalQty     string
	OriginalAmount  string
	OriginalTax     string
	CreditedQty     string
	CreditedAmount  string
	CreditedTax     string
	AvailableQty    string
	AvailableAmount string
	AvailableTax    string
	ProposedQty     string
	ProposedAmount  string
	ProposedTax     string
}

// AdjustPreview answers a totals preview without recording anything.
type AdjustPreview struct {
	InvoiceID   string
	Kind        string
	NetAmount   string
	TaxAmount   string
	TotalAmount string
	Currency    string
	Lines       []AdjustPreviewLine
}

// AdjustEvent is one audit entry on an adjustment.
type AdjustEvent struct {
	Kind   string
	Actor  string
	At     string
	Detail string
}

// BillSourceKind names the producer shape behind one source family.
type BillSourceKind string

const (
	BillKindTime        BillSourceKind = "time"
	BillKindQuantity    BillSourceKind = "quantity"
	BillKindMaterial    BillSourceKind = "material"
	BillKindSubcontract BillSourceKind = "subcontract"
	BillKindMilestone   BillSourceKind = "milestone"
	BillKindExpense     BillSourceKind = "expense"
)

// BillPreview answers a totals preview without mutating the registry.
type BillPreview struct {
	FamilyID             string
	Revision             uint64
	BillableQty          string
	BillableUnit         string
	Amount               string
	Currency             string
	RemainingAfterQty    string
	RemainingAfterAmount string
}

// BillSourceView is one customer-safe family row for drawers and adapters.
type BillSourceView struct {
	FamilyID          string
	Revision          uint64
	Description       string
	ServiceDate       string
	Kind              BillSourceKind
	Unit              string
	UnitDimension     string
	ApprovedQty       string
	ClaimedQty        string
	RemainingQty      string
	ApprovedAmount    string
	ClaimedAmount     string
	RemainingAmount   string
	CostAmount        string
	BillableUnit      string
	BillableRemaining string
	RateRef           string
	PricingMode       PricingMode
	Category          string
	MilestoneRef      string
	Eligible          bool
	Reasons           []string
}

type PricingMode string

const (
	PricingUnitPrice      PricingMode = "unit-price"
	PricingTimeMaterial   PricingMode = "time-material"
	PricingFixedMilestone PricingMode = "fixed-milestone"
)
