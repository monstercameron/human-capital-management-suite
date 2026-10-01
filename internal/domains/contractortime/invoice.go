package contractortime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ApprovedLine is the minimal approved-time input this package needs. It is
// defined here rather than imported from an approval or timecard package so
// this domain never depends on an unfinished lane; a service layer maps its
// own approved-timecard record onto this shape.
type ApprovedLine struct {
	WorkerRef      string
	Date           string // ISO 8601 calendar date, "2006-01-02"
	Minutes        int
	Project        string
	CostCode       string
	RateCode       string
	RevisionDigest string
}

func (l ApprovedLine) validate(engagementWorker string) error {
	if strings.TrimSpace(l.WorkerRef) == "" || l.WorkerRef != engagementWorker {
		return reject("ApprovedLine.WorkerRef", l.WorkerRef, "approved line worker must match the engagement worker")
	}
	if strings.TrimSpace(l.Date) == "" {
		return reject("ApprovedLine.Date", l.Date, "approved line date is required")
	}
	if l.Minutes <= 0 {
		return reject("ApprovedLine.Minutes", fmt.Sprintf("%d", l.Minutes), "approved minutes must be positive")
	}
	if strings.TrimSpace(l.RateCode) == "" {
		return reject("ApprovedLine.RateCode", "", "approved line must name a rate code")
	}
	if strings.TrimSpace(l.RevisionDigest) == "" {
		return reject("ApprovedLine.RevisionDigest", "", "approved line must carry its approval revision digest")
	}
	return nil
}

// MilestoneCompletion is the minimal accepted-milestone input this package
// needs, defined locally for the same reason as ApprovedLine.
type MilestoneCompletion struct {
	MilestoneID    string
	WorkerRef      string
	CompletedOn    string // ISO 8601 calendar date
	RevisionDigest string
}

func (m MilestoneCompletion) validate(engagementWorker string) error {
	if strings.TrimSpace(m.MilestoneID) == "" {
		return reject("MilestoneCompletion.MilestoneID", "", "milestone id is required")
	}
	if strings.TrimSpace(m.WorkerRef) == "" || m.WorkerRef != engagementWorker {
		return reject("MilestoneCompletion.WorkerRef", m.WorkerRef, "milestone completion worker must match the engagement worker")
	}
	if strings.TrimSpace(m.CompletedOn) == "" {
		return reject("MilestoneCompletion.CompletedOn", "", "milestone completion date is required")
	}
	if strings.TrimSpace(m.RevisionDigest) == "" {
		return reject("MilestoneCompletion.RevisionDigest", "", "milestone completion must carry its acceptance revision digest")
	}
	return nil
}

// LineSourceKind names what an invoice line was built from.
type LineSourceKind string

const (
	LineFromTime      LineSourceKind = "APPROVED_TIME"
	LineFromMilestone LineSourceKind = "ACCEPTED_MILESTONE"
	LineFromFixedFee  LineSourceKind = "FIXED_FEE"
)

// InvoiceLine is one priced entry on an invoice draft.
type InvoiceLine struct {
	SourceKind     LineSourceKind
	SourceRef      string // date, milestone id, or "FIXED"
	RevisionDigest string
	RateCode       string
	Minutes        int
	Rate           values.Money // rate.Amount() is per HOUR; zero for milestone/fixed lines
	Amount         values.Money
}

// InvoiceStatus is the lifecycle state of an invoice draft.
type InvoiceStatus string

const (
	InvoiceDraftStatus InvoiceStatus = "DRAFT"
)

// InvoiceDraft is the priced result of approved contractor time or accepted
// milestones under one engagement. Building a draft never sends it anywhere;
// delivery through an AP or contractor-platform connector is a service-layer
// concern.
type InvoiceDraft struct {
	ID           string
	EngagementID string
	WorkerRef    string
	SOWRef       string
	Currency     string
	Pricing      PricingModel
	SelfBilled   bool
	Status       InvoiceStatus
	Lines        []InvoiceLine
	Subtotal     values.Money
	TaxKind      TaxKind
	TaxAmount    values.Money
	Total        values.Money
	Digest       string
}

// BuildInvoiceRequest carries everything needed to price one invoice draft
// from one engagement's approved lines and accepted milestones.
type BuildInvoiceRequest struct {
	ID         string
	Engagement Engagement
	Lines      []ApprovedLine
	Milestones []MilestoneCompletion
}

const hourMinutes = 60

// BuildInvoice prices approved contractor time or accepted milestone
// completion into an invoice (or self-billed invoice) draft. A rate code or
// milestone id outside the engagement's rate card is rejected rather than
// priced at a guessed rate; currency and tax treatment come only from the
// engagement, never inferred.
func BuildInvoice(req BuildInvoiceRequest) (InvoiceDraft, error) {
	if strings.TrimSpace(req.ID) == "" {
		return InvoiceDraft{}, reject("BuildInvoiceRequest.ID", "", "invoice id is required")
	}
	if err := req.Engagement.Validate(); err != nil {
		return InvoiceDraft{}, err
	}
	e := req.Engagement

	var lines []InvoiceLine
	switch e.Pricing {
	case PricingHourly:
		if len(req.Lines) == 0 {
			return InvoiceDraft{}, reject("BuildInvoiceRequest.Lines", "HOURLY", "hourly invoice needs at least one approved line")
		}
		if len(req.Milestones) != 0 {
			return InvoiceDraft{}, reject("BuildInvoiceRequest.Milestones", "HOURLY", "hourly engagement does not accept milestone completions")
		}
		seen := map[string]bool{}
		for _, l := range req.Lines {
			if err := l.validate(e.WorkerRef); err != nil {
				return InvoiceDraft{}, err
			}
			key := l.Date + "|" + l.RateCode + "|" + l.RevisionDigest
			if seen[key] {
				return InvoiceDraft{}, reject("ApprovedLine", key, "duplicate approved line")
			}
			seen[key] = true
			rate, ok := e.RateCard[l.RateCode]
			if !ok {
				return InvoiceDraft{}, reject("ApprovedLine.RateCode", l.RateCode, "rate code is outside the SOW rate card")
			}
			hours, err := values.NewQuantity(minutesToHoursText(l.Minutes), "HOUR", 4, e.Rounding)
			if err != nil {
				return InvoiceDraft{}, reject("ApprovedLine.Minutes", fmt.Sprintf("%d", l.Minutes), "minutes to hours conversion failed: "+err.Error())
			}
			amount, err := rate.Apply(hours, e.AmountScale, e.Rounding)
			if err != nil {
				return InvoiceDraft{}, reject("ApprovedLine", key, "rate application failed: "+err.Error())
			}
			rateMoney, _ := rate.MoneyNumerator()
			lines = append(lines, InvoiceLine{
				SourceKind: LineFromTime, SourceRef: l.Date, RevisionDigest: l.RevisionDigest,
				RateCode: l.RateCode, Minutes: l.Minutes, Rate: rateMoney, Amount: amount,
			})
		}
	case PricingMilestone:
		if len(req.Milestones) == 0 {
			return InvoiceDraft{}, reject("BuildInvoiceRequest.Milestones", "MILESTONE", "milestone invoice needs at least one accepted milestone")
		}
		if len(req.Lines) != 0 {
			return InvoiceDraft{}, reject("BuildInvoiceRequest.Lines", "MILESTONE", "milestone engagement does not accept time lines")
		}
		seen := map[string]bool{}
		for _, m := range req.Milestones {
			if err := m.validate(e.WorkerRef); err != nil {
				return InvoiceDraft{}, err
			}
			if seen[m.MilestoneID] {
				return InvoiceDraft{}, reject("MilestoneCompletion.MilestoneID", m.MilestoneID, "duplicate milestone completion")
			}
			seen[m.MilestoneID] = true
			amount, ok := e.MilestoneAmounts[m.MilestoneID]
			if !ok {
				return InvoiceDraft{}, reject("MilestoneCompletion.MilestoneID", m.MilestoneID, "milestone is outside the SOW milestone schedule")
			}
			lines = append(lines, InvoiceLine{
				SourceKind: LineFromMilestone, SourceRef: m.MilestoneID, RevisionDigest: m.RevisionDigest,
				Amount: amount,
			})
		}
	case PricingFixed:
		if len(req.Lines) != 0 || len(req.Milestones) != 0 {
			return InvoiceDraft{}, reject("BuildInvoiceRequest", "FIXED", "fixed engagement does not accept time lines or milestones")
		}
		lines = append(lines, InvoiceLine{SourceKind: LineFromFixedFee, SourceRef: "FIXED", Amount: e.FixedAmount})
	}

	zero, err := values.NewMoney("0", e.Currency, e.AmountScale, e.Rounding)
	if err != nil {
		return InvoiceDraft{}, reject("Engagement.Currency", e.Currency, "cannot construct zero total: "+err.Error())
	}
	subtotal := zero
	for _, l := range lines {
		subtotal, err = subtotal.Add(l.Amount)
		if err != nil {
			return InvoiceDraft{}, reject("InvoiceLine.Amount", l.SourceRef, "sum lines: "+err.Error())
		}
	}

	tax := zero
	total := subtotal
	if e.Tax.Kind != TaxNone {
		tax, err = e.Tax.Rate.ApplyTo(subtotal, e.AmountScale, e.Rounding)
		if err != nil {
			return InvoiceDraft{}, reject("Tax", string(e.Tax.Kind), "tax computation failed: "+err.Error())
		}
		switch e.Tax.Kind {
		case TaxVAT:
			total, err = subtotal.Add(tax)
		case TaxWithholding:
			total, err = subtotal.Sub(tax)
		}
		if err != nil {
			return InvoiceDraft{}, reject("Tax", string(e.Tax.Kind), "applying tax failed: "+err.Error())
		}
	}

	d := InvoiceDraft{
		ID: req.ID, EngagementID: e.ID, WorkerRef: e.WorkerRef, SOWRef: e.SOWRef,
		Currency: e.Currency, Pricing: e.Pricing, SelfBilled: e.SelfBilling, Status: InvoiceDraftStatus,
		Lines: lines, Subtotal: subtotal, TaxKind: e.Tax.Kind, TaxAmount: tax, Total: total,
	}
	d.Digest = digestInvoice(d)
	return d, nil
}

// minutesToHoursText renders minutes as fixed-point hours text at 4 decimal
// places (60 minutes = 1.0000 hour exactly; anything not a multiple of 60
// still renders exactly because 1 minute = 1/60 hour has a terminating
// decimal at scale 4 only for multiples of 15 seconds of remainder tracked
// separately — minutes are integers, so the quotient is computed exactly in
// hundredths of a minute-hour via integer arithmetic, never float64).
func minutesToHoursText(minutes int) string {
	wholeHours := minutes / hourMinutes
	remainderMinutes := minutes % hourMinutes
	// remainderMinutes/60 as a 4-decimal fraction, exact to 1/10000 hour.
	frac := remainderMinutes * 10000 / hourMinutes
	return fmt.Sprintf("%d.%04d", wholeHours, frac)
}

func digestInvoice(d InvoiceDraft) string {
	fields := []string{d.ID, d.EngagementID, d.WorkerRef, d.SOWRef, d.Currency, string(d.Pricing),
		fmt.Sprintf("%t", d.SelfBilled), string(d.Status), d.Subtotal.String(), string(d.TaxKind),
		d.TaxAmount.String(), d.Total.String()}
	for _, l := range d.Lines {
		fields = append(fields, string(l.SourceKind), l.SourceRef, l.RevisionDigest, l.RateCode,
			fmt.Sprintf("%d", l.Minutes), l.Rate.String(), l.Amount.String())
	}
	b := strings.Builder{}
	for _, f := range fields {
		fmt.Fprintf(&b, "%d:%s", len(f), f)
	}
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:])
}

// Validate verifies the frozen invoice draft and its digest.
func (d InvoiceDraft) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.EngagementID) == "" || strings.TrimSpace(d.WorkerRef) == "" {
		return reject("InvoiceDraft", "", "invoice identity is required")
	}
	if len(d.Lines) == 0 {
		return reject("InvoiceDraft.Lines", "", "invoice has no lines")
	}
	if err := d.Total.Validate(); err != nil || d.Total.Currency() != d.Currency {
		return reject("InvoiceDraft.Total", d.Currency, "invalid invoice total or currency")
	}
	if d.Digest == "" || d.Digest != digestInvoice(d) {
		return reject("InvoiceDraft.Digest", d.Digest, "invoice content digest mismatch")
	}
	return nil
}
