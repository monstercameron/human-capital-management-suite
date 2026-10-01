// Package agencytime approves agency-temp time as the host and packages it
// for delivery to the agency or a vendor-management system (VMS). Agency
// assignments name the agency, the host entity, the work order and the
// worksite; approved time reaches the agency through a generic canonical
// export payload with format adapters, never the host's own payroll.
//
// The package is pure: no database, no clock, no network, no package-level
// mutable state.
package agencytime

const schemaVersion = 1

// Version reports this package's contract version.
func Version() int { return schemaVersion }

// Explanation is the ARCH-GO-009 explain view of an export payload.
type Explanation struct {
	AssignmentID string
	Agency       string
	HostEntity   string
	WorkOrder    string
	Worksite     string
	LineCount    int
	Digest       string
}

// Explain summarizes an export payload for audit and inspection.
func (p ExportPayload) Explain() (Explanation, error) {
	if err := p.Validate(); err != nil {
		return Explanation{}, err
	}
	return Explanation{
		AssignmentID: p.Assignment.ID,
		Agency:       p.Assignment.AgencyRef,
		HostEntity:   p.Assignment.HostEntityRef,
		WorkOrder:    p.Assignment.WorkOrderRef,
		Worksite:     p.Assignment.WorksiteRef,
		LineCount:    len(p.Lines),
		Digest:       p.Digest,
	}, nil
}
