package authz

import (
	"errors"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PolicyVersion is the version fingerprint of the compiled-in P1A bootstrap
// policy table. It changes whenever [PolicyTable] or [FieldRegistry] changes,
// so a recorded decision can always be replayed against the policy that
// produced it.
const PolicyVersion = "authz.p1a.bootstrap.v2"

// Effect is the outcome of one authorization ruling. It is shared by the
// tenant, scope and field layers so that "allow" and "deny" mean the same
// thing everywhere in a decision.
type Effect uint8

// Effects. EffectUnspecified is the zero value and is never legal in a
// completed ruling.
const (
	EffectUnspecified Effect = iota
	// EffectAllow permits the action or discloses the value.
	EffectAllow
	// EffectDenied withholds the value or blocks the action under an
	// explicit policy rule: no grant, an incompatible purpose, or a
	// mandatory, non-delegable restriction.
	EffectDenied
	// EffectRedacted permits disclosure only as a masked or transformed
	// value; the raw value is never returned. It always carries at least one
	// obligation.
	EffectRedacted
	// EffectWithheld means the requested item is withheld not because a
	// field-level or record-level rule denies it on its own terms, but
	// because the record it belongs to is not disclosable to this
	// principal. Every field on a non-disclosable subject is withheld
	// uniformly, so that a caller cannot distinguish "denied" from
	// "withheld" and infer which fields would otherwise have been granted.
	EffectWithheld
)

var effectWire = map[Effect]string{
	EffectAllow:    "ALLOW",
	EffectDenied:   "DENIED",
	EffectRedacted: "REDACTED",
	EffectWithheld: "WITHHELD",
}

// String returns the stable wire token, or "EFFECT_UNSPECIFIED".
func (e Effect) String() string {
	if s, ok := effectWire[e]; ok {
		return s
	}
	return "EFFECT_UNSPECIFIED"
}

// Valid reports whether e is a legal, non-zero effect.
func (e Effect) Valid() bool { _, ok := effectWire[e]; return ok }

// rank orders effects from least to most permissive, for composing several
// candidate rulings (one per role a principal holds) into the single most
// permissive ruling that still respects an explicit deny. Unspecified and
// invalid values rank below every legal effect so that a missing ruling never
// outranks a real one.
func (e Effect) rank() int {
	switch e {
	case EffectDenied:
		return 1
	case EffectWithheld:
		return 2
	case EffectRedacted:
		return 3
	case EffectAllow:
		return 4
	default:
		return 0
	}
}

// RoleID names one P1A bootstrap role template. Role identity is a policy
// token, not a display label.
type RoleID string

// The P1A bootstrap role templates.
const (
	RoleWorkerSelf     RoleID = "worker_self"
	RoleManager        RoleID = "manager"
	RoleHRPartner      RoleID = "hr_partner"
	RoleCompAdmin      RoleID = "comp_admin"
	RolePayrollManager RoleID = "payroll_manager"
	RoleAuditor        RoleID = "auditor"
	// RoleFinancePartner is PROMOUX-015's finance approver: it reviews the
	// compensation a promotion moves under compensation_review and nothing
	// else. No cost-center relationship graph exists in this release, so its
	// scope is the administrative tenant/organization boundary (see
	// [administrativeRoleOrder]) rather than FinancePartnerFor(cost_center).
	RoleFinancePartner RoleID = "finance_partner"
)

// roleEvaluationOrder is the fixed order roles are evaluated in wherever more
// than one of a principal's roles could produce a ruling. Ties are broken by
// [Effect.rank], but the order still has to be fixed for the evidence trail
// (matched rule IDs) to be deterministic across runs with the same input.
var roleEvaluationOrder = []RoleID{RoleWorkerSelf, RoleManager, RoleHRPartner, RoleCompAdmin, RolePayrollManager, RoleAuditor, RoleFinancePartner}

// The machine bootstrap role templates. Humans never match them and machines
// never match the human templates above: see [RolesForKind]. Both templates
// grant least privilege by construction — every domain absent from
// [MachinePolicyTable] is denied, exactly as for the human table — and the
// grants cover only non-sensitive baseline domains. A machine that needs a
// sensitive domain holds no grant until policy classifies one for it.
const (
	// RoleMachineObserver is the first-party service observer: it reads
	// worker identity and contact facts and nothing else.
	RoleMachineObserver RoleID = "machine_observer"
	// RoleIntegrationSync is the third-party integration sync identity: it
	// reads worker identity facts and nothing else, and it holds no
	// capability scope (see [GrantedCapabilityScopes]).
	RoleIntegrationSync RoleID = "integration_sync"
)

// MachinePolicyVersion is the version fingerprint of the compiled-in machine
// bootstrap policy table. It changes whenever [MachinePolicyTable] changes,
// so a recorded decision can always be replayed against the policy that
// produced it. It is separate from [PolicyVersion] because the machine table
// has its own lifecycle: the human table is frozen, this one grows as
// INTAPI-001 registers machine clients.
const MachinePolicyVersion = "authz.p1a.machine.v1"

// MachinePolicyTable is the compiled-in least-privilege authorization policy
// for machine subject kinds: for each machine role template, the purpose-bound
// grant over each data domain it may touch. A role/domain pair absent from
// the table has no grant, which is deny-by-default.
var MachinePolicyTable = map[RoleID]map[DataDomain]PurposeGrant{
	RoleMachineObserver: {
		DomainCore:    {RuleID: "p1a.machine_observer.core", AnyPurpose: true, Effect: EffectAllow},
		DomainContact: {RuleID: "p1a.machine_observer.contact", AnyPurpose: true, Effect: EffectAllow},
	},
	RoleIntegrationSync: {
		DomainCore: {RuleID: "p1a.integration_sync.core", AnyPurpose: true, Effect: EffectAllow},
	},
}

// machineCapabilityScopes is the capability scope each machine template
// grants. It is deliberately narrower than the field grants above: a scope
// reaches a whole capability, so a template holds a scope only for
// capabilities whose every classified data domain it is also granted. The
// observer reads worker identity through the people capabilities; the sync
// identity holds no capability scope at all until INTAPI-001 registers
// machine clients with explicit scope grants.
var machineCapabilityScopes = map[RoleID][]string{
	RoleMachineObserver: {"scope:people.read"},
}

// GrantedCapabilityScopes returns the sorted, de-duplicated capability scopes
// the kind-gated role set grants. It never contains a wildcard: every grant
// names its exact scope. Humans resolve no scope here — a human capability
// call is authorized by purpose, with field policy enforced at intent
// resolution — while machines resolve only what their least-privilege
// template names, which is empty until INTAPI-001 grants more.
func GrantedCapabilityScopes(kind trust.SubjectKind, roles []string) []string {
	var out []string
	for _, role := range RolesForKind(kind, roles) {
		out = append(out, machineCapabilityScopes[role]...)
	}
	return dedupeSorted(out)
}

// RolesForKind returns the subset of roles that grants authority for a
// principal of kind, in fixed evaluation order. Human principals match the
// human bootstrap templates (with the legacy hcm_admin alias); service and
// integration principals match only their least-privilege machine templates;
// agents and unspecified kinds match nothing — an agent acts only through a
// server-verified delegation. A role naming a template of another kind
// grants nothing: a token-claimed human role never authorizes a machine,
// and a machine role never authorizes a human.
func RolesForKind(kind trust.SubjectKind, roles []string) []RoleID {
	held := make(map[RoleID]struct{}, len(roles))
	for _, r := range roles {
		held[RoleID(r)] = struct{}{}
	}
	switch kind {
	case trust.SubjectKindHuman:
		if _, ok := held[RoleID("hcm_admin")]; ok {
			held[RoleCompAdmin] = struct{}{}
		}
		out := make([]RoleID, 0, len(roleEvaluationOrder))
		for _, r := range roleEvaluationOrder {
			if _, ok := held[r]; ok {
				out = append(out, r)
			}
		}
		return out
	case trust.SubjectKindService:
		if _, ok := held[RoleMachineObserver]; ok {
			return []RoleID{RoleMachineObserver}
		}
		return nil
	case trust.SubjectKindIntegration:
		if _, ok := held[RoleIntegrationSync]; ok {
			return []RoleID{RoleIntegrationSync}
		}
		return nil
	default:
		return nil
	}
}

// rolesOf is the human-kind entry of [RolesForKind], kept for the callers
// that resolve without a subject kind at hand. New call sites must prefer
// [RolesForKind]: a role outside the caller's kind grants nothing, and only
// RolesForKind enforces that.
func rolesOf(roles []string) []RoleID {
	return RolesForKind(trust.SubjectKindHuman, roles)
}

// DataDomain is a stable business data domain, not a physical table. One
// domain may span several tables; one table may carry fields from several
// domains. Policy always refers to the domain.
type DataDomain string

// Data domains covered by the P1A bootstrap policy.
const (
	DomainCore              DataDomain = "worker.core"
	DomainContact           DataDomain = "worker.contact"
	DomainCompensation      DataDomain = "worker.compensation"
	DomainTax               DataDomain = "worker.tax"
	DomainBank              DataDomain = "worker.bank"
	DomainPerformance       DataDomain = "worker.performance"
	DomainMedical           DataDomain = "worker.medical"
	DomainEmployeeRelations DataDomain = "worker.employee_relations"
	DomainImmigration       DataDomain = "worker.immigration"
)

// FieldID names one governed field. It is a policy token, never a struct
// field name, so that renaming a Go field never silently changes
// authorization meaning.
type FieldID string

// FieldDefinition binds a field to the one data domain that governs it.
type FieldDefinition struct {
	ID     FieldID
	Domain DataDomain
}

// P1A bootstrap field identifiers. The set is closed: [FieldRegistry] is the
// only lookup this package or any caller uses to classify a field, so API
// serialization, UI rendering, export and agent/tool filtering all inherit
// the same answer.
const (
	FieldWorkerNumber        FieldID = "worker.worker_number"
	FieldJobTitle            FieldID = "assignment.job_title"
	FieldWorkEmail           FieldID = "person.work_email"
	FieldHomeAddress         FieldID = "person.home_address"
	FieldBaseSalary          FieldID = "compensation.base_salary"
	FieldBonusTarget         FieldID = "compensation.bonus_target"
	FieldTaxID               FieldID = "tax.tax_id"
	FieldBankAccountNumber   FieldID = "bank.account_number"
	FieldPerformanceRating   FieldID = "performance.rating"
	FieldMedicalAccomodation FieldID = "medical.accommodation"
	FieldCaseNotes           FieldID = "employee_relations.case_notes"
	FieldVisaStatus          FieldID = "immigration.visa_status"
)

// FieldRegistry is the closed, single source of truth mapping every governed
// [FieldID] to the [DataDomain] that governs it. [ResolveFields] refuses a
// field absent from this map rather than guessing its sensitivity.
var FieldRegistry = map[FieldID]FieldDefinition{
	FieldWorkerNumber:        {ID: FieldWorkerNumber, Domain: DomainCore},
	FieldJobTitle:            {ID: FieldJobTitle, Domain: DomainCore},
	FieldWorkEmail:           {ID: FieldWorkEmail, Domain: DomainContact},
	FieldHomeAddress:         {ID: FieldHomeAddress, Domain: DomainContact},
	FieldBaseSalary:          {ID: FieldBaseSalary, Domain: DomainCompensation},
	FieldBonusTarget:         {ID: FieldBonusTarget, Domain: DomainCompensation},
	FieldTaxID:               {ID: FieldTaxID, Domain: DomainTax},
	FieldBankAccountNumber:   {ID: FieldBankAccountNumber, Domain: DomainBank},
	FieldPerformanceRating:   {ID: FieldPerformanceRating, Domain: DomainPerformance},
	FieldMedicalAccomodation: {ID: FieldMedicalAccomodation, Domain: DomainMedical},
	FieldCaseNotes:           {ID: FieldCaseNotes, Domain: DomainEmployeeRelations},
	FieldVisaStatus:          {ID: FieldVisaStatus, Domain: DomainImmigration},
}

// Purpose tokens the P1A bootstrap policy recognizes. A [trust.Principal]
// carries the wider set of purposes it is authorized for; these are only the
// ones the bootstrap field grants below key off.
const (
	PurposeSelfService        = "self_service_view"
	PurposeCompensationReview = "compensation_review"
	PurposePayrollProcessing  = "payroll_processing"
	PurposePerformanceReview  = "performance_review"
	PurposeAccommodationCase  = "accommodation_case"
	PurposeCaseManagement     = "case_management"
	PurposeImmigrationCase    = "immigration_case"
	PurposeAuditReview        = "audit_review"
)

// obligationEvidenceLogged is attached to every redacted ruling: a redacted
// value is only ever handed to a principal under a logging obligation.
const obligationEvidenceLogged = "evidence_logged"

// PurposeGrant is one role's grant over one data domain: which purposes it is
// good for, what effect it produces, and what obligations it carries.
type PurposeGrant struct {
	// RuleID is the stable, explainable identifier for this grant. It is
	// what appears in a [Decision]'s matched-rule list.
	RuleID string
	// AnyPurpose reports whether the grant applies regardless of declared
	// purpose. It is only true for the two baseline, non-sensitive domains.
	AnyPurpose bool
	// Purposes is the closed set of purposes the grant applies under when
	// AnyPurpose is false.
	Purposes []string
	// Effect is EffectAllow or EffectRedacted. A grant never encodes
	// EffectDenied or EffectWithheld: those are the absence of a grant, not
	// a grant.
	Effect Effect
	// Obligations lists the obligation tokens a matching decision must
	// carry forward.
	Obligations []string
}

func (g PurposeGrant) appliesTo(purpose string) bool {
	if g.AnyPurpose {
		return true
	}
	return slices.Contains(g.Purposes, purpose)
}

// PolicyTable is the compiled-in P1A bootstrap authorization policy: for each
// of the six role templates, the purpose-bound grant over each data domain.
// A role/domain pair absent from the table has no grant, which is
// deny-by-default rather than an omission to fix later.
var PolicyTable = map[RoleID]map[DataDomain]PurposeGrant{
	RoleWorkerSelf: {
		DomainCore:         {RuleID: "p1a.worker_self.core", AnyPurpose: true, Effect: EffectAllow},
		DomainContact:      {RuleID: "p1a.worker_self.contact", AnyPurpose: true, Effect: EffectAllow},
		DomainCompensation: {RuleID: "p1a.worker_self.compensation", Purposes: []string{PurposeSelfService}, Effect: EffectAllow},
		DomainTax:          {RuleID: "p1a.worker_self.tax", Purposes: []string{PurposeSelfService}, Effect: EffectAllow},
		DomainBank:         {RuleID: "p1a.worker_self.bank", Purposes: []string{PurposeSelfService}, Effect: EffectAllow},
		DomainPerformance:  {RuleID: "p1a.worker_self.performance", Purposes: []string{PurposeSelfService}, Effect: EffectAllow},
		DomainMedical:      {RuleID: "p1a.worker_self.medical", Purposes: []string{PurposeSelfService}, Effect: EffectAllow},
		DomainImmigration:  {RuleID: "p1a.worker_self.immigration", Purposes: []string{PurposeSelfService}, Effect: EffectAllow},
	},
	RoleManager: {
		DomainCore:         {RuleID: "p1a.manager.core", AnyPurpose: true, Effect: EffectAllow},
		DomainContact:      {RuleID: "p1a.manager.contact", AnyPurpose: true, Effect: EffectAllow},
		DomainCompensation: {RuleID: "p1a.manager.compensation", Purposes: []string{PurposeCompensationReview}, Effect: EffectAllow},
		DomainPerformance:  {RuleID: "p1a.manager.performance", Purposes: []string{PurposePerformanceReview}, Effect: EffectAllow},
	},
	RoleHRPartner: {
		DomainCore:         {RuleID: "p1a.hr_partner.core", AnyPurpose: true, Effect: EffectAllow},
		DomainContact:      {RuleID: "p1a.hr_partner.contact", AnyPurpose: true, Effect: EffectAllow},
		DomainCompensation: {RuleID: "p1a.hr_partner.compensation", Purposes: []string{PurposeCompensationReview}, Effect: EffectAllow},
		DomainPerformance:  {RuleID: "p1a.hr_partner.performance", Purposes: []string{PurposePerformanceReview}, Effect: EffectAllow},
		DomainMedical:      {RuleID: "p1a.hr_partner.medical", Purposes: []string{PurposeAccommodationCase}, Effect: EffectAllow},
		DomainEmployeeRelations: {
			RuleID: "p1a.hr_partner.employee_relations", Purposes: []string{PurposeCaseManagement}, Effect: EffectAllow,
		},
		DomainImmigration: {RuleID: "p1a.hr_partner.immigration", Purposes: []string{PurposeImmigrationCase}, Effect: EffectAllow},
	},
	RoleCompAdmin: {
		DomainCore:    {RuleID: "p1a.comp_admin.core", AnyPurpose: true, Effect: EffectAllow},
		DomainContact: {RuleID: "p1a.comp_admin.contact", AnyPurpose: true, Effect: EffectAllow},
		DomainCompensation: {
			RuleID: "p1a.comp_admin.compensation", Purposes: []string{PurposeCompensationReview, PurposePayrollProcessing}, Effect: EffectAllow,
		},
		DomainTax:  {RuleID: "p1a.comp_admin.tax", Purposes: []string{PurposePayrollProcessing}, Effect: EffectAllow},
		DomainBank: {RuleID: "p1a.comp_admin.bank", Purposes: []string{PurposePayrollProcessing}, Effect: EffectAllow},
	},
	RolePayrollManager: {
		DomainCore:    {RuleID: "p1a.payroll_manager.core", AnyPurpose: true, Effect: EffectAllow},
		DomainContact: {RuleID: "p1a.payroll_manager.contact", AnyPurpose: true, Effect: EffectAllow},
		DomainCompensation: {
			RuleID: "p1a.payroll_manager.compensation", Purposes: []string{PurposeCompensationReview, PurposePayrollProcessing}, Effect: EffectAllow,
		},
		DomainTax:  {RuleID: "p1a.payroll_manager.tax", Purposes: []string{PurposePayrollProcessing}, Effect: EffectAllow},
		DomainBank: {RuleID: "p1a.payroll_manager.bank", Purposes: []string{PurposePayrollProcessing}, Effect: EffectAllow},
	},
	RoleFinancePartner: {
		DomainCore:         {RuleID: "p1a.finance_partner.core", AnyPurpose: true, Effect: EffectAllow},
		DomainCompensation: {RuleID: "p1a.finance_partner.compensation", Purposes: []string{PurposeCompensationReview}, Effect: EffectAllow},
	},
	RoleAuditor: {
		DomainCore:    {RuleID: "p1a.auditor.core", AnyPurpose: true, Effect: EffectAllow},
		DomainContact: {RuleID: "p1a.auditor.contact", AnyPurpose: true, Effect: EffectAllow},
		DomainCompensation: {
			RuleID: "p1a.auditor.compensation", Purposes: []string{PurposeAuditReview}, Effect: EffectRedacted, Obligations: []string{obligationEvidenceLogged},
		},
		DomainTax: {
			RuleID: "p1a.auditor.tax", Purposes: []string{PurposeAuditReview}, Effect: EffectRedacted, Obligations: []string{obligationEvidenceLogged},
		},
		DomainBank: {
			RuleID: "p1a.auditor.bank", Purposes: []string{PurposeAuditReview}, Effect: EffectRedacted, Obligations: []string{obligationEvidenceLogged},
		},
		DomainPerformance: {
			RuleID: "p1a.auditor.performance", Purposes: []string{PurposeAuditReview}, Effect: EffectRedacted, Obligations: []string{obligationEvidenceLogged},
		},
		DomainMedical: {
			RuleID: "p1a.auditor.medical", Purposes: []string{PurposeAuditReview}, Effect: EffectRedacted, Obligations: []string{obligationEvidenceLogged},
		},
		DomainEmployeeRelations: {
			RuleID: "p1a.auditor.employee_relations", Purposes: []string{PurposeAuditReview}, Effect: EffectRedacted, Obligations: []string{obligationEvidenceLogged},
		},
		DomainImmigration: {
			RuleID: "p1a.auditor.immigration", Purposes: []string{PurposeAuditReview}, Effect: EffectRedacted, Obligations: []string{obligationEvidenceLogged},
		},
	},
}

// administrativeRoleOrder is the subset of roles whose [AuthorizationScope]
// does not depend on a relationship fact: a comp admin, payroll manager or auditor's
// authority over a record comes from their role and the tenant/organization
// boundary already resolved by [ResolveTenantScope], not from being
// someone's manager or HR partner. It is a fixed-order slice, not a map, so
// that a principal holding more than one administrative role still resolves
// to a deterministic matched rule.
var administrativeRoleOrder = []RoleID{RoleCompAdmin, RolePayrollManager, RoleAuditor, RoleFinancePartner}

// ErrInvalidPolicyInput is returned when a caller-supplied argument to one of
// this package's resolvers is malformed enough that no decision, not even a
// deny, can be computed from it. It is distinct from an authorization denial:
// a caller receiving this error has a programming bug, not a real subject to
// evaluate.
var ErrInvalidPolicyInput = errors.New("authz: invalid policy input")

// dedupeSorted returns a sorted copy of in with empty strings and duplicates
// removed, so that rule-ID and obligation lists in a [Decision] are
// deterministic regardless of evaluation order.
func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return slices.Compact(out)
}
