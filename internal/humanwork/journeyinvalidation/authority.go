package journeyinvalidation

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// JourneyKind is the entity kind a promotion journey's invalidation subject
// carries. Its id is the journey's intent id.
const JourneyKind values.Kind = "promotion_journey"

// visibilitySource attributes the relationship fact a delivery is authorized
// under to the read that established it.
const visibilitySource = "journey_engine.inspect"

// JourneyRef is the invalidation subject for one journey.
func JourneyRef(tenant values.TenantId, intentID string) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: JourneyKind, Id: intentID}
}

// deliver turns one committed record into this subscription's next message,
// or reports that the viewer may not see it (ok false, nil error).
//
// Authority is decided twice and both must agree:
//
//  1. The journey must be visible to this viewer through the engine's own
//     read (workspace.JourneyEngine.Inspect under the viewer's context) --
//     the same rule that decides whether the list and detail pages show it.
//     A refusal, an unknown journey or any other failure drops the record.
//  2. The hint must survive productquery.EmitInvalidation's authorization
//     check, reached through promotion.SubscriberSequencer, which is also
//     what numbers the message.
//
// P1A publishes no organization graph and no durable "can see this journey"
// relationship, so the relationship fact and resource organization handed to
// step 2 are attributed to the read in step 1: they name exactly the subject
// that read just admitted, for exactly this instant, and nothing wider. A
// principal whose roles admit no relationship-scoped access at all (a
// worker_self viewer, for example) is still refused by step 2.
func (s *Subscription) deliver(ctx context.Context, record Committed) ([]byte, bool, error) {
	if s.authorize != nil {
		if err := s.authorize(ctx); err != nil {
			if ctx.Err() != nil {
				return nil, false, ctx.Err()
			}
			return nil, false, fmt.Errorf("%w: %v", ErrRevoked, err)
		}
	}
	now := s.now().UTC()
	if expires := s.principal.ExpiresAt(); !expires.IsZero() && !now.Before(expires) {
		return nil, false, fmt.Errorf("%w: credential expired", ErrRevoked)
	}
	tenant := s.principal.Tenant()
	if record.Tenant != tenant {
		// Unreachable through the hub, which fans out by tenant; refused
		// anyway so no caller of deliver can cross a tenant.
		return nil, false, nil
	}
	detail, err := s.inspector.Inspect(ctx, record.IntentID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		return nil, false, nil
	}
	occurred := detail.Summary.UpdatedAt
	if occurred.IsZero() {
		occurred = detail.Summary.CreatedAt
	}
	transition := promotion.Transition{
		Tenant:     tenant,
		JourneyRef: JourneyRef(tenant, record.IntentID),
		WorkerRef:  detail.Summary.Worker,
		OccurredAt: occurred,
		Revision:   record.Revision,
	}
	if transition.Validate() != nil {
		return nil, false, nil
	}
	subject, err := transition.RegionSubject(s.region)
	if err != nil {
		return nil, false, nil
	}
	instant := values.NewInstant(now)
	target, err := s.target(subject, record.Revision, instant)
	if err != nil {
		return nil, false, nil
	}
	message, ok, err := EmitForSubscriber(s.sequencer, s.key, s.region, productquery.InvalidationRequest{
		Principal:   s.principal,
		EffectiveAt: instant,
		// SourceSequence and Watermark satisfy EmitInvalidation's own
		// ordering check only; SubscriberSequencer replaces both with this
		// subscriber's contiguous counter.
		SourceSequence: record.Revision,
		Watermark:      record.Revision - 1,
		Targets:        []productquery.InvalidationTarget{target},
	})
	if err != nil || !ok {
		return nil, false, nil
	}
	message.SourceSequence += s.base
	message.Watermark += s.base
	if err := message.Validate(); err != nil {
		return nil, false, nil
	}
	raw, err := message.CanonicalBytes()
	if err != nil {
		return nil, false, nil
	}
	return raw, true, nil
}

// target builds the one invalidation target for subject, with the decision
// productquery.EmitInvalidation will re-evaluate and compare. The inputs are
// exactly the ones EmitInvalidation derives itself (principal organization
// from the principal, no purpose override, no fields), so a decision that
// differs in any respect is refused there rather than trusted here.
func (s *Subscription) target(subject values.EntityRef, revision uint64, now values.Instant) (productquery.InvalidationTarget, error) {
	fact, err := visibilityFact(subject, now)
	if err != nil {
		return productquery.InvalidationTarget{}, err
	}
	principalOrg := authz.OrgUnitRef{}
	resourceOrg := authz.OrgUnitRef{}
	if scope := s.principal.OrganizationScopeID(); scope != "" {
		principalOrg = authz.OrgUnitRef{Tenant: s.principal.Tenant(), ID: scope}
		resourceOrg = principalOrg
	}
	relationships := []authz.RelationshipFact{fact}
	decision, err := authz.Enforce(authz.Request{
		Principal:     s.principal,
		EffectiveAt:   now,
		Subject:       subject,
		PrincipalOrg:  principalOrg,
		ResourceOrg:   resourceOrg,
		Relationships: relationships,
	})
	if err != nil {
		return productquery.InvalidationTarget{}, err
	}
	if !decision.SubjectDisclosable {
		return productquery.InvalidationTarget{}, errors.New("journeyinvalidation: subject not disclosable")
	}
	return productquery.InvalidationTarget{
		Subject: subject, Revision: revision, Decision: decision,
		ResourceOrg: resourceOrg, Relationships: relationships,
	}, nil
}

// visibilityFact is the assigned-population fact the engine read just
// established for subject, effective from this instant only.
func visibilityFact(subject values.EntityRef, now values.Instant) (authz.RelationshipFact, error) {
	effective, err := values.NewOpenInstantInterval(now)
	if err != nil {
		return authz.RelationshipFact{}, err
	}
	known, err := values.NewKnownAt(now)
	if err != nil {
		return authz.RelationshipFact{}, err
	}
	recorded, err := values.NewRecordedAt(now)
	if err != nil {
		return authz.RelationshipFact{}, err
	}
	fact := authz.RelationshipFact{
		Kind: authz.RelationshipAssignedPopulation, Subject: subject, Source: visibilitySource,
		Effective: effective, RecordedAt: recorded, KnownAt: known,
	}
	return fact, fact.Validate()
}
