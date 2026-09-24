package chatapps

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestTodo_CHAT_043(t *testing.T) {
	s, actor := fixture()
	manifest := Manifest{
		AppID:   "scoped-agent",
		Version: 1,
		Scopes:  []string{"chat:invoke", "hcm:write"},
		Agent:   &AgentManifest{DisplayName: "People Assistant", Description: "Helps in this conversation"},
	}
	installation, err := s.Install(context.Background(), actor, manifest, []string{"chat:invoke"}, "manager")
	if err != nil {
		t.Fatal(err)
	}
	visible, err := s.Agent(context.Background(), installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if visible.ID != manifest.AppID || visible.InstallationID != installation.ID || visible.DisplayName != manifest.Agent.DisplayName || visible.Description != manifest.Agent.Description {
		t.Fatalf("agent identity was not bound to installation: %+v installation=%+v", visible, installation)
	}
	if visible.Status != Active || !reflect.DeepEqual(visible.Capabilities, []string{"chat:invoke"}) {
		t.Fatalf("installation grant was not disclosed separately: %+v", visible)
	}
	visible.Capabilities[0] = "forged"
	again, err := s.Agent(context.Background(), installation.ID)
	if err != nil || !reflect.DeepEqual(again.Capabilities, []string{"chat:invoke"}) {
		t.Fatalf("caller mutation changed stored grant: %+v err=%v", again, err)
	}
	if _, err := s.ChangeStatus(context.Background(), actor, installation.ID, Suspended); err != nil {
		t.Fatal(err)
	}
	suspended, err := s.Agent(context.Background(), installation.ID)
	if err != nil || suspended.Status != Suspended || !reflect.DeepEqual(suspended.Capabilities, []string{"chat:invoke"}) {
		t.Fatalf("agent status and installation grant were conflated: %+v err=%v", suspended, err)
	}
}

func TestTodo_CHAT_043_Security(t *testing.T) {
	s, actor := fixture()
	manifest := Manifest{AppID: "unnamed-agent", Version: 1, Agent: &AgentManifest{DisplayName: "  \t"}}
	if _, err := s.Install(context.Background(), actor, manifest, nil, "manager"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unnamed visible agent was registered: %v", err)
	}

	// A malformed persisted grant must not disclose capabilities that the
	// installed manifest never declared.
	installation := Installation{
		ID: "t1:c1:stored-agent", Tenant: "t1", Conversation: "c1", AppID: "stored-agent", Version: 1,
		Manifest:      Manifest{AppID: "stored-agent", Version: 1, Scopes: []string{"chat:invoke"}, Agent: &AgentManifest{DisplayName: "Stored Agent"}},
		GrantedScopes: []string{"chat:invoke", "hcm:write"}, Status: Active,
	}
	if err := s.Repo.Put(context.Background(), installation); err != nil {
		t.Fatal(err)
	}
	visible, err := s.Agent(context.Background(), installation.ID)
	if err != nil || !reflect.DeepEqual(visible.Capabilities, []string{"chat:invoke"}) {
		t.Fatalf("undeclared capability disclosed: %+v err=%v", visible, err)
	}
	installation.Manifest.AppID = "different-agent"
	if err := s.Repo.Put(context.Background(), installation); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Agent(context.Background(), installation.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched identity accepted: %v", err)
	}
}

func TestTodo_CHAT_043_Golden(t *testing.T) {
	s, _ := fixture()
	visible, err := s.Agent(context.Background(), "t1:c1:app")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(visible)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"ID":"app","DisplayName":"Helper","Description":"","InstallationID":"t1:c1:app","Status":"ACTIVE","Capabilities":["chat:invoke"]}`
	if string(got) != want {
		t.Fatalf("agent disclosure changed:\n got %s\nwant %s", got, want)
	}
}
