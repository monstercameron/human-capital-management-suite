package chatfilter

import (
	"context"
	"errors"
	"testing"
	"time"
)

// roleAuthority answers the permission from a table: the subjects that hold
// it for the whole workspace, and the channel each manager holds it for.
type roleAuthority struct {
	workspace map[string]bool
	channel   map[string]string
}

func (r roleAuthority) AuthorizeFilters(_ context.Context, a Actor, c string) error {
	if r.workspace[a.Subject] || c != "" && r.channel[a.Subject] == c {
		return nil
	}
	return ErrDenied
}
func (roleAuthority) CanReadFilterConversation(context.Context, Actor, string) bool { return true }

func oneChannel(id, channel, action string, match ...string) Definition {
	d := custom(id, "words", action, match...)
	d.Channels = []string{channel}
	return d
}

// TestTodo_CHATMOD_003_Security_WorkspaceOwned: a channel's manager can write,
// change and switch their own filter for the channel, and can do none of that
// to a filter a workspace administrator wrote for it.
func TestTodo_CHATMOD_003_Security_WorkspaceOwned(t *testing.T) {
	ctx := t.Context()
	store := &fixtureStore{}
	s := &Service{Store: store, Registry: NewRegistry(), Authority: roleAuthority{workspace: map[string]bool{"admin": true}, channel: map[string]string{"manager": "room"}}}
	admin, manager := Actor{Tenant: "tenant", Subject: "admin"}, Actor{Tenant: "tenant", Subject: "manager"}

	// The administrator's filter for the channel, saved with a forged claim
	// that it is the channel's own.
	theirs := oneChannel("client-names", "room", "block", "quartz")
	theirs.Authority = AuthorityChannel
	if err := s.CreateVersion(ctx, admin, theirs); err != nil {
		t.Fatal(err)
	}
	if got := store.defs[0].Authority; got != AuthorityWorkspace {
		t.Fatalf("an administrator's version is stored as %q, want the workspace's whatever was sent", got)
	}
	if err := s.Enable(ctx, admin, Enablement{RuleID: theirs.ID, Channel: "room", Enabled: true}, false); err != nil {
		t.Fatal(err)
	}
	weaker := theirs
	weaker.Version, weaker.Action, weaker.Match = "1.1.0", "flag", []string{"zzzz"}
	weaker.Authority = AuthorityWorkspace
	if err := s.CreateVersion(ctx, manager, weaker); !errors.Is(err, ErrDenied) {
		t.Fatalf("a manager replaced the administrator's filter with a weaker version: %v", err)
	}
	if err := s.Enable(ctx, manager, Enablement{RuleID: theirs.ID, Channel: "room", Enabled: false}, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("a manager switched the administrator's filter off: %v", err)
	}
	if err := s.Enable(ctx, manager, Enablement{RuleID: theirs.ID, Channel: "room", Enabled: true}, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("a manager put the administrator's filter into a dry run: %v", err)
	}
	if len(store.defs) != 1 || len(store.enabled) != 1 || !store.enabled[0].Enabled || !store.enabled[0].DryRunUntil.IsZero() {
		t.Fatalf("a refused command changed the store: defs=%+v enabled=%+v", store.defs, store.enabled)
	}
	out, err := s.Evaluate(ctx, Input{Tenant: "tenant", Channel: "room", Body: "quartz"}, false)
	if err != nil || out.Action != "block" {
		t.Fatalf("the administrator's filter no longer blocks: %+v %v", out, err)
	}

	// The manager's own filter: theirs to change and to switch.
	own := oneChannel("project-names", "room", "mask", "garnet")
	own.Authority = AuthorityWorkspace
	if err := s.CreateVersion(ctx, manager, own); err != nil {
		t.Fatal(err)
	}
	if got := store.defs[1].Authority; got != AuthorityChannel {
		t.Fatalf("a manager's version is stored as %q, want the channel's whatever was sent", got)
	}
	own.Version, own.Action = "1.1.0", "flag"
	if err := s.CreateVersion(ctx, manager, own); err != nil {
		t.Fatalf("a manager could not change their own filter: %v", err)
	}
	if err := s.Enable(ctx, manager, Enablement{RuleID: own.ID, Channel: "room", Enabled: true}, true); err != nil {
		t.Fatalf("a manager could not switch their own filter: %v", err)
	}
	// Once an administrator saves a version of it, it is the workspace's.
	own.Version = "2.0.0"
	if err := s.CreateVersion(ctx, admin, own); err != nil {
		t.Fatal(err)
	}
	own.Version = "2.1.0"
	if err := s.CreateVersion(ctx, manager, own); !errors.Is(err, ErrDenied) {
		t.Fatalf("a manager changed a filter after an administrator took it over: %v", err)
	}

	// A version stored before the field existed proves nothing about its author.
	legacy := oneChannel("legacy", "room", "block", "opal")
	store.defs = append(store.defs, legacy)
	s.invalidate("tenant")
	legacy.Version = "1.1.0"
	if err := s.CreateVersion(ctx, manager, legacy); !errors.Is(err, ErrDenied) {
		t.Fatalf("a manager changed a filter of unknown authorship: %v", err)
	}
	if err := s.Enable(ctx, manager, Enablement{RuleID: "legacy", Channel: "room", Enabled: false}, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("a manager switched off a filter of unknown authorship: %v", err)
	}
	// The built-in lists keep their per-channel switch in either direction.
	builtin := Builtins()[0].ID
	if err := s.Enable(ctx, manager, Enablement{RuleID: builtin, Channel: "room", Enabled: true}, false); err != nil {
		t.Fatalf("a manager could not switch a built-in list on in their channel: %v", err)
	}
}

// tickClock is a clock that moves on by step every time it is read.
type tickClock struct {
	at   time.Time
	step time.Duration
}

func (c *tickClock) now() time.Time {
	c.at = c.at.Add(c.step)
	return c.at
}

// TestTodo_CHATMOD_003_Security_Deadline: judging one message has a time
// budget. Past it the message is not judged and the caller is told the filters
// are unavailable; it is never passed as clean.
func TestTodo_CHATMOD_003_Security_Deadline(t *testing.T) {
	defs := []Definition{custom("a", "words", "block", "quartz"), custom("b", "words", "mask", "opal"), custom("c", "pattern", "flag", "ACC-[0-9]{4}")}
	in := Input{Tenant: "tenant", Channel: "room", Body: "quartz opal ACC-1234"}

	slow := NewRegistry()
	clock := &tickClock{at: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), step: 40 * time.Millisecond}
	slow.EvaluationBudget, slow.Clock = 100*time.Millisecond, clock.now
	e, err := slow.Compile(defs)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Evaluate(in)
	if !errors.Is(err, ErrDeadline) || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an evaluation past its budget returned %+v, %v; want the deadline as an unavailable filter", out, err)
	}
	if out.Action != "" || len(out.Hits) != 0 || out.Refusal() != nil {
		t.Fatalf("a result was returned beside the deadline: %+v", out)
	}

	// The same rules inside the budget judge the message.
	fast := NewRegistry()
	steady := &tickClock{at: clock.at, step: time.Millisecond}
	fast.EvaluationBudget, fast.Clock = 100*time.Millisecond, steady.now
	e, err = fast.Compile(defs)
	if err != nil {
		t.Fatal(err)
	}
	if out, err = e.Evaluate(in); err != nil || out.Action != "block" || len(out.Hits) != 3 {
		t.Fatalf("inside the budget: %+v %v", out, err)
	}

	// The default budget applies when none is set, and the service passes the
	// refusal through instead of recording a clean message.
	store := &fixtureStore{}
	registry := NewRegistry()
	late := &tickClock{at: clock.at, step: DefaultEvaluationBudget}
	registry.Clock = late.now
	s := &Service{Store: store, Registry: registry, Authority: fixtureAuthority{workspace: true}}
	admin := Actor{Tenant: "tenant", Subject: "admin"}
	for _, d := range defs[:2] {
		if err = s.CreateVersion(t.Context(), admin, d); err != nil {
			t.Fatal(err)
		}
		if err = s.Enable(t.Context(), admin, Enablement{RuleID: d.ID, Enabled: true}, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Evaluate(t.Context(), in, true); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("the service judged a message past the default budget: %v", err)
	}
	if len(store.hits) != 0 {
		t.Fatalf("hits were recorded for a message that was not judged: %+v", store.hits)
	}
}

// resolvingDelivery knows one channel and counts what it is asked to deliver.
type resolvingDelivery struct {
	known     string
	asked     []string
	delivered []Record
}

func (d *resolvingDelivery) ResolveFilterTarget(_ context.Context, _ Actor, target string) error {
	d.asked = append(d.asked, target)
	if target != d.known {
		return ErrUnknownTarget
	}
	return nil
}
func (d *resolvingDelivery) DeliverFilterHit(_ context.Context, r Record) error {
	d.delivered = append(d.delivered, r)
	return nil
}

// TestTodo_CHATMOD_003_NotifyTarget: a "notify" filter is saved only when the
// delivery port can tell the channel it names, and its hits are delivered with
// a digest and never the matched text.
func TestTodo_CHATMOD_003_NotifyTarget(t *testing.T) {
	ctx := t.Context()
	store := &fixtureStore{}
	delivery := &resolvingDelivery{known: "#security"}
	s := &Service{Store: store, Registry: NewRegistry(), Authority: fixtureAuthority{workspace: true}, Delivery: delivery}
	admin := Actor{Tenant: "tenant", Subject: "admin"}

	d := custom("codes", "words", "notify", "quartz")
	d.Target = "#nowhere"
	err := s.CreateVersion(ctx, admin, d)
	if !errors.Is(err, ErrUnknownTarget) || !errors.Is(err, ErrInvalid) || len(store.defs) != 0 {
		t.Fatalf("a filter that tells nobody was saved: %v %+v", err, store.defs)
	}
	d.Target = "#security"
	if err = s.CreateVersion(ctx, admin, d); err != nil {
		t.Fatal(err)
	}
	if len(delivery.asked) != 2 {
		t.Fatalf("the delivery port was asked %v", delivery.asked)
	}
	// An action that tells nobody is not asked about.
	if err = s.CreateVersion(ctx, admin, custom("masked", "words", "mask", "opal")); err != nil || len(delivery.asked) != 2 {
		t.Fatalf("a mask filter asked for a target: %v %v", err, delivery.asked)
	}
	if err = s.Enable(ctx, admin, Enablement{RuleID: d.ID, Enabled: true}, false); err != nil {
		t.Fatal(err)
	}
	out, err := s.Evaluate(ctx, Input{Tenant: "tenant", Channel: "general", Subject: "author", Body: "the quartz account"}, true)
	if err != nil || out.Refusal() != nil || out.Masked != "the quartz account" {
		t.Fatalf("a notify filter changed the message: %+v %v", out, err)
	}
	if len(delivery.delivered) != 1 {
		t.Fatalf("delivered %d hits, want 1", len(delivery.delivered))
	}
	got := delivery.delivered[0]
	if got.Hit.Target != "#security" || got.Channel != "general" || got.Hit.RuleName != d.Name || got.Hit.Digest == "" || got.Hit.Masked != "[removed word]" {
		t.Fatalf("delivered record %+v", got)
	}
	// A dry run records and tells nobody.
	if err = s.Enable(ctx, admin, Enablement{RuleID: d.ID, Enabled: true}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Evaluate(ctx, Input{Tenant: "tenant", Channel: "general", Subject: "author", Body: "quartz"}, true); err != nil || len(delivery.delivered) != 1 {
		t.Fatalf("a dry run delivered: %v %d", err, len(delivery.delivered))
	}
}
