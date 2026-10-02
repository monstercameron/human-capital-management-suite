package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

type chattoneFilterFixture struct {
	result chatfilter.Result
	err    error
	inputs []chatfilter.Input
	record []bool
}

func (f *chattoneFilterFixture) Evaluate(_ context.Context, in chatfilter.Input, record bool) (chatfilter.Result, error) {
	f.inputs = append(f.inputs, in)
	f.record = append(f.record, record)
	return f.result, f.err
}

type chattoneIdentityFixture struct{ err error }

func (i chattoneIdentityFixture) FilterInput(_ context.Context, p chat.Principal) (chatfilter.Input, error) {
	return chatfilter.Input{Tenant: p.TenantID, Subject: p.SubjectID, Roles: []string{"moderator"}}, i.err
}

type chattoneRoomFixture struct {
	err  error
	room chat.Conversation
	reqs []chat.GetConversationRequest
}

func (r *chattoneRoomFixture) GetConversation(_ context.Context, req chat.GetConversationRequest) (chat.Conversation, error) {
	r.reqs = append(r.reqs, req)
	return r.room, r.err
}

func TestTodo_CHATTONE_004_ContentPolicy(t *testing.T) {
	id := chatrewrite.Identity{Tenant: "tenant", Person: "person", Conversation: "room"}
	room := chat.Conversation{ID: "room", TenantID: "tenant", Kind: chat.Group}
	blocked := chatfilter.Result{Action: "block", Hits: []chatfilter.Hit{{RuleName: "slurs", Action: "block"}}}
	masked := chatfilter.Result{Action: "mask", Masked: "x", Hits: []chatfilter.Hit{{RuleName: "words", Action: "mask"}}}
	for _, tc := range []struct {
		name     string
		result   chatfilter.Result
		filter   error
		identity error
		roomErr  error
		accepted bool
		wantErr  bool
	}{
		{"no rule", chatfilter.Result{}, nil, nil, nil, true, false},
		{"masked is not blocked", masked, nil, nil, nil, true, false},
		{"blocked by a hard filter", blocked, nil, nil, nil, false, false},
		{"filter unavailable is not a pass", chatfilter.Result{}, chatfilter.ErrUnavailable, nil, nil, false, true},
		{"unknown writer is not a pass", chatfilter.Result{}, nil, chatfilter.ErrDenied, nil, false, true},
		{"unreadable room is not a pass", chatfilter.Result{}, nil, nil, chat.ErrPermissionDenied, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter, rooms := &chattoneFilterFixture{result: tc.result, err: tc.filter}, &chattoneRoomFixture{room: room, err: tc.roomErr}
			ok, err := ChattoneContentPolicy{Filters: filter, Identity: chattoneIdentityFixture{tc.identity}, Conversations: rooms}.Accept(context.Background(), id, "Please keep 42.")
			if ok != tc.accepted || (err != nil) != tc.wantErr {
				t.Fatalf("accepted=%v err=%v", ok, err)
			}
			if tc.roomErr != nil || tc.identity != nil {
				return
			}
			// The filter sees the writer's own identity and exemptions in this
			// conversation, and the check never records a hit for a draft that was not sent.
			if len(filter.inputs) != 1 || filter.record[0] || filter.inputs[0].Tenant != "tenant" || filter.inputs[0].Subject != "person" || filter.inputs[0].Channel != "room" ||
				filter.inputs[0].Body != "Please keep 42." || !filter.inputs[0].Direct || len(filter.inputs[0].Roles) != 1 {
				t.Fatalf("filter input %+v record %v", filter.inputs, filter.record)
			}
		})
	}
	if ok, err := (ChattoneContentPolicy{}).Accept(context.Background(), id, "x"); ok || !errors.Is(err, chatrewrite.ErrUnavailable) {
		t.Fatal("an unconfigured policy accepted a body", ok, err)
	}
}

func chattonePrompt(data string) chatrewrite.Prompt {
	return chatrewrite.Prompt{Identity: chatrewrite.Identity{Tenant: "tenant", Person: "person", Conversation: "room"}, TaskProfile: chatrewrite.TaskProfileID, Instruction: "Rewrite tone only.", Data: "<untrusted_data>\n" + data + "\n</untrusted_data>"}
}

func TestTodo_CHATTONE_004_OutboundVerifier(t *testing.T) {
	v, err := NewChattoneOutboundVerifier()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, ok := range []string{
		`{"draft":"The deploy broke prod again, fix it now","register_context":["short and direct"]}`,
		`{"draft":"Order 1234 5678 9012 3456 shipped on 2026-10-01 to +1 555 0100"}`, // not a Luhn number
		`{"draft":"Call me on 555-0100 about 12-34-5678"}`,
	} {
		if err := v.Verify(ctx, chattonePrompt(ok)); err != nil {
			t.Fatalf("ordinary text refused: %q: %v", ok, err)
		}
	}
	for name, data := range map[string]string{
		"national id":    `{"draft":"my ssn is 123-45-6789"}`,
		"card number":    `{"draft":"charge 4111 1111 1111 1111 please"}`,
		"card hyphens":   `{"draft":"charge 4111-1111-1111-1111 please"}`,
		"private key":    `{"draft":"-----BEGIN RSA PRIVATE KEY----- abc"}`,
		"cloud key":      `{"draft":"use AKIAABCDEFGHIJKLMNOP now"}`,
		"api token":      `{"draft":"token sk-abcdefghijklmnopqrstuv"}`,
		"password":       `{"draft":"the password is hunter22 ok"}`,
		"in the context": `{"draft":"fine","register_context":["pw: password=hunter22"]}`,
	} {
		if err := v.Verify(ctx, chattonePrompt(data)); !errors.Is(err, chatrewrite.ErrPolicy) {
			t.Fatalf("%s: sensitive data allowed to leave: %v", name, err)
		}
	}
	// Sensitive text in the instruction (an administrator's house style) is held to the same rule.
	instruction := chattonePrompt(`{"draft":"fine"}`)
	instruction.Instruction = "Write like this: the password is hunter22"
	if err := v.Verify(ctx, instruction); !errors.Is(err, chatrewrite.ErrPolicy) {
		t.Fatalf("sensitive instruction allowed: %v", err)
	}
	for name, mutate := range map[string]func(*chatrewrite.Prompt){
		"no identity":      func(p *chatrewrite.Prompt) { p.Identity = chatrewrite.Identity{} },
		"other task":       func(p *chatrewrite.Prompt) { p.TaskProfile = "other" },
		"empty":            func(p *chatrewrite.Prompt) { p.Instruction = " " },
		"undelimited data": func(p *chatrewrite.Prompt) { p.Data = `{"draft":"x"}` },
		"oversize data": func(p *chatrewrite.Prompt) {
			p.Data = "<untrusted_data>" + strings.Repeat("a", chattoneMaxDataBytes) + "</untrusted_data>"
		},
		"invalid utf-8": func(p *chatrewrite.Prompt) { p.Data = "<untrusted_data>\xff</untrusted_data>" },
	} {
		p := chattonePrompt(`{"draft":"x"}`)
		mutate(&p)
		if err := v.Verify(ctx, p); !errors.Is(err, chatrewrite.ErrInvalid) {
			t.Fatalf("%s: malformed prompt allowed: %v", name, err)
		}
	}
	if err := (*ChattoneOutboundVerifier)(nil).Verify(ctx, chattonePrompt(`{}`)); !errors.Is(err, chatrewrite.ErrUnavailable) {
		t.Fatal("nil verifier allowed a prompt")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := v.Verify(cancelled, chattonePrompt(`{"draft":"x"}`)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled context verified", err)
	}
}

func TestTodo_CHATTONE_004_MeaningGuard(t *testing.T) {
	guard := ChattoneMeaningGuard{}
	for _, tc := range []struct {
		name, original, rewrite string
		preserved               bool
	}{
		{"insult removed, facts kept", "You idiot, the deploy broke prod again, fix it now", "The deploy broke production again. Please fix it now.", true},
		{"a no stays a no", "No, I cannot approve this budget", "No, I cannot approve this budget at this time", true},
		{"a no is not turned into a yes", "No, I will not approve the budget this quarter", "I will approve the budget this quarter", false},
		{"a refusal is not softened away", "I won't sign off on the release schedule", "I will sign off on the release schedule", false},
		{"contractions count", "We don't ship the report on Friday", "We do not ship the report on Friday", true},
		{"typographic apostrophe", "We don’t ship the report on Friday", "We do not ship the report on Friday", true},
		{"apology added", "The report is late because the data was wrong", "I am sorry the report is late because the data was wrong", false},
		{"promise added", "The report is late because the data was wrong", "The report is late because the data was wrong, I promise it ships tomorrow", false},
		{"agreement added", "The schedule needs more review before launch", "I agree the schedule needs more review before launch", false},
		{"commitment already there", "I will send the schedule review before launch", "I will send the schedule review before the launch", true},
		{"a question stays a question", "Why is the report late again?", "Could you tell me why the report is late again?", true},
		{"a question is not made a statement", "Why is the report late again?", "The report is late again.", false},
		{"a statement is not made a question", "The report is late again.", "Why is the report late again?", false},
		{"courtesy is tone, not content", "Fix the broken deploy pipeline now", "Please fix the broken deploy pipeline now, thanks", true},
		{"a different message", "The quarterly budget review moved to Thursday afternoon", "Lunch tomorrow sounds fantastic everyone", false},
		{"short drafts are not judged on topic", "Fix it now", "Please fix it now", true},
		{"german negation", "Nein, das Budget wird nicht freigegeben", "Das Budget wird freigegeben", false},
		{"german kept", "Nein, das Budget wird nicht freigegeben", "Nein, das Budget wird derzeit nicht freigegeben", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer, err := guard.Check(context.Background(), chatrewrite.MeaningQuestion{Identity: chatrewrite.Identity{Tenant: "t", Person: "p", Conversation: "c"}, Original: tc.original, Rewrite: tc.rewrite, Question: chatrewrite.PreservationQuestion})
			if err != nil || answer.Preserved != tc.preserved {
				t.Fatalf("%q => %q: got %+v err %v, want preserved=%v", tc.original, tc.rewrite, answer, err, tc.preserved)
			}
			// The service accepts only full confidence: a pass is exactly 1, a fail exactly 0.
			if (answer.Preserved && answer.Confidence != 1) || (!answer.Preserved && answer.Confidence != 0) {
				t.Fatalf("confidence %v", answer.Confidence)
			}
		})
	}
	if _, err := guard.Check(context.Background(), chatrewrite.MeaningQuestion{Original: "a", Rewrite: "b", Question: "other"}); !errors.Is(err, chatrewrite.ErrInvalid) {
		t.Fatal("a different question was answered", err)
	}
	if _, err := guard.Check(context.Background(), chatrewrite.MeaningQuestion{Original: " ", Rewrite: "b", Question: chatrewrite.PreservationQuestion}); !errors.Is(err, chatrewrite.ErrInvalid) {
		t.Fatal("an empty draft was judged", err)
	}
}
