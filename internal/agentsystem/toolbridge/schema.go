package toolbridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
)

const maxSchemaDepth = 32

func validateArguments(schema, raw json.RawMessage) (json.RawMessage, string, error) {
	_, canonical, err := decodeJSON(raw)
	if err != nil {
		return nil, "", err
	}
	if err := validateJSONSchema(schema, canonical); err != nil {
		return nil, "", err
	}
	return canonical, digest(canonical), nil
}

func validateJSONSchema(schema, raw []byte) error {
	rules, err := decodeSchemaRules(schema)
	if err != nil {
		return err
	}
	typ, err := schemaString(rules, "type")
	if err != nil || typ != "object" {
		return errors.New("schema must declare object type")
	}
	if err := validateKeywordCompatibility(typ, rules); err != nil {
		return err
	}
	value, _, err := decodeJSON(raw)
	if err != nil {
		return err
	}
	return validateValue(value, rules, 0)
}

func validateSchemaDocument(schema []byte) error {
	_, err := decodeSchemaRules(schema)
	return err
}

func decodeSchemaRules(schema []byte) (map[string]json.RawMessage, error) {
	if _, _, err := decodeJSON(schema); err != nil {
		return nil, errors.New("schema is not strict JSON")
	}
	var rules map[string]json.RawMessage
	if err := json.Unmarshal(schema, &rules); err != nil || rules == nil {
		return nil, errors.New("schema must be an object")
	}
	if err := validateSchemaRules(rules, 0); err != nil {
		return nil, err
	}
	return rules, nil
}

// validateSchemaRules validates the schema document independently of any
// particular instance. Without this pass, malformed optional child schemas
// could remain hidden until a value happened to use that property.
func validateSchemaRules(rules map[string]json.RawMessage, depth int) error {
	if depth > maxSchemaDepth {
		return errors.New("schema nesting exceeds limit")
	}
	if len(rules) == 0 {
		return errors.New("empty schema")
	}
	for keyword := range rules {
		if !supportedKeyword(keyword) {
			return fmt.Errorf("unsupported schema keyword %q", keyword)
		}
	}
	typ, err := schemaString(rules, "type")
	if err != nil {
		return err
	}
	switch typ {
	case "object", "array", "string", "number", "integer", "boolean", "null":
	default:
		return fmt.Errorf("unsupported schema type %q", typ)
	}
	if err := validateKeywordCompatibility(typ, rules); err != nil {
		return err
	}
	if raw, ok := rules["properties"]; ok {
		var properties map[string]json.RawMessage
		if err := json.Unmarshal(raw, &properties); err != nil || properties == nil {
			return errors.New("properties must be an object")
		}
		for name, childRaw := range properties {
			var child map[string]json.RawMessage
			if err := json.Unmarshal(childRaw, &child); err != nil || child == nil {
				return fmt.Errorf("property %q schema is invalid", name)
			}
			if err := validateSchemaRules(child, depth+1); err != nil {
				return fmt.Errorf("property %q: %w", name, err)
			}
		}
	}
	if raw, ok := rules["items"]; ok {
		var child map[string]json.RawMessage
		if err := json.Unmarshal(raw, &child); err != nil || child == nil {
			return errors.New("items must be a schema object")
		}
		if err := validateSchemaRules(child, depth+1); err != nil {
			return fmt.Errorf("items: %w", err)
		}
	}
	if raw, ok := rules["required"]; ok {
		var required []string
		if err := json.Unmarshal(raw, &required); err != nil {
			return errors.New("required must be an array of strings")
		}
	}
	if raw, ok := rules["additionalProperties"]; ok {
		var allowed bool
		if err := json.Unmarshal(raw, &allowed); err != nil {
			return errors.New("additionalProperties must be boolean")
		}
	}
	if raw, ok := rules["enum"]; ok {
		var choices []any
		if err := json.Unmarshal(raw, &choices); err != nil || len(choices) == 0 {
			return errors.New("enum must be a non-empty array")
		}
	}
	for _, key := range []string{"minLength", "maxLength", "minItems", "maxItems"} {
		if _, _, err := integerRule(rules, key); err != nil {
			return err
		}
	}
	return nil
}

func decodeJSON(raw []byte) (any, []byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil, errors.New("empty JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, nil, errors.New("multiple JSON values")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	return value, canonical, nil
}

func validateValue(value any, rules map[string]json.RawMessage, depth int) error {
	if depth > maxSchemaDepth {
		return errors.New("schema nesting exceeds limit")
	}
	if len(rules) == 0 {
		return errors.New("empty schema")
	}
	for keyword := range rules {
		if !supportedKeyword(keyword) {
			return fmt.Errorf("unsupported schema keyword %q", keyword)
		}
	}
	if err := validateType(value, rules); err != nil {
		return err
	}
	typ, _ := schemaString(rules, "type")
	if err := validateKeywordCompatibility(typ, rules); err != nil {
		return err
	}
	if err := validateEnum(value, rules); err != nil {
		return err
	}
	switch typed := value.(type) {
	case map[string]any:
		return validateObject(typed, rules, depth)
	case []any:
		return validateArray(typed, rules, depth)
	case string:
		return validateString(typed, rules)
	case json.Number:
		return validateNumber(typed, rules)
	default:
		return nil
	}
}

func validateKeywordCompatibility(typ string, rules map[string]json.RawMessage) error {
	for _, keyword := range []string{"properties", "required", "additionalProperties"} {
		if _, ok := rules[keyword]; ok && typ != "object" {
			return fmt.Errorf("keyword %s requires object type", keyword)
		}
	}
	for _, keyword := range []string{"items", "minItems", "maxItems"} {
		if _, ok := rules[keyword]; ok && typ != "array" {
			return fmt.Errorf("keyword %s requires array type", keyword)
		}
	}
	for _, keyword := range []string{"minLength", "maxLength"} {
		if _, ok := rules[keyword]; ok && typ != "string" {
			return fmt.Errorf("keyword %s requires string type", keyword)
		}
	}
	for _, keyword := range []string{"minimum", "maximum"} {
		if _, ok := rules[keyword]; ok && typ != "integer" && typ != "number" {
			return fmt.Errorf("keyword %s requires numeric type", keyword)
		}
	}
	return nil
}

func supportedKeyword(keyword string) bool {
	switch keyword {
	case "type", "properties", "required", "additionalProperties", "items", "enum", "const", "minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems":
		return true
	default:
		return false
	}
}

func validateType(value any, rules map[string]json.RawMessage) error {
	typ, err := schemaString(rules, "type")
	if err != nil {
		return err
	}
	valid := false
	switch typ {
	case "object":
		_, valid = value.(map[string]any)
	case "array":
		_, valid = value.([]any)
	case "string":
		_, valid = value.(string)
	case "number":
		_, valid = value.(json.Number)
	case "integer":
		if n, ok := value.(json.Number); ok {
			_, err := n.Int64()
			valid = err == nil
		}
	case "boolean":
		_, valid = value.(bool)
	case "null":
		valid = value == nil
	default:
		return fmt.Errorf("unsupported schema type %q", typ)
	}
	if !valid {
		return fmt.Errorf("value does not match %s", typ)
	}
	return nil
}

func validateEnum(value any, rules map[string]json.RawMessage) error {
	if raw, ok := rules["const"]; ok {
		var want any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&want); err != nil || !reflect.DeepEqual(value, want) {
			return errors.New("value does not match const")
		}
	}
	if raw, ok := rules["enum"]; ok {
		var choices []any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&choices); err != nil || choices == nil {
			return errors.New("enum must be a non-empty array")
		}
		for _, choice := range choices {
			if reflect.DeepEqual(value, choice) {
				return nil
			}
		}
		return errors.New("value is not in enum")
	}
	return nil
}

func validateObject(value map[string]any, rules map[string]json.RawMessage, depth int) error {
	var properties map[string]json.RawMessage
	if raw, ok := rules["properties"]; ok {
		if err := json.Unmarshal(raw, &properties); err != nil || properties == nil {
			return errors.New("properties must be an object")
		}
	}
	var required []string
	if raw, ok := rules["required"]; ok {
		if err := json.Unmarshal(raw, &required); err != nil {
			return errors.New("required must be an array of strings")
		}
	}
	for _, name := range required {
		if _, ok := value[name]; !ok {
			return fmt.Errorf("required property %q is missing", name)
		}
	}
	for name, item := range value {
		raw, exists := properties[name]
		if !exists {
			allowed, err := additionalAllowed(rules)
			if err != nil || !allowed {
				return fmt.Errorf("property %q is not allowed", name)
			}
			continue
		}
		var child map[string]json.RawMessage
		if err := json.Unmarshal(raw, &child); err != nil || child == nil {
			return fmt.Errorf("property %q schema is invalid", name)
		}
		if err := validateValue(item, child, depth+1); err != nil {
			return fmt.Errorf("property %q: %w", name, err)
		}
	}
	return nil
}

func additionalAllowed(rules map[string]json.RawMessage) (bool, error) {
	raw, ok := rules["additionalProperties"]
	if !ok {
		return false, nil
	}
	var allowed bool
	if err := json.Unmarshal(raw, &allowed); err != nil {
		return false, errors.New("additionalProperties must be boolean")
	}
	return allowed, nil
}

func validateArray(value []any, rules map[string]json.RawMessage, depth int) error {
	if err := checkLength(len(value), rules, "minItems", "maxItems"); err != nil {
		return err
	}
	raw, ok := rules["items"]
	if !ok {
		return errors.New("array schema requires items")
	}
	var child map[string]json.RawMessage
	if err := json.Unmarshal(raw, &child); err != nil || child == nil {
		return errors.New("items must be a schema object")
	}
	for index, item := range value {
		if err := validateValue(item, child, depth+1); err != nil {
			return fmt.Errorf("item %d: %w", index, err)
		}
	}
	return nil
}

func validateString(value string, rules map[string]json.RawMessage) error {
	return checkLength(len([]rune(value)), rules, "minLength", "maxLength")
}

func checkLength(actual int, rules map[string]json.RawMessage, minKey, maxKey string) error {
	if min, ok, err := integerRule(rules, minKey); err != nil || ok && actual < min {
		return fmt.Errorf("value is below %s", minKey)
	}
	if max, ok, err := integerRule(rules, maxKey); err != nil || ok && actual > max {
		return fmt.Errorf("value is above %s", maxKey)
	}
	return nil
}

func validateNumber(value json.Number, rules map[string]json.RawMessage) error {
	n, err := value.Float64()
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return errors.New("number is invalid")
	}
	for key, compare := range map[string]func(float64, float64) bool{
		"minimum": func(v, bound float64) bool { return v >= bound },
		"maximum": func(v, bound float64) bool { return v <= bound },
	} {
		if raw, ok := rules[key]; ok {
			var number json.Number
			if json.Unmarshal(raw, &number) != nil {
				return fmt.Errorf("%s must be a number", key)
			}
			bound, _ := number.Float64()
			if !compare(n, bound) {
				return fmt.Errorf("number violates %s", key)
			}
		}
	}
	return nil
}

func schemaString(rules map[string]json.RawMessage, key string) (string, error) {
	raw, ok := rules[key]
	if !ok {
		return "", fmt.Errorf("schema %s is required", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("schema %s must be a string", key)
	}
	return value, nil
}

func integerRule(rules map[string]json.RawMessage, key string) (int, bool, error) {
	raw, ok := rules[key]
	if !ok {
		return 0, false, nil
	}
	var number int
	if err := json.Unmarshal(raw, &number); err != nil || number < 0 {
		return 0, true, fmt.Errorf("%s must be a non-negative integer", key)
	}
	return number, true, nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
