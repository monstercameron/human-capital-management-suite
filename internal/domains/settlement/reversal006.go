// SETTLE-006: handle payment return and reversal.
//
// ApplyCorrectiveIntent records one governed return or reversal against
// an original payment without deleting it. The original stays immutable;
// the result carries payable, balance, funding and reporting deltas plus
// a bank-data-free communication. The function is kernel-pure.
package settlement

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CorrectiveVersion is the rejection version for corrective intents.
const CorrectiveVersion = "settlement-corrective/v1"

var (
	// ErrCorrectiveRejected is the SETTLE-006 sentinel. A return or
	// reversal that lacks authority, evidence, or a valid original is
	// refused; the original payment is never deleted.
	ErrCorrectiveRejected = errors.New("SETTLE_006_REJECTED")
)

// CorrectiveRejection is the stable SETTLE-006 failure shape.
type CorrectiveRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *CorrectiveRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrCorrectiveRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the SETTLE_006_REJECTED sentinel to errors.Is.
func (r *CorrectiveRejection) Unwrap() error { return ErrCorrectiveRejected }

func correctiveReject(field, state, reason string) error {
	return &CorrectiveRejection{Field: field, State: state, Version: CorrectiveVersion, Reason: reason}
}

// CorrectiveKind is the closed SETTLE-006 intent vocabulary.
type CorrectiveKind string

const (
	CorrectiveReturn   CorrectiveKind = "RETURN"
	CorrectiveReversal CorrectiveKind = "REVERSAL"
)

// Valid reports whether the kind is declared.
func (k CorrectiveKind) Valid() bool {
	return k == CorrectiveReturn || k == CorrectiveReversal
}

// CorrectiveIntent is one governed return or reversal. References are
// tokenized: no bank account, routing or credential payload is accepted.
type CorrectiveIntent struct {
	Tenant              string
	OriginalInstruction string
	OriginalAmount      values.Decimal
	OriginalState       EvidenceState
	Kind                CorrectiveKind
	Reason              string
	EvidenceRef         string
	AuthorityRef        string
	IdempotencyKey      string
	OccurredAt          time.Time
}

// CorrectiveResult carries the governed deltas and the safe
// communication. Deltas restore payable, balance, funding and reporting;
// Communication names tokenized refs only.
type CorrectiveResult struct {
	Kind           CorrectiveKind
	InstructionRef string
	PayableDelta   values.Decimal
	BalanceDelta   values.Decimal
	FundingDelta   values.Decimal
	ReportingDelta values.Decimal
	Communication  string
	IdempotencyKey string
	Digest         string
}

func (r CorrectiveResult) computedDigest(in CorrectiveIntent) string {
	w := canonicalbytes.New("hcmnext.domains.settlement.CorrectiveResult", 1).
		String("tenant", in.Tenant).
		String("instruction", in.OriginalInstruction).
		String("kind", string(r.Kind)).
		String("reason", in.Reason).
		String("evidence", in.EvidenceRef).
		String("authority", in.AuthorityRef).
		String("idempotency", in.IdempotencyKey).
		Value("payable", r.PayableDelta).
		Value("balance", r.BalanceDelta).
		Value("funding", r.FundingDelta).
		Value("reporting", r.ReportingDelta).
		String("occurred_at", in.OccurredAt.UTC().Format(time.RFC3339))
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// ApplyCorrectiveIntent records one return or reversal. Only a settled
// original can be corrected; the original itself is never mutated here.
func ApplyCorrectiveIntent(in CorrectiveIntent) (CorrectiveResult, error) {
	if strings.TrimSpace(in.Tenant) == "" {
		return CorrectiveResult{}, correctiveReject("corrective.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.OriginalInstruction) == "" {
		return CorrectiveResult{}, correctiveReject("corrective.original_instruction", "MISSING", "original instruction ref is required")
	}
	if err := in.OriginalAmount.Validate(); err != nil || in.OriginalAmount.Sign() <= 0 {
		return CorrectiveResult{}, correctiveReject("corrective.original_amount", "INVALID", "original amount must be a valid positive decimal")
	}
	if in.OriginalState != EvidenceSettled {
		return CorrectiveResult{}, correctiveReject("corrective.original_state", string(in.OriginalState), "only a settled payment can be returned or reversed")
	}
	if !in.Kind.Valid() {
		return CorrectiveResult{}, correctiveReject("corrective.kind", "UNDECLARED", fmt.Sprintf("kind %q is not declared", in.Kind))
	}
	if strings.TrimSpace(in.Reason) == "" {
		return CorrectiveResult{}, correctiveReject("corrective.reason", "MISSING", "reason is required")
	}
	if strings.TrimSpace(in.EvidenceRef) == "" {
		return CorrectiveResult{}, correctiveReject("corrective.evidence_ref", "MISSING", "rail evidence ref is required")
	}
	if strings.TrimSpace(in.AuthorityRef) == "" {
		return CorrectiveResult{}, correctiveReject("corrective.authority_ref", "MISSING", "authority ref is required")
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return CorrectiveResult{}, correctiveReject("corrective.idempotency_key", "MISSING", "idempotency key is required")
	}
	if in.OccurredAt.IsZero() {
		return CorrectiveResult{}, correctiveReject("corrective.occurred_at", "MISSING", "occurrence instant is required")
	}
	for name, v := range map[string]string{"reason": in.Reason, "evidence": in.EvidenceRef, "authority": in.AuthorityRef} {
		if containsBankPayload(v) {
			return CorrectiveResult{}, correctiveReject("corrective."+name, "LEAK", "bank payload is never accepted in corrective input")
		}
	}
	res := CorrectiveResult{
		Kind: in.Kind, InstructionRef: in.OriginalInstruction,
		PayableDelta: in.OriginalAmount, BalanceDelta: in.OriginalAmount,
		FundingDelta: in.OriginalAmount, ReportingDelta: in.OriginalAmount,
		Communication: fmt.Sprintf("instruction %s %s for %s: evidence %s; contact payroll support quoting %s",
			in.OriginalInstruction, strings.ToLower(string(in.Kind)), in.Reason, in.EvidenceRef, in.IdempotencyKey),
		IdempotencyKey: in.IdempotencyKey,
	}
	res.Digest = res.computedDigest(in)
	return res, nil
}

func containsBankPayload(s string) bool {
	for _, token := range []string{"acct:", "routing:", "iban:", "swift:", "card:", "cvv"} {
		for i := 0; i+len(token) <= len(s); i++ {
			match := true
			for j := 0; j < len(token); j++ {
				c := s[i+j]
				if c >= 'A' && c <= 'Z' {
					c += 'a' - 'A'
				}
				if c != token[j] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}
