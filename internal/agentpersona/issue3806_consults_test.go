package agentpersona

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
)

type consultCatalog map[string]ConsultTarget

func (c consultCatalog) ResolveConsult(ref ConsultRef) (ConsultTarget, error) {
	target, ok := c[ref.PersonaID]
	if !ok || target.Version.Profile.Version != ref.Version {
		return ConsultTarget{}, ErrNotFound
	}
	return target, nil
}

func consultSpecialist(t *testing.T, validator Validator, id string, audience Audience) PersonaVersion {
	t.Helper()
	profile, _ := personaFixture(t)
	profile.PersonaID, profile.Handle, profile.DisplayName = id, id, "Specialist "+id
	profile.Audience = audience
	return mustPersonaVersion(t, profile, validator)
}

func consultFixture(t *testing.T) (PersonaProfile, Validator, consultCatalog, PersonaVersion, PersonaVersion) {
	t.Helper()
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	comp := consultSpecialist(t, validator, "persona.comp", profile.Audience)
	legal := consultSpecialist(t, validator, "persona.legal", profile.Audience)
	resolver := consultCatalog{
		comp.Profile.PersonaID:  {Version: comp, State: StatePublished},
		legal.Profile.PersonaID: {Version: legal, State: StatePublished},
	}
	validator.Consults = resolver
	profile.Consults = []ConsultRef{
		{PersonaID: comp.Profile.PersonaID, Version: comp.Profile.Version, Digest: comp.Digest},
		{PersonaID: legal.Profile.PersonaID, Version: legal.Profile.Version, Digest: legal.Digest},
	}
	return profile, validator, resolver, comp, legal
}

func TestIssue3806_ConsultsArePinnedAndSealedIntoTheDigest(t *testing.T) {
	profile, validator, _, _, _ := consultFixture(t)
	withConsults := mustPersonaVersion(t, profile, validator)
	if len(withConsults.Profile.Consults) != 2 {
		t.Fatalf("consults not kept: %+v", withConsults.Profile.Consults)
	}
	plain := profile
	plain.Consults = nil
	without := mustPersonaVersion(t, plain, validator)
	if withConsults.Digest == without.Digest {
		t.Fatal("consult list does not change the reviewed digest")
	}
	withConsults.Profile.Consults[0].Version = 9
	if err := withConsults.Verify(); err == nil {
		t.Fatal("changing a pinned consult kept the seal")
	}
}

func TestIssue3806_ConsultsRefuseBadLists(t *testing.T) {
	profile, validator, resolver, comp, legal := consultFixture(t)
	ref := func(v PersonaVersion) ConsultRef {
		return ConsultRef{PersonaID: v.Profile.PersonaID, Version: v.Profile.Version, Digest: v.Digest}
	}
	cases := map[string]func(*PersonaProfile, Validator) Validator{
		"three entries": func(p *PersonaProfile, v Validator) Validator {
			p.Consults = append(p.Consults, ConsultRef{PersonaID: "persona.third", Version: 1, Digest: "sha256:x"})
			return v
		},
		"duplicate": func(p *PersonaProfile, v Validator) Validator {
			p.Consults = []ConsultRef{ref(comp), ref(comp)}
			return v
		},
		"itself": func(p *PersonaProfile, v Validator) Validator {
			p.Consults = []ConsultRef{{PersonaID: p.PersonaID, Version: p.Version, Digest: "sha256:x"}}
			return v
		},
		"unpinned version": func(p *PersonaProfile, v Validator) Validator {
			p.Consults = []ConsultRef{{PersonaID: comp.Profile.PersonaID, Digest: comp.Digest}}
			return v
		},
		"missing digest": func(p *PersonaProfile, v Validator) Validator {
			p.Consults = []ConsultRef{{PersonaID: comp.Profile.PersonaID, Version: 1}}
			return v
		},
		"wrong digest": func(p *PersonaProfile, v Validator) Validator {
			p.Consults = []ConsultRef{{PersonaID: comp.Profile.PersonaID, Version: 1, Digest: "sha256:other"}}
			return v
		},
		"unknown persona": func(p *PersonaProfile, v Validator) Validator {
			p.Consults = []ConsultRef{{PersonaID: "persona.ghost", Version: 1, Digest: "sha256:x"}}
			return v
		},
		"unpublished specialist": func(p *PersonaProfile, v Validator) Validator {
			v.Consults = consultCatalog{comp.Profile.PersonaID: {Version: comp, State: StateDraft}, legal.Profile.PersonaID: resolver[legal.Profile.PersonaID]}
			return v
		},
		"no resolver": func(p *PersonaProfile, v Validator) Validator {
			v.Consults = nil
			return v
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := profile
			p.Consults = append([]ConsultRef(nil), profile.Consults...)
			v := mutate(&p, validator)
			if _, err := v.Build(p); !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("Build = %v, want ErrInvalidProfile", err)
			}
		})
	}
}

func TestIssue3806_ConsultSpecialistMustServeEveryoneWhoCanAskTheParent(t *testing.T) {
	profile, validator, resolver, comp, _ := consultFixture(t)
	narrow := Audience{Roles: []string{"manager"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-east"}}
	_, wide := personaFixture(t)
	for key, grant := range wide.grants {
		grant.OrganizationScopes = append(grant.OrganizationScopes, "org-east")
		wide.grants[key] = grant
	}
	narrowed := consultSpecialist(t, personaValidator(wide), "persona.comp", narrow)
	resolver[comp.Profile.PersonaID] = ConsultTarget{Version: narrowed, State: StatePublished}
	profile.Consults = []ConsultRef{{PersonaID: narrowed.Profile.PersonaID, Version: narrowed.Profile.Version, Digest: narrowed.Digest}}
	if _, err := validator.Build(profile); !errors.Is(err, ErrInvalidProfile) || !errors.Is(err, ErrAudience) {
		t.Fatalf("narrower specialist audience Build = %v", err)
	}
}

func TestIssue3806_ConsultSpecialistMayNotConsultAgain(t *testing.T) {
	profile, validator, resolver, comp, _ := consultFixture(t)
	chainedProfile, _ := personaFixture(t)
	chainedProfile.PersonaID, chainedProfile.Handle = "persona.comp", "persona.comp"
	chainedProfile.Consults = []ConsultRef{{PersonaID: "persona.legal", Version: 1, Digest: comp.Digest}}
	chained, err := Seal(chainedProfile)
	if err != nil {
		t.Fatal(err)
	}
	resolver[comp.Profile.PersonaID] = ConsultTarget{Version: chained, State: StatePublished}
	profile.Consults = []ConsultRef{{PersonaID: chained.Profile.PersonaID, Version: chained.Profile.Version, Digest: chained.Digest}}
	if _, err := validator.Build(profile); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("depth-2 chain Build = %v", err)
	}
}

func TestIssue3806_ConsultRuntimeRefusesUnlistedAndDeeperCalls_Security(t *testing.T) {
	profile, validator, _, comp, _ := consultFixture(t)
	parent := mustPersonaVersion(t, profile, validator)
	listed := ConsultRef{PersonaID: comp.Profile.PersonaID, Version: comp.Profile.Version, Digest: comp.Digest}
	if _, err := AuthorizeConsult(parent, 0, listed); err != nil {
		t.Fatalf("listed depth-0 consult refused: %v", err)
	}
	if _, err := AuthorizeConsult(parent, 1, listed); !errors.Is(err, ErrConsultDenied) {
		t.Fatalf("depth-2 consult = %v", err)
	}
	for name, ref := range map[string]ConsultRef{
		"unlisted persona": {PersonaID: "persona.payroll", Version: 1, Digest: "sha256:x"},
		"other version":    {PersonaID: comp.Profile.PersonaID, Version: 2, Digest: comp.Digest},
		"other digest":     {PersonaID: comp.Profile.PersonaID, Version: comp.Profile.Version, Digest: "sha256:forged"},
	} {
		if _, err := AuthorizeConsult(parent, 0, ref); !errors.Is(err, ErrConsultDenied) {
			t.Fatalf("%s = %v", name, err)
		}
	}
	plain := profile
	plain.Consults = nil
	noConsults := mustPersonaVersion(t, plain, validator)
	if _, err := AuthorizeConsult(noConsults, 0, listed); !errors.Is(err, ErrConsultDenied) {
		t.Fatalf("persona without a list consulted: %v", err)
	}
}

func TestIssue3806_ConsultRuntimeRefusesRetiredOrSwappedSpecialist_Security(t *testing.T) {
	profile, validator, resolver, comp, _ := consultFixture(t)
	parent := mustPersonaVersion(t, profile, validator)
	listed := ConsultRef{PersonaID: comp.Profile.PersonaID, Version: comp.Profile.Version, Digest: comp.Digest}
	if got, err := AuthorizeAndResolveConsult(parent, 0, listed, resolver); err != nil || got.Digest != comp.Digest {
		t.Fatalf("published listed specialist refused: %v", err)
	}
	if _, err := AuthorizeAndResolveConsult(parent, -1, listed, resolver); !errors.Is(err, ErrConsultDenied) {
		t.Fatalf("negative depth = %v", err)
	}
	if _, err := AuthorizeAndResolveConsult(parent, 0, listed, nil); !errors.Is(err, ErrConsultDenied) {
		t.Fatalf("no resolver = %v", err)
	}
	retired := consultCatalog{comp.Profile.PersonaID: {Version: comp, State: StateRetired}}
	if _, err := AuthorizeAndResolveConsult(parent, 0, listed, retired); !errors.Is(err, ErrConsultDenied) {
		t.Fatalf("retired specialist = %v", err)
	}
	other := consultSpecialist(t, validator, "persona.comp", profile.Audience)
	other.Profile.DisplayName = "Forged after sealing"
	swapped := consultCatalog{comp.Profile.PersonaID: {Version: other, State: StatePublished}}
	if _, err := AuthorizeAndResolveConsult(parent, 0, listed, swapped); !errors.Is(err, ErrConsultDenied) {
		t.Fatalf("swapped specialist = %v", err)
	}
	unlisted := ConsultRef{PersonaID: "persona.payroll", Version: 1, Digest: "sha256:x"}
	if _, err := AuthorizeAndResolveConsult(parent, 0, unlisted, resolver); !errors.Is(err, ErrConsultDenied) {
		t.Fatalf("unlisted specialist = %v", err)
	}
}

func TestIssue3806_ConsultBudgetSplitNeverExceedsTheParent(t *testing.T) {
	parent := agentbudget.Limits{Steps: 10, Tokens: 9000, WallClock: 90 * time.Second, SpendMicros: 3000}
	shares, err := SplitConsultBudget(parent, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares.Children) != 2 {
		t.Fatalf("children = %d", len(shares.Children))
	}
	sum := shares.Parent
	for _, c := range shares.Children {
		sum.Steps += c.Steps
		sum.Tokens += c.Tokens
		sum.WallClock += c.WallClock
		sum.SpendMicros += c.SpendMicros
		if c.Steps <= 0 || c.Tokens <= 0 {
			t.Fatalf("child share is empty: %+v", c)
		}
	}
	if sum.Steps > parent.Steps || sum.Tokens > parent.Tokens || sum.WallClock > parent.WallClock || sum.SpendMicros > parent.SpendMicros {
		t.Fatalf("split %+v exceeds parent %+v", sum, parent)
	}
	if shares.Parent.Tokens <= 0 {
		t.Fatal("the parent keeps nothing to write the answer")
	}
	if _, err := SplitConsultBudget(agentbudget.Limits{Steps: 1, Tokens: 1}, 2); !errors.Is(err, ErrConsultDenied) {
		t.Fatalf("starved split = %v", err)
	}
	if _, err := SplitConsultBudget(parent, 3); !errors.Is(err, ErrConsultDenied) {
		t.Fatalf("three consults = %v", err)
	}
}

func TestIssue3806_ConsultedAnswerAudienceFloorIsTheIntersection_Security(t *testing.T) {
	parent := Audience{Roles: []string{"manager", "hr"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-west", "org-east"}}
	specialist := Audience{Roles: []string{"hr"}, Populations: []string{"*"}, OrganizationScopes: []string{"org-west"}}
	got := CombinedAudience(parent, specialist)
	if !equalStrings(got.Roles, []string{"hr"}) || !equalStrings(got.Populations, []string{"employees"}) || !equalStrings(got.OrganizationScopes, []string{"org-west"}) {
		t.Fatalf("combined audience = %+v", got)
	}
	disjoint := CombinedAudience(parent, Audience{Roles: []string{"contractor"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-west"}})
	if len(disjoint.Roles) != 0 {
		t.Fatalf("disjoint roles must leave nobody: %+v", disjoint)
	}
	if AudienceAllowsAnyone(disjoint) {
		t.Fatal("empty floor must not be presented as an audience")
	}
}

func TestIssue3806_ManagerResolvesOnlyThePublishedCurrentVersion(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	version := mustPersonaVersion(t, profile, validator)
	manager := NewManager(Dependencies{Validator: validator, Reviews: &mutablePersonaReviewAuthority{allowed: true}})
	if err := manager.Register(version); err != nil {
		t.Fatal(err)
	}
	ref := ConsultRef{PersonaID: profile.PersonaID, Version: profile.Version, Digest: version.Digest}
	if target, err := manager.ResolveConsult(ref); err != nil || target.State == StatePublished {
		t.Fatalf("draft resolved as %+v, %v", target, err)
	}
	if _, err := manager.ResolveConsult(ConsultRef{PersonaID: "persona.none", Version: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
	review := mustReview(t, manager, version)
	manager.deps.Evaluations = fixedPersonaEvaluationStore{record: evaluationFor(version)}
	if _, err := manager.Publish(profile.PersonaID, profile.Version, PublishRequest{Review: review, EvaluationDigest: "eval-run-1"}); err != nil {
		t.Fatal(err)
	}
	target, err := manager.ResolveConsult(ref)
	if err != nil || target.State != StatePublished || target.Version.Digest != version.Digest {
		t.Fatalf("published resolved as %+v, %v", target, err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
