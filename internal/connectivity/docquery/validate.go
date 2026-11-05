package docquery

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// QuarantinedDocument is one ledger entry: a refused document plus the
// machine-readable reason it was refused.
type QuarantinedDocument struct {
	// Type is the document type the payload claimed.
	Type string
	// Reason is the sentinel-wrapped refusal cause.
	Reason error
	// Field names the offending field when the refusal is field-scoped.
	Field string
}

// Error renders the ledger entry without payload bytes.
func (q QuarantinedDocument) Error() string {
	if q.Field == "" {
		return fmt.Sprintf("type %q quarantined: %v", q.Type, q.Reason)
	}
	return fmt.Sprintf("type %q field %q quarantined: %v", q.Type, q.Field, q.Reason)
}

// ValidatedDocument is a document that passed validation: exactly the
// declared fields, each holding a value of the declared wire shape.
type ValidatedDocument struct {
	// Type is the registered document type name.
	Type string
	// Fields holds the validated field values keyed by field name.
	Fields map[string]any
}

// ValidateDocument checks raw (canonical JSON) against the registered type
// and returns either the validated document or a quarantine entry. It never
// returns a Go error for payload problems: refusal is ledger data, so one
// bad document cannot fail a batch ingest. Only an unregistered type name
// is a hard error (a programming error, not a payload problem).
func (r *Registry) ValidateDocument(typeName string, raw json.RawMessage) (ValidatedDocument, *QuarantinedDocument, error) {
	t, err := r.Lookup(typeName)
	if err != nil {
		return ValidatedDocument{}, nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		q := QuarantinedDocument{Type: typeName, Reason: fmt.Errorf("%w: %v", ErrInvalidDocument, err)}
		r.recordQuarantine(q)
		return ValidatedDocument{}, &q, nil
	}

	// Undeclared fields quarantine: foreign-source schema drift must be
	// an explicit registration decision.
	for name := range doc {
		if _, ok := t.index[name]; !ok {
			q := QuarantinedDocument{
				Type:   typeName,
				Field:  name,
				Reason: fmt.Errorf("%w: %q not declared on %q", ErrUndeclaredField, name, typeName),
			}
			r.recordQuarantine(q)
			return ValidatedDocument{}, &q, nil
		}
	}

	out := make(map[string]any, len(t.Fields))
	for _, f := range t.Fields {
		v, present := doc[f.Name]
		if !present || v == nil {
			if f.Required {
				q := QuarantinedDocument{
					Type:   typeName,
					Field:  f.Name,
					Reason: fmt.Errorf("%w: %q", ErrMissingField, f.Name),
				}
				r.recordQuarantine(q)
				return ValidatedDocument{}, &q, nil
			}
			continue
		}
		if err := checkValue(f.WireType, v); err != nil {
			q := QuarantinedDocument{
				Type:   typeName,
				Field:  f.Name,
				Reason: fmt.Errorf("%w: field %q expects %s: %v", ErrTypeMismatch, f.Name, f.WireType, err),
			}
			r.recordQuarantine(q)
			return ValidatedDocument{}, &q, nil
		}
		out[f.Name] = v
	}
	return ValidatedDocument{Type: typeName, Fields: out}, nil, nil
}

// checkValue reports whether v matches the canonical-JSON spelling of wire.
func checkValue(wire string, v any) error {
	switch {
	case stringKind[wire]:
		return checkString(v)
	case instantKind[wire]:
		return checkInstant(v)
	case boolKind[wire]:
		return checkBool(v)
	case intKind[wire]:
		return checkInt(wire, v)
	case decimalKind[wire]:
		return checkDecimal(v)
	}
	switch wire {
	case "[]string":
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("want array, got %T", v)
		}
		for i, e := range arr {
			if err := checkString(e); err != nil {
				return fmt.Errorf("element %d: %v", i, err)
			}
		}
		return nil
	case "[]byte":
		return checkBinary(v)
	}
	if e, ok := sliceElem(wire); ok {
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("want array, got %T", v)
		}
		for i, el := range arr {
			if err := checkValue(e, el); err != nil {
				return fmt.Errorf("element %d: %v", i, err)
			}
		}
		return nil
	}
	if ev, ok := mapValue(wire); ok {
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("want object, got %T", v)
		}
		for k, el := range m {
			if err := checkValue(ev, el); err != nil {
				return fmt.Errorf("key %q: %v", k, err)
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported wire type %q", wire)
}

func checkString(v any) error {
	if _, ok := v.(string); !ok {
		return fmt.Errorf("want string, got %T", v)
	}
	return nil
}

func checkBool(v any) error {
	if _, ok := v.(bool); !ok {
		return fmt.Errorf("want bool, got %T", v)
	}
	return nil
}

// checkInstant accepts an RFC 3339 timestamp string or a $date ExtJSON
// marker object (the canonical spelling the mongo connector emits).
func checkInstant(v any) error {
	if _, ok := v.(string); ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return fmt.Errorf("want timestamp string or $date marker, got %T", v)
	}
	d, ok := m["$date"]
	if !ok {
		return fmt.Errorf("want $date marker, got %v", keys(m))
	}
	if _, ok := d.(string); !ok {
		return fmt.Errorf("want $date string, got %T", d)
	}
	return nil
}

// checkInt accepts integer-syntax JSON numbers within the wire width.
func checkInt(wire string, v any) error {
	n, ok := v.(json.Number)
	if !ok {
		return fmt.Errorf("want number, got %T", v)
	}
	i, err := n.Int64()
	if err != nil {
		return fmt.Errorf("want integer syntax: %v", err)
	}
	switch wire {
	case "int32":
		if i < -2147483648 || i > 2147483647 {
			return fmt.Errorf("%d overflows int32", i)
		}
	case "uint32":
		if i < 0 || i > 4294967295 {
			return fmt.Errorf("%d overflows uint32", i)
		}
	}
	return nil
}

// checkDecimal accepts a JSON number or a $numberDecimal ExtJSON marker.
func checkDecimal(v any) error {
	if _, ok := v.(json.Number); ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return fmt.Errorf("want number or $numberDecimal marker, got %T", v)
	}
	d, ok := m["$numberDecimal"]
	if !ok {
		return fmt.Errorf("want $numberDecimal marker, got %v", keys(m))
	}
	if _, ok := d.(string); !ok {
		return fmt.Errorf("want $numberDecimal string, got %T", d)
	}
	return nil
}

// checkBinary accepts a $binary ExtJSON marker object.
func checkBinary(v any) error {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return fmt.Errorf("want $binary marker, got %T", v)
	}
	b, ok := m["$binary"]
	if !ok {
		return fmt.Errorf("want $binary marker, got %v", keys(m))
	}
	inner, ok := b.(map[string]any)
	if !ok {
		return fmt.Errorf("want $binary object, got %T", b)
	}
	s, ok := inner["base64"].(string)
	if !ok || s == "" {
		return fmt.Errorf("want $binary.base64 string")
	}
	return nil
}

// sliceElem splits "[]Elem" into Elem.
func sliceElem(wire string) (string, bool) {
	if len(wire) > 2 && wire[:2] == "[]" {
		return wire[2:], true
	}
	return "", false
}

// mapValue splits "map[string]V" into V.
func mapValue(wire string) (string, bool) {
	const prefix = "map[string]"
	if len(wire) > len(prefix) && wire[:len(prefix)] == prefix {
		return wire[len(prefix):], true
	}
	return "", false
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
