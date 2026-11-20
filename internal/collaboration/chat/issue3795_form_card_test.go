package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func issue3795Card() FormCard {
	return FormCard{
		Title: "Salary adjustment", Addressee: "u2", FormRef: "salary-adjustment", RunRef: "run-1",
		Fields: []FormField{
			{ID: "reason", Label: "Reason", Type: FormFieldText, Required: true, MaxLength: 40},
			{ID: "effective", Label: "Effective date", Type: FormFieldDate, Required: true},
			{ID: "code", Label: "Reason code", Type: FormFieldSelect, Required: true, Options: []FormOption{{Value: "merit", Label: "Merit"}, {Value: "market", Label: "Market"}}},
			{ID: "approver", Label: "Budget approver", Type: FormFieldPerson},
		},
	}
}

func issue3795People(ids ...string) func(string) (bool, error) {
	return func(s string) (bool, error) {
		for _, id := range ids {
			if id == s {
				return true, nil
			}
		}
		return false, nil
	}
}

func TestIssue3795_FormCardBodyRoundTrips(t *testing.T) {
	card := issue3795Card()
	body, err := FormCardBody(card)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ParseFormCard(body)
	if !ok || got.Title != card.Title || got.Addressee != "u2" || len(got.Fields) != 4 || got.Fields[2].Options[1].Value != "market" {
		t.Fatalf("parsed %+v, %v", got, ok)
	}
	if !IsSystemBody(body) {
		t.Error("a form card is not left out of search and agent context")
	}
	if _, ok := ParseFormCard(body + "x"); ok {
		t.Error("a body with trailing text parsed")
	}
	if _, ok := ParseFormCard(strings.Replace(body, `"title"`, `"surprise":1,"title"`, 1)); ok {
		t.Error("a body with an unknown member parsed")
	}
}

func TestIssue3795_FormCardRefusesBadDefinitions(t *testing.T) {
	mutate := func(f func(*FormCard)) FormCard {
		c := issue3795Card()
		f(&c)
		return c
	}
	for name, c := range map[string]FormCard{
		"no title":           mutate(func(c *FormCard) { c.Title = " " }),
		"no addressee":       mutate(func(c *FormCard) { c.Addressee = "" }),
		"no fields":          mutate(func(c *FormCard) { c.Fields = nil }),
		"too many fields":    mutate(func(c *FormCard) { c.Fields = make([]FormField, 13) }),
		"duplicate id":       mutate(func(c *FormCard) { c.Fields[1].ID = "reason" }),
		"upper-case id":      mutate(func(c *FormCard) { c.Fields[1].ID = "Effective" }),
		"unknown type":       mutate(func(c *FormCard) { c.Fields[1].Type = "file" }),
		"select one option":  mutate(func(c *FormCard) { c.Fields[2].Options = c.Fields[2].Options[:1] }),
		"duplicate option":   mutate(func(c *FormCard) { c.Fields[2].Options[1].Value = "merit" }),
		"options on text":    mutate(func(c *FormCard) { c.Fields[0].Options = []FormOption{{Value: "a", Label: "A"}} }),
		"huge text limit":    mutate(func(c *FormCard) { c.Fields[0].MaxLength = 5000 }),
		"marker in label":    mutate(func(c *FormCard) { c.Fields[0].Label = "x" + FormCardMarker }),
		"control in title":   mutate(func(c *FormCard) { c.Title = "a\x00b" }),
		"newline in run ref": mutate(func(c *FormCard) { c.RunRef = "a\nb" }),
	} {
		if _, err := FormCardBody(c); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
}

func TestIssue3795_CheckAnswersAcceptsAndCleans(t *testing.T) {
	got, err := issue3795Card().CheckAnswers(map[string]string{
		"reason": "  Promotion  ", "effective": "2027-02-01", "code": "merit", "approver": "u3",
	}, issue3795People("u3"))
	if err != nil {
		t.Fatal(err)
	}
	if got["reason"] != "Promotion" || got["effective"] != "2027-02-01" || got["code"] != "merit" || got["approver"] != "u3" {
		t.Fatalf("cleaned = %v", got)
	}
	// An optional field left empty is left out, not recorded as an empty string.
	got, err = issue3795Card().CheckAnswers(map[string]string{"reason": "x", "effective": "2027-02-01", "code": "market", "approver": " "}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := got["approver"]; has || len(got) != 3 {
		t.Fatalf("cleaned = %v", got)
	}
}

func TestIssue3795_CheckAnswersNamesEveryRefusedField(t *testing.T) {
	_, err := issue3795Card().CheckAnswers(map[string]string{
		"reason": strings.Repeat("x", 41), "effective": "2027-02-30", "code": "bonus", "approver": "stranger", "extra": "1",
	}, issue3795People("u3"))
	var invalid *FormValidationError
	if !errors.As(err, &invalid) || !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("err = %v, want a validation error that is an invalid argument", err)
	}
	want := map[string]string{"reason": FormErrTooLong, "effective": FormErrBadDate, "code": FormErrBadOption, "approver": FormErrUnknownUser, "extra": FormErrUnknownField}
	for field, code := range want {
		if invalid.Fields[field] != code {
			t.Errorf("%s = %q, want %q (all: %v)", field, invalid.Fields[field], code, invalid.Fields)
		}
	}
	_, err = issue3795Card().CheckAnswers(map[string]string{}, nil)
	if !errors.As(err, &invalid) || invalid.Fields["reason"] != FormErrRequired || invalid.Fields["effective"] != FormErrRequired || invalid.Fields["code"] != FormErrRequired || len(invalid.Fields) != 3 {
		t.Fatalf("missing required fields: %v", err)
	}
	if _, err := issue3795Card().CheckAnswers(map[string]string{"reason": "x", "effective": "2027-02-01", "code": "merit", "approver": "u3"}, func(string) (bool, error) { return false, ErrUnavailable }); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a lookup failure is %v, want it passed up rather than read as a bad answer", err)
	}
}

func issue3795Service(f *fakeStore) *Service {
	s := NewService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: f})
	return s
}

func TestIssue3795_PostFormCardWritesTheCard(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u2", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	s := issue3795Service(f)
	post, err := s.PostFormCard(context.Background(), FormCardPost{TenantID: "t1", Sender: Principal{TenantID: "t1", SubjectID: "u2"}, ConversationID: "c1", Card: func() FormCard { c := issue3795Card(); c.Addressee = "u9"; return c }(), IdempotencyKey: "run-1:step-2"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ParseFormCard(f.sent.Body)
	if !ok || got.Addressee != "u9" || post.ID == "" || f.sent.ConversationID != "c1" {
		t.Fatalf("sent = %+v (%+v, %v)", f.sent, got, ok)
	}
}

func TestIssue3795_PostFormCardRefusesBadRequests_Security(t *testing.T) {
	member := Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u2", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}
	left := member
	left.LeftAt = timePtr(time.Unix(2, 0))
	agent := Principal{TenantID: "t1", SubjectID: "agent-1"}
	good := issue3795Card()
	for name, c := range map[string]struct {
		f    *fakeStore
		post FormCardPost
	}{
		"sender in another tenant": {&fakeStore{conversation: conversation(), membership: member}, FormCardPost{TenantID: "t1", Sender: Principal{TenantID: "t2", SubjectID: "agent-1"}, ConversationID: "c1", Card: good, IdempotencyKey: "k"}},
		"asks itself":              {&fakeStore{conversation: conversation(), membership: member}, FormCardPost{TenantID: "t1", Sender: Principal{TenantID: "t1", SubjectID: "u2"}, ConversationID: "c1", Card: good, IdempotencyKey: "k"}},
		"no key":                   {&fakeStore{conversation: conversation(), membership: member}, FormCardPost{TenantID: "t1", Sender: agent, ConversationID: "c1", Card: good}},
		"bad card":                 {&fakeStore{conversation: conversation(), membership: member}, FormCardPost{TenantID: "t1", Sender: agent, ConversationID: "c1", Card: FormCard{}, IdempotencyKey: "k"}},
		"addressee not in room":    {&fakeStore{conversation: conversation()}, FormCardPost{TenantID: "t1", Sender: agent, ConversationID: "c1", Card: good, IdempotencyKey: "k"}},
		"addressee left the room":  {&fakeStore{conversation: conversation(), membership: left}, FormCardPost{TenantID: "t1", Sender: agent, ConversationID: "c1", Card: good, IdempotencyKey: "k"}},
		"no such conversation":     {&fakeStore{membership: member}, FormCardPost{TenantID: "t1", Sender: agent, ConversationID: "c1", Card: good, IdempotencyKey: "k"}},
	} {
		if _, err := issue3795Service(c.f).PostFormCard(context.Background(), c.post); err == nil || c.f.mutations != 0 || c.f.sent.ID != "" {
			t.Errorf("%s: err %v, mutations %d, sent %q", name, err, c.f.mutations, c.f.sent.ID)
		}
	}
}

// A person cannot type a form card into a conversation, or edit one in.
func TestIssue3795_HandTypedFormCardIsRefused_Security(t *testing.T) {
	body, err := FormCardBody(issue3795Card())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	s := issue3795Service(f)
	_, err = s.SendPost(context.Background(), SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "look: " + body, IdempotencyKey: "k"})
	if !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("send = %v, mutations %d", err, f.mutations)
	}
}
