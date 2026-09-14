package industrypack

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type countingPublication struct {
	calls int
	err   error
}

func (c *countingPublication) SavePublication(context.Context, string, PublicationReport) (ActivationEffects, error) {
	c.calls++
	if c.err != nil {
		return ActivationEffects{}, c.err
	}
	return ActivationEffects{AuthoritativeRows: 1}, nil
}

func publicationPack(t *testing.T, version int, workflow, config string) IndustryPack {
	t.Helper()
	parentVersion, parentDigest := 0, ""
	if version > 1 {
		parentVersion = version - 1
		parentDigest, _ = publicationPack(t, version-1, "3", "1").Digest()
	}
	pack, err := NewIndustryPack(IndustryPack{
		PackID: "healthcare-base", Version: version, ParentVersion: parentVersion, ParentDigest: parentDigest, Industry: IndustryHealthcare,
		Owner: "hcmnext", Scope: "healthcare-us", Support: "maintained",
		Dependencies:            []PinnedRef{{ID: "us-country", Version: "2026.2", Digest: "sha256:us"}},
		WorkflowDefinitionRefs:  []PinnedRef{{ID: "credential-promotion", Version: workflow}},
		ConfigurationObjectRefs: []PinnedRef{{ID: "job-family", Version: config}},
		RulePackRefs:            []PinnedRef{{ID: "us-overtime", Version: "1"}},
		Compatibility: []CompatibilityDeclaration{
			{Component: "hcmnext", MinimumVersion: "1.4", MaximumVersion: "2"},
			{Component: "us-country", MinimumVersion: "2026.1"},
			{Component: "customer-acme", MinimumVersion: "3", MaximumVersion: "3.9"},
		},
	})
	if err != nil {
		t.Fatalf("NewIndustryPack: %v", err)
	}
	return pack
}

func publicationCheck(t *testing.T) PublicationCheck {
	t.Helper()
	prior := publicationPack(t, 1, "3", "1")
	experience := experienceSpec(t)
	content := fixtureSpec(t)
	return PublicationCheck{
		Candidate:       publicationPack(t, 2, "4", "2"),
		Prior:           &prior,
		Installed:       map[string]string{"hcmnext": "1.9.2", "us-country": "2026.2", "customer-acme": "3.1"},
		Published:       []PublishedPack{{ID: "us-country", Version: "2026.2", Digest: "sha256:us"}},
		ActiveWorkflows: []ActiveWorkflow{{InstanceID: "wi-1", DefinitionID: "credential-promotion", Version: "3"}},
		Migrations: []Migration{
			{Kind: "workflow_definition", ObjectID: "credential-promotion", FromVersion: "3", ToVersion: "4", Resolved: true},
			{Kind: "configuration_object", ObjectID: "job-family", FromVersion: "1", ToVersion: "2", Resolved: true},
		},
		Content:    &content,
		Experience: &experience,
	}
}

func wantBlock(t *testing.T, err error, field, state, version string) *PublicationBlock {
	t.Helper()
	var b *PublicationBlock
	if !errors.As(err, &b) || !errors.Is(err, ErrPublicationBlocked) || b.Code != PublicationRejectionCode {
		t.Fatalf("err = %v, want %s", err, PublicationRejectionCode)
	}
	if b.Field != field || b.State != state || b.Version != version || !strings.Contains(b.Error(), PublicationRejectionCode) {
		t.Fatalf("block = %+v, want %s %s@%s", b, field, state, version)
	}
	return b
}

// TestTodo_PACK_004 proves a compatible candidate with resolved migrations
// publishes, and that an unknown dependency or a mandatory country rule
// override blocks with PACK_004_REJECTED and persists nothing.
func TestTodo_PACK_004(t *testing.T) {
	ctx := context.Background()
	store := &countingPublication{}
	report, effects, err := PublishChecked(ctx, store, "acme", publicationCheck(t))
	if err != nil || store.calls != 1 || effects.AuthoritativeRows != 1 || report.Version != 2 || len(report.Changed) != 2 || !strings.HasPrefix(report.Digest, "sha256:") {
		t.Fatalf("publish = %+v, %+v, %v (calls %d)", report, effects, err, store.calls)
	}

	unknown := publicationCheck(t)
	unknown.Published = nil
	mandatory := publicationCheck(t)
	mandatory.Experience.Registry[1].OverridesContent = &promotionRule
	for name, tc := range map[string]struct {
		check                 PublicationCheck
		field, state, version string
	}{
		"unknown dependency":              {unknown, "dependencies[0]", ImpactMissingDependency, "2026.2"},
		"mandatory country rule override": {mandatory, "FORM:credential-attestation.overrides_content", StateMandatoryOverride, "2"},
	} {
		store := &countingPublication{}
		_, effects, err := PublishChecked(ctx, store, "acme", tc.check)
		wantBlock(t, err, tc.field, tc.state, tc.version)
		if store.calls != 0 || effects != (ActivationEffects{}) {
			t.Fatalf("%s: a block persisted", name)
		}
	}
}

// TestTodo_PACK_004_Golden pins the complete, ordered impact list of a
// candidate that breaks every gate at once.
func TestTodo_PACK_004_Golden(t *testing.T) {
	check := publicationCheck(t)
	check.Installed = map[string]string{"hcmnext": "2.1", "us-country": "2026.2"}
	check.Published = nil
	check.Migrations = nil
	check.Content, check.Experience = nil, nil
	_, err := CheckPublication(check)
	b := wantBlock(t, err, "compatibility[0]", ImpactIncompatibleVersion, "2.1")
	var got []string
	for _, i := range b.Impacted {
		got = append(got, i.Field+"|"+i.Object+"|"+i.State+"|"+i.Version)
	}
	want := strings.Join([]string{
		"compatibility[0]|hcmnext|INCOMPATIBLE_VERSION|2.1",
		"compatibility[2]|customer-acme|COMPONENT_NOT_INSTALLED|",
		"configuration_object_refs|job-family|UNRESOLVED_MIGRATION|1",
		"dependencies[0]|us-country|MISSING_DEPENDENCY|2026.2",
		"workflow_definition_refs|credential-promotion#wi-1|ACTIVE_WORKFLOW_BREAK|3",
	}, "\n")
	if strings.Join(got, "\n") != want {
		t.Fatalf("impact list:\n%s\nwant:\n%s", strings.Join(got, "\n"), want)
	}
}

// TestTodo_PACK_004_Conformance proves the version-range semantics and that
// a first publication (no prior) has no migration or workflow impact.
func TestTodo_PACK_004_Conformance(t *testing.T) {
	for _, tc := range []struct {
		v, lo, hi string
		want      bool
	}{
		{"1.4", "1.4", "2", true}, {"2.0.0", "1.4", "2", true}, {"2.0.1", "1.4", "2", false}, {"1.3.9", "1.4", "", false},
		{"10", "9", "", true}, {"1", "", "", true}, {"1.x", "1", "", false}, {"1", "1.x", "", false}, {"1", "", "x", false}, {"-1", "", "", true},
	} {
		if got := versionInRange(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Errorf("versionInRange(%q, %q, %q) = %v", tc.v, tc.lo, tc.hi, got)
		}
	}
	if _, ok := versionParts("-1"); ok {
		t.Fatal("negative version parsed")
	}
	first := publicationCheck(t)
	first.Prior, first.Migrations, first.ActiveWorkflows = nil, nil, nil
	if r, err := CheckPublication(first); err != nil || len(r.Changed) != 0 {
		t.Fatalf("first publication = %+v, %v", r, err)
	}
	dropped := publicationCheck(t)
	dropped.Candidate = publicationPack(t, 2, "3", "1")
	dropped.Candidate.WorkflowDefinitionRefs = nil
	dropped.Candidate.CanonicalDigest = ""
	if dropped.Candidate, _ = NewIndustryPack(dropped.Candidate); true {
		_, err := CheckPublication(dropped)
		b := wantBlock(t, err, "workflow_definition_refs", ImpactActiveWorkflowBreak, "3")
		if !strings.Contains(b.Impacted[0].Detail, "drops it") {
			t.Fatalf("detail = %q", b.Impacted[0].Detail)
		}
	}
}

// TestTodo_PACK_004_Mutation breaks each gate in turn and proves the exact
// first impacted object and zero persistence.
func TestTodo_PACK_004_Mutation(t *testing.T) {
	cases := []struct {
		name                  string
		mutate                func(*PublicationCheck)
		field, state, version string
	}{
		{"invalid manifest", func(c *PublicationCheck) { c.Candidate.Owner = "" }, "manifest", ImpactInvalidManifest, "2"},
		{"platform too new", func(c *PublicationCheck) { c.Installed["hcmnext"] = "2.0.1" }, "compatibility[0]", ImpactIncompatibleVersion, "2.0.1"},
		{"country too old", func(c *PublicationCheck) { c.Installed["us-country"] = "2025.9" }, "compatibility[1]", ImpactIncompatibleVersion, "2025.9"},
		{"customer missing", func(c *PublicationCheck) { delete(c.Installed, "customer-acme") }, "compatibility[2]", ImpactComponentMissing, ""},
		{"dependency digest drift", func(c *PublicationCheck) { c.Published[0].Digest = "sha256:other" }, "dependencies[0]", ImpactMissingDependency, "2026.2"},
		{"unresolved workflow migration", func(c *PublicationCheck) { c.Migrations[0].Resolved = false }, "workflow_definition_refs", ImpactActiveWorkflowBreak, "3"},
		{"unresolved config migration", func(c *PublicationCheck) { c.Migrations[1].Resolved = false }, "configuration_object_refs", ImpactUnresolvedMigration, "1"},
		{"rule pack re-versioned", func(c *PublicationCheck) {
			c.Candidate.RulePackRefs = []PinnedRef{{ID: "us-overtime", Version: "2"}}
			c.Candidate.CanonicalDigest = ""
			c.Candidate, _ = NewIndustryPack(c.Candidate)
		}, "rule_pack_refs", ImpactUnresolvedMigration, "1"},
		{"stale parent", func(c *PublicationCheck) {
			other := publicationPack(t, 1, "3", "2")
			c.Prior = &other
		}, "parent_digest", ImpactStaleParent, "1"},
		{"content rejected", func(c *PublicationCheck) { c.Content.Packs = nil }, "content", ImpactBindingRejected, "2"},
		{"experience wiring", func(c *PublicationCheck) { c.Experience.DefaultLocale = "" }, "default_locale", StateMissingDeclaration, "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := publicationCheck(t)
			tc.mutate(&check)
			store := &countingPublication{}
			_, effects, err := PublishChecked(context.Background(), store, "acme", check)
			wantBlock(t, err, tc.field, tc.state, tc.version)
			if store.calls != 0 || effects != (ActivationEffects{}) {
				t.Fatal("a blocked publication persisted")
			}
		})
	}
	if _, _, err := PublishChecked(context.Background(), nil, "acme", publicationCheck(t)); !errors.Is(err, ErrPublicationBlocked) {
		t.Fatalf("nil store = %v", err)
	}
	boom := errors.New("db down")
	if _, _, err := PublishChecked(context.Background(), &countingPublication{err: boom}, "acme", publicationCheck(t)); !errors.Is(err, boom) {
		t.Fatalf("store failure = %v", err)
	}
}
