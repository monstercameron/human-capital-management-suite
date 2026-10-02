package chatfilter

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fixtureStore struct {
	defs    []Definition
	enabled []Enablement
	hits    []Record
}

func (s *fixtureStore) Definitions(context.Context, string) ([]Definition, error) { return s.defs, nil }
func (s *fixtureStore) CreateVersion(_ context.Context, _ string, d Definition) error {
	s.defs = append(s.defs, d)
	return nil
}
func (s *fixtureStore) Enablements(context.Context, string) ([]Enablement, error) {
	return s.enabled, nil
}
func (s *fixtureStore) PutEnablement(_ context.Context, _ string, e Enablement) error {
	for i, old := range s.enabled {
		if old.RuleID == e.RuleID && old.Channel == e.Channel {
			s.enabled[i] = e
			return nil
		}
	}
	s.enabled = append(s.enabled, e)
	return nil
}
func (s *fixtureStore) RecordHits(_ context.Context, _ string, h []Record) error {
	s.hits = append(s.hits, h...)
	return nil
}
func (s *fixtureStore) Hits(context.Context, string) ([]Record, error) {
	return append([]Record(nil), s.hits...), nil
}

type fixtureAuthority struct {
	workspace bool
	channel   string
	readable  bool
}

func (f fixtureAuthority) AuthorizeFilters(_ context.Context, a Actor, c string) error {
	if a.Subject != "admin" || (!f.workspace && c != f.channel) {
		return ErrDenied
	}
	return nil
}
func (f fixtureAuthority) CanReadFilterConversation(context.Context, Actor, string) bool {
	return f.readable
}

type fixtureDelivery struct{ delivered int }

func (f *fixtureDelivery) DeliverFilterHit(context.Context, Record) error { f.delivered++; return nil }
func TestTodo_CHATMOD_003_Service(t *testing.T) {
	ctx := t.Context()
	store := &fixtureStore{}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	delivery := &fixtureDelivery{}
	s := &Service{Store: store, Registry: NewRegistry(), Authority: fixtureAuthority{workspace: true, readable: true}, Delivery: delivery, Now: func() time.Time { return now }}
	a := Actor{Tenant: "tenant", Subject: "admin"}
	defs, err := s.List(ctx, a)
	if err != nil || len(defs) != 9 {
		t.Fatalf("builtins: %+v %v", defs, err)
	}
	in := Input{Tenant: "tenant", Channel: "general", Body: "quartz"}
	out, err := s.Evaluate(ctx, in, true)
	if err != nil || len(out.Hits) > 0 {
		t.Fatalf("off by default: %+v %v", out, err)
	}
	d := rule("mask")
	if err = s.CreateVersion(ctx, a, d); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateVersion(ctx, a, d); !errors.Is(err, ErrConflict) {
		t.Fatal("version reused")
	}
	if err = s.Enable(ctx, a, Enablement{RuleID: d.ID, Enabled: true}, true); err != nil {
		t.Fatal(err)
	}
	out, err = s.Evaluate(ctx, in, true)
	if err != nil || out.Masked != "quartz" || !store.hits[0].Hit.DryRun {
		t.Fatalf("dry run %+v %v", out, err)
	}
	now = now.Add(7 * 24 * time.Hour)
	out, err = s.Evaluate(ctx, in, true)
	if err != nil || out.Masked != "[removed word]" || out.Hits[0].DryRun {
		t.Fatalf("expiry %+v %v", out, err)
	}
	if err = s.Enable(ctx, a, Enablement{RuleID: d.ID, Channel: "general", Enabled: false}, false); !errors.Is(err, ErrDenied) {
		t.Fatal("workspace rule weakened")
	}
	d.Version = "1.1.0"
	d.Match = []string{"topaz"}
	if err = s.CreateVersion(ctx, a, d); err != nil {
		t.Fatal(err)
	}
	out, _ = s.Evaluate(ctx, in, false)
	if len(out.Hits) > 0 {
		t.Fatal("old version still active")
	}
	search, err := s.SearchFilters(ctx, a, "restricted")
	if err != nil || len(search) != 1 || search[0].Version != "1.1.0" {
		t.Fatalf("search %+v %v", search, err)
	}
	try, err := s.Try(ctx, a, d, Input{Body: "topaz"})
	if err != nil || try.Action != "mask" {
		t.Fatalf("try %+v %v", try, err)
	}
	d = rule("notify")
	d.Target = "review-channel"
	if err = s.CreateVersion(ctx, a, d); err != nil {
		t.Fatal(err)
	}
	if err = s.Enable(ctx, a, Enablement{RuleID: d.ID, Enabled: true}, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Evaluate(ctx, in, true); err != nil || delivery.delivered != 1 {
		t.Fatal("notification missing", err)
	}
	selected := rule("block")
	selected.ID = "selected"
	selected.Channels = []string{"general", "team"}
	if err = s.CreateVersion(ctx, a, selected); err != nil {
		t.Fatal("selected scope", err)
	}
	if err = s.Enable(ctx, a, Enablement{RuleID: selected.ID, Enabled: true}, false); err != nil {
		t.Fatal("workspace enablement of selected scope", err)
	}
	out, err = s.Evaluate(ctx, in, false)
	if err != nil || out.Refusal() == nil {
		t.Fatal("selected channel not filtered", err)
	}
	s.Authority = fixtureAuthority{channel: "general"}
	if err = s.Enable(ctx, a, Enablement{RuleID: selected.ID, Channel: "general", Enabled: false}, false); !errors.Is(err, ErrDenied) {
		t.Fatal("channel manager weakened selected workspace scope")
	}
}
func TestChatfilterBaseline_ServiceSecurity(t *testing.T) {
	store := &fixtureStore{hits: []Record{{Tenant: "t", Channel: "private", Subject: "writer", Hit: Hit{RuleName: "Restricted term", Digest: "sha256:secret", Masked: "[removed word]", Span: Span{1, 5}}}}}
	s := &Service{Store: store, Registry: NewRegistry(), Authority: fixtureAuthority{workspace: true}}
	a := Actor{Tenant: "t", Subject: "admin"}
	rows, err := s.ReadHits(t.Context(), a, "")
	if err != nil || len(rows) != 1 || rows[0].Hit.Digest != "" || rows[0].Hit.Masked != "" || rows[0].Subject != "" || rows[0].Hit.Span != (Span{}) {
		t.Fatalf("private text metadata leaked: %+v %v", rows, err)
	}
	if _, err = s.List(t.Context(), Actor{Tenant: "t", Subject: "member"}); !errors.Is(err, ErrDenied) {
		t.Fatal("member managed filters")
	}
	s.Authority = fixtureAuthority{channel: "c"}
	d := rule("block")
	d.Channels = []string{"c"}
	if err = s.CreateVersion(t.Context(), a, d); err != nil {
		t.Fatal(err)
	}
	d.Hard = true
	d.Version = "2.0.0"
	if err = s.CreateVersion(t.Context(), a, d); !errors.Is(err, ErrDenied) {
		t.Fatal("channel admin asserted hard workspace rule")
	}
	s.Authority = fixtureAuthority{workspace: true}
	if err = s.CreateVersion(t.Context(), a, d); err != nil {
		t.Fatal(err)
	}
	s.Authority = fixtureAuthority{channel: "c"}
	if err = s.Enable(t.Context(), a, Enablement{RuleID: d.ID, Channel: "c", Enabled: false}, false); !errors.Is(err, ErrDenied) {
		t.Fatal("channel manager disabled a hard rule")
	}
}

func TestTodo_CHATMOD_002_Enablement(t *testing.T) {
	en := rule("block")
	en.ID = "builtin-en-profanity"
	en.Product = true
	en.Language = "en"
	en.Version = "2.0.0"
	de := en
	de.ID = "builtin-de-profanity"
	de.Language = "de"
	de.Match = []string{"quarz"}
	s := &Service{Store: &fixtureStore{defs: []Definition{en, de}}, Registry: NewRegistry(), Authority: fixtureAuthority{workspace: true}, Delivery: &fixtureDelivery{}}
	a := Actor{Tenant: "t", Subject: "admin"}
	ctx := t.Context()
	for _, action := range []string{"block", "mask", "flag"} {
		if err := s.Enable(ctx, a, Enablement{RuleID: "builtin-en-profanity", Enabled: true, Action: action}, false); err != nil {
			t.Fatal(err)
		}
		out, err := s.Evaluate(ctx, Input{Tenant: "t", Channel: "general", Body: "quartz", Language: "en-US"}, true)
		if err != nil || out.Action != action {
			t.Fatalf("builtin outcome %+v %v", out, err)
		}
	}
	if err := s.Enable(ctx, a, Enablement{RuleID: "builtin-en-profanity", Channel: "general", Enabled: false}, false); err != nil {
		t.Fatal(err)
	}
	out, err := s.Evaluate(ctx, Input{Tenant: "t", Channel: "general", Body: "quartz", Language: "en-US"}, false)
	if err != nil || len(out.Hits) != 0 {
		t.Fatal("channel off override missing", err)
	}
	if err := s.Enable(ctx, a, Enablement{RuleID: "builtin-de-profanity", Channel: "general", Enabled: true}, false); err != nil {
		t.Fatal(err)
	}
	out, err = s.Evaluate(ctx, Input{Tenant: "t", Channel: "general", Body: "QUARZ", Language: "de-DE"}, false)
	if err != nil || out.Action != "block" {
		t.Fatal("channel on override missing", err)
	}
	settings, err := s.ListEnablements(ctx, a)
	if err != nil || len(settings) != 3 {
		t.Fatal("settings", err)
	}
}
