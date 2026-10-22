package application

import (
	"context"
	"strings"
)

// personaOutcomeEmojiTable is the production outcome chooser (AGENTUX-052):
// it picks the contextual reaction for an answered question from the
// question's words, without a model call. The reactor only consults it for
// an answered question, only from the closed set, and never for a private
// answer in a channel — those keep ✅ so the reaction cannot reveal the
// answer's topic.
type personaOutcomeEmojiTable struct{}

func (personaOutcomeEmojiTable) ChooseOutcomeEmoji(_ context.Context, question string, allowed []string) (string, bool) {
	text := strings.ToLower(strings.TrimSpace(question))
	pick := ""
	switch {
	case text == "":
		// Nothing to read the topic from.
	case agentUX075Mentions(text, "birthday", "birthday's", "geburtstag", "cumpleaños", "عيد ميلاد"):
		pick = "🎂"
	case agentUX075Mentions(text, "payday", "pay check", "paycheck", "salary", "salaries", "compensation", "wage", "gehalt", "lohn", "راتب", "مرتب"):
		pick = "💸"
	case agentUX075Mentions(text, "holiday", "holidays", "vacation", "vacations", "pto", "time off", "day off", "carryover", "carry over", "urlaub", "ferien", "إجازة", "عطلة"):
		pick = "🏖️"
	case agentUX075Mentions(text, "thank", "thx", "appreciate", "danke", "merci", "gracias", "شكرا", "شكراً"):
		pick = "🙏"
	case agentUX075Mentions(text, "congrat", "celebrat", "well done", "great news"):
		pick = "🎉"
	case agentUX075Mentions(text, "when", "schedule", "meeting", "deadline", "termin", "موعد"):
		pick = "📅"
	case agentUX075Mentions(text, "idea", "suggest", "proposal", "idee", "اقتراح"):
		pick = "💡"
	case agentUX075Mentions(text, "fix", "broken", "error", "bug", "fehler", "خطأ"):
		pick = "🔧"
	case agentUX075Mentions(text, "policy", "policies", "rule", "richtlinie", "سياسة"):
		pick = "📝"
	case agentUX075Mentions(text, "travel", "flight", "trip", "reise", "flug", "سفر"):
		pick = "✈️"
	}
	if pick == "" {
		return "", false
	}
	for _, emoji := range allowed {
		if emoji == pick {
			return pick, true
		}
	}
	return "", false
}

var _ personaOutcomeEmojiChooser = personaOutcomeEmojiTable{}
