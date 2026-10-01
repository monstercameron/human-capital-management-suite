package agencytime

import "strings"

// Assignment names the agency temp's engagement as the host client sees it:
// which agency supplied the worker, which host legal entity and worksite the
// worker reports to, and which work order authorizes the placement. Approved
// time under this assignment runs through the normal session and period
// templates with the host manager as approver (WTIME-005/WTIME-007); this
// package only shapes what leaves the host system afterward.
type Assignment struct {
	ID string

	WorkerRef string

	AgencyRef     string // the staffing agency that employs the worker
	HostEntityRef string // the host legal entity the worker is placed with
	WorkOrderRef  string // the work order or purchase order authorizing the placement
	WorksiteRef   string // the physical or logical worksite the worker reports to

	Role string // job/role the worker fills, used for the AWR qualifying-week key
}

// Validate reports whether the assignment names every required party.
func (a Assignment) Validate() error {
	if strings.TrimSpace(a.ID) == "" {
		return reject("Assignment.ID", "", "assignment id is required")
	}
	if strings.TrimSpace(a.WorkerRef) == "" {
		return reject("Assignment.WorkerRef", "", "assignment worker reference is required")
	}
	if strings.TrimSpace(a.AgencyRef) == "" {
		return reject("Assignment.AgencyRef", "", "assignment must name the supplying agency")
	}
	if strings.TrimSpace(a.HostEntityRef) == "" {
		return reject("Assignment.HostEntityRef", "", "assignment must name the host entity")
	}
	if strings.TrimSpace(a.WorkOrderRef) == "" {
		return reject("Assignment.WorkOrderRef", "", "assignment must name the work order")
	}
	if strings.TrimSpace(a.WorksiteRef) == "" {
		return reject("Assignment.WorksiteRef", "", "assignment must name the worksite")
	}
	return nil
}
