// Package workorderbilling creates auditable customer-job billing drafts.
// It does not issue accounts-receivable invoices or record payments.
package workorderbilling

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrRejected = errors.New("work order billing request rejected")

type PricingMode string

const (
	PricingUnitPrice      PricingMode = "unit-price"
	PricingTimeMaterial   PricingMode = "time-material"
	PricingFixedMilestone PricingMode = "fixed-milestone"
)

// PricingPolicy is immutable once referenced by a draft. Version must identify
// the approved customer contract schedule; calculations never infer FX/tax.
type PricingPolicy struct {
	ID, Version, Currency string
	Mode                  PricingMode
	AmountScale           int32
	Rounding              values.RoundingMode
	UnitRates             map[string]values.Decimal
	TMMultipliers         map[string]values.Decimal
	MilestoneAmounts      map[string]values.Money
}

type SourceKind string

const (
	SourceAcceptedQuantity    SourceKind = "accepted-quantity"
	SourceApprovedTime        SourceKind = "approved-time"
	SourceApprovedMaterial    SourceKind = "approved-material"
	SourceApprovedSubcontract SourceKind = "approved-subcontract"
	SourceAcceptedMilestone   SourceKind = "accepted-milestone"
)

// Source is a frozen view of a governed work-order record at its revision.
// Quantity/Unit is used for measured or time sources; Amount for costs.
type Source struct {
	ID, Revision, WorkOrderID   string
	Kind                        SourceKind
	Accepted, Approved          bool
	Quantity                    values.Decimal
	Unit, Category, MilestoneID string
	Amount                      values.Money
}

type Line struct {
	ID                                   string
	SourceID, SourceRevision, SourceKind string
	Description                          string
	Quantity                             values.Decimal
	Unit                                 string
	UnitPrice                            values.Money
	PricingFactor                        values.Decimal
	SourceAmount                         values.Money
	Category                             string
	Amount                               values.Money
	PolicyID, PolicyVersion              string
}

type Status string

const (
	Draft      Status = "draft"
	Approved   Status = "approved"
	Rejected   Status = "rejected"
	Issued     Status = "issued"
	Exported   Status = "exported"
	Reconciled Status = "reconciled"
	Corrected  Status = "corrected"
)

type Event struct{ Type, Actor, At, IdempotencyKey, Detail string }
type BillingDraft struct {
	ID, WorkOrderID, PolicyID, PolicyVersion string
	Currency                                 string
	Mode                                     PricingMode
	Status                                   Status
	Lines                                    []Line
	Total                                    values.Money
	Events                                   []Event
	ReconcilesTo                             string
	CorrectionOf                             string
	Digest                                   string
}

// Money's text marshaler is intentionally write-only in kernel/values. These
// adapters keep the domain artifact compatible with ordinary JSON persistence.
func parseMoneyJSON(raw json.RawMessage) (values.Money, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return values.Money{}, err
	}
	space := strings.LastIndexByte(text, ' ')
	if space < 0 {
		return values.Money{}, fmt.Errorf("invalid money text")
	}
	decimalAndMode, currency := text[:space], text[space+1:]
	slash := strings.LastIndexByte(decimalAndMode, '/')
	if slash < 0 {
		return values.Money{}, fmt.Errorf("money text missing rounding mode")
	}
	number, modeText := decimalAndMode[:slash], decimalAndMode[slash+1:]
	mode, err := values.ParseRoundingMode(modeText)
	if err != nil {
		return values.Money{}, err
	}
	frac := ""
	if dot := strings.IndexByte(number, '.'); dot >= 0 {
		frac = number[dot+1:]
	}
	scale := len(frac)
	return values.NewMoney(number, currency, int32(scale), mode)
}

func (l *Line) UnmarshalJSON(data []byte) error {
	type plain Line
	var wire struct {
		*plain
		UnitPrice    json.RawMessage `json:"UnitPrice"`
		SourceAmount json.RawMessage `json:"SourceAmount"`
		Amount       json.RawMessage `json:"Amount"`
	}
	wire.plain = (*plain)(l)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	var err error
	if l.UnitPrice, err = parseMoneyJSON(wire.UnitPrice); err != nil {
		return err
	}
	if l.SourceAmount, err = parseMoneyJSON(wire.SourceAmount); err != nil {
		return err
	}
	if l.Amount, err = parseMoneyJSON(wire.Amount); err != nil {
		return err
	}
	return nil
}

func (d *BillingDraft) UnmarshalJSON(data []byte) error {
	type plain BillingDraft
	var wire struct {
		*plain
		Total json.RawMessage `json:"Total"`
	}
	wire.plain = (*plain)(d)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	total, err := parseMoneyJSON(wire.Total)
	if err != nil {
		return err
	}
	d.Total = total
	return nil
}

type BuildRequest struct {
	ID, WorkOrderID     string
	Policy              PricingPolicy
	Sources             []Source
	DescriptionBySource map[string]string
}

func fail(f string, a ...any) error { return errors.Join(ErrRejected, fmt.Errorf(f, a...)) }

func (p PricingPolicy) validate() error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Version) == "" {
		return fail("pricing policy id and version are required")
	}
	if p.Mode != PricingUnitPrice && p.Mode != PricingTimeMaterial && p.Mode != PricingFixedMilestone {
		return fail("unknown pricing mode %q", p.Mode)
	}
	if _, err := values.NewMoney("0", p.Currency, p.AmountScale, p.Rounding); err != nil {
		return fail("currency/amount precision: %v", err)
	}
	return nil
}

func Build(req BuildRequest) (BillingDraft, error) {
	if req.ID == "" || req.WorkOrderID == "" {
		return BillingDraft{}, fail("draft and work order ids are required")
	}
	if err := req.Policy.validate(); err != nil {
		return BillingDraft{}, err
	}
	d := BillingDraft{ID: req.ID, WorkOrderID: req.WorkOrderID, PolicyID: req.Policy.ID, PolicyVersion: req.Policy.Version, Currency: req.Policy.Currency, Mode: req.Policy.Mode, Status: Draft}
	seen := map[string]bool{}
	for _, s := range req.Sources {
		if s.ID == "" || s.Revision == "" || s.WorkOrderID != req.WorkOrderID {
			return BillingDraft{}, fail("source identity, revision, or work order mismatch")
		}
		if seen[s.ID] {
			return BillingDraft{}, fail("duplicate source %q", s.ID)
		}
		seen[s.ID] = true
		line, err := price(req.Policy, s, req.DescriptionBySource[s.ID])
		if err != nil {
			return BillingDraft{}, err
		}
		d.Lines = append(d.Lines, line)
	}
	if len(d.Lines) == 0 {
		return BillingDraft{}, fail("at least one billable source is required")
	}
	total, err := values.NewMoney("0", req.Policy.Currency, req.Policy.AmountScale, req.Policy.Rounding)
	if err != nil {
		return BillingDraft{}, err
	}
	for _, l := range d.Lines {
		total, err = total.Add(l.Amount)
		if err != nil {
			return BillingDraft{}, fail("sum lines: %v", err)
		}
	}
	d.Total = total
	d.Digest = digestDraft(d)
	return d, nil
}

func price(p PricingPolicy, s Source, description string) (Line, error) {
	zero, err := values.NewDecimal("0", p.AmountScale, p.Rounding)
	if err != nil {
		return Line{}, fail("initialize line decimal: %v", err)
	}
	zeroMoney, err := values.NewMoneyFromDecimal(zero, p.Currency)
	if err != nil {
		return Line{}, fail("initialize line money: %v", err)
	}
	line := Line{ID: "line:" + s.ID, SourceID: s.ID, SourceRevision: s.Revision, SourceKind: string(s.Kind), Description: description, PolicyID: p.ID, PolicyVersion: p.Version, Quantity: zero, UnitPrice: zeroMoney, PricingFactor: zero, SourceAmount: zeroMoney, Amount: zeroMoney}
	var amount values.Money
	switch p.Mode {
	case PricingUnitPrice:
		if !s.Accepted || s.Kind != SourceAcceptedQuantity {
			return Line{}, fail("unit price requires accepted quantity source %s", s.ID)
		}
		if err := s.Quantity.Validate(); err != nil || s.Quantity.Sign() <= 0 {
			return Line{}, fail("accepted quantity source %s must have a positive exact quantity", s.ID)
		}
		rate, ok := p.UnitRates[s.Unit]
		if !ok {
			return Line{}, fail("no approved rate for unit %s", s.Unit)
		}
		if err := rate.Validate(); err != nil {
			return Line{}, fail("unit rate: %v", err)
		}
		amountD, err := s.Quantity.Mul(rate, p.AmountScale, p.Rounding)
		if err != nil {
			return Line{}, fail("unit extension: %v", err)
		}
		amount, err = values.NewMoneyFromDecimal(amountD, p.Currency)
		if err != nil {
			return Line{}, err
		}
		if s.Quantity.Validate() == nil {
			line.Quantity = s.Quantity
		}
		line.Unit = s.Unit
		line.UnitPrice, err = values.NewMoneyFromDecimal(rate, p.Currency)
		if err != nil {
			return Line{}, err
		}
	case PricingTimeMaterial:
		if !s.Approved || (s.Kind != SourceApprovedTime && s.Kind != SourceApprovedMaterial && s.Kind != SourceApprovedSubcontract) {
			return Line{}, fail("time/material requires approved cost source %s", s.ID)
		}
		if err := s.Amount.Validate(); err != nil || s.Amount.Currency() != p.Currency {
			return Line{}, fail("source %s has invalid or mismatched currency", s.ID)
		}
		if s.Amount.Amount().Sign() < 0 {
			return Line{}, fail("source %s cost cannot be negative", s.ID)
		}
		mult, ok := p.TMMultipliers[s.Category]
		if !ok {
			return Line{}, fail("no approved time/material multiplier for %s", s.Category)
		}
		if err := mult.Validate(); err != nil {
			return Line{}, fail("multiplier: %v", err)
		}
		d, err := s.Amount.Amount().Mul(mult, p.AmountScale, p.Rounding)
		if err != nil {
			return Line{}, fail("time/material extension: %v", err)
		}
		amount, err = values.NewMoneyFromDecimal(d, p.Currency)
		if err != nil {
			return Line{}, err
		}
		if s.Quantity.Validate() == nil {
			line.Quantity = s.Quantity
		}
		line.Unit = s.Unit
		line.PricingFactor = mult
		line.SourceAmount = s.Amount
		line.Category = s.Category
	case PricingFixedMilestone:
		if !s.Accepted || s.Kind != SourceAcceptedMilestone {
			return Line{}, fail("fixed billing requires accepted milestone %s", s.ID)
		}
		milestoneAmount, ok := p.MilestoneAmounts[s.MilestoneID]
		if !ok {
			return Line{}, fail("milestone %s has no approved amount", s.MilestoneID)
		}
		if err := milestoneAmount.Validate(); err != nil || milestoneAmount.Currency() != p.Currency {
			return Line{}, fail("milestone amount has invalid or mismatched currency")
		}
		amount = milestoneAmount
		line.UnitPrice = milestoneAmount
	}
	line.Amount = amount
	return line, nil
}

func digestDraft(d BillingDraft) string {
	// Length-prefix fields so arbitrary labels cannot produce delimiter collisions.
	fields := []string{d.ID, d.WorkOrderID, d.PolicyID, d.PolicyVersion, d.Currency, string(d.Mode), d.Total.String(), d.ReconcilesTo, d.CorrectionOf}
	for _, l := range d.Lines {
		fields = append(fields, l.ID, l.SourceID, l.SourceRevision, l.SourceKind, l.Description,
			l.Quantity.String(), l.Unit, l.UnitPrice.String(), l.PricingFactor.String(),
			l.SourceAmount.String(), l.Category, l.Amount.String(), l.PolicyID, l.PolicyVersion)
	}
	b := ""
	for _, field := range fields {
		b += fmt.Sprintf("%d:%s", len(field), field)
	}
	h := sha256.Sum256([]byte(b))
	return hex.EncodeToString(h[:])
}

// Validate verifies the frozen calculation and its digest before any command
// consumes a draft. Exported fields allow serialization, so callers must not
// treat a previously valid digest as proof after mutating the value.
func (d BillingDraft) Validate() error {
	if d.ID == "" || d.WorkOrderID == "" || d.PolicyID == "" || d.PolicyVersion == "" {
		return fail("draft identity and policy binding are required")
	}
	if d.Mode != PricingUnitPrice && d.Mode != PricingTimeMaterial && d.Mode != PricingFixedMilestone {
		return fail("unknown pricing mode")
	}
	if d.Status != Draft && d.Status != Approved && d.Status != Rejected && d.Status != Issued && d.Status != Exported && d.Status != Reconciled && d.Status != Corrected {
		return fail("unknown draft status")
	}
	if err := d.Total.Validate(); err != nil || d.Total.Currency() != d.Currency {
		return fail("invalid draft total or currency")
	}
	if len(d.Lines) == 0 {
		return fail("draft has no lines")
	}
	seen := map[string]bool{}
	zeroTotal, err := values.NewDecimal("0", d.Total.Amount().Scale(), d.Total.Amount().Rounding())
	if err != nil {
		return fail("invalid zero total: %v", err)
	}
	total, err := values.NewMoneyFromDecimal(zeroTotal, d.Currency)
	if err != nil {
		return fail("invalid zero total: %v", err)
	}
	for _, l := range d.Lines {
		if l.ID == "" || l.SourceID == "" || l.SourceRevision == "" || l.SourceKind == "" || l.PolicyID != d.PolicyID || l.PolicyVersion != d.PolicyVersion {
			return fail("invalid line identity or policy binding")
		}
		if seen[l.SourceID] {
			return fail("duplicate source %q", l.SourceID)
		}
		seen[l.SourceID] = true
		if err := l.Amount.Validate(); err != nil || l.Amount.Currency() != d.Currency {
			return fail("invalid line amount or currency")
		}
		switch d.Mode {
		case PricingUnitPrice:
			if l.SourceKind != string(SourceAcceptedQuantity) || l.Quantity.Validate() != nil || l.Quantity.Sign() <= 0 || l.Unit == "" || l.UnitPrice.Validate() != nil || l.UnitPrice.Currency() != d.Currency {
				return fail("invalid unit-price line")
			}
			extended, e := l.Quantity.Mul(l.UnitPrice.Amount(), l.Amount.Amount().Scale(), l.Amount.Amount().Rounding())
			if e != nil || !extended.Equal(l.Amount.Amount()) {
				return fail("unit-price line total does not match its quantity and price")
			}
		case PricingTimeMaterial:
			if (l.SourceKind != string(SourceApprovedTime) && l.SourceKind != string(SourceApprovedMaterial) && l.SourceKind != string(SourceApprovedSubcontract)) || l.SourceAmount.Validate() != nil || l.SourceAmount.Currency() != d.Currency || l.PricingFactor.Validate() != nil || l.Category == "" {
				return fail("invalid time/material line")
			}
			extended, e := l.SourceAmount.Amount().Mul(l.PricingFactor, l.Amount.Amount().Scale(), l.Amount.Amount().Rounding())
			if e != nil || !extended.Equal(l.Amount.Amount()) {
				return fail("time/material line total does not match its source and factor")
			}
		case PricingFixedMilestone:
			if l.SourceKind != string(SourceAcceptedMilestone) || l.UnitPrice.Validate() != nil || !l.UnitPrice.Amount().Equal(l.Amount.Amount()) || l.UnitPrice.Currency() != d.Currency {
				return fail("invalid fixed-milestone line")
			}
		}
		total, err = total.Add(l.Amount)
		if err != nil {
			return fail("sum lines: %v", err)
		}
	}
	if !total.Amount().Equal(d.Total.Amount()) {
		return fail("draft total does not equal line sum")
	}
	if d.Digest == "" || d.Digest != digestDraft(d) {
		return fail("billing content digest mismatch")
	}
	return nil
}

// Transition appends a single idempotent decision event and returns a copy.
// Replaying the same key and decision is a no-op; reusing a key differently fails.
func (d BillingDraft) Transition(action, actor, at, key, detail string) (BillingDraft, error) {
	if err := d.Validate(); err != nil {
		return d, err
	}
	if actor == "" || at == "" || key == "" {
		return d, fail("actor, timestamp, and idempotency key are required")
	}
	for _, e := range d.Events {
		if e.IdempotencyKey == key {
			if e.Type == action && e.Actor == actor && e.Detail == detail {
				return d, nil
			}
			return d, fail("idempotency key reused")
		}
	}
	next := d.Status
	switch action {
	case "approve":
		if d.Status != Draft {
			return d, fail("only draft can be approved")
		}
		next = Approved
	case "reject":
		if d.Status != Draft {
			return d, fail("only draft can be rejected")
		}
		if strings.TrimSpace(detail) == "" {
			return d, fail("rejection reason required")
		}
		next = Rejected
	case "issue":
		if d.Status != Approved {
			return d, fail("only approved draft can be issued")
		}
		next = Issued
	case "export":
		if d.Status != Approved && d.Status != Issued {
			return d, fail("only approved or issued draft can be exported")
		}
		next = Exported
	case "reconcile":
		if d.Status != Exported && d.Status != Issued {
			return d, fail("only issued/exported draft can be reconciled")
		}
		if detail == "" {
			return d, fail("external reference required")
		}
		next = Reconciled
	default:
		return d, fail("unknown action %q", action)
	}
	d.Status = next
	if action == "reconcile" {
		d.ReconcilesTo = detail
	}
	d.Events = append(append([]Event(nil), d.Events...), Event{Type: action, Actor: actor, At: at, IdempotencyKey: key, Detail: detail})
	d.Digest = digestDraft(d)
	return d, nil
}

// Correct creates a new draft linked to the original; original lines/events
// remain unchanged. Corrections are explicit full replacement drafts.
func Correct(original BillingDraft, replacement BillingDraft, actor, at, key, reason string) (BillingDraft, error) {
	if err := original.Validate(); err != nil {
		return BillingDraft{}, err
	}
	if err := replacement.Validate(); err != nil {
		return BillingDraft{}, err
	}
	if original.Status != Issued && original.Status != Exported && original.Status != Reconciled {
		return BillingDraft{}, fail("only issued billing can be corrected")
	}
	if replacement.WorkOrderID != original.WorkOrderID || replacement.PolicyID != original.PolicyID || replacement.PolicyVersion != original.PolicyVersion {
		return BillingDraft{}, fail("correction must retain work order and pricing version")
	}
	if replacement.Status != Draft {
		return BillingDraft{}, fail("replacement must be an unapproved draft")
	}
	if actor == "" || at == "" || key == "" || strings.TrimSpace(reason) == "" {
		return BillingDraft{}, fail("correction actor, timestamp, key, and reason are required")
	}
	replacement.CorrectionOf = original.ID
	replacement.Events = append(append([]Event(nil), replacement.Events...), Event{Type: "correction", Actor: actor, At: at, IdempotencyKey: key, Detail: reason})
	replacement.Digest = digestDraft(replacement)
	return replacement, nil
}
