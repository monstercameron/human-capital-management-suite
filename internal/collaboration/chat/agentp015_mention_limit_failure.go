package chat

import (
	"strconv"
	"strings"
	"time"
)

// AgentMentionLimitCode is the outcome of a mention refused because the person
// had asked the same agent as many times as the hour allows (AGENTP-015).
const AgentMentionLimitCode = "MENTION_LIMIT_REACHED"

// AgentAnswerMentionLimit is the card's wording for that refusal: how many
// times the person asked this agent in the last hour and the time they can ask
// again, in the clock the person reads (12-hour in English, 24-hour in German,
// the Arabic day-period in Arabic). The card has no "Ask again" until then,
// because the same question would be refused the same way. A refusal that does
// not carry both facts falls back to the general limit sentence.
func AgentAnswerMentionLimit(locale, agent string, asked int, retryAt time.Time) AgentAnswerFailure {
	if asked < 1 || retryAt.IsZero() {
		return AgentAnswerFailureFor(locale, agent, AgentMentionLimitCode)
	}
	count := strconv.Itoa(asked)
	sentence := ""
	switch {
	case strings.HasPrefix(locale, "de"):
		sentence = "Sie haben {agent} in der letzten Stunde " + count + "-mal gefragt. Ab " + retryAt.Format("15:04") + " Uhr können Sie wieder fragen."
	case strings.HasPrefix(locale, "ar"):
		period := "ص"
		if retryAt.Hour() >= 12 {
			period = "م"
		}
		sentence = "لقد سألت {agent} " + count + " مرة خلال الساعة الماضية. يمكنك السؤال مرة أخرى في " + retryAt.Format("3:04") + " " + period + "."
	default:
		sentence = "You have asked {agent} " + count + " times in the last hour. You can ask again at " + retryAt.Format("3:04 PM") + "."
	}
	return AgentAnswerFailure{Class: "mention_limit", Sentence: strings.ReplaceAll(sentence, "{agent}", agent), Retryable: false}
}
