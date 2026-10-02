package application

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAgentUXSearch_StarterInstructions(t *testing.T) {
	v1, _ := agenttemplate.PersonaStarterFor(localAgentDemoAssistantStarterID, 1)
	v2 := AssistantWorkspaceStarter()
	if v1.Version != 1 || v2.Version != 2 || len(v1.SkillPins) != 2 || len(v2.SkillPins) != 3 || !slices.Equal(v1.SkillPins, v2.SkillPins[:2]) {
		t.Fatal("legacy starter mutated or skill missing")
	}
	text := personaStarterInstructions(v2)
	for _, required := range []string{"you MUST search first", "one search per question", "Use documents_search for a question about this conversation's documents", "Use workspace_documents_search instead", "Never say there are no company documents", "one line each", "top 5 policies", "only future observed dates", "Assistant cannot search workspace documents right now", "untrusted reference data"} {
		if !strings.Contains(text, required) {
			t.Fatal("missing instruction: " + required)
		}
	}
	if personaStarterInstructions(v1) == text {
		t.Fatal("v1 instructions rewritten")
	}
	if ref := AssistantWorkspaceEvaluationRecord().Reference; ref.Version != 4 || ref.ID != localAgentDemoAssistantSuiteID || !personaRequestDigest(ref.Digest) {
		t.Fatalf("suite not versioned: %+v", ref)
	}
}

func TestAgentUXSearch_ManifestVersions_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(context.Background(), db.SQL); err != nil {
		t.Fatal(err)
	}
	agents := commonAgentOpenIntegrationStore(t, db)
	ctx := context.Background()
	tenant := pgstore.TenantID(localAgentDemoTenant)
	db.Exec(t, "INSERT INTO tenant(tenant_id) VALUES($1)", tenant)
	v1, _ := agenttemplate.PersonaStarterFor(localAgentDemoAssistantStarterID, 1)
	first, err := ensureLocalAgentDemoAssistantManifest(ctx, agents, tenant, v1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureLocalAgentDemoAssistantManifest(ctx, agents, tenant, AssistantWorkspaceStarter())
	if err != nil || second.Version != first.Version+1 || len(second.ToolCeiling) != 3 || second.EvaluationRefs[0].Version != 4 {
		t.Fatalf("immutable manifest upgrade: %+v %v", second, err)
	}
	third, err := ensureLocalAgentDemoAssistantManifest(ctx, agents, tenant, AssistantWorkspaceStarter())
	if err != nil || !samePersonaStarterManifest(second, third) {
		t.Fatalf("manifest replay rewrote bytes: %+v %v", third, err)
	}
	var count int
	db.SQL.QueryRow(`SELECT count(*) FROM agent_definition_version WHERE tenant_id=$1 AND definition_id=$2`, tenant, first.ID).Scan(&count)
	if count != 2 {
		t.Fatalf("legacy manifest lost: %d", count)
	}
	personas, err := agentpersonastore.New(agents, func(id values.TenantId) uuid.UUID { return pgstore.TenantID(id.String()) })
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := personas.Scoped(values.TenantId(localAgentDemoTenant))
	if err != nil {
		t.Fatal(err)
	}
	policy := validPersonaProfileForLifecycleTest(t, "owner").Profile
	policy.EvalSuiteRef = localAgentDemoAssistantSuiteID
	row, state, err := ensureLocalAgentDemoAssistantVersion(ctx, scoped, v1, first, policy, time.Now().UTC())
	if err != nil || state != agentpersonastore.StateDraft {
		t.Fatalf("initial version: %+v %s %v", row, state, err)
	}
	// Publication itself is covered by the normal runtime preparation suite.
	// Here an append-only lifecycle fixture isolates immutable version rollover.
	for _, step := range []struct {
		from, to agentpersonastore.LifecycleState
	}{{agentpersonastore.StateDraft, agentpersonastore.StateInReview}, {agentpersonastore.StateInReview, agentpersonastore.StatePublished}} {
		db.Exec(t, `INSERT INTO persona_lifecycle_events(tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES($1,$2,$3,$4,$5,$6,'test lifecycle fixture','reviewer',$7,$8,'fixture review','reviewer','fixture evaluation',$8,'fixture suite')`, tenant, string(step.to), row.PersonaID, row.Version, string(step.from), string(step.to), time.Now().UTC(), row.ContentDigest)
	}
	before, err := scoped.GetVersion(ctx, row.PersonaID, row.Version)
	if err != nil {
		t.Fatal(err)
	}
	next, state, err := ensureLocalAgentDemoAssistantVersion(ctx, scoped, AssistantWorkspaceStarter(), second, policy, time.Now().UTC())
	if err != nil || state != agentpersonastore.StateDraft || next.Version != row.Version+1 {
		t.Fatalf("profile upgrade: %+v %s %v", next, state, err)
	}
	old, err := scoped.GetVersion(ctx, row.PersonaID, row.Version)
	if err != nil || old.ContentDigest != before.ContentDigest || !slices.Equal(old.Profile, before.Profile) {
		t.Fatal("published version rewritten")
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(next.Profile, &profile) != nil || profile.Instructions != assistantWorkspaceInstructions || len(profile.SkillPins) != 3 || profile.Manifest.Version != uint32(second.Version) {
		t.Fatalf("new profile lacks search: %+v", profile)
	}
	var previous agentpersona.PersonaProfile
	if json.Unmarshal(before.Profile, &previous) != nil || profile.TierCeiling != previous.TierCeiling || profile.AlwaysPrivate != previous.AlwaysPrivate || !slices.Equal(profile.ConversationKinds, previous.ConversationKinds) || !slices.Equal(profile.ChannelClasses, previous.ChannelClasses) {
		t.Fatal("workspace skill widened the published audience or tier ceiling")
	}
}
