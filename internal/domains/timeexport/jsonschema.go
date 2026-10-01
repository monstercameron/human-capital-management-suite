package timeexport

import (
	"encoding/json"
	"fmt"
)

// schema is a minimal, purpose-built subset of JSON Schema (draft 2020-12
// vocabulary names, not the full draft): "type", "properties", "required",
// "items" and "additionalProperties". It exists so the CONFORMANCE test can
// validate emitted bytes against a schema authored in testdata without a
// third-party dependency; it is not a general-purpose validator and it
// rejects any schema keyword it does not implement rather than silently
// ignoring it.
type schema struct {
	Type                 string             `json:"type"`
	Properties           map[string]*schema `json:"properties"`
	Required             []string           `json:"required"`
	Items                *schema            `json:"items"`
	AdditionalProperties *bool              `json:"additionalProperties"`
}

// knownSchemaKeywords are the only top-level keys this validator accepts in
// any schema node, so an authored schema that uses an unimplemented keyword
// fails loudly instead of being silently under-validated.
var knownSchemaKeywords = map[string]bool{
	"type": true, "properties": true, "required": true, "items": true,
	"additionalProperties": true, "$schema": true, "$id": true, "title": true, "description": true,
}

// ValidateAgainstSchema validates data (JSON bytes) against a schema
// document (also JSON bytes). It returns a descriptive error naming the
// first violation found; it does not collect every violation.
func ValidateAgainstSchema(schemaBytes, dataBytes []byte) error {
	if err := checkKnownKeywordsRecursive(schemaBytes); err != nil {
		return err
	}
	var s schema
	if err := json.Unmarshal(schemaBytes, &s); err != nil {
		return fmt.Errorf("timeexport: schema is not valid JSON: %w", err)
	}
	var data any
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return fmt.Errorf("timeexport: data is not valid JSON: %w", err)
	}
	return validateNode(&s, data, "$")
}

func checkKnownKeywordsRecursive(raw []byte) error {
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return fmt.Errorf("timeexport: schema is not valid JSON: %w", err)
	}
	return checkKeywordsValue(generic)
}

func checkKeywordsValue(v any) error {
	obj, ok := v.(map[string]any)
	if !ok {
		if arr, ok := v.([]any); ok {
			for _, item := range arr {
				if err := checkKeywordsValue(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for key, val := range obj {
		if key == "properties" {
			props, ok := val.(map[string]any)
			if !ok {
				return fmt.Errorf("timeexport: schema \"properties\" must be an object")
			}
			for _, p := range props {
				if err := checkKeywordsValue(p); err != nil {
					return err
				}
			}
			continue
		}
		if !knownSchemaKeywords[key] {
			return fmt.Errorf("timeexport: schema uses unimplemented keyword %q", key)
		}
		if err := checkKeywordsValue(val); err != nil {
			return err
		}
	}
	return nil
}

func validateNode(s *schema, data any, path string) error {
	if s == nil {
		return nil
	}
	switch s.Type {
	case "object":
		obj, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("timeexport: %s: expected an object", path)
		}
		for _, req := range s.Required {
			if _, ok := obj[req]; !ok {
				return fmt.Errorf("timeexport: %s: missing required property %q", path, req)
			}
		}
		for key, val := range obj {
			propSchema, known := s.Properties[key]
			if !known {
				if s.AdditionalProperties != nil && !*s.AdditionalProperties {
					return fmt.Errorf("timeexport: %s: unmapped property %q", path, key)
				}
				continue
			}
			if err := validateNode(propSchema, val, path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		arr, ok := data.([]any)
		if !ok {
			return fmt.Errorf("timeexport: %s: expected an array", path)
		}
		for i, item := range arr {
			if err := validateNode(s.Items, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		if _, ok := data.(string); !ok {
			return fmt.Errorf("timeexport: %s: expected a string", path)
		}
	case "integer":
		n, ok := data.(float64)
		if !ok || n != float64(int64(n)) {
			return fmt.Errorf("timeexport: %s: expected an integer", path)
		}
	case "":
		// No declared type: any value is accepted at this node.
	default:
		return fmt.Errorf("timeexport: %s: schema declares unsupported type %q", path, s.Type)
	}
	return nil
}
