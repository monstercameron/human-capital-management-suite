package workorderbilling

import "github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling/billview"

// Client-visible view values live in billview so the browser bundle does not
// link this package. These aliases keep every existing name in place.

type AdjustEvent = billview.AdjustEvent
type AdjustInvoice = billview.AdjustInvoice
type AdjustInvoiceLine = billview.AdjustInvoiceLine
type AdjustLine = billview.AdjustLine
type AdjustPreview = billview.AdjustPreview
type AdjustPreviewLine = billview.AdjustPreviewLine
type Adjustment = billview.Adjustment
type BillPreview = billview.BillPreview
type BillSourceKind = billview.BillSourceKind
type BillSourceView = billview.BillSourceView
type PricingMode = billview.PricingMode

const (
	BillKindExpense       = billview.BillKindExpense
	BillKindMaterial      = billview.BillKindMaterial
	BillKindMilestone     = billview.BillKindMilestone
	BillKindQuantity      = billview.BillKindQuantity
	BillKindSubcontract   = billview.BillKindSubcontract
	BillKindTime          = billview.BillKindTime
	PricingFixedMilestone = billview.PricingFixedMilestone
	PricingTimeMaterial   = billview.PricingTimeMaterial
	PricingUnitPrice      = billview.PricingUnitPrice
)
