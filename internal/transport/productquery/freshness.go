package productquery

import (
	"errors"
	"fmt"
	"time"
)

// ALIGN-019: every product response carries the reliability state of the
// projection it was built from. Project attaches that state at build time;
// this file makes it verifiable afterwards. A response that crossed a
// serialization boundary (protobuf, JSON, SSR data) can no longer be
// trusted to name its own freshness: VerifyFreshness recomputes the
// authoritative state from the projection and the verification instant and
// refuses anything else, so a tampered or replayed freshness label fails
// closed instead of presenting stale data as current.

// ErrFreshnessMismatch is returned when a response names a freshness its
// projection does not support at the verification instant.
var ErrFreshnessMismatch = errors.New("productquery: response freshness does not match its projection")

// ErrFreshnessInstant is returned when verification is attempted without a
// usable verification instant.
var ErrFreshnessInstant = errors.New("productquery: freshness verification requires a non-zero instant")

// VerifyFreshness proves the Freshness label on env is the one its
// Projection derives at now. The envelope is validated first, so a foreign
// row, an unbounded projection, or an invalid freshness spelling is refused
// before any comparison. An UNKNOWN label never verifies: a response whose
// reliability cannot be derived must be refetched, not trusted.
func VerifyFreshness(env Envelope, now time.Time) error {
	if err := env.Validate(); err != nil {
		return err
	}
	if now.IsZero() {
		return ErrFreshnessInstant
	}
	want := env.Projection.freshnessAt(now)
	if env.Freshness != want {
		return fmt.Errorf("%w: envelope says %s, projection derives %s", ErrFreshnessMismatch, env.Freshness, want)
	}
	return nil
}

// FreshnessAt reports the authoritative freshness of env at now without
// trusting its label. It returns the derived state for callers that must
// branch on reliability (refetch, banner, block) rather than verify a label.
func FreshnessAt(env Envelope, now time.Time) (Freshness, error) {
	if err := env.Validate(); err != nil {
		return FreshnessUnknown, err
	}
	if now.IsZero() {
		return FreshnessUnknown, ErrFreshnessInstant
	}
	return env.Projection.freshnessAt(now), nil
}
