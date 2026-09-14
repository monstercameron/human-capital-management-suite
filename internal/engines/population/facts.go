// POP-003 fact port: population resolution never reads a database directly.
// It reads through FactReader, a bitemporal fact port over subject fields
// this package defines and a caller (typically a domain adapter) implements
// against real storage; tests implement it with an in-memory fake.
package population

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrFactReader wraps every error a FactReader implementation returns so a
// caller can distinguish "the port failed" from an engine-internal error.
var ErrFactReader = errors.New("population: fact reader failed")

// Fact is one field's bitemporal value for one subject: the presence state
// (VALUE, UNKNOWN, UNAVAILABLE, REDACTED, NOT_APPLICABLE, ABSENT) and the
// instant the asserting authority came to know it. KnownAt lets Resolve refuse
// a correction the source claims to have, but claims to have known about only
// after the caller's declared knowledge boundary: a future-known correction
// must never silently change a historical resolution.
type Fact struct {
	Presence values.Presence[string]
	KnownAt  values.KnownAt
}

// FactReader is the bitemporal fact port over subject fields. Every method
// takes the caller's declared effective instant (asOf) and knowledge boundary
// (knownAt) explicitly; the engine never assumes a reader defaults to "now".
type FactReader interface {
	// Subjects enumerates the candidate subjects of kind that could possibly be
	// members as of asOf. The engine applies criteria to exactly this set; a
	// subject a FactReader never enumerates is never resolved.
	Subjects(ctx context.Context, kind SubjectKind, asOf values.Instant) ([]values.EntityRef, error)
	// ReadFact returns subject's value of field as of asOf, as known no later
	// than knownAt where the reader can honor that; Resolve independently
	// verifies the boundary and never trusts a reader that returns a fact whose
	// own KnownAt is after the requested boundary.
	ReadFact(ctx context.Context, subject values.EntityRef, field string, asOf values.Instant, knownAt values.KnownAt) (Fact, error)
	// Watermark returns the latest instant the source has fully ingested facts
	// of kind as of, so Resolve can tell "no fact" from "source has not caught
	// up yet" rather than treating both the same way.
	Watermark(ctx context.Context, kind SubjectKind) (values.Instant, error)
}

// ObligationReason names why a subject's membership could not be fully
// determined. It is typed vocabulary, not a free-form string, so a consumer
// can branch on it without parsing prose.
type ObligationReason uint8

// Obligation reasons.
const (
	ObligationUnspecified ObligationReason = iota
	// ObligationSourceUnavailable means the fact reader could not retrieve the
	// field right now.
	ObligationSourceUnavailable
	// ObligationFactUnknown means the value exists but is not known to the
	// source at all.
	ObligationFactUnknown
	// ObligationFactRedacted means a value exists and this reader may not
	// return it.
	ObligationFactRedacted
	// ObligationStaleWatermark means the source has not ingested facts as
	// current as the requested knowledge boundary, so an absent fact cannot be
	// trusted as a true absence.
	ObligationStaleWatermark
	// ObligationFutureKnowledge means the reader returned a fact whose KnownAt
	// is after the requested knowledge boundary; the fact is discarded as not
	// yet known, and this obligation records that a correction exists but was
	// excluded.
	ObligationFutureKnowledge
	// ObligationIdentityUnresolved means the subject reference itself could not
	// be resolved to a concrete identity.
	ObligationIdentityUnresolved
	// ObligationExpectedAbsent means an expected roster member has no
	// observed counterpart.
	ObligationExpectedAbsent
	// ObligationUnexpectedPresent means an observed member is outside the
	// expected roster.
	ObligationUnexpectedPresent
	// ObligationDuplicateObserved means an observed member appears more
	// than once, so raw counts overstate coverage.
	ObligationDuplicateObserved
	// ObligationUnauthorizedMember means an observed member is outside the
	// authorized set.
	ObligationUnauthorizedMember
)

var obligationWire = map[ObligationReason]string{
	ObligationSourceUnavailable:  "SOURCE_UNAVAILABLE",
	ObligationFactUnknown:        "FACT_UNKNOWN",
	ObligationFactRedacted:       "FACT_REDACTED",
	ObligationStaleWatermark:     "STALE_WATERMARK",
	ObligationFutureKnowledge:    "FUTURE_KNOWLEDGE",
	ObligationIdentityUnresolved: "IDENTITY_UNRESOLVED",
	ObligationExpectedAbsent:     "EXPECTED_ABSENT",
	ObligationUnexpectedPresent:  "UNEXPECTED_PRESENT",
	ObligationDuplicateObserved:  "DUPLICATE_OBSERVED",
	ObligationUnauthorizedMember: "UNAUTHORIZED_MEMBER",
}

// String returns the wire token, or OBLIGATION_UNSPECIFIED.
func (o ObligationReason) String() string {
	if s, ok := obligationWire[o]; ok {
		return s
	}
	return "OBLIGATION_UNSPECIFIED"
}

// Obligation records one blocking reason a subject's membership carries.
type Obligation struct {
	Reason ObligationReason
	Field  string
}
