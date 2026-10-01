// Package contractortime turns approved contractor time or milestone
// completion into an invoice or self-billed invoice draft, without any of the
// employee-style controls (schedule lockout, geofence, photo, break
// attestation) that would create evidence of control over the contractor.
//
// The package is pure: no database, no clock, no network, no package-level
// mutable state. Every timestamp and threshold arrives as a parameter.
package contractortime

// schemaVersion is this package's contract version (ARCH-GO-009).
const schemaVersion = 1

// Version reports this package's contract version.
func Version() int { return schemaVersion }

// Explanation is the ARCH-GO-009 explain view of an invoice draft: enough to
// audit what was billed and why without re-deriving the arithmetic.
type Explanation struct {
	EngagementID string
	WorkerRef    string
	Pricing      PricingModel
	SelfBilled   bool
	LineCount    int
	Total        string // Money.String(); empty when the draft has no valid total
	Digest       string
}

// Explain summarizes an invoice draft for audit and inspection.
func (d InvoiceDraft) Explain() (Explanation, error) {
	if err := d.Validate(); err != nil {
		return Explanation{}, err
	}
	return Explanation{
		EngagementID: d.EngagementID,
		WorkerRef:    d.WorkerRef,
		Pricing:      d.Pricing,
		SelfBilled:   d.SelfBilled,
		LineCount:    len(d.Lines),
		Total:        d.Total.String(),
		Digest:       d.Digest,
	}, nil
}
