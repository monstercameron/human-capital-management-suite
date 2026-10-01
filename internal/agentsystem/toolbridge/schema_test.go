package toolbridge

import (
	"encoding/json"
	"testing"
)

func TestTodo_AGENT_025_Schema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"workerId":{"type":"string","minLength":2},"limit":{"type":"integer","minimum":1,"maximum":5},"tags":{"type":"array","items":{"type":"string"},"minItems":1}},"required":["workerId","limit"],"additionalProperties":false}`)
	tests := []struct {
		name  string
		raw   json.RawMessage
		valid bool
	}{
		{name: "valid nested values", raw: json.RawMessage(`{"tags":["active"],"limit":3,"workerId":"w7"}`), valid: true},
		{name: "unknown field", raw: json.RawMessage(`{"workerId":"w7","limit":3,"admin":true}`)},
		{name: "wrong scalar type", raw: json.RawMessage(`{"workerId":7,"limit":3}`)},
		{name: "integer bound", raw: json.RawMessage(`{"workerId":"w7","limit":6}`)},
		{name: "required field", raw: json.RawMessage(`{"workerId":"w7"}`)},
		{name: "bad item type", raw: json.RawMessage(`{"workerId":"w7","limit":3,"tags":[1]}`)},
		{name: "trailing data", raw: json.RawMessage(`{"workerId":"w7","limit":3} nope`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := validateArguments(schema, tc.raw)
			if tc.valid && err != nil {
				t.Fatalf("valid argument error = %v", err)
			}
			if !tc.valid && (err == nil || len(got) != 0) {
				t.Fatalf("invalid arguments accepted: %s, err=%v", got, err)
			}
		})
	}
}

func TestTodo_AGENT_025_SchemaRefusesUnsupportedConstraints(t *testing.T) {
	for _, schema := range []json.RawMessage{
		json.RawMessage(`{"type":"object","$ref":"https://attacker/schema"}`),
		json.RawMessage(`{"type":"object","additionalProperties":{"type":"string"}}`),
		json.RawMessage(`{"type":"object","properties":{"field":{"type":"string","maximum":4}},"additionalProperties":false}`),
	} {
		if err := validateJSONSchema(schema, []byte(`{"x":1}`)); err == nil {
			t.Fatalf("unsupported schema constraint was accepted: %s", schema)
		}
	}
	deep := `{"type":"object"}`
	for range maxSchemaDepth + 1 {
		deep = `{"type":"object","properties":{"x":` + deep + `},"additionalProperties":false}`
	}
	value := `null`
	for range maxSchemaDepth + 1 {
		value = `{"x":` + value + `}`
	}
	if err := validateJSONSchema(json.RawMessage(deep), []byte(value)); err == nil {
		t.Fatal("overly deep schema was accepted")
	}
}

func TestTodo_AGENT_025_SchemaRefusesMalformedOptionalChild(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"optional":{"type":"not-a-type"}},"additionalProperties":false}`)
	if err := validateJSONSchema(schema, []byte(`{}`)); err == nil {
		t.Fatal("malformed optional child schema was accepted when the property was absent")
	}
}
