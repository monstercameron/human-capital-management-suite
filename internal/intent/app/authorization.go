package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// ErrAuthorizationDenied is what a resolver returns when the BOOTSTRAP
// authorization policy refuses the read outright: the subject is not
// disclosable to this caller, a field the intent cannot be answered without is
// denied, or every field it asked for is denied.
//
// It is a distinct error rather than an empty answer because the three
// outcomes are different products. A partial denial is still an answer - the
// domain packages report a denied field by name - but "you may not read any of
// this under this purpose" is a refusal, and the service projects it as
// PERMISSION_DENIED rather than as a malformed request.
var ErrAuthorizationDenied = errors.New("app: the bootstrap authorization policy denies this read")

// peopleFieldPolicy maps every governed worker-state field this cell reads
// onto the field the BOOTSTRAP policy classifies.
//
// The two vocabularies are deliberately separate: internal/domains/people
// names the eighteen fields a worker-state explanation can disclose, and
// internal/trust/authz names the twelve fields its closed FieldRegistry
// classifies into data domains. This table is the only place the two meet, so
// a field whose sensitivity changes is a one-line edit here rather than a
// silent re-classification spread across four call sites.
//
// Every worker-state field P1A reads is worker.core: the compartmentalised
// domains (tax, bank, medical, employee relations, immigration) have no
// worker-state field at all, which is why none of them appears.
var peopleFieldPolicy = map[people.FieldID]authz.FieldID{
	people.FieldWorkerNumber:     authz.FieldWorkerNumber,
	people.FieldLifecycleStatus:  authz.FieldWorkerNumber,
	people.FieldLegalName:        authz.FieldWorkerNumber,
	people.FieldPreferredName:    authz.FieldWorkerNumber,
	people.FieldEmploymentID:     authz.FieldWorkerNumber,
	people.FieldLegalEntity:      authz.FieldWorkerNumber,
	people.FieldWorkerType:       authz.FieldWorkerNumber,
	people.FieldHireDate:         authz.FieldWorkerNumber,
	people.FieldEmploymentStatus: authz.FieldWorkerNumber,
	people.FieldAssignmentID:     authz.FieldJobTitle,
	people.FieldJobCode:          authz.FieldJobTitle,
	people.FieldGrade:            authz.FieldJobTitle,
	people.FieldOrgUnit:          authz.FieldJobTitle,
	people.FieldPositionID:       authz.FieldJobTitle,
	people.FieldLocation:         authz.FieldJobTitle,
	people.FieldPayZone:          authz.FieldJobTitle,
	people.FieldFTE:              authz.FieldJobTitle,
	people.FieldManagerRelation:  authz.FieldJobTitle,
}

// AuthorizationPolicyVersion is the policy this cell evaluates every governed
// read under. It is the BOOTSTRAP policy's own version, not a local constant:
// a result that claims a policy version must name the policy that actually
// decided it.
const AuthorizationPolicyVersion = authz.PolicyVersion

// authorizationRequest is one intent's governed-read authorization question.
type authorizationRequest struct {
	// Subject is the record being read.
	Subject values.EntityRef
	// EvaluatedAt is the instant every temporal check is made at.
	EvaluatedAt values.Instant
	// Gate is the set of policy fields the intent cannot be answered without.
	// A single denial anywhere in it refuses the whole call.
	Gate []authz.FieldID
	// Read is the set of policy fields the domain projection discloses. A
	// partial denial is passed through and reported per field; a total denial
	// refuses.
	Read []authz.FieldID
	// Relationships are the relationship facts between the principal and
	// Subject the scope stage evaluates (PROMOUX-015: [managerChainFacts]).
	Relationships []authz.RelationshipFact
}

// authorizationResult is the evaluated decision plus the projections the
// domain packages consume.
type authorizationResult struct {
	Decision authz.Decision
	Purpose  string
}

// authorize evaluates one governed read against the BOOTSTRAP policy.
//
// There is exactly one evaluator (authz.Enforce), and this is the only call
// site in the cell: a second, weaker decision made locally would be a policy
// nobody published. The refusal rules are stated here rather than inside
// authz because they are about what an intent needs, not about what the
// policy says - the policy answers per field, and only the intent knows which
// of its fields are load-bearing.
func authorizeRead(principal *trust.Principal, purpose string, req authorizationRequest) (authorizationResult, error) {
	if principal == nil {
		return authorizationResult{}, fmt.Errorf("%w: no verified principal", ErrAuthorizationDenied)
	}
	fields := mergeFields(req.Gate, req.Read)
	decision, err := authz.Enforce(authz.Request{
		Principal:   principal,
		Purpose:     purpose,
		EffectiveAt: req.EvaluatedAt,
		Subject:     req.Subject,
		// No organization-graph projection exists in P1A: the organization
		// domain owns the part_of edges and does not publish them yet. A zero
		// PrincipalOrg is authz's documented single-company default, which
		// resolves the tenant boundary as the whole of the authority rather
		// than guessing an org closure the cell cannot read.
		Fields:        fields,
		Relationships: req.Relationships,
	})
	if err != nil {
		return authorizationResult{}, fmt.Errorf("app: evaluate the bootstrap authorization policy: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return authorizationResult{}, fmt.Errorf("app: the authorization decision is not recordable: %w", err)
	}

	if !decision.SubjectDisclosable {
		return authorizationResult{}, fmt.Errorf("%w: subject is not disclosable (%s)",
			ErrAuthorizationDenied, decision.SubjectDenialReason)
	}
	for _, f := range req.Gate {
		if ruling := decision.Fields[f]; ruling.Effect != authz.EffectAllow {
			return authorizationResult{}, fmt.Errorf("%w: %s is %s under purpose %q (%s)",
				ErrAuthorizationDenied, f, ruling.Effect, decision.Purpose, ruling.Reason)
		}
	}
	if len(req.Read) > 0 && !anyAllowed(decision, req.Read) {
		return authorizationResult{}, fmt.Errorf("%w: every requested field is denied under purpose %q",
			ErrAuthorizationDenied, decision.Purpose)
	}
	return authorizationResult{Decision: decision, Purpose: decision.Purpose}, nil
}

// anyAllowed reports whether at least one of fields was granted.
func anyAllowed(d authz.Decision, fields []authz.FieldID) bool {
	for _, f := range fields {
		switch d.Fields[f].Effect {
		case authz.EffectAllow, authz.EffectRedacted:
			return true
		}
	}
	return false
}

// mergeFields returns the sorted, de-duplicated union of two field sets, so
// one decision covers the whole question and the digest it carries is stable.
func mergeFields(sets ...[]authz.FieldID) []authz.FieldID {
	seen := map[authz.FieldID]struct{}{}
	out := make([]authz.FieldID, 0, 8)
	for _, set := range sets {
		for _, f := range set {
			if _, ok := seen[f]; ok {
				continue
			}
			seen[f] = struct{}{}
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}

// peopleFields is the policy-field projection of a worker-state field set.
func peopleFields(fields []people.FieldID) []authz.FieldID {
	out := make([]authz.FieldID, 0, len(fields))
	for _, f := range fields {
		if mapped, ok := peopleFieldPolicy[f]; ok {
			out = append(out, mapped)
		}
	}
	return mergeFields(out)
}

// peopleDecision narrows the composed policy decision onto the shape
// internal/domains/people consumes.
//
// The people package's decision is deliberately narrower: it rules ALLOW or
// DENY per field and knows nothing about redaction or obligations. A REDACTED
// ruling is projected as DENY rather than as ALLOW, because this cell has no
// masking transform to apply and disclosing a raw value under a ruling that
// said "masked only" would be the exact failure the ruling exists to prevent.
func peopleDecision(result authorizationResult, fields []people.FieldID) people.AuthorizationDecision {
	rulings := make(map[people.FieldID]people.FieldRuling, len(fields))
	for _, f := range fields {
		rulings[f] = peopleRuling(result.Decision, f)
	}
	return people.AuthorizationDecision{
		PolicyVersion:      AuthorizationPolicyVersion,
		Purpose:            result.Purpose,
		SubjectDisclosable: result.Decision.SubjectDisclosable,
		Fields:             rulings,
	}
}

func peopleRuling(d authz.Decision, f people.FieldID) people.FieldRuling {
	mapped, ok := peopleFieldPolicy[f]
	if !ok {
		return people.FieldRuling{Effect: people.EffectDeny, Reason: "field_not_classified_by_policy"}
	}
	ruling := d.Fields[mapped]
	if ruling.Effect == authz.EffectAllow {
		return people.FieldRuling{Effect: people.EffectAllow}
	}
	reason := ruling.Reason
	if reason == "" {
		reason = "no_grant_for_domain"
	}
	if ruling.Effect == authz.EffectRedacted {
		reason = "granted_redacted_no_masking_transform"
	}
	return people.FieldRuling{Effect: people.EffectDeny, Reason: reason}
}

// dataopsDecision narrows the composed policy decision onto the shape
// internal/domains/dataops consumes. The DataOps field vocabulary is open, so
// the mapping goes through the worker-state tokens this cell compares: a
// comparison field with no classification is denied, never defaulted.
func dataopsDecision(result authorizationResult, fields []dataops.FieldID) dataops.Authorization {
	rulings := make(map[dataops.FieldID]dataops.Ruling, len(fields))
	for _, f := range fields {
		ruling := peopleRuling(result.Decision, people.FieldID(f))
		if ruling.Effect == people.EffectAllow {
			rulings[f] = dataops.Ruling{Effect: dataops.EffectAllow}
			continue
		}
		rulings[f] = dataops.Ruling{Effect: dataops.EffectDeny, Reason: ruling.Reason}
	}
	return dataops.Authorization{
		PolicyVersion:      AuthorizationPolicyVersion,
		Purpose:            result.Purpose,
		SubjectDisclosable: result.Decision.SubjectDisclosable,
		Fields:             rulings,
	}
}

// intelligenceDecision projects the composed policy decision onto the
// section-level shape internal/domains/intelligence consumes.
//
// Sections are not fields, and the BOOTSTRAP policy has no section
// vocabulary. What it does decide is whether this caller may reach the
// transaction at all, and that is exactly what gates the sections here: a
// disclosable transaction discloses the sections the request asked for, and a
// non-disclosable one never reaches this function because authorizeRead has
// already refused. Inventing a per-section policy locally would be a second
// evaluator the platform never published.
func intelligenceDecision(result authorizationResult, sections []intelligence.Section) intelligence.AuthorizationDecision {
	rulings := make(map[intelligence.Section]intelligence.Ruling, len(sections))
	for _, s := range sections {
		rulings[s] = intelligence.Ruling{Effect: intelligence.EffectAllow}
	}
	fields := make(map[string]intelligence.Ruling, len(intelligence.RedactableFields()))
	for _, f := range intelligence.RedactableFields() {
		fields[f] = intelligence.Ruling{Effect: intelligence.EffectAllow}
	}
	return intelligence.AuthorizationDecision{
		PolicyVersion:          AuthorizationPolicyVersion,
		Purpose:                result.Purpose,
		TransactionDisclosable: result.Decision.SubjectDisclosable,
		Sections:               rulings,
		Fields:                 fields,
	}
}
