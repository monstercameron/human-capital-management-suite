package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

func TestComposePersonaPickerCompositionRequiresProductionSources(t *testing.T) {
	store := (*agentpersonastore.Store)(nil)
	for _, tc := range []struct {
		name string
		in   PersonaPickerCompositionInput
	}{
		{name: "missing available persona reader", in: PersonaPickerCompositionInput{Store: store}},
		{name: "missing production store", in: PersonaPickerCompositionInput{Personas: personaCandidateProfiles{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComposePersonaPickerComposition(tc.in)
			if err == nil {
				t.Fatalf("composition=%+v, want error", got)
			}
			if got.PersonaReferences != nil {
				t.Fatalf("composition supplied fallback persona references: %+v", got)
			}
		})
	}
}

func TestComposePersonaPickerCompositionWiresProductionReferenceSource(t *testing.T) {
	composition, err := ComposePersonaPickerComposition(PersonaPickerCompositionInput{
		Personas: personaCandidateProfiles{},
		Store:    &agentpersonastore.Store{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if composition.PersonaReferences == nil {
		t.Fatal("persona references were not composed")
	}
	if _, ok := composition.PersonaReferences.(*productionPersonaChatReferenceSource); !ok {
		t.Fatalf("persona references type=%T, want production source", composition.PersonaReferences)
	}
}

func TestComposePersonaPickerCompositionDoesNotAcceptTypedNilStore(t *testing.T) {
	var store *agentpersonastore.Store
	_, err := ComposePersonaPickerComposition(PersonaPickerCompositionInput{
		Personas: personaCandidateProfiles{},
		Store:    store,
	})
	if err == nil || err.Error() != "application: persona picker requires the production persona store" {
		t.Fatalf("error=%v", err)
	}
}
