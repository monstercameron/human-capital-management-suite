package idempotency

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle status of one idempotency record. There are
// RESERVED and COMPLETED describe active requests. COMPENSATED closes a
// completed scope after its inverse is durably recorded. TOMBSTONE is the
// compact permanent form of an expired request: it retains uniqueness and
// digest while dropping result references. There is no FAILED state; a caller
// whose effect fails rolls its whole transaction back.
type Status string

// The declared lifecycle statuses.
const (
	StatusReserved  Status = "RESERVED"
	StatusCompleted Status = "COMPLETED"
	StatusTombstone Status = "TOMBSTONE"
	// StatusCompensated permanently closes an effect scope after its inverse
	// was verified. Identity retains the original result; CompensatedByRef
	// identifies the durable compensation event.
	StatusCompensated Status = "COMPENSATED"
)

// Valid reports whether s is one of the two declared statuses.
func (s Status) Valid() bool {
	return s == StatusReserved || s == StatusCompleted || s == StatusTombstone || s == StatusCompensated
}

// Scope names the semantic uniqueness scope TX-006 declares: tenant,
// capability, effect scope and idempotency key. It deliberately carries no
// principal field -- platform-foundation-gap-closure.md section 6: "a change
// of executor, workflow worker, connector worker or governed repair
// principal does not create a new namespace for the same semantic effect."
// A caller that genuinely needs separate principals to produce distinct
// business decisions folds the principal into EffectScope itself, rather
// than this package widening its scope for everyone.
type Scope struct {
	// Tenant is the owning tenant. Required.
	Tenant uuid.UUID
	// Capability names the capability/version whose effect is guarded.
	Capability string
	// EffectScope names the semantic effect scope within the capability
	// (e.g. the governed write class or resource class the effect touches).
	EffectScope string
	// Key is the idempotency key itself, as declared by the request.
	Key string
}

// Validate rejects a scope that cannot identify a record.
func (s Scope) Validate() error {
	switch {
	case s.Tenant == uuid.Nil:
		return refuse(CodeInvalidScope, s, "scope has no tenant")
	case strings.TrimSpace(s.Capability) == "":
		return refuse(CodeInvalidScope, s, "scope has no capability")
	case strings.TrimSpace(s.EffectScope) == "":
		return refuse(CodeInvalidScope, s, "scope has no effect scope")
	case strings.TrimSpace(s.Key) == "":
		return refuse(CodeInvalidScope, s, "scope has no idempotency key")
	}
	return nil
}

// String renders the scope's stable text form, for logs and error messages
// only -- never parsed back.
func (s Scope) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", s.Tenant, s.Capability, s.EffectScope, s.Key)
}

// digestPattern matches the content_digest domain migrations/00002_tenant_
// primitives.sql declares: a lower-case sha256 hex digest. Every canonical
// digest in this codebase (ledger_event.digest, stream_head.head_digest)
// already commits to that single profile, so this package validates the
// same shape in Go before ever reaching the database's own CHECK
// constraint -- a caller gets a typed refusal, not a generic constraint
// violation.
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidDigest reports whether digest is a well-formed canonical request
// digest.
func ValidDigest(digest string) bool {
	return digestPattern.MatchString(digest)
}

// ResultIdentity is the stored result identity a completed [Guard] call
// attaches to its record: a result reference, a ledger event reference, an
// effect identity and an evidence id. A caller states whichever references
// its own effect actually produced -- none is required by itself, but
// [Empty] reports true only when none at all is present, and a completion
// that would store nothing is refused rather than silently recording an
// empty identity worth nothing on replay.
type ResultIdentity struct {
	// ResultRef references the domain result the effect produced.
	ResultRef string
	// EventRef references the ledger event the effect appended, if any
	// (typically internal/data/ledger.EventRef's own stable text form).
	EventRef string
	// EffectIdentity references the external or durable effect (an outbox
	// EffectIdentity, a connector operation id, ...) the effect produced.
	EffectIdentity string
	// EvidenceID references governed evidence the effect recorded.
	EvidenceID string
	// ActionPlanBindingRef names a durable, canonical accepted-action to
	// transaction-plan binding persisted by the same commit transaction.
	ActionPlanBindingRef string
}

// Empty reports whether the identity carries no reference at all.
func (r ResultIdentity) Empty() bool {
	return r.ResultRef == "" && r.EventRef == "" && r.EffectIdentity == "" && r.EvidenceID == "" && r.ActionPlanBindingRef == ""
}

// RetentionPolicy is the caller-declared lifecycle policy for one guarded
// effect: how long the record survives, and the maximum window over which
// the effect's own source can still legitimately retry or redeliver the same
// request. [RetentionPolicy.Validate] -- and therefore
// [PostgresStore.Reserve] -- refuses a policy whose Retention would expire
// before that RetryWindow closes: a record that could disappear while the
// window it exists to cover is still open would let a legitimate retry land
// as if it were a brand new request, which is exactly the duplicate effect
// this whole package exists to prevent. REFACTOR requires the lifecycle to
// stay policy-owned, so this package never invents a default for either
// field.
type RetentionPolicy struct {
	// Retention is how long the record survives after it is reserved.
	Retention time.Duration
	// RetryWindow is the maximum window the request's own source may still
	// retry or redeliver it. Zero means the source never redelivers.
	RetryWindow time.Duration
	// Class is an optional assertion about the capability's registered policy.
	// It cannot override [CapabilityRetentionRegistry]; an empty value derives
	// the class from the registry.
	Class RetentionClass
}

// RetentionClass is the capability's declared post-expiry behavior.
type RetentionClass string

const (
	RetentionExpiring           RetentionClass = "EXPIRING"
	RetentionPermanentTombstone RetentionClass = "PERMANENT_TOMBSTONE"
)

// Validate rejects a policy that cannot be honored: non-positive retention,
// a negative retry window, or a retention shorter than the declared retry
// window (the RED case "retention expires before retry/redelivery window").
func (p RetentionPolicy) Validate() error {
	switch {
	case p.Retention <= 0:
		return &Error{Code: CodeInvalidRecord, Detail: "retention must be positive"}
	case p.RetryWindow < 0:
		return &Error{Code: CodeInvalidRecord, Detail: "retry window cannot be negative"}
	case p.Class != "" && p.Class != RetentionExpiring && p.Class != RetentionPermanentTombstone:
		return &Error{Code: CodeInvalidRecord, Detail: fmt.Sprintf("unknown retention class %q", p.Class)}
	case p.Retention < p.RetryWindow:
		return &Error{
			Code: CodeRetentionTooShort,
			Detail: fmt.Sprintf(
				"retention %s is shorter than the declared retry/redelivery window %s",
				p.Retention, p.RetryWindow),
		}
	}
	return nil
}

// Record is one stored idempotency record: the scope it guards, the
// canonical digest of the request that reserved it, its lifecycle status,
// the result identity a completed record carries, and its recording and
// expiry instants.
type Record struct {
	Scope            Scope
	RequestDigest    string
	Status           Status
	RetentionClass   RetentionClass
	Identity         ResultIdentity
	CompensatedByRef string
	CreatedAt        time.Time
	ExpiresAt        time.Time
}
