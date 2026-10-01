package portable

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

// CanonicalJSON returns deterministic JSON for a well-formed portable definition.
func (d Definition) CanonicalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("%w: encode: %v", ErrInvalidDefinition, err)
	}
	return data, nil
}

// Validate checks the format and manifest fields while requiring explicit empty
// source and tool ceilings to remain distinguishable from omitted data.
func (d Definition) Validate() error {
	manifest := agentmanifest.Manifest{
		SchemaVersion: d.ManifestSchema, ID: "portable.validation", Version: 1,
		OwnerID: "portable.validation", Purpose: d.Purpose,
		InstructionsDigest: d.InstructionsDigest, SourceCeiling: d.SourceCeiling,
		ToolCeiling: d.ToolCeiling, ModelPolicy: d.ModelPolicy,
		AutonomyCeiling: d.AutonomyCeiling, Budget: d.Budget,
		OutputSchema: d.OutputSchema, ContextGrants: []agentmanifest.Reference{},
		EvaluationRefs: d.EvaluationRefs,
	}
	if d.FormatVersion != CurrentFormatVersion {
		return fmt.Errorf("%w: unsupported format version", ErrInvalidDefinition)
	}
	if d.SourceCeiling == nil || d.ToolCeiling == nil || d.EvaluationRefs == nil {
		return fmt.Errorf("%w: ceilings and evaluations must be explicit arrays", ErrInvalidDefinition)
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDefinition, err)
	}
	return nil
}

// Parse strictly parses one portable definition, refusing ambiguous JSON,
// unknown fields, invalid UTF-8, oversized input, and trailing values.
func Parse(data []byte) (Definition, error) {
	var definition Definition
	if len(data) == 0 || len(data) > maxPortableBytes || !utf8.Valid(data) {
		return definition, fmt.Errorf("%w: input size or encoding", ErrInvalidDefinition)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return definition, fmt.Errorf("%w: %v", ErrInvalidDefinition, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return definition, fmt.Errorf("%w: decode: %v", ErrInvalidDefinition, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return definition, fmt.Errorf("%w: trailing JSON", ErrInvalidDefinition)
	}
	if err := definition.Validate(); err != nil {
		return definition, err
	}
	return definition, nil
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func consumeValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[name]; exists {
				return fmt.Errorf("duplicate JSON field %q", name)
			}
			seen[name] = struct{}{}
			if err := consumeValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := consumeValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}
