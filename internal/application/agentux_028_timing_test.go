package application

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// agentUX028Line captures the records written to the default logger.
type agentUX028Line struct {
	message string
	attrs   map[string]slog.Value
	order   []string
}

type agentUX028Capture struct{ lines []agentUX028Line }

func (*agentUX028Capture) Enabled(context.Context, slog.Level) bool { return true }
func (c *agentUX028Capture) Handle(_ context.Context, record slog.Record) error {
	line := agentUX028Line{message: record.Message, attrs: map[string]slog.Value{}}
	record.Attrs(func(attr slog.Attr) bool {
		line.attrs[attr.Key] = attr.Value
		line.order = append(line.order, attr.Key)
		return true
	})
	c.lines = append(c.lines, line)
	return nil
}
func (c *agentUX028Capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *agentUX028Capture) WithGroup(string) slog.Handler      { return c }

func (c *agentUX028Capture) timing() []agentUX028Line {
	var out []agentUX028Line
	for _, line := range c.lines {
		if line.message == "hcmnext.persona_run_timing" {
			out = append(out, line)
		}
	}
	return out
}

var agentUX028Key = regexp.MustCompile(`^(run_id|total_ms|failed|(stage|event)_[a-z0-9_]+_(count|ms))$`)

// One log line per run reports the stage durations and the counts of authority
// resolutions, fence steps and store calls, and holds nothing of what was asked
// or answered. With a model that answers at once the run is done well inside
// the limit.
func TestTodo_AGENTUX_028(t *testing.T) {
	capture := &agentUX028Capture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })

	timedCtx, _, run, elapsed, err, model, outputs := agentUXSpeedInstantRun(t)
	// Work the executor's fixtures do not do, measured the way the served ports
	// measure it: two authority resolutions, one fence step, three store calls.
	for name, times := range map[string]int{"authority.boundary_verify": 2, "fence.model": 1, "store.thread_context": 3} {
		for i := 0; i < times; i++ {
			agentUXSpeedEvent(timedCtx, name)()
		}
	}
	agentUXSpeedEmit(timedCtx, err != nil)
	if err != nil || run.State != runstate.StateCompleted || model.calls != 2 || outputs != 1 {
		t.Fatalf("instant run = %s, %d model calls, %d outputs, %v", run.State, model.calls, outputs, err)
	}
	if elapsed >= 10*time.Second {
		t.Fatalf("a run with an instant model took %s; the limit is ten seconds and the aim three", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Logf("a run with an instant model took %s, over the three second aim", elapsed)
	}

	lines := capture.timing()
	if len(lines) != 1 {
		t.Fatalf("%d timing lines for one run, want one", len(lines))
	}
	line := lines[0]
	if line.attrs["run_id"].String() != run.ID || line.attrs["failed"].Kind() != slog.KindBool || line.attrs["failed"].Bool() || line.attrs["total_ms"].Kind() != slog.KindInt64 {
		t.Fatalf("the line does not name the run, its outcome and its total: %v", line.attrs)
	}
	// Every stage the run went through is there with a count and a duration.
	for _, stage := range []string{"claim", "build_model_work", "model_execute", "tool_execute", "checkpoint_model_result", "validate_output", "checkpoint_validation", "delivery", "checkpoint_delivery"} {
		count, hasCount := line.attrs["stage_"+stage+"_count"]
		duration, hasDuration := line.attrs["stage_"+stage+"_ms"]
		if !hasCount || !hasDuration || count.Int64() < 1 || duration.Kind() != slog.KindInt64 || duration.Int64() < 0 {
			t.Fatalf("stage %s is not reported with its count and duration: %v", stage, line.order)
		}
	}
	if got := line.attrs["stage_model_execute_count"].Int64(); got != 2 {
		t.Fatalf("the two model turns are counted as %d", got)
	}
	// The counts of authority resolutions, fence steps and store calls.
	for key, want := range map[string]int64{"event_authority_boundary_verify_count": 2, "event_fence_model_count": 1, "event_store_thread_context_count": 3} {
		if got, ok := line.attrs[key]; !ok || got.Int64() != want {
			t.Fatalf("%s = %v, want %d: %v", key, got, want, line.order)
		}
	}
	// No content: a closed set of keys, and every value a number, a boolean or
	// the run's own identifier. Nothing of the question, the document or the
	// answer the fixtures used can appear.
	for _, key := range line.order {
		if !agentUX028Key.MatchString(key) {
			t.Fatalf("the timing line carries an unexpected field %q", key)
		}
		value := line.attrs[key]
		switch key {
		case "run_id":
		case "failed":
			if value.Kind() != slog.KindBool {
				t.Fatalf("%s is not a boolean", key)
			}
		default:
			if value.Kind() != slog.KindInt64 {
				t.Fatalf("%s is %s, not a number: %v", key, value.Kind(), value)
			}
		}
	}
	rendered := ""
	for _, key := range line.order {
		rendered += key + "=" + line.attrs[key].String() + " "
	}
	for _, content := range []string{"Explain the policy", "I will search", "A grounded response", "Leave policy", "leave", "alice", "room-a", "thread-a", "post-a"} {
		if strings.Contains(rendered, content) {
			t.Fatalf("the timing line holds %q: %s", content, rendered)
		}
	}

	// A failed run is reported once too, as failed.
	capture.lines = nil
	failedCtx, _ := withAgentUXRunTiming(context.Background())
	agentUXSpeedStage(failedCtx, "claim")()
	agentUXSpeedEmit(failedCtx, true)
	if lines = capture.timing(); len(lines) != 1 || !lines[0].attrs["failed"].Bool() || lines[0].attrs["stage_claim_count"].Int64() != 1 {
		t.Fatalf("a failed run's line = %+v", lines)
	}
	// A context that measures nothing writes nothing.
	capture.lines = nil
	agentUXSpeedEmit(context.Background(), false)
	if len(capture.timing()) != 0 {
		t.Fatal("a run that was not timed wrote a timing line")
	}
	// The names the served ports measure under are the three families the line
	// reports counts for.
	families := map[string]int{}
	for _, name := range []string{"authority.boundary_verify", "authority.verify", "fence.context", "fence.model", "fence.output", "fence.delivery", "store.admission.get", "store.thread_context", "store.tool_journal"} {
		families[name[:strings.Index(name, ".")]]++
		key := "event_" + strings.NewReplacer(".", "_", "-", "_").Replace(name) + "_count"
		if !agentUX028Key.MatchString(key) {
			t.Fatalf("event %s would be reported under %q", name, key)
		}
	}
	if len(families) != 3 {
		t.Fatalf("event families = %v, want authority, fence and store", families)
	}
}
