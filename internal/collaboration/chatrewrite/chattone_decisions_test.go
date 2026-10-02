package chatrewrite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestTodo_CHATTONE_001 pins, as executable checks, the decisions of the
// CHATTONE-001 record that bind the writer-requested style controls: opt-in,
// tone only, nothing sent for the writer, and the stated fallbacks when the
// model is unavailable, over budget or the checks fail, and the decisions that
// bind rewording itself: abusive text is never reworded (3), the writer is never
// blocked or lectured (4) and a failed rewrite is never shown, the message
// being delivered as written instead (10). Delivery to readers (5, 6, 7, 8, 9)
// is decided where renderings are selected and produced, in the application
// package (CHATTONE-003 and the rendering producer).
func TestTodo_CHATTONE_001(t *testing.T) {
	t.Run("rewording is not a way to make threats acceptable", func(t *testing.T) {
		r, model, _ := rewordFixture()
		got, err := r.Reword(context.Background(), rewordRequest("I will find you and hurt you if this is late"))
		if err != nil || got.Outcome != RewordHardFilter || got.Text != "" || model.calls != 0 {
			t.Fatalf("an abusive message was offered for rewording: %+v %v calls %d", got, err, model.calls)
		}
	})
	t.Run("the writer is never blocked: every failure is the message as written", func(t *testing.T) {
		for name, arrange := range map[string]func(*Reworder, *fixtureModel){
			"model down":   func(_ *Reworder, m *fixtureModel) { m.err = ErrUnavailable },
			"budget spent": func(r *Reworder, _ *fixtureModel) { r.Service.Ledger = NewMemoryLedger(0) },
			"checks failed": func(r *Reworder, _ *fixtureModel) {
				r.Service.Meaning = &fixtureMeaning{answer: MeaningAnswer{false, 1}}
			},
		} {
			r, model, _ := rewordFixture()
			arrange(r, model)
			got, err := r.Reword(context.Background(), rewordRequest(heatedMessage))
			if err != nil || got.Outcome != RewordFallback || got.Text != "" {
				t.Errorf("%s: %+v %v", name, got, err)
			}
		}
	})
	t.Run("opt in: a workspace is off until it is configured", func(t *testing.T) {
		s, model, _, _, _ := fixtureService()
		s.Registry.RequireConfiguration()
		if _, enabled := s.Registry.Styles("tenant"); enabled || s.Registry.Configured("tenant") {
			t.Fatal("an unconfigured workspace reported the controls on")
		}
		output, err := s.Rewrite(context.Background(), fixtureRequest("Please keep this draft unchanged."))
		if !errors.Is(err, ErrDisabled) || output != "" || model.calls != 0 {
			t.Fatalf("an unconfigured workspace reached the model: %q %v calls %d", output, err, model.calls)
		}
		if err := s.Registry.Configure("tenant", true, DefaultStyles()); err != nil {
			t.Fatal(err)
		}
		if _, enabled := s.Registry.Styles("tenant"); !enabled || !s.Registry.Configured("tenant") {
			t.Fatal("an administrator's choice did not turn the controls on")
		}
		if _, enabled := s.Registry.Styles("other"); enabled {
			t.Fatal("one workspace's setting leaked to another")
		}
		if err := s.Registry.Configure("tenant", false, DefaultStyles()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Rewrite(context.Background(), fixtureRequest("Please keep this draft unchanged.")); !errors.Is(err, ErrDisabled) {
			t.Fatalf("an administrator's off was not honoured: %v", err)
		}
	})
	t.Run("the writer is never sent for: the service only returns a preview", func(t *testing.T) {
		typ := reflect.TypeOf(&Service{})
		if typ.NumMethod() != 1 || typ.Method(0).Name != "Rewrite" {
			t.Fatalf("the rewrite service exposes more than a preview: %d methods", typ.NumMethod())
		}
		s, _, _, _, _ := fixtureService()
		draft := "Please keep this draft unchanged."
		request := fixtureRequest(draft)
		output, err := s.Rewrite(context.Background(), request)
		if err != nil || output == "" || request.Draft != draft {
			t.Fatalf("preview %q %v", output, err)
		}
	})
	t.Run("tone only: every fact survives and nothing is added", func(t *testing.T) {
		s, model, _, _, _ := fixtureService()
		model.transform = func(text string, _ int) string { return text + " I apologise, 99 percent." }
		if output, err := s.Rewrite(context.Background(), fixtureRequest("Keep @Dana and 42 unchanged.")); !errors.Is(err, ErrPreservation) || output != "" {
			t.Fatalf("an added figure was offered: %q %v", output, err)
		}
	})
	t.Run("fallbacks: no rewrite is shown when the model, budget or checks fail", func(t *testing.T) {
		for name, tc := range map[string]struct {
			arrange func(*Service, *fixtureModel, *fixtureOutbound)
			want    error
			calls   int
		}{
			"unavailable": {func(_ *Service, m *fixtureModel, _ *fixtureOutbound) { m.err = ErrUnavailable }, ErrUnavailable, 1},
			"over budget": {func(_ *Service, m *fixtureModel, _ *fixtureOutbound) { m.err = ErrLimit }, ErrLimit, 1},
			"wrapped budget": {func(_ *Service, m *fixtureModel, _ *fixtureOutbound) {
				m.err = errors.Join(errors.New("budget"), ErrLimit)
			}, ErrLimit, 1},
			"sensitive draft": {func(_ *Service, _ *fixtureModel, o *fixtureOutbound) { o.err = ErrPolicy }, ErrPolicy, 0},
			"outbound down":   {func(_ *Service, _ *fixtureModel, o *fixtureOutbound) { o.err = errors.New("down") }, ErrUnavailable, 0},
		} {
			t.Run(name, func(t *testing.T) {
				s, model, _, outbound, _ := fixtureService()
				tc.arrange(s, model, outbound)
				output, err := s.Rewrite(context.Background(), fixtureRequest("Please keep this draft unchanged."))
				if !errors.Is(err, tc.want) || output != "" || model.calls != tc.calls {
					t.Fatalf("%s: %q %v calls %d", name, output, err, model.calls)
				}
			})
		}
	})
}

// TestTodo_CHATTONE_001_Golden pins the default registry: the three controls,
// their order, registers and instructions. Changing one is a decision, not an
// edit.
func TestTodo_CHATTONE_001_Golden(t *testing.T) {
	got := []string{}
	for _, s := range DefaultStyles() {
		got = append(got, strings.Join([]string{s.ID, s.Label, s.Register, s.Instruction}, "|"))
	}
	want := []string{
		"professional|Professional|professional|Use neutral, courteous language; remove heat.",
		"friendly|Friendly|friendly|Use warm, positive language.",
		"concise|Concise|concise|Use shorter, direct language, keeping the same content.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default styles changed:\n%q", got)
	}
	if TaskProfileID != "chat-writing-style-v1" || MaxDraftBytes != 12000 || MaxContextMessages != 6 || MaxContextBytes != 3072 {
		t.Fatal("the task profile or the draft and context bounds changed")
	}
	// A model is evaluated for exactly this instruction text. Changing it is a new
	// version that must be evaluated again; update this line only with that run.
	if got := InstructionDigest(); got != "c15efa7c26d966424cdfa1ceaa805f80e2888216344c44fa78e5a65b512e3624" {
		t.Fatalf("the instruction text changed: digest %s", got)
	}
}

type localMeaning struct{ *fixtureMeaning }

func (localMeaning) Local() bool { return true }

// TestTodo_CHATTONE_001_LocalMeaning: a meaning check that runs inside the
// process takes no place from the person's day and is not put through the
// outbound verifier, but still writes its usage line; the model call it follows
// is what the day counts.
func TestTodo_CHATTONE_001_LocalMeaning(t *testing.T) {
	s, model, _, outbound, _ := fixtureService()
	s.Meaning = localMeaning{&fixtureMeaning{answer: MeaningAnswer{true, 1}}}
	s.Ledger = NewMemoryLedger(2)
	for i := 0; i < 2; i++ {
		if _, err := s.Rewrite(context.Background(), fixtureRequest("Please keep this draft unchanged.")); err != nil {
			t.Fatalf("rewrite %d: %v", i, err)
		}
	}
	if _, err := s.Rewrite(context.Background(), fixtureRequest("Please keep this draft unchanged.")); !errors.Is(err, ErrLimit) || model.calls != 2 {
		t.Fatalf("a spent day reached the model: %v calls %d", err, model.calls)
	}
	// The draft is verified once per request (three requests); the local meaning
	// check added no verification of its own.
	if len(outbound.prompts) != 3 {
		t.Fatalf("outbound verifications %d", len(outbound.prompts))
	}
	lines := s.Ledger.(*MemoryLedger).Lines()
	operations := map[string]int{}
	for _, l := range lines {
		operations[l.Operation]++
	}
	if operations["rewrite"] != 2 || operations["meaning"] != 2 {
		t.Fatalf("usage lines %v", operations)
	}
}
