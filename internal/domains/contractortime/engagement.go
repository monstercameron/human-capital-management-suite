package contractortime

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PricingModel is how a contractor engagement is priced. It is data on the
// engagement, never inferred from the time entries it later prices.
type PricingModel string

const (
	PricingHourly    PricingModel = "HOURLY"
	PricingMilestone PricingModel = "MILESTONE"
	PricingFixed     PricingModel = "FIXED"
)

func (p PricingModel) Valid() bool {
	switch p {
	case PricingHourly, PricingMilestone, PricingFixed:
		return true
	}
	return false
}

// TaxKind is the tax treatment applied to an invoice total. Both VAT and
// withholding are represented, and which one applies is data supplied by the
// caller (the tenant's tax configuration), never guessed by this package.
type TaxKind string

const (
	TaxNone        TaxKind = "NONE"
	TaxVAT         TaxKind = "VAT"         // added on top of the subtotal
	TaxWithholding TaxKind = "WITHHOLDING" // deducted from the subtotal
)

func (k TaxKind) Valid() bool {
	switch k {
	case TaxNone, TaxVAT, TaxWithholding:
		return true
	}
	return false
}

// TaxTreatment names the tax rule and rate to apply. Rate is ignored when Kind
// is TaxNone. Jurisdiction is a free-text label carried through for audit; it
// drives no branching here.
type TaxTreatment struct {
	Kind         TaxKind
	Rate         values.Percentage
	Jurisdiction string
}

func (t TaxTreatment) validate() error {
	if !t.Kind.Valid() {
		return reject("Tax.Kind", string(t.Kind), "unknown tax treatment kind")
	}
	if t.Kind == TaxNone {
		return nil
	}
	if err := t.Rate.Validate(); err != nil {
		return reject("Tax.Rate", string(t.Kind), "tax rate is required when a tax treatment is declared: "+err.Error())
	}
	return nil
}

// Engagement is a contractor's pricing contract: the SOW or PO it is bound
// to, its pricing model, rate card, currency, tax treatment and self-billing
// flag. Every invoice built from this engagement must stay inside it; a rate
// or milestone not on the engagement is rejected, never guessed.
type Engagement struct {
	ID        string
	WorkerRef string
	// SOWRef or PORef; the engagement must be bound to exactly one governing
	// document and every invoice traces back to it.
	SOWRef string

	Pricing     PricingModel
	Currency    string
	AmountScale int32
	Rounding    values.RoundingMode

	// RateCard maps a rate code to its approved money-per-HOUR rate. Used only
	// when Pricing is HOURLY. A time entry referencing a rate code absent here
	// is rejected rather than priced at a guessed rate.
	RateCard map[string]values.Rate

	// MilestoneAmounts maps a milestone id to its approved fixed amount. Used
	// only when Pricing is MILESTONE.
	MilestoneAmounts map[string]values.Money

	// FixedAmount is the approved recurring fee. Used only when Pricing is
	// FIXED.
	FixedAmount values.Money

	Tax TaxTreatment

	// SelfBilling is true when the client, not the contractor, issues the
	// invoice document (a self-billed invoice / RCTI arrangement).
	SelfBilling bool
}

// Validate reports whether the engagement is complete and internally
// consistent for its declared pricing model.
func (e Engagement) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return reject("ID", "", "engagement id is required")
	}
	if strings.TrimSpace(e.WorkerRef) == "" {
		return reject("WorkerRef", "", "engagement worker reference is required")
	}
	if strings.TrimSpace(e.SOWRef) == "" {
		return reject("SOWRef", "", "engagement must name a SOW or PO")
	}
	if !e.Pricing.Valid() {
		return reject("Pricing", string(e.Pricing), "unknown pricing model")
	}
	if _, err := values.NewMoney("0", e.Currency, e.AmountScale, e.Rounding); err != nil {
		return reject("Currency", e.Currency, "currency, amount scale or rounding mode invalid: "+err.Error())
	}
	if err := e.Tax.validate(); err != nil {
		return err
	}
	switch e.Pricing {
	case PricingHourly:
		if len(e.RateCard) == 0 {
			return reject("RateCard", "HOURLY", "hourly engagement needs at least one approved rate")
		}
		for code, rate := range e.RateCard {
			if strings.TrimSpace(code) == "" {
				return reject("RateCard", "HOURLY", "rate code cannot be empty")
			}
			if err := rate.Validate(); err != nil {
				return reject("RateCard["+code+"]", "HOURLY", "rate invalid: "+err.Error())
			}
			if rate.PerUnit() != "HOUR" {
				return reject("RateCard["+code+"]", "HOURLY", "rate must be per HOUR")
			}
			money, ok := rate.MoneyNumerator()
			if !ok || money.Currency() != e.Currency {
				return reject("RateCard["+code+"]", "HOURLY", "rate currency must match the engagement currency")
			}
		}
	case PricingMilestone:
		if len(e.MilestoneAmounts) == 0 {
			return reject("MilestoneAmounts", "MILESTONE", "milestone engagement needs at least one approved amount")
		}
		for id, amount := range e.MilestoneAmounts {
			if strings.TrimSpace(id) == "" {
				return reject("MilestoneAmounts", "MILESTONE", "milestone id cannot be empty")
			}
			if err := amount.Validate(); err != nil || amount.Currency() != e.Currency {
				return reject("MilestoneAmounts["+id+"]", "MILESTONE", "milestone amount invalid or wrong currency")
			}
		}
	case PricingFixed:
		if err := e.FixedAmount.Validate(); err != nil || e.FixedAmount.Currency() != e.Currency {
			return reject("FixedAmount", "FIXED", "fixed engagement needs a valid amount in the engagement currency")
		}
	}
	return nil
}
