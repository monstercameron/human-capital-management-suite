package main

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatmod002Refusal is a refusal of the exact shape the server sends: code
// InvalidArgument, one ErrorDetail with the content_blocked reason and one
// FieldViolation per span.
func chatmod002Refusal(t *testing.T, reason string, spans ...string) error {
	t.Helper()
	violations := make([]*commonv1.FieldViolation, 0, len(spans))
	for _, span := range spans {
		violations = append(violations, &commonv1.FieldViolation{FieldPath: "body", RuleRef: chatmod002SpanRule, Description: span})
	}
	withDetail, err := status.New(codes.InvalidArgument, "chat.content_blocked").WithDetails(&commonv1.ErrorDetail{ReasonRef: reason, FieldViolations: violations})
	if err != nil {
		t.Fatal(err)
	}
	return withDetail.Err()
}

func TestTodo_CHATMOD_002_Author(t *testing.T) {
	cases := []struct {
		name        string
		draft       string
		spans       []string
		wantWords   []string
		wantBlocked bool
	}{
		{"one word", "you damn fool", []string{"4-8"}, []string{"damn"}, true},
		{"leading whitespace is added back", "  \n you damn fool", []string{"4-8"}, []string{"damn"}, true},
		{"accents count bytes", "café damn", []string{"6-10"}, []string{"damn"}, true},
		{"an accented word", "un idiot é", []string{"3-8"}, []string{"idiot"}, true},
		{"arabic is two bytes a letter", "أنت damn", []string{"7-11"}, []string{"damn"}, true},
		{"an arabic word", "قال أحمق هنا", []string{"7-15"}, []string{"أحمق"}, true},
		{"plural keeps order", "damn you idiot", []string{"0-4", "9-14"}, []string{"damn", "idiot"}, true},
		{"the same word twice is named once", "damn damn", []string{"0-4", "5-9"}, []string{"damn"}, true},
		{"no spans is blocked and unnamed", "damn", nil, nil, true},
		{"end past the text names nothing", "damn", []string{"0-99"}, nil, true},
		{"start inside a rune names nothing", "é damn", []string{"1-3"}, nil, true},
		{"end inside a rune names nothing", "damn é", []string{"0-6"}, nil, true},
		{"reversed span names nothing", "damn", []string{"3-1"}, nil, true},
		{"empty span names nothing", "damn", []string{"2-2"}, nil, true},
		{"not a number names nothing", "damn", []string{"a-b"}, nil, true},
		{"one bad span among good ones names nothing", "damn you idiot", []string{"0-4", "9-99"}, nil, true},
	}
	for _, tc := range cases {
		blocked, words := chatmod002BlockedWords(chatmod002Refusal(t, chatmod002Reason, tc.spans...), tc.draft)
		if blocked != tc.wantBlocked || !reflect.DeepEqual(words, tc.wantWords) {
			t.Errorf("%s: blocked=%v words=%q, want blocked=%v words=%q", tc.name, blocked, words, tc.wantBlocked, tc.wantWords)
		}
	}

	t.Run("at most sixteen spans are read", func(t *testing.T) {
		letters := strings.Fields("a b c d e f g h i j k l m n o p q r s t")
		var offsets []string
		position := 0
		for _, word := range letters {
			offsets = append(offsets, strconv.Itoa(position)+"-"+strconv.Itoa(position+len(word)))
			position += len(word) + 1
		}
		_, words := chatmod002BlockedWords(chatmod002Refusal(t, chatmod002Reason, offsets...), strings.Join(letters, " "))
		if len(words) != 16 {
			t.Fatalf("20 spans named %d words, want 16", len(words))
		}
	})

	t.Run("a different InvalidArgument is not a block", func(t *testing.T) {
		for name, err := range map[string]error{
			"other reason": chatmod002Refusal(t, "structural.request_rejected", "0-4"),
			"no detail":    status.Error(codes.InvalidArgument, "chat.body_required"),
			"plain error":  errors.New("boom"),
			"wrong code":   status.Error(codes.PermissionDenied, "chat.content_blocked"),
			"unavailable":  status.Error(codes.Unavailable, "down"),
			"nil":          nil,
		} {
			if blocked, words := chatmod002BlockedWords(err, "damn"); blocked || words != nil {
				t.Errorf("%s was treated as a block: %v %q", name, blocked, words)
			}
		}
		other := chatmod002Refusal(t, "structural.request_rejected", "0-4")
		if got := chatFriendlyError(other); got != "The details were not accepted." {
			t.Fatalf("other InvalidArgument reads %q", got)
		}
		if got := actionFailureNotice("save this edit", other); got != "We couldn't save this edit. The details were not accepted." {
			t.Fatalf("other InvalidArgument notice reads %q", got)
		}
	})

	t.Run("the refusal reads as the owner's sentence in three languages", func(t *testing.T) {
		blocked := chatmod002Refusal(t, chatmod002Reason, "0-4", "9-14")
		_, words := chatmod002BlockedWords(blocked, "damn you idiot")
		want := map[string]string{
			"en-US": `This message was not sent: it contains words this workspace does not allow: "damn", "idiot".`,
			"de-DE": "Diese Nachricht wurde nicht gesendet: Sie enthält Wörter, die dieser Arbeitsbereich nicht erlaubt: „damn“, „idiot“.",
		}
		for locale, sentence := range want {
			if got := chatui.ModAuthorSentence(locale, chatui.ModAuthorSurfaceMessage, words); got != sentence {
				t.Errorf("%s: %q, want %q", locale, got, sentence)
			}
		}
		ar := chatui.ModAuthorSentence("ar", chatui.ModAuthorSurfaceMessage, words)
		if !strings.Contains(ar, "لم تُرسل هذه الرسالة") || !strings.Contains(ar, "damn") || !strings.Contains(ar, "idiot") || !strings.Contains(ar, "⁨") {
			t.Errorf("ar sentence: %q", ar)
		}
		if got := chatui.ModAuthorSentence("en-US", chatui.ModAuthorSurfaceMessage, words[:1]); got != `This message was not sent: it contains a word this workspace does not allow: "damn".` {
			t.Errorf("singular: %q", got)
		}
		if got := chatui.ModAuthorSentence("en-US", chatui.ModAuthorSurfaceMessage, nil); got != "This message was not sent: it contains a word this workspace does not allow." {
			t.Errorf("unnamed: %q", got)
		}
		// Anywhere that reads the error without a field says it was not saved, and
		// never that the service did not answer.
		notice := actionFailureNotice("send this message", blocked)
		if notice != "This was not saved: it contains a word this workspace does not allow." {
			t.Errorf("notice: %q", notice)
		}
		if got := chatFriendlyError(blocked); strings.Contains(got, "not accepted") || strings.Contains(notice, "We couldn't") || strings.Contains(notice, "did not answer") {
			t.Errorf("the generic sentence leaked: %q %q", got, notice)
		}
	})

	t.Run("the error code for to-do, poll and widget surfaces", func(t *testing.T) {
		if got := chatmod002ErrorCode(chatmod002Refusal(t, chatmod002Reason, "0-4"), "save"); got != chatui.ModAuthorErrorBlocked {
			t.Errorf("blocked: %q", got)
		}
		if got := chatmod002ErrorCode(status.Error(codes.Unavailable, "down"), "save"); got != "save" {
			t.Errorf("other: %q", got)
		}
	})
}

// Whatever the reason a message did not send, its text stays in its box, and
// nowhere once the author has moved on.
func TestTodo_CHATMOD_002_AuthorKeepsFailedText(t *testing.T) {
	s := &chatState{}
	s.mutate(func(m *chatui.Model) { m.SelectedID, m.ShowThread, m.ThreadParentID = "c1", true, "root" })
	if box := s.keepFailedText("c1", "root", "a reply"); box != "thread-composer" || s.snapshot().ThreadDrafts["root"] != "a reply" {
		t.Fatalf("thread reply: box=%q drafts=%v", box, s.snapshot().ThreadDrafts)
	}
	// Typing reports the box's text, and sending empties it.
	s.setThreadDraft("root", "a reply, edited")
	s.setThreadDraft("root", "")
	if len(s.snapshot().ThreadDrafts) != 0 {
		t.Fatalf("a sent reply left its text: %v", s.snapshot().ThreadDrafts)
	}
	if box := s.keepFailedText("c1", "elsewhere", "a reply"); box != "" || len(s.snapshot().ThreadDrafts) != 0 {
		t.Fatalf("a reply to a thread that is not open was kept: %q %v", box, s.snapshot().ThreadDrafts)
	}
	if box := s.keepFailedText("c2", "root", "a reply"); box != "" {
		t.Fatalf("a reply for a conversation the author left was kept: %q", box)
	}
	if box := s.keepFailedText("c1", "root", "  \n"); box != "" {
		t.Fatalf("blank text was kept: %q", box)
	}
	if box := s.keepFailedText("c1", "", "a message"); box != "chat-composer" || s.snapshot().Draft != "a message" {
		t.Fatalf("main composer: box=%q draft=%q", box, s.snapshot().Draft)
	}
	model := s.snapshot()
	swapChatDraft(&model, map[string]string{}, "c2")
	if model.ThreadDrafts != nil {
		t.Fatal("a thread draft followed the author into another conversation")
	}
	for key, want := range map[string]string{"reply:root": "root", "composer:c1": ""} {
		if parent, ok := chatmod002KeyParent(key); !ok || parent != want {
			t.Errorf("%s: parent=%q ok=%v", key, parent, ok)
		}
	}
	if _, ok := chatmod002KeyParent(chatui.ModAuthorKeyEdit("p")); ok {
		t.Error("an edit key was taken for a message box")
	}
}

// The refusal belongs to the open conversation and to the text it refused.
func TestTodo_CHATMOD_002_AuthorState(t *testing.T) {
	s := &chatState{}
	s.mutate(func(m *chatui.Model) { m.SelectedID = "c1" })
	key := chatui.ModAuthorKeyComposer("c1")
	if !s.setAuthorBlocked("c1", key, chatui.AuthorBlocked{Surface: chatui.ModAuthorSurfaceMessage, Words: []string{"damn"}, Text: "you damn fool", Stamp: 1}) {
		t.Fatal("a refusal for the open conversation was dropped")
	}
	if s.setAuthorBlocked("c2", chatui.ModAuthorKeyComposer("c2"), chatui.AuthorBlocked{Text: "x", Stamp: 2}) {
		t.Fatal("a refusal for a conversation the author left was kept")
	}
	if got := s.snapshot().AuthorBlocked; len(got) != 1 || got[key].Words[0] != "damn" {
		t.Fatalf("model: %+v", got)
	}
	// Typing a different text drops it; the same text (trimmed) does not.
	s.setDraft("c1", "you damn fool  ")
	if len(s.snapshot().AuthorBlocked) != 1 {
		t.Fatal("the refusal went away although the text did not change")
	}
	s.setDraft("c1", "you fool")
	if len(s.snapshot().AuthorBlocked) != 0 {
		t.Fatal("the refusal stayed after the text changed")
	}
	// Sending clears the draft, which clears it too; opening another conversation clears all.
	s.setAuthorBlocked("c1", key, chatui.AuthorBlocked{Text: "damn", Stamp: 3})
	s.setAuthorBlocked("c1", chatui.ModAuthorKeyEdit("p1"), chatui.AuthorBlocked{Text: "damn", Stamp: 4})
	s.setDraft("c1", "")
	if got := s.snapshot().AuthorBlocked; len(got) != 1 {
		t.Fatalf("after send: %+v", got)
	}
	model := s.snapshot()
	swapChatDraft(&model, map[string]string{}, "c2")
	if model.AuthorBlocked != nil {
		t.Fatalf("a refusal followed the author into another conversation: %+v", model.AuthorBlocked)
	}
	s.clearAuthorBlocked(chatui.ModAuthorKeyEdit("p1"))
	if len(s.snapshot().AuthorBlocked) != 0 {
		t.Fatal("clearAuthorBlocked left the entry")
	}
}

// TestTodo_CHATUX_018_RuleName: the refusal's rule name is read from its own
// violation; a name the server could not publish, or none, names nothing.
func TestTodo_CHATUX_018_RuleName(t *testing.T) {
	refusal := func(rule string) error {
		violations := []*commonv1.FieldViolation{{FieldPath: "body", RuleRef: chatmod002SpanRule, Description: "0-4"}}
		if rule != "" {
			violations = append(violations, &commonv1.FieldViolation{FieldPath: "body", RuleRef: chatmod002RuleRef, Description: rule})
		}
		withDetail, err := status.New(codes.InvalidArgument, "chat.content_blocked").WithDetails(&commonv1.ErrorDetail{ReasonRef: chatmod002Reason, FieldViolations: violations})
		if err != nil {
			t.Fatal(err)
		}
		return withDetail.Err()
	}
	for rule, want := range map[string]string{
		"Built-in word list: Profanity": "Built-in word list: Profanity",
		"  Project Falcon 1.2.0  ":      "Project Falcon 1.2.0",
		"":                              "",
		"field rejected by chatfilter.blocked_rule":  "",
		strings.Repeat("a", chatmod002WordRuneCap+1): "",
	} {
		if got := chatmod002BlockedRule(refusal(rule)); got != want {
			t.Errorf("rule %q read as %q, want %q", rule, got, want)
		}
	}
	if chatmod002BlockedRule(errors.New("other")) != "" || chatmod002BlockedRule(nil) != "" {
		t.Error("a name was read from an error that is not a refusal")
	}
	// The words are still read from the same refusal.
	if blocked, words := chatmod002BlockedWords(refusal("Profanity"), "damn you"); !blocked || !reflect.DeepEqual(words, []string{"damn"}) {
		t.Errorf("the rule violation disturbed the words: %v %v", blocked, words)
	}
	if got := chatui.ModAuthorRuleLine("en-US", "Profanity"); got != "Rule: Profanity" {
		t.Errorf("rule line %q", got)
	}
}
