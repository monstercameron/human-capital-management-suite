package chatgate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fixtureAuthority struct{ deny map[string]bool }

func (a *fixtureAuthority) Check(_ context.Context, p Actor, _ Scope, action string) error {
	if a.deny[action] || ((action == "admin" || action == "export" || action == "install") && p.Person != "admin") {
		return ErrDenied
	}
	return nil
}
func (a *fixtureAuthority) Policy(context.Context, Scope) (Policy, error) {
	return Policy{Ceiling: "INTERNAL"}, nil
}
func (a *fixtureAuthority) Facts(context.Context, Actor, Scope) (map[string]string, error) {
	return map[string]string{"team": "Payroll"}, nil
}
func (a *fixtureAuthority) Reference(_ context.Context, _ Actor, _ Scope, _ Field, v json.RawMessage) error {
	if string(v) == `"forged"` {
		return ErrDenied
	}
	return nil
}
func fixture(t *testing.T, mode string) (*Service, Command, Definition) {
	t.Helper()
	s := &Service{Repository: NewMemoryRepository(), Authority: &fixtureAuthority{deny: map[string]bool{}}, Registry: NewRegistry(), Clock: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }}
	c := Command{Scope: Scope{"tenant", "room"}, Actor: Actor{"tenant", "admin"}, Key: "define"}
	d := Definition{Mode: mode, Purpose: "Join the payroll discussion", Fields: []Field{{ID: "team", Kind: "single_choice", KindVersion: "1.0.0", Label: "Team", Purpose: "Route your welcome", DataClass: "INTERNAL", Required: true, Options: []string{"Payroll", "Finance"}, Visibility: Visibility{Administrators: true}, RetentionDays: 30}}}
	if e := s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.Key = "publish"
	c.ExpectedRevision = 1
	if _, e := s.Publish(t.Context(), c, Version{1, 0, 0}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision = 2
	return s, c, d
}
func applicant(c Command, key string) Command { c.Actor.Person = "member"; c.Key = key; return c }
func TestTodo_CHATGATE_002(t *testing.T) {
	s, c, d := fixture(t, "automatic")
	g, e := s.Get(t.Context(), c.Actor, c.Scope, true)
	if e != nil || g.Current != "1.0.0" || len(g.Versions) != 1 || g.Versions[0].Digest != ContentDigest(g.Versions[0]) {
		t.Fatalf("published %+v %v", g, e)
	}
	g.Versions[0].Fields[0].Label = "tampered"
	again, _ := s.Get(t.Context(), c.Actor, c.Scope, true)
	if again.Versions[0].Fields[0].Label == "tampered" {
		t.Fatal("mutable returned definition")
	}
	d.Fields[0].Required = false
	c.Key = "next"
	if e = s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	c.Key = "bump"
	if _, e = s.Publish(t.Context(), c, Version{1, 0, 0}); !errors.Is(e, ErrInvalid) && !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.Publish(t.Context(), c, Version{1, 0, 1}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	c.Key = "pause"
	if e = s.Lifecycle(t.Context(), c, "paused"); e != nil {
		t.Fatal(e)
	}
	if e = s.CanJoin(t.Context(), Actor{"tenant", "new"}, c.Scope); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_002_Property(t *testing.T) {
	for n := 0; n < 100; n++ {
		old := Definition{Version: Version{uint32(n + 1), 2, 3}}
		next := old
		next.Fields = []Field{{ID: "added", Required: n%2 == 0}}
		v := RequiredBump(old, next)
		if !old.Version.Less(v) || (n%2 == 0 && v.Major != old.Version.Major+1) || (n%2 != 0 && v.Minor != 3) {
			t.Fatalf("nonmonotone %v", v)
		}
	}
}
func TestTodo_CHATGATE_002_Security(t *testing.T) {
	s, c, d := fixture(t, "review")
	c.Actor.Person = "member"
	c.Key = "bad"
	if e := s.Define(t.Context(), c, d); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if _, e := s.Get(t.Context(), c.Actor, c.Scope, true); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	c.Actor.Tenant = "other"
	if _, e := s.Get(t.Context(), c.Actor, c.Scope, false); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_003(t *testing.T) {
	r := NewRegistry()
	for _, id := range []string{"short_text", "long_text", "single_choice", "multiple_choice", "boolean", "date", "person", "team", "location", "acknowledgement"} {
		k, ok := r.Kind(id, "1.0.0")
		if !ok || !json.Valid([]byte(k.Schema)) {
			t.Fatal(id)
		}
	}
	_, _, d := fixture(t, "automatic")
	if e := r.ValidateDefinition(d, Policy{}); e != nil {
		t.Fatal(e)
	}
	k, _ := r.Kind("long_text", "1.0.0")
	v, _ := json.Marshal(strings.Repeat("界", 501))
	if e := k.Validate(Field{}, v); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	v, _ = json.Marshal(strings.Repeat("界", 500))
	if e := k.Validate(Field{}, v); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_003_Property(t *testing.T) {
	k, _ := NewRegistry().Kind("multiple_choice", "1.0.0")
	for n := 0; n < 100; n++ {
		v, _ := json.Marshal([]string{strings.Repeat("x", n)})
		e := k.Validate(Field{Options: []string{"x"}}, v)
		if (n == 1) != (e == nil) {
			t.Fatalf("n=%d %v", n, e)
		}
	}
}
func TestTodo_CHATGATE_003_Security(t *testing.T) {
	r := NewRegistry()
	_, _, d := fixture(t, "review")
	for _, label := range []string{"Password", "API key", "Access token", "Private key", "Passport number", "Medical history", "Government identifier", "كلمة المرور"} {
		x := d
		x.Fields = append([]Field(nil), d.Fields...)
		x.Fields[0].Label = label
		if e := r.ValidateDefinition(x, Policy{}); !errors.Is(e, ErrDenied) {
			t.Fatal(label, e)
		}
	}
	d.Fields[0].KindVersion = "99.0.0"
	if e := r.ValidateDefinition(d, Policy{}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_003_Accessibility(t *testing.T) {
	r := NewRegistry()
	for _, k := range r.kinds {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if strings.TrimSpace(k.Names[locale]) == "" {
				t.Fatal(k.ID, locale)
			}
		}
	}
}
func TestTodo_CHATGATE_004(t *testing.T) {
	for _, mode := range []string{"automatic", "review", "rule"} {
		t.Run(mode, func(t *testing.T) {
			s, c, d := fixture(t, mode)
			if mode == "rule" {
				d.Rules = []Rule{{When: Expression{Operator: "equals", Fact: "team", Values: []string{"Payroll"}}, Outcome: "admitted", Reason: "Team matches"}}
				c.Key = "rules"
				if e := s.Define(t.Context(), c, d); e != nil {
					t.Fatal(e)
				}
				c.ExpectedRevision++
				c.Key = "rules-publish"
				if _, e := s.Publish(t.Context(), c, Version{2, 0, 0}); e != nil {
					t.Fatal(e)
				}
				c.ExpectedRevision++
			}
			g, _ := s.Get(t.Context(), c.Actor, c.Scope, false)
			ac := applicant(c, "submit")
			sub, e := s.Submit(t.Context(), ac, g.Current, map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
			if e != nil {
				t.Fatal(e)
			}
			want := "admitted"
			if mode == "review" {
				want = "review"
			}
			if sub.Status != want {
				t.Fatalf("%+v", sub)
			}
			if mode == "review" {
				c.Key = "decision"
				c.ExpectedRevision++
				sub, e = s.Review(t.Context(), c, sub.ID, 1, true, "Welcome")
				if e != nil {
					t.Fatal(e)
				}
			}
			if !s.Repository.(*MemoryRepository).Member(c.Scope, "member") {
				t.Fatal("admission missing membership")
			}
			again, e := s.Submit(t.Context(), ac, g.Current, map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
			if e != nil || again.ID == "" {
				t.Fatalf("idempotent replay %v", e)
			}
		})
	}
}
func TestTodo_CHATGATE_004_Security(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	ac := applicant(c, "submit")
	for _, values := range []map[string]json.RawMessage{{}, {"team": json.RawMessage(`"Unknown"`)}, {"team": json.RawMessage(`"Payroll"`), "extra": json.RawMessage(`true`)}} {
		if _, e := s.Submit(t.Context(), ac, "1.0.0", values); e == nil {
			t.Fatal("forged answers accepted")
		}
	}
	if _, e := s.Submit(t.Context(), ac, "0.0.1", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	ac.Actor.Tenant = "other"
	if _, e := s.Submit(t.Context(), ac, "1.0.0", nil); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_004_Fault(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	r := s.Repository.(*MemoryRepository)
	r.FailMembership = ErrUnavailable
	ac := applicant(c, "submit")
	values := map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}
	if _, e := s.Submit(t.Context(), ac, "1.0.0", values); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	list, e := s.ListSubmissions(t.Context(), c.Actor, c.Scope, "")
	if e != nil || len(list) != 0 || r.Member(c.Scope, "member") {
		t.Fatalf("partial decision %+v %v", list, e)
	}
	r.FailMembership = nil
	if _, e = s.Submit(t.Context(), ac, "1.0.0", values); e != nil || !r.Member(c.Scope, "member") {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_004_Property(t *testing.T) {
	for n := 0; n < 30; n++ {
		s, c, _ := fixture(t, "automatic")
		ac := applicant(c, "submit")
		sub, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
		if e != nil {
			t.Fatal(e)
		}
		ac.Key = "withdraw"
		ac.ExpectedRevision++
		if e = s.Withdraw(t.Context(), ac, sub.ID, 1); e != nil {
			t.Fatal(e)
		}
		if s.Repository.(*MemoryRepository).Member(c.Scope, "member") {
			t.Fatal("withdraw retained membership")
		}
	}
}
func TestTodo_CHATGATE_007(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	ac := applicant(c, "submit")
	sub, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
	if e != nil {
		t.Fatal(e)
	}
	r := ReadRequest{Actor: ac.Actor, Scope: c.Scope, Person: "member", Purpose: "My answers"}
	v, e := s.SearchOwnAnswers(t.Context(), r, "Payroll")
	if e != nil || string(v["team"]) != `"Payroll"` {
		t.Fatalf("own search %v %v", v, e)
	}
	ac.Key = "withdraw"
	ac.ExpectedRevision++
	if e = s.Withdraw(t.Context(), ac, sub.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadAnswers(t.Context(), r); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_007_Security(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	ac := applicant(c, "submit")
	_, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
	if e != nil {
		t.Fatal(e)
	}
	r := ReadRequest{Actor: Actor{"tenant", "stranger"}, Scope: c.Scope, Person: "member", Purpose: "Read", Fields: []string{"team"}}
	if _, e = s.ReadAnswers(t.Context(), r); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	r.Actor = c.Actor
	if _, e = s.SearchOwnAnswers(t.Context(), r, "Payroll"); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	r.Scope.Conversation = "another"
	if _, e = s.ReadAnswers(t.Context(), r); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_007_Property(t *testing.T) {
	s, c, d := fixture(t, "automatic")
	ac := applicant(c, "submit")
	if _, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	d.Fields[0].Visibility.Members = true
	c.Key = "visibility"
	if e := s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	c.Key = "publish-new"
	if _, e := s.Publish(t.Context(), c, Version{2, 0, 0}); e != nil {
		t.Fatal(e)
	}
	r := ReadRequest{Actor: Actor{"tenant", "stranger"}, Scope: c.Scope, Person: "member", Purpose: "Read", Fields: []string{"team"}}
	if _, e := s.ReadAnswers(t.Context(), r); !errors.Is(e, ErrDenied) {
		t.Fatal("new version widened historical visibility", e)
	}
}
func TestTodo_CHATGATE_008(t *testing.T) {
	s, c, d := fixture(t, "automatic")
	consumer := Consumer{ID: "welcome", Version: "1.0.0", Kinds: []string{"single_choice"}, Effect: "welcome message", Permission: "install"}
	if e := s.Registry.RegisterConsumer(consumer); e != nil {
		t.Fatal(e)
	}
	d.Fields[0].Visibility.Consumers = []string{"welcome"}
	c.Key = "consumer-form"
	if e := s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	c.Key = "consumer-publish"
	if _, e := s.Publish(t.Context(), c, Version{2, 0, 0}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	c.Key = "install"
	if e := s.Install(t.Context(), c, Installation{ConsumerID: "welcome", Version: "1.0.0", Mapping: map[string]string{"team": "team"}}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	ac := applicant(c, "submit")
	if _, e := s.Submit(t.Context(), ac, "2.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}); e != nil {
		t.Fatal(e)
	}
	v, e := s.ReadAnswers(t.Context(), ReadRequest{Actor: Actor{"tenant", "agent"}, Scope: c.Scope, Person: "member", Consumer: "welcome", ConsumerVersion: "1.0.0", Purpose: "Welcome", Fields: []string{"team"}})
	if e != nil || string(v["team"]) != `"Payroll"` {
		t.Fatalf("consumer %v %v", v, e)
	}
}
func TestTodo_CHATGATE_008_Contract(t *testing.T) {
	if APIVersion != "hcmnext.chat.v1.ChannelGateService" {
		t.Fatal(APIVersion)
	}
	d := Definition{Extensions: map[string]json.RawMessage{"future": json.RawMessage(`{"enabled":true}`)}}
	b, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	var back Definition
	if e = json.Unmarshal(b, &back); e != nil || string(back.Extensions["future"]) != `{"enabled":true}` {
		t.Fatal(e, string(b))
	}
}
func TestTodo_CHATGATE_008_Security(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	c.Key = "unknown"
	if e := s.Install(t.Context(), c, Installation{ConsumerID: "unknown", Version: "1.0.0", Mapping: map[string]string{"team": "team"}}); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := s.Registry.RegisterConsumer(Consumer{ID: "x", Version: "bad", Effect: "x", Permission: "install", Fields: []string{"team"}}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_008_Property(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	for n := 0; n < 30; n++ {
		ac := applicant(c, "replay")
		sub, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
		if e != nil || sub.ID == "" {
			t.Fatal(e)
		}
	}
	list, e := s.ListSubmissions(t.Context(), c.Actor, c.Scope, "")
	if e != nil || len(list) != 1 {
		t.Fatalf("replay multiplied submission %v %v", list, e)
	}
}
