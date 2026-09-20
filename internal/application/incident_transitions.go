package application

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/advisory"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/incidentstate"
)

// ApplyIncidentTransition applies one incident lifecycle transition and, when
// the transitioned incident newly enters a customer-facing state, publishes
// the customer advisory for it (REV-017-03). It is the application layer's
// incident-transition entry point: the served binaries reach this package,
// while the transition rules stay owned by internal/operations/incidentstate
// and publication stays owned by internal/operations/advisory.
//
// before is the incident as currently recorded, cmd is the lifecycle command
// to apply, and audience/delivery are the caller's authorization and
// delivery evidence for any resulting publication. It returns the
// transitioned incident, the published advisory and published=true when a
// new customer-facing status was entered; transitions that stay silent
// return published=false with no error. It performs no storage or network
// I/O: the caller persists the returned incident through its own
// tenant-scoped transaction.
func ApplyIncidentTransition(pub *advisory.Publisher, before incidentstate.Incident, cmd incidentstate.Command, now time.Time, audience advisory.Audience, delivery advisory.DeliveryEvidence) (after incidentstate.Incident, published advisory.Advisory, didPublish bool, err error) {
	after, err = incidentstate.Transition(before, cmd, now)
	if err != nil {
		return incidentstate.Incident{}, advisory.Advisory{}, false, err
	}
	published, didPublish, err = advisory.PublishForTransition(pub, before, after, audience, delivery, now)
	if err != nil {
		return incidentstate.Incident{}, advisory.Advisory{}, false, err
	}
	return after, published, didPublish, nil
}
