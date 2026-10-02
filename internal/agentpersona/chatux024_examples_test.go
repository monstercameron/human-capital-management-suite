package agentpersona

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// An agent's definition may hold example questions. They are sealed with the
// profile like its guidance, a profile without them encodes as it always did,
// and only whole sentences are accepted.
func TestTodo_CHATUX_024(t *testing.T) {
	profile, catalog := personaFixture(t)
	legacy := mustPersonaVersion(t, profile, personaValidator(catalog))
	encoded, err := json.Marshal(legacy.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "example_questions") {
		t.Fatalf("a profile with no examples changed its encoding: %s", encoded)
	}
	// An empty list is the same profile as no list: the digest is the old one.
	profile.ExampleQuestions = []string{}
	if empty := mustPersonaVersion(t, profile, personaValidator(catalog)); empty.Digest != legacy.Digest || empty.Verify() != nil {
		t.Fatalf("an empty example list changed the digest: %s != %s", empty.Digest, legacy.Digest)
	}

	profile.ExampleQuestions = []string{"How many days of leave carry over?", "Which holidays are left this year?", "Summarise the travel policy.", "ما العطل المتبقية هذا العام؟"}
	withExamples := mustPersonaVersion(t, profile, personaValidator(catalog))
	if withExamples.Digest == legacy.Digest || len(withExamples.Profile.ExampleQuestions) != 4 || withExamples.Verify() != nil {
		t.Fatalf("examples were not sealed into the profile: %+v", withExamples)
	}
	// The instructions digest is the instructions' alone.
	if withExamples.Profile.InstructionsDigest != legacy.Profile.InstructionsDigest {
		t.Fatal("example questions changed the instructions digest")
	}
	// The sealed version does not share the caller's slice.
	profile.ExampleQuestions[0] = "Changed after sealing?"
	if withExamples.Profile.ExampleQuestions[0] != "How many days of leave carry over?" || withExamples.Verify() != nil {
		t.Fatal("the sealed profile shares its examples with the caller")
	}
	// It survives storage: decoded and sealed again it has the same digest.
	stored, err := json.Marshal(withExamples.Profile)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PersonaProfile
	if err = json.Unmarshal(stored, &decoded); err != nil {
		t.Fatal(err)
	}
	if again, err := Seal(decoded); err != nil || again.Digest != withExamples.Digest {
		t.Fatalf("a stored profile with examples does not verify: %v", err)
	}

	for name, examples := range map[string][]string{
		"a stem to be finished":   {"Summarise the policy on"},
		"a stem with its space":   {"Summarise the policy on "},
		"blank":                   {""},
		"space around":            {" How many days carry over?"},
		"two lines":               {"How many days carry over?\nAnd holidays?"},
		"a control character":     {"How many days\u0000 carry over?"},
		"too long":                {strings.Repeat("a", MaxExampleQuestionCharacters) + "?"},
		"the same question twice": {"How many days carry over?", "How many days carry over?"},
		"too many":                {"One?", "Two?", "Three?", "Four?", "Five?", "Six?", "Seven?"},
	} {
		if err := ValidateExampleQuestions(examples); !errors.Is(err, ErrExampleQuestion) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
		profile.ExampleQuestions = examples
		if _, err := personaValidator(catalog).Build(profile); !errors.Is(err, ErrInvalidProfile) || !errors.Is(err, ErrExampleQuestion) {
			t.Errorf("%s: a profile holding it was sealed (%v)", name, err)
		}
	}
	if err := ValidateExampleQuestions(nil); err != nil {
		t.Fatalf("no examples: %v", err)
	}
	if err := ValidateExampleQuestions([]string{"One?", "Two.", "Three!", "أربعة؟", "五。", "Six？"}); err != nil {
		t.Fatalf("six whole sentences: %v", err)
	}
}
