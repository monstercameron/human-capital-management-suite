package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type agentUXAmbientBlockingModel struct {
	entered, release chan struct{}
}

func TestAgentUXAmbient_InstallationConsent_Security_Integration(t *testing.T) {
	s, m, _ := agentUXAmbientFixture(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "installed-old", "author", "I'll send the deck")
	if err := s.ProcessMessage(ctx, "tenant-a", "general", "installed-old", "task-catcher", "UTC"); err != nil {
		t.Fatal(err)
	}
	offers, err := s.ListOffers(ctx, "tenant-a", "general", "author")
	if err != nil || len(offers) != 1 || offers[0].InstallationVersion != 1 {
		t.Fatalf("unpinned offer %+v %v", offers, err)
	}
	if err = s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_app_installation SET version=2 WHERE tenant_id='tenant-a' AND app_id='task-catcher'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	grants, _, err := s.Grants(ctx, "tenant-a", "general", "author")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if g.Agent == "task-catcher" && g.Enabled {
			t.Fatal("replacement inherited consent")
		}
	}
	agentUXAmbientPost(t, s, "installed-new", "author", "We need to renew the license")
	if err = s.ProcessMessage(ctx, "tenant-a", "general", "installed-new", "task-catcher", "UTC"); err != nil || m.calls != 1 {
		t.Fatalf("replacement read without grant %v calls%d", err, m.calls)
	}
	if _, err = s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offers[0].ID, Action: "ADD", ExpectedRevision: offers[0].Revision}); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("retired installation offer added %v", err)
	}
	if err = s.SetGrant(ctx, "tenant-a", "general", "author", AgentUXAmbientGrant{Agent: "task-catcher", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessMessage(ctx, "tenant-a", "general", "installed-new", "task-catcher", "UTC"); err != nil || m.calls != 2 {
		t.Fatalf("fresh consent failed %v calls%d", err, m.calls)
	}
}

func (m *agentUXAmbientBlockingModel) ExtractAmbient(ctx context.Context, agent string, input AgentUXAmbientModelInput) (AgentUXAmbientProposal, error) {
	close(m.entered)
	select {
	case <-m.release:
		return AgentUXAmbientFixtureProposal(input.Message, agent), nil
	case <-ctx.Done():
		return AgentUXAmbientProposal{}, ctx.Err()
	}
}

func TestAgentUXAmbient_PostingWhileModelRuns_Performance_Integration(t *testing.T) {
	s, _, e := agentUXAmbientFixture(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "slow-model", "author", "I'll send the deck")
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_conversation SET post_sequence=1 WHERE tenant_id='tenant-a' AND id='general'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	m := &agentUXAmbientBlockingModel{entered: make(chan struct{}), release: make(chan struct{})}
	s.Model = m
	done := make(chan error, 1)
	go func() { done <- s.ProcessMessage(ctx, "tenant-a", "general", "slow-model", "task-catcher", "UTC") }()
	select {
	case <-m.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("model never received candidate")
	}
	postCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	post, err := chatstore.NewAdapter(e.chat).SendPost(postCtx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "peer"}, TenantID: "tenant-a", ConversationID: "general", IdempotencyKey: "during-model"}, chat.Post{AuthorID: "peer", AuthorHomeTenantID: "tenant-a", Body: "Still able to chat"})
	close(m.release)
	if err != nil || post.ID == "" {
		t.Fatalf("model held the human posting lock: %+v %v", post, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	offers, err := s.ListOffers(ctx, "tenant-a", "general", "author")
	if err != nil || len(offers) != 1 {
		t.Fatalf("model result missing: %+v %v", offers, err)
	}
}

func TestAgentUXAmbient_RevokedDuringModel_Security_Integration(t *testing.T) {
	s, _, _ := agentUXAmbientFixture(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "revoked-model", "author", "I'll send the deck")
	m := &agentUXAmbientBlockingModel{entered: make(chan struct{}), release: make(chan struct{})}
	s.Model = m
	done := make(chan error, 1)
	go func() { done <- s.ProcessMessage(ctx, "tenant-a", "general", "revoked-model", "task-catcher", "UTC") }()
	select {
	case <-m.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("model never received candidate")
	}
	if err := s.SetOptOut(ctx, "tenant-a", "general", "author", true); err != nil {
		close(m.release)
		t.Fatal(err)
	}
	close(m.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	offers, err := s.ListOffers(ctx, "tenant-a", "general", "author")
	if err != nil || len(offers) != 0 {
		t.Fatalf("revoked authority still offered: %+v %v", offers, err)
	}
	var outcome string
	if err = s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT outcome FROM agentux_ambient_read WHERE tenant_id='tenant-a' AND post_id='revoked-model' AND stage='RESULT'`).Scan(&outcome)
	}); err != nil || outcome != "IGNORED_SOURCE_CHANGED" {
		t.Fatalf("revoked result not recorded: %q %v", outcome, err)
	}
}
