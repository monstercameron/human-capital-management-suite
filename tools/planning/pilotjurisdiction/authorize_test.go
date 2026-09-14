package pilotjurisdiction

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func authorizeTestSigner(t *testing.T) *legal.Signer {
	t.Helper()
	seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	signer, err := legal.NewSigner(priv)
	if err != nil {
		t.Fatalf("legal.NewSigner: %v", err)
	}
	return signer
}

// authorizeTestRegistry registers both of legal's own seed packs (California
// and New York) so an "out of reviewed scope" resolution has somewhere real
// to land, rather than failing for the unrelated reason that nothing is
// registered at all.
func authorizeTestRegistry(t *testing.T) *legal.Registry {
	t.Helper()
	reg := legal.NewRegistry()
	ca, err := legal.CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	if err := reg.Register(ca); err != nil {
		t.Fatalf("Register(CA): %v", err)
	}
	ny, err := legal.NewYorkPromotionPack()
	if err != nil {
		t.Fatalf("NewYorkPromotionPack: %v", err)
	}
	if err := reg.Register(ny); err != nil {
		t.Fatalf("Register(NY): %v", err)
	}
	return reg
}

func mustLocalDate(t *testing.T, y int, m int, d int) values.LocalDate {
	t.Helper()
	date, err := values.NewLocalDate(y, time.Month(m), d)
	if err != nil {
		t.Fatalf("NewLocalDate: %v", err)
	}
	return date
}

func mustInstant(t *testing.T, unixSec int64) values.Instant {
	t.Helper()
	i, err := values.NewInstantFromUnix(unixSec, 0)
	if err != nil {
		t.Fatalf("NewInstantFromUnix: %v", err)
	}
	return i
}

func mustKnownAt(t *testing.T, unixSec int64) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(mustInstant(t, unixSec))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return k
}

// TestTodo_SELECT_001_UncertaintyBehavior is GREEN's "returns UNKNOWN |
// HUMAN_REVIEW_REQUIRED instead of guessing outside the reviewed scope",
// driven through genuinely ambiguous or out-of-scope facts rather than a
// hand-set flag: Authorize is never told "this is ambiguous" or "this is
// out of scope" directly, it is only given contradictory or foreign
// jurisdiction facts and must derive the right refusal itself by calling
// legal.Resolve.
func TestTodo_SELECT_001_UncertaintyBehavior(t *testing.T) {
	profile := validFixture()
	registry := authorizeTestRegistry(t)
	signer := authorizeTestSigner(t)
	now := mustInstant(t, 1_780_000_000)

	t.Run("genuinely contradictory facts return HUMAN_REVIEW_REQUIRED via legal.ErrLegalContextUnknown", func(t *testing.T) {
		// The worker physically works on-site in California (RemoteWork:
		// false) but the employer asserts the employment jurisdiction is New
		// York. legal.ResolveJurisdictionSet treats an on-site work location
		// that disagrees with the asserted employment jurisdiction as
		// contradictory, not as "New York wins" or "California wins" - this
		// is a real ambiguity legal.Resolve itself detects, not a flag this
		// test sets.
		input := legal.LegalContextInput{
			LegalEntityID:          "acme-inc",
			WorkLocation:           legal.Jurisdiction{Country: "US", State: "CA"},
			EmploymentJurisdiction: legal.Jurisdiction{Country: "US", State: "NY"},
			RemoteWork:             false,
			EffectiveDate:          mustLocalDate(t, 2026, 6, 1),
			KnownAt:                mustKnownAt(t, 1_770_000_000),
		}

		ctx, status, err := profile.Authorize(input, registry, signer, now)
		if ctx != nil {
			t.Fatalf("expected a nil LegalContext, got %+v", ctx)
		}
		if status != ScopeStatusHumanReviewRequired {
			t.Fatalf("status = %s, want %s", status, ScopeStatusHumanReviewRequired)
		}
		if !errors.Is(err, legal.ErrLegalContextUnknown) {
			t.Fatalf("expected err to wrap legal.ErrLegalContextUnknown, got %v", err)
		}
	})

	t.Run("a resolution that lands outside this profile's reviewed jurisdiction returns UNKNOWN", func(t *testing.T) {
		// Facts are entirely consistent - the worker works on-site in New
		// York and the employer agrees - so legal.Resolve succeeds
		// confidently. It just is not this profile's jurisdiction.
		input := legal.LegalContextInput{
			LegalEntityID:          "acme-inc",
			WorkLocation:           legal.Jurisdiction{Country: "US", State: "NY"},
			EmploymentJurisdiction: legal.Jurisdiction{Country: "US", State: "NY"},
			RemoteWork:             false,
			EffectiveDate:          mustLocalDate(t, 2026, 6, 1),
			KnownAt:                mustKnownAt(t, 1_770_000_000),
		}

		ctx, status, err := profile.Authorize(input, registry, signer, now)
		if ctx != nil {
			t.Fatalf("expected a nil LegalContext, got %+v", ctx)
		}
		if status != ScopeStatusUnknown {
			t.Fatalf("status = %s, want %s", status, ScopeStatusUnknown)
		}
		if !errors.Is(err, ErrOutsideReviewedScope) {
			t.Fatalf("expected err to wrap ErrOutsideReviewedScope, got %v", err)
		}
	})

	t.Run("a resolution inside this profile's reviewed jurisdiction is IN_SCOPE", func(t *testing.T) {
		input := legal.LegalContextInput{
			LegalEntityID:          "acme-inc",
			WorkLocation:           legal.Jurisdiction{Country: "US", State: "CA"},
			EmploymentJurisdiction: legal.Jurisdiction{Country: "US", State: "CA"},
			RemoteWork:             false,
			EffectiveDate:          mustLocalDate(t, 2026, 6, 1),
			KnownAt:                mustKnownAt(t, 1_770_000_000),
		}

		ctx, status, err := profile.Authorize(input, registry, signer, now)
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if status != ScopeStatusInScope {
			t.Fatalf("status = %s, want %s", status, ScopeStatusInScope)
		}
		if ctx == nil {
			t.Fatal("expected a non-nil LegalContext for an in-scope resolution")
		}
		if got := ctx.Jurisdiction(); got != (legal.Jurisdiction{Country: "US", State: "CA"}) {
			t.Fatalf("resolved jurisdiction = %s, want US-CA", got)
		}
	})
}
