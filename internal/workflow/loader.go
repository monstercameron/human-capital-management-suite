package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/authoredwire"
)

// Load parses a workflow definition from its declarative document shape.
//
// The shape is JSON rather than YAML deliberately: the third-party library
// firewall classifies the YAML parser as a tooling-only dependency, and a
// production package inside internal/ may not carry it
// (definitions/architecture/dependency-roles.yaml). The standard library's
// decoder gives the property that matters here anyway.
//
// That property is that an unknown key is an error, not a silent omission. A
// loader that quietly drops a key it does not recognize is exactly how a
// definition carrying, say, inline transform code slips past the compiler that
// is supposed to reject it. Loading only parses; nothing is validated until
// [Compile] sees it.
func Load(data []byte) (Definition, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var def Definition
	if err := dec.Decode(&def); err != nil {
		return Definition{}, fmt.Errorf("workflow: load definition: %w", err)
	}
	return def, nil
}

// LoadFile reads and parses a workflow definition document.
func LoadFile(path string) (Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, fmt.Errorf("workflow: load definition: %w", err)
	}
	return Load(data)
}

// Marshal renders a definition back to the document shape [Load] accepts. It
// exists so a definition authored as Go values and one authored as a document
// can be compared as the same artifact.
func Marshal(def Definition) ([]byte, error) {
	b, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("workflow: marshal definition: %w", err)
	}
	return append(b, '\n'), nil
}

// LoadProtoJSON parses a workflow definition from the stored authoring form:
// the protojson rendering of an AuthoredWorkflowDefinition (WF-EXT-010).
// Like [Load] it is strict — an unknown field is an error, never a silent
// omission — because protojson's default rejects unknown fields. Layout and
// canvas state in the artifact do not enter the definition; see
// [FromAuthoredProto].
func LoadProtoJSON(data []byte) (Definition, error) {
	pb, err := authoredwire.Unmarshal(data)
	if err != nil {
		return Definition{}, fmt.Errorf("workflow: load definition protojson: %w", err)
	}
	return FromAuthoredProto(pb), nil
}

// MarshalProtoJSON renders a definition as its stored authoring form: the
// protojson rendering of the AuthoredWorkflowDefinition mirror. It is the
// artifact [LoadProtoJSON] accepts.
func MarshalProtoJSON(def Definition) ([]byte, error) {
	b, err := authoredwire.Marshal(ToAuthoredProto(def))
	if err != nil {
		return nil, fmt.Errorf("workflow: marshal definition protojson: %w", err)
	}
	return append(b, '\n'), nil
}
