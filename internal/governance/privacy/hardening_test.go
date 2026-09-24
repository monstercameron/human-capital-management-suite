package privacy

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestCanonical_EncodingAndDigest_Boundaries(t *testing.T) {
	base := appendFields(nil, "a", "", "b", "value")
	if len(base) == 0 || string(base) == "" {
		t.Fatal("canonical encoding omitted empty/value fields")
	}
	if string(appendFields(nil, "ab", "c")) == string(appendFields(nil, "a", "bc")) {
		t.Fatal("field framing is ambiguous")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("odd field list did not panic")
		}
	}()
	//lint:ignore SA5012 deliberate odd arity: this test proves appendFields panics on an odd argument count. owner=privacy-governance expires=2027-03-24
	appendFields(nil, "odd")
}

func TestCanonical_StringSliceAndDigest_AreDeterministic(t *testing.T) {
	a := appendStringSlice(nil, "item", []string{"a", "b"})
	b := appendStringSlice(nil, "item", []string{"a", "b"})
	if digestHex(a) != digestHex(b) || len(digestHex(a)) != 64 {
		t.Fatalf("digest = %q, want stable sha256", digestHex(a))
	}
	if digestHex(appendStringSlice(nil, "item", []string{"b", "a"})) == digestHex(a) {
		t.Fatal("string slice order did not affect digest")
	}
}

func TestNotice_ValidateActiveAtAndDigest_Boundaries(t *testing.T) {
	base := fixtureNotice(t)
	mutants := []struct {
		name   string
		mutate func(Notice) Notice
	}{
		{"id", func(n Notice) Notice { n.ID = ""; return n }},
		{"version", func(n Notice) Notice { n.Version = ""; return n }},
		{"purpose", func(n Notice) Notice { n.Purpose = ""; return n }},
		{"data classes", func(n Notice) Notice { n.DataClasses = nil; return n }},
		{"empty data class", func(n Notice) Notice { n.DataClasses = []string{""}; return n }},
		{"jurisdiction", func(n Notice) Notice { n.Jurisdiction = ""; return n }},
		{"locale", func(n Notice) Notice { n.Locale = ""; return n }},
		{"effective from", func(n Notice) Notice { n.EffectiveFrom = values.Instant{}; return n }},
		{"reversed interval", func(n Notice) Notice { n.EffectiveTo = n.EffectiveFrom; return n }},
	}
	for _, tc := range mutants {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(base).Validate(); !errors.Is(err, ErrNoticeInvalid) {
				t.Fatalf("Validate() = %v, want ErrNoticeInvalid", err)
			}
		})
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid notice rejected: %v", err)
	}
	if base.ActiveAt(mustInstant(t, fxNoticeEffectiveFrom-1)) || !base.ActiveAt(mustInstant(t, fxNoticeEffectiveFrom)) || base.ActiveAt(mustInstant(t, fxNoticeEffectiveTo)) || base.ActiveAt(values.Instant{}) {
		t.Fatal("ActiveAt boundary behavior is incorrect")
	}
	invalid := base
	invalid.ID = ""
	if invalid.ActiveAt(mustInstant(t, fxEvaluateAt)) {
		t.Fatal("invalid notice reported active")
	}
	if base.Digest() == (func() string { n := base; n.Locale = "es-MX"; return n.Digest() })() {
		t.Fatal("notice digest ignored locale")
	}
}

func TestPresentation_ConstructorValidateAndMatch_Boundaries(t *testing.T) {
	notice := fixtureNotice(t)
	valid, err := NewPresentation("p", notice, fixturePrincipal, "en-US", true, mustInstant(t, fxPresentedAt), mustInstant(t, fxAcknowledgedAt))
	if err != nil {
		t.Fatalf("NewPresentation: %v", err)
	}
	if err := valid.Validate(); err != nil || valid.EvidenceID == "" {
		t.Fatalf("valid presentation = %+v, err=%v", valid, err)
	}
	if !valid.MatchesNotice(notice) {
		t.Fatal("presentation did not match source notice")
	}
	for name, mutate := range map[string]func(Presentation) Presentation{
		"id":                   func(p Presentation) Presentation { p.ID = ""; return p },
		"notice reference":     func(p Presentation) Presentation { p.NoticeDigest = "other"; return p },
		"principal":            func(p Presentation) Presentation { p.Principal = ""; return p },
		"locale":               func(p Presentation) Presentation { p.Locale = ""; return p },
		"presented at":         func(p Presentation) Presentation { p.PresentedAt = values.Instant{}; return p },
		"ack before presented": func(p Presentation) Presentation { p.AcknowledgedAt = mustInstant(t, fxPresentedAt-1); return p },
		"missing evidence":     func(p Presentation) Presentation { p.EvidenceID = ""; return p },
		"tampered evidence":    func(p Presentation) Presentation { p.EvidenceID = "tampered"; return p },
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(valid).Validate(); !errors.Is(err, ErrPresentationInvalid) {
				t.Fatalf("Validate() = %v, want ErrPresentationInvalid", err)
			}
		})
	}
	if _, err := NewPresentation("p", Notice{}, fixturePrincipal, "en-US", true, mustInstant(t, fxPresentedAt), values.Instant{}); !errors.Is(err, ErrPresentationInvalid) {
		t.Fatalf("invalid notice constructor error = %v", err)
	}
	changed := notice
	changed.Version = "v2"
	if valid.MatchesNotice(changed) {
		t.Fatal("presentation matched a changed notice version")
	}
}

func TestConsent_ConstructorValidateScopeStatusAndWithdraw_Boundaries(t *testing.T) {
	notice := fixtureNotice(t)
	presentation := fixturePresentation(t, notice)
	valid, err := NewOptionalProcessing("c", fixturePrincipal, []string{"ai_assist", "analytics"}, presentation.ID, mustInstant(t, fxGrantedAt), mustInstant(t, fxGrantedAt+100))
	if err != nil {
		t.Fatalf("NewOptionalProcessing: %v", err)
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid consent rejected: %v", err)
	}
	if !valid.HasScope("ai_assist") || valid.HasScope("marketing") {
		t.Fatal("HasScope returned incorrect results")
	}
	if valid.StatusAt(mustInstant(t, fxGrantedAt-1)) != ConsentStatusUnspecified || valid.StatusAt(mustInstant(t, fxGrantedAt)) != ConsentStatusGranted || valid.StatusAt(mustInstant(t, fxGrantedAt+100)) != ConsentStatusExpired || valid.StatusAt(values.Instant{}) != ConsentStatusUnspecified {
		t.Fatal("consent status boundary behavior is incorrect")
	}
	mutants := []struct {
		name   string
		mutate func(OptionalProcessing) OptionalProcessing
	}{
		{"id", func(c OptionalProcessing) OptionalProcessing { c.ID = ""; return c }},
		{"principal", func(c OptionalProcessing) OptionalProcessing { c.Principal = ""; return c }},
		{"scope empty", func(c OptionalProcessing) OptionalProcessing { c.Scope = nil; return c }},
		{"scope entry empty", func(c OptionalProcessing) OptionalProcessing { c.Scope = []string{""}; return c }},
		{"presentation", func(c OptionalProcessing) OptionalProcessing { c.PresentationID = ""; return c }},
		{"granted at", func(c OptionalProcessing) OptionalProcessing { c.GrantedAt = values.Instant{}; return c }},
		{"expiry before grant", func(c OptionalProcessing) OptionalProcessing { c.ExpiresAt = c.GrantedAt; return c }},
		{"withdrawal before grant", func(c OptionalProcessing) OptionalProcessing { c.WithdrawnAt = mustInstant(t, fxGrantedAt-1); return c }},
		{"missing evidence", func(c OptionalProcessing) OptionalProcessing { c.EvidenceID = ""; return c }},
		{"tampered evidence", func(c OptionalProcessing) OptionalProcessing { c.EvidenceID = "tampered"; return c }},
	}
	for _, tc := range mutants {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(valid).Validate(); !errors.Is(err, ErrConsentInvalid) {
				t.Fatalf("Validate() = %v, want ErrConsentInvalid", err)
			}
		})
	}
	withdrawn, err := valid.Withdraw(mustInstant(t, fxGrantedAt+50))
	if err != nil || withdrawn.StatusAt(mustInstant(t, fxGrantedAt+50)) != ConsentStatusWithdrawn || valid.WithdrawnAt.IsSet() {
		t.Fatalf("Withdraw result = %+v, err=%v; original was mutated", withdrawn, err)
	}
	if _, err := valid.Withdraw(values.Instant{}); !errors.Is(err, ErrConsentInvalid) {
		t.Fatalf("unset withdrawal error = %v", err)
	}
	if _, err := valid.Withdraw(mustInstant(t, fxGrantedAt-1)); !errors.Is(err, ErrConsentInvalid) {
		t.Fatalf("early withdrawal error = %v", err)
	}
}

func TestAuthority_Evaluate_AllDecisionBranchesAndEvidence(t *testing.T) {
	base := fixtureAuthorityInput(t)
	allowed := EvaluateAuthority(base)
	if !allowed.Allowed || allowed.Code != AuthorityAllowed || allowed.NoticeVersion != base.Notice.Version || allowed.Locale != base.Presentation.Locale || len(allowed.Obligations) != 0 || allowed.EvidenceID == "" || allowed.InputsDigest == "" {
		t.Fatalf("allowed decision = %+v", allowed)
	}
	if strings.Contains(allowed.Explain(), "manager_name") {
		t.Fatal("Explain leaked notice content")
	}
	cases := []struct {
		name  string
		input AuthorityInput
		code  AuthorityCode
	}{
		{"mandatory", func() AuthorityInput { in := base; in.Notice = fixtureMandatoryNotice(t); return in }(), AuthorityMandatoryBasisNotConsent},
		{"invalid notice", func() AuthorityInput { in := base; in.Notice.ID = ""; return in }(), AuthorityNoticeInvalid},
		{"expired notice", func() AuthorityInput { in := base; in.EffectiveAt = mustInstant(t, fxNoticeEffectiveTo); return in }(), AuthorityNoticeExpired},
		{"not presented", func() AuthorityInput { in := base; in.Presentation = nil; return in }(), AuthorityNoticeNotPresented},
		{"invalid presentation", func() AuthorityInput {
			in := base
			p := *in.Presentation
			p.EvidenceID = "tampered"
			in.Presentation = &p
			return in
		}(), AuthorityPresentationInvalid},
		{"stale notice", func() AuthorityInput { in := base; in.Notice.Version = "v2"; return in }(), AuthorityNoticeVersionStale},
		{"wrong presentation principal", func() AuthorityInput {
			in := base
			p, _ := NewPresentation("other-principal", in.Notice, "other", "en-US", true, mustInstant(t, fxPresentedAt), values.Instant{})
			in.Presentation = &p
			return in
		}(), AuthorityPresentationWrongWho},
		{"unsupported locale", func() AuthorityInput { in := base; in.SupportedLocales = []string{"fr-FR"}; return in }(), AuthorityUnsupportedLocale},
		{"inaccessible", func() AuthorityInput {
			in := base
			p, _ := NewPresentation("inaccessible", in.Notice, fixturePrincipal, "en-US", false, mustInstant(t, fxPresentedAt), values.Instant{})
			in.Presentation = &p
			c := fixtureConsent(t, p.ID)
			in.Consent = &c
			return in
		}(), AuthorityPresentationNotA11y},
		{"no consent", func() AuthorityInput { in := base; in.Consent = nil; return in }(), AuthorityConsentNotGranted},
		{"invalid consent", func() AuthorityInput {
			in := base
			c := *in.Consent
			c.EvidenceID = "tampered"
			in.Consent = &c
			return in
		}(), AuthorityConsentInvalid},
		{"wrong consent principal", func() AuthorityInput {
			in := base
			c := *in.Consent
			c.Principal = "other"
			c.EvidenceID = consentEvidencePrefix + c.canonicalDigest()
			in.Consent = &c
			return in
		}(), AuthorityConsentWrongWho},
		{"wrong consent presentation", func() AuthorityInput {
			in := base
			c := *in.Consent
			c.PresentationID = "other"
			c.EvidenceID = consentEvidencePrefix + c.canonicalDigest()
			in.Consent = &c
			return in
		}(), AuthorityConsentWrongNotice},
		{"purpose out of scope", func() AuthorityInput { in := base; in.Purpose = "marketing"; return in }(), AuthorityPurposeOutOfScope},
		{"notice purpose mismatch", func() AuthorityInput {
			in := base
			in.Purpose = "analytics"
			c := *in.Consent
			c.Scope = []string{"analytics"}
			c.EvidenceID = consentEvidencePrefix + c.canonicalDigest()
			in.Consent = &c
			return in
		}(), AuthorityPurposeOutOfScope},
		{"withdrawn", func() AuthorityInput {
			in := base
			c, _ := in.Consent.Withdraw(mustInstant(t, fxEvaluateAt-1))
			in.Consent = &c
			return in
		}(), AuthorityProcessingBlocked},
		{"expired consent", func() AuthorityInput {
			in := base
			c, _ := NewOptionalProcessing("expired", fixturePrincipal, []string{"ai_assist"}, in.Presentation.ID, mustInstant(t, fxGrantedAt), mustInstant(t, fxEvaluateAt))
			in.Consent = &c
			return in
		}(), AuthorityConsentExpired},
		{"not yet granted", func() AuthorityInput {
			in := base
			c, _ := NewOptionalProcessing("future", fixturePrincipal, []string{"ai_assist"}, in.Presentation.ID, mustInstant(t, fxEvaluateAt+1), values.Instant{})
			in.Consent = &c
			return in
		}(), AuthorityConsentNotGranted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateAuthority(tc.input)
			if got.Allowed || got.Code != tc.code || got.EvidenceID == "" {
				t.Fatalf("decision = %+v, want denied %s with evidence", got, tc.code)
			}
		})
	}
	withdrawn := func() AuthorityInput {
		in := base
		c, err := in.Consent.Withdraw(mustInstant(t, fxEvaluateAt-1))
		if err != nil {
			t.Fatal(err)
		}
		in.Consent = &c
		return in
	}()
	got := EvaluateAuthority(withdrawn)
	if strings.Join(got.Obligations, ",") != "REQUIRE_DELETION,RESTRICT_PROCESSING" {
		t.Fatalf("withdrawal obligations = %v", got.Obligations)
	}
	if got.InputsDigest == EvaluateAuthority(base).InputsDigest {
		t.Fatal("different authority inputs shared a digest")
	}
}

func TestRenderNoticeDocument_RendersEscapedAccessibleDocument(t *testing.T) {
	n := fixtureNotice(t)
	n.Purpose = "<script>alert(1)</script>"
	html, err := RenderNoticeDocument(n)
	if err != nil {
		t.Fatalf("RenderNoticeDocument: %v", err)
	}
	for _, want := range []string{"<!doctype html>", "role=\"status\"", "aria-live=\"polite\"", "for=\"ack\"", "&lt;script&gt;"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered document missing %q", want)
		}
	}
}

func TestDisclosurePolicy_ValidateDigestAndQueryDigest_Boundaries(t *testing.T) {
	base := disclosurePolicy()
	invalid := []struct {
		name   string
		mutate func(DisclosurePolicy) DisclosurePolicy
	}{
		{"missing id", func(p DisclosurePolicy) DisclosurePolicy { p.ID = ""; return p }},
		{"missing version", func(p DisclosurePolicy) DisclosurePolicy { p.Version = ""; return p }},
		{"zero min cell", func(p DisclosurePolicy) DisclosurePolicy { p.MinCell = 0; return p }},
		{"zero budget", func(p DisclosurePolicy) DisclosurePolicy { p.Budget = 0; return p }},
		{"zero repeated budget", func(p DisclosurePolicy) DisclosurePolicy { p.RepeatedQueryBudget = 0; return p }},
		{"negative round", func(p DisclosurePolicy) DisclosurePolicy { p.Round = -1; return p }},
		{"negative noise", func(p DisclosurePolicy) DisclosurePolicy { p.Noise = -1; return p }},
		{"noise too large", func(p DisclosurePolicy) DisclosurePolicy { p.Noise = 128; return p }},
		{"mixed transforms", func(p DisclosurePolicy) DisclosurePolicy { p.Round = 2; p.Noise = 1; return p }},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(base).Validate(); !errors.Is(err, ErrInvalidDisclosurePolicy) {
				t.Fatalf("Validate() = %v, want ErrInvalidDisclosurePolicy", err)
			}
		})
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	firstPolicyDigest, secondPolicyDigest := base.Digest(), base.Digest()
	if firstPolicyDigest != secondPolicyDigest || len(firstPolicyDigest) != 64 {
		t.Fatalf("policy digest is not stable sha256: %q", firstPolicyDigest)
	}
	derived := AnalyticsQuery{TenantID: "t", Principal: "p", Purpose: "x", Dimensions: []string{"b", "a"}}
	if QueryDigest(derived) != QueryDigest(AnalyticsQuery{TenantID: "t", Principal: "p", Purpose: "x", Dimensions: []string{"a", "b"}}) {
		t.Fatal("derived query digest was order-dependent")
	}
	provided := derived
	provided.Digest = "provided-digest"
	if QueryDigest(provided) != provided.Digest {
		t.Fatal("provided query digest was not honored")
	}
}

func TestDisclosureBudget_ConsumeAndApply_AllSecurityBranches(t *testing.T) {
	p := disclosurePolicy()
	q := disclosureQuery()
	if used, err := (*DisclosureBudget)(nil).Consume(q, p); used != 0 || !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("nil budget = (%d, %v)", used, err)
	}
	b := NewDisclosureBudget()
	if used, err := b.Consume(q, p); used != 1 || err != nil {
		t.Fatalf("first consume = (%d, %v)", used, err)
	}
	otherQuery := q
	otherQuery.Dimensions = []string{"team"}
	if used, err := b.Consume(otherQuery, p); used != 2 || err != nil {
		t.Fatalf("second total consume = (%d, %v)", used, err)
	}
	if used, err := b.Consume(q, p); used != 3 || err != nil {
		t.Fatalf("second repeat consume = (%d, %v)", used, err)
	}
	if used, err := b.Consume(q, p); used != 3 || !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("repeat limit = (%d, %v)", used, err)
	}
	totalBudget := NewDisclosureBudget()
	for _, query := range []AnalyticsQuery{q, otherQuery, {TenantID: q.TenantID, Principal: q.Principal, Purpose: q.Purpose, Dimensions: []string{"region"}}} {
		if used, err := totalBudget.Consume(query, p); err != nil || used == 0 {
			t.Fatalf("total budget setup = (%d, %v)", used, err)
		}
	}
	if used, err := totalBudget.Consume(AnalyticsQuery{TenantID: q.TenantID, Principal: q.Principal, Purpose: q.Purpose, Dimensions: []string{"country"}}, p); used != 3 || !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("total budget limit = (%d, %v)", used, err)
	}
	for _, tc := range []struct {
		name   string
		policy DisclosurePolicy
		query  AnalyticsQuery
		budget *DisclosureBudget
		want   error
	}{
		{"invalid policy", func() DisclosurePolicy { x := p; x.MinCell = 0; return x }(), q, NewDisclosureBudget(), ErrInvalidDisclosurePolicy},
		{"missing tenant", p, func() AnalyticsQuery { x := q; x.TenantID = ""; return x }(), NewDisclosureBudget(), ErrDisclosureDenied},
		{"missing principal", p, func() AnalyticsQuery { x := q; x.Principal = ""; return x }(), NewDisclosureBudget(), ErrDisclosureDenied},
		{"missing purpose", p, func() AnalyticsQuery { x := q; x.Purpose = ""; return x }(), NewDisclosureBudget(), ErrDisclosureDenied},
		{"nil budget", p, q, nil, ErrBudgetExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Apply(tc.policy, tc.query, nil, tc.budget)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Apply() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDisclosureApply_SuppressionTransformsReviewAndEvidence(t *testing.T) {
	p := disclosurePolicy()
	p.SensitiveDimensions = []string{"salary"}
	q := disclosureQuery()
	cells := []AnalyticsCell{{Key: "small", Dimension: "department", Value: 2}, {Key: "sensitive", Dimension: "salary", Value: 20, Sensitive: true}, {Key: "large", Dimension: "department", Value: 8}, {Key: "only", Dimension: "team", Value: 9}}
	p.RequireReview = true
	got, err := Apply(p, q, cells, NewDisclosureBudget())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.Evidence.Status != DisclosureReviewRequired || got.Evidence.Transform != "NONE" || got.Evidence.BudgetUsed != 1 {
		t.Fatalf("review result evidence = %+v", got.Evidence)
	}
	if len(got.Evidence.Suppressed) != 4 || got.Cells[0].Value != 0 || got.Cells[1].Value != 0 {
		t.Fatalf("suppression result = %+v", got)
	}
	if cells[0].Value != 2 {
		t.Fatal("Apply mutated input cells")
	}
	noSuppression := p
	noSuppression.Complementary = false
	noSuppression.RequireReview = false
	allowed, err := Apply(noSuppression, q, []AnalyticsCell{{Key: "x", Dimension: "d", Value: 10}}, NewDisclosureBudget())
	if err != nil || allowed.Evidence.Status != DisclosureAllowed {
		t.Fatalf("allowed result = %+v, %v", allowed, err)
	}
	noise := noSuppression
	noise.Noise = 2
	noisy, err := Apply(noise, q, []AnalyticsCell{{Key: "x", Value: 10}}, NewDisclosureBudget())
	if err != nil || noisy.Evidence.Transform != "NOISE:2" || noisy.Cells[0].Value < 8 || noisy.Cells[0].Value > 12 {
		t.Fatalf("noise result = %+v, %v", noisy, err)
	}
	round := noSuppression
	round.Round = 5
	rounded, err := Apply(round, q, []AnalyticsCell{{Key: "x", Value: 18}}, NewDisclosureBudget())
	if err != nil || rounded.Evidence.Transform != "ROUND:5" || rounded.Cells[0].Value != 15 {
		t.Fatalf("round result = %+v, %v", rounded, err)
	}
}
