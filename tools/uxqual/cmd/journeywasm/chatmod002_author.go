package main

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATMOD-002: what the client does with a language filter's refusal.
//
// The server refuses a blocked text with InvalidArgument and an ErrorDetail whose
// ReasonRef is chat.content_blocked, one FieldViolation per blocking span: the
// path is "body" (or "text" on the extension surfaces), the rule is
// chatfilter.blocked_span, and the description is "START-END", decimal UTF-8
// byte offsets into strings.TrimSpace of the submitted text, at most sixteen.

const (
	chatmod002Reason      = "chat.content_blocked"
	chatmod002SpanRule    = "chatfilter.blocked_span"
	chatmod002MaxSpans    = 16
	chatmod002WordRuneCap = 80
)

// chatmod002BlockedWords reports whether err is a language-filter refusal and,
// when the offsets are valid for text, the offending words in order.
//
// The offsets count into the trimmed text, so the text's leading whitespace is
// added back. A span that does not land on rune boundaries inside text makes the
// whole answer unnamed: a half-right list would point at the wrong word.
func chatmod002BlockedWords(err error, text string) (blocked bool, words []string) {
	if err == nil {
		return false, nil
	}
	reported, ok := status.FromError(err)
	if !ok || reported.Code() != codes.InvalidArgument {
		return false, nil
	}
	lead := len(text) - len(strings.TrimLeftFunc(text, unicode.IsSpace))
	valid := true
	seen := map[string]bool{}
	for _, d := range reported.Details() {
		detail, ok := d.(*commonv1.ErrorDetail)
		if !ok || detail.GetReasonRef() != chatmod002Reason {
			continue
		}
		blocked = true
		spans := 0
		for _, violation := range detail.GetFieldViolations() {
			if violation.GetRuleRef() != chatmod002SpanRule {
				continue
			}
			spans++
			if spans > chatmod002MaxSpans {
				break
			}
			word, ok := chatmod002Span(text, lead, violation.GetDescription())
			if !ok {
				valid = false
				continue
			}
			if !seen[word] {
				seen[word] = true
				words = append(words, word)
			}
		}
	}
	if !blocked || !valid {
		return blocked, nil
	}
	return true, words
}

// chatmod002Span is the text one "START-END" description names.
func chatmod002Span(text string, lead int, description string) (string, bool) {
	startText, endText, found := strings.Cut(strings.TrimSpace(description), "-")
	if !found {
		return "", false
	}
	start, startErr := strconv.Atoi(startText)
	end, endErr := strconv.Atoi(endText)
	if startErr != nil || endErr != nil || start < 0 || end <= start {
		return "", false
	}
	start, end = start+lead, end+lead
	if end > len(text) || !utf8.RuneStart(text[start]) || (end < len(text) && !utf8.RuneStart(text[end])) {
		return "", false
	}
	word := text[start:end]
	if !utf8.ValidString(word) || strings.TrimSpace(word) == "" || utf8.RuneCountInString(word) > chatmod002WordRuneCap {
		return "", false
	}
	return word, true
}

// chatmod002Notice is the one sentence for a refused text on a surface that has
// no field of its own for it, or "" and false when err is not a filter refusal.
// text may be empty, and then the words are not named.
func chatmod002Notice(err error, surface, text, locale string) (string, bool) {
	blocked, words := chatmod002BlockedWords(err, text)
	if !blocked {
		return "", false
	}
	return chatui.ModAuthorSentence(locale, surface, words), true
}

// chatmod002ErrorCode is the error code a to-do, poll or widget surface records
// for a failed save: the filter's own code for a refusal, else other.
func chatmod002ErrorCode(err error, other string) string {
	if blocked, _ := chatmod002BlockedWords(err, ""); blocked {
		return chatui.ModAuthorErrorBlocked
	}
	return other
}

// chatmod002FieldKey is the key of the field a message to conversation (or to
// the thread under parent) was typed in.
func chatmod002FieldKey(conversation, parent string) string {
	if parent != "" {
		return chatui.ModAuthorKeyReply(parent)
	}
	return chatui.ModAuthorKeyComposer(conversation)
}

// localeTag is the language the chat model is drawn in.
func (s *chatState) localeTag() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.model.Locale == "" {
		return "en-US"
	}
	return s.model.Locale
}

// setAuthorBlocked records a refusal for key while conversationID is the open
// conversation; an answer for a conversation the author has left is dropped.
func (s *chatState) setAuthorBlocked(conversationID, key string, entry chatui.AuthorBlocked) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SelectedID != conversationID {
		return false
	}
	s.model.AuthorBlocked = chatui.WithAuthorBlocked(s.model.AuthorBlocked, key, &entry)
	return true
}

// clearAuthorBlocked drops the refusal for key, if any.
func (s *chatState) clearAuthorBlocked(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.model.AuthorBlocked[key]; ok {
		s.model.AuthorBlocked = chatui.WithAuthorBlocked(s.model.AuthorBlocked, key, nil)
	}
}

// setThreadDraft records what the reply box under root holds. It is local only:
// no render is asked for, the box already shows the text.
func (s *chatState) setThreadDraft(parent, value string) {
	if parent == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]string, len(s.model.ThreadDrafts)+1)
	for k, v := range s.model.ThreadDrafts {
		if k != parent {
			next[k] = v
		}
	}
	if value != "" {
		next[parent] = value
	}
	if len(next) == 0 {
		next = nil
	}
	s.model.ThreadDrafts = next
}

// chatmod002KeyParent reads a field key: the root message of a thread reply box,
// or "" for the main composer. ok is false for a key that is no message box.
func chatmod002KeyParent(key string) (parent string, ok bool) {
	switch {
	case strings.HasPrefix(key, "reply:"):
		return strings.TrimPrefix(key, "reply:"), true
	case strings.HasPrefix(key, "composer:"):
		return "", true
	}
	return "", false
}

// keepFailedText keeps the text of a message that did not send, whatever the
// reason: the main composer takes it back as its draft, a thread reply box keeps
// it while that thread is still open. It names the box to write the text into,
// or "" when the author has moved on and there is nowhere to put it.
func (s *chatState) keepFailedText(conversationID, parent, body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	s.mu.RLock()
	selected, threadOpen := s.model.SelectedID, s.model.ShowThread && s.model.ThreadParentID == parent
	s.mu.RUnlock()
	if selected != conversationID {
		return ""
	}
	if parent == "" {
		s.restoreDraft(conversationID, body)
		return "chat-composer"
	}
	if !threadOpen {
		return ""
	}
	s.setThreadDraft(parent, body)
	return "thread-composer"
}

// dropStaleAuthorBlockedLocked forgets the composer's refusal once the text in
// the field is no longer the text that was refused. The caller holds s.mu.
func (s *chatState) dropStaleAuthorBlockedLocked(conversationID, value string) {
	key := chatui.ModAuthorKeyComposer(conversationID)
	if entry, ok := s.model.AuthorBlocked[key]; ok && strings.TrimSpace(entry.Text) != strings.TrimSpace(value) {
		s.model.AuthorBlocked = chatui.WithAuthorBlocked(s.model.AuthorBlocked, key, nil)
	}
}
