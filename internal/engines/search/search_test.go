package search

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

const (
	searchTenant = values.TenantId("acme-search")
	searchID1    = "018f5a2e-6b3a-7c3a-8b7a-1a2b3c4d5e6f"
	searchID2    = "018f5a2e-6b3a-7c3a-8b7a-1a2b3c4d5e70"
)

func searchInstant(t *testing.T, second int64) values.Instant {
	t.Helper()
	instant, err := values.NewInstantFromUnix(second, 0)
	if err != nil {
		t.Fatal(err)
	}
	return instant
}

func searchKnownAt(t *testing.T, second int64) values.KnownAt {
	t.Helper()
	knownAt, err := values.NewKnownAt(searchInstant(t, second))
	if err != nil {
		t.Fatal(err)
	}
	return knownAt
}

func envelope(t *testing.T) QueryEnvelope {
	t.Helper()
	return QueryEnvelope{
		Tenant: searchTenant, Purpose: "people.search", Classification: dlp.ClassPublic,
		Fields:      []authz.FieldID{authz.FieldWorkerNumber, authz.FieldJobTitle},
		EffectiveAt: searchInstant(t, 100), KnownAt: searchKnownAt(t, 100),
		PolicyDigest: "policy.authz.v1", QueryDigest: "query.sha256:1",
		SemanticPlanDigest: "plan.sha256:1", IndexDigest: "index.sha256:1",
		MinimumWatermark: searchInstant(t, 90), Mode: ModeFuzzy, MaxResults: 10,
	}
}

func candidate(t *testing.T, id, text string) Candidate {
	t.Helper()
	return Candidate{
		Subject:    values.EntityRef{Tenant: searchTenant, Kind: "worker", Id: id},
		SearchText: text, SourceDigest: "source:" + id, PolicyDigest: "policy.authz.v1", Purpose: "people.search",
		Classification: dlp.ClassPublic, Fields: []authz.FieldID{authz.FieldWorkerNumber, authz.FieldJobTitle},
		EffectiveAt: searchInstant(t, 100), KnownAt: searchKnownAt(t, 100),
		Watermark: searchInstant(t, 100),
	}
}

func allowDecision(c Candidate) AuthorizationDecision {
	return AuthorizationDecision{Allowed: c.Subject.Id == searchID1, PolicyDigest: "policy.authz.v1", Evidence: "authz:" + c.Subject.Id}
}

// TestTodo_SEARCH_001 proves the authorization prefilter runs before ranking
// and binds tenant, field, purpose, classification, time and policy evidence.
func TestTodo_SEARCH_001(t *testing.T) {
	e := envelope(t)
	var ranked []Candidate
	results, proof, err := Execute(context.Background(), e, []Candidate{candidate(t, searchID2, "denied"), candidate(t, searchID1, "allowed")}, AuthorizerFunc(func(_ context.Context, _ QueryEnvelope, c Candidate) (AuthorizationDecision, error) {
		return allowDecision(c), nil
	}), RankerFunc(func(_ context.Context, _ QueryEnvelope, got []Candidate) ([]RankedCandidate, error) {
		ranked = append(ranked, got...)
		return []RankedCandidate{{Subject: got[0].Subject, Score: 7}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 1 || ranked[0].Subject.Id != searchID1 || len(results) != 1 {
		t.Fatalf("ranker/results received unauthorized data: ranked=%+v results=%+v", ranked, results)
	}
	if results[0].SourceDigest == "" || results[0].AuthorizationEvidence == "" || proof.EnvelopeDigest != e.Digest() || proof.SuppressedDigest == "" {
		t.Fatalf("missing authorization evidence: results=%+v proof=%+v", results, proof)
	}
}

func TestTodo_SEARCH_001_Golden(t *testing.T) {
	e := envelope(t)
	a, _, err := Execute(context.Background(), e, []Candidate{candidate(t, searchID1, "allowed")}, AuthorizerFunc(func(context.Context, QueryEnvelope, Candidate) (AuthorizationDecision, error) {
		return allowDecision(candidate(t, searchID1, "allowed")), nil
	}), RankerFunc(func(context.Context, QueryEnvelope, []Candidate) ([]RankedCandidate, error) {
		return []RankedCandidate{{Subject: values.EntityRef{Tenant: searchTenant, Kind: "worker", Id: searchID1}, Score: 1}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := Execute(context.Background(), e, []Candidate{candidate(t, searchID1, "allowed")}, AuthorizerFunc(func(context.Context, QueryEnvelope, Candidate) (AuthorizationDecision, error) {
		return allowDecision(candidate(t, searchID1, "allowed")), nil
	}), RankerFunc(func(context.Context, QueryEnvelope, []Candidate) ([]RankedCandidate, error) {
		return []RankedCandidate{{Subject: values.EntityRef{Tenant: searchTenant, Kind: "worker", Id: searchID1}, Score: 1}}, nil
	}))
	if err != nil || a[0].Subject.String() != b[0].Subject.String() || e.Digest() == "" || Explain() == "" || Version() != 1 {
		t.Fatalf("equivalent authorized plans diverged: a=%+v b=%+v err=%v", a, b, err)
	}
}

func TestTodo_SEARCH_001_Security(t *testing.T) {
	e := envelope(t)
	for name, mutate := range map[string]func(*QueryEnvelope){
		"wrong purpose": func(e *QueryEnvelope) { e.Purpose = "payroll.search" },
		"wrong policy":  func(e *QueryEnvelope) { e.PolicyDigest = "policy.changed" },
		"wrong tenant":  func(e *QueryEnvelope) { e.Tenant = "other-tenant" },
	} {
		t.Run(name, func(t *testing.T) {
			candidateValue := candidate(t, searchID1, "secret")
			mutate(&e)
			calls := 0
			_, _, err := Execute(context.Background(), e, []Candidate{candidateValue}, AuthorizerFunc(func(context.Context, QueryEnvelope, Candidate) (AuthorizationDecision, error) {
				calls++
				return allowDecision(candidateValue), nil
			}), RankerFunc(func(context.Context, QueryEnvelope, []Candidate) ([]RankedCandidate, error) {
				return []RankedCandidate{{Subject: candidateValue.Subject}}, nil
			}))
			if err == nil || calls != 0 {
				t.Fatalf("changed authorization context err=%v authorizer calls=%d, want refusal before authorization", err, calls)
			}
			e = envelope(t)
		})
	}
}

func TestTodo_SEARCH_001_Mutation(t *testing.T) {
	e := envelope(t)
	if _, _, err := Execute(context.Background(), e, nil, nil, RankerFunc(func(context.Context, QueryEnvelope, []Candidate) ([]RankedCandidate, error) { return nil, nil })); !errors.Is(err, ErrNoAuthorizer) {
		t.Fatalf("nil authorizer err=%v, want ErrNoAuthorizer", err)
	}
	if _, _, err := Execute(context.Background(), e, []Candidate{candidate(t, searchID1, "ok")}, AuthorizerFunc(func(context.Context, QueryEnvelope, Candidate) (AuthorizationDecision, error) {
		return allowDecision(candidate(t, searchID1, "ok")), nil
	}), nil); !errors.Is(err, ErrNoRanker) {
		t.Fatalf("nil ranker err=%v, want ErrNoRanker", err)
	}
}

func populationDefinition() population.Definition {
	return population.Definition{
		ID: "search-selection", Owner: "search", Subject: population.SubjectWorker,
		Scope:             population.Scope{Tenant: searchTenant, OrganizationScopeRef: "org:acme-search", Purpose: "people.search"},
		TemporalBasis:     population.TemporalBasisAsOfCaller,
		UnknownDisclosure: population.UnknownDisclosureExcludeAndReport,
		CountDisclosure:   population.CountDisclosureExact,
		Criteria:          population.Criteria{Root: population.Predicate{Kind: population.PredicateEquals, Field: "selected", Values: []string{"true"}}},
	}
}

func versions() population.PolicyVersions {
	return population.PolicyVersions{AuthZVersion: "policy.authz.v1", PrivacyVersion: "privacy.v1", OrganizationVersion: "org.v1", PurposeVersion: "purpose.v1"}
}

func freezeFixture(t *testing.T) FrozenSelection {
	t.Helper()
	e := envelope(t)
	results := []Result{{Subject: values.EntityRef{Tenant: searchTenant, Kind: "worker", Id: searchID1}, SourceDigest: "source:" + searchID1, PolicyDigest: e.PolicyDigest, AuthorizationEvidence: "authz:" + searchID1, Watermark: e.MinimumWatermark, Score: 3}}
	frozen, err := FreezeAuthorized(FreezeRequest{
		Envelope: e, Results: results, Selected: []values.EntityRef{results[0].Subject},
		Definition: populationDefinition(), RevisionVersion: "revision.v1", Versions: versions(),
		AsOf: e.EffectiveAt, KnownAt: e.KnownAt,
		Watermarks: map[population.SubjectKind]values.Instant{population.SubjectWorker: searchInstant(t, 100)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return frozen
}

func rev038LargeFreezeFixture(t *testing.T, count int) FrozenSelection {
	t.Helper()
	e := envelope(t)
	results := make([]Result, count)
	selected := make([]values.EntityRef, count)
	for i := range results {
		subject := values.EntityRef{Tenant: searchTenant, Kind: "worker", Id: fmt.Sprintf("018f5a2e-6b3a-7c3a-8b7a-%012x", i+1)}
		selected[i] = subject
		results[i] = Result{Subject: subject, SourceDigest: "source:" + subject.Id, PolicyDigest: e.PolicyDigest, AuthorizationEvidence: "authz:" + subject.Id, Watermark: e.MinimumWatermark}
	}
	frozen, err := FreezeAuthorized(FreezeRequest{
		Envelope: e, Results: results, Selected: selected, Definition: populationDefinition(),
		RevisionVersion: "revision.v1", Versions: versions(), AsOf: e.EffectiveAt, KnownAt: e.KnownAt,
		Watermarks: map[population.SubjectKind]values.Instant{population.SubjectWorker: e.MinimumWatermark},
	})
	if err != nil {
		t.Fatal(err)
	}
	return frozen
}

func TestTodo_REV_038_02(t *testing.T) {
	frozen := rev038LargeFreezeFixture(t, 600)
	if err := frozen.Validate(); err != nil {
		t.Fatalf("600-member selection failed paginated validation: %v", err)
	}
	action, err := PrepareAction(context.Background(), frozen, ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 1000}, searchInstant(t, 110), ActionGovernanceFunc(authorizeAction))
	if err != nil {
		t.Fatalf("PrepareAction: %v", err)
	}
	if len(action.Children) != 600 || action.EffectAuthorized {
		t.Fatalf("paginated selection children=%d effect_authorized=%v, want 600 and false", len(action.Children), action.EffectAuthorized)
	}
	seen := make(map[string]bool, len(action.Children))
	for _, child := range action.Children {
		id := child.Subject.String()
		if seen[id] {
			t.Fatalf("duplicate child subject %s", id)
		}
		seen[id] = true
	}
}

func TestTodo_REV_038_02_Security(t *testing.T) {
	frozen := rev038LargeFreezeFixture(t, 4)
	protected := frozen
	protected.Snapshot.MembershipProtected = true
	protectedErr := protected.Validate()
	if !errors.Is(protectedErr, ErrInvalidFreeze) {
		t.Fatalf("protected membership err=%v, want ErrInvalidFreeze", protectedErr)
	}
	inconsistent := frozen
	inconsistent.Snapshot.Count = values.Value(2)
	inconsistentErr := inconsistent.Validate()
	if !errors.Is(inconsistentErr, ErrInvalidFreeze) {
		t.Fatalf("inconsistent count err=%v, want ErrInvalidFreeze", inconsistentErr)
	}
	if protectedErr.Error() != inconsistentErr.Error() {
		t.Fatalf("population refusal differs by protected/count state: %q vs %q", protectedErr, inconsistentErr)
	}
}

func authorizeAction(_ context.Context, req ActionRequest) (GovernanceDecision, error) {
	return GovernanceDecision{Allowed: true, SnapshotDigest: req.SnapshotDigest, PolicyDigest: req.PolicyDigest, Evidence: "governance:action.v1", EvaluatedAt: req.At}, nil
}

// TestSearchToPopulationToBatchPreservesScopeSnapshotAndNonDisclosure proves
// search membership is frozen before a fresh, separate governance proposal is
// made, with deterministic bounded child identities and no action effect.
func TestSearchToPopulationToBatchPreservesScopeSnapshotAndNonDisclosure(t *testing.T) {
	frozen := freezeFixture(t)
	original := frozen.Snapshot.SubjectIDList()
	if len(original) != 1 || frozen.EnvelopeDigest == "" || frozen.PolicyDigest == "" {
		t.Fatalf("incomplete frozen selection: %+v", frozen)
	}
	at := searchInstant(t, 110)
	action, err := PrepareAction(context.Background(), frozen, ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 10}, at, ActionGovernanceFunc(authorizeAction))
	if err != nil {
		t.Fatal(err)
	}
	if action.Status != "PROPOSED" || action.EffectAuthorized || len(action.Children) != 1 || action.Children[0].Status != "PENDING" {
		t.Fatalf("proposal escaped its zero-effect boundary: %+v", action)
	}
	if action.Children[0].Subject.Id != searchID1 || action.Children[0].ID == "" || action.GovernanceEvidence == "" {
		t.Fatalf("proposal lost bounded child evidence: %+v", action)
	}
	frozen.Selection[0].Id = searchID2
	if frozen.Snapshot.SubjectIDList()[0] != original[0] {
		t.Fatal("mutating caller selection changed the frozen snapshot")
	}
}

func TestTodo_SEARCH_003_Property(t *testing.T) {
	a := freezeFixture(t)
	b := freezeFixture(t)
	if a.Snapshot.Digest != b.Snapshot.Digest {
		t.Fatalf("equivalent freezes differ: %s != %s", a.Snapshot.Digest, b.Snapshot.Digest)
	}
}

func TestTodo_SEARCH_003_Golden(t *testing.T) {
	action, err := PrepareAction(context.Background(), freezeFixture(t), ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 1}, searchInstant(t, 110), ActionGovernanceFunc(authorizeAction))
	if err != nil || action.Children[0].ResultDigest == "" {
		t.Fatalf("golden proposal invalid: %+v err=%v", action, err)
	}
}

func TestTodo_SEARCH_003_Race(t *testing.T) {
	frozen := freezeFixture(t)
	at := searchInstant(t, 110)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = PrepareAction(context.Background(), frozen, ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 1}, at, ActionGovernanceFunc(authorizeAction))
		}()
	}
	wg.Wait()
}

func TestTodo_SEARCH_003_Integration(t *testing.T) {
	frozen := freezeFixture(t)
	action, err := PrepareAction(context.Background(), frozen, ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 1}, searchInstant(t, 110), ActionGovernanceFunc(authorizeAction))
	if err != nil {
		t.Fatal(err)
	}
	if action.Status != "PROPOSED" || action.EffectAuthorized || len(action.Children) != 1 {
		t.Fatalf("integration proposal = %+v", action)
	}
	if action.Children[0].Subject != frozen.Selection[0] || action.Children[0].Status != "PENDING" || action.Children[0].ResultDigest == "" {
		t.Fatalf("integration child lost frozen identity or zero-effect state: %+v", action.Children[0])
	}
}

func TestQueryEnvelope_Digest_SeparatesDelimiterCollisions(t *testing.T) {
	a := envelope(t)
	b := envelope(t)
	a.QueryDigest = "q;plan=plan2"
	a.SemanticPlanDigest = "plan1"
	b.QueryDigest = "q"
	b.SemanticPlanDigest = "plan2;plan=plan1"
	if a.Digest() == b.Digest() {
		t.Fatalf("delimiter-collision envelopes share digest: %s", a.Digest())
	}
	if a.Digest() == "" || b.Digest() == "" {
		t.Fatal("digest was empty")
	}
}

func TestTodo_SEARCH_003_Fault(t *testing.T) {
	frozen := freezeFixture(t)
	_, err := PrepareAction(context.Background(), frozen, ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 1}, searchInstant(t, 110), ActionGovernanceFunc(func(context.Context, ActionRequest) (GovernanceDecision, error) {
		return GovernanceDecision{}, errors.New("governance unavailable")
	}))
	if !errors.Is(err, ErrActionNotFresh) {
		t.Fatalf("governance fault err=%v, want ErrActionNotFresh", err)
	}
}

func TestTodo_SEARCH_003_Security(t *testing.T) {
	frozen := freezeFixture(t)
	frozen.Selection = append(frozen.Selection, values.EntityRef{Tenant: "other-tenant", Kind: "worker", Id: searchID2})
	if _, err := PrepareAction(context.Background(), frozen, ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 10}, searchInstant(t, 110), ActionGovernanceFunc(authorizeAction)); err == nil {
		t.Fatal("unbound selection was accepted")
	}
	_, err := FreezeAuthorized(FreezeRequest{Envelope: envelope(t), Results: []Result{}, Selected: []values.EntityRef{{Tenant: searchTenant, Kind: "worker", Id: searchID1}}, Definition: populationDefinition(), RevisionVersion: "revision.v1", Versions: versions(), AsOf: envelope(t).EffectiveAt, KnownAt: envelope(t).KnownAt, Watermarks: map[population.SubjectKind]values.Instant{population.SubjectWorker: searchInstant(t, 100)}})
	if !errors.Is(err, ErrSelectionNotVisible) {
		t.Fatalf("hidden selection err=%v, want ErrSelectionNotVisible", err)
	}
}

func TestTodo_SEARCH_003_Conformance(t *testing.T) {
	action, err := RecommendAction(context.Background(), freezeFixture(t), ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 1}, searchInstant(t, 110), ActionGovernanceFunc(authorizeAction))
	if err != nil || action.EffectAuthorized {
		t.Fatalf("RecommendAction conformance failed: %+v err=%v", action, err)
	}
}

func BenchmarkTodo_SEARCH_003(b *testing.B) {
	instant, err := values.NewInstantFromUnix(100, 0)
	if err != nil {
		b.Fatal(err)
	}
	knownAt, err := values.NewKnownAt(instant)
	if err != nil {
		b.Fatal(err)
	}
	e := QueryEnvelope{
		Tenant: searchTenant, Purpose: "people.search", Classification: dlp.ClassPublic,
		Fields: []authz.FieldID{authz.FieldWorkerNumber}, EffectiveAt: instant, KnownAt: knownAt,
		PolicyDigest: "policy.authz.v1", QueryDigest: "query.sha256:1", SemanticPlanDigest: "plan.sha256:1", IndexDigest: "index.sha256:1", MinimumWatermark: instant, Mode: ModeExact, MaxResults: 1,
	}
	frozen, err := FreezeAuthorized(FreezeRequest{Envelope: e, Results: []Result{{Subject: values.EntityRef{Tenant: searchTenant, Kind: "worker", Id: searchID1}, SourceDigest: "source:" + searchID1, PolicyDigest: e.PolicyDigest, AuthorizationEvidence: "authz", Watermark: instant}}, Selected: []values.EntityRef{{Tenant: searchTenant, Kind: "worker", Id: searchID1}}, Definition: populationDefinition(), RevisionVersion: "revision.v1", Versions: versions(), AsOf: instant, KnownAt: knownAt, Watermarks: map[population.SubjectKind]values.Instant{population.SubjectWorker: instant}})
	if err != nil {
		b.Fatal(err)
	}
	at := instant
	for i := 0; i < b.N; i++ {
		_, _ = PrepareAction(context.Background(), frozen, ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 1}, at, ActionGovernanceFunc(authorizeAction))
	}
}

func TestTodo_SEARCH_003_Mutation(t *testing.T) {
	frozen := freezeFixture(t)
	if _, err := PrepareAction(context.Background(), frozen, ActionTemplate{ID: "promotion.change", Version: "v1", MaxMembers: 0}, searchInstant(t, 110), ActionGovernanceFunc(authorizeAction)); !errors.Is(err, ErrActionBound) {
		t.Fatalf("unbounded template err=%v, want ErrActionBound", err)
	}
	stale := frozen
	stale.MinimumWatermark = searchInstant(t, 1000)
	if err := stale.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := FreezeAuthorized(FreezeRequest{Envelope: envelope(t), Results: []Result{{Subject: values.EntityRef{Tenant: searchTenant, Kind: "worker", Id: searchID1}, SourceDigest: "source:" + searchID1, PolicyDigest: "policy.authz.v1", AuthorizationEvidence: "authz", Watermark: searchInstant(t, 10)}}, Selected: []values.EntityRef{{Tenant: searchTenant, Kind: "worker", Id: searchID1}}, Definition: populationDefinition(), RevisionVersion: "revision.v1", Versions: versions(), AsOf: envelope(t).EffectiveAt, KnownAt: envelope(t).KnownAt, Watermarks: map[population.SubjectKind]values.Instant{population.SubjectWorker: searchInstant(t, 10)}}); !errors.Is(err, ErrStaleWatermark) {
		t.Fatalf("stale watermark err=%v, want ErrStaleWatermark", err)
	}
}
