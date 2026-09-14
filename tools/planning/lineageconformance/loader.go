package lineageconformance

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
)

// LoadInput reads the live registries below root: the SLICE-016 closure
// witnesses compiled from closurewitness.LoadSnapshot, the compiled
// definitions and bindings, the default producers and the todo and test
// facts producers are validated against. It never mutates the repository.
func LoadInput(root, asOf string) (Input, error) {
	snap, err := closurewitness.LoadSnapshot(root, asOf)
	if err != nil {
		return Input{}, fmt.Errorf("lineageconformance: load closure snapshot: %w", err)
	}
	witnesses, err := closurewitness.Compile(snap)
	if err != nil {
		return Input{}, fmt.Errorf("lineageconformance: compile closure witnesses: %w", err)
	}
	producers, err := DefaultProducers()
	if err != nil {
		return Input{}, err
	}
	return Input{
		AsOf: asOf, Witnesses: witnesses,
		Definitions: definitions.All(), Bindings: definitions.Bindings(),
		Producers: producers, Todos: snap.Todos, TestExists: snap.TestExists,
	}, nil
}
