package industrypack

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
)

type countingActivation struct {
	calls int
	saved ExperienceBinding
	err   error
}

func (c *countingActivation) SaveExperienceBinding(_ context.Context, _ string, b ExperienceBinding) (ActivationEffects, error) {
	c.calls++
	c.saved = b
	if c.err != nil {
		return ActivationEffects{}, c.err
	}
	return ActivationEffects{AuthoritativeRows: 1 + len(b.Artifacts)}, nil
}

var (
	promotionRule    = ContentRef{Kind: ContentRule, Namespace: "healthcare", ID: "promotion-approval", Version: "2026.1"}
	annualizeFormula = ContentRef{Kind: ContentFormula, Namespace: "healthcare", ID: "annualize", Version: "1"}
)

func experienceSpec(t *testing.T) ExperienceBindingSpec {
	t.Helper()
	content, err := Bind(fixtureSpec(t))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	pack := bindingManifest(t, "healthcare-base")
	pack.WorkflowDefinitionRefs = []PinnedRef{{ID: "credential-promotion", Version: "3", Digest: "sha256:wf3"}}
	pack.FormRefs = []PinnedRef{{ID: "credential-attestation", Version: "2", Digest: "sha256:form2"}}
	pack.SkillRefs = []PinnedRef{{ID: "licence-lookup", Version: "1"}}
	pack.MetricRefs = []PinnedRef{{ID: "time-to-credential", Version: "1", Digest: "sha256:m1"}}
	if pack, err = NewIndustryPack(pack); err != nil {
		t.Fatalf("NewIndustryPack: %v", err)
	}
	form := ExperienceRef{Kind: ExperienceForm, ID: "credential-attestation", Version: "2"}
	skill := ExperienceRef{Kind: ExperienceSkill, ID: "licence-lookup", Version: "1"}
	return ExperienceBindingSpec{
		Pack:          pack,
		Content:       content,
		DefaultLocale: "en-US",
		Registry: []ExperienceArtifact{
			{Ref: ExperienceRef{Kind: ExperienceWorkflow, ID: "credential-promotion", Version: "3"}, Digest: "sha256:wf3",
				SideEffects: []string{"PROMOTION_EFFECTIVE"}, AuthorityScopes: []string{"workforce.promotion.execute"}, Locales: []string{"en-US", "es-US"},
				Dependencies: []ExperienceRef{form, skill}, ContentDependencies: []ContentRef{promotionRule}},
			{Ref: form, Digest: "sha256:form2", SideEffects: []string{"ATTESTATION_RECORDED"}, AuthorityScopes: []string{"workforce.credential.attest"},
				Locales: []string{"en-US"}, Accessibility: "WCAG_2_2_AA"},
			{Ref: skill, Digest: "sha256:skill1", SideEffects: []string{SideEffectNone}, Locales: []string{"en-US"}, RequiredCapabilities: []string{"licence-registry-provider"}},
			{Ref: ExperienceRef{Kind: ExperienceMetric, ID: "time-to-credential", Version: "1"}, Digest: "sha256:m1", SideEffects: []string{SideEffectNone},
				Locales: []string{"en-US"}, Metric: &MetricDefinition{Unit: "day", Aggregation: "p50", DefinitionDigest: "sha256:def1"}, ContentDependencies: []ContentRef{annualizeFormula}},
			// A published artifact the pack does not reference never binds.
			{Ref: ExperienceRef{Kind: ExperienceSkill, ID: "unreferenced", Version: "1"}, Digest: "sha256:x"},
		},
		DeferredCapabilities: []string{"licence-registry-provider"},
	}
}

func artifactByID(t *testing.T, b ExperienceBinding, id string) BoundExperience {
	t.Helper()
	for _, a := range b.Artifacts {
		if a.Ref.ID == id {
			return a
		}
	}
	t.Fatalf("artifact %s not bound", id)
	return BoundExperience{}
}

func wantRejection(t *testing.T, err error, field, state, version string) *ExperienceRejection {
	t.Helper()
	var r *ExperienceRejection
	if !errors.As(err, &r) || !errors.Is(err, ErrExperienceRejected) {
		t.Fatalf("err = %v, want %s", err, RejectionCode)
	}
	if r.Code != RejectionCode || r.Field != field || r.State != state || r.Version != version {
		t.Fatalf("rejection = %+v, want field=%s state=%s version=%s", r, field, state, version)
	}
	if !strings.Contains(r.Error(), RejectionCode) {
		t.Fatalf("message %q lacks the code", r.Error())
	}
	return r
}

// TestTodo_PACK_003 proves every workflow, form, skill and metric reference
// resolves against the registry with explicit declarations, that dependencies
// on bound artifacts and PACK-002 content are type-checked, that a deferred
// capability stays gated, and that an unknown dependency or a mandatory
// country rule override is PACK_003_REJECTED with zero side effects.
func TestTodo_PACK_003(t *testing.T) {
	ctx := context.Background()
	store := &countingActivation{}
	b, effects, err := ActivateExperience(ctx, store, "acme", experienceSpec(t))
	if err != nil {
		t.Fatalf("ActivateExperience: %v", err)
	}
	if len(b.Artifacts) != 4 || store.calls != 1 || effects.AuthoritativeRows != 5 || !strings.HasPrefix(b.Digest, "sha256:") {
		t.Fatalf("binding = %+v, effects %+v, calls %d", b, effects, store.calls)
	}
	if wf := artifactByID(t, b, "credential-promotion"); !wf.Active || wf.Gate != "" {
		t.Fatalf("workflow = %+v", wf)
	}
	if skill := artifactByID(t, b, "licence-lookup"); skill.Active || skill.Gate != "DEFERRED_CAPABILITY:licence-registry-provider" {
		t.Fatalf("a skill needing a deferred capability bound active: %+v", skill)
	}

	unknown := experienceSpec(t)
	unknown.Registry[0].Dependencies = append(unknown.Registry[0].Dependencies, ExperienceRef{Kind: ExperienceForm, ID: "missing-form", Version: "9"})
	mandatory := experienceSpec(t)
	mandatory.Registry[1].OverridesContent = &promotionRule
	for name, tc := range map[string]struct {
		spec                  ExperienceBindingSpec
		field, state, version string
	}{
		"unknown artifact dependency":     {unknown, "WORKFLOW:credential-promotion.dependencies", StateUnknownDependency, "3"},
		"mandatory country rule override": {mandatory, "FORM:credential-attestation.overrides_content", StateMandatoryOverride, "2"},
	} {
		store := &countingActivation{}
		_, effects, err := ActivateExperience(ctx, store, "acme", tc.spec)
		wantRejection(t, err, tc.field, tc.state, tc.version)
		if store.calls != 0 || effects != (ActivationEffects{}) {
			t.Fatalf("%s: rejection persisted: calls=%d effects=%+v", name, store.calls, effects)
		}
	}
}

// TestTodo_PACK_003_Golden pins the canonical digest of the fixture binding
// and proves it is independent of registry order.
func TestTodo_PACK_003_Golden(t *testing.T) {
	spec := experienceSpec(t)
	b, err := BindExperience(spec)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(spec.Registry)-1; i < j; i, j = i+1, j-1 {
		spec.Registry[i], spec.Registry[j] = spec.Registry[j], spec.Registry[i]
	}
	again, err := BindExperience(spec)
	if err != nil || again.Digest != b.Digest {
		t.Fatalf("digest depends on registry order: %s vs %s (%v)", again.Digest, b.Digest, err)
	}
	var order []string
	for _, a := range b.Artifacts {
		order = append(order, string(a.Ref.Kind)+":"+a.Ref.ID)
	}
	if got, want := strings.Join(order, ","), "FORM:credential-attestation,METRIC:time-to-credential,SKILL:licence-lookup,WORKFLOW:credential-promotion"; got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
	const golden = "sha256:0e77526910cba208810d64b89f2bd41898fddc730b1e852e9f9e8c205fa4fe8b"
	if b.Digest != golden {
		t.Fatalf("golden digest = %s, want %s", b.Digest, golden)
	}
}

// TestTodo_PACK_003_Browser proves the reviewer catalog is accessible markup:
// a labelled region in the pack locale, a captioned table with scoped headers,
// escaped content and a visible gated state.
func TestTodo_PACK_003_Browser(t *testing.T) {
	spec := experienceSpec(t)
	b, err := BindExperience(spec)
	if err != nil {
		t.Fatal(err)
	}
	b.Artifacts[0].Ref.ID = `<script>alert(1)</script>`
	page := RenderCatalogHTML(b, "en-US")
	for _, want := range []string{`lang="en-US"`, `aria-labelledby="pack-catalog-title"`, `<h2 id="pack-catalog-title">healthcare-base v1</h2>`,
		`<caption>`, `<th scope="col">State</th>`, `<th scope="row">WORKFLOW</th>`, `Gated (DEFERRED_CAPABILITY:licence-registry-provider)`, `WCAG_2_2_AA`, `&lt;script&gt;`} {
		if !strings.Contains(page, want) {
			t.Fatalf("catalog lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "<script>") {
		t.Fatal("catalog rendered unescaped artifact content")
	}
	if rows := len(regexp.MustCompile(`<tr><th scope="row">`).FindAllString(page, -1)); rows != len(b.Artifacts) {
		t.Fatalf("rows = %d, want %d", rows, len(b.Artifacts))
	}
}

// TestTodo_PACK_003_Mutation removes or corrupts each declaration and
// dependency in turn; every mutant is rejected with the exact field, state
// and version, and none activates.
func TestTodo_PACK_003_Mutation(t *testing.T) {
	overridable := ContentRef{Kind: ContentReferenceData, Namespace: "healthcare", ID: "job-family", Version: "2"}
	cases := []struct {
		name                  string
		mutate                func(*ExperienceBindingSpec)
		field, state, version string
	}{
		{"invalid manifest", func(s *ExperienceBindingSpec) { s.Pack.PackID, s.Pack.ID = "", "" }, "manifest", StateInvalidManifest, "1"},
		{"no default locale", func(s *ExperienceBindingSpec) { s.DefaultLocale = " " }, "default_locale", StateMissingDeclaration, "1"},
		{"unpublished version", func(s *ExperienceBindingSpec) { s.Registry[1].Ref.Version = "7" }, "form_refs[0]", StateUnresolved, "2"},
		{"digest drift", func(s *ExperienceBindingSpec) { s.Registry[0].Digest = "sha256:other" }, "workflow_definition_refs[0]", StateDigestMismatch, "3"},
		{"no digest", func(s *ExperienceBindingSpec) { s.Pack.SkillRefs[0].Digest = ""; s.Registry[2].Digest = "" }, "skill_refs[0].digest", StateMissingDeclaration, "1"},
		{"undeclared side effects", func(s *ExperienceBindingSpec) { s.Registry[3].SideEffects = nil }, "metric_refs[0].side_effects", StateMissingDeclaration, "1"},
		{"blank side effect", func(s *ExperienceBindingSpec) { s.Registry[3].SideEffects = []string{" "} }, "metric_refs[0].side_effects", StateMissingDeclaration, "1"},
		{"NONE beside an effect", func(s *ExperienceBindingSpec) {
			s.Registry[1].SideEffects = []string{"ATTESTATION_RECORDED", SideEffectNone}
		}, "form_refs[0].side_effects", StateMissingDeclaration, "2"},
		{"no authority", func(s *ExperienceBindingSpec) { s.Registry[0].AuthorityScopes = nil }, "workflow_definition_refs[0].authority_scopes", StateMissingDeclaration, "3"},
		{"no default locale shipped", func(s *ExperienceBindingSpec) { s.Registry[1].Locales = []string{"fr-CA"} }, "form_refs[0].locales", StateMissingDeclaration, "2"},
		{"no accessibility", func(s *ExperienceBindingSpec) { s.Registry[1].Accessibility = "" }, "form_refs[0].accessibility", StateMissingDeclaration, "2"},
		{"no metric definition", func(s *ExperienceBindingSpec) { s.Registry[3].Metric.DefinitionDigest = "" }, "metric_refs[0].metric", StateMissingDeclaration, "1"},
		{"unknown content dependency", func(s *ExperienceBindingSpec) {
			s.Registry[3].ContentDependencies = []ContentRef{{Kind: ContentFormula, Namespace: "healthcare", ID: "annualize", Version: "2"}}
		}, "METRIC:time-to-credential.content_dependencies", StateUnknownDependency, "1"},
		{"override of absent content", func(s *ExperienceBindingSpec) {
			s.Registry[0].OverridesContent = &ContentRef{Kind: ContentRule, Namespace: "canada", ID: "promotion-approval", Version: "1"}
		}, "WORKFLOW:credential-promotion.overrides_content", StateUnknownDependency, "3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := experienceSpec(t)
			tc.mutate(&spec)
			store := &countingActivation{}
			_, effects, err := ActivateExperience(context.Background(), store, "acme", spec)
			wantRejection(t, err, tc.field, tc.state, tc.version)
			if store.calls != 0 || effects != (ActivationEffects{}) {
				t.Fatalf("mutant activated: calls=%d effects=%+v", store.calls, effects)
			}
		})
	}

	// An override of content the binding publishes as overridable binds.
	spec := experienceSpec(t)
	for i := range spec.Content.Contents {
		if spec.Content.Contents[i].Ref == overridable {
			spec.Content.Contents[i].Overridable = true
		}
	}
	spec.Registry[1].OverridesContent = &overridable
	if _, err := BindExperience(spec); err != nil {
		t.Fatalf("override of overridable content: %v", err)
	}

	// Wiring and persistence failures.
	if _, _, err := ActivateExperience(context.Background(), nil, "acme", experienceSpec(t)); !errors.Is(err, ErrExperienceRejected) {
		t.Fatalf("nil store = %v", err)
	}
	if _, _, err := ActivateExperience(context.Background(), &countingActivation{}, " ", experienceSpec(t)); !errors.Is(err, ErrExperienceRejected) {
		t.Fatalf("blank tenant = %v", err)
	}
	boom := errors.New("db down")
	if _, effects, err := ActivateExperience(context.Background(), &countingActivation{err: boom}, "acme", experienceSpec(t)); !errors.Is(err, boom) || effects != (ActivationEffects{}) {
		t.Fatalf("store failure = %v, %+v", err, effects)
	}
}
