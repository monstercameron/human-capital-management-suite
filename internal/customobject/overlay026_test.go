// WF-EXT-026 RED: workflow variation requires Go code and a release; no
// tenant-owned definition namespace, overlay lifecycle or inheritance
// exists for workflows.
package customobject

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/cryptoagility"
)

var overlay026Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func overlay026Keys() (author, reviewer cryptoagility.Key, policy cryptoagility.AlgorithmPolicy, keys map[string]cryptoagility.Key) {
	seedA := make([]byte, ed25519.SeedSize)
	seedA[0], seedA[31] = 1, 26
	privA := ed25519.NewKeyFromSeed(seedA)
	seedR := make([]byte, ed25519.SeedSize)
	seedR[0], seedR[31] = 2, 26
	privR := ed25519.NewKeyFromSeed(seedR)
	author = cryptoagility.Key{ID: "tenant-key-a", Algorithm: cryptoagility.AlgorithmEd25519, Private: privA, Public: privA.Public().(ed25519.PublicKey)}
	reviewer = cryptoagility.Key{ID: "tenant-key-r", Algorithm: cryptoagility.AlgorithmEd25519, Private: privR, Public: privR.Public().(ed25519.PublicKey)}
	policy = cryptoagility.AlgorithmPolicy{Active: cryptoagility.AlgorithmEd25519, Accepted: map[string]bool{cryptoagility.AlgorithmEd25519: true}}
	keys = map[string]cryptoagility.Key{"tenant-key-a": author, "tenant-key-r": reviewer}
	return author, reviewer, policy, keys
}

func overlay026Template() OverlayTemplate {
	return OverlayTemplate{
		ID:                "product/onboarding/v4",
		MandatoryPhases:   []string{"identity-proofing", "eligibility-check"},
		MaxLoopBound:      10,
		AllowCompensation: false,
	}
}

func overlay026Draft() Overlay {
	return Overlay{
		ID:         OverlayID{Tenant: "t-acme", Name: "onboarding-plus"},
		TemplateID: "product/onboarding/v4",
		Level:      OverlayEnterprise,
		Author:     "u-author",
		Steps: []OverlayStep{
			{Name: "identity-proofing", Kind: StepPhase},
			{Name: "manager-approval", Kind: StepApproval},
			{Name: "eligibility-check", Kind: StepPhase},
			{Name: "welcome-notice", Kind: StepNotice},
			{Name: "doc-reminders", Kind: StepLoop, Bound: 3},
			{Name: "hris-sync", Kind: StepCapability, Capability: "hris.write"},
		},
		Thresholds: map[string]string{"approval_amount": "5000"},
	}
}

type passFixtures026 struct{}

func (passFixtures026) RunFixtures(Overlay) (FixtureReport, error) {
	return FixtureReport{Passed: 12, Total: 12}, nil
}

type failFixtures026 struct{}

func (failFixtures026) RunFixtures(Overlay) (FixtureReport, error) {
	return FixtureReport{Passed: 11, Total: 12}, nil
}

// TestTodo_WF_EXT_026 is the PRIMARY test: a tenant overlay moves DRAFT,
// VALIDATED, REVIEWED, PUBLISHED, ACTIVE with separation of duties and
// signing; inheritance runs enterprise, company, legal entity with
// lineage; publish requires passing simulation fixtures.
func TestTodo_WF_EXT_026(t *testing.T) {
	allow := []string{"hris.write", "notify.send"}

	t.Run("overlay walks the lifecycle to active", func(t *testing.T) {
		authorKey, reviewerKey, policy, keys := overlay026Keys()
		o := overlay026Draft()
		var err error
		o, err = ValidateOverlay(o, overlay026Template(), allow, passFixtures026{})
		if err != nil {
			t.Fatalf("ValidateOverlay: %v", err)
		}
		if o.State != OverlayValidated {
			t.Fatalf("state = %s, want VALIDATED", o.State)
		}
		o, err = ReviewOverlay(o, "u-reviewer")
		if err != nil {
			t.Fatalf("ReviewOverlay: %v", err)
		}
		o, err = PublishOverlay(o, overlay026Template(), allow, passFixtures026{}, authorKey, reviewerKey, policy, overlay026Now)
		if err != nil {
			t.Fatalf("PublishOverlay: %v", err)
		}
		if o.State != OverlayPublished || len(o.Signatures) != 2 || o.Digest == "" {
			t.Fatalf("published = %+v", o)
		}
		o, err = ActivateOverlay(o, keys, policy, overlay026Now)
		if err != nil {
			t.Fatalf("ActivateOverlay: %v", err)
		}
		if o.State != OverlayActive {
			t.Fatalf("state = %s, want ACTIVE", o.State)
		}
	})

	t.Run("publish requires passing fixtures", func(t *testing.T) {
		authorKey, reviewerKey, policy, _ := overlay026Keys()
		o := overlay026Draft()
		if _, err := ValidateOverlay(o, overlay026Template(), allow, failFixtures026{}); !errors.Is(err, ErrOverlayFixtures) {
			t.Fatalf("validate err = %v, want ErrOverlayFixtures", err)
		}
		o, _ = ValidateOverlay(o, overlay026Template(), allow, passFixtures026{})
		o, _ = ReviewOverlay(o, "u-reviewer")
		if _, err := PublishOverlay(o, overlay026Template(), allow, failFixtures026{}, authorKey, reviewerKey, policy, overlay026Now); !errors.Is(err, ErrOverlayFixtures) {
			t.Fatalf("publish err = %v, want ErrOverlayFixtures", err)
		}
	})

	t.Run("reviewer must differ from the author", func(t *testing.T) {
		o := overlay026Draft()
		o, _ = ValidateOverlay(o, overlay026Template(), allow, passFixtures026{})
		if _, err := ReviewOverlay(o, "u-author"); !errors.Is(err, ErrOverlaySoD) {
			t.Fatalf("self-review err = %v, want ErrOverlaySoD", err)
		}
	})

	t.Run("inheritance merges enterprise company legal-entity with lineage", func(t *testing.T) {
		authorKey, reviewerKey, policy, _ := overlay026Keys()
		publish := func(o Overlay) Overlay {
			t.Helper()
			var err error
			o, err = ValidateOverlay(o, overlay026Template(), allow, passFixtures026{})
			if err != nil {
				t.Fatalf("validate %s: %v", o.Level, err)
			}
			o, err = ReviewOverlay(o, "u-reviewer")
			if err != nil {
				t.Fatalf("review %s: %v", o.Level, err)
			}
			o, err = PublishOverlay(o, overlay026Template(), allow, passFixtures026{}, authorKey, reviewerKey, policy, overlay026Now)
			if err != nil {
				t.Fatalf("publish %s: %v", o.Level, err)
			}
			return o
		}
		ent := publish(overlay026Draft())
		co := overlay026Draft()
		co.Level = OverlayCompany
		co.Thresholds = map[string]string{"approval_amount": "2000"}
		co.Steps = append(co.Steps, OverlayStep{Name: "team-notice", Kind: StepNotice})
		co = publish(co)
		le := overlay026Draft()
		le.Level = OverlayLegalEntity
		le.Thresholds = map[string]string{"approval_amount": "500"}
		le = publish(le)
		resolved, err := ResolveInheritance(overlay026Template(), allow, ent, co, le)
		if err != nil {
			t.Fatalf("ResolveInheritance: %v", err)
		}
		if resolved.Thresholds["approval_amount"] != "500" {
			t.Fatalf("thresholds = %v, want legal-entity win", resolved.Thresholds)
		}
		// Company adds team-notice, keeps manager-approval; enterprise
		// steps survive where not overridden.
		names := map[string]bool{}
		for _, s := range resolved.Steps {
			names[s.Name] = true
		}
		for _, want := range []string{"identity-proofing", "team-notice", "hris-sync"} {
			if !names[want] {
				t.Fatalf("steps miss %q: %+v", want, resolved.Steps)
			}
		}
		lineage := map[string]OverlayLevel{}
		for _, l := range resolved.Lineage {
			lineage[l.Key] = l.Level
		}
		if lineage["threshold:approval_amount"] != OverlayLegalEntity {
			t.Fatalf("lineage = %+v", resolved.Lineage)
		}
		if lineage["step:team-notice"] != OverlayCompany || lineage["step:hris-sync"] != OverlayLegalEntity {
			t.Fatalf("lineage = %+v", resolved.Lineage)
		}
	})
}

// TestTodo_WF_EXT_026_Golden pins the canonical overlay bytes and digest.
func TestTodo_WF_EXT_026_Golden(t *testing.T) {
	o := overlay026Draft()
	const wantDigest = `sha256:95cf8cccd562b0354fbb8fb6a6012784d51d7410a79cfbb7ac70987b83fd8ba2`
	const wantCanonical = `overlay/v1
tenant: t-acme
name: onboarding-plus
template: product/onboarding/v4
level: enterprise
author: u-author
reviewer: 
step: doc-reminders|loop|3|
step: eligibility-check|phase|0|
step: hris-sync|capability-call|0|hris.write
step: identity-proofing|phase|0|
step: manager-approval|approval|0|
step: welcome-notice|notice|0|
threshold: approval_amount=5000
`
	if got := o.DigestFor(overlay026Template()); got != wantDigest {
		t.Fatalf("digest:\n%s\nwant:\n%s", got, wantDigest)
	}
	if got := o.Canonical(); got != wantCanonical {
		t.Fatalf("canonical:\n%s\nwant:\n%s", got, wantCanonical)
	}
}

// TestTodo_WF_EXT_026_Security proves an overlay cannot weaken mandatory
// controls: phases, compensation authorship, loop bounds, capability
// allow-list, and signature integrity.
func TestTodo_WF_EXT_026_Security(t *testing.T) {
	allow := []string{"hris.write", "notify.send"}
	template := overlay026Template()

	t.Run("mandatory phases cannot be omitted", func(t *testing.T) {
		o := overlay026Draft()
		o.Steps = []OverlayStep{{Name: "identity-proofing", Kind: StepPhase}}
		if _, err := ValidateOverlay(o, template, allow, passFixtures026{}); !errors.Is(err, ErrOverlayControl) {
			t.Fatalf("err = %v, want ErrOverlayControl", err)
		}
	})

	t.Run("compensation cannot be authored", func(t *testing.T) {
		o := overlay026Draft()
		o.Steps = append(o.Steps, OverlayStep{Name: "undo-pay", Kind: StepCompensation})
		if _, err := ValidateOverlay(o, template, allow, passFixtures026{}); !errors.Is(err, ErrOverlayControl) {
			t.Fatalf("err = %v, want ErrOverlayControl", err)
		}
	})

	t.Run("loops must be bounded", func(t *testing.T) {
		o := overlay026Draft()
		o.Steps = append(o.Steps, OverlayStep{Name: "poll-forever", Kind: StepLoop})
		if _, err := ValidateOverlay(o, template, allow, passFixtures026{}); !errors.Is(err, ErrOverlayControl) {
			t.Fatalf("unbounded err = %v, want ErrOverlayControl", err)
		}
		o = overlay026Draft()
		o.Steps = append(o.Steps, OverlayStep{Name: "poll-long", Kind: StepLoop, Bound: 99})
		if _, err := ValidateOverlay(o, template, allow, passFixtures026{}); !errors.Is(err, ErrOverlayControl) {
			t.Fatalf("over-bound err = %v, want ErrOverlayControl", err)
		}
	})

	t.Run("capabilities must be allow-listed", func(t *testing.T) {
		o := overlay026Draft()
		o.Steps = append(o.Steps, OverlayStep{Name: "drop-table", Kind: StepCapability, Capability: "db.admin"})
		if _, err := ValidateOverlay(o, template, allow, passFixtures026{}); !errors.Is(err, ErrOverlayControl) {
			t.Fatalf("err = %v, want ErrOverlayControl", err)
		}
	})

	t.Run("tampered overlays fail activation", func(t *testing.T) {
		authorKey, reviewerKey, policy, keys := overlay026Keys()
		o := overlay026Draft()
		o, _ = ValidateOverlay(o, template, allow, passFixtures026{})
		o, _ = ReviewOverlay(o, "u-reviewer")
		o, err := PublishOverlay(o, template, allow, passFixtures026{}, authorKey, reviewerKey, policy, overlay026Now)
		if err != nil {
			t.Fatalf("PublishOverlay: %v", err)
		}
		o.Thresholds["approval_amount"] = "999999999"
		if _, err := ActivateOverlay(o, keys, policy, overlay026Now); !errors.Is(err, ErrOverlaySignature) {
			t.Fatalf("tampered err = %v, want ErrOverlaySignature", err)
		}
	})

	t.Run("tenant namespace cannot squat the product namespace", func(t *testing.T) {
		for _, tenant := range []string{"product", "Product", " product "} {
			o := overlay026Draft()
			o.ID = OverlayID{Tenant: tenant, Name: "onboarding"}
			if _, err := ValidateOverlay(o, template, allow, passFixtures026{}); !errors.Is(err, ErrOverlayInvalid) {
				t.Fatalf("tenant %q err = %v, want ErrOverlayInvalid", tenant, err)
			}
		}
	})

	t.Run("activation requires two distinct signers", func(t *testing.T) {
		authorKey, reviewerKey, policy, keys := overlay026Keys()
		o := overlay026Draft()
		o, _ = ValidateOverlay(o, template, allow, passFixtures026{})
		o, _ = ReviewOverlay(o, "u-reviewer")
		o, err := PublishOverlay(o, template, allow, passFixtures026{}, authorKey, reviewerKey, policy, overlay026Now)
		if err != nil {
			t.Fatalf("PublishOverlay: %v", err)
		}
		o.Signatures = []cryptoagility.Signature{o.Signatures[0], o.Signatures[0]}
		if _, err := ActivateOverlay(o, keys, policy, overlay026Now); !errors.Is(err, ErrOverlaySignature) {
			t.Fatalf("duplicate-signer err = %v, want ErrOverlaySignature", err)
		}
	})

	t.Run("inheritance refuses unsigned draft levels", func(t *testing.T) {
		ent, co, le := overlay026Draft(), overlay026Draft(), overlay026Draft()
		co.Level, le.Level = OverlayCompany, OverlayLegalEntity
		if _, err := ResolveInheritance(template, allow, ent, co, le); !errors.Is(err, ErrOverlayState) {
			t.Fatalf("draft err = %v, want ErrOverlayState", err)
		}
	})

	t.Run("inheritance revalidates the merged view", func(t *testing.T) {
		// White-box: levels marked PUBLISHED whose merge violates the
		// template rules must still refuse.
		ent := overlay026Draft()
		ent.State = OverlayPublished
		co := overlay026Draft()
		co.Level, co.State = OverlayCompany, OverlayPublished
		co.Steps = []OverlayStep{
			{Name: "identity-proofing", Kind: StepNotice},
			{Name: "eligibility-check", Kind: StepPhase},
			{Name: "db-breakout", Kind: StepCapability, Capability: "db.admin"},
			{Name: "poll-x", Kind: StepLoop, Bound: 99999},
			{Name: "undo-pay", Kind: StepCompensation},
		}
		le := overlay026Draft()
		le.Level, le.State = OverlayLegalEntity, OverlayPublished
		le.Steps = nil
		if _, err := ResolveInheritance(template, allow, ent, co, le); !errors.Is(err, ErrOverlayControl) {
			t.Fatalf("poisoned merge err = %v, want ErrOverlayControl", err)
		}
	})
}
