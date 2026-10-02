package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// captureAudienceLog routes the default logger to a buffer for one test. The
// tests that use it do not run in parallel.
func captureAudienceLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	var mu sync.Mutex
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{w: &buffer, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buffer
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// agentUX033Source is a workspace of several conversations that each hold the
// viewer, two agents and some people the trusted directory cannot describe.
func agentUX033Source(rooms, strangers int) (*DatabasePersonaAudienceSource, *audienceDirectoryFake) {
	directory := &audienceDirectoryFake{facts: map[string]PersonaAudienceMember{
		"tenant/alice": {SubjectID: "alice", Roles: []string{"hcm_admin"}, Populations: []string{"employees"}, OrganizationScope: "org-a"},
	}}
	chatFake := audienceChatFake{members: map[string][]chat.Membership{}}
	for i := 0; i < rooms; i++ {
		id := fmt.Sprintf("room-%d", i)
		chatFake.rooms = append(chatFake.rooms, chat.Conversation{ID: id, TenantID: "tenant"})
		members := []chat.Membership{
			{ConversationID: id, TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "alice"},
			{ConversationID: id, TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "policy-helper"},
			{ConversationID: id, TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "assistant"},
		}
		for j := 0; j < strangers; j++ {
			members = append(members, chat.Membership{ConversationID: id, TenantID: "tenant", HomeTenantID: "tenant", SubjectID: fmt.Sprintf("stranger-%d-%d", i, j)})
		}
		chatFake.members[id] = members
	}
	return &DatabasePersonaAudienceSource{
		Chat: chatFake,
		Installations: audienceInstallStoreFake{store: audienceInstallFake{identities: map[string]agentpersonastore.PersonaChatIdentity{
			"policy-helper": {TenantID: values.TenantId("tenant"), AgentID: "policy-helper", PersonaID: "persona.policy", Active: true},
			"assistant":     {TenantID: values.TenantId("tenant"), AgentID: "assistant", PersonaID: "persona.assistant", Active: true},
		}}},
		Directory: directory,
	}, directory
}

// An agent identity is classified from the agent identity registry: it is
// never looked up in the human directory, never listed as a member of an
// audience, and never the reason for an omission line. A page load over many
// conversations writes one line with counts, not one line per member.
func TestTodo_AGENTUX_033_Integration(t *testing.T) {
	log := captureAudienceLog(t)
	ctx := audienceSourceContext(t)

	// Agents only: nothing is omitted, so nothing is logged at all.
	source, directory := agentUX033Source(6, 0)
	audience, err := source.ListCurrentPersonaAudience(ctx, "tenant", "alice")
	if err != nil || len(audience) != 6 {
		t.Fatalf("audience=%d err=%v", len(audience), err)
	}
	for _, conversation := range audience {
		if len(conversation.Members) != 1 || conversation.Members[0].SubjectID != "alice" {
			t.Fatalf("%s lists %+v; an agent identity must not be a member of an audience", conversation.ConversationID, conversation.Members)
		}
	}
	for _, call := range directory.calls {
		if strings.Contains(call, "policy-helper") || strings.Contains(call, "assistant") {
			t.Fatalf("an agent identity was looked up in the human directory: %v", directory.calls)
		}
	}
	if strings.Contains(log.String(), "persona_projection_item") || strings.Contains(log.String(), "directory_facts_missing") {
		t.Fatalf("a read with no human left out wrote an omission line:\n%s", log.String())
	}

	// Humans the directory cannot describe are the only thing left out, and the
	// read says so once, with a count.
	log.Reset()
	source, directory = agentUX033Source(6, 3)
	audience, err = source.ListCurrentPersonaAudience(ctx, "tenant", "alice")
	if err != nil || len(audience) != 6 {
		t.Fatalf("audience=%d err=%v", len(audience), err)
	}
	lines := strings.Split(strings.TrimSpace(log.String()), "\n")
	summary := 0
	for _, line := range lines {
		if strings.Contains(line, "persona_projection_item_omitted") {
			t.Fatalf("a per-item omission line was written: %s", line)
		}
		if strings.Contains(line, "persona_projection_items_omitted") {
			summary++
			if !strings.Contains(line, "members_omitted=18") || !strings.Contains(line, "conversations_omitted=0") || !strings.Contains(line, "directory_facts_incomplete:18") {
				t.Fatalf("the summary does not carry the count: %s", line)
			}
		}
		if strings.Contains(line, "policy-helper") || strings.Contains(line, "assistant") {
			t.Fatalf("the log names an agent identity: %s", line)
		}
	}
	if summary != 1 {
		t.Fatalf("want one summary line for the read, got %d:\n%s", summary, log.String())
	}
	if got := len(directory.calls); got != 6*(1+3) {
		t.Fatalf("directory lookups=%d, want the viewer and the three people of each of six conversations only", got)
	}
}

type agentUX033Directory struct{ calls []string }

func (d *agentUX033Directory) ResolvePersonaCatalogTarget(_ context.Context, _ values.TenantId, id string) (productui.PersonaAdminTarget, error) {
	d.calls = append(d.calls, id)
	if id == "policy-helper" {
		return productui.PersonaAdminTarget{}, errors.New("agent identity is not a human directory entry")
	}
	return productui.PersonaAdminTarget{ID: id, Label: "Walt Brennan"}, nil
}

// Agent setup reads the same conversations: an agent identity among their
// members is classified from the agent identity registry, is not looked up in
// the human directory, and is not counted among the omissions (the count is
// what the page reports as members it could not resolve).
func TestTodo_AGENTUX_033_AdminTargetsSkipAgentIdentity(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	log := captureAudienceLog(t)
	room := chat.Conversation{ID: "general", TenantID: "tenant-a", Name: "General", Kind: chat.PublicChannel}
	members := []chat.Membership{
		{ConversationID: room.ID, TenantID: room.TenantID, HomeTenantID: room.TenantID, SubjectID: "admin"},
		{ConversationID: room.ID, TenantID: room.TenantID, HomeTenantID: room.TenantID, SubjectID: "policy-helper"},
	}
	directory := &agentUX033Directory{}
	source := ChatDirectoryPersonaCatalogTargets{
		Chat: personaCatalogChatFake{rooms: []chat.Conversation{room}, members: members}, Directory: directory, Roles: personaCatalogRoleDirectoryFake{roles: []string{"hcm_admin"}},
		Agents: audienceInstallStoreFake{store: audienceInstallFake{identities: map[string]agentpersonastore.PersonaChatIdentity{
			"policy-helper": {TenantID: values.TenantId("tenant-a"), AgentID: "policy-helper", PersonaID: "persona.policy", Active: true},
		}}},
	}
	users, rooms, state, err := source.ListPersonaCatalogTargetsWithState(context.Background(), *principal, "tenant-a")
	if err != nil || len(rooms) != 1 || len(users) != 1 || users[0].ID != "admin" || state.Omitted != 0 {
		t.Fatalf("users=%+v rooms=%+v omitted=%d err=%v", users, rooms, state.Omitted, err)
	}
	if len(directory.calls) != 1 || directory.calls[0] != "admin" {
		t.Fatalf("directory lookups=%v; the agent identity must not be looked up as a person", directory.calls)
	}
	if strings.Contains(log.String(), "policy-helper") {
		t.Fatalf("the agent identity was logged as an omission:\n%s", log.String())
	}
}
