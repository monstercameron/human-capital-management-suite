package privacy

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/hipaa"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// This file implements planning/todos.md PRIV-009: a reproducible
// financial-breach and notification-decision matrix.
//
// An incident workflow has three stages, and the third never runs before
// the first two:
//
//  1. The [BreachIncident] records discovery time, affected-data scope,
//     per-jurisdiction population, the jurisdiction-matrix version it is
//     evaluated against, and the instant its evidence was sealed. Evidence
//     must be sealed before any remediation that would alter it: a
//     remediated-at instant preceding the seal blocks the decision.
//  2. The [NotificationMatrix] is the versioned, complete duty table: one
//     rule per (duty, data class) pair naming the notify threshold, the
//     notice deadline, and the citable authority. An incomplete or
//     contradictory matrix never constructs.
//  3. [DecideNotifications] resolves the consumer count and one
//     notify/no-notify [DutyDecision] per duty -- per jurisdiction for
//     state breach law -- bound into a digest-pinned [NotificationDecision]
//     certificate.
//
// A legal hold ([BreachIncident.HoldRef], the RECORDS-HOLD-001 wiring)
// travels on the incident into the certificate: held evidence is preserved
// regardless of remediation, and the decision names the hold that requires
// it.

// ErrBreachBlocked is returned when an incident, matrix or decision cannot
// prove a complete, reproducible notification evaluation.
var ErrBreachBlocked = errors.New("privacy: breach notification blocked")

const breachEvidencePrefix = "ev:privacy:breach:"

// BreachDataClass is the closed vocabulary of data classes a breach
// incident may scope. Payment, bank-detail and FTI data -- PRIV-009's RED
// clause -- are all declared; anything else fails closed rather than
// passing through a matrix with no rule for it.
type BreachDataClass string

// The closed breach-data-class vocabulary.
const (
	BreachPaymentCard BreachDataClass = "PAYMENT_CARD"
	BreachBankDetail  BreachDataClass = "BANK_DETAIL"
	BreachFTI         BreachDataClass = "FTI"
	BreachCredentials BreachDataClass = "CREDENTIALS"
	BreachHealthInfo  BreachDataClass = "HEALTH_INFO"
	BreachContactInfo BreachDataClass = "CONTACT_INFO"
)

var allBreachDataClasses = []BreachDataClass{
	BreachPaymentCard,
	BreachBankDetail,
	BreachFTI,
	BreachCredentials,
	BreachHealthInfo,
	BreachContactInfo,
}

// AllBreachDataClasses returns a fresh copy of the closed vocabulary.
func AllBreachDataClasses() []BreachDataClass { return slices.Clone(allBreachDataClasses) }

func (c BreachDataClass) valid() bool { return slices.Contains(allBreachDataClasses, c) }

func containsBreachClass(classes []BreachDataClass, class BreachDataClass) bool {
	return slices.Contains(classes, class)
}

// Duty is the closed vocabulary of notice duties PRIV-009's GREEN clause
// names: federal, state breach law, GLBA, and tenant contract.
type Duty string

// The closed duty vocabulary.
const (
	DutyFederal        Duty = "FEDERAL"
	DutyStateBreachLaw Duty = "STATE_BREACH_LAW"
	DutyGLBA           Duty = "GLBA"
	DutyTenantContract Duty = "TENANT_CONTRACT"
	// DutyHIPAA summarizes an applicable HIPAA breach notification evaluation.
	// Its specific recipients and clocks are carried in HIPAAClocks.
	DutyHIPAA Duty = "HIPAA"
)

var allDuties = []Duty{DutyFederal, DutyStateBreachLaw, DutyGLBA, DutyTenantContract}

// AllDuties returns a fresh copy of the closed duty vocabulary.
func AllDuties() []Duty { return slices.Clone(allDuties) }

func (d Duty) valid() bool { return slices.Contains(allDuties, d) }

// NotifyDecision is the per-duty answer.
type NotifyDecision string

// The closed notify-decision vocabulary.
const (
	DecisionNotify   NotifyDecision = "NOTIFY"
	DecisionNoNotify NotifyDecision = "NO_NOTIFY"
)

// JurisdictionPopulation is the affected-consumer count for one
// jurisdiction. Counts are exact recorded figures, never estimates: the
// consumer-count calculation sums them, and state-law decisions read each
// jurisdiction's own count against the matrix threshold.
type JurisdictionPopulation struct {
	Jurisdiction legal.Jurisdiction `json:"jurisdiction"`
	Consumers    int64              `json:"consumers"`
}

// BreachIncident is one recorded incident touching financial or otherwise
// sensitive data.
type BreachIncident struct {
	ID              string                   `json:"id"`
	Tenant          values.TenantId          `json:"tenant"`
	DiscoveredAt    values.Instant           `json:"discovered_at"`
	AffectedClasses []BreachDataClass        `json:"affected_classes"`
	Populations     []JurisdictionPopulation `json:"populations"`
	// MatrixVersion is the jurisdiction-matrix version this incident is
	// evaluated against. [DecideNotifications] refuses a matrix whose
	// version differs: a decision reproducible under another matrix is not
	// this incident's decision.
	MatrixVersion string `json:"matrix_version"`
	// EvidenceSealedAt is when the incident evidence was preserved. It
	// must be set, must not precede discovery, and must not follow any
	// remediation: evidence is preserved before remediation alters it.
	EvidenceSealedAt values.Instant `json:"evidence_sealed_at"`
	// RemediatedAt is zero until remediation begins. Once set, it must not
	// precede the seal.
	RemediatedAt values.Instant `json:"remediated_at,omitempty"`
	// HoldRef names the RECORDS-HOLD-001 legal hold preserving this
	// incident's evidence, when one grips it. It is carried into the
	// decision certificate verbatim.
	HoldRef    string `json:"hold_ref,omitempty"`
	EvidenceID string `json:"evidence_id"`
	// HIPAAApplicability records confirmed, assumed, or explicit non-applicability.
	// Health-info incidents with unresolved or absent scope fail closed.
	HIPAAApplicability hipaa.Applicability `json:"hipaa_applicability,omitempty"`
	// HIPAAEntityRole identifies which HIPAA notice path applies.
	HIPAAEntityRole hipaa.EntityRole `json:"hipaa_entity_role,omitempty"`
	// HIPAAMatrixVersion pins the companion HIPAA clock matrix.
	HIPAAMatrixVersion string `json:"hipaa_matrix_version,omitempty"`
}

// Digest is the canonical content digest of this incident record.
func (in BreachIncident) Digest() string {
	disSec, disNsec := in.DiscoveredAt.Unix()
	sealSec, sealNsec := in.EvidenceSealedAt.Unix()
	remSec, remNsec := in.RemediatedAt.Unix()
	dst := appendFields(nil,
		"id", in.ID,
		"tenant", in.Tenant.String(),
		"discovered_at_sec", itoa(disSec),
		"discovered_at_nsec", itoa(int64(disNsec)),
		"matrix_version", in.MatrixVersion,
		"sealed_at_sec", itoa(sealSec),
		"sealed_at_nsec", itoa(int64(sealNsec)),
		"remediated_at_sec", itoa(remSec),
		"remediated_at_nsec", itoa(int64(remNsec)),
		"hold_ref", in.HoldRef,
	)
	if in.HIPAAApplicability != "" || in.HIPAAEntityRole != "" || in.HIPAAMatrixVersion != "" {
		dst = appendFields(dst,
			"hipaa_applicability", string(in.HIPAAApplicability),
			"hipaa_entity_role", string(in.HIPAAEntityRole),
			"hipaa_matrix_version", in.HIPAAMatrixVersion,
		)
	}
	classes := slices.Clone(in.AffectedClasses)
	slices.Sort(classes)
	for _, c := range classes {
		dst = appendField(dst, "class", string(c))
	}
	pops := slices.Clone(in.Populations)
	slices.SortFunc(pops, func(a, b JurisdictionPopulation) int {
		return strings.Compare(a.Jurisdiction.String(), b.Jurisdiction.String())
	})
	for _, p := range pops {
		dst = appendFields(dst,
			"population_jurisdiction", p.Jurisdiction.String(),
			"population_consumers", itoa(p.Consumers),
		)
	}
	return digestHex(dst)
}

// validate checks the incident record structurally: every required field,
// every token from its closed vocabulary, positive per-jurisdiction counts
// with no duplicate jurisdiction, and the discovery/seal/remediation
// ordering that proves evidence was preserved before alteration.
func (in BreachIncident) validate() error {
	if strings.TrimSpace(in.ID) == "" {
		return fmt.Errorf("%w: incident has no id", ErrBreachBlocked)
	}
	if err := in.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrBreachBlocked, err)
	}
	if !in.DiscoveredAt.IsSet() {
		return fmt.Errorf("%w: incident %q records no discovery time", ErrBreachBlocked, in.ID)
	}
	if len(in.AffectedClasses) == 0 {
		return fmt.Errorf("%w: incident %q scopes no affected data", ErrBreachBlocked, in.ID)
	}
	for _, c := range in.AffectedClasses {
		if !c.valid() {
			return fmt.Errorf("%w: incident %q names undeclared data class %q", ErrBreachBlocked, in.ID, string(c))
		}
	}
	if containsBreachClass(in.AffectedClasses, BreachHealthInfo) {
		switch in.HIPAAApplicability {
		case hipaa.ApplicabilityConfirmed, hipaa.ApplicabilityAssumed:
			if !in.HIPAAEntityRole.Valid() || strings.TrimSpace(in.HIPAAMatrixVersion) == "" {
				return fmt.Errorf("%w: applicable health-info incident must pin HIPAA role and clock-matrix version", ErrBreachBlocked)
			}
		case hipaa.ApplicabilityNotApplicable:
			if in.HIPAAEntityRole != "" || in.HIPAAMatrixVersion != "" {
				return fmt.Errorf("%w: non-applicable health-info incident carries HIPAA role or matrix version", ErrBreachBlocked)
			}
		default:
			return fmt.Errorf("%w: health-info incident has unresolved HIPAA applicability", ErrBreachBlocked)
		}
	} else if in.HIPAAApplicability != "" || in.HIPAAEntityRole != "" || in.HIPAAMatrixVersion != "" {
		return fmt.Errorf("%w: HIPAA breach scope is present without HEALTH_INFO data", ErrBreachBlocked)
	}
	if len(in.Populations) == 0 {
		return fmt.Errorf("%w: incident %q records no affected population", ErrBreachBlocked, in.ID)
	}
	seen := make(map[string]struct{}, len(in.Populations))
	for _, p := range in.Populations {
		if err := p.Jurisdiction.Validate(); err != nil {
			return fmt.Errorf("%w: incident %q: %v", ErrBreachBlocked, in.ID, err)
		}
		key := p.Jurisdiction.String()
		if _, dup := seen[key]; dup {
			return fmt.Errorf("%w: incident %q duplicates jurisdiction %q", ErrBreachBlocked, in.ID, key)
		}
		seen[key] = struct{}{}
		if p.Consumers <= 0 {
			return fmt.Errorf("%w: incident %q jurisdiction %q has no positive consumer count", ErrBreachBlocked, in.ID, key)
		}
	}
	if strings.TrimSpace(in.MatrixVersion) == "" {
		return fmt.Errorf("%w: incident %q names no jurisdiction-matrix version", ErrBreachBlocked, in.ID)
	}
	if !in.EvidenceSealedAt.IsSet() {
		return fmt.Errorf("%w: incident %q sealed no evidence before deciding", ErrBreachBlocked, in.ID)
	}
	if in.EvidenceSealedAt.Before(in.DiscoveredAt) {
		return fmt.Errorf("%w: incident %q evidence sealed before discovery", ErrBreachBlocked, in.ID)
	}
	if in.RemediatedAt.IsSet() && in.RemediatedAt.Before(in.EvidenceSealedAt) {
		return fmt.Errorf("%w: incident %q remediated before its evidence was sealed", ErrBreachBlocked, in.ID)
	}
	if in.EvidenceID != "" && in.EvidenceID != breachEvidencePrefix+"incident:"+in.Digest() {
		return fmt.Errorf("%w: incident %q evidence id does not match its own digest", ErrBreachBlocked, in.ID)
	}
	return nil
}

// consumerCount sums the per-jurisdiction populations.
func (in BreachIncident) consumerCount() int64 {
	var total int64
	for _, p := range in.Populations {
		total += p.Consumers
	}
	return total
}

// MatrixRule is one cell of the jurisdiction matrix: when an incident
// affects at least MinConsumers consumers of Class, Duty notifies within
// DeadlineHours under Authority.
type MatrixRule struct {
	Duty          Duty            `json:"duty"`
	Class         BreachDataClass `json:"class"`
	MinConsumers  int64           `json:"min_consumers"`
	DeadlineHours int64           `json:"deadline_hours"`
	// Authority cites the ground: a statute section for breach law and
	// GLBA, the contract reference for tenant-contract duty.
	Authority string `json:"authority"`
}

// NotificationMatrix is the complete, versioned duty table one incident is
// decided against. It is built only by [NewNotificationMatrix], which
// refuses an incomplete table: every (duty, class) pair must have exactly
// one rule, so no duty/class combination can pass undecided.
type NotificationMatrix struct {
	Version string                       `json:"version"`
	Rules   []MatrixRule                 `json:"rules"`
	HIPAA   *hipaa.BreachMatrixExtension `json:"hipaa,omitempty"`
}

func matrixKey(duty Duty, class BreachDataClass) string {
	return string(duty) + "|" + string(class)
}

// NewNotificationMatrix validates a complete duty table: non-empty version,
// every rule well-formed (declared duty and class, a positive notify
// threshold, a positive notice deadline, a cited authority), no duplicate
// (duty, class) cell, and no missing cell. A matrix that cannot show every
// duty was evaluated for every data class never constructs.
func NewNotificationMatrix(version string, rules []MatrixRule) (NotificationMatrix, error) {
	if strings.TrimSpace(version) == "" {
		return NotificationMatrix{}, fmt.Errorf("%w: matrix has no version", ErrBreachBlocked)
	}
	byKey := make(map[string]MatrixRule, len(rules))
	for i, r := range rules {
		if !r.Duty.valid() {
			return NotificationMatrix{}, fmt.Errorf("%w: rule %d names undeclared duty %q", ErrBreachBlocked, i, string(r.Duty))
		}
		if !r.Class.valid() {
			return NotificationMatrix{}, fmt.Errorf("%w: rule %d names undeclared data class %q", ErrBreachBlocked, i, string(r.Class))
		}
		if r.MinConsumers < 1 {
			return NotificationMatrix{}, fmt.Errorf("%w: rule %d (%s/%s) has no positive notify threshold", ErrBreachBlocked, i, r.Duty, r.Class)
		}
		if r.DeadlineHours < 1 {
			return NotificationMatrix{}, fmt.Errorf("%w: rule %d (%s/%s) has no positive notice deadline", ErrBreachBlocked, i, r.Duty, r.Class)
		}
		if strings.TrimSpace(r.Authority) == "" {
			return NotificationMatrix{}, fmt.Errorf("%w: rule %d (%s/%s) cites no authority", ErrBreachBlocked, i, r.Duty, r.Class)
		}
		key := matrixKey(r.Duty, r.Class)
		if _, dup := byKey[key]; dup {
			return NotificationMatrix{}, fmt.Errorf("%w: duplicate rule for %s/%s", ErrBreachBlocked, r.Duty, r.Class)
		}
		byKey[key] = r
	}
	for _, duty := range allDuties {
		for _, class := range allBreachDataClasses {
			if _, ok := byKey[matrixKey(duty, class)]; !ok {
				return NotificationMatrix{}, fmt.Errorf("%w: matrix has no rule for %s/%s: duty unevaluated", ErrBreachBlocked, duty, class)
			}
		}
	}
	ordered := slices.Clone(rules)
	slices.SortFunc(ordered, func(a, b MatrixRule) int {
		return strings.Compare(matrixKey(a.Duty, a.Class), matrixKey(b.Duty, b.Class))
	})
	return NotificationMatrix{Version: version, Rules: ordered}, nil
}

// rule returns the cell for (duty, class). It is only called on matrices
// [NewNotificationMatrix] completed, so a missing cell is a caller bug.
func (m NotificationMatrix) rule(duty Duty, class BreachDataClass) MatrixRule {
	for _, r := range m.Rules {
		if r.Duty == duty && r.Class == class {
			return r
		}
	}
	panic(fmt.Sprintf("privacy: matrix %q has no rule for %s/%s", m.Version, duty, class))
}

// DutyDecision is the reproducible answer for one duty -- one jurisdiction
// for state breach law, the whole incident population for the other three.
type DutyDecision struct {
	Duty         Duty           `json:"duty"`
	Jurisdiction string         `json:"jurisdiction,omitempty"`
	Decision     NotifyDecision `json:"decision"`
	Consumers    int64          `json:"consumers"`
	// Threshold is the lowest matrix threshold any affected class met
	// (for NOTIFY) or the lowest threshold none met (for NO_NOTIFY): the
	// exact figure the decision turned on.
	Threshold int64 `json:"threshold"`
	// DeadlineHours and Authority come from the triggering rule for
	// NOTIFY; for NO_NOTIFY the deadline is zero and the authority cites
	// the evaluated rule set's version instead of a notice ground.
	DeadlineHours int64  `json:"deadline_hours"`
	Authority     string `json:"authority"`
	Reason        string `json:"reason"`
}

// NotificationDecision is the complete, digest-bound certificate for one
// incident: consumer count plus one decision per duty.
type NotificationDecision struct {
	IncidentID         string               `json:"incident_id"`
	IncidentDigest     string               `json:"incident_digest"`
	Tenant             values.TenantId      `json:"tenant"`
	MatrixVersion      string               `json:"matrix_version"`
	ConsumerCount      int64                `json:"consumer_count"`
	Decisions          []DutyDecision       `json:"decisions"`
	HoldRef            string               `json:"hold_ref,omitempty"`
	DecidedAt          values.Instant       `json:"decided_at"`
	EvidenceID         string               `json:"evidence_id"`
	HIPAAApplicability hipaa.Applicability  `json:"hipaa_applicability,omitempty"`
	HIPAAEntityRole    hipaa.EntityRole     `json:"hipaa_entity_role,omitempty"`
	HIPAAMatrixVersion string               `json:"hipaa_matrix_version,omitempty"`
	HIPAAClocks        []HIPAAClockDecision `json:"hipaa_clocks,omitempty"`
}

// Digest is the canonical content digest of this certificate.
func (d NotificationDecision) Digest() string {
	sec, nsec := d.DecidedAt.Unix()
	dst := appendFields(nil,
		"incident_id", d.IncidentID,
		"incident_digest", d.IncidentDigest,
		"tenant", d.Tenant.String(),
		"matrix_version", d.MatrixVersion,
		"consumer_count", itoa(d.ConsumerCount),
		"hold_ref", d.HoldRef,
		"decided_at_sec", itoa(sec),
		"decided_at_nsec", itoa(int64(nsec)),
	)
	if d.HIPAAMatrixVersion != "" || d.HIPAAApplicability != "" || d.HIPAAEntityRole != "" || len(d.HIPAAClocks) > 0 {
		dst = appendFields(dst,
			"hipaa_applicability", string(d.HIPAAApplicability),
			"hipaa_entity_role", string(d.HIPAAEntityRole),
			"hipaa_matrix_version", d.HIPAAMatrixVersion,
		)
	}
	ordered := slices.Clone(d.Decisions)
	slices.SortFunc(ordered, func(a, b DutyDecision) int {
		if a.Duty != b.Duty {
			return strings.Compare(string(a.Duty), string(b.Duty))
		}
		return strings.Compare(a.Jurisdiction, b.Jurisdiction)
	})
	for _, dec := range ordered {
		dst = appendFields(dst,
			"duty", string(dec.Duty),
			"jurisdiction", dec.Jurisdiction,
			"decision", string(dec.Decision),
			"consumers", itoa(dec.Consumers),
			"threshold", itoa(dec.Threshold),
			"deadline_hours", itoa(dec.DeadlineHours),
			"authority", dec.Authority,
			"reason", dec.Reason,
		)
	}
	if d.HIPAAMatrixVersion != "" || len(d.HIPAAClocks) > 0 {
		clocks := slices.Clone(d.HIPAAClocks)
		slices.SortFunc(clocks, compareHIPAAClockDecision)
		for _, clock := range clocks {
			dst = appendFields(dst,
				"hipaa_recipient", string(clock.Recipient),
				"hipaa_jurisdiction", clock.Jurisdiction,
				"hipaa_decision", string(clock.Decision),
				"hipaa_consumers", itoa(clock.Consumers),
				"hipaa_threshold", itoa(clock.Threshold),
				"hipaa_deadline_days", itoa(int64(clock.DeadlineDays)),
				"hipaa_deadline_from", clock.DeadlineFrom,
				"hipaa_authority", clock.Authority,
				"hipaa_reason", clock.Reason,
			)
		}
	}
	return digestHex(dst)
}

// Validate reports whether d is internally consistent: bound to an
// incident, carrying every duty (state law once per decided jurisdiction),
// well-formed decisions, and an EvidenceID matching its own digest.
func (d NotificationDecision) Validate() error {
	if strings.TrimSpace(d.IncidentID) == "" {
		return fmt.Errorf("%w: decision names no incident", ErrBreachBlocked)
	}
	if d.IncidentDigest == "" {
		return fmt.Errorf("%w: decision is not bound to an incident digest", ErrBreachBlocked)
	}
	if err := d.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrBreachBlocked, err)
	}
	if strings.TrimSpace(d.MatrixVersion) == "" {
		return fmt.Errorf("%w: decision names no matrix version", ErrBreachBlocked)
	}
	if !d.DecidedAt.IsSet() {
		return fmt.Errorf("%w: decision has no decided_at", ErrBreachBlocked)
	}
	if len(d.Decisions) == 0 {
		return fmt.Errorf("%w: decision carries no duty decisions", ErrBreachBlocked)
	}
	seenGlobal := make(map[Duty]struct{})
	seenState := make(map[string]struct{})
	for i, dec := range d.Decisions {
		if !dec.Duty.valid() && dec.Duty != DutyHIPAA {
			return fmt.Errorf("%w: decision %d names undeclared duty %q", ErrBreachBlocked, i, string(dec.Duty))
		}
		if dec.Decision != DecisionNotify && dec.Decision != DecisionNoNotify {
			return fmt.Errorf("%w: decision %d carries undeclared answer %q", ErrBreachBlocked, i, string(dec.Decision))
		}
		if dec.Consumers < 1 {
			return fmt.Errorf("%w: decision %d (%s) has no consumer count", ErrBreachBlocked, i, dec.Duty)
		}
		if dec.Threshold < 1 {
			return fmt.Errorf("%w: decision %d (%s) has no threshold", ErrBreachBlocked, i, dec.Duty)
		}
		if strings.TrimSpace(dec.Reason) == "" {
			return fmt.Errorf("%w: decision %d (%s) carries no reason", ErrBreachBlocked, i, dec.Duty)
		}
		if dec.Decision == DecisionNotify {
			if dec.DeadlineHours < 1 || strings.TrimSpace(dec.Authority) == "" {
				return fmt.Errorf("%w: decision %d (%s) notifies with no deadline or authority", ErrBreachBlocked, i, dec.Duty)
			}
		}
		if dec.Duty == DutyStateBreachLaw {
			if dec.Jurisdiction == "" {
				return fmt.Errorf("%w: state-law decision %d names no jurisdiction", ErrBreachBlocked, i)
			}
			if _, dup := seenState[dec.Jurisdiction]; dup {
				return fmt.Errorf("%w: duplicate state-law decision for %q", ErrBreachBlocked, dec.Jurisdiction)
			}
			seenState[dec.Jurisdiction] = struct{}{}
		} else {
			if dec.Jurisdiction != "" {
				return fmt.Errorf("%w: non-state decision %d (%s) names a jurisdiction", ErrBreachBlocked, i, dec.Duty)
			}
			if _, dup := seenGlobal[dec.Duty]; dup {
				return fmt.Errorf("%w: duplicate decision for duty %s", ErrBreachBlocked, dec.Duty)
			}
			seenGlobal[dec.Duty] = struct{}{}
		}
		if i > 0 && decisionLess(d.Decisions[i], d.Decisions[i-1]) {
			return fmt.Errorf("%w: duty decisions are not in canonical order", ErrBreachBlocked)
		}
	}
	for _, duty := range []Duty{DutyFederal, DutyGLBA, DutyTenantContract} {
		if _, ok := seenGlobal[duty]; !ok {
			return fmt.Errorf("%w: duty %s was never decided", ErrBreachBlocked, duty)
		}
	}
	if _, hasHIPAADuty := seenGlobal[DutyHIPAA]; hasHIPAADuty {
		if err := validateHIPAAClockDecisions(d.HIPAAMatrixVersion, d.HIPAAClocks); err != nil {
			return err
		}
		if d.HIPAAApplicability != hipaa.ApplicabilityConfirmed && d.HIPAAApplicability != hipaa.ApplicabilityAssumed {
			return fmt.Errorf("%w: HIPAA duty has no applicable coverage decision", ErrBreachBlocked)
		}
		if !d.HIPAAEntityRole.Valid() {
			return fmt.Errorf("%w: HIPAA duty has no entity role", ErrBreachBlocked)
		}
	} else if d.HIPAAMatrixVersion != "" || d.HIPAAEntityRole != "" || len(d.HIPAAClocks) > 0 || d.HIPAAApplicability != "" && d.HIPAAApplicability != hipaa.ApplicabilityNotApplicable {
		return fmt.Errorf("%w: HIPAA certificate evidence has no HIPAA duty", ErrBreachBlocked)
	}
	if len(seenState) == 0 {
		return fmt.Errorf("%w: state breach law was never decided for any jurisdiction", ErrBreachBlocked)
	}
	if d.EvidenceID != breachEvidencePrefix+d.Digest() {
		return fmt.Errorf("%w: decision evidence id does not match its own digest", ErrBreachBlocked)
	}
	return nil
}

func decisionLess(a, b DutyDecision) bool {
	if a.Duty != b.Duty {
		return a.Duty < b.Duty
	}
	return a.Jurisdiction < b.Jurisdiction
}

// DecideNotifications evaluates incident against matrix at `at` and returns
// the digest-bound certificate. It refuses an invalid incident, a matrix
// whose version is not the incident's recorded one, and a decision instant
// preceding the evidence seal: the decision is always read off preserved
// evidence, never off a live, half-remediated system.
func DecideNotifications(incident BreachIncident, matrix NotificationMatrix, at values.Instant) (NotificationDecision, error) {
	if err := incident.validate(); err != nil {
		return NotificationDecision{}, err
	}
	if strings.TrimSpace(matrix.Version) == "" || len(matrix.Rules) == 0 {
		return NotificationDecision{}, fmt.Errorf("%w: matrix is empty", ErrBreachBlocked)
	}
	if matrix.Version != incident.MatrixVersion {
		return NotificationDecision{}, fmt.Errorf("%w: incident records matrix %q but was evaluated against %q", ErrBreachBlocked, incident.MatrixVersion, matrix.Version)
	}
	if matrix.HIPAA != nil {
		if err := matrix.HIPAA.Validate(); err != nil || matrix.HIPAA.PRIV009MatrixVersion != matrix.Version {
			return NotificationDecision{}, fmt.Errorf("%w: attached HIPAA clock matrix is invalid or detached", ErrBreachBlocked)
		}
	}
	if !at.IsSet() {
		return NotificationDecision{}, fmt.Errorf("%w: no decision instant", ErrBreachBlocked)
	}
	if at.Before(incident.EvidenceSealedAt) {
		return NotificationDecision{}, fmt.Errorf("%w: decision precedes the evidence seal", ErrBreachBlocked)
	}

	decisions := make([]DutyDecision, 0, len(incident.Populations)+3)
	for _, duty := range []Duty{DutyFederal, DutyGLBA, DutyTenantContract} {
		decisions = append(decisions, decideGlobalDuty(duty, incident.AffectedClasses, incident.consumerCount(), matrix))
	}
	pops := slices.Clone(incident.Populations)
	slices.SortFunc(pops, func(a, b JurisdictionPopulation) int {
		return strings.Compare(a.Jurisdiction.String(), b.Jurisdiction.String())
	})
	for _, p := range pops {
		decisions = append(decisions, decideStateDuty(incident.AffectedClasses, p, matrix))
	}
	var hipaaClocks []HIPAAClockDecision
	var hipaaMatrixVersion string
	if containsBreachClass(incident.AffectedClasses, BreachHealthInfo) && incident.HIPAAApplicability != hipaa.ApplicabilityNotApplicable {
		if matrix.HIPAA == nil {
			return NotificationDecision{}, fmt.Errorf("%w: applicable HEALTH_INFO incident has no HIPAA clock extension", ErrBreachBlocked)
		}
		if incident.HIPAAMatrixVersion != matrix.HIPAA.Version {
			return NotificationDecision{}, fmt.Errorf("%w: incident HIPAA matrix %q does not match attached %q", ErrBreachBlocked, incident.HIPAAMatrixVersion, matrix.HIPAA.Version)
		}
		if incident.HIPAAApplicability != hipaa.ApplicabilityConfirmed && incident.HIPAAApplicability != hipaa.ApplicabilityAssumed {
			return NotificationDecision{}, fmt.Errorf("%w: HIPAA coverage is unresolved", ErrBreachBlocked)
		}
		hipaaMatrixVersion = matrix.HIPAA.Version
		hipaaClocks = decideHIPAAClocks(incident, *matrix.HIPAA)
		decisions = append(decisions, DutyDecision{
			Duty: DutyHIPAA, Decision: DecisionNotify, Consumers: incident.consumerCount(), Threshold: 1,
			DeadlineHours: 60 * 24, Authority: "45 CFR 164.404(b)",
			Reason: "HIPAA individual notice is required; recipient clocks are recorded in the HIPAA matrix extension",
		})
	}
	slices.SortFunc(decisions, func(a, b DutyDecision) int {
		if decisionLess(a, b) {
			return -1
		}
		if decisionLess(b, a) {
			return 1
		}
		return 0
	})

	decision := NotificationDecision{
		IncidentID: incident.ID, IncidentDigest: incident.Digest(),
		Tenant: incident.Tenant, MatrixVersion: matrix.Version,
		ConsumerCount: incident.consumerCount(), Decisions: decisions,
		HoldRef: incident.HoldRef, DecidedAt: at,
		HIPAAApplicability: incident.HIPAAApplicability, HIPAAEntityRole: incident.HIPAAEntityRole,
		HIPAAMatrixVersion: hipaaMatrixVersion, HIPAAClocks: hipaaClocks,
	}
	decision.EvidenceID = breachEvidencePrefix + decision.Digest()
	if err := decision.Validate(); err != nil {
		return NotificationDecision{}, err
	}
	return decision, nil
}

// decideGlobalDuty resolves one non-state duty over the whole incident
// population: the first affected class (in matrix order) whose threshold
// the population meets notifies; otherwise the duty records which lowest
// threshold it fell short of.
func decideGlobalDuty(duty Duty, classes []BreachDataClass, consumers int64, matrix NotificationMatrix) DutyDecision {
	ordered := slices.Clone(classes)
	slices.Sort(ordered)
	var lowest int64
	var lowestAuthority string
	for _, class := range ordered {
		rule := matrix.rule(duty, class)
		if lowest == 0 || rule.MinConsumers < lowest {
			lowest, lowestAuthority = rule.MinConsumers, rule.Authority
		}
		if consumers >= rule.MinConsumers {
			return DutyDecision{
				Duty: duty, Decision: DecisionNotify, Consumers: consumers,
				Threshold: rule.MinConsumers, DeadlineHours: rule.DeadlineHours,
				Authority: rule.Authority,
				Reason:    fmt.Sprintf("%s meets the %s threshold of %d (matrix %s)", class, duty, rule.MinConsumers, matrix.Version),
			}
		}
	}
	return DutyDecision{
		Duty: duty, Decision: DecisionNoNotify, Consumers: consumers,
		Threshold: lowest, Authority: lowestAuthority,
		Reason: fmt.Sprintf("no affected class meets its %s threshold (lowest %d; matrix %s)", duty, lowest, matrix.Version),
	}
}

// decideStateDuty resolves state breach law for one jurisdiction's own
// population: state thresholds judge state residents, never the national
// total.
func decideStateDuty(classes []BreachDataClass, pop JurisdictionPopulation, matrix NotificationMatrix) DutyDecision {
	dec := decideGlobalDuty(DutyStateBreachLaw, classes, pop.Consumers, matrix)
	dec.Jurisdiction = pop.Jurisdiction.String()
	return dec
}
