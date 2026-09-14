// Post-authority Gate B operating and continuation evidence:
// GATEB-EVID-001 compiles an immutable evidence manifest that maps every
// required acceptance bullet to its exact todo, test, fixture, command,
// result, evidence digest, owner, retention, expiry and sign-off.
//
// Verdict rules, in order:
//  1. The manifest must declare exactly the required acceptance bullets:
//     unknown or duplicate bullets are structural errors, never verdicts.
//  2. A criterion missing any binding (todo, test, fixture, command,
//     result, digest, owner, retention) is UNBOUND and blocks.
//  3. Stale, expired, failed or unsigned criteria block: GATE_BLOCKED.
//  4. A waiver excuses a failed criterion only when it names by, reason,
//     scope, control and a live expiry; anything less is void and blocks.
//     A waiver on a passing criterion is void. Waivers never pass: a
//     live complete waiver downgrades to CONDITIONAL_CONTINUE, named.
//  5. All bound, fresh, passing and signed clears CONTINUE.
//  6. Additional authority is granted only on a clean CONTINUE with a
//     requested scope; every other verdict grants zero authority.
//  7. Prior decisions accumulate append-only: each compile copies its
//     input priors and appends the new verdict bound to the report
//     digest, including blocked verdicts, so a block can never erase
//     the trail that led to it.
//
// Compile is pure: inputs are never mutated and nothing is persisted.
package gateb

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Version reports this package's vocabulary version.
const Version = 1

// Required acceptance bullets: the Gate B operating-evidence families
// from the execution plan, each retiring a named risk.
const (
	BulletTransactionCorrectness = "transaction-correctness"
	BulletAccessIsolation        = "access-isolation"
	BulletOperabilityRestore     = "operability-restore"
	BulletOperabilitySupport     = "operability-support"
	BulletOperabilityRelease     = "operability-release"
	BulletCustomerEvidence       = "customer-evidence"
	BulletPerformanceSoak        = "performance-soak"
	BulletAssurance              = "assurance"
	BulletPilotDecision          = "pilot-decision"
	BulletRetentionDisposition   = "retention-disposition"
)

// RequiredBullets is the closed acceptance set every manifest maps.
var RequiredBullets = []string{
	BulletTransactionCorrectness,
	BulletAccessIsolation,
	BulletOperabilityRestore,
	BulletOperabilitySupport,
	BulletOperabilityRelease,
	BulletCustomerEvidence,
	BulletPerformanceSoak,
	BulletAssurance,
	BulletPilotDecision,
	BulletRetentionDisposition,
}

// CriterionResult is the closed result vocabulary for one evidence item.
type CriterionResult string

// The results.
const (
	ResultPass CriterionResult = "PASS"
	ResultFail CriterionResult = "FAIL"
)

// CriterionStatus is the closed evaluated vocabulary.
type CriterionStatus string

// The evaluated states.
const (
	StatusPass       CriterionStatus = "PASS"
	StatusFail       CriterionStatus = "FAIL"
	StatusUnbound    CriterionStatus = "UNBOUND"
	StatusStale      CriterionStatus = "STALE"
	StatusExpired    CriterionStatus = "EXPIRED"
	StatusUnsigned   CriterionStatus = "UNSIGNED"
	StatusWaived     CriterionStatus = "WAIVED"
	StatusVoidWaiver CriterionStatus = "VOID_WAIVER"
)

// Verdict is the closed gate vocabulary.
type Verdict string

// The verdicts.
const (
	VerdictContinue            Verdict = "CONTINUE"
	VerdictConditionalContinue Verdict = "CONDITIONAL_CONTINUE"
	VerdictGateBlocked         Verdict = "GATE_BLOCKED"
)

// Waiver excuses a failed criterion only when it names who, why, the
// bounded scope, the compensating control and a live expiry.
type Waiver struct {
	By               string
	Reason           string
	Scope            string
	Control          string
	ExpiresUnixMilli int64
}

// AcceptanceCriterion binds one acceptance bullet to its exact evidence.
type AcceptanceCriterion struct {
	Bullet            string
	TodoID            string
	Test              string
	Fixture           string
	Command           string
	Result            CriterionResult
	EvidenceDigest    string
	Owner             string
	RetentionDays     int64
	ObservedUnixMilli int64
	MaxAgeMillis      int64
	ExpiresUnixMilli  int64
	SignOff           string
	Waiver            *Waiver
}

// PriorDecision is one retained gate decision, append-only.
type PriorDecision struct {
	GateRef          string
	Verdict          string
	Digest           string
	DecidedUnixMilli int64
}

// Manifest is the fully declared Gate B evidence submission.
type Manifest struct {
	GateRef             string
	NowUnixMilli        int64
	Criteria            []AcceptanceCriterion
	PriorDecisions      []PriorDecision
	RequestContinuation bool
	ContinuationScope   string
}

// CompileError is the typed structural refusal: the manifest itself is
// malformed, so no verdict exists. Evidence problems are verdicts, not
// errors.
type CompileError struct {
	Field  string
	Reason string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("gateb: field %s: %s", e.Field, e.Reason)
}

// CriterionReport is one evaluated bullet: the exact submitted binding
// plus its evaluated status, so the report itself maps every bullet to
// its todo, test, fixture, command, result, digest, owner, retention,
// expiry and sign-off.
type CriterionReport struct {
	AcceptanceCriterion
	Status CriterionStatus
	Reason string
}

// Blocker names a bullet that blocks continuation and why.
type Blocker struct {
	Bullet string
	Reason string
}

// AuthorityGrant is the continuation-authority verdict: permitted only
// on a clean CONTINUE with a requested scope.
type AuthorityGrant struct {
	Permitted bool
	Scope     string
	Reason    string
}

// Report is the digested compilation account with decisions retained.
type Report struct {
	GateRef   string
	Verdict   Verdict
	Criteria  []CriterionReport
	Blockers  []Blocker
	Grant     AuthorityGrant
	Decisions []PriorDecision
	Digest    string
}

// Compile evaluates one Gate B evidence manifest.
func Compile(manifest Manifest) (Report, error) {
	if err := validate(manifest); err != nil {
		return Report{}, err
	}
	covered := make(map[string]bool, len(manifest.Criteria))
	report := Report{GateRef: manifest.GateRef, Verdict: VerdictContinue}
	for _, criterion := range manifest.Criteria {
		covered[criterion.Bullet] = true
		status, reason := effectiveStatus(criterion, manifest.NowUnixMilli)
		report.Criteria = append(report.Criteria, CriterionReport{AcceptanceCriterion: criterion, Status: status, Reason: reason})
		switch status {
		case StatusPass:
		case StatusWaived:
			report.Verdict = VerdictConditionalContinue
		default:
			report.Verdict = VerdictGateBlocked
			report.Blockers = append(report.Blockers, Blocker{Bullet: criterion.Bullet, Reason: reason})
		}
	}
	sort.Slice(report.Criteria, func(i, j int) bool { return report.Criteria[i].Bullet < report.Criteria[j].Bullet })
	for _, bullet := range RequiredBullets {
		if !covered[bullet] {
			report.Verdict = VerdictGateBlocked
			report.Blockers = append(report.Blockers, Blocker{Bullet: bullet, Reason: "acceptance bullet has no bound criterion"})
		}
	}
	report.Grant = grantAuthority(manifest, report.Verdict)
	report.Decisions = append(append([]PriorDecision(nil), manifest.PriorDecisions...), PriorDecision{
		GateRef: manifest.GateRef, Verdict: string(report.Verdict),
		DecidedUnixMilli: manifest.NowUnixMilli,
	})
	report.Digest = digestReport(manifest, report)
	report.Decisions[len(report.Decisions)-1].Digest = report.Digest
	return report, nil
}

func validate(manifest Manifest) error {
	if strings.TrimSpace(manifest.GateRef) == "" {
		return &CompileError{Field: "gate_ref", Reason: "gate ref is required"}
	}
	if manifest.NowUnixMilli < 0 {
		return &CompileError{Field: "now", Reason: "review time cannot be negative"}
	}
	if len(manifest.Criteria) == 0 {
		return &CompileError{Field: "criteria", Reason: "manifest needs criteria"}
	}
	required := make(map[string]bool, len(RequiredBullets))
	for _, bullet := range RequiredBullets {
		required[bullet] = true
	}
	seen := make(map[string]bool, len(manifest.Criteria))
	for i, criterion := range manifest.Criteria {
		if !required[criterion.Bullet] {
			return &CompileError{Field: fmt.Sprintf("criteria[%d].bullet", i), Reason: fmt.Sprintf("bullet %q is not required", criterion.Bullet)}
		}
		if seen[criterion.Bullet] {
			return &CompileError{Field: fmt.Sprintf("criteria[%d].bullet", i), Reason: fmt.Sprintf("duplicate bullet %q", criterion.Bullet)}
		}
		seen[criterion.Bullet] = true
		switch criterion.Result {
		case ResultPass, ResultFail:
		default:
			return &CompileError{Field: fmt.Sprintf("criteria[%d].result", i), Reason: fmt.Sprintf("result %q is not declared", criterion.Result)}
		}
		if criterion.ObservedUnixMilli < 0 {
			return &CompileError{Field: fmt.Sprintf("criteria[%d].observed", i), Reason: "observed time cannot be negative"}
		}
		if criterion.MaxAgeMillis < 0 || criterion.ExpiresUnixMilli < 0 {
			return &CompileError{Field: fmt.Sprintf("criteria[%d].freshness", i), Reason: "max age and expiry cannot be negative"}
		}
	}
	for i, prior := range manifest.PriorDecisions {
		if strings.TrimSpace(prior.GateRef) == "" || strings.TrimSpace(prior.Digest) == "" {
			return &CompileError{Field: fmt.Sprintf("prior[%d]", i), Reason: "prior decision needs a gate ref and digest"}
		}
	}
	return nil
}

// effectiveStatus resolves bindings, freshness, waivers and signatures:
// anything but a bound, fresh, passing, signed criterion blocks.
func effectiveStatus(criterion AcceptanceCriterion, now int64) (CriterionStatus, string) {
	for field, value := range map[string]string{
		"todo": criterion.TodoID, "test": criterion.Test, "fixture": criterion.Fixture,
		"command": criterion.Command, "evidence digest": criterion.EvidenceDigest, "owner": criterion.Owner,
	} {
		if strings.TrimSpace(value) == "" {
			return StatusUnbound, fmt.Sprintf("criterion binds no %s", field)
		}
	}
	if criterion.RetentionDays <= 0 {
		return StatusUnbound, "criterion binds no retention period"
	}
	if criterion.ExpiresUnixMilli > 0 && now >= criterion.ExpiresUnixMilli {
		return StatusExpired, "evidence expired"
	}
	if now >= criterion.ObservedUnixMilli+criterion.MaxAgeMillis {
		return StatusStale, "evidence is stale"
	}
	if criterion.Waiver != nil {
		if criterion.Result != ResultFail {
			return StatusVoidWaiver, "waiver on passing evidence is void"
		}
		waiver := criterion.Waiver
		if strings.TrimSpace(waiver.By) == "" || strings.TrimSpace(waiver.Reason) == "" ||
			strings.TrimSpace(waiver.Scope) == "" || strings.TrimSpace(waiver.Control) == "" ||
			waiver.ExpiresUnixMilli <= 0 {
			return StatusVoidWaiver, "waiver names no by, reason, scope, control and expiry"
		}
		if now >= waiver.ExpiresUnixMilli {
			return StatusVoidWaiver, "waiver expired"
		}
		return StatusWaived, fmt.Sprintf("waived by %s: %s", waiver.By, waiver.Reason)
	}
	if criterion.Result != ResultPass {
		return StatusFail, "evidence reports failure"
	}
	if strings.TrimSpace(criterion.SignOff) == "" {
		return StatusUnsigned, "evidence carries no sign-off"
	}
	return StatusPass, "bound, fresh, passing and signed"
}

func grantAuthority(manifest Manifest, verdict Verdict) AuthorityGrant {
	if verdict != VerdictContinue {
		return AuthorityGrant{Reason: "authority needs a clean CONTINUE verdict"}
	}
	if !manifest.RequestContinuation || strings.TrimSpace(manifest.ContinuationScope) == "" {
		return AuthorityGrant{Reason: "no continuation scope requested"}
	}
	return AuthorityGrant{Permitted: true, Scope: manifest.ContinuationScope, Reason: "clean continue with requested scope"}
}

func digestReport(manifest Manifest, report Report) string {
	parts := []string{"gateb001-manifest", manifest.GateRef, string(report.Verdict), fmt.Sprint(manifest.NowUnixMilli)}
	for _, criterion := range report.Criteria {
		binding := criterion.AcceptanceCriterion
		waiver := ""
		if binding.Waiver != nil {
			waiver = strings.Join([]string{
				binding.Waiver.By, binding.Waiver.Reason, binding.Waiver.Scope,
				binding.Waiver.Control, fmt.Sprint(binding.Waiver.ExpiresUnixMilli),
			}, "\x00")
		}
		parts = append(parts, strings.Join([]string{
			binding.Bullet, binding.TodoID, binding.Test, binding.Fixture, binding.Command,
			string(binding.Result), binding.EvidenceDigest, binding.Owner,
			fmt.Sprint(binding.RetentionDays, binding.ObservedUnixMilli, binding.MaxAgeMillis, binding.ExpiresUnixMilli),
			binding.SignOff, waiver, string(criterion.Status), criterion.Reason,
		}, "\x00"))
	}
	var blockers []string
	for _, blocker := range report.Blockers {
		blockers = append(blockers, blocker.Bullet+"\x00"+blocker.Reason)
	}
	sort.Strings(blockers)
	parts = append(parts, "blockers:"+strings.Join(blockers, ","))
	parts = append(parts, fmt.Sprintf("grant:%v:%s:%s", report.Grant.Permitted, report.Grant.Scope, report.Grant.Reason))
	for _, prior := range manifest.PriorDecisions {
		parts = append(parts, strings.Join([]string{
			prior.GateRef, prior.Verdict, prior.Digest, fmt.Sprint(prior.DecidedUnixMilli),
		}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
