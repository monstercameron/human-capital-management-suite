package recruiting

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

var ErrRehireScreeningInvalid = errors.New("recruiting: invalid rehire screening request")

// RehireViewerRole is the declared role scope for a rehire screening view.
type RehireViewerRole string

const (
	RehireViewerHR        RehireViewerRole = "HR"
	RehireViewerRecruiter RehireViewerRole = "RECRUITING"
	RehireViewerCandidate RehireViewerRole = "CANDIDATE"
)

// RehireScreening is a read-only projection. Reason is withheld from
// candidate-facing callers while the status remains available for screening.
type RehireScreening struct {
	Status   people.RehireStatus
	Reason   people.RehireReason
	Revision uint64
}

// ReadRehireScreening projects the latest people-owned eligibility decision
// for recruiting. It performs no writes and only exposes the reason to HR and
// recruiting roles.
func ReadRehireScreening(record people.RehireEligibility, role RehireViewerRole) (RehireScreening, error) {
	if err := record.Validate(); err != nil {
		return RehireScreening{}, fmt.Errorf("%w: %v", ErrRehireScreeningInvalid, err)
	}
	view := RehireScreening{Status: record.Status, Revision: record.Revision}
	switch role {
	case RehireViewerHR, RehireViewerRecruiter:
		view.Reason = record.Reason
	case RehireViewerCandidate:
	default:
		return RehireScreening{}, fmt.Errorf("%w: role %q is not declared", ErrRehireScreeningInvalid, role)
	}
	return view, nil
}
