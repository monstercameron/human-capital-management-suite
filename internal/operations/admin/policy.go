package admin

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
)

// OperatorRole is the reserved role a caller's authenticated trust.Principal
// must carry for any AdminService method to run. It is minted only into an
// operator's own credential (a JIT/HMAC token in development, a federation
// claim in production); no ordinary end-user or first-party service
// credential issued for the intents/registry surface ever carries it. This
// is the whole of AdminService's "distinct trust/route policy" - every other
// admission step (authentication, deadline capping, strict validation,
// trusted-field derivation) is the identical shared chain every other
// service on the process uses (internal/transport/admin, the wire adapter
// hosting this package's capabilities, reuses that chain unchanged).
const OperatorRole = "hcmnext.trust.role.operator"

// Operator route-policy errors. All are matchable with errors.Is.
var (
	ErrNoPrincipal          = errors.New("admin: no authenticated principal")
	ErrOperatorRoleRequired = errors.New("admin: this method requires the operator profile")
)

// RequireOperator evaluates AdminService's distinct trust/route policy
// against an already-authenticated principal: it fails closed on a nil
// principal and on one that does not carry [OperatorRole]. It is a pure
// function of its argument - no context, no clock, no I/O - so the transport
// adapter that reads the principal off a request context can call it
// without this package ever knowing what carried the request.
func RequireOperator(principal *trust.Principal) error {
	if principal == nil {
		return ErrNoPrincipal
	}
	if !principal.HasRole(OperatorRole) {
		return ErrOperatorRoleRequired
	}
	return nil
}

// operatorPolicyVersion and operatorPurpose name the fixed authorization
// policy AdminService's read passthroughs evaluate under. They are the
// admin surface's own coarse-grained policy: an authenticated operator
// principal is, by definition of holding OperatorRole, entitled to full
// disclosure of the read-only diagnostic surfaces this package exposes.
// This is deliberately coarser than the fine-grained, subject-specific
// tenant AuthZ evaluation internal/trust/authz performs for an ordinary
// end-user read; ADMIN-006 (support-safe diagnostic sessions, time/purpose-
// bound and allowlisted) is where a narrower, session-scoped operator grant
// belongs. SVC-011/ADMIN-001 requires only that a caller without
// OperatorRole cannot reach these methods at all.
const (
	operatorPolicyVersion = "hcmnext.admin.operator-profile/1"
	operatorPurpose       = "operator_diagnostics"
)

// ParseFields validates tokens as internal/domains/people.FieldID values,
// rejecting an unknown or duplicate token rather than silently narrowing or
// widening the projection. An empty tokens list means every known field.
func ParseFields(tokens []string) ([]people.FieldID, error) {
	if len(tokens) == 0 {
		return people.AllFields(), nil
	}
	out := make([]people.FieldID, 0, len(tokens))
	seen := make(map[people.FieldID]bool, len(tokens))
	for i, t := range tokens {
		f := people.FieldID(t)
		if err := f.Validate(); err != nil {
			return nil, fmt.Errorf("admin: field at index %d is not recognized: %w", i, err)
		}
		if seen[f] {
			return nil, fmt.Errorf("admin: field at index %d is requested twice", i)
		}
		seen[f] = true
		out = append(out, f)
	}
	return out, nil
}

// ParseSections validates tokens as internal/domains/intelligence.Section
// values, with the same fail-closed discipline as [ParseFields].
func ParseSections(tokens []string) ([]intelligence.Section, error) {
	if len(tokens) == 0 {
		return intelligence.AllSections(), nil
	}
	out := make([]intelligence.Section, 0, len(tokens))
	seen := make(map[intelligence.Section]bool, len(tokens))
	for i, t := range tokens {
		s := intelligence.Section(t)
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("admin: section at index %d is not recognized: %w", i, err)
		}
		if seen[s] {
			return nil, fmt.Errorf("admin: section at index %d is requested twice", i)
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

// OperatorWorkerAuthorization builds the full-disclosure
// people.AuthorizationDecision the operator profile evaluates under for a
// GetWorkerState call projecting exactly fields.
func OperatorWorkerAuthorization(fields []people.FieldID) people.AuthorizationDecision {
	rulings := make(map[people.FieldID]people.FieldRuling, len(fields))
	for _, f := range fields {
		rulings[f] = people.FieldRuling{Effect: people.EffectAllow}
	}
	return people.AuthorizationDecision{
		PolicyVersion:      operatorPolicyVersion,
		Purpose:            operatorPurpose,
		SubjectDisclosable: true,
		Fields:             rulings,
	}
}

// OperatorTransactionAuthorization builds the full-disclosure
// intelligence.AuthorizationDecision the operator profile evaluates under
// for an ExplainTransaction call projecting exactly sections.
func OperatorTransactionAuthorization(sections []intelligence.Section) intelligence.AuthorizationDecision {
	rulings := make(map[intelligence.Section]intelligence.Ruling, len(sections))
	for _, s := range sections {
		rulings[s] = intelligence.Ruling{Effect: intelligence.EffectAllow}
	}
	return intelligence.AuthorizationDecision{
		PolicyVersion:          operatorPolicyVersion,
		Purpose:                operatorPurpose,
		TransactionDisclosable: true,
		Sections:               rulings,
	}
}

// OperatorWorkflowInstanceAuthorization builds the full-disclosure
// inspect.Authorization the operator profile evaluates a GetWorkflowInstance
// call's traversal (definition/instance/node/governance/transaction/
// connector/observation/trace) under: every stage and every protected field
// allowed, the same coarse, full-disclosure operator profile
// [OperatorWorkerAuthorization] and [OperatorTransactionAuthorization] grant.
// subject names the caller the rendered view records as having asked.
func OperatorWorkflowInstanceAuthorization(subject string) inspect.Authorization {
	return inspect.AllowAll(operatorPolicyVersion, operatorPurpose, subject)
}

// OperatorWorkItemAuthorization builds the full-disclosure
// inspect.WorkItemAuthorization the operator profile evaluates a
// GetWorkflowInstance call's work-item-and-transitions section under.
func OperatorWorkItemAuthorization() inspect.WorkItemAuthorization {
	return inspect.WorkItemAuthorization{Disclosed: true}
}

// Durable operator bindings (RBAC-RT-009).
//
// [RequireOperator] answers from the credential: a principal whose token
// signs [OperatorRole] passes. That is the legacy admission path the
// served AdminService still calls, and it is exactly the RED this todo
// closes: a role string minted into a bearer credential is not reviewable
// as a grant of platform authority. The durable path is [AuthorizeOperator]:
// operator authority comes from an [OperatorGrant] a different principal
// approved, recorded with its approver, reason and validity window, so the
// grant can be listed, reviewed and revoked. Token roles are never
// consulted by [AuthorizeOperator]; a token-only operator or administrator
// claim is refused.
//
// Separation of duties holds at issuance ([NewOperatorGrant] refuses a
// self-approved grant) and at evaluation ([AuthorizeOperator] refuses to
// honor one, even hand-built). The approver and the operator must differ.

// OperatorGrant is one durable, reviewable grant of operator authority.
// Subject names the operator, Approver the administrator who approved the
// grant, Reason the human justification a reviewer reads, GrantedAt the
// instant the grant takes effect and ExpiresAt the instant it lapses (zero
// means the grant does not expire). Grants are data: persisting and
// serving them is the composition root's job, not this package's.
type OperatorGrant struct {
	Subject   string
	Approver  string
	Reason    string
	GrantedAt time.Time
	ExpiresAt time.Time
}

// Operator binding errors. All are matchable with errors.Is.
var (
	// ErrOperatorGrantInvalid reports a grant that is not reviewable: a
	// missing subject, approver or reason, a zero start, or an inverted
	// window. Such a grant never authorizes.
	ErrOperatorGrantInvalid = errors.New("admin: operator grant is not reviewable")
	// ErrOperatorGrantRequired reports a principal with no live grant: no
	// binding names the subject, or every binding naming it is expired,
	// premature or unreviewable. Token roles never satisfy this error.
	ErrOperatorGrantRequired = errors.New("admin: operator authority requires a durable grant")
	// ErrSeparationOfDuties reports a grant whose approver is the operator
	// itself. It is distinct from [ErrOperatorGrantRequired] so a reviewer
	// can tell "no grant" from "a grant that violates separation of
	// duties".
	ErrSeparationOfDuties = errors.New("admin: the grant approver must differ from the operator")
)

// NewOperatorGrant issues one reviewable grant, refusing a self-approved
// one at the source: the approver and the operator must differ. Comparison
// is case-insensitive after trimming, matching the subject comparison the
// rest of the suite applies, so a case-only dodge is still a dodge.
func NewOperatorGrant(approver, subject, reason string, grantedAt time.Time, expiresAt time.Time) (OperatorGrant, error) {
	grant := OperatorGrant{
		Subject:   strings.TrimSpace(subject),
		Approver:  strings.TrimSpace(approver),
		Reason:    strings.TrimSpace(reason),
		GrantedAt: grantedAt,
		ExpiresAt: expiresAt,
	}
	if err := grant.Validate(); err != nil {
		return OperatorGrant{}, err
	}
	return grant, nil
}

// Validate reports whether the grant is reviewable and duty-separated: a
// named subject, a named approver that differs from the subject, a reason,
// a real start, and a window that does not invert. A validation failure
// means the grant must be fixed or discarded, never honored.
func (g OperatorGrant) Validate() error {
	if strings.TrimSpace(g.Subject) == "" {
		return fmt.Errorf("%w: subject is required", ErrOperatorGrantInvalid)
	}
	if strings.TrimSpace(g.Approver) == "" {
		return fmt.Errorf("%w: approver is required", ErrOperatorGrantInvalid)
	}
	if strings.EqualFold(strings.TrimSpace(g.Approver), strings.TrimSpace(g.Subject)) {
		return fmt.Errorf("%w: approver %q is the operator", ErrSeparationOfDuties, g.Approver)
	}
	if strings.TrimSpace(g.Reason) == "" {
		return fmt.Errorf("%w: reason is required", ErrOperatorGrantInvalid)
	}
	if g.GrantedAt.IsZero() {
		return fmt.Errorf("%w: grant start is required", ErrOperatorGrantInvalid)
	}
	if !g.ExpiresAt.IsZero() && !g.ExpiresAt.After(g.GrantedAt) {
		return fmt.Errorf("%w: expiry must be after the grant start", ErrOperatorGrantInvalid)
	}
	return nil
}

// GrantsForSubject returns the grants naming subject, comparing
// case-insensitively after trimming. It reports, never decides.
func GrantsForSubject(grants []OperatorGrant, subject string) []OperatorGrant {
	var out []OperatorGrant
	for _, g := range grants {
		if strings.EqualFold(strings.TrimSpace(g.Subject), strings.TrimSpace(subject)) {
			out = append(out, g)
		}
	}
	return out
}

// AuthorizeOperator evaluates operator authority from durable bindings: it
// authorizes iff some valid, duty-separated grant naming the principal's
// subject covers now. It is a pure function of its arguments - no context,
// no clock, no I/O - and it never consults the principal's token roles, so
// a token-only operator or administrator claim is refused with
// [ErrOperatorGrantRequired]. A subject whose only grants are
// self-approved is refused with [ErrSeparationOfDuties].
func AuthorizeOperator(principal *trust.Principal, grants []OperatorGrant, now time.Time) error {
	if principal == nil {
		return ErrNoPrincipal
	}
	selfApproved := false
	for _, g := range GrantsForSubject(grants, principal.Subject()) {
		if err := g.Validate(); err != nil {
			if errors.Is(err, ErrSeparationOfDuties) {
				selfApproved = true
			}
			continue
		}
		if now.Before(g.GrantedAt) {
			continue
		}
		if !g.ExpiresAt.IsZero() && !now.Before(g.ExpiresAt) {
			continue
		}
		return nil
	}
	if selfApproved {
		return ErrSeparationOfDuties
	}
	return ErrOperatorGrantRequired
}

// Operator field disclosure through the policy table (RBAC-RT-009).
//
// [OperatorWorkerAuthorization] grants every projected field
// unconditionally to any principal admitted as an operator. That is the
// second RED this todo closes. [OperatorWorkerAuthorizationForRoles] rules
// each projected field through the compiled-in P1A bootstrap policy table
// ([authz.PolicyTable]) instead: the field's data domain, from the closed
// reviewed mapping [OperatorFieldDomain], granted to one of the operator's
// durable HCM roles under [operatorPurpose], which the principal must
// itself authorize. Absence of a grant is a denial; a field with no
// mapping is denied; a redacted-only grant is denied, because
// [people.Effect] has no masked outcome and the raw value must never be
// returned. The legacy constructor stays for the token-admission path its
// read-only callers still serve; new callers resolve the operator's
// durable roles and call this one.

// operatorFieldDomains is the closed, reviewed binding of every
// worker-state field to the policy-table data domain that governs its
// disclosure to an operator. Identity, employment and assignment
// descriptors are worker core; grade and pay zone are
// compensation-adjacent (the band and the geography multiplier set pay),
// so they disclose only under a compensation grant, which no role holds
// under the operator purpose. Adding a people field without extending
// this table denies it: the coverage test pins that exhaustiveness.
var operatorFieldDomains = map[people.FieldID]authz.DataDomain{
	people.FieldWorkerNumber:     authz.DomainCore,
	people.FieldLifecycleStatus:  authz.DomainCore,
	people.FieldLegalName:        authz.DomainCore,
	people.FieldPreferredName:    authz.DomainCore,
	people.FieldEmploymentID:     authz.DomainCore,
	people.FieldLegalEntity:      authz.DomainCore,
	people.FieldWorkerType:       authz.DomainCore,
	people.FieldHireDate:         authz.DomainCore,
	people.FieldEmploymentStatus: authz.DomainCore,
	people.FieldAssignmentID:     authz.DomainCore,
	people.FieldJobCode:          authz.DomainCore,
	people.FieldGrade:            authz.DomainCompensation,
	people.FieldOrgUnit:          authz.DomainCore,
	people.FieldPositionID:       authz.DomainCore,
	people.FieldLocation:         authz.DomainCore,
	people.FieldPayZone:          authz.DomainCompensation,
	people.FieldFTE:              authz.DomainCore,
	people.FieldManagerRelation:  authz.DomainCore,
}

// OperatorFieldDomain reports the policy-table data domain governing an
// operator's disclosure of field. The second result is false for a field
// with no reviewed mapping, which callers must disclose as a denial,
// never as a default.
func OperatorFieldDomain(field people.FieldID) (authz.DataDomain, bool) {
	domain, ok := operatorFieldDomains[field]
	return domain, ok
}

// OperatorWorkerAuthorizationForRoles builds the people
// authorization decision the operator profile evaluates a GetWorkerState
// call under when the caller was admitted through a durable binding:
// every projected field ruled through the policy table for the operator's
// durable HCM roles under the operator purpose. roles is the
// server-resolved durable set, never credential claims; a nil principal
// withholds the subject and denies every field.
func OperatorWorkerAuthorizationForRoles(principal *trust.Principal, roles []string, fields []people.FieldID) people.AuthorizationDecision {
	if principal == nil {
		rulings := make(map[people.FieldID]people.FieldRuling, len(fields))
		for _, f := range fields {
			rulings[f] = people.FieldRuling{Effect: people.EffectDeny, Reason: "no_principal"}
		}
		return people.AuthorizationDecision{
			PolicyVersion:       authz.PolicyVersion,
			Purpose:             operatorPurpose,
			SubjectDisclosable:  false,
			SubjectDenialReason: "no_principal",
			Fields:              rulings,
		}
	}
	purposeAuthorized := principal.AuthorizesPurpose(operatorPurpose)
	rulings := make(map[people.FieldID]people.FieldRuling, len(fields))
	for _, f := range fields {
		rulings[f] = ruleOperatorField(purposeAuthorized, roles, f)
	}
	return people.AuthorizationDecision{
		PolicyVersion:      authz.PolicyVersion,
		Purpose:            operatorPurpose,
		SubjectDisclosable: true,
		Fields:             rulings,
	}
}

// ruleOperatorField rules one worker-state field for an operator holding
// durable roles: the most permissive table grant for the field's domain
// under the operator purpose wins, and anything less than an allow is a
// denial with a policy token explaining it.
func ruleOperatorField(purposeAuthorized bool, roles []string, field people.FieldID) people.FieldRuling {
	if !purposeAuthorized {
		return people.FieldRuling{Effect: people.EffectDeny, Reason: "purpose_not_authorized_for_principal"}
	}
	domain, ok := operatorFieldDomains[field]
	if !ok {
		return people.FieldRuling{Effect: people.EffectDeny, Reason: "unknown_field"}
	}
	best := people.FieldRuling{Effect: people.EffectDeny, Reason: "no_grant_for_domain"}
	redacted := false
	for _, role := range roles {
		domains, ok := authz.PolicyTable[authz.RoleID(strings.ToLower(strings.TrimSpace(role)))]
		if !ok {
			continue
		}
		grant, ok := domains[domain]
		if !ok {
			continue
		}
		if !grant.AnyPurpose && !slices.Contains(grant.Purposes, operatorPurpose) {
			continue
		}
		switch grant.Effect {
		case authz.EffectAllow:
			return people.FieldRuling{Effect: people.EffectAllow, Reason: grant.RuleID}
		case authz.EffectRedacted:
			redacted = true
		}
	}
	if redacted {
		return people.FieldRuling{Effect: people.EffectDeny, Reason: "granted_redacted"}
	}
	return best
}
