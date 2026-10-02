package chat

import (
	"strings"
	"testing"
	"time"
)

// TestTodo_AGENTP_015_FailureSentence: a mention refused by the hourly limit
// says how many times the person asked and when they can ask again, in each
// language, and is not a retryable failure.
func TestTodo_AGENTP_015_FailureSentence(t *testing.T) {
	retryAt := time.Date(2026, 10, 2, 15, 40, 0, 0, time.UTC)
	for _, tc := range []struct{ locale, want string }{
		{"en-US", "You have asked Policy Helper 10 times in the last hour. You can ask again at 3:40 PM."},
		{"de-DE", "Sie haben Policy Helper in der letzten Stunde 10-mal gefragt. Ab 15:40 Uhr können Sie wieder fragen."},
		{"ar", "لقد سألت Policy Helper 10 مرة خلال الساعة الماضية. يمكنك السؤال مرة أخرى في 3:40 م."},
	} {
		got := AgentAnswerMentionLimit(tc.locale, "Policy Helper", 10, retryAt)
		if got.Sentence != tc.want || got.Retryable || got.Class != "mention_limit" {
			t.Fatalf("%s: %+v", tc.locale, got)
		}
	}
	// Without the count or the time the stored code still reads as a limit, not
	// as an agent that is unavailable.
	for _, got := range []AgentAnswerFailure{AgentAnswerMentionLimit("en-US", "Policy Helper", 0, retryAt), AgentAnswerMentionLimit("en-US", "Policy Helper", 10, time.Time{}), AgentAnswerFailureFor("en-US", "Policy Helper", AgentMentionLimitCode)} {
		if got.Class != "limit" || got.Retryable || !strings.Contains(got.Sentence, "limit") {
			t.Fatalf("general fallback: %+v", got)
		}
	}
}
