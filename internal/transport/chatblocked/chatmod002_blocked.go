// Package chatblocked projects a language-filter refusal onto the owned error
// model (CHATMOD-002). A message, edit, name or to-do text that the workspace's
// filters block is a request the author can fix, so it is an invalid-argument
// condition that carries what the author needs to fix it: the byte span of every
// blocking term in the text they submitted, and the name of the rule that refused
// it. Nothing else about the rule crosses
// the edge (not the list's other terms and not the rule's contents).
package chatblocked

import (
	"errors"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

const (
	// ReasonRef is the stable owned reason clients match on.
	ReasonRef = "chat.content_blocked"
	// SpanRuleRef marks a field violation whose description is "START-END".
	SpanRuleRef = "chatfilter.blocked_span"
	// RuleNameRef marks the field violation whose description is the name of the
	// rule that refused the text.
	RuleNameRef = "chatfilter.blocked_rule"
	// MaxRuleNameRunes bounds the name that crosses the edge.
	MaxRuleNameRunes = 80
	// MaxSpans bounds how many blocking terms are named.
	MaxSpans = 16
)

// Envelope returns the owned error for a filter refusal or failure, and false
// for any other error. field is the request field that held the text ("body"
// for a message, "text" for to-do, poll and widget text). Offsets are UTF-8
// byte offsets into the TRIMMED text the service judged.
func Envelope(err error, field string) (*envelope.Error, bool) {
	if err == nil {
		return nil, false
	}
	var blocked *chatfilter.BlockedError
	switch {
	case errors.As(err, &blocked):
		out := envelope.New(envelope.CodeInvalidArgument, ReasonRef, "the text contains a word this workspace does not allow")
		for i, span := range blockedSpans(blocked) {
			if i == MaxSpans {
				break
			}
			out.WithViolation(field, strconv.Itoa(span.Start)+"-"+strconv.Itoa(span.End), SpanRuleRef)
		}
		// The author is told which rule refused the text, by the name an
		// administrator gave it (CHATUX-018): the word alone does not say whether
		// it was a built-in list or the workspace's own.
		if name := ruleName(blocked.RuleName); name != "" {
			out.WithViolation(field, name, RuleNameRef)
		}
		return out, true
	case errors.Is(err, chatfilter.ErrBlocked):
		return envelope.New(envelope.CodeInvalidArgument, ReasonRef, "the text contains a word this workspace does not allow"), true
	case errors.Is(err, chatfilter.ErrInvalid):
		return envelope.New(envelope.CodeInvalidArgument, "chat.filter_invalid_text", "the text could not be checked"), true
	case errors.Is(err, chatfilter.ErrUnavailable):
		return envelope.New(envelope.CodeUnavailable, "chat.filter_unavailable", "message filters are unavailable"), true
	}
	return nil, false
}

func blockedSpans(b *chatfilter.BlockedError) []chatfilter.Span {
	if len(b.Spans) > 0 {
		return b.Spans
	}
	return []chatfilter.Span{b.Span}
}

// ruleName is the rule's name as one short line, or "" when it has none worth
// showing. The envelope refuses a description it cannot publish, so the name is
// kept to printable text of bounded length.
func ruleName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	runes := []rune(name)
	if len(runes) > MaxRuleNameRunes {
		runes = runes[:MaxRuleNameRunes]
	}
	return string(runes)
}
