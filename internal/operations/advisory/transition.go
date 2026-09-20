package advisory

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/incidentstate"
)

// AdvisoryPolicyVersion identifies the customer-advisory policy the
// transition bridge publishes under. It is a fixed marker, not a secret:
// the advisory digest already binds tenant, incident, version and facts.
const AdvisoryPolicyVersion = "ops-advisory/v1"

// PublishForTransition publishes a customer advisory when after enters a
// newly reached customer-facing state (REV-017-03). before is the incident
// as it stood prior to the transition and after is the transitioned
// incident; audience and delivery are the caller's authorization and
// delivery evidence for this publication, and at is the publication time.
//
// Transitions that leave customer-facing status unchanged (Detected to
// Triaged, Monitoring to Resolved) and states with no customer mapping
// (FalsePositive, Merged, Split, Duplicate) publish nothing and return
// published=false with no error: silence is the correct customer outcome,
// not a failure. Audience, delivery and incident validation failures are
// returned as errors so a caller bug fails closed instead of notifying the
// wrong tenant.
//
// The advisory carries only the incident's scoped affected set: build keeps
// facts whose tenant matches the incident and whose kind is safe, and drops
// everything else before the digest is computed.
func PublishForTransition(p *Publisher, before, after incidentstate.Incident, audience Audience, delivery DeliveryEvidence, at time.Time) (Advisory, bool, error) {
	afterStatus, ok := statusFor(after.State)
	if !ok {
		return Advisory{}, false, nil
	}
	if beforeStatus, ok := statusFor(before.State); ok && beforeStatus == afterStatus {
		return Advisory{}, false, nil
	}
	return publishTransition(p, after, audience, delivery, at)
}

func publishTransition(p *Publisher, after incidentstate.Incident, audience Audience, delivery DeliveryEvidence, at time.Time) (Advisory, bool, error) {
	advisory, err := p.Publish(Request{
		ID:            fmt.Sprintf("adv-%s-v%d", after.ID, after.Version),
		PolicyVersion: AdvisoryPolicyVersion,
		Incident:      after,
		Audience:      audience,
		Delivery:      delivery,
		PublishedAt:   at,
	})
	if err != nil {
		return Advisory{}, false, err
	}
	return advisory, true, nil
}
