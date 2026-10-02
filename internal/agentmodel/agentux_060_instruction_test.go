package agentmodel

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// The instruction a policy answer is generated under tells the model, on the
// search turn and on the answer turn alike, to cite only what it used, not to
// put the document's title in front of the answer, and to answer a list
// question as a counted list (AGENTUX-060). Whether a model obeys is seen in a
// real run; that the instruction says it is read from the source here.
func TestTodo_AGENTUX_060(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "schemaflux_adapter.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var instructions []string
	ast.Inspect(file, func(n ast.Node) bool {
		literal, ok := n.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		if text, err := strconv.Unquote(literal.Value); err == nil && strings.HasPrefix(text, "Return a typed reply and classify only the invoking human's requested actions") && strings.Contains(text, "citation markers") {
			instructions = append(instructions, text)
		}
		return true
	})
	if len(instructions) != 2 {
		t.Fatalf("found %d policy answer instructions, want the search turn and the answer turn", len(instructions))
	}
	for index, instruction := range instructions {
		for _, rule := range []string{
			"Cite only documents actually used; never cite another search hit merely because it was returned.",
			"Do not repeat a document title as an answer prefix.",
			"For a list question give a numbered list, state how many documents were found, and limit it to the requested number.",
			"Write no links and no line beginning with Source",
		} {
			if !strings.Contains(instruction, rule) {
				t.Fatalf("instruction %d does not say %q", index+1, rule)
			}
		}
	}
}
