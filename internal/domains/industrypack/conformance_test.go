package industrypack

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var (
	usOvertimeRule = ContentRef{Kind: ContentRule, Namespace: "us", ID: "overtime", Version: "2026.1"}
	acmeCostCenter = ContentRef{Kind: ContentReferenceData, Namespace: "acme", ID: "cost-centers", Version: "1"}
)

type countingIndustryActivations struct{ calls int }

func (c *countingIndustryActivations) SaveIndustryActivation(context.Context, IndustryResult, ActivationReceipt) (ActivationEffects, error) {
	c.calls++
	return ActivationEffects{AuthoritativeRows: 3, BusinessEvents: 1, OutboxEntries: 1}, nil
}

func conformanceManifest(t *testing.T, pack IndustryPack) IndustryPack {
	t.Helper()
	pack.Owner, pack.Scope, pack.Support = "hcmnext", "us", "maintained"
	if len(pack.Compatibility) == 0 {
		pack.Compatibility = []CompatibilityDeclaration{{Component: "hcmnext", MinimumVersion: "1", MaximumVersion: "2"}}
	}
	out, err := NewIndustryPack(pack)
	if err != nil {
		t.Fatalf("NewIndustryPack(%s): %v", pack.PackID, err)
	}
	return out
}

// industryComposition is one industry pack combined with the same US country
// pack and the same customer pack.
func industryComposition(t *testing.T, industry Industry, packID, workflowID, retained string) IndustryComposition {
	t.Helper()
	start := bindingDate(t, "2026-01-01")
	jobFamily := ContentRef{Kind: ContentReferenceData, Namespace: packID, ID: "job-family", Version: "1"}
	country := conformanceManifest(t, IndustryPack{PackID: "us-country", Version: 1, Industry: IndustryGeneral})
	countryDigest, _ := country.Digest()
	customer := conformanceManifest(t, IndustryPack{PackID: "acme", Version: 1, Industry: IndustryGeneral})
	pack := conformanceManifest(t, IndustryPack{PackID: packID, Version: 1, Industry: industry,
		Dependencies:           []PinnedRef{{ID: "us-country", Version: "1", Digest: countryDigest}},
		WorkflowDefinitionRefs: []PinnedRef{{ID: workflowID, Version: "1", Digest: "sha256:wf-" + workflowID}},
		FormRefs:               []PinnedRef{{ID: workflowID + "-form", Version: "1"}},
		MetricRefs:             []PinnedRef{{ID: workflowID + "-cycle-time", Version: "1"}},
		Compatibility: []CompatibilityDeclaration{
			{Component: "hcmnext", MinimumVersion: "1.4", MaximumVersion: "2"},
			{Component: "us-country", MinimumVersion: "1"},
		},
	})
	content := BindingSpec{Packs: []Pack{
		{Manifest: country, References: []ContentRef{usOvertimeRule},
			Contents: []Content{{Ref: usOvertimeRule, Effective: OpenWindow(start), Digest: "sha256:us-overtime"}}},
		{Manifest: pack, References: []ContentRef{jobFamily},
			Contents: []Content{{Ref: jobFamily, Effective: OpenWindow(start), Digest: "sha256:" + packID + "-job-family", Dependencies: []ContentRef{usOvertimeRule}}}},
		{Manifest: customer, References: []ContentRef{acmeCostCenter},
			Contents: []Content{{Ref: acmeCostCenter, Effective: OpenWindow(start), Digest: "sha256:acme-cost-centers"}}},
	}}
	wf := ExperienceRef{Kind: ExperienceWorkflow, ID: workflowID, Version: "1"}
	form := ExperienceRef{Kind: ExperienceForm, ID: workflowID + "-form", Version: "1"}
	experience := ExperienceBindingSpec{Pack: pack, DefaultLocale: "en-US", Registry: []ExperienceArtifact{
		{Ref: wf, Digest: "sha256:wf-" + workflowID, SideEffects: []string{"WORKFORCE_CHANGE"}, AuthorityScopes: []string{packID + ".workflow.execute"},
			Locales: []string{"en-US", "es-US"}, Dependencies: []ExperienceRef{form}, ContentDependencies: []ContentRef{usOvertimeRule, jobFamily}},
		{Ref: form, Digest: "sha256:form-" + workflowID, SideEffects: []string{"ATTESTATION_RECORDED"}, AuthorityScopes: []string{packID + ".form.submit"},
			Locales: []string{"en-US"}, Accessibility: "WCAG_2_2_AA"},
		{Ref: ExperienceRef{Kind: ExperienceMetric, ID: workflowID + "-cycle-time", Version: "1"}, Digest: "sha256:metric-" + workflowID,
			SideEffects: []string{SideEffectNone}, Locales: []string{"en-US"}, Metric: &MetricDefinition{Unit: "hour", Aggregation: "p90", DefinitionDigest: "sha256:def-" + workflowID}},
	}}
	experience.Content, _ = Bind(content)
	return IndustryComposition{
		Publication: PublicationCheck{Candidate: pack, Installed: map[string]string{"hcmnext": "1.8", "us-country": "1"},
			Published: []PublishedPack{{ID: "us-country", Version: "1", Digest: countryDigest}}, Content: &content, Experience: &experience},
		Content:    content,
		Experience: experience,
		Layers: LayerSpec{
			Packs: []LayerPack{
				{Layer: LayerPlatform, PackID: "platform", Version: 12, Settings: []LayerSetting{
					{Family: "session", Key: "idle_minutes", Values: []string{"30"}, Mandatory: true, Class: ClassSecurity}}},
				{Layer: LayerCountry, PackID: "us-country", Version: 1, Settings: []LayerSetting{
					{Family: "retention", Key: "document_classes", Values: []string{"i9", "w4"}, Mandatory: true, Class: ClassLegal}}},
				{Layer: LayerIndustry, PackID: packID, Version: 1, Settings: []LayerSetting{
					{Family: "retention", Key: "document_classes", Values: []string{retained}},
					{Family: "session", Key: "idle_minutes", Values: []string{"20"}}}},
				{Layer: LayerCustomer, PackID: "acme", Version: 4, Settings: []LayerSetting{
					{Family: "branding", Key: "theme", Values: []string{"acme-dark"}},
					{Family: "session", Key: "idle_minutes", Values: []string{"15"}}}},
			},
			Policies: []FamilyPolicy{
				{Family: "session", Strategy: StrategyRestrictive, LowerIsStricter: true},
				{Family: "retention", Strategy: StrategyAdditive},
				{Family: "branding", Strategy: StrategyPrecedence},
			},
		},
	}
}

func representativeIndustries(t *testing.T) map[Industry]IndustryComposition {
	t.Helper()
	return map[Industry]IndustryComposition{
		IndustryHealthcare:    industryComposition(t, IndustryHealthcare, "healthcare", "credential-promotion", "license-verification"),
		IndustryRetail:        industryComposition(t, IndustryRetail, "retail", "seasonal-hire", "minor-work-permit"),
		IndustryManufacturing: industryComposition(t, IndustryManufacturing, "manufacturing", "shift-certification", "osha-training"),
	}
}

func signedFor(res IndustryResult) ActivationRequest {
	target := PackTarget{Tenant: "acme", Cell: "cell-us-1"}
	env := SignPackVersion(SignedPackVersion{PackID: res.PackID, Version: res.Version, BundleDigest: res.BundleDigest, Target: target,
		EffectiveAt: publishNow.Add(time.Hour), RollbackVersion: 0, Publisher: "release:ana", Approver: "release:ben", SignedAt: publishNow.Add(-time.Minute)},
		"pack-2026", publishKey)
	return ActivationRequest{Envelope: env, Target: target, TrustedKeys: map[string]ed25519.PublicKey{"pack-2026": publishKey.Public().(ed25519.PublicKey)},
		Now: publishNow, MaxAge: 24 * time.Hour}
}

func wantConformanceRejection(t *testing.T, err error, stage, field, state, version string) {
	t.Helper()
	var r *ConformanceRejection
	if !errors.As(err, &r) || !errors.Is(err, ErrConformanceRejected) || r.Code != ConformanceRejectionCode || !strings.Contains(r.Error(), ConformanceRejectionCode) {
		t.Fatalf("err = %v, want %s", err, ConformanceRejectionCode)
	}
	if r.Stage != stage || r.Field != field || r.State != state || r.Version != version {
		t.Fatalf("rejection = %s.%s %s@%s, want %s.%s %s@%s (%v)", r.Stage, r.Field, r.State, r.Version, stage, field, state, version, r.Cause)
	}
}

// TestTodo_PACK_007 proves Healthcare, Retail and Manufacturing each combine
// with one country and one customer pack, preserve the mandatory rules and
// activate from a signed bundle; an unknown dependency or a mandatory country
// rule override is PACK_007_REJECTED with zero effects.
func TestTodo_PACK_007(t *testing.T) {
	ctx := context.Background()
	for industry, spec := range representativeIndustries(t) {
		res, err := ComposeIndustry(spec)
		if err != nil {
			t.Fatalf("%s: %v", industry, err)
		}
		store := &countingIndustryActivations{}
		activated, receipt, effects, err := ActivateIndustry(ctx, store, spec, signedFor(res))
		if err != nil || store.calls != 1 || effects.AuthoritativeRows != 3 || activated.BundleDigest != res.BundleDigest || receipt.BundleDigest != res.BundleDigest {
			t.Fatalf("%s: activate = %+v, %+v, %v", industry, receipt, effects, err)
		}
		mandatory := strings.Join(res.Mandatory, ";")
		for _, want := range []string{"rule:RULE|us/overtime@2026.1=sha256:us-overtime", "setting:SECURITY:session.idle_minutes=15", "setting:LEGAL:retention.document_classes="} {
			if !strings.Contains(mandatory, want) {
				t.Fatalf("%s: mandatory %q lacks %q", industry, mandatory, want)
			}
		}
		if !strings.Contains(mandatory, "i9") || !strings.Contains(mandatory, "w4") {
			t.Fatalf("%s: legal retention lost: %s", industry, mandatory)
		}
	}

	unknown := industryComposition(t, IndustryRetail, "retail", "seasonal-hire", "minor-work-permit")
	unknown.Experience.Registry[0].Dependencies = append(unknown.Experience.Registry[0].Dependencies, ExperienceRef{Kind: ExperienceSkill, ID: "background-check", Version: "1"})
	unknown.Publication.Experience = &unknown.Experience
	override := industryComposition(t, IndustryManufacturing, "manufacturing", "shift-certification", "osha-training")
	override.Experience.Registry[1].OverridesContent = &usOvertimeRule
	override.Publication.Experience = &override.Experience
	for name, tc := range map[string]struct {
		spec                         IndustryComposition
		stage, field, state, version string
	}{
		"unknown dependency":              {unknown, "publication", "WORKFLOW:seasonal-hire.dependencies", StateUnknownDependency, "1"},
		"mandatory country rule override": {override, "publication", "FORM:shift-certification-form.overrides_content", StateMandatoryOverride, "1"},
	} {
		store := &countingIndustryActivations{}
		_, _, effects, err := ActivateIndustry(ctx, store, tc.spec, ActivationRequest{})
		wantConformanceRejection(t, err, tc.stage, tc.field, tc.state, tc.version)
		if store.calls != 0 || effects != (ActivationEffects{}) {
			t.Fatalf("%s: a rejected composition persisted", name)
		}
	}
}

// TestTodo_PACK_007_Golden pins each industry's deterministic config,
// workflow, model and bundle digests.
func TestTodo_PACK_007_Golden(t *testing.T) {
	var lines []string
	for _, industry := range []Industry{IndustryHealthcare, IndustryManufacturing, IndustryRetail} {
		spec := representativeIndustries(t)[industry]
		res, err := ComposeIndustry(spec)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ComposeIndustry(representativeIndustries(t)[industry])
		if err != nil || again.BundleDigest != res.BundleDigest {
			t.Fatalf("%s: composition is not deterministic", industry)
		}
		lines = append(lines, fmt.Sprintf("%s config=%s workflow=%s model=%s bundle=%s", industry, res.ConfigDigest[7:19], res.WorkflowDigest[7:19], res.ModelDigest[7:19], res.BundleDigest[7:19]))
	}
	const golden = "HEALTHCARE config=02478c2188f1 workflow=3809c4724d15 model=f51e524cab5a bundle=34aca541876c\nMANUFACTURING config=10e904dbe45c workflow=5482228befb9 model=70a086a51da8 bundle=267bb871ee7b\nRETAIL config=1c431d52854b workflow=65c47dc4c832 model=23b5d01cd097 bundle=7e610fc9dc39"
	if got := strings.Join(lines, "\n"); got != golden {
		t.Fatalf("golden digests:\n%s", got)
	}
}

// TestTodo_PACK_007_Conformance proves the three industries share the
// country and platform mandatory constraints exactly while producing distinct
// industry digests.
func TestTodo_PACK_007_Conformance(t *testing.T) {
	shared := map[string]bool{}
	bundles := map[string]Industry{}
	for industry, spec := range representativeIndustries(t) {
		res, err := ComposeIndustry(spec)
		if err != nil {
			t.Fatal(err)
		}
		if res.Industry != industry {
			t.Fatalf("result industry = %s, want %s", res.Industry, industry)
		}
		var common []string
		for _, m := range res.Mandatory {
			if !strings.HasPrefix(m, "setting:LEGAL:retention") {
				common = append(common, m)
			}
		}
		shared[strings.Join(common, ";")] = true
		if prior, dup := bundles[res.BundleDigest]; dup {
			t.Fatalf("%s and %s produced the same bundle", prior, industry)
		}
		bundles[res.BundleDigest] = industry
	}
	if len(shared) != 1 || len(bundles) != 3 {
		t.Fatalf("shared mandatory sets = %d, bundles = %d", len(shared), len(bundles))
	}
}

// TestTodo_PACK_007_Mutation breaks each stage and proves the stage-qualified
// rejection and zero effects.
func TestTodo_PACK_007_Mutation(t *testing.T) {
	base := func(t *testing.T) IndustryComposition {
		return industryComposition(t, IndustryHealthcare, "healthcare", "credential-promotion", "license-verification")
	}
	cases := []struct {
		name                         string
		mutate                       func(*IndustryComposition, *ActivationRequest)
		stage, field, state, version string
	}{
		{"platform incompatible", func(s *IndustryComposition, _ *ActivationRequest) { s.Publication.Installed["hcmnext"] = "3" },
			"publication", "compatibility[0]", ImpactIncompatibleVersion, "3"},
		{"customer overrides country rule", func(s *IndustryComposition, _ *ActivationRequest) {
			v2 := usOvertimeRule
			v2.Version = "2026.1-acme"
			s.Content.Packs[2].References = append(s.Content.Packs[2].References, v2)
			s.Content.Packs[2].Contents = append(s.Content.Packs[2].Contents, Content{Ref: v2, Effective: OpenWindow(bindingDate(t, "2026-01-01")), Digest: "sha256:lax", OverrideOf: &usOvertimeRule})
			s.Publication.Content = nil
		}, "content", "RULE|us/overtime@2026.1-acme", ConformanceContentRefused, "2026.1-acme"},
		{"form accessibility missing", func(s *IndustryComposition, _ *ActivationRequest) {
			s.Experience.Registry[1].Accessibility = ""
			s.Publication.Experience = nil
		}, "experience", "form_refs[0].accessibility", StateMissingDeclaration, "1"},
		{"customer weakens security", func(s *IndustryComposition, _ *ActivationRequest) {
			s.Layers.Packs[3].Settings[1].Values = []string{"60"}
		},
			"layers", "settings[session.idle_minutes]", LayerWeakenedMandatory, "acme@4"},
		{"bundle not signed over proof", func(_ *IndustryComposition, a *ActivationRequest) {
			a.Envelope.BundleDigest = "sha256:other"
		}, "activation", "bundle_digest", ConformanceBundleMismatch, "1"},
		{"unapproved activation", func(_ *IndustryComposition, a *ActivationRequest) {
			a.Envelope.Approver = a.Envelope.Publisher
			a.Envelope = SignPackVersion(a.Envelope, "pack-2026", publishKey)
		}, "activation", "approver", SignatureUnapproved, "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := base(t)
			proven, err := ComposeIndustry(spec)
			if err != nil {
				t.Fatal(err)
			}
			act := signedFor(proven)
			tc.mutate(&spec, &act)
			store := &countingIndustryActivations{}
			_, _, effects, err := ActivateIndustry(context.Background(), store, spec, act)
			wantConformanceRejection(t, err, tc.stage, tc.field, tc.state, tc.version)
			if store.calls != 0 || effects != (ActivationEffects{}) {
				t.Fatal("a rejected composition persisted")
			}
		})
	}
	if _, _, _, err := ActivateIndustry(context.Background(), nil, base(t), ActivationRequest{}); !errors.Is(err, ErrConformanceRejected) {
		t.Fatalf("nil store = %v", err)
	}
	if err := conformanceReject("x", errors.New("plain")); !strings.Contains(err.Error(), "x.x") {
		t.Fatalf("plain rejection = %v", err)
	}
}
