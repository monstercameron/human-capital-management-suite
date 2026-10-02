package agentpersona

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CHATUX-024: an agent's owner may give it a few example questions. The empty
// conversation with the agent offers them to a person who has not asked it
// anything yet. They are words for people: no model reads them and they grant
// nothing.
//
// PersonaProfile.ExampleQuestions holds them the way it holds Guidance: an
// optional field beside the instructions, outside the instructions digest. A
// profile without examples encodes exactly as it did before the field existed,
// so every version sealed earlier still verifies.

const (
	// MaxExampleQuestions is how many example questions a profile may hold. The
	// empty conversation shows three; the rest are there for the owner to choose
	// from without publishing a new version each time.
	MaxExampleQuestions = 6
	// MaxExampleQuestionCharacters keeps an example to what fits on one button.
	MaxExampleQuestionCharacters = 160
)

// ErrExampleQuestion reports an example question the profile may not hold.
var ErrExampleQuestion = errors.New("example questions must be at most six whole sentences of one line each, up to 160 characters, ending in a question mark or a full stop")

// exampleQuestionEnds are the marks a whole sentence may end in, in the
// languages the product is written in.
const exampleQuestionEnds = "?.!؟。？！"

// ValidateExampleQuestions checks the example questions of a profile. An
// example must be a sentence a person could send as it stands: one line, not
// blank, no control characters, and ending where a sentence ends. A stem to be
// finished by the reader ("Summarise the policy on") is refused, because on the
// page it reads as a sentence with its topic missing.
func ValidateExampleQuestions(examples []string) error {
	if len(examples) > MaxExampleQuestions {
		return ErrExampleQuestion
	}
	seen := make(map[string]bool, len(examples))
	for _, example := range examples {
		if !utf8.ValidString(example) || example != strings.TrimSpace(example) || example == "" || utf8.RuneCountInString(example) > MaxExampleQuestionCharacters {
			return ErrExampleQuestion
		}
		for _, r := range example {
			if unicode.IsControl(r) {
				return ErrExampleQuestion
			}
		}
		last, _ := utf8.DecodeLastRuneInString(example)
		if !strings.ContainsRune(exampleQuestionEnds, last) || seen[example] {
			return ErrExampleQuestion
		}
		seen[example] = true
	}
	return nil
}
