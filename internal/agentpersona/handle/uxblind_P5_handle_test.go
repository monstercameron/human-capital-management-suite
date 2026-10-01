package handle

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
)

func personaFixture() (PersonaRegistration, PersonRegistration) {
	return PersonaRegistration{
		Tenant:      "harborcare",
		PersonaID:   "persona-comp-analyst",
		Handle:      "comp-analyst",
		DisplayName: "Comp Analyst",
		Agent:       chatapps.Agent{ID: "agent-comp", DisplayName: "Comp Analyst", InstallationID: "harborcare:dm:agent-comp", Status: chatapps.Active, Capabilities: []string{"chat:invoke"}},
	}, PersonRegistration{Tenant: "harborcare", PersonID: "person-dana", Handle: "dana-ruiz", DisplayName: "Dana Ruiz, HR"}
}

func TestTodo_AGENTP_005(t *testing.T) {
	r := NewRegistry()
	persona, _ := personaFixture()
	identity, err := r.RegisterPersona(persona)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Agent.ID != persona.Agent.ID || identity.Agent.InstallationID != persona.Agent.InstallationID || identity.Status != Active {
		t.Fatalf("persona identity did not reuse CHAT-043 agent identity: %+v", identity)
	}
	if _, err := r.RegisterPersona(persona); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("duplicate persona registration error = %v, want ErrAlreadyRegistered", err)
	}
	otherTenant := persona
	otherTenant.Tenant = "ironridge"
	otherTenant.PersonaID = "persona-comp-analyst-ironridge"
	if _, err := r.RegisterPersona(otherTenant); err != nil {
		t.Fatalf("same handle was not allowed in a different tenant: %v", err)
	}
	for _, surface := range []Surface{Post, MentionChip, Notification, SearchResult} {
		projection, err := identity.Project(surface, "@Dana-Ruiz")
		if err != nil {
			t.Fatalf("surface %s: %v", surface, err)
		}
		if !projection.Badge.IsAgent || projection.Badge.AgentID != "agent-comp" || projection.Badge.ActingFor != "acting for @dana-ruiz" {
			t.Fatalf("surface %s lost permanent agent badge or invoker: %+v", surface, projection.Badge)
		}
	}

	person := PersonRegistration{Tenant: "harborcare", PersonID: "person-dana", Handle: "comp_analyst", DisplayName: "Dana Ruiz"}
	if err := r.RegisterPerson(person); !errors.Is(err, ErrConflict) {
		t.Fatalf("human handle collision was accepted: %v", err)
	}
	if err := r.RegisterPerson(PersonRegistration{Tenant: "harborcare", PersonID: "person-dana", Handle: "dana-ruiz", DisplayName: "Comp Analyst"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("human display-name collision with persona was accepted: %v", err)
	}
}

func TestTodo_AGENTP_005_Golden(t *testing.T) {
	persona, _ := personaFixture()
	identity, err := NewRegistry().RegisterPersona(persona)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := identity.Project(Notification, "dana-ruiz")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(projection.Badge)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"is_agent":true,"persona_id":"persona-comp-analyst","agent_id":"agent-comp","label":"Agent","acting_for":"acting for @dana-ruiz"}`
	if string(got) != want {
		t.Fatalf("agent badge changed:\n got %s\nwant %s", got, want)
	}
}

func TestTodo_AGENTP_005_Security(t *testing.T) {
	r := NewRegistry()
	persona, person := personaFixture()
	if err := r.RegisterPerson(person); err != nil {
		t.Fatal(err)
	}
	persona.Handle = "dana.ruiz"
	if _, err := r.RegisterPersona(persona); !errors.Is(err, ErrConflict) {
		t.Fatalf("persona handle collided with human display name without denial: %v", err)
	}
	persona.Handle = "comp-analyst"
	persona.DisplayName = "Dana Ruiz"
	if _, err := r.RegisterPersona(persona); !errors.Is(err, ErrConflict) {
		t.Fatalf("persona display name collided with human handle without denial: %v", err)
	}
	if _, err := r.RegisterPersona(PersonaRegistration{Tenant: "harborcare", PersonaID: "bad", Handle: "Dana Ruiz, HR", DisplayName: "Other", Agent: persona.Agent}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("human-style persona handle was accepted: %v", err)
	}
	if _, err := r.RegisterPersona(PersonaRegistration{Tenant: "harborcare", PersonaID: "suspended", Handle: "suspended-agent", DisplayName: "Suspended Agent", Agent: chatapps.Agent{ID: "a2", InstallationID: "i2", Status: chatapps.Suspended}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-active CHAT-043 identity was accepted: %v", err)
	}
	// CHAT-043's name can be rendered by callers independently of the profile
	// label, so it must reserve the same human namespace too.
	persona.Agent.DisplayName = "Dana Ruiz, HR"
	if _, err := r.RegisterPersona(persona); !errors.Is(err, ErrConflict) {
		t.Fatalf("CHAT-043 display name collided with a human display name without denial: %v", err)
	}
	persona.DisplayName = "Comp Analyst"
	persona.Agent.DisplayName = persona.DisplayName
	identity, err := r.RegisterPersona(persona)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RetirePersona("harborcare", identity.PersonaID); err != nil {
		t.Fatal(err)
	}
	other := persona
	other.PersonaID = "other-persona"
	if _, err := r.RegisterPersona(other); !errors.Is(err, ErrConflict) {
		t.Fatalf("retired handle was reusable: %v", err)
	}
	if err := r.RegisterPerson(PersonRegistration{Tenant: "harborcare", PersonID: "new-person", Handle: persona.Handle, DisplayName: "New Colleague"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("human was allowed to reuse a retired persona handle: %v", err)
	}
	retired, err := r.Persona("harborcare", identity.PersonaID)
	if err != nil || retired.Status != Retired {
		t.Fatalf("retired persona state = %+v, err=%v", retired, err)
	}
	if _, err := retired.Project(Post, "dana-ruiz"); !errors.Is(err, ErrRetired) {
		t.Fatalf("retired persona projected as active: %v", err)
	}

	// A capability slice is caller-owned at the chat boundary; mutating the
	// registration input or returned identity must not alter the stored badge.
	persona.Agent.Capabilities[0] = "forged"
	stored, err := r.Persona("harborcare", identity.PersonaID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored.Agent.Capabilities, []string{"chat:invoke"}) {
		t.Fatalf("stored CHAT-043 identity was mutable through caller data: %+v", stored.Agent.Capabilities)
	}
}

func TestTodo_AGENTP_005_RenameCollisionKeepsCurrentClaims(t *testing.T) {
	r := NewRegistry()
	persona, person := personaFixture()
	if err := r.RegisterPerson(person); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RegisterPersona(persona); err != nil {
		t.Fatal(err)
	}
	renamed := PersonRegistration{Tenant: person.Tenant, PersonID: person.PersonID, Handle: "comp-analyst", DisplayName: "Dana Ruiz"}
	if err := r.RegisterPerson(renamed); !errors.Is(err, ErrConflict) {
		t.Fatalf("colliding rename error = %v, want ErrConflict", err)
	}
	if err := r.CheckPerson(person); err != nil {
		t.Fatalf("failed rename removed current person's namespace claims: %v", err)
	}
}

func TestTodo_AGENTP_005_Property(t *testing.T) {
	corpus := []struct {
		name string
		base string
		look string
	}{
		{name: "cyrillic_a", base: "dana", look: "dаna"},
		{name: "cyrillic_o", base: "comp", look: "cоmp"},
		{name: "greek_rho", base: "pepsona", look: "peρsona"},
		{name: "fullwidth", base: "agent", look: "ａｇｅｎｔ"},
		{name: "accent_and_separators", base: "danaruiz", look: "Dána-Ruiz"},
		{name: "zero_width", base: "analyst", look: "an\u200baly\u200dst"},
	}
	for _, tc := range corpus {
		t.Run(tc.name, func(t *testing.T) {
			base, err := ConfusableSkeleton(tc.base)
			if err != nil {
				t.Fatal(err)
			}
			look, err := ConfusableSkeleton(tc.look)
			if err != nil {
				t.Fatal(err)
			}
			if base != look {
				t.Fatalf("confusable skeleton mismatch: %q=%q, %q=%q", tc.base, base, tc.look, look)
			}
		})
	}

	r := NewRegistry()
	persona, _ := personaFixture()
	if err := r.RegisterPerson(PersonRegistration{Tenant: "ironridge", PersonID: "human", Handle: "dana", DisplayName: "Dana Ruiz"}); err != nil {
		t.Fatal(err)
	}
	persona.Tenant = "ironridge"
	persona.Handle = "dаna"
	persona.PersonaID = "spoof"
	if _, err := r.RegisterPersona(persona); !errors.Is(err, ErrConflict) {
		t.Fatalf("confusable persona handle was accepted: %v", err)
	}
}
