package agentpersonastore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type fixture struct {
	db   *pgtest.DB
	ids  map[values.TenantId]uuid.UUID
	when time.Time
}

func newFixture(t *testing.T, tenants ...values.TenantId) *fixture {
	t.Helper()
	f := &fixture{db: pgtest.NewEmpty(t), ids: map[values.TenantId]uuid.UUID{}, when: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	if err := agentstore.Migrate(context.Background(), f.db.SQL); err != nil {
		t.Fatalf("apply isolated agent migrations: %v", err)
	}
	for _, tenant := range tenants {
		id := uuid.New()
		f.ids[tenant] = id
		f.db.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, id)
	}
	return f
}

func (f *fixture) store(t *testing.T, tenant values.TenantId) *TenantStore {
	return f.storeWithAuthorities(t, tenant, testReviewSource{}, testEvaluationSource{})
}

func (f *fixture) storeWithAuthorities(t *testing.T, tenant values.TenantId, reviews ReviewEvidenceSource, evaluations EvaluationEvidenceSource) *TenantStore {
	t.Helper()
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	root, err := NewWithPublicationAuthorities(conn, func(key values.TenantId) uuid.UUID { return f.ids[key] }, reviews, evaluations)
	if err != nil {
		t.Fatal(err)
	}
	store, err := root.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func version(tenant values.TenantId, id string, n int64) PersonaVersion {
	return PersonaVersion{
		TenantID: tenant, PersonaID: id, Version: n, AgentVersion: "agent-v7",
		Handle: "policy-helper", DisplayName: "Policy Helper",
		Profile:       jsonBytes(`{"owner":"user:business-owner","skills":[{"id":"policy.search","version":1}],"tier":"T0"}`),
		ContentDigest: "sha256:persona-v7",
		CreatedAt:     time.Date(2026, 9, 28, 12, 0, 0, 123456000, time.UTC),
	}
}

func jsonBytes(text string) []byte { return []byte(text) }

func testChannelPolicy() ChannelPolicy {
	return ChannelPolicy{PlacementClass: "PRIVATE", MaxTier: "T1", AllowedDataClasses: []string{"WORKFORCE"},
		AlwaysPrivate: true, ConversationSearchAllowed: false,
		AllowedChannelClasses: []ConversationClass{ConversationPrivate},
		AllowExternalMembers:  false, AllowCrossCompanyMembers: false}
}

func lifecycle(tenant values.TenantId, id string, n int64, eventID string, from, to LifecycleState) LifecycleEvent {
	return LifecycleEvent{TenantID: tenant, EventID: eventID, PersonaID: id, PersonaVersion: n, From: from, To: to,
		Reason: "reviewed change", ActorID: "user:reviewer", OccurredAt: time.Date(2026, 9, 28, 13, 0, 0, 0, time.UTC)}
}

func draftOwners(v PersonaVersion) (PersonaOwner, PersonaOwner) {
	return PersonaOwner{TenantID: v.TenantID, PersonaID: v.PersonaID, Role: BusinessOwner, PrincipalID: "user:business-owner"},
		PersonaOwner{TenantID: v.TenantID, PersonaID: v.PersonaID, Role: TechnicalSteward, PrincipalID: "user:technical-steward"}
}

type testReviewSource struct {
	corrupt      bool
	grantRevoked bool
	reviewer     string
	permission   string
	decision     string
}

func (source testReviewSource) ResolvePersonaReview(_ context.Context, _ dbport.Tx, tenant values.TenantId, personaID string, version int64, digest, reviewID string) (VerifiedReview, error) {
	if source.corrupt {
		digest = "sha256:forged"
	}
	reviewer, permission, decision := source.reviewer, source.permission, source.decision
	if reviewer == "" {
		reviewer = "user:independent-reviewer"
	}
	if permission == "" {
		permission = "persona:review"
	}
	if decision == "" {
		decision = "APPROVE"
	}
	return VerifiedReview{ReviewID: reviewID, TenantID: string(tenant), PersonaID: personaID, PersonaVersion: version,
		ReviewerID: reviewer, Permission: permission, Decision: decision, ProfileDigest: digest,
		ReviewDigest: "sha256:review-proof", GrantCurrent: !source.grantRevoked}, nil
}

type testEvaluationSource struct{ corrupt bool }

func (source testEvaluationSource) ResolvePersonaEvaluation(_ context.Context, _ dbport.Tx, tenant values.TenantId, runID, personaID string, version int64, profileDigest string) (VerifiedEvaluation, error) {
	digest := profileDigest
	if source.corrupt {
		digest = "sha256:wrong-version"
	}
	return VerifiedEvaluation{RunID: runID, TenantID: string(tenant), PersonaID: personaID, PersonaVersion: version,
		ProfileDigest: digest, SuiteDigest: "sha256:suite", RunDigest: "sha256:run", Passed: true, Fresh: true}, nil
}

func appendLifecycleForTest(t *testing.T, s *TenantStore, ctx context.Context, event LifecycleEvent) error {
	t.Helper()
	if event.To != StatePublished {
		return s.AppendLifecycle(ctx, event)
	}
	// Test evidence is bound to this exact tenant/version/digest; it is never
	// installed in production code or seeded into the database.
	version, err := s.GetVersion(ctx, event.PersonaID, event.PersonaVersion)
	if err != nil {
		return err
	}
	owners, err := s.ListOwners(ctx, event.PersonaID)
	if err != nil {
		return err
	}
	hasOwner := false
	for _, owner := range owners {
		if owner.Role == BusinessOwner {
			hasOwner = true
			break
		}
	}
	if !hasOwner {
		if err := s.PutOwner(ctx, PersonaOwner{TenantID: version.TenantID, PersonaID: version.PersonaID, Role: BusinessOwner, PrincipalID: "user:business-owner", AssignedBy: "test", AssignedAt: version.CreatedAt}); err != nil {
			return err
		}
	}
	return s.Publish(ctx, event, PublicationEvidence{ReviewID: "test-review", EvaluationRunID: "test-run"})
}

func TestTodo_AGENTP_004(t *testing.T) {
	f := newFixture(t, "harborcare-demo")
	s := f.store(t, "harborcare-demo")
	ctx := context.Background()
	v := version("harborcare-demo", "persona-policy", 1)
	v.Profile = jsonBytes(`{"owner":"user:owner","skills":[{"id":"policy.search","version":1}],"tier":"T0"}`)
	if err := s.PutVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := s.PutOwner(ctx, PersonaOwner{TenantID: v.TenantID, PersonaID: v.PersonaID, Role: BusinessOwner, PrincipalID: "user:owner", AssignedBy: "user:admin", AssignedAt: f.when}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutOwner(ctx, PersonaOwner{TenantID: v.TenantID, PersonaID: v.PersonaID, Role: TechnicalSteward, PrincipalID: "user:steward", AssignedBy: "user:admin", AssignedAt: f.when}); err != nil {
		t.Fatal(err)
	}
	for _, event := range []LifecycleEvent{
		lifecycle(v.TenantID, v.PersonaID, v.Version, "evt-draft", "", StateDraft),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "evt-review", StateDraft, StateInReview),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "evt-publish", StateInReview, StatePublished),
	} {
		if err := appendLifecycleForTest(t, s, ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	state, err := s.Lifecycle(ctx, v.PersonaID, v.Version)
	if err != nil || state != StatePublished {
		t.Fatalf("Lifecycle = %q, %v, want PUBLISHED", state, err)
	}
	events, err := s.ListLifecycle(ctx, v.PersonaID, v.Version)
	if err != nil || len(events) != 3 || events[0].To != StateDraft || events[2].From != StateInReview {
		t.Fatalf("lifecycle event log = %+v, %v", events, err)
	}
	if got := events[2]; got.ProfileDigest != v.ContentDigest || got.EvaluationProfileDigest != v.ContentDigest || got.ReviewDigest == "" || got.EvaluationDigest == "" || got.ReviewerID != got.ActorID {
		t.Fatalf("publication evidence not durably pinned: %+v", got)
	}
	owners, err := s.ListOwners(ctx, v.PersonaID)
	if err != nil || len(owners) != 2 {
		t.Fatalf("owners = %+v, %v", owners, err)
	}
	if err := s.Install(ctx, PersonaInstallation{TenantID: v.TenantID, InstallationID: "install-1", PersonaID: v.PersonaID, PersonaVersion: v.Version,
		ConversationID: "channel-1", ConversationClass: ConversationPrivate, InstallerID: "user:manager", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1, CreatedAt: f.when, UpdatedAt: f.when}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetInstallation(ctx, "install-1")
	if err != nil || got.State != InstallationActive || got.PersonaVersion != v.Version || got.ConversationClass != ConversationPrivate || !reflect.DeepEqual(got.ChannelPolicy, testChannelPolicy()) || got.Revision != 1 || got.RevocationEpoch != 1 {
		t.Fatalf("installation = %+v, %v", got, err)
	}
	read, err := s.GetVersion(ctx, v.PersonaID, v.Version)
	var gotProfile, wantProfile map[string]any
	if err == nil {
		err = json.Unmarshal(read.Profile, &gotProfile)
	}
	if err == nil {
		err = json.Unmarshal(v.Profile, &wantProfile)
	}
	if err != nil || !reflect.DeepEqual(gotProfile, wantProfile) || read.ContentDigest != v.ContentDigest {
		t.Fatalf("version round trip = %+v, %v", read, err)
	}
	for name, statement := range map[string]string{
		"version update": `UPDATE persona_versions SET display_name='changed'`,
		"version delete": `DELETE FROM persona_versions`,
		"event update":   `UPDATE persona_lifecycle_events SET reason='changed'`,
		"event delete":   `DELETE FROM persona_lifecycle_events`,
	} {
		conn := f.db.NewConn(t)
		if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
			t.Fatal(err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := tenancy.WithTenant(ctx, tx, f.ids[v.TenantID]); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, statement+` WHERE tenant_id=$1`, f.ids[v.TenantID]); err == nil {
			t.Errorf("%s accepted a mutation", name)
		}
		_ = tx.Rollback(ctx)
	}
	if err := s.PutVersion(ctx, v); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate version = %v, want ErrConflict", err)
	}
}

func TestTodo_AGENTP_006(t *testing.T) {
	f := newFixture(t, "publication-tenant")
	s := f.store(t, "publication-tenant")
	ctx := context.Background()
	v := version("publication-tenant", "test-persona", 1)
	owner, steward := draftOwners(v)
	if err := s.CreateDraft(ctx, v, owner, steward, owner.PrincipalID, f.when); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "to-review", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	event := lifecycle(v.TenantID, v.PersonaID, v.Version, "verified-publish", StateInReview, StatePublished)
	if err := s.AppendLifecycle(ctx, event); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("caller-authored publication accepted: %v", err)
	}
	unconfiguredRoot, err := New(f.db.NewConn(t), func(key values.TenantId) uuid.UUID { return f.ids[key] })
	if err != nil {
		t.Fatal(err)
	}
	unconfigured, err := unconfiguredRoot.Scoped(v.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if err := unconfigured.Publish(ctx, event, PublicationEvidence{ReviewID: "review-1", EvaluationRunID: "eval-1"}); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("missing trusted authorities accepted: %v", err)
	}
	badEvaluation := f.storeWithAuthorities(t, v.TenantID, testReviewSource{}, testEvaluationSource{corrupt: true})
	if err := badEvaluation.Publish(ctx, event, PublicationEvidence{ReviewID: "review-1", EvaluationRunID: "eval-1"}); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("wrong-version evaluation accepted: %v", err)
	}
	badReview := f.storeWithAuthorities(t, v.TenantID, testReviewSource{corrupt: true}, testEvaluationSource{})
	if err := badReview.Publish(ctx, event, PublicationEvidence{ReviewID: "review-1", EvaluationRunID: "eval-1"}); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("wrong-profile review accepted: %v", err)
	}
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, f.ids[v.TenantID]); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO persona_lifecycle_events (tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at) VALUES ($1,'direct-forgery',$2,$3,'IN_REVIEW','PUBLISHED','forged','attacker',now())`, f.ids[v.TenantID], v.PersonaID, v.Version); err == nil {
		t.Fatal("database accepted a PUBLISHED event without evidence pins")
	}
	_ = tx.Rollback(ctx)
	for name, source := range map[string]testReviewSource{
		"revoked grant":    {grantRevoked: true},
		"self review":      {reviewer: owner.PrincipalID},
		"wrong permission": {permission: "persona:read"},
	} {
		t.Run(name, func(t *testing.T) {
			denied := f.storeWithAuthorities(t, v.TenantID, source, testEvaluationSource{})
			if err := denied.Publish(ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "denied-"+name, StateInReview, StatePublished), PublicationEvidence{ReviewID: "review-1", EvaluationRunID: "eval-1"}); !errors.Is(err, ErrPublicationEvidenceRequired) {
				t.Fatalf("invalid review accepted: %v", err)
			}
		})
	}
	if state, err := s.Lifecycle(ctx, v.PersonaID, v.Version); err != nil || state != StateInReview {
		t.Fatalf("failed publications changed lifecycle: %q, %v", state, err)
	}
	if err := s.Publish(ctx, event, PublicationEvidence{ReviewID: "review-1", EvaluationRunID: "eval-1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(ctx, event, PublicationEvidence{ReviewID: "review-1", EvaluationRunID: "eval-1"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("publication replay = %v, want conflict", err)
	}
}

func TestTodo_AGENTP_006_RequiresOwner(t *testing.T) {
	f := newFixture(t, "publication-owner-tenant")
	ctx := context.Background()
	v := version("publication-owner-tenant", "owner-required", 1)
	missingProfileOwner := v
	missingProfileOwner.Profile = jsonBytes(`{"skills":[]}`)
	if err := f.store(t, v.TenantID).PutVersion(ctx, missingProfileOwner); !errors.Is(err, ErrInvalid) {
		t.Fatalf("version without profile owner = %v, want ErrInvalid", err)
	}
	s := f.store(t, v.TenantID)
	if err := s.PutVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := s.PutOwner(ctx, PersonaOwner{TenantID: v.TenantID, PersonaID: v.PersonaID, Role: TechnicalSteward, PrincipalID: "user:steward", AssignedBy: "user:admin", AssignedAt: f.when}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "owner-draft", "", StateDraft)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "owner-review", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "owner-publish", StateInReview, StatePublished), PublicationEvidence{ReviewID: "review", EvaluationRunID: "eval"}); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("publication without durable business owner = %v, want evidence-required", err)
	}
	if err := s.PutOwner(ctx, PersonaOwner{TenantID: v.TenantID, PersonaID: v.PersonaID, Role: BusinessOwner, PrincipalID: "user:other-owner", AssignedBy: "user:admin", AssignedAt: f.when}); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "owner-publish-mismatch", StateInReview, StatePublished), PublicationEvidence{ReviewID: "review", EvaluationRunID: "eval"}); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("profile/durable owner mismatch = %v, want evidence-required", err)
	}
	if state, err := s.Lifecycle(ctx, v.PersonaID, v.Version); err != nil || state != StateInReview {
		t.Fatalf("owner failures changed lifecycle: %q, %v", state, err)
	}
}

func TestTodo_AGENTP_006_Race(t *testing.T) {
	f := newFixture(t, "publication-race-tenant")
	s := f.store(t, "publication-race-tenant")
	concurrentStore := f.store(t, "publication-race-tenant")
	ctx := context.Background()
	v := version("publication-race-tenant", "race-persona", 1)
	owner, steward := draftOwners(v)
	if err := s.CreateDraft(ctx, v, owner, steward, owner.PrincipalID, f.when); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "race-to-review", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, concurrentStore := range []*TenantStore{s, concurrentStore} {
		go func(store *TenantStore) {
			<-start
			event := lifecycle(v.TenantID, v.PersonaID, v.Version, "race-publish", StateInReview, StatePublished)
			results <- store.Publish(ctx, event, PublicationEvidence{ReviewID: "review-race", EvaluationRunID: "eval-race"})
		}(concurrentStore)
	}
	close(start)
	var successes, conflicts int
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent publish error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("publish outcomes: success=%d conflict=%d", successes, conflicts)
	}
	if state, err := s.Lifecycle(ctx, v.PersonaID, v.Version); err != nil || state != StatePublished {
		t.Fatalf("concurrent lifecycle = %q, %v", state, err)
	}
}

func TestTodo_AGENTP_004_Security(t *testing.T) {
	f := newFixture(t, "harborcare-demo", "ironridge-demo")
	a, b := f.store(t, "harborcare-demo"), f.store(t, "ironridge-demo")
	v := version("harborcare-demo", "private-persona", 1)
	businessOwner, technicalSteward := draftOwners(v)
	if err := a.CreateDraft(context.Background(), v, businessOwner, technicalSteward, "user:creator", f.when); err != nil {
		t.Fatal(err)
	}
	if got, err := b.ListVersions(context.Background(), ""); err != nil || len(got) != 0 {
		t.Fatalf("tenant B saw tenant A versions: %+v, %v", got, err)
	}
	if _, err := b.GetVersion(context.Background(), v.PersonaID, v.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant Get = %v, want ErrNotFound", err)
	}
	if got, err := b.ListOwners(context.Background(), v.PersonaID); err != nil || len(got) != 0 {
		t.Fatalf("tenant B saw tenant A owners: %+v, %v", got, err)
	}
	if err := appendLifecycleForTest(t, b, context.Background(), lifecycle("ironridge-demo", v.PersonaID, v.Version, "foreign", "", StateDraft)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant lifecycle = %v, want ErrNotFound", err)
	}
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, f.ids["ironridge-demo"]); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM persona_versions WHERE tenant_id=$1`, f.ids["harborcare-demo"]).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("RLS exposed %d foreign persona versions", visible)
	}
}

func TestTodo_AGENTP_004_Integration(t *testing.T) {
	f := newFixture(t, "integration-tenant")
	s := f.store(t, "integration-tenant")
	v := version("integration-tenant", "integration-persona", 1)
	businessOwner, technicalSteward := draftOwners(v)
	if err := s.CreateDraft(context.Background(), v, businessOwner, technicalSteward, "user:creator", f.when); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Lifecycle(context.Background(), v.PersonaID, v.Version); err != nil || got != StateDraft {
		t.Fatalf("pgtest lifecycle = %q, %v", got, err)
	}
	owners, err := s.ListOwners(context.Background(), v.PersonaID)
	if err != nil || len(owners) != 2 || owners[0].AssignedBy != "user:creator" || owners[1].AssignedAt != f.when {
		t.Fatalf("pgtest draft owners = %+v, %v", owners, err)
	}
	rows, err := s.ListVersions(context.Background(), v.PersonaID)
	if err != nil || len(rows) != 1 || rows[0].TenantID != v.TenantID {
		t.Fatalf("pgtest versions = %+v, %v", rows, err)
	}
}

func TestTodo_AGENTP_004_CreateDraftRejectsInvalidAndDuplicate(t *testing.T) {
	f := newFixture(t, "create-draft-tenant", "other-tenant")
	s := f.store(t, "create-draft-tenant")
	ctx := context.Background()
	v := version("create-draft-tenant", "draft-persona", 1)
	businessOwner, technicalSteward := draftOwners(v)
	for name, tc := range map[string]struct {
		version PersonaVersion
		owner   PersonaOwner
		steward PersonaOwner
		actor   string
		at      time.Time
	}{
		"tenant mismatch":       {version: version("other-tenant", "wrong-tenant", 1), owner: businessOwner, steward: technicalSteward, actor: "user:creator", at: f.when},
		"owner tenant mismatch": {version: v, owner: PersonaOwner{TenantID: "other-tenant", PersonaID: v.PersonaID, Role: BusinessOwner, PrincipalID: "user:owner"}, steward: technicalSteward, actor: "user:creator", at: f.when},
		"invalid role":          {version: v, owner: PersonaOwner{TenantID: v.TenantID, PersonaID: v.PersonaID, Role: TechnicalSteward, PrincipalID: "user:owner"}, steward: technicalSteward, actor: "user:creator", at: f.when},
		"empty owner":           {version: v, owner: PersonaOwner{TenantID: v.TenantID, PersonaID: v.PersonaID, Role: BusinessOwner}, steward: technicalSteward, actor: "user:creator", at: f.when},
		"empty actor":           {version: v, owner: businessOwner, steward: technicalSteward, at: f.when},
		"empty timestamp":       {version: v, owner: businessOwner, steward: technicalSteward, actor: "user:creator"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := s.CreateDraft(ctx, tc.version, tc.owner, tc.steward, tc.actor, tc.at); !errors.Is(err, ErrInvalid) {
				t.Fatalf("CreateDraft error = %v, want ErrInvalid", err)
			}
		})
	}
	if err := s.CreateDraft(ctx, v, businessOwner, technicalSteward, "user:creator", f.when); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateDraft(ctx, v, businessOwner, technicalSteward, "user:creator", f.when); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate CreateDraft = %v, want ErrConflict", err)
	}
	owners, err := s.ListOwners(ctx, v.PersonaID)
	if err != nil || len(owners) != 2 {
		t.Fatalf("duplicate changed owners = %+v, %v", owners, err)
	}
	if got, err := s.Lifecycle(ctx, v.PersonaID, v.Version); err != nil || got != StateDraft {
		t.Fatalf("duplicate lifecycle = %q, %v", got, err)
	}
}

func TestTodo_AGENTP_004_CreateDraftRollsBackOnOwnerOrEventFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		table    string
		trigger  string
		function string
	}{
		{name: "owner", table: "persona_owners", trigger: "reject_persona_owner", function: "reject_persona_owner"},
		{name: "event", table: "persona_lifecycle_events", trigger: "reject_persona_event", function: "reject_persona_event"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant := values.TenantId("atomic-" + tc.name + "-tenant")
			f := newFixture(t, tenant)
			s := f.store(t, tenant)
			f.db.Exec(t, `CREATE FUNCTION `+tc.function+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected `+tc.name+` failure'; END $$`)
			f.db.Exec(t, `CREATE TRIGGER `+tc.trigger+` BEFORE INSERT ON `+tc.table+` FOR EACH ROW EXECUTE FUNCTION `+tc.function+`()`)
			v := version(tenant, "atomic-persona", 1)
			businessOwner, technicalSteward := draftOwners(v)
			err := s.CreateDraft(context.Background(), v, businessOwner, technicalSteward, "user:creator", f.when)
			if err == nil {
				t.Fatal("CreateDraft succeeded despite injected database failure")
			}
			versions, err := s.ListVersions(context.Background(), v.PersonaID)
			if err != nil || len(versions) != 0 {
				t.Fatalf("failed draft left versions %+v, %v", versions, err)
			}
			owners, err := s.ListOwners(context.Background(), v.PersonaID)
			if err != nil || len(owners) != 0 {
				t.Fatalf("failed draft left owners %+v, %v", owners, err)
			}
			if _, err := s.Lifecycle(context.Background(), v.PersonaID, v.Version); !errors.Is(err, ErrNotFound) {
				t.Fatalf("failed draft lifecycle = %v, want ErrNotFound", err)
			}
		})
	}
}

type failingDB struct{}

func (failingDB) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("injected begin failure")
}

func TestTodo_AGENTP_004_Fault(t *testing.T) {
	root, err := New(failingDB{}, func(values.TenantId) uuid.UUID { return uuid.New() })
	if err != nil {
		t.Fatal(err)
	}
	s, err := root.Scoped("fault-tenant")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutVersion(context.Background(), version("fault-tenant", "fault-persona", 1)); err == nil {
		t.Fatal("injected database failure was hidden")
	}
	f := newFixture(t, "fault-tenant")
	s = f.store(t, "fault-tenant")
	v := version("fault-tenant", "atomic-persona", 1)
	if err := s.PutVersion(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	if err := appendLifecycleForTest(t, s, context.Background(), lifecycle(v.TenantID, v.PersonaID, v.Version, "bad-transition", StatePublished, StateSuspended)); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid lifecycle write = %v, want ErrConflict", err)
	}
	if _, err := s.Lifecycle(context.Background(), v.PersonaID, v.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed lifecycle write left state: %v", err)
	}
}

func TestTodo_AGENTP_004_Recovery(t *testing.T) {
	f := newFixture(t, "recovery-tenant")
	s := f.store(t, "recovery-tenant")
	v := version("recovery-tenant", "recoverable-persona", 1)
	if err := s.PutVersion(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	for _, event := range []LifecycleEvent{
		lifecycle(v.TenantID, v.PersonaID, v.Version, "restore-draft", "", StateDraft),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "restore-review", StateDraft, StateInReview),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "restore-publish", StateInReview, StatePublished),
	} {
		if err := appendLifecycleForTest(t, s, context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Install(context.Background(), PersonaInstallation{TenantID: v.TenantID, InstallationID: "valid-install", PersonaID: v.PersonaID, PersonaVersion: v.Version,
		ConversationID: "dm-1", ConversationClass: ConversationOneToOne, InstallerID: "user:manager", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	// Simulate a restore importing installation metadata before the referenced
	// immutable version image. Reconciliation must preserve the row and stop it.
	f.db.Exec(t, `INSERT INTO persona_installations
		(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at)
		VALUES ($1,'orphan-install','recoverable-persona',99,'dm-2','ONE_TO_ONE','user:manager',
		'{"max_tier":"T1","allowed_data_classes":["WORKFORCE"],"always_private":false,"conversation_search_allowed":false,"allowed_channel_classes":["ONE_TO_ONE"],"allow_external_members":false,"allow_cross_company_members":false}',
		'ACTIVE','',1,1,now(),now())`, f.ids["recovery-tenant"])
	changed, err := s.ReconcileInstallations(context.Background())
	if err != nil || len(changed) != 1 || changed[0].InstallationID != "orphan-install" || changed[0].Reason != MissingVersionAfterRestore {
		t.Fatalf("reconciliation = %+v, %v", changed, err)
	}
	orphan, err := s.GetInstallation(context.Background(), "orphan-install")
	if err != nil || orphan.State != InstallationSuspended || orphan.SuspensionReason != MissingVersionAfterRestore || orphan.Revision != 2 || orphan.RevocationEpoch != 2 {
		t.Fatalf("orphan installation = %+v, %v", orphan, err)
	}
	valid, err := s.GetInstallation(context.Background(), "valid-install")
	if err != nil || valid.State != InstallationActive {
		t.Fatalf("valid installation changed during recovery = %+v, %v", valid, err)
	}
	replayed, err := s.ReconcileInstallations(context.Background())
	if err != nil || len(replayed) != 0 {
		t.Fatalf("already suspended orphan reconciled again = %+v, %v", replayed, err)
	}
	again, err := s.GetInstallation(context.Background(), "orphan-install")
	if err != nil || again.Revision != orphan.Revision || again.RevocationEpoch != orphan.RevocationEpoch {
		t.Fatalf("restore replay advanced orphan fencing twice = %+v, %v", again, err)
	}
}

func TestTodo_AGENTP_004_MigrationFault(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if _, err := db.SQL.ExecContext(ctx, `CREATE TABLE persona_owners (collision integer)`); err != nil {
		t.Fatal(err)
	}
	if err := agentstore.Migrate(ctx, db.SQL); err == nil {
		t.Fatal("migration succeeded despite a table-name collision")
	}
	for _, table := range []string{"persona_versions", "persona_lifecycle_events"} {
		var relation sql.NullString
		if err := db.SQL.QueryRowContext(ctx, `SELECT to_regclass($1)::text`, table).Scan(&relation); err != nil {
			t.Fatal(err)
		}
		if relation.Valid {
			t.Errorf("failed 00006 migration left partial table %q", relation.String)
		}
	}
	var collision sql.NullString
	if err := db.SQL.QueryRowContext(ctx, `SELECT to_regclass('persona_owners')::text`).Scan(&collision); err != nil || !collision.Valid {
		t.Fatalf("pre-existing collision table = %v, %v; want preserved", collision, err)
	}
}
