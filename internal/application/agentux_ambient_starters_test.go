package application

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestAgentUXAmbient_StarterManifest_Golden(t *testing.T) {
	starters := AgentUXAmbientStarters()
	if len(starters) != 2 {
		t.Fatal("missing agent")
	}
	contracts := AgentUXAmbientEvaluationContracts()
	for _, starter := range starters {
		if starter.DefaultGrants == nil || len(starter.DefaultGrants) != 0 || starter.AutoInstall || starter.Published || starter.TierCeiling != "T1" || len(starter.SkillPins) != 1 {
			t.Fatalf("starter overgranted %+v", starter)
		}
		if len(starter.AudienceRoles) != 1 || starter.AudienceRoles[0] != "ALL_MEMBERS" || len(starter.AudiencePopulations) != 1 || starter.AudiencePopulations[0] != "TENANT_WIDE" || len(starter.AllowedChannelClasses) != 1 || starter.AllowedChannelClasses[0] != "ANY_INTERNAL" {
			t.Fatal("starter does not cover current internal channel members")
		}
		manifest, err := AgentUXAmbientManifest(personaRunTestManifest(), starter)
		if err != nil {
			t.Fatal(err)
		}
		if manifest.OutputSchema.ID != AgentUXAmbientProposalSchemaID || manifest.InstructionsDigest != personaRunBytesDigest([]byte(AgentUXAmbientInstructions(starter.Handle))) || manifest.EvaluationRefs[0].Digest != personaRunBytesDigest(contracts[starter.EvaluationSuite]) || len(manifest.SourceCeiling) != 1 {
			t.Fatalf("manifest contracts not pinned %+v", manifest)
		}
		var cases []json.RawMessage
		if json.Unmarshal(contracts[starter.EvaluationSuite], &cases) != nil || len(cases) != 40 {
			t.Fatal("contract does not contain forty labels")
		}
	}
	if AgentUXAmbientInstructions("unknown") != "" {
		t.Fatal("unknown starter gets instructions")
	}
	if _, err := AgentUXAmbientManifest(personaRunTestManifest(), agenttemplate.PersonaStarter{Handle: "unknown"}); err == nil {
		t.Fatal("unknown manifest accepted")
	}
}

type agentUXAmbientPrepareFixture struct{ calls []string }

func (p *agentUXAmbientPrepareFixture) PrepareAmbientPersona(_ context.Context, starter agenttemplate.PersonaStarter, instructions, schema string) (AgentUXAmbientPreparationReceipt, error) {
	p.calls = append(p.calls, starter.Handle)
	if instructions == "" || schema != AgentUXAmbientProposalSchema {
		return AgentUXAmbientPreparationReceipt{}, ErrAgentUXAmbientInvalid
	}
	return AgentUXAmbientPreparationReceipt{Agent: starter.Handle, State: "PUBLISHED", Version: 1}, nil
}

type agentUXAmbientPrepareChat struct{ s *AgentUXAmbientService }

func (p agentUXAmbientPrepareChat) SendPost(ctx context.Context, r chat.SendPostRequest) (chat.Post, error) {
	adapter := chatstore.NewAdapter(p.s.DB.(*chatstore.Store))
	post, err := adapter.SendPost(ctx, r, chat.Post{AuthorID: r.Principal.SubjectID, AuthorHomeTenantID: r.Principal.TenantID, Body: r.Body})
	if err != nil {
		return post, err
	}
	err = p.s.DB.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET created_at=$4 WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, r.TenantID, r.ConversationID, post.ID, p.s.Now().AddDate(0, 0, 0).Add(1))
		return err
	})
	return post, err
}

func TestAgentUXAmbient_PrepareExamples_Integration(t *testing.T) {
	s, m, _ := agentUXAmbientFixture(t)
	fixture := &agentUXAmbientPrepareFixture{}
	p := AgentUXAmbientDemoPreparation{Service: s, Personas: fixture, Chat: agentUXAmbientPrepareChat{s}}
	for range 2 {
		receipts, err := p.Prepare(t.Context(), "tenant-a", "general", "author", "America/New_York")
		if err != nil {
			t.Fatal(err)
		}
		if len(receipts) != 2 || receipts[0].PrivateOffers != 1 || receipts[1].PublicOffers != 1 {
			t.Fatalf("examples %+v", receipts)
		}
	}
	if m.calls != 2 {
		t.Fatalf("examples did not really run once per agent: %d", m.calls)
	}
	for _, input := range m.inputs {
		if input.SourceKind != "CHAT_MESSAGE" || !input.Untrusted {
			t.Fatal("seed bypassed pipeline")
		}
	}
}
