package runtime_test

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func wfd006Write(name, value string) runtime.VariableWrite {
	return runtime.VariableWrite{
		VariableName: name,
		SchemaRef:    "schema.test.wfd006/v1",
		Value:        json.RawMessage(value),
		WriterNodeID: "write_" + name,
		WriterRef:    "transform-execution:1",
		Reason:       "seeded test write",
		CausationID:  "cause:wfd006",
	}
}

// wfd006OracleField is the test's own JSON path reader: it decodes raw
// independently of the implementation and walks dot-separated object fields.
func wfd006OracleField(t *testing.T, raw string, path string) (string, bool) {
	t.Helper()
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("oracle decode %q: %v", raw, err)
	}
	current := decoded
	for _, segment := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = obj[segment]
		if !ok {
			return "", false
		}
	}
	rendered, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("oracle marshal: %v", err)
	}
	return string(rendered), true
}

// TestTodo_WF_DATA_006_Property proves an attempt-pinned variable view never
// observes a revision written after its attempt began, no matter how the
// writes interleave across variables.
func TestTodo_WF_DATA_006_Property(t *testing.T) {
	for seed := uint64(1); seed <= 64; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
			variables := []string{"alpha", "beta", "gamma"}
			log, err := runtime.NewVariableRevisionLog(uuid.New(), uuid.New(), nil)
			if err != nil {
				t.Fatalf("empty log: %v", err)
			}
			type recorded struct {
				name  string
				head  int64
				value string
			}
			var history []recorded
			at := fixedInstant
			appendWrite := func() {
				name := variables[rng.IntN(len(variables))]
				at = at.Add(time.Second)
				value := fmt.Sprintf(`{"n":%d,"s":"v-%d"}`, len(history)+1, len(history)+1)
				rev, err := log.Append(wfd006Write(name, value), at)
				if err != nil {
					t.Fatalf("append: %v", err)
				}
				history = append(history, recorded{name: name, head: rev.Revision, value: value})
			}

			before := 1 + rng.IntN(8)
			for range before {
				appendWrite()
			}
			pinned := log.Head()
			attempt := log.PinAttempt()
			after := 1 + rng.IntN(6)
			for range after {
				appendWrite()
			}

			if attempt.Head() != pinned {
				t.Fatalf("pinned head = %d, want %d: later writes moved the attempt boundary", attempt.Head(), pinned)
			}
			if log.Head() != pinned+int64(after) {
				t.Fatalf("log head = %d, want %d", log.Head(), pinned+int64(after))
			}
			for _, name := range variables {
				for _, path := range []string{"n", "s"} {
					got, ok := attempt.Variable(name, path)
					// The oracle replays the recorded history: the value in
					// force is the last write at or before the pin.
					want := ""
					found := false
					for _, w := range history {
						if w.name == name && w.head <= pinned {
							want, found = w.value, true
						}
					}
					if !found {
						if ok {
							t.Fatalf("seed %d: %s.%s reads %q, but %s was never written before the pin",
								seed, name, path, got, name)
						}
						continue
					}
					wantField, ok := wfd006OracleField(t, want, path)
					if !ok {
						t.Fatalf("seed %d: oracle cannot read %s.%s from %s", seed, name, path, want)
					}
					if !ok || got != wantField {
						t.Fatalf("seed %d: %s.%s = %q, want the revision pinned at head %d (%q)",
							seed, name, path, got, pinned, wantField)
					}
				}
			}
			if _, ok := attempt.Variable("alpha", "missing"); ok {
				t.Fatal("a missing field must not resolve")
			}
			if _, ok := attempt.Variable("never_written", "n"); ok {
				t.Fatal("an unwritten variable must not resolve")
			}
		})
	}
}

// TestTodo_WF_DATA_006_Recovery proves a resumed attempt — one rebuilt from
// the stored revision rows — reads the same revisions its first incarnation
// pinned, because the pin derives from the durable history alone.
func TestTodo_WF_DATA_006_Recovery(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	log, err := runtime.NewVariableRevisionLog(tenant, instance, nil)
	if err != nil {
		t.Fatalf("empty log: %v", err)
	}
	writes := []runtime.VariableWrite{
		wfd006Write("proposal", `{"grade":"L5","band":{"min":100}}`),
		wfd006Write("approval", `{"approved":false}`),
		wfd006Write("proposal", `{"grade":"L6","band":{"min":120}}`),
	}
	for i, w := range writes {
		if _, err := log.Append(w, fixedInstant.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	first := log.PinAttempt()
	before := map[string]string{}
	for _, tc := range [][2]string{{"proposal", "grade"}, {"proposal", "band.min"}, {"approval", "approved"}} {
		raw, ok := first.Variable(tc[0], tc[1])
		if !ok {
			t.Fatalf("first attempt cannot read %s.%s", tc[0], tc[1])
		}
		before[tc[0]+"."+tc[1]] = raw
	}

	// Crash: the in-memory log is gone. Resume rebuilds it from the stored
	// rows and pins the same head again.
	stored := log.Revisions()
	rebuilt, err := runtime.NewVariableRevisionLog(tenant, instance, stored)
	if err != nil {
		t.Fatalf("rebuild from stored rows: %v", err)
	}
	resumed := rebuilt.PinAttempt()
	if resumed.Head() != first.Head() {
		t.Fatalf("resumed head = %d, want %d", resumed.Head(), first.Head())
	}
	for _, tc := range [][2]string{{"proposal", "grade"}, {"proposal", "band.min"}, {"approval", "approved"}} {
		raw, ok := resumed.Variable(tc[0], tc[1])
		if !ok {
			t.Fatalf("resumed attempt cannot read %s.%s", tc[0], tc[1])
		}
		if raw != before[tc[0]+"."+tc[1]] {
			t.Fatalf("resumed %s.%s = %s, want %s", tc[0], tc[1], raw, before[tc[0]+"."+tc[1]])
		}
	}
	if before["proposal.grade"] != `"L6"` || before["proposal.band.min"] != `120` || before["approval.approved"] != `false` {
		t.Fatalf("reads do not match the written revisions: %+v", before)
	}
}
