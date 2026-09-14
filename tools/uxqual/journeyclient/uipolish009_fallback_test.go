package journeyclient

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
)

func TestTodo_UIPOLISH_009_FallbackSuppressesOverlappingMutations(t *testing.T) {
	for _, key := range []string{
		"journey:propose:worker-1", "journey:create-worker",
		"journey:execute:intent-1", "journey:decision:intent-1",
	} {
		t.Run(key, func(t *testing.T) {
			var queued []func()
			app := &App{Async: func(work func()) { queued = append(queued, work) }}
			spec := taskmux.Spec{Key: key, Priority: taskmux.Interactive, Duplicate: taskmux.KeepExisting}
			calls := 0
			work := func(context.Context) { calls++ }
			app.runTask(context.Background(), spec, work)
			app.runTask(context.Background(), spec, work)
			if len(queued) != 1 {
				t.Fatalf("overlapping mutation queued %d RPC units, want one", len(queued))
			}
			queued[0]()
			if calls != 1 {
				t.Fatalf("mutation ran %d times, want one", calls)
			}
			app.runTask(context.Background(), spec, work)
			if len(queued) != 2 {
				t.Fatal("completed mutation did not release the retry fence")
			}
			queued[1]()
			if calls != 2 {
				t.Fatalf("retry ran %d total times, want two", calls)
			}
		})
	}
}

func TestTodo_UIPOLISH_009_FallbackKeepsIndependentKeys(t *testing.T) {
	var queued []func()
	app := &App{Async: func(work func()) { queued = append(queued, work) }}
	for _, key := range []string{"journey:execute:first", "journey:execute:second"} {
		app.runTask(context.Background(), taskmux.Spec{Key: key, Duplicate: taskmux.KeepExisting}, func(context.Context) {})
	}
	if len(queued) != 2 {
		t.Fatalf("independent actions queued %d RPC units, want two", len(queued))
	}
}
