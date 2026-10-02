package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestAgentUXAmbient_Surface_Security_Integration(t *testing.T) {
	s, _, _ := agentUXAmbientFixture(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "surface-private", "author", "I'll send the deck")
	if err := s.ProcessMessage(ctx, "tenant-a", "general", "surface-private", "task-catcher", "UTC"); err != nil {
		t.Fatal(err)
	}
	surface := AgentUXAmbientSurface{Service: s, ViewerZone: func(context.Context, string, string) (string, error) { return "Europe/Berlin", nil }, SourceLink: func(context.Context, string, string, string) (string, error) {
		return "/workspace/app/chat?post=surface-private", nil
	}}
	if _, err := surface.Snapshot(ctx, "general"); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("unauthenticated snapshot %v", err)
	}
	auth := func(tenant, person string) context.Context {
		principal, err := localAgentDemoPrincipal(s.Now(), tenant, person, "organization")
		if err != nil {
			t.Fatal(err)
		}
		return trust.WithPrincipal(ctx, principal)
	}
	owner := auth("tenant-a", "author")
	snapshot, err := surface.Snapshot(owner, "general")
	if err != nil || len(snapshot.Cards) != 1 || snapshot.Cards[0].Zone != "Europe/Berlin" {
		t.Fatalf("owner snapshot %+v %v", snapshot, err)
	}
	peer := auth("tenant-a", "peer")
	private, err := surface.Snapshot(peer, "general")
	if err != nil || len(private.Cards) != 0 {
		t.Fatalf("peer snapshot %+v %v", private, err)
	}
	if _, err = surface.Control(peer, ambientagents.Command{Conversation: "general", ID: snapshot.Cards[0].ID, Action: "ADD", ExpectedRevision: 1}); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("private command leak %v", err)
	}
	if _, err = surface.Grant(peer, ambientagents.GrantCommand{Conversation: "general", Agent: "task-catcher", Enabled: true}); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("member read grant %v", err)
	}
	if _, err = surface.Snapshot(auth("tenant-b", "author"), "general"); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("cross-tenant snapshot %v", err)
	}
	if snapshot, err = surface.Control(owner, ambientagents.Command{Conversation: "general", ID: snapshot.Cards[0].ID, Action: "ADD", ExpectedRevision: 1}); err != nil || len(snapshot.Tasks) != 1 {
		t.Fatalf("private Add %+v %v", snapshot, err)
	}
	if snapshot, err = surface.OptOut(owner, ambientagents.OptOutCommand{Conversation: "general", OptOut: true}); err != nil || !snapshot.OptOut {
		t.Fatalf("opt-out %+v %v", snapshot, err)
	}
	if _, err = surface.Grant(owner, ambientagents.GrantCommand{Conversation: "general", Agent: "task-catcher", Enabled: true, AutomaticPublic: true}); err != nil {
		t.Fatal(err)
	}
}
