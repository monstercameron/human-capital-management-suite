package authz

import (
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// FieldRuling is the decision for one field: an effect, the rule that
// produced it, a reason token safe to surface, and any obligations that ride
// along with a redacted disclosure.
type FieldRuling struct {
	Effect      Effect
	RuleID      string
	Reason      string
	Obligations []string
}

// FieldDecision is the TRUST-010 result: a ruling for every field that was
// requested, plus the purpose and policy version it was evaluated under.
type FieldDecision struct {
	Purpose       string
	PolicyVersion string
	Rulings       map[FieldID]FieldRuling
}

// Covers reports whether the decision rules on every field in fields, naming
// the first field it is silent about. A caller must treat a missing ruling
// as a refusal to answer, never as an implicit allow or deny.
func (d FieldDecision) Covers(fields []FieldID) error {
	for _, f := range fields {
		if _, ok := d.Rulings[f]; !ok {
			return fmt.Errorf("%w: no ruling for field %q", ErrInvalidPolicyInput, f)
		}
	}
	return nil
}

// ResolveFields implements TRUST-010: field-level and purpose authorization.
// A compensation, medical, bank or employee-relations field is denied before
// this ruling ever reaches a repository, serializer, UI or tool, unless a
// role held by the principal carries an explicit grant for that field's data
// domain under the declared purpose. Absence of a grant is a denial, not an
// omission: the policy is deny-by-default.
//
// mandatoryDenies carries non-delegable restriction rule IDs from
// [TenantScopeDecision.MandatoryDenies] (for example, the cross-tenant
// sensitive-domain restriction). A mandatory deny always wins: no role grant
// can be composed to bypass it.
func ResolveFields(principal *trust.Principal, purpose string, fields []FieldID, mandatoryDenies []string) (FieldDecision, error) {
	if principal == nil {
		return FieldDecision{}, fmt.Errorf("%w: nil principal", ErrInvalidPolicyInput)
	}
	return resolveFields(principal.SubjectKind(), principal.Roles(), principal.AuthorizesPurpose, purpose, fields, mandatoryDenies)
}

// ResolveFieldsWithRoles is [ResolveFields] over an explicit server-resolved
// role set instead of the principal's credential roles. A non-nil roles,
// even empty, governs as-is: an empty durable assignment authorizes nothing
// and never falls back to the credential.
func ResolveFieldsWithRoles(principal *trust.Principal, roles []string, purpose string, fields []FieldID, mandatoryDenies []string) (FieldDecision, error) {
	if principal == nil {
		return FieldDecision{}, fmt.Errorf("%w: nil principal", ErrInvalidPolicyInput)
	}
	if roles == nil {
		roles = principal.Roles()
	}
	return resolveFields(principal.SubjectKind(), roles, principal.AuthorizesPurpose, purpose, fields, mandatoryDenies)
}

// resolveFields is [ResolveFields] over an explicit role set instead of the
// principal's own. The directory decision point resolves with an aliased set
// (a legacy administrator token acting under its mapped template), so the
// grant loop lives here and both callers share it.
func resolveFields(kind trust.SubjectKind, roles []string, authorizes func(string) bool, purpose string, fields []FieldID, mandatoryDenies []string) (FieldDecision, error) {
	if authorizes == nil {
		return FieldDecision{}, fmt.Errorf("%w: nil purpose authorizer", ErrInvalidPolicyInput)
	}

	decision := FieldDecision{
		Purpose:       purpose,
		PolicyVersion: PolicyVersion,
		Rulings:       make(map[FieldID]FieldRuling, len(fields)),
	}

	held := RolesForKind(kind, roles)
	crossTenantMandatoryDeny := slices.Contains(mandatoryDenies, mandatoryDenyCrossTenantSensitive)

	for _, f := range fields {
		decision.Rulings[f] = ruleField(authorizes, purpose, f, held, crossTenantMandatoryDeny)
	}
	return decision, nil
}

func ruleField(authorizes func(string) bool, purpose string, f FieldID, held []RoleID, crossTenantMandatoryDeny bool) FieldRuling {
	def, ok := FieldRegistry[f]
	if !ok {
		return FieldRuling{Effect: EffectDenied, RuleID: "p1a.field.unknown", Reason: "unknown_field"}
	}
	if purpose == "" {
		return FieldRuling{Effect: EffectDenied, RuleID: "p1a.field.purpose_required", Reason: "purpose_required"}
	}
	if !authorizes(purpose) {
		return FieldRuling{Effect: EffectDenied, RuleID: "p1a.field.purpose_not_authorized", Reason: "purpose_not_authorized_for_principal"}
	}
	if crossTenantMandatoryDeny && def.Domain != DomainCore && def.Domain != DomainContact {
		return FieldRuling{Effect: EffectDenied, RuleID: mandatoryDenyCrossTenantSensitive, Reason: "cross_tenant_mandatory_deny"}
	}

	best := FieldRuling{Effect: EffectDenied, RuleID: "p1a.field.deny_default", Reason: "no_grant_for_domain"}
	for _, role := range held {
		domains, ok := PolicyTable[role]
		if !ok {
			domains, ok = MachinePolicyTable[role]
		}
		if !ok {
			continue
		}
		grant, ok := domains[def.Domain]
		if !ok || !grant.appliesTo(purpose) {
			continue
		}
		candidate := FieldRuling{
			Effect:      grant.Effect,
			RuleID:      grant.RuleID,
			Reason:      grantReason(grant.Effect),
			Obligations: grant.Obligations,
		}
		if candidate.Effect.rank() > best.Effect.rank() {
			best = candidate
		}
	}
	return best
}

func grantReason(e Effect) string {
	switch e {
	case EffectAllow:
		return "granted"
	case EffectRedacted:
		return "granted_redacted"
	default:
		return "no_grant_for_domain"
	}
}

// withholdFields returns a ruling for every field in fields with
// EffectWithheld and one uniform reason, regardless of what any field's own
// domain grant would otherwise have produced. It is used whenever the
// subject itself is not disclosable: the point of withholding is that a
// caller cannot distinguish "this field would have been denied" from "this
// field would have been allowed", because either answer would leak whether
// the record exists.
func withholdFields(purpose string, fields []FieldID) FieldDecision {
	rulings := make(map[FieldID]FieldRuling, len(fields))
	for _, f := range fields {
		rulings[f] = FieldRuling{
			Effect: EffectWithheld,
			RuleID: "p1a.field.subject_not_disclosable",
			Reason: "subject_not_disclosable",
		}
	}
	return FieldDecision{Purpose: purpose, PolicyVersion: PolicyVersion, Rulings: rulings}
}
