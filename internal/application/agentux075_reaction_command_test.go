package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type agentUX075SettingWriter struct {
	calls []string
	react []bool
	err   error
}

func (w *agentUX075SettingWriter) SetPersonaReactions(_ context.Context, tenant, persona, actor string, react bool, _ time.Time) error {
	w.calls = append(w.calls, tenant+"/"+persona+"/"+actor)
	w.react = append(w.react, react)
	return w.err
}

// "React to questions with an emoji" reaches the store through the same
// authenticated command path as every other Agent setup action: the request is
// mapped to a typed command bound to the signed-in actor, a request that carries
// anything else with it is refused, and only an agent that exists in the actor's
// tenant can be set (AGENTUX-075).
func TestTodo_AGENTUX_075_ReactionCommand(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	off, on := false, true
	if err := factory.ExecutePersonaAdminCommand(ctx, productui.PersonaAdminCommandRequest{Action: "SET_REACTIONS", PersonaID: "policy-helper", ReactToQuestions: &off}); err != nil {
		t.Fatalf("SET_REACTIONS: %v", err)
	}
	if executor.command.Action != PersonaAdminSetReactions || executor.command.PersonaID != "policy-helper" || executor.command.ReactToQuestions == nil || *executor.command.ReactToQuestions || authorizer.action != PersonaAdminSetReactions {
		t.Fatalf("mapped command %+v, authorizer %+v", executor.command, authorizer)
	}
	if executor.actor.Subject != principal.Subject() || executor.actor.Tenant != principal.Tenant() {
		t.Fatalf("the command is not bound to the signed-in actor: %+v", executor.actor)
	}
	calls := executor.calls
	for name, request := range map[string]productui.PersonaAdminCommandRequest{
		"no choice":                  {Action: "SET_REACTIONS", PersonaID: "policy-helper"},
		"no agent":                   {Action: "SET_REACTIONS", ReactToQuestions: &on},
		"a conversation carried":     {Action: "SET_REACTIONS", PersonaID: "policy-helper", ReactToQuestions: &on, ConversationID: "direct"},
		"a decision carried":         {Action: "SET_REACTIONS", PersonaID: "policy-helper", ReactToQuestions: &on, Decision: "APPROVE"},
		"another field carried":      {Action: "SET_REACTIONS", PersonaID: "policy-helper", ReactToQuestions: &on, DisplayName: "Renamed"},
		"the choice on another kind": {Action: "SUSPEND", PersonaID: "policy-helper", ReactToQuestions: &on},
	} {
		if err := factory.ExecutePersonaAdminCommand(ctx, request); !errors.Is(err, ErrPersonaAdminCommandUnavailable) {
			t.Errorf("%s: %v, want a refusal", name, err)
		}
	}
	if executor.calls != calls {
		t.Fatalf("a refused request reached the executor: %d calls, want %d", executor.calls, calls)
	}
	if !validPersonaAdminCommand(PersonaAdminCommand{Action: PersonaAdminSetReactions, PersonaID: "p", ReactToQuestions: &on}) || validPersonaAdminCommand(PersonaAdminCommand{Action: PersonaAdminSetReactions, PersonaID: "p"}) {
		t.Fatal("a command without its choice is valid, or one with it is not")
	}
	if action, ok := personaAdminRoleAction(PersonaAdminSetReactions); !ok || action == "" {
		t.Fatal("SET_REACTIONS has no role action to check")
	}
}

// The executor stores the choice for an agent that exists, as the actor, and
// stops before the store for one that does not.
func TestTodo_AGENTUX_075_ReactionCommandExecutes(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{{PersonaID: "policy-helper", Version: 1}}}
	writer := &agentUX075SettingWriter{}
	executor := &PersonaAdminLifecycleExecutor{Store: personaAdminLifecycleStoreFake{tenant: tenant}, Reactions: writer, Now: func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC) }}
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	off := false
	if err := executor.setReactions(ctx, actor, PersonaAdminCommand{Action: PersonaAdminSetReactions, PersonaID: "policy-helper", ReactToQuestions: &off}); err != nil {
		t.Fatalf("setting the choice: %v", err)
	}
	if len(writer.calls) != 1 || writer.calls[0] != principal.Tenant().String()+"/policy-helper/"+principal.Subject() || writer.react[0] {
		t.Fatalf("stored %v %v, want the actor's choice for the agent in the actor's tenant", writer.calls, writer.react)
	}
	if err := executor.setReactions(ctx, actor, PersonaAdminCommand{Action: PersonaAdminSetReactions, PersonaID: "no-such-agent", ReactToQuestions: &off}); err == nil || len(writer.calls) != 1 {
		t.Fatalf("an agent that does not exist was set: %v, %d writes", err, len(writer.calls))
	}
	if !executor.PersonaAdminCommandAvailable(ctx, PersonaAdminSetReactions) || (&PersonaAdminLifecycleExecutor{}).PersonaAdminCommandAvailable(ctx, PersonaAdminSetReactions) {
		t.Fatal("the action is offered without a store behind it, or not offered with one")
	}
	writer.err = errors.New("down")
	if err := executor.setReactions(ctx, actor, PersonaAdminCommand{Action: PersonaAdminSetReactions, PersonaID: "policy-helper", ReactToQuestions: &off}); !errors.Is(err, ErrPersonaAdminLifecycleUnavailable) {
		t.Fatalf("a store that is down: %v", err)
	}
	if err := (&PersonaAdminLifecycleExecutor{Store: personaAdminLifecycleStoreFake{tenant: tenant}}).setReactions(ctx, actor, PersonaAdminCommand{Action: PersonaAdminSetReactions, PersonaID: "policy-helper", ReactToQuestions: &off}); err == nil {
		t.Fatal("a choice was accepted with no store to keep it")
	}
}
