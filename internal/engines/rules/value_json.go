package rules

import (
	"encoding/json"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// MarshalJSON gives the rules engine's otherwise private value representation
// a lossless wire form for persisted decision tables and compiled expressions.
func (v Value) MarshalJSON() ([]byte, error) {
	if v.kind == KindUnspecified {
		return []byte(`{"kind":""}`), nil
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	type wireValue struct {
		Kind  string  `json:"kind"`
		Text  string  `json:"text,omitempty"`
		Scale int32   `json:"scale,omitempty"`
		Bool  *bool   `json:"bool,omitempty"`
		Int   *int64  `json:"int,omitempty"`
		Items []Value `json:"items,omitempty"`
	}
	w := wireValue{Kind: v.kind.String()}
	switch v.kind {
	case KindDecimal:
		w.Text, w.Scale = v.dec.String(), v.dec.Scale()
	case KindString:
		w.Text = v.str
	case KindBool:
		b := v.b
		w.Bool = &b
	case KindInt:
		i := v.i
		w.Int = &i
	case KindList:
		w.Items, _ = v.List()
	}
	return json.Marshal(w)
}

// UnmarshalJSON validates kind/value agreement and preserves decimal scale.
func (v *Value) UnmarshalJSON(data []byte) error {
	if v == nil {
		return fmt.Errorf("rules: cannot decode value into nil receiver")
	}
	var w struct {
		Kind  string  `json:"kind"`
		Text  string  `json:"text"`
		Scale int32   `json:"scale"`
		Bool  *bool   `json:"bool"`
		Int   *int64  `json:"int"`
		Items []Value `json:"items"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	if w.Kind == "" {
		*v = Value{}
		return nil
	}
	var next Value
	switch w.Kind {
	case "DECIMAL":
		d, err := values.NewDecimal(w.Text, w.Scale, values.RoundingExactRequired)
		if err != nil {
			return err
		}
		next = DecimalValue(d)
	case "STRING":
		next = StringValue(w.Text)
	case "BOOL":
		if w.Bool == nil {
			return fmt.Errorf("rules: BOOL value is absent")
		}
		next = BoolValue(*w.Bool)
	case "INT":
		if w.Int == nil {
			return fmt.Errorf("rules: INT value is absent")
		}
		next = IntValue(*w.Int)
	case "LIST":
		next = ListValue(w.Items...)
	default:
		return fmt.Errorf("rules: unknown value kind %q", w.Kind)
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
