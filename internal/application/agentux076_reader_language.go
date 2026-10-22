package application

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// AGENTUX-076 / CHATLANG-005: an agent answers in the language its asker reads.
// The run's model work puts one developer message after the agent's own
// instructions when the asker's reading language is one the product offers and
// is not English, which the agent answers in already. The outbound verifier asks
// for the same language through the same function, so the message it rebuilds is
// the message that was sent.
//
// The language is read once for a few seconds and shared by the model work and
// the verifier, so a person changing their language while a run is in flight
// cannot make the verifier refuse what the work built.

// personaReaderLanguageTTL is how long one read of a person's reading language
// is shared between a run's model work and its verifier.
const personaReaderLanguageTTL = 20 * time.Second

type personaReaderLanguageEntry struct {
	language string
	at       time.Time
}

// CachedPersonaReaderLanguage wraps a reading-language read so that it is made
// once per person for personaReaderLanguageTTL. Composition builds one and gives
// it to both the model work and the verifier.
func CachedPersonaReaderLanguage(read func(context.Context, string, string) string) func(context.Context, string, string) string {
	if read == nil {
		return nil
	}
	var mu sync.Mutex
	entries := map[string]personaReaderLanguageEntry{}
	return func(ctx context.Context, tenant, person string) string {
		key := tenant + "\x00" + person
		now := time.Now()
		mu.Lock()
		entry, ok := entries[key]
		mu.Unlock()
		if ok && now.Sub(entry.at) <= personaReaderLanguageTTL {
			return entry.language
		}
		language := read(ctx, tenant, person)
		mu.Lock()
		for name, held := range entries {
			if now.Sub(held.at) > personaReaderLanguageTTL {
				delete(entries, name)
			}
		}
		entries[key] = personaReaderLanguageEntry{language: language, at: now}
		mu.Unlock()
		return language
	}
}

// personaReaderLanguageMessage is the message a run adds for this asker, or
// false when there is none: no language is known, it is not one the product
// offers, or it is English.
func personaReaderLanguageMessage(ctx context.Context, read func(context.Context, string, string) string, tenant, person string) (agentmodel.ModelMessage, bool) {
	if read == nil {
		return agentmodel.ModelMessage{}, false
	}
	tag := read(ctx, tenant, person)
	if chatlang.LanguageName(tag) == "English" {
		return agentmodel.ModelMessage{}, false
	}
	return PersonaReaderLanguageMessage(tag)
}

// addPersonaReaderLanguage adds the reader-language message to a run's model
// request, right after the agent's own instructions, when there is one.
func addPersonaReaderLanguage(ctx context.Context, read func(context.Context, string, string) string, tenant, person string, model *agentmodel.ModelRequest) {
	if message, ok := personaReaderLanguageMessage(ctx, read, tenant, person); ok && len(model.Messages) >= 2 {
		model.Messages = slices.Insert(model.Messages, 2, message)
	}
}
