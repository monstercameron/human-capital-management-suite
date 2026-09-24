// Package workreview owns the restricted evidence-review finding
// vocabulary the workflow may see: the closed verdict set, the safe
// reason codes, and the sealed Finding type that carries them.
//
// It lives in engines (below the domain layer) because both the leave
// domain and the workflow step adapters consume findings, and neither
// direction may point upward at internal/humanwork/workitem, which owns
// the compartment machinery and the durable store. workitem re-exports
// this vocabulary unchanged, so existing importers keep compiling.
//
// The digest domain separator ("workitem-finding") and the seal error
// text are byte-identical to the vocabulary's previous home: seals
// stamped before the move still verify after it.
package workreview

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Version is the public contract version for this evidence-review vocabulary.
func Version() int { return 1 }

// Review verdicts: the closed typed-finding vocabulary. Free-form
// eligibility or diagnosis never leaves the compartment.
const (
	ReviewSufficient   = "SUFFICIENT"
	ReviewInsufficient = "INSUFFICIENT"
	ReviewMoreInfo     = "MORE_INFORMATION_REQUIRED"
	ReviewUnknown      = "UNKNOWN"
)

// ValidVerdict reports whether verdict is one of the four typed findings.
func ValidVerdict(verdict string) bool {
	switch verdict {
	case ReviewSufficient, ReviewInsufficient, ReviewMoreInfo, ReviewUnknown:
		return true
	default:
		return false
	}
}

// Safe reason codes: the only reasons a finding may carry. Sensitive
// notes remain compartmented artifacts; the workflow sees only these.
var safeReasons = map[string]bool{
	"evidence-clear":         true,
	"evidence-contradictory": true,
	"evidence-partial":       true,
	"evidence-unreadable":    true,
}

// ValidReason reports whether reason is a safe reason code: the only
// reasons a finding may carry without exposing free-form detail.
func ValidReason(reason string) bool {
	return safeReasons[reason]
}

// Finding is the minimum typed result the workflow may see. It binds
// requirement and artifact versions, reviewed scope, safe reason, expiry
// and evidence receipt — and nothing else. It is workflow input, never
// legal eligibility by itself: the type carries no eligibility verdict.
type Finding struct {
	TaskID             string
	Verdict            string
	RequirementID      string
	RequirementVersion string
	ArtifactID         string
	ArtifactVersion    string
	Scope              []string
	Reason             string
	ExpiresTick        int64
	EvidenceReceipt    string
	Digest             string
}

func findingDigest(finding Finding) string {
	scope := append([]string(nil), finding.Scope...)
	sort.Strings(scope)
	parts := []string{"workitem-finding", finding.TaskID, finding.Verdict, finding.RequirementID, finding.RequirementVersion, finding.ArtifactID, finding.ArtifactVersion, strings.Join(scope, ","), finding.Reason, fmt.Sprint(finding.ExpiresTick), finding.EvidenceReceipt}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Seal returns f with its content digest stamped. The digest binds every
// field except Digest itself, so Seal is idempotent: sealing an already
// sealed finding recomputes the identical value.
func Seal(f Finding) Finding {
	f.Digest = findingDigest(f)
	return f
}

// Verify recomputes the finding seal.
func (finding Finding) Verify() error {
	if finding.Digest == "" || findingDigest(finding) != finding.Digest {
		return fmt.Errorf("workitem: finding seal is broken")
	}
	return nil
}

// Explain returns a safe, stable summary of the sealed finding without
// exposing evidence content.
func (finding Finding) Explain() string {
	return "workreview finding " + finding.Verdict + " (" + finding.Reason + ")"
}
