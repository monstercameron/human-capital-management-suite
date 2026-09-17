package productquery_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func freshEnvelope(t *testing.T) productquery.Envelope {
	t.Helper()
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	env, err := productquery.Project(request(p, []productquery.Candidate{
		allowedCandidate("00000000-0000-4000-8000-000000000001"),
	}))
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	return env
}

func staleEnvelope(t *testing.T) productquery.Envelope {
	t.Helper()
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	req := request(p, []productquery.Candidate{
		allowedCandidate("00000000-0000-4000-8000-000000000001"),
	})
	req.ObservedNow = testNow.Add(time.Hour)
	env, err := productquery.Project(req)
	if err != nil {
		t.Fatalf("Project(stale): %v", err)
	}
	return env
}

// TestTodo_ALIGN_019 proves every product response carries verifiable
// projection freshness: a current projection verifies, a stale one verifies
// as stale, and a tampered label is refused.
func TestTodo_ALIGN_019(t *testing.T) {
	env := freshEnvelope(t)
	if env.Freshness != productquery.FreshnessCurrent {
		t.Fatalf("fresh envelope freshness = %s, want CURRENT", env.Freshness)
	}
	if err := productquery.VerifyFreshness(env, testNow); err != nil {
		t.Fatalf("VerifyFreshness(current): %v", err)
	}
	stale := staleEnvelope(t)
	if stale.Freshness != productquery.FreshnessStale {
		t.Fatalf("aged envelope freshness = %s, want STALE", stale.Freshness)
	}
	if err := productquery.VerifyFreshness(stale, testNow.Add(time.Hour)); err != nil {
		t.Fatalf("VerifyFreshness(stale): %v", err)
	}
	// A stale response relabeled CURRENT is an upgrade attack.
	upgraded := stale
	upgraded.Freshness = productquery.FreshnessCurrent
	if err := productquery.VerifyFreshness(upgraded, testNow.Add(time.Hour)); !errors.Is(err, productquery.ErrFreshnessMismatch) {
		t.Fatalf("VerifyFreshness(upgraded) = %v, want ErrFreshnessMismatch", err)
	}
}

func TestTodo_ALIGN_019_Property(t *testing.T) {
	env := freshEnvelope(t)
	for i := 0; i < 2; i++ {
		if err := productquery.VerifyFreshness(env, testNow); err != nil {
			t.Fatalf("VerifyFreshness pass %d: %v", i, err)
		}
	}
	again := freshEnvelope(t)
	if again.Digest() != env.Digest() {
		t.Fatalf("identical projections digested differently: %s != %s", again.Digest(), env.Digest())
	}
	if got, err := productquery.FreshnessAt(env, testNow); err != nil || got != productquery.FreshnessCurrent {
		t.Fatalf("FreshnessAt = %s, %v, want CURRENT", got, err)
	}
}

func TestTodo_ALIGN_019_Golden(t *testing.T) {
	env := freshEnvelope(t)
	const wantDigest = "f1ef173164e61cd2106d89949d1764378298e86374a366d943eeab4465b47820"
	if env.Digest() != wantDigest {
		t.Fatalf("fresh envelope digest=%q want=%q", env.Digest(), wantDigest)
	}
}

func TestTodo_ALIGN_019_Security(t *testing.T) {
	env := freshEnvelope(t)
	// A current response relabeled STALE is a downgrade attack.
	downgraded := env
	downgraded.Freshness = productquery.FreshnessStale
	if err := productquery.VerifyFreshness(downgraded, testNow); !errors.Is(err, productquery.ErrFreshnessMismatch) {
		t.Fatalf("VerifyFreshness(downgraded) = %v, want ErrFreshnessMismatch", err)
	}
	// Verification without an instant proves nothing.
	if err := productquery.VerifyFreshness(env, time.Time{}); !errors.Is(err, productquery.ErrFreshnessInstant) {
		t.Fatalf("VerifyFreshness(zero now) = %v, want ErrFreshnessInstant", err)
	}
	// A cross-tenant envelope is refused as invalid before any comparison.
	foreign := env
	foreign.Rows = []productquery.Row{{Subject: subject("other", "00000000-0000-4000-8000-000000000002")}}
	if err := productquery.VerifyFreshness(foreign, testNow); err == nil || errors.Is(err, productquery.ErrFreshnessMismatch) {
		t.Fatalf("VerifyFreshness(foreign) = %v, want a validation refusal", err)
	}
	// An UNKNOWN label never verifies, even at a valid instant.
	unknown := env
	unknown.Freshness = productquery.FreshnessUnknown
	if err := productquery.VerifyFreshness(unknown, testNow); !errors.Is(err, productquery.ErrFreshnessMismatch) {
		t.Fatalf("VerifyFreshness(unknown) = %v, want ErrFreshnessMismatch", err)
	}
}

func TestTodo_ALIGN_019_Conformance(t *testing.T) {
	env := freshEnvelope(t)
	// Freshness survives the canonical serialization boundary with its
	// verifiability intact: a renderer receives bytes, not trust.
	raw, err := env.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	var restored productquery.Envelope
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatalf("unmarshal canonical envelope: %v", err)
	}
	if err := productquery.VerifyFreshness(restored, testNow); err != nil {
		t.Fatalf("VerifyFreshness(restored): %v", err)
	}
	// Tampering with the serialized freshness is detected after restore.
	var tampered map[string]any
	if err := json.Unmarshal(raw, &tampered); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	tampered["freshness"] = "STALE"
	edited, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("remarshal tampered envelope: %v", err)
	}
	var forged productquery.Envelope
	if err := json.Unmarshal(edited, &forged); err != nil {
		t.Fatalf("unmarshal forged envelope: %v", err)
	}
	if err := productquery.VerifyFreshness(forged, testNow); !errors.Is(err, productquery.ErrFreshnessMismatch) {
		t.Fatalf("VerifyFreshness(forged) = %v, want ErrFreshnessMismatch", err)
	}
}

func FuzzTodo_ALIGN_019_Fuzz(f *testing.F) {
	f.Add(int64(0), int64(1))
	f.Fuzz(func(t *testing.T, nowUnix, skewUnix int64) {
		now := time.Unix(nowUnix, 0).UTC()
		if now.IsZero() {
			t.Skip("zero instant is refused by contract")
		}
		env := productquery.Envelope{
			ContractVersion: productquery.Version(),
			Tenant:          values.TenantId("acme"),
			Purpose:         authz.PurposeCompensationReview,
			Projection: productquery.Projection{
				Name: "worker_summary", DefinitionVersion: "worker-summary.v3",
				SchemaVersion: "schema.v5", SourceSequence: 12, Watermark: 10,
				ObservedAt: time.Unix(skewUnix, 0).UTC(), MaxAge: time.Minute,
			},
			Freshness: productquery.FreshnessCurrent,
		}
		first := productquery.VerifyFreshness(env, now)
		second := productquery.VerifyFreshness(env, now)
		if (first == nil) != (second == nil) {
			t.Fatalf("verification is not deterministic: %v vs %v", first, second)
		}
	})
}
