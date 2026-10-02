package chatgate

import (
	"encoding/json"
	"strings"
)

// Unknown additive definition fields survive read/edit/write round trips.
func (d *Definition) UnmarshalJSON(data []byte) error {
	type plain Definition
	var known plain
	if e := json.Unmarshal(data, &known); e != nil {
		return e
	}
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(data, &fields); e != nil {
		return e
	}
	for key := range fields {
		if reservedDefinitionField(key) {
			delete(fields, key)
		}
	}
	if known.Extensions == nil {
		known.Extensions = map[string]json.RawMessage{}
	}
	for k, v := range fields {
		known.Extensions[k] = v
	}
	*d = Definition(known)
	return nil
}
func (d Definition) MarshalJSON() ([]byte, error) {
	type plain Definition
	known := plain(d)
	known.Extensions = nil
	b, e := json.Marshal(known)
	if e != nil {
		return nil, e
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(b, &fields); e != nil {
		return nil, e
	}
	delete(fields, "Extensions")
	for k, v := range d.Extensions {
		if !reservedDefinitionField(k) {
			fields[k] = v
		}
	}
	return json.Marshal(fields)
}
func reservedDefinitionField(key string) bool {
	for _, known := range []string{"Version", "Digest", "Fields", "Mode", "Rules", "Purpose", "AnswerBy", "Extensions"} {
		if strings.EqualFold(key, known) {
			return true
		}
	}
	return false
}
