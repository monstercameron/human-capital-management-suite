package diagnosticsession

import (
	"bytes"
	"encoding/json"
	"strings"
)

const RedactedValue = "[REDACTED]"

// SensitiveKey is case-insensitive and covers credentials, personal data and
// unbounded content that must never appear in support evidence.
func SensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, token := range []string{"password", "secret", "token", "authorization", "cookie", "email", "phone", "ssn", "salary", "access_key", "private_key"} {
		if strings.Contains(k, token) {
			return true
		}
	}
	return false
}

// Redact returns a redacted copy of an arbitrary evidence payload. Supported
// values (typed nested maps, slices, arrays, structs, pointers and interface
// values) are normalized through JSON first, so numbers decode as json.Number
// and output stays deterministic and JSON-safe. Cyclic values and payloads
// that JSON cannot represent (channels, functions and other unsupported or
// unsafe shapes) fail closed to RedactedValue instead of leaking raw values
// or recursing forever. Unexported struct fields are dropped by the JSON
// encoding and never appear in the output. The input is never mutated.
func Redact(value any) any {
	if value == nil {
		return nil
	}
	normalized, ok := normalizeJSON(value)
	if !ok {
		return RedactedValue
	}
	return redactNormalized(normalized)
}

// normalizeJSON round-trips value through its JSON encoding and decodes the
// result with json.UseNumber so numeric values remain safe and predictable.
// It reports false when the payload is cyclic or otherwise not representable
// in JSON.
func normalizeJSON(value any) (any, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out any
	if err := decoder.Decode(&out); err != nil {
		return nil, false
	}
	return out, true
}

// redactNormalized copies the normalized JSON shape, replacing values under
// sensitive keys with RedactedValue. It builds fresh maps and slices and
// never mutates its input.
func redactNormalized(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			if SensitiveKey(k) {
				out[k] = RedactedValue
			} else {
				out[k] = redactNormalized(x)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = redactNormalized(x)
		}
		return out
	default:
		return value
	}
}
