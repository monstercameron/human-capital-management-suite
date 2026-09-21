package authz

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Directory disclosure is the RBAC-RT-001 decision point: per-subject field
// rulings for one worker directory row. Row visibility (which workers a
// caller may list) is decided upstream by the listing policy; this point
// decides, for each visible row, which governed fields may be serialized for
// this viewing subject. Deny by default: a field without a grant scoped to
// this subject is omitted or masked, never copied.
//
// The rule, per viewing subject over one visible row:
//
//   - self (the row is the principal's own record) and administrative roles
//     (compensation, payroll, audit and finance administration, including the
//     legacy hcm_admin token acting under its mapped template) resolve
//     through the role-grant table, so an auditor still receives REDACTED
//     values with their logging obligation rather than raw pay.
//   - the manager role's compensation disclosure is chain-scoped: it applies
//     only to subjects in the principal's reporting line. A manager viewing
//     a same-unit colleague outside her chain, or her own manager, receives
//     no pay, no legal name and no manager linkage for that row.
//   - the HR-partner grant stays role-wide until RBAC-RT-007 supplies
//     HR-partner relationship facts this point can evaluate; durable-role
//     resolution stays with RBAC-RT-002. Neither is widened here, only left
//     where the grant table already put it.
//   - anything else (worker_self viewing another record, an unrecognized
//     role, no role at all) keeps the row but loses compensation and the
//     legal name: worker_self compensation is self-only and unknown roles
//     hold no grant.
//
// Tenant isolation is not re-decided here: the listing this point serves is
// already confined to the principal's tenant by admission and row-level
// security, so the call is same-tenant by construction and carries no
// mandatory cross-tenant denies.

// DirectorySubject is what the directory serialization path knows about one
// visible row: whether the row is the viewer's own record, and whether the
// viewer appears on the row's reporting line as established by the governed
// listing's own manager references.
type DirectorySubject struct {
	Self           bool
	InManagerChain bool
}

// DirectoryDisclosure is the ruling for one directory row: the
// compensation-domain ruling applied to base pay and bonus target together
// (one domain, one grant, one ruling), the legal-name ruling, and the
// manager-linkage ruling. Every effect is Allow, Redacted or Denied; a
// serializer omits on Denied, substitutes its stand-in on Redacted, and
// copies on Allow.
type DirectoryDisclosure struct {
	Purpose        string
	PolicyVersion  string
	Pay            FieldRuling
	LegalName      FieldRuling
	ManagerLinkage FieldRuling
}

// directoryCompensationFields is the closed set of wire pay fields one
// compensation-domain ruling governs.
var directoryCompensationFields = []FieldID{FieldBaseSalary, FieldBonusTarget}

// directoryAdministratorRoles holds the role templates whose directory
// reach is administrative (role- and tenant-bound) rather than
// relationship-bound, mirroring [administrativeRoleOrder].
var directoryAdministratorRoles = []RoleID{RoleCompAdmin, RolePayrollManager, RoleAuditor, RoleFinancePartner}

// directoryRoleAlias maps the legacy hcm_admin token onto the comp_admin
// template for directory disclosure. The task rule treats the two as
// equivalent for directory reads; RBAC-RT-009 owns deriving administrator
// authority from durable bindings and retires this alias when it lands.
func directoryRoleAlias(roles []string) []string {
	for _, r := range roles {
		if RoleID(r) == RoleCompAdmin {
			return roles
		}
	}
	for _, r := range roles {
		if r == "hcm_admin" {
			return append(append([]string(nil), roles...), string(RoleCompAdmin))
		}
	}
	return roles
}

// ResolveDirectoryDisclosure resolves the per-subject directory ruling
// through the policy decision point: compensation through the purpose-bound
// grant table, legal name and manager linkage through the subject-scoped
// rule below. An empty purpose falls back to the principal's default
// purpose, matching [Enforce]; a principal authorizing no purpose receives
// Denied rulings rather than an error, because "no purpose" is a disclosure
// answer, not a malformed request.
func ResolveDirectoryDisclosure(principal *trust.Principal, purpose string, subject DirectorySubject) (DirectoryDisclosure, error) {
	return ResolveDirectoryDisclosureWithRoles(principal, nil, purpose, subject)
}

// ResolveDirectoryDisclosureWithRoles is [ResolveDirectoryDisclosure] over
// an explicit server-resolved role set instead of the principal's credential
// roles. A non-nil roles, even empty, governs as-is: an empty durable
// assignment authorizes nothing and never falls back to the credential. A
// nil roles keeps the legacy credential-role behavior for callers with no
// role store.
func ResolveDirectoryDisclosureWithRoles(principal *trust.Principal, roles []string, purpose string, subject DirectorySubject) (DirectoryDisclosure, error) {
	if principal == nil {
		return DirectoryDisclosure{}, fmt.Errorf("%w: nil principal", ErrInvalidPolicyInput)
	}
	if purpose == "" {
		purpose = principal.DefaultPurpose()
	}
	if roles == nil {
		roles = principal.Roles()
	}

	roles = directoryRoleAlias(roles)
	held := rolesOf(roles)
	heldSet := make(map[RoleID]struct{}, len(held))
	for _, r := range held {
		heldSet[r] = struct{}{}
	}
	_, isAdmin := intersection(heldSet, directoryAdministratorRoles)
	_, isManager := heldSet[RoleManager]
	_, isHRPartner := heldSet[RoleHRPartner]

	fields, err := resolveFields(roles, principal.AuthorizesPurpose, purpose, directoryCompensationFields, nil)
	if err != nil {
		return DirectoryDisclosure{}, err
	}
	pay := fields.Rulings[FieldBaseSalary]

	switch {
	case subject.Self || isAdmin:
		// Self and administrative reach: the grant table's answer stands,
		// including REDACTED with obligations for the auditor.
	case isManager:
		if !subject.InManagerChain {
			pay = FieldRuling{Effect: EffectDenied, RuleID: "p1a.directory.manager_chain_required", Reason: "manager_chain_required"}
		}
	case isHRPartner:
		// Role-wide until RBAC-RT-007 supplies the relationship facts.
	default:
		if pay.Effect == EffectAllow {
			pay = FieldRuling{Effect: EffectDenied, RuleID: "p1a.directory.self_or_grant_required", Reason: "self_or_grant_required"}
		}
	}

	legalName := FieldRuling{Effect: EffectDenied, RuleID: "p1a.directory.identity.deny_default", Reason: "relationship_required"}
	switch {
	case subject.Self:
		legalName = FieldRuling{Effect: EffectAllow, RuleID: "p1a.directory.identity.self", Reason: "granted"}
	case isAdmin:
		legalName = FieldRuling{Effect: EffectAllow, RuleID: "p1a.directory.identity.administrative", Reason: "granted"}
	case isManager && subject.InManagerChain:
		legalName = FieldRuling{Effect: EffectAllow, RuleID: "p1a.directory.identity.manager_chain", Reason: "granted"}
	case isHRPartner:
		legalName = FieldRuling{Effect: EffectAllow, RuleID: "p1a.directory.identity.legacy_role_grant", Reason: "granted"}
	}

	linkage := FieldRuling{Effect: EffectAllow, RuleID: "p1a.directory.manager_linkage.row_visible", Reason: "granted"}
	if isManager && !subject.Self && !isAdmin && !subject.InManagerChain {
		linkage = FieldRuling{Effect: EffectDenied, RuleID: "p1a.directory.manager_chain_required", Reason: "manager_chain_required"}
	}

	return DirectoryDisclosure{
		Purpose:        purpose,
		PolicyVersion:  PolicyVersion,
		Pay:            pay,
		LegalName:      legalName,
		ManagerLinkage: linkage,
	}, nil
}

func intersection(held map[RoleID]struct{}, roles []RoleID) (RoleID, bool) {
	for _, r := range roles {
		if _, ok := held[r]; ok {
			return r, true
		}
	}
	return "", false
}
