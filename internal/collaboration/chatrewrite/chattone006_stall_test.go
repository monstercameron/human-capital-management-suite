package chatrewrite

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stallOnceModel answers nothing on its first call (it waits for the caller's
// bound to pass) and the draft unchanged on its second.
type stallOnceModel struct {
	calls  int
	answer string
}

func (m *stallOnceModel) Rewrite(ctx context.Context, p Prompt) (string, error) {
	checked, err := m.RewriteChecked(ctx, p)
	return checked.Text, err
}

func (m *stallOnceModel) RewriteChecked(ctx context.Context, _ Prompt) (Checked, error) {
	m.calls++
	if m.calls == 1 {
		<-ctx.Done()
		return Checked{}, ctx.Err()
	}
	return Checked{Text: m.answer, MeaningPreserved: true}, nil
}

// TestTodo_CHATTONE_006: a stalled model call holds the rewrite for the whole
// wait and ends in the general "unavailable". The served rewrite gives one
// call a bound near the slowest ordinary answer and asks once more when it
// passes: with a provider that stalls once, the writer gets one answered
// rewrite, the model is called twice, and the ledger carries one line for the
// one answered call.
func TestTodo_CHATTONE_006(t *testing.T) {
	s, _, _, _, ledger := fixtureService()
	model := &stallOnceModel{answer: "Please keep this draft"}
	s.Model = model
	s.CallTimeout = 150 * time.Millisecond
	// A rewrite holds two reservations (the call and the meaning check). The
	// ceiling leaves no room for the retry: if the second call took its own
	// reservation, the rewrite would fail limited instead of answering.
	ledger.Limit = 2
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	start := time.Now()
	output, err := s.Rewrite(ctx, fixtureRequest("Please keep this draft"))
	elapsed := time.Since(start)
	if err != nil || output == "" {
		t.Fatalf("a rewrite whose first call stalled: %q %v", output, err)
	}
	if model.calls != 2 {
		t.Fatalf("the model was called %d times, want the stalled call asked once more", model.calls)
	}
	lines := 0
	for _, line := range ledger.Lines() {
		if line.Operation == "rewrite" {
			lines++
			if !line.Succeeded {
				t.Fatalf("the answered call was recorded a failure: %+v", line)
			}
		}
	}
	if lines != 1 {
		t.Fatalf("the ledger carries %d rewrite lines, want one for the answered call", lines)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("a stalled call held the rewrite %s", elapsed)
	}
	if def := (&Service{}).callTimeout(); def != CallTimeout || CallTimeout != 10*time.Second {
		t.Fatalf("one call is bound %s by default, want the shared ten seconds", def)
	}
}

// TestTodo_CHATTONE_006_NoAnswerTwice: when both calls stall, the rewrite
// fails stalled (so the status line can name the retry), not silently
// unavailable, after exactly two bounded calls and one ledger line.
func TestTodo_CHATTONE_006_NoAnswerTwice(t *testing.T) {
	s, _, _, _, ledger := fixtureService()
	model := &stallAlwaysModel{}
	s.Model = model
	s.CallTimeout = 150 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	start := time.Now()
	_, err := s.Rewrite(ctx, fixtureRequest("Please keep this draft"))
	elapsed := time.Since(start)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("a rewrite stalled twice: %v", err)
	}
	if model.calls != 2 {
		t.Fatalf("the model was called %d times, want exactly two bounded calls", model.calls)
	}
	if lines := len(ledger.Lines()); lines != 1 || ledger.Lines()[0].Succeeded {
		t.Fatalf("the ledger carries %+v, want one failed rewrite line", ledger.Lines())
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("two stalled calls held the rewrite %s", elapsed)
	}
}

// stallAlwaysModel never answers; it waits out every call bound.
type stallAlwaysModel struct {
	calls int
}

func (m *stallAlwaysModel) Rewrite(ctx context.Context, p Prompt) (string, error) {
	checked, err := m.RewriteChecked(ctx, p)
	return checked.Text, err
}

func (m *stallAlwaysModel) RewriteChecked(ctx context.Context, _ Prompt) (Checked, error) {
	m.calls++
	<-ctx.Done()
	return Checked{}, ctx.Err()
}

// TestTodo_CHATTONE_006_RetryRule: the one rule the served rewrite and the
// qualification tool share. A stalled attempt is asked once more; any other
// error, and a parent that will not continue, return at once without another
// call; two stalls are the stalled error.
func TestTodo_CHATTONE_006_RetryRule(t *testing.T) {
	stall := func(ctx context.Context) (Checked, error) {
		<-ctx.Done()
		return Checked{}, ctx.Err()
	}
	answer := Checked{Text: "done", MeaningPreserved: true}
	calls := 0
	got, err := RetryStalledCall(context.Background(), 50*time.Millisecond, func(ctx context.Context) (Checked, error) {
		calls++
		if calls == 1 {
			return stall(ctx)
		}
		return answer, nil
	})
	if err != nil || got != answer || calls != 2 {
		t.Fatalf("stall once: %+v %v after %d calls", got, err, calls)
	}
	calls = 0
	if _, err := RetryStalledCall(context.Background(), 20*time.Millisecond, func(ctx context.Context) (Checked, error) {
		calls++
		return stall(ctx)
	}); !errors.Is(err, ErrStalled) || calls != 2 {
		t.Fatalf("stall twice: %v after %d calls", err, calls)
	}
	calls = 0
	boom := errors.New("boom")
	if _, err := RetryStalledCall(context.Background(), time.Second, func(context.Context) (Checked, error) {
		calls++
		return Checked{}, boom
	}); !errors.Is(err, boom) || calls != 1 {
		t.Fatalf("a refused call: %v after %d calls", err, calls)
	}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	if _, err := RetryStalledCall(parent, time.Second, func(ctx context.Context) (Checked, error) {
		calls++
		return stall(ctx)
	}); err == nil || calls != 1 {
		t.Fatalf("a cancelled parent: %v after %d calls", err, calls)
	}
}
