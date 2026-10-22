package chat

import (
	"strings"
	"testing"
)

// AGENTUX-076: the mark that says an answer is not from the documents is a code
// at the end of the body. Splitting it returns exactly the text that was joined,
// whatever else trails the answer; it survives the private-reason split so that
// every surface drawing the body can say it in the reader's language; and a body
// without it, or with a malformed one, is never changed.
func TestTodo_AGENTUX_076_Ungrounded(t *testing.T) {
	answer := "Paris is the capital of France."
	mark := UngroundedMark()
	if mark == "" || strings.ContainsAny(mark, " ") || !strings.HasPrefix(mark, "<!--chat.agent.") {
		t.Fatalf("the mark is not a comment code: %q", mark)
	}

	for name, tc := range map[string]struct {
		body, clean string
		ungrounded  bool
	}{
		"plain answer":               {answer, answer, false},
		"mark on its own line":       {answer + "\n" + mark, answer, true},
		"mark after blanks":          {answer + "\n\n  " + mark + "\n", answer, true},
		"mark with a trailing token": {answer + "\n" + mark + "\n\n[chat-agent-question-context:abc](/chat/share/x)", answer + "\n\n[chat-agent-question-context:abc](/chat/share/x)", true},
		"mark only":                  {mark, "", true},
		"malformed mark":             {answer + "\n<!--chat.agent.source:non", answer + "\n<!--chat.agent.source:non", false},
		"another source marker":      {answer + "\n<!--chat.agent.source.readable:true-->", answer + "\n<!--chat.agent.source.readable:true-->", false},
	} {
		clean, got := SplitUngrounded(tc.body)
		if clean != tc.clean || got != tc.ungrounded {
			t.Errorf("%s: SplitUngrounded = (%q, %t), want (%q, %t)", name, clean, got, tc.clean, tc.ungrounded)
		}
	}

	// The last mark wins, so an answer that quotes the code in its text keeps the quotation.
	quoted := "The code is " + mark + " in the notes.\n" + mark
	if clean, got := SplitUngrounded(quoted); !got || clean != "The code is "+mark+" in the notes." {
		t.Errorf("a quoted code was taken for the mark: (%q, %t)", clean, got)
	}

	// Through the private-reason split the mark stays on the body, last, so the
	// copy saved in the asker's conversation says it where it is drawn, and the
	// server writes no English sentence into a message a person reads.
	for _, reason := range []string{PrivateReasonAgent, PrivateReasonAsked, PrivateReasonAudience, ""} {
		body := answer + "\n" + mark
		if reason != "" {
			body += "\n" + PrivateReasonMarker(reason)
		}
		clean, got := SplitPrivateReason(body)
		if got != reason || clean != answer+"\n"+mark {
			t.Errorf("reason %q: SplitPrivateReason = (%q, %q), want the answer with the mark kept", reason, clean, got)
		}
		if again, ungrounded := SplitUngrounded(clean); again != answer || !ungrounded {
			t.Errorf("reason %q: the kept mark does not split off: (%q, %t)", reason, again, ungrounded)
		}
	}
	if strings.Contains(answer+"\n"+mark, "Not from your documents") {
		t.Error("English prose in the stored body")
	}
}
