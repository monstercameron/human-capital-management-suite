package chatblocked

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

func TestTodo_CHATMOD_002_BlockedEnvelope(t *testing.T) {
	blocked := &chatfilter.BlockedError{RuleName: "Profanity", Span: chatfilter.Span{Start: 9, End: 13}}
	owned, ok := Envelope(fmt.Errorf("chat: %w", blocked), "body")
	if !ok || owned.GRPCCode() != codes.InvalidArgument || owned.ReasonRef() != ReasonRef || owned.Retryable() {
		t.Fatalf("blocked text must be a non-retryable invalid argument: %+v", owned)
	}
	violations := owned.Violations()
	if len(violations) == 0 || violations[0].FieldPath != "body" || violations[0].RuleRef != SpanRuleRef || violations[0].Description != "9-13" {
		t.Fatalf("the span is not carried: %+v", violations)
	}
	// What crosses the wire: a gRPC status whose detail carries the same facts,
	// and no rule name, list term or message text.
	st := status.Convert(owned)
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("status %v", st.Code())
	}
	var detail *commonv1.ErrorDetail
	for _, d := range st.Details() {
		if ed, isDetail := d.(*commonv1.ErrorDetail); isDetail {
			detail = ed
		}
	}
	if detail == nil || detail.GetReasonRef() != ReasonRef || len(detail.GetFieldViolations()) == 0 || detail.GetFieldViolations()[0].GetDescription() != "9-13" {
		t.Fatalf("detail %+v", detail)
	}
	for _, leaked := range []string{"Profanity", "damn"} {
		if contains(st.Message(), leaked) || contains(owned.Error(), leaked) {
			t.Fatalf("the refusal leaks %q: %s", leaked, owned.Error())
		}
	}
	// A bare sentinel is still a refusal, without spans.
	if bare, ok := Envelope(chatfilter.ErrBlocked, "text"); !ok || bare.ReasonRef() != ReasonRef || len(bare.Violations()) != 0 {
		t.Fatalf("bare sentinel: %+v %v", bare, ok)
	}
	// Unusable text and an unavailable filter are told apart from a refusal.
	if invalid, ok := Envelope(chatfilter.ErrInvalid, "body"); !ok || invalid.GRPCCode() != codes.InvalidArgument || invalid.ReasonRef() == ReasonRef {
		t.Fatalf("invalid: %+v", invalid)
	}
	if down, ok := Envelope(chatfilter.ErrUnavailable, "body"); !ok || down.GRPCCode() != codes.Unavailable {
		t.Fatalf("unavailable: %+v", down)
	}
	if _, ok := Envelope(errors.New("other"), "body"); ok {
		t.Fatal("an unrelated error was claimed")
	}
	if _, ok := Envelope(nil, "body"); ok {
		t.Fatal("nil was claimed")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestTodo_CHATMOD_002_BlockedEnvelope_ManyWords: every blocking word is named,
// in order, up to the cap; the author fixes them all in one pass.
func TestTodo_CHATMOD_002_BlockedEnvelope_ManyWords(t *testing.T) {
	spans := []chatfilter.Span{{Start: 0, End: 4}, {Start: 10, End: 15}}
	owned, _ := Envelope(&chatfilter.BlockedError{Span: spans[0], Spans: spans}, "body")
	got := owned.Violations()
	if len(got) != 2 || got[0].Description != "0-4" || got[1].Description != "10-15" {
		t.Fatalf("spans %+v", got)
	}
	var many []chatfilter.Span
	for i := 0; i < 40; i++ {
		many = append(many, chatfilter.Span{Start: i * 10, End: i*10 + 3})
	}
	owned, _ = Envelope(&chatfilter.BlockedError{Span: many[0], Spans: many}, "text")
	if n := len(owned.Violations()); n != MaxSpans || owned.Violations()[0].FieldPath != "text" {
		t.Fatalf("the cap is %d, got %d", MaxSpans, n)
	}
}

// TestTodo_CHATUX_018_RuleName: the refusal names the rule that refused the text,
// as its own violation after the spans, kept to one short line; the message and
// the error string still carry neither the rule nor the text.
func TestTodo_CHATUX_018_RuleName(t *testing.T) {
	owned, _ := Envelope(&chatfilter.BlockedError{RuleName: "Built-in word list: Profanity", Span: chatfilter.Span{Start: 9, End: 13}}, "body")
	got := owned.Violations()
	if len(got) != 2 || got[0].RuleRef != SpanRuleRef || got[1].RuleRef != RuleNameRef || got[1].Description != "Built-in word list: Profanity" || got[1].FieldPath != "body" {
		t.Fatalf("violations %+v", got)
	}
	if contains(owned.Error(), "Profanity") {
		t.Fatalf("the error string carries the rule: %s", owned.Error())
	}
	long := ""
	for i := 0; i < 200; i++ {
		long += "a"
	}
	owned, _ = Envelope(&chatfilter.BlockedError{RuleName: long, Span: chatfilter.Span{Start: 0, End: 1}}, "body")
	if got = owned.Violations(); len(got) != 2 || len(got[1].Description) != MaxRuleNameRunes {
		t.Fatalf("the name is not bounded: %+v", got)
	}
	// A refusal with no rule name says none.
	owned, _ = Envelope(&chatfilter.BlockedError{Span: chatfilter.Span{Start: 0, End: 1}}, "body")
	if got = owned.Violations(); len(got) != 1 {
		t.Fatalf("an empty name is carried: %+v", got)
	}
}
