package application

import (
	"regexp"
	"testing"
)

// Before/after for the per-call MustCompile hoists. The "Inline" variants
// replicate line-for-line the compilation each call site did before; the
// "Shared" variants use the hoisted package vars. The matched inputs are
// representative of production traffic.

var chatrevBenchTitle = "Budget review (version 3, approved)"

func BenchmarkChatrev_LegacyVersionInline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		legacy := regexp.MustCompile(` \(version ([0-9]+)(?:,.*)?\)$`)
		_ = legacy.FindStringSubmatch(chatrevBenchTitle)
		_ = legacy.ReplaceAllString(chatrevBenchTitle, "")
	}
}

func BenchmarkChatrev_LegacyVersionShared(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = agentUXAnswerLegacyVersionPattern.FindStringSubmatch(chatrevBenchTitle)
		_ = agentUXAnswerLegacyVersionPattern.ReplaceAllString(chatrevBenchTitle, "")
	}
}

var chatrevBenchReminder = "remind me tomorrow at 3:30pm about the 2026-11-02 review"

func BenchmarkChatrev_AmbientScreenInline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`).FindString(chatrevBenchReminder)
		_ = regexp.MustCompile(`\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)\b`).FindStringSubmatch(chatrevBenchReminder)
	}
}

func BenchmarkChatrev_AmbientScreenShared(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = agentUXAmbientISODatePattern.FindString(chatrevBenchReminder)
		_ = agentUXAmbientClock12Pattern.FindStringSubmatch(chatrevBenchReminder)
	}
}

var chatrevBenchDraft = "Upcoming: team lunch on Jan 12th and the review on 2026-11-02"

func BenchmarkChatrev_ReplyRuleInline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`).FindAllString(chatrevBenchDraft, -1)
		pattern := regexp.MustCompile(`(?i)\b(Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)\.?\s+(\d{1,2})(?:st|nd|rd|th)?(?:,?\s+(20\d{2}))?\b`)
		_ = pattern.FindAllStringSubmatch(chatrevBenchDraft, -1)
	}
}

func BenchmarkChatrev_ReplyRuleShared(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = announcementReplyISODatePattern.FindAllString(chatrevBenchDraft, -1)
		_ = announcementReplyMonthPattern.FindAllStringSubmatch(chatrevBenchDraft, -1)
	}
}

var chatrevBenchReply = "See [the policy](https://example.invalid/policy) and ![chart](https://example.invalid/c.png) plus www.example.invalid/x"

func BenchmarkChatrev_ReplySanitizeInline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		body := regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`).ReplaceAllString(chatrevBenchReply, "")
		linkPattern := regexp.MustCompile(`\[([^\]]+)\]\(([^)]*)\)`)
		body = linkPattern.ReplaceAllString(body, "$1")
		_ = regexp.MustCompile(`(?i)(?:https?://|www\.|//)[^\s<>()]+`).ReplaceAllString(body, "")
	}
}

func BenchmarkChatrev_ReplySanitizeShared(b *testing.B) {
	for i := 0; i < b.N; i++ {
		body := personaReplyOutputImagePattern.ReplaceAllString(chatrevBenchReply, "")
		body = personaReplyOutputLinkPattern.ReplaceAllString(body, "$1")
		_ = personaReplyOutputUnsafeURLPattern.ReplaceAllString(body, "")
	}
}
