package app

import (
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

const (
	journeyFailureDomainUnavailable = "intent.domain_unavailable"
	journeyFailureStorageFailed     = "STORAGE_FAILED"
	journeyFailureStagePrecondition = "journey.stage_precondition"
)

// journeyFailureReason gives approval-start failures a stable operational
// category. The browser maps the same owned reason into reader copy; keeping
// the category on the business event makes the failed attempt diagnosable
// without logging provider messages or request contents.
func journeyFailureReason(err error) string {
	if err == nil {
		return ""
	}
	if owned, ok := envelope.As(err); ok {
		switch owned.ReasonRef() {
		case journeyFailureDomainUnavailable, journeyFailureStorageFailed:
			return owned.ReasonRef()
		}
	}
	switch {
	case errors.Is(err, workspace.ErrJourneyStage):
		return journeyFailureStagePrecondition
	case errors.Is(err, workspace.ErrJourneyUnavailable):
		return journeyFailureDomainUnavailable
	default:
		return ""
	}
}
