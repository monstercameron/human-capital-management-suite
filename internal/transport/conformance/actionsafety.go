package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ALIGN-062: duplicate, stale, and concurrent actions are safe. Duplicate
// submissions of the same query collapse onto one stable request key, so
// retries never fan out into extra work; a stale invalidation (older than
// the envelope the client holds) requires no refetch while a newer one
// does; and the check is a pure function of the two messages, so
// concurrent clients decide identically.

// RequestKey returns the stable deduplication key for a query request.
// Two requests with the same key ask the same question: same tenant,
// purpose, instant, evaluated scope, bounded subject set, versions, and
// freshness position. The key names digests and identifiers only.
func RequestKey(req QueryRequest) (string, error) {
	if err := req.Validate(); err != nil {
		return "", err
	}
	subjects := make([]string, 0, len(req.Requested))
	for _, subject := range req.Requested {
		subjects = append(subjects, subject.String())
	}
	sort.Strings(subjects)
	h := sha256.New()
	write := func(label, value string) { fmt.Fprintf(h, "%s=%d:%s;", label, len(value), value) }
	write("tenant", req.Tenant.String())
	write("purpose", req.Purpose)
	write("effective_at", req.EffectiveAt.String())
	write("scope", req.Scope.InputsDigest())
	write("schema", req.SchemaVersion)
	write("definition", req.DefinitionVersion)
	write("freshness.source", req.Freshness.SourceVersion)
	write("freshness.projection", req.Freshness.ProjectionVersion)
	write("freshness.source_sequence", fmt.Sprint(req.Freshness.SourceSequence))
	write("freshness.projection_sequence", fmt.Sprint(req.Freshness.ProjectionSequence))
	write("subjects", strings.Join(subjects, ","))
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// RefetchRequired reports whether an invalidation obliges the holder of
// env to refetch. The invalidation must name the envelope's own tenant
// and projection version — anything else is refused rather than judged —
// and only a source sequence past the envelope's applied projection
// position requires a refetch. A duplicate or stale invalidation is
// safely ignorable.
func RefetchRequired(env QueryEnvelope, inv Invalidation) (bool, error) {
	if err := env.Validate(); err != nil {
		return false, err
	}
	if err := inv.Validate(); err != nil {
		return false, err
	}
	if inv.Tenant != env.Tenant {
		return false, fmt.Errorf("%w: invalidation tenant does not match the envelope", ErrInvalidationRejected)
	}
	if inv.ProjectionVersion != env.Freshness.ProjectionVersion {
		return false, fmt.Errorf("%w: invalidation projection does not match the envelope", ErrInvalidationRejected)
	}
	return inv.SourceSequence > env.Freshness.ProjectionSequence, nil
}
