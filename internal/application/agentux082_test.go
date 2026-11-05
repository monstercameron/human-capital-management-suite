package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_AGENTUX_082 pins the day-after refusal path without a database: a
// lapsed model route ends the run as MODEL_ROUTE_EXPIRED (a merely missing or
// invalid route keeps MODEL_ROUTE_UNAVAILABLE), the served runtime logs one
// line naming the cause, the asker is told the agent's setup expired and who
// can renew it, renewal targets keep every existing placement, and the catalog
// carries each live version's evidence window to the card.
func TestTodo_AGENTUX_082(t *testing.T) {
	t.Run("failure code names expiry", func(t *testing.T) {
		expired := fmt.Errorf("%w: current tenant model policy expired: %w", errPersonaRunModelWork, errPersonaModelRouteExpired)
		if got := personaRunModelWorkFailureCode(expired); got != "MODEL_ROUTE_EXPIRED" {
			t.Fatalf("expired work failure code = %s, want MODEL_ROUTE_EXPIRED", got)
		}
		stale := errors.Join(errPersonaRunModelWork, errPersonaModelRouteUnavailable)
		if got := personaRunModelWorkFailureCode(stale); got != "MODEL_ROUTE_UNAVAILABLE" {
			t.Fatalf("unavailable work failure code = %s, want MODEL_ROUTE_UNAVAILABLE", got)
		}
		if got := personaRunModelWorkFailureCode(errPersonaRunModelWork); got != "CONTEXT_UNAVAILABLE" {
			t.Fatalf("other work failure code = %s, want CONTEXT_UNAVAILABLE", got)
		}
	})

	t.Run("expired run refuses once, logs once, and tells the asker", func(t *testing.T) {
		var logged bytes.Buffer
		previous := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
		defer slog.SetDefault(previous)
		cause := fmt.Errorf("%w: current tenant model policy expired: %w", errPersonaRunModelWork, errPersonaModelRouteExpired)
		executor, run, store := r4ReadyRun(t, r4FailingWork{err: cause})
		err := executor.execute(context.Background(), agentrun.Record{ID: run.ID}, run)
		var failure *PersonaRunFailure
		if !errors.As(err, &failure) || failure.Code != "MODEL_ROUTE_EXPIRED" {
			t.Fatalf("expired run ended with %v, want code MODEL_ROUTE_EXPIRED", err)
		}
		stored, getErr := store.Get(context.Background(), run.ID)
		if getErr != nil || stored.TerminalCode != "MODEL_ROUTE_EXPIRED" || stored.Retryable {
			t.Fatalf("stored run = %+v (%v), want FAILED MODEL_ROUTE_EXPIRED without retry", stored, getErr)
		}
		if count := strings.Count(logged.String(), "hcmnext.persona_model_route_unavailable"); count != 1 {
			t.Fatalf("route log lines = %d, want one: %q", count, logged.String())
		}
		if !strings.Contains(logged.String(), "expired") {
			t.Fatalf("route log line does not name the cause: %q", logged.String())
		}
		if got := chatcore.AgentAnswerFailureFor("en-US", "Policy Helper", stored.TerminalCode); got.Class != "expired" {
			t.Fatalf("card class for %s = %q, want expired (%q)", stored.TerminalCode, got.Class, got.Sentence)
		}
	})

	t.Run("route failure keeps its code from the first build", func(t *testing.T) {
		executor, run, _ := r4ReadyRun(t, r4FailingWork{err: errors.Join(errPersonaRunModelWork, errPersonaModelRouteUnavailable)})
		err := executor.execute(context.Background(), agentrun.Record{ID: run.ID}, run)
		var failure *PersonaRunFailure
		if !errors.As(err, &failure) || failure.Code != "MODEL_ROUTE_UNAVAILABLE" {
			t.Fatalf("first-build route failure ended with %v, want MODEL_ROUTE_UNAVAILABLE", err)
		}
	})

	t.Run("renewal targets keep every placement", func(t *testing.T) {
		targets := agentUX082UnionPlacementConversations(
			[]string{"general", "direct"},
			[]string{"general", "design", "benefits", "direct", "", "design"},
		)
		want := []string{"general", "direct", "design", "benefits"}
		if strings.Join(targets, ",") != strings.Join(want, ",") {
			t.Fatalf("union = %v, want %v", targets, want)
		}
		if again := agentUX082UnionPlacementConversations(nil, nil); len(again) != 0 {
			t.Fatalf("empty union = %v, want no targets", again)
		}
	})

	t.Run("expired deployment reads as expired, missing as missing", func(t *testing.T) {
		ctx := context.Background()
		tenant := uuid.New()
		ref := agentmanifest.Reference{ID: "policy", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
		now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
		past, future := now.Add(-time.Hour), now.Add(23*time.Hour)
		lapsed := agentUX082RoutesFake{currentErr: agentstore.ErrPersonaModelRouteNotFound, latest: agentstore.PersonaModelRoutePolicy{EffectiveUntil: &past}}
		if !agentUX082RouteExpired(ctx, lapsed, tenant, "entity", ref, "digest", now) {
			t.Fatal("a deployment past its window does not read as expired")
		}
		current := agentUX082RoutesFake{latest: agentstore.PersonaModelRoutePolicy{EffectiveUntil: &future}}
		if agentUX082RouteExpired(ctx, current, tenant, "entity", ref, "digest", now) {
			t.Fatal("a deployment inside its window reads as expired")
		}
		absent := agentUX082RoutesFake{currentErr: agentstore.ErrPersonaModelRouteNotFound, latestErr: agentstore.ErrPersonaModelRouteNotFound}
		if agentUX082RouteExpired(ctx, absent, tenant, "entity", ref, "digest", now) {
			t.Fatal("a deployment that never existed reads as expired")
		}
		legacy := agentUX082LegacyRoutesFake{err: agentstore.ErrPersonaModelRouteNotFound}
		if agentUX082RouteExpired(ctx, legacy, tenant, "entity", ref, "digest", now) {
			t.Fatal("a reader without deployment history reads as expired")
		}
	})

	t.Run("catalog carries the evidence window", func(t *testing.T) {
		ctx, _ := catalogContext(t)
		starter, ok := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
		if !ok {
			t.Fatal("policy helper starter is missing")
		}
		sealed, err := agentpersona.Seal(agentpersona.PersonaProfile{Manifest: agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1}, PersonaID: "persona-a", Version: 1, Handle: "persona", DisplayName: "Persona", AvatarRef: "avatar", Purpose: "Help safely", Audience: agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}}, SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, Instructions: "Answer only with approved records.", Owner: "owner", Steward: "steward", EvalSuiteRef: starter.EvaluationSuite, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1}})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
		svc := &PersonaAdminCatalogService{
			Versions: catalogVersions{
				{Profile: sealed, Lifecycle: agentpersona.StatePublished, Owner: "owner", Steward: "steward", EvaluationExpiresAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)},
			},
			Authorizer: &catalogAuth{},
			Now:        func() time.Time { return now },
		}
		snapshot, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
		if err != nil || len(snapshot.Personas) != 1 {
			t.Fatalf("snapshot = %+v err=%v, want one persona", snapshot, err)
		}
		if got := snapshot.Personas[0]; got.EvidenceExpiresAt != "2026-11-01" || got.EvidenceExpired {
			t.Fatalf("current evidence projects as %+v, want 2026-11-01 without expiry", got)
		}
		svc.Versions = catalogVersions{
			{Profile: sealed, Lifecycle: agentpersona.StatePublished, Owner: "owner", Steward: "steward", EvaluationExpiresAt: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)},
		}
		snapshot, err = svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
		if err != nil || len(snapshot.Personas) != 1 {
			t.Fatalf("snapshot = %+v err=%v, want one persona", snapshot, err)
		}
		if got := snapshot.Personas[0]; got.EvidenceExpiresAt != "2026-10-02" || !got.EvidenceExpired {
			t.Fatalf("lapsed evidence projects as %+v, want 2026-10-02 with expiry", got)
		}
	})
}

// agentUX082RoutesFake is a route reader with deployment history: the current
// lookup fails while the latest deployment is retained.
type agentUX082RoutesFake struct {
	currentErr error
	latest     agentstore.PersonaModelRoutePolicy
	latestErr  error
}

func (f agentUX082RoutesFake) CurrentPersonaModelRoutePolicy(context.Context, uuid.UUID, string, string, int64, int64, string, time.Time) (agentstore.PersonaModelRoutePolicy, error) {
	return agentstore.PersonaModelRoutePolicy{}, f.currentErr
}

func (f agentUX082RoutesFake) LatestPersonaModelRouteDeployment(context.Context, uuid.UUID, string, string, int64, int64, string) (agentstore.PersonaModelRoutePolicy, error) {
	return f.latest, f.latestErr
}

// agentUX082LegacyRoutesFake is a route reader from before deployment history:
// it answers the current lookup only.
type agentUX082LegacyRoutesFake struct{ err error }

func (f agentUX082LegacyRoutesFake) CurrentPersonaModelRoutePolicy(context.Context, uuid.UUID, string, string, int64, int64, string, time.Time) (agentstore.PersonaModelRoutePolicy, error) {
	return agentstore.PersonaModelRoutePolicy{}, f.err
}

// TestTodo_AGENTUX_082_Integration starts from Policy Helper in four
// conversations a day after it was prepared: the routes lapsed with their
// evaluations and the reviews are revoked. Renewal carries every placement to
// the new version (the first renewal on real data moved two of four), refreshes
// the retained evidence window, and a second run changes nothing.
func TestTodo_AGENTUX_082_Integration(t *testing.T) {
	agentUXGeneralPublishRunnableWith(t, false, false, func(t *testing.T, preparation localAgentDemoPreparation, adminCtx context.Context, policy agentpersona.PersonaProfile, scoped *agentpersonastore.TenantStore, general string, agentDB *pgtest.DB) {
		ctx := context.Background()
		for _, room := range []string{"design", "benefits"} {
			conversation := localDevPersonaDemoConversationID(localAgentDemoTenant, room)
			principal := chatcore.Principal{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}
			if _, err := preparation.chatService.CreateConversation(adminCtx, chatcore.CreateConversationRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: conversation, Kind: chatcore.PublicChannel, Name: room, IdempotencyKey: "agentux082-room-" + room}); err != nil {
				t.Fatal(err)
			}
		}
		starter, ok := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
		if !ok {
			t.Fatal("policy helper starter is missing")
		}
		added, _, err := preparation.prepareSpec(adminCtx, localAgentDemoStarterSpec{starter: starter, plan: localAgentDemoStarterPlan{PersonaID: localAgentDemoPersonaID}, rooms: []string{"design", "benefits"}}, policy)
		if err != nil {
			t.Fatalf("place Policy Helper in two more rooms: %v", err)
		}
		if added.installationsCreated != 2 {
			t.Fatalf("extra rooms created %d installations, want 2 (%+v)", added.installationsCreated, added)
		}
		direct, err := chatcore.DirectPairConversationID(localAgentDemoTenant, []chatcore.MemberRef{{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}, {TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAgentID}})
		if err != nil {
			t.Fatal(err)
		}
		four := []string{general, direct, localDevPersonaDemoConversationID(localAgentDemoTenant, "design"), localDevPersonaDemoConversationID(localAgentDemoTenant, "benefits")}
		held := agentUX082PlacementVersions(t, ctx, scoped)
		for _, conversation := range four {
			version, ok := held[conversation]
			if !ok {
				t.Fatalf("Policy Helper is not placed in %s (%v)", conversation, held)
			}
			if version != added.version {
				t.Fatalf("placement %s is at v%d, want v%d", conversation, version, added.version)
			}
		}

		// The retained evidence window is readable before the lapse, through
		// the same readers the card and the run use.
		details, err := scoped.ReadPublicationEvidenceDetails(ctx, localAgentDemoPersonaID, added.version)
		if err != nil || details.EvaluationExpiresAt.IsZero() || !details.EvaluationExpiresAt.After(time.Now()) {
			t.Fatalf("pre-lapse evidence details = %+v err=%v, want a future expiry", details, err)
		}

		// A day later: routes lapsed, reviews revoked, evaluations lapsed.
		agentDB.Exec(t, `ALTER TABLE persona_model_route_deployment DISABLE TRIGGER USER`)
		agentDB.Exec(t, `UPDATE persona_model_route_deployment SET effective_from=effective_from-interval '3 days', effective_until=now()-interval '1 hour'`)
		agentDB.Exec(t, `ALTER TABLE persona_model_route_deployment ENABLE TRIGGER USER`)
		agentDB.Exec(t, `ALTER TABLE persona_evaluation_evidence DISABLE TRIGGER USER`)
		agentDB.Exec(t, `UPDATE persona_evaluation_evidence SET issued_at=issued_at-interval '3 days', expires_at=now()-interval '1 hour'`)
		agentDB.Exec(t, `ALTER TABLE persona_evaluation_evidence ENABLE TRIGGER USER`)
		agentDB.Exec(t, `UPDATE persona_review_decision SET revoked_at=now(), revoked_by='test: long ago' WHERE revoked_at IS NULL`)
		agentDB.Exec(t, `UPDATE persona_review_grant SET revoked_at=now(), revoked_by='test: long ago' WHERE revoked_at IS NULL`)
		if _, err := scoped.ResolvePublicationEvidence(ctx, localAgentDemoPersonaID, added.version); err == nil {
			t.Fatalf("v%d still has current evidence after the lapse", added.version)
		}

		// Renewal carries all four placements to the new version.
		renewed, _, ran, err := preparation.prepareEntry(adminCtx, localAgentDemoStarterList()[0], policy)
		if err != nil || !ran {
			t.Fatalf("renew Policy Helper: ran=%t err=%v", ran, err)
		}
		if renewed.version != added.version+1 || !renewed.published {
			t.Fatalf("Policy Helper was not renewed: %+v", renewed)
		}
		moved := agentUX082PlacementVersions(t, ctx, scoped)
		for _, conversation := range four {
			version, ok := moved[conversation]
			if !ok {
				t.Fatalf("renewal lost placement %s", conversation)
			}
			if version != renewed.version {
				t.Fatalf("placement %s is at v%d, want renewed v%d", conversation, version, renewed.version)
			}
		}
		// The shared cell's scaffolding placement (the starter handle as a
		// conversation, with no channel policy) is skipped and counted, not
		// renewed and not fatal.
		if version, ok := moved[localAgentDemoAgentID]; !ok || version != added.version {
			t.Fatalf("scaffolding placement moved or vanished: %v", moved)
		}
		if renewed.installationsSkipped != 1 {
			t.Fatalf("renewal skipped %d placements, want the 1 scaffolding row (%+v)", renewed.installationsSkipped, renewed)
		}
		fresh, err := scoped.ReadPublicationEvidenceDetails(ctx, localAgentDemoPersonaID, renewed.version)
		if err != nil || fresh.EvaluationExpiresAt.IsZero() || !fresh.EvaluationExpiresAt.After(time.Now()) {
			t.Fatalf("post-renewal evidence details = %+v err=%v, want a future expiry", fresh, err)
		}

		again, _, _, err := preparation.prepareEntry(adminCtx, localAgentDemoStarterList()[0], policy)
		if err != nil || again.published || again.installationsCreated != 0 || again.installationsRefreshed != 0 || again.version != renewed.version {
			t.Fatalf("second run changed something: %+v", again)
		}
	})
}

func agentUX082PlacementVersions(t *testing.T, ctx context.Context, scoped *agentpersonastore.TenantStore) map[string]int64 {
	t.Helper()
	rows, err := scoped.ListVersionRolloutInstallations(ctx, localAgentDemoPersonaID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, row := range rows {
		if _, dup := out[row.ConversationID]; dup {
			t.Fatalf("duplicate placement in %s", row.ConversationID)
		}
		out[row.ConversationID] = row.PersonaVersion
	}
	return out
}
