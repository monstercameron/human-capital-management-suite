package application

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

// TestTodo_AGENTUX_049_Starters_Integration prepares Birthday Buddy, Task
// Catcher, Reminder and Support Desk on the cell Policy Helper and Assistant are
// prepared on, with the same recorded evaluator and no model: each is reviewed,
// evaluated, published and placed, and a second run changes nothing.
func TestTodo_AGENTUX_049_Starters_Integration(t *testing.T) {
	agentUXGeneralPublishRunnableWith(t, false, false, func(t *testing.T, preparation localAgentDemoPreparation, adminCtx context.Context, policy agentpersona.PersonaProfile, scoped *agentpersonastore.TenantStore, general string, _ *pgtest.DB) {
		room := localDevPersonaDemoConversationID(localAgentDemoTenant, localAgentDemoAmbientRoom)
		principal := chatcore.Principal{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}
		if _, err := preparation.chatService.CreateConversation(adminCtx, chatcore.CreateConversationRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: room, Kind: chatcore.PublicChannel, Name: localAgentDemoAmbientRoom, IdempotencyKey: "starters-room-create"}); err != nil {
			t.Fatal(err)
		}
		// The seeded workspace's shape: the administrator is a member of the room,
		// not its manager, so installing an agent there is not theirs to do until
		// the preparation makes them its manager.
		if err := preparation.chat.RunTenantTx(adminCtx, localAgentDemoTenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(adminCtx, `UPDATE chat_membership SET role='MEMBER' WHERE tenant_id=$1 AND conversation_id=$2 AND member_id=$3`, localAgentDemoTenant, room, localAgentDemoAdmin)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := ProvisionLocalDevPersonaChatPolicy(adminCtx, preparation.chat, ServeProfileLocalDev, localAgentDemoTenant, room, localAgentDemoAmbientRoom); err != nil {
			t.Fatal(err)
		}
		actor := PersonaAdminCommandActor{Principal: preparation.admin, Tenant: preparation.admin.Tenant(), Subject: localAgentDemoAdmin}
		_, placementErr := NewPersonaPublicAudienceAuthority(preparation.chat).ResolvePersonaAdminPlacement(adminCtx, actor, room)
		if !errors.Is(placementErr, ErrPersonaAdminLifecycleUnavailable) || !strings.Contains(placementErr.Error(), "needs the installer as an active manager") || !strings.Contains(placementErr.Error(), room) {
			t.Fatalf("a placement refused for a missing manager says %v", placementErr)
		}
		placed := map[string]int{}
		for _, entry := range localAgentDemoStarterList() {
			if entry.spec == nil {
				continue
			}
			first, direct, ran, err := preparation.prepareEntry(adminCtx, entry, policy)
			if err != nil || !ran {
				t.Fatalf("prepare %s: ran=%t err=%v", entry.Name, ran, err)
			}
			spec, _ := entry.spec()
			wantPlaced := 1
			if spec.unplaced {
				wantPlaced = 0
			}
			if direct != "" || first.state != agentpersonastore.StatePublished || !first.published || !first.evaluationRecorded || first.installationsCreated != wantPlaced || first.version != 1 {
				t.Fatalf("%s first run = %+v direct=%q", entry.Name, first, direct)
			}
			second, _, _, err := preparation.prepareEntry(adminCtx, entry, policy)
			if err != nil || second.published || second.evaluationRecorded || second.installationsCreated != 0 || second.installationsRefreshed != 0 || second.modelRouteCreated || second.providerDeploymentCreated || second.duplicateInstallationsGone != 0 || second.skillGrantsCreated != 0 || second.version != 1 {
				t.Fatalf("%s second run changed something: %+v err=%v", entry.Name, second, err)
			}
			for _, conversation := range []string{general, room} {
				rows, err := scoped.ListActiveInstallations(adminCtx, conversation)
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					if row.PersonaID == spec.plan.PersonaID {
						placed[entry.Name+"@"+conversation]++
					}
				}
			}
		}
		for name, conversation := range map[string]string{"Birthday Buddy": general, "Task Catcher": room, "Reminder": room} {
			if placed[name+"@"+conversation] != 1 {
				t.Fatalf("%s is installed %d times where it belongs: %v", name, placed[name+"@"+conversation], placed)
			}
		}
		if placed["Support Desk@"+general]+placed["Support Desk@"+room]+placed["Task Catcher@"+general]+placed["Birthday Buddy@"+room] != 0 {
			t.Fatalf("an agent is installed where it does not belong: %v", placed)
		}
		var manages bool
		if err := preparation.chat.RunTenantTx(adminCtx, localAgentDemoTenant, func(tx dbport.Tx) error {
			return tx.QueryRow(adminCtx, `SELECT EXISTS(SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND member_id=$3 AND role='MANAGER' AND state='active')`, localAgentDemoTenant, room, localAgentDemoAdmin).Scan(&manages)
		}); err != nil || !manages {
			t.Fatalf("the administrator does not manage %s after the preparation installed agents there: %v", localAgentDemoAmbientRoom, err)
		}
		// The provider deployment the serving process reads still loads with the
		// four new profiles beside the three it had.
		deployment, err := LoadPersonaModelDeployment(preparation.deploymentPath)
		if err != nil || len(deployment.Profiles) != 7 {
			t.Fatalf("deployment profiles=%d err=%v", len(deployment.Profiles), err)
		}
	})
}

// TestTodo_AGENTUX_079_Renewal_Integration starts from a cell prepared by the
// previous preparation (six policy contracts at revision 1, Policy Helper and
// Assistant published and installed) a long time later: the model routes have
// lapsed with their evaluations, the reviews are revoked. The new preparation
// carries each published agent across by renewing it with current evidence
// (no model call: the recorded evaluator), keeps its installations, prepares
// the four starters, and a second run changes nothing.
func TestTodo_AGENTUX_079_Renewal_Integration(t *testing.T) {
	agentUXGeneralPublishRunnableWith(t, false, false, func(t *testing.T, preparation localAgentDemoPreparation, adminCtx context.Context, policy agentpersona.PersonaProfile, scoped *agentpersonastore.TenantStore, general string, agentDB *pgtest.DB) {
		ctx := context.Background()
		tenant := values.TenantId(localAgentDemoTenant)
		room := localDevPersonaDemoConversationID(localAgentDemoTenant, localAgentDemoAmbientRoom)
		principal := chatcore.Principal{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}
		if _, err := preparation.chatService.CreateConversation(adminCtx, chatcore.CreateConversationRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: room, Kind: chatcore.PublicChannel, Name: localAgentDemoAmbientRoom, IdempotencyKey: "renewal-room-create"}); err != nil {
			t.Fatal(err)
		}
		// The previous preparation's policy source: its six contracts at revision 1.
		seed, err := base64.StdEncoding.DecodeString(preparation.material.PolicySeed)
		if err != nil || len(seed) != ed25519.SeedSize {
			t.Fatalf("policy signing fixture: %v", err)
		}
		policyKey := ed25519.NewKeyFromSeed(seed)
		authority, err := newAgentUXGeneralPolicyTestPool(t, agentDB)
		if err != nil {
			t.Fatal(err)
		}
		sources, err := NewLocalPersonaOpenAIPolicySources(policyKey.Public().(ed25519.PublicKey), preparation.mapper, AgentPolicySourceStateFunc(func(context.Context, values.TenantId, string) (uint64, bool, error) { return 1, false, nil }))
		if err != nil {
			t.Fatal(err)
		}
		publisher, err := agentmodelpolicystore.NewPublisher(authority, sources)
		if err != nil {
			t.Fatal(err)
		}
		old := LocalPersonaOpenAIPolicyRecords()[:6]
		for _, record := range old {
			doc, err := LocalPersonaOpenAIPolicyAuthorityDocument(tenant, preparation.mapper(tenant), record, LocalPersonaOpenAIPolicySourceID, LocalPersonaOpenAIPolicyKeyID, 1, preparation.now.Add(-time.Hour), preparation.now.AddDate(1, 0, 0))
			if err != nil {
				t.Fatal(err)
			}
			assertion, err := SignAgentPolicyAuthority(doc, policyKey)
			if err != nil {
				t.Fatal(err)
			}
			if err := publisher.Publish(ctx, preparation.mapper(tenant), record, assertion); err != nil {
				t.Fatal(err)
			}
		}
		before := map[string]int64{}
		for _, id := range []string{localAgentDemoPersonaID, localAgentDemoAssistantPersonaID} {
			row, state, err := latestPersonaVersion(ctx, scoped, id, "")
			if err != nil || state != agentpersonastore.StatePublished {
				t.Fatalf("%s before: %+v %s %v", id, row, state, err)
			}
			before[id] = row.Version
		}
		// A long time later: routes lapsed, reviews revoked, evaluations lapsed.
		agentDB.Exec(t, `ALTER TABLE persona_model_route_deployment DISABLE TRIGGER USER`)
		agentDB.Exec(t, `UPDATE persona_model_route_deployment SET effective_from=effective_from-interval '3 days', effective_until=now()-interval '1 hour'`)
		agentDB.Exec(t, `ALTER TABLE persona_model_route_deployment ENABLE TRIGGER USER`)
		agentDB.Exec(t, `ALTER TABLE persona_evaluation_evidence DISABLE TRIGGER USER`)
		agentDB.Exec(t, `UPDATE persona_evaluation_evidence SET issued_at=issued_at-interval '3 days', expires_at=now()-interval '1 hour'`)
		agentDB.Exec(t, `ALTER TABLE persona_evaluation_evidence ENABLE TRIGGER USER`)
		agentDB.Exec(t, `UPDATE persona_review_decision SET revoked_at=now(), revoked_by='test: long ago' WHERE revoked_at IS NULL`)
		agentDB.Exec(t, `UPDATE persona_review_grant SET revoked_at=now(), revoked_by='test: long ago' WHERE revoked_at IS NULL`)
		for id, version := range before {
			if _, err := scoped.ResolvePublicationEvidence(ctx, id, version); err == nil {
				t.Fatalf("%s v%d still has current evidence", id, version)
			}
		}

		// The new preparation: its policy source step, then every agent on the list.
		if err := ensureLocalAgentDemoPolicyUpgrade(ctx, preparation.core, preparation.agents, authority, preparation.mapper, tenant, policyKey, preparation.now); err != nil {
			t.Fatalf("policy source upgrade: %v", err)
		}
		run := func() map[string]localAgentDemoPreparedAgent {
			results := map[string]localAgentDemoPreparedAgent{}
			for _, entry := range localAgentDemoStarterList() {
				prepared, _, ran, err := preparation.prepareEntry(adminCtx, entry, policy)
				if err != nil || !ran {
					t.Fatalf("prepare %s: ran=%t err=%v", entry.Name, ran, err)
				}
				if prepared.state != agentpersonastore.StatePublished {
					t.Fatalf("%s is %s", entry.Name, prepared.state)
				}
				results[entry.Name] = prepared
			}
			return results
		}
		first := run()
		for name, id := range map[string]string{"Policy Helper": localAgentDemoPersonaID, "Assistant": localAgentDemoAssistantPersonaID} {
			if first[name].version != before[id]+1 || !first[name].published {
				t.Fatalf("%s was not renewed: before v%d, now %+v", name, before[id], first[name])
			}
			row, state, err := latestPersonaVersion(ctx, scoped, id, "")
			if err != nil || state != agentpersonastore.StatePublished || row.Version != before[id]+1 {
				t.Fatalf("%s latest %+v %s %v", name, row, state, err)
			}
			if _, err := scoped.ResolvePublicationEvidence(ctx, id, row.Version); err != nil {
				t.Fatalf("%s has no current evidence after renewal: %v", name, err)
			}
			rows, err := scoped.ListActiveInstallations(ctx, general)
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, installed := range rows {
				if installed.PersonaID == id {
					found++
					if installed.PersonaVersion != row.Version {
						t.Fatalf("%s is installed at v%d, not the renewed v%d", name, installed.PersonaVersion, row.Version)
					}
				}
			}
			if found != 1 {
				t.Fatalf("%s is installed %d times in #general", name, found)
			}
		}
		for _, name := range []string{"Birthday Buddy", "Task Catcher", "Reminder", "Support Desk"} {
			if first[name].version != 1 || !first[name].published {
				t.Fatalf("%s: %+v", name, first[name])
			}
		}
		second := run()
		for name, prepared := range second {
			if prepared.published || prepared.evaluationRecorded || prepared.installationsCreated != 0 || prepared.installationsRefreshed != 0 || prepared.modelRouteCreated || prepared.duplicateInstallationsGone != 0 || prepared.version != first[name].version {
				t.Fatalf("%s changed on a second run: %+v (was %+v)", name, prepared, first[name])
			}
		}
	})
}
