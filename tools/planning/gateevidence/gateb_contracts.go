// Gate B decision contract: WEDGE-015 executes the limited-write
// authority decision over the NEXT-009 pre-write amendment.
//
// An unresolved pilot dependency (IMPLIED or MISSING), a failed
// scenario, a stale security approval, an unmet RPO/RTO or an unowned
// repair blocks with REMEDIATE or STOP and zero write authority. A
// complete receipt yields exactly one signed PROCEED_LIMITED decision
// granting only the exact tenant/domain/field/capability/effective
// scope with expiry and rollback/bypass conditions. Widening scope
// always requires a new decision: the record is immutable and any scope
// edit invalidates its digest.
package gateevidence

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// GateBContractVersion is the version of the Gate B decision contract.
const GateBContractVersion = 1

// GateBDependencyState is the closed pilot-dependency resolution.
type GateBDependencyState string

// The pilot-dependency states.
const (
	GateBDependencyResolved GateBDependencyState = "RESOLVED"
	GateBDependencyImplied  GateBDependencyState = "IMPLIED"
	GateBDependencyMissing  GateBDependencyState = "MISSING"
)

// GateBDependency is one named pilot dependency.
type GateBDependency struct {
	Name  string               `json:"name"`
	State GateBDependencyState `json:"state"`
}

// GateBScenario is one named pilot scenario outcome.
type GateBScenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

// GateBSecurityApproval is the bounded security approval.
type GateBSecurityApproval struct {
	Approver   string        `json:"approver"`
	ApprovedAt time.Time     `json:"approved_at"`
	ValidFor   time.Duration `json:"valid_for"`
}

// GateBObjective is one met/unmet RPO or RTO objective.
type GateBObjective struct {
	Met bool `json:"met"`
}

// GateBPilotReceipt is the complete Gate B evidence envelope.
type GateBPilotReceipt struct {
	SchemaVersion  int                   `json:"schema_version"`
	ManifestTodoID string                `json:"manifest_todo_id"`
	Dependencies   []GateBDependency     `json:"dependencies"`
	Scenarios      []GateBScenario       `json:"scenarios"`
	Security       GateBSecurityApproval `json:"security"`
	RPO            GateBObjective        `json:"rpo"`
	RTO            GateBObjective        `json:"rto"`
	RepairOwner    string                `json:"repair_owner"`
}

// GateBOutcome is the closed Gate B decision.
type GateBOutcome string

// The Gate B outcomes.
const (
	GateBProceedLimited GateBOutcome = "PROCEED_LIMITED"
	GateBRemediate      GateBOutcome = "REMEDIATE"
	GateBStop           GateBOutcome = "STOP"
)

// GateBScope is the exact limited-write grant boundary.
type GateBScope struct {
	Tenant     string    `json:"tenant"`
	Domain     string    `json:"domain"`
	Fields     []string  `json:"fields"`
	Capability string    `json:"capability"`
	Effective  string    `json:"effective"`
	Expiry     time.Time `json:"expiry"`
}

// GateBDecisionRecord is the immutable decision-shaped WEDGE-015
// record. WriteAuthority is true only for a signed PROCEED_LIMITED
// with a complete scope, expiry and rollback/bypass conditions.
type GateBDecisionRecord struct {
	SchemaVersion     int          `json:"schema_version"`
	Gate              string       `json:"gate"`
	Decision          GateBOutcome `json:"decision"`
	EvidenceDigest    string       `json:"evidence_digest"`
	SnapshotDigest    string       `json:"snapshot_digest"`
	Scope             GateBScope   `json:"scope"`
	RollbackCondition string       `json:"rollback_condition"`
	BypassCondition   string       `json:"bypass_condition"`
	Signer            string       `json:"signer"`
	SignedAt          string       `json:"signed_at"`
	WriteAuthority    bool         `json:"write_authority"`
	Signature         *Signature   `json:"signature,omitempty"`
	Blockers          []string     `json:"blockers,omitempty"`
}

func (d GateBDecisionRecord) unsignedDigest() string {
	copy := d
	copy.Signature = nil
	return digestValue(copy)
}

// Digest re-derives the record digest rather than trusting the carrier.
func (d GateBDecisionRecord) Digest() string { return d.unsignedDigest() }

// EvaluateGateBDecision turns one pilot receipt into a decision. Any
// IMPLIED or MISSING dependency, failed scenario, stale approval,
// unmet objective or unowned repair remediates with zero authority.
func EvaluateGateBDecision(receipt GateBPilotReceipt, now time.Time) GateBDecisionRecord {
	record := GateBDecisionRecord{
		SchemaVersion: GateBContractVersion, Gate: "GATE_B",
		Decision:       GateBProceedLimited,
		EvidenceDigest: digestValue(receipt),
		SnapshotDigest: digestValue(receipt),
		WriteAuthority: true,
		Scope: GateBScope{
			Tenant: "harborcare-demo", Domain: "BI.PEOPLE",
			Fields: []string{"job.level"}, Capability: "promotion.execute",
			Effective: "2026-10-07", Expiry: now.Add(7 * 24 * time.Hour),
		},
		RollbackCondition: "restore ledger head sha256:head-9 on any failed scenario",
		BypassCondition:   "none: bypass requires a new decision",
	}
	block := func(blocker string) {
		record.Decision = GateBRemediate
		record.WriteAuthority = false
		record.Blockers = append(record.Blockers, blocker)
	}
	if len(receipt.Dependencies) == 0 {
		block("DEPENDENCY_MISSING: no pilot dependencies declared")
	}
	for _, dependency := range receipt.Dependencies {
		if dependency.State != GateBDependencyResolved {
			block("DEPENDENCY_" + string(dependency.State) + ":" + dependency.Name)
		}
	}
	if len(receipt.Scenarios) == 0 {
		block("SCENARIO_MISSING: no pilot scenarios declared")
	}
	for _, scenario := range receipt.Scenarios {
		if !scenario.Passed {
			block("SCENARIO_FAILED:" + scenario.Name)
		}
	}
	if strings.TrimSpace(receipt.Security.Approver) == "" ||
		receipt.Security.ValidFor <= 0 ||
		now.After(receipt.Security.ApprovedAt.Add(receipt.Security.ValidFor)) {
		block("SECURITY_APPROVAL_STALE: security approval missing or expired")
	}
	if !receipt.RPO.Met {
		block("RPO_UNMET: recovery point objective unmet")
	}
	if !receipt.RTO.Met {
		block("RTO_UNMET: recovery time objective unmet")
	}
	if strings.TrimSpace(receipt.RepairOwner) == "" {
		block("REPAIR_UNOWNED: repair path has no owner")
	}
	if record.Decision == GateBProceedLimited {
		record.Scope.Fields = append([]string(nil), record.Scope.Fields...)
		sort.Strings(record.Scope.Fields)
	} else {
		record.WriteAuthority = false
	}
	sort.Strings(record.Blockers)
	return record
}

// ValidateGateBDecision checks one recorded decision. Scope widening is
// a new decision: any PROCEED_LIMITED with an incomplete scope, past
// expiry, missing conditions or missing signature is invalid.
func ValidateGateBDecision(d GateBDecisionRecord, now time.Time) []ContractViolation {
	var out []ContractViolation
	if d.SchemaVersion == 0 || d.Gate != "GATE_B" {
		out = append(out, violation("DECISION_INCOMPLETE", "gate", "decision must be a Gate B record"))
	}
	switch d.Decision {
	case GateBProceedLimited, GateBRemediate, GateBStop:
	default:
		out = append(out, violation("DECISION_INVALID", "decision", "decision is outside PROCEED_LIMITED, REMEDIATE, STOP"))
	}
	if d.Decision == GateBProceedLimited {
		scope := d.Scope
		if strings.TrimSpace(scope.Tenant) == "" || strings.TrimSpace(scope.Domain) == "" ||
			len(scope.Fields) == 0 || strings.TrimSpace(scope.Capability) == "" ||
			strings.TrimSpace(scope.Effective) == "" || scope.Expiry.IsZero() {
			out = append(out, violation("SCOPE_INCOMPLETE", "scope", "limited grant must name tenant, domain, fields, capability, effective window and expiry"))
		}
		if !scope.Expiry.IsZero() && !scope.Expiry.After(now) {
			out = append(out, violation("SCOPE_EXPIRED", "scope", "limited grant expiry is past"))
		}
		if strings.TrimSpace(d.RollbackCondition) == "" || strings.TrimSpace(d.BypassCondition) == "" {
			out = append(out, violation("CONDITION_MISSING", "conditions", "rollback and bypass conditions are required"))
		}
		if !d.WriteAuthority {
			out = append(out, violation("AUTHORITY_MISSING", "write_authority", "a limited-proceed decision carries write authority"))
		}
	} else if d.WriteAuthority {
		out = append(out, violation("WRITE_AUTHORITY_FORBIDDEN", "write_authority", "only PROCEED_LIMITED carries write authority"))
	}
	for _, fieldValue := range []struct{ field, value string }{
		{"evidence_digest", d.EvidenceDigest}, {"snapshot_digest", d.SnapshotDigest},
		{"signer", d.Signer}, {"signed_at", d.SignedAt},
	} {
		if strings.TrimSpace(fieldValue.value) == "" {
			out = append(out, violation("DECISION_INCOMPLETE", fieldValue.field, "missing"))
		}
	}
	if d.Signature == nil {
		out = append(out, violation("DECISION_UNSIGNED", "signature", "decision must carry a signature when recorded"))
	}
	return out
}

// SignGateBDecision signs a decision record without changing its scope.
func SignGateBDecision(d GateBDecisionRecord, signer, signedAt string, priv ed25519.PrivateKey) (GateBDecisionRecord, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return GateBDecisionRecord{}, fmt.Errorf("private key has %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	d.Signer, d.SignedAt = signer, signedAt
	digest := d.unsignedDigest()
	signed, err := SignDigest(priv, digest)
	if err != nil {
		return GateBDecisionRecord{}, err
	}
	d.Signature = &Signature{Algorithm: "ed25519", PublicKey: hex.EncodeToString(priv.Public().(ed25519.PublicKey)), Value: signed}
	return d, nil
}

// VerifyGateBDecision verifies the signature and the authority shape.
func VerifyGateBDecision(d GateBDecisionRecord) (bool, error) {
	if d.Signature == nil {
		return false, fmt.Errorf("decision has no signature")
	}
	if d.Decision != GateBProceedLimited && d.WriteAuthority {
		return false, fmt.Errorf("only a limited-proceed decision carries write authority")
	}
	ok, err := VerifyDigestSignature(d.Signature.PublicKey, d.unsignedDigest(), d.Signature.Value)
	if err != nil {
		return false, err
	}
	return ok, nil
}
