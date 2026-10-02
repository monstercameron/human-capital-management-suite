package agenticon

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATBUG_033 pins the fallback an agent with no stored icon wears: a
// real icon from the generator's own vocabulary, derived from its id alone, never
// the neutral default every agent would share, and never the same glyph for two
// agents of one set.
func TestTodo_CHATBUG_033(t *testing.T) {
	ids := []string{"policy-helper", "assistant", "agent:coach", "birthday-bot", "persona.comp-analyst"}
	for i := 0; i < 40; i++ {
		ids = append(ids, fmt.Sprintf("agent-%02d", i))
	}
	all := Fallbacks(ids)
	if len(all) != len(ids) {
		t.Fatalf("want one fallback per id, got %d for %d", len(all), len(ids))
	}
	glyphs := map[string]string{}
	for id, value := range all {
		if !value.Valid() || value.Glyph == "neutral" {
			t.Fatalf("%s: the fallback is not a real icon: %+v", id, value)
		}
		if other, taken := glyphs[value.Glyph]; taken {
			t.Fatalf("%s and %s share the glyph %s", id, other, value.Glyph)
		}
		glyphs[value.Glyph] = id
	}
	// The same set always yields the same icons, whatever order it arrives in.
	reversed := make([]string, len(ids))
	for i, id := range ids {
		reversed[len(ids)-1-i] = id
	}
	again := Fallbacks(reversed)
	for id, value := range all {
		if again[id] != value {
			t.Fatalf("%s: the fallback depends on the order of the set: %+v vs %+v", id, value, again[id])
		}
	}
	// An agent's own id is enough: the two named agents differ and are stable.
	first, second := Fallback("policy-helper"), Fallback("assistant")
	if first == second || first != Fallback("policy-helper") || !first.Valid() || !second.Valid() {
		t.Fatalf("policy-helper=%+v assistant=%+v", first, second)
	}
	// A stored icon always wins; no id and no stored icon is the only empty case.
	stored := Generate(Input{Name: "Policy Helper"})
	if got := ValueFor(stored, "policy-helper", []string{"assistant"}); got != stored {
		t.Fatalf("a stored icon was replaced: %+v", got)
	}
	if got := ValueFor(Value{}, "  ", nil); got.Valid() {
		t.Fatalf("no id yielded an icon: %+v", got)
	}
	// Siblings keep one page's agents apart.
	a, b := ValueFor(Value{}, "policy-helper", []string{"assistant", "policy-helper"}), ValueFor(Value{}, "assistant", []string{"assistant", "policy-helper"})
	if a.Glyph == b.Glyph {
		t.Fatalf("two agents on one page share a glyph: %+v %+v", a, b)
	}
}

// TestTodo_CHATBUG_033_Browser renders the fallback: a decorative drawing, not
// the neutral diamond, and the same drawing as the SVG a server page would send.
func TestTodo_CHATBUG_033_Browser(t *testing.T) {
	node, err := ui.RenderToString(NodeFor(Value{}, "policy-helper", "assistant", "policy-helper"))
	if err != nil {
		t.Fatal(err)
	}
	neutral, err := ui.RenderToString(Node(Value{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(node, `class="agent-icon"`) || !strings.Contains(node, `aria-hidden="true"`) || node == neutral {
		t.Fatalf("the fallback is not its own decorative icon: %s", node)
	}
	value := ValueFor(Value{}, "policy-helper", []string{"assistant", "policy-helper"})
	if SVG(value) == SVG(Value{}) || !strings.Contains(SVG(value), `aria-hidden="true"`) {
		t.Fatal("the server-rendered fallback is the shared neutral drawing")
	}
}
