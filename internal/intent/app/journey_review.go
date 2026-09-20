package app

// REV-091-02: the live proposal review page shows PROMOUX-005's reporting-line
// impact and PROMOUX-006's compensation guardrail. Both were tested domain
// results that no production read ever produced: EvaluateCompensationGuardrail
// had no caller, and ManagementImpact existed only inside the preflight. This
// file computes both for Inspect from the journey's own pinned request and the
// cell's own ports (the pay-band catalog and the org reader), under the same
// authorization rules the rest of the journey read applies.

import (
	"context"
	"strings"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// journeyReviewPorts are the cell's own ports the review reads: the pay-band
// catalog the compensation guardrail resolves against and the org reader the
// reporting-line impact walks. Either nil yields the review's typed
// unavailable or unevaluated state rather than a guess.
type journeyReviewPorts struct {
	bands        rewards.PayBandCatalog
	managerFacts org.WorkerFacts
}

// journeyReviewWithheldReason is the denial token a manager hop carries when
// this viewer may not learn about the worker at that hop.
const journeyReviewWithheldReason = "journey_review.subject_not_disclosed"

// journeyPromotionReview builds the review for one stored promotion request.
// It never fails the detail read: a request it cannot decode yields no
// review, and a guardrail it cannot evaluate yields the typed unavailable
// state rather than an error.
func (e *journeyEngine) journeyPromotionReview(
	ctx context.Context, principal *trust.Principal, purpose string, msg *intentsv1.IntentInstance,
	subject values.EntityRef, evaluatedAt values.Instant, relationships []authz.RelationshipFact,
) *workspace.JourneyPromotionReview {
	payload, err := decodeStruct(msg.GetRequest().GetProtobufWireBytes())
	if err != nil {
		return nil
	}
	tenant := values.TenantId(msg.GetTenantId())
	review := &workspace.JourneyPromotionReview{
		Guardrail: journeyCompensationGuardrail(ctx, e.review.bands, principal, purpose, tenant, subject, evaluatedAt, relationships, payload),
	}
	review.Impact, review.TargetManagerName = e.journeyManagementImpact(ctx, principal, purpose, tenant, subject, evaluatedAt, payload)
	review.ManagerUnchanged = requestedManagerRef(payload) == ""
	e.journeyPositionLabels(ctx, tenant, payload, review)
	return review
}

// requestedManagerRef is the new manager the request names, or "" when it
// names none and the worker keeps their current reporting line.
func requestedManagerRef(payload *structValue) string {
	target, err := fieldsOf(payload, "target")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(optionalStr(target, "manager_ref"))
}

// journeyPositionLabels resolves the current and target positions to their
// directory titles and the target organization to its name, so the review
// and the comparison never print a position identifier. A position the
// directory does not list, or a cell with no directory, leaves the label
// empty; these are presentation labels only and decide nothing.
func (e *journeyEngine) journeyPositionLabels(ctx context.Context, tenant values.TenantId, payload *structValue, review *workspace.JourneyPromotionReview) {
	currentRef, targetRef := "", ""
	if placement, err := fieldsOf(payload, "current_placement"); err == nil {
		currentRef = displayPositionID(optionalStr(placement, "position_id"))
	}
	targetOrg := ""
	if target, err := fieldsOf(payload, "target"); err == nil {
		targetRef = displayPositionID(optionalStr(target, "position_id"))
		targetOrg = strings.TrimSpace(optionalStr(target, "org_unit"))
	}
	if e.positions == nil || (currentRef == "" && targetRef == "" && targetOrg == "") {
		return
	}
	now := e.now()
	effective, err := values.NewLocalDate(now.Year(), now.Month(), now.Day())
	if err != nil {
		return
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(now.UTC()))
	if err != nil {
		return
	}
	rows, err := e.positions.Directory(ctx, tenant, position.AsOf{EffectiveOn: effective, KnownAt: knownAt})
	if err != nil {
		return
	}
	for _, row := range rows {
		switch {
		case targetRef != "" && row.Position.Id == targetRef:
			review.TargetPositionTitle = strings.TrimSpace(row.Title)
			if name := strings.TrimSpace(row.Organization); name != "" && strings.EqualFold(row.OrgUnit, targetOrg) {
				review.TargetOrganizationName = name
			}
		case currentRef != "" && row.Position.Id == currentRef:
			review.CurrentPositionTitle = strings.TrimSpace(row.Title)
		}
		if review.TargetOrganizationName == "" && targetOrg != "" && strings.EqualFold(row.OrgUnit, targetOrg) {
			review.TargetOrganizationName = strings.TrimSpace(row.Organization)
		}
	}
}

// journeyCompensationGuardrail evaluates PROMOUX-006's guardrail from the
// request's pinned current compensation and target role.
//
// Authorization comes first and is explicit: the viewer must be allowed the
// subject's base salary under the same purpose and relationships the journey
// read used. A viewer who is not gets the typed NOT_AUTHORIZED state with no
// amount at all, whatever the pinned request holds.
func journeyCompensationGuardrail(
	ctx context.Context, bands rewards.PayBandCatalog, principal *trust.Principal, purpose string,
	tenant values.TenantId, subject values.EntityRef, evaluatedAt values.Instant,
	relationships []authz.RelationshipFact, payload *structValue,
) promotion.CompensationGuardrail {
	unavailable := func(reason promotion.GuardrailUnavailableReason) promotion.CompensationGuardrail {
		return promotion.CompensationGuardrail{Status: promotion.GuardrailStatusUnavailable, Reason: reason}
	}
	if _, err := authorizeRead(principal, purpose, authorizationRequest{
		Subject: subject, EvaluatedAt: evaluatedAt,
		Gate:          []authz.FieldID{authz.FieldBaseSalary},
		Relationships: relationships,
	}); err != nil {
		return unavailable(promotion.GuardrailReasonNotAuthorized)
	}
	current, err := compensationSnapshot(payload, "current")
	if err != nil {
		return unavailable("")
	}
	base, ok := current.Base.Get()
	if !ok {
		return unavailable(promotion.GuardrailReasonNotAuthorized)
	}
	target, err := targetPlacement(payload)
	if err != nil {
		return unavailable("")
	}
	effective, err := localDate(payload, "effective_date")
	if err != nil {
		return unavailable("")
	}
	payZone := target.PayZone
	if payZone == "" {
		if placement, placementErr := fieldsOf(payload, "current_placement"); placementErr == nil {
			payZone = optionalStr(placement, "pay_zone")
		}
	}
	if bands == nil || target.JobCode == "" || target.Grade == "" || strings.TrimSpace(payZone) == "" {
		return unavailable(promotion.GuardrailReasonBandUnresolved)
	}
	guardrail, err := promotion.EvaluateCompensationGuardrail(ctx, promotion.CompensationGuardrailRequest{
		Current: current,
		Target: rewards.BandQuery{
			Tenant: tenant, JobCode: target.JobCode, Grade: target.Grade,
			PayZone: payZone, Currency: base.Currency(), AsOf: effective,
		},
		Catalog:       bands,
		Annualization: rewards.DefaultAnnualization(),
	})
	if err != nil {
		return unavailable("")
	}
	return guardrail
}

// journeyManagementImpact evaluates PROMOUX-005's reporting-line impact. The
// target manager is the one the request named, or else the worker's current
// recorded manager (a promotion in place keeps its reporting line). The
// evaluation itself is the promotion domain's, over the cell's org reader,
// with every hop disclosed only as this viewer's authorization allows; an
// undisclosed or unresolvable manager yields the zero impact and no name.
func (e *journeyEngine) journeyManagementImpact(
	ctx context.Context, principal *trust.Principal, purpose string, tenant values.TenantId,
	subject values.EntityRef, evaluatedAt values.Instant, payload *structValue,
) (promotion.ManagementImpact, string) {
	if e.review.managerFacts == nil || e.locate == nil {
		return promotion.ManagementImpact{}, ""
	}
	managerRef := requestedManagerRef(payload)
	if located, found, err := e.locate(ctx, tenant, optionalStr(payload, "worker_ref")); err == nil && found {
		subject = located.Ref
		if managerRef == "" && located.Created != nil {
			managerRef = strings.TrimSpace(located.Created.ManagerRelationshipRef)
		}
	}
	if managerRef == "" {
		return promotion.ManagementImpact{}, ""
	}
	manager, found, err := e.locate(ctx, tenant, managerRef)
	if err != nil || !found || manager.Created == nil {
		return promotion.ManagementImpact{}, ""
	}
	// The org walk consults the viewer only for hops that exist, so a
	// manager at the top of the chain would pass it unexamined. Naming a
	// manager is disclosure of two facts, and both are checked here: who the
	// worker reports to, and the manager's own name.
	if !journeyReviewDiscloses(principal, purpose, evaluatedAt, subject, people.FieldManagerRelation) ||
		!journeyReviewDiscloses(principal, purpose, evaluatedAt, manager.Ref, people.FieldPreferredName) {
		return promotion.ManagementImpact{}, ""
	}
	now := values.NewInstant(e.now().UTC())
	knownAt, err := values.NewKnownAt(now)
	if err != nil {
		return promotion.ManagementImpact{}, ""
	}
	impact, _, err := promotion.EvaluateManagementImpact(ctx, tenant, subject, e.review.managerFacts, promotion.TargetManagerSelection{
		Reference:  manager.Ref,
		AsOf:       now,
		KnownAt:    knownAt,
		ChainDepth: maxManagerChainDepth,
		Authorize:  journeyManagerHopAuthorizer(principal, purpose, evaluatedAt),
	})
	if err != nil || !impact.Evaluated() {
		return promotion.ManagementImpact{}, ""
	}
	return impact, strings.TrimSpace(manager.Created.DisplayName())
}

// journeyManagerHopAuthorizer discloses a reporting hop only when this viewer
// may read that hop's worker's manager relationship under the journey's
// purpose. A refusal is a withheld hop, never an error, so the cycle walk
// reports "not certified" rather than guessing.
func journeyManagerHopAuthorizer(principal *trust.Principal, purpose string, evaluatedAt values.Instant) org.Authorizer {
	fields := []people.FieldID{people.FieldManagerRelation}
	return func(f org.ManagerRelationshipFact) people.AuthorizationDecision {
		result, err := authorizeRead(principal, purpose, authorizationRequest{
			Subject: f.Worker, EvaluatedAt: evaluatedAt, Read: peopleFields(fields),
		})
		if err != nil {
			return people.AuthorizationDecision{
				PolicyVersion: AuthorizationPolicyVersion, Purpose: nonEmptyPurpose(purpose),
				SubjectDisclosable: false, SubjectDenialReason: journeyReviewWithheldReason,
				Fields: map[people.FieldID]people.FieldRuling{
					people.FieldManagerRelation: {Effect: people.EffectDeny, Reason: journeyReviewWithheldReason},
				},
			}
		}
		return peopleDecision(result, fields)
	}
}

// journeyReviewDiscloses reports whether the viewer may read field about
// subject under the journey's purpose.
func journeyReviewDiscloses(principal *trust.Principal, purpose string, evaluatedAt values.Instant, subject values.EntityRef, field people.FieldID) bool {
	_, err := authorizeRead(principal, purpose, authorizationRequest{
		Subject: subject, EvaluatedAt: evaluatedAt, Read: peopleFields([]people.FieldID{field}),
	})
	return err == nil
}

func nonEmptyPurpose(purpose string) string {
	if strings.TrimSpace(purpose) == "" {
		return "journey.review"
	}
	return purpose
}
