package industrypack

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	publishNow = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	publishKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	otherKey   = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
)

type countingActivations struct {
	calls int
	err   error
}

func (c *countingActivations) SaveActivation(context.Context, ActivationReceipt) (ActivationEffects, error) {
	c.calls++
	if c.err != nil {
		return ActivationEffects{}, c.err
	}
	return ActivationEffects{AuthoritativeRows: 1, OutboxEntries: 1}, nil
}

func activationRequest() ActivationRequest {
	env := SignPackVersion(SignedPackVersion{PackID: "healthcare-base", Industry: IndustryHealthcare, Version: 3, BundleDigest: "sha256:bundle3",
		Target: PackTarget{Tenant: "acme", Cell: "cell-us-1"}, EffectiveAt: publishNow.Add(time.Hour), RollbackVersion: 2,
		Publisher: "release:ana", Approver: "release:ben", SignedAt: publishNow.Add(-time.Minute)}, "pack-2026", publishKey)
	return ActivationRequest{Envelope: env, Target: PackTarget{Tenant: "acme", Cell: "cell-us-1"}, ActiveVersion: 2,
		TrustedKeys: map[string]ed25519.PublicKey{"pack-2026": publishKey.Public().(ed25519.PublicKey)}}
}

func wantRefusal(t *testing.T, err error, field, state string) {
	t.Helper()
	var r *ActivationRefusal
	if !errors.As(err, &r) || !errors.Is(err, ErrActivationRefused) || r.Code != SignatureRejectionCode || r.Field != field || r.State != state ||
		!strings.Contains(r.Error(), SignatureRejectionCode) {
		t.Fatalf("err = %v, want %s %s %s", err, SignatureRejectionCode, field, state)
	}
}

// TestTodo_PACK_005 proves a signed, approved, current envelope activates
// with a receipt binding target, bundle, effective time and rollback version,
// and that unsigned, wrong-scope, stale and unapproved envelopes cannot.
func TestTodo_PACK_005(t *testing.T) {
	ctx := context.Background()
	store := &countingActivations{}
	r, effects, err := ActivateSigned(ctx, store, authorityForIndustry(t, IndustryHealthcare), activationRequest())
	if err != nil || store.calls != 1 || effects.AuthoritativeRows != 1 {
		t.Fatalf("activate = %+v, %+v, %v", r, effects, err)
	}
	if r.Target != (PackTarget{Tenant: "acme", Cell: "cell-us-1"}) || r.BundleDigest != "sha256:bundle3" || !r.EffectiveAt.Equal(publishNow.Add(time.Hour)) ||
		r.RollbackVersion != 2 || r.Version != 3 || r.KeyID != "pack-2026" || !strings.HasPrefix(r.Digest, "sha256:") {
		t.Fatalf("receipt = %+v", r)
	}
	for name, tc := range map[string]struct {
		mutate       func(*ActivationRequest)
		field, state string
	}{
		"unsigned":    {func(q *ActivationRequest) { q.Envelope.Signature = nil }, "signature", SignatureMissing},
		"wrong scope": {func(q *ActivationRequest) { q.Target.Cell = "cell-eu-1" }, "target", SignatureWrongScope},
		"stale": {func(q *ActivationRequest) {
			q.Envelope.SignedAt = publishNow
			q.Envelope = SignPackVersion(q.Envelope, "pack-2026", publishKey)
		}, "signed_at", SignatureStale},
		"unapproved": {func(q *ActivationRequest) {
			q.Envelope.Approver = ""
			q.Envelope = SignPackVersion(q.Envelope, "pack-2026", publishKey)
		}, "approver", SignatureUnapproved},
	} {
		req := activationRequest()
		tc.mutate(&req)
		store := &countingActivations{}
		authority := authorityForIndustry(t, IndustryHealthcare)
		if name == "stale" {
			authority = authorityFor(t, IndustryHealthcare, "acme", "ACTIVE", []string{IndustryEntitlementCapability(IndustryHealthcare)}, publishNow.Add(-time.Hour), publishNow.Add(72*time.Hour), publishNow.Add(48*time.Hour), 24*time.Hour)
		}
		_, effects, err := ActivateSigned(ctx, store, authority, req)
		wantRefusal(t, err, tc.field, tc.state)
		if store.calls != 0 || effects != (ActivationEffects{}) {
			t.Fatalf("%s: a refused activation persisted", name)
		}
	}
}

// TestTodo_PACK_005_Golden pins the signing payload and receipt digest.
func TestTodo_PACK_005_Golden(t *testing.T) {
	req := activationRequest()
	want := "hcmnext.industrypack.SignedPackVersion/v1\nhealthcare-base\nHEALTHCARE\n3\nsha256:bundle3\nacme\ncell-us-1\n2026-09-14T13:00:00Z\n2\nrelease:ana\nrelease:ben\n2026-09-14T11:59:00Z\npack-2026"
	if got := string(req.Envelope.SigningPayload()); got != want {
		t.Fatalf("payload:\n%s", got)
	}
	r, err := VerifyActivation(context.Background(), authorityForIndustry(t, IndustryHealthcare), req)
	if err != nil {
		t.Fatal(err)
	}
	const golden = "sha256:f0755c8f253a5291b94771dc2527f5401bceeab952096ecb04873d6a50d6a562"
	if r.Digest != golden {
		t.Fatalf("receipt digest = %s, want %s", r.Digest, golden)
	}
}

// TestTodo_PACK_005_Mutation tampers each bound field after signing and each
// refusal gate in turn.
func TestTodo_PACK_005_Mutation(t *testing.T) {
	cases := []struct {
		name         string
		mutate       func(*ActivationRequest)
		field, state string
	}{
		{"malformed", func(q *ActivationRequest) { q.Envelope.BundleDigest = "bundle" }, "envelope", SignatureMalformed},
		{"no key id", func(q *ActivationRequest) { q.Envelope.KeyID = "" }, "signature", SignatureMissing},
		{"untrusted key", func(q *ActivationRequest) { q.Envelope = SignPackVersion(q.Envelope, "rogue", otherKey) }, "key_id", SignatureUntrusted},
		{"signed by wrong key", func(q *ActivationRequest) { q.Envelope.Signature = ed25519.Sign(otherKey, q.Envelope.SigningPayload()) }, "signature", SignatureInvalid},
		{"bundle tampered", func(q *ActivationRequest) { q.Envelope.BundleDigest = "sha256:evil" }, "signature", SignatureInvalid},
		{"effective tampered", func(q *ActivationRequest) { q.Envelope.EffectiveAt = q.Envelope.EffectiveAt.Add(-time.Hour) }, "signature", SignatureInvalid},
		{"rollback tampered", func(q *ActivationRequest) { q.Envelope.RollbackVersion = 1 }, "signature", SignatureInvalid},
		{"target tampered", func(q *ActivationRequest) { q.Envelope.Target.Tenant = "globex" }, "signature", SignatureInvalid},
		{"signed in future", func(q *ActivationRequest) {
			q.Envelope.SignedAt = publishNow.Add(time.Hour)
			q.Envelope = SignPackVersion(q.Envelope, "pack-2026", publishKey)
		}, "signed_at", SignatureStale},
		{"not newer", func(q *ActivationRequest) { q.ActiveVersion = 3 }, "version", SignatureStale},
		{"rollback not active", func(q *ActivationRequest) { q.ActiveVersion = 1 }, "rollback_version", SignatureStale},
		{"self approved", func(q *ActivationRequest) {
			q.Envelope.Approver = "RELEASE:ANA"
			q.Envelope = SignPackVersion(q.Envelope, "pack-2026", publishKey)
		}, "approver", SignatureUnapproved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := activationRequest()
			tc.mutate(&req)
			store := &countingActivations{}
			_, effects, err := ActivateSigned(context.Background(), store, authorityForIndustry(t, IndustryHealthcare), req)
			wantRefusal(t, err, tc.field, tc.state)
			if store.calls != 0 || effects != (ActivationEffects{}) {
				t.Fatal("a refused activation persisted")
			}
		})
	}
	noAge := activationRequest()
	noAgeAuthority := authorityFor(t, IndustryHealthcare, "acme", "ACTIVE", []string{IndustryEntitlementCapability(IndustryHealthcare)},
		publishNow.Add(-24*time.Hour), publishNow.Add(2000*time.Hour), publishNow.Add(1000*time.Hour), 0)
	if _, err := VerifyActivation(context.Background(), noAgeAuthority, noAge); err != nil {
		t.Fatalf("unbounded age = %v", err)
	}
	if (PackTarget{Tenant: "a", Cell: "b"}).String() != "a@b" {
		t.Fatal("target string")
	}
	if _, _, err := ActivateSigned(context.Background(), nil, authorityForIndustry(t, IndustryHealthcare), activationRequest()); !errors.Is(err, ErrActivationRefused) {
		t.Fatalf("nil store = %v", err)
	}
	boom := errors.New("db down")
	if _, _, err := ActivateSigned(context.Background(), &countingActivations{err: boom}, authorityForIndustry(t, IndustryHealthcare), activationRequest()); !errors.Is(err, boom) {
		t.Fatalf("store failure = %v", err)
	}
}
