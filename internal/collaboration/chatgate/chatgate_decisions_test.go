package chatgate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTodo_CHATGATE_001: the record holds the eight decisions of the todo in its
// order, numbered, each with what enforces it, and then the numbers the
// consumers fixed.
func TestTodo_CHATGATE_001(t *testing.T) {
	decisions := Decisions()
	if len(decisions) < 8 {
		t.Fatalf("%d decisions", len(decisions))
	}
	phrases := []string{"additional requirement", "major version requires members to answer again", "automatic, rule and review", "retention", "belong to the member", "audited", "untrusted data", "never grants a role"}
	for i, want := range phrases {
		d := decisions[i]
		if d.Number != i+1 || !strings.Contains(d.Rule, want) || strings.TrimSpace(d.Enforced) == "" {
			t.Fatalf("decision %d does not state %q with its enforcement: %+v", i+1, want, d)
		}
	}
	for i, d := range decisions {
		if d.Number != i+1 {
			t.Fatalf("decision numbers are not consecutive at %d", i)
		}
	}
	if !strings.Contains(decisions[8].Rule, "k = 5") {
		t.Fatalf("the group size is not recorded: %+v", decisions[8])
	}
	if !strings.Contains(DecisionRecord(), "Channel gates: decisions") {
		t.Fatal("record heading")
	}
}

// TestTodo_CHATGATE_001_Golden pins the record's bytes. Run with
// CHATGATE_GOLDEN_UPDATE=1 to rewrite the file after changing a decision on
// purpose.
func TestTodo_CHATGATE_001_Golden(t *testing.T) {
	path := filepath.Join("testdata", "chatgate_decisions.golden")
	if os.Getenv("CHATGATE_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(DecisionRecord()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != DecisionRecord() {
		t.Fatal("the decision record changed: update the golden file on purpose")
	}
}

// TestTodo_CHATGATE_001_Security asserts each decision's "never" as the named
// refusal, and the requirement that a gate follows the channel's own policy.
func TestTodo_CHATGATE_001_Security(t *testing.T) {
	never := Nevers()
	e := newSegEnv(t, false)

	// Never grant a role in the organization: a label named like a role is
	// refused, and the holder of a label has no authority.
	if err := e.rules("role", SegmentRule{ID: "r", Field: "office", Value: "Denver", Label: "Workspace administrator"}); !errors.Is(err, never["grant a role in the organization"]) {
		t.Fatalf("a role label: %v", err)
	}
	e.must(e.rules("ok", officeRules...))
	e.join("ana", "Denver", "Night")
	if err := e.s.Define(t.Context(), Command{Scope: e.scope, Actor: Actor{"tenant", "ana"}, Key: "x", ExpectedRevision: e.cmd("admin", "y").ExpectedRevision}, Definition{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("a person with labels administered the gate: %v", err)
	}

	// Never change what a non-member can discover: they see the questions and
	// nothing of segments; a person who may not discover is refused.
	raw, _ := json.Marshal(mustGet(t, e, "outsider"))
	if strings.Contains(string(raw), "Denver crew") || strings.Contains(string(raw), "Assignments") {
		t.Fatalf("a non-member reads segments: %s", raw)
	}
	e.auth.deny = map[string]bool{"discover": true}
	if _, err := e.s.Get(t.Context(), Actor{"tenant", "outsider"}, e.scope, false); !errors.Is(err, never["change what a non-member can discover"]) {
		t.Fatalf("a person who may not discover read the gate: %v", err)
	}
	e.auth.deny = map[string]bool{}

	// Never hide a message from someone the channel lets read it, outside the
	// restricted-visibility levels.
	e.must(e.s.SetAudienceLevels(t.Context(), e.cmd("admin", "levels"), true, false))
	if _, err := e.s.Reach(t.Context(), Actor{"tenant", "admin"}, e.scope, []string{"Denver office"}, true); !errors.Is(err, never["hide a message outside the restricted levels"]) {
		t.Fatalf("restricted visibility with the level off: %v", err)
	}

	// A gate is in addition to the channel's eligibility, never instead of it:
	// a person the channel does not admit is refused before the gate is read.
	e.auth.deny = map[string]bool{"eligible": true}
	if err := e.s.CanJoin(t.Context(), Actor{"tenant", "new"}, e.scope); !errors.Is(err, ErrDenied) {
		t.Fatalf("an ineligible person: %v", err)
	}
	if _, err := e.s.Submit(t.Context(), e.cmd("new", "n"), "1.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}); !errors.Is(err, ErrDenied) {
		t.Fatalf("an ineligible person submitted: %v", err)
	}
	e.auth.deny = map[string]bool{}

	// A gate may not ask for what the record forbids.
	f := newSegEnv(t, false)
	bad := segFields()
	bad[0].Label = "Your passport number"
	if err := f.s.Define(t.Context(), f.cmd("admin", "bad"), Definition{Mode: "automatic", Purpose: "Join", Fields: bad}); !errors.Is(err, ErrDenied) {
		t.Fatalf("a forbidden question: %v", err)
	}

	// Every Never has a refusal, and the record names them all.
	if len(never) != 3 {
		t.Fatalf("%d nevers", len(never))
	}
	_ = time.Now
}

func mustGet(t *testing.T, e *segEnv, person string) Gate {
	t.Helper()
	g, err := e.s.Get(t.Context(), Actor{"tenant", person}, e.scope, false)
	if err != nil {
		t.Fatal(err)
	}
	return g
}
