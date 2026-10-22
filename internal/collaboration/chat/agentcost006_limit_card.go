package chat

import "strings"

// AgentDailyLimitCode is the outcome of a run an owner's daily limit stopped
// (AGENTCOST-006).
const AgentDailyLimitCode = "DAILY_LIMIT_REACHED"

// AgentAnswerDailyLimit is the card's wording for that refusal: which agent, in
// which conversation, and when the day ends. The day is midnight to midnight UTC
// (the product carries no workspace time zone yet), so the sentence says UTC
// rather than leave a reader in another zone to read "00:00" as their own. A
// card that cannot name the conversation says it for the agent.
func AgentAnswerDailyLimit(locale, agent, conversation string) AgentAnswerFailure {
	card := AgentAnswerFailureFor(locale, agent, AgentDailyLimitCode)
	conversation = strings.TrimSpace(conversation)
	if conversation == "" {
		return card
	}
	sentence := "{agent} reached today's limit for {conversation}. It resets at 00:00 UTC."
	switch {
	case strings.HasPrefix(locale, "de"):
		sentence = "{agent} hat das heutige Limit für {conversation} erreicht. Es wird um 00:00 Uhr UTC zurückgesetzt."
	case strings.HasPrefix(locale, "ar"):
		sentence = "بلغ {agent} حد اليوم في {conversation}. يُعاد تعيينه عند 00:00 بالتوقيت العالمي (UTC)."
	}
	card.Sentence = strings.NewReplacer("{agent}", agent, "{conversation}", conversation).Replace(sentence)
	return card
}
