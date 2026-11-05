package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
)

// AttemptVariables is one attempt's pinned view over a [VariableRevisionLog].
// It answers the VARIABLE question a compiled mapping asks: the value of a
// variable field as of the attempt's begin, never whatever a later write
// stored. The pin is the log head when the attempt's inputs resolved, so it
// derives from the durable history alone and a resumed attempt re-pins the
// same head; a revision written after the pin stays invisible no matter how
// much the log grows underneath.
//
// The zero value reads nothing: a path with no variable history serves every
// VARIABLE mapping as unresolved rather than inventing a value.
type AttemptVariables struct {
	log  *VariableRevisionLog
	head int64
}

// PinAttempt pins the log's current head as this attempt's variable horizon.
// A nil log pins an empty view.
func (l *VariableRevisionLog) PinAttempt() AttemptVariables {
	if l == nil {
		return AttemptVariables{}
	}
	return AttemptVariables{log: l, head: l.Head()}
}

// Head is the instance revision the attempt reads as of.
func (a AttemptVariables) Head() int64 { return a.head }

// Variable returns the canonical JSON text of path within the revision of
// name in force at the pinned head. Path descends dot-separated object
// fields ("band.min"); anything else -- an unwritten variable, a missing
// field, a descent into an array or scalar -- does not resolve.
func (a AttemptVariables) Variable(name, path string) (string, bool) {
	if a.log == nil {
		return "", false
	}
	rev, ok := a.log.AsOf(name, a.head)
	if !ok {
		return "", false
	}
	return variableFieldJSON(rev.Value, path)
}

// variableFieldJSON extracts one dot-separated object field from a revision
// value and renders it as canonical JSON: compact, with sorted object keys
// and numbers in their stored spelling.
func variableFieldJSON(value json.RawMessage, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	dec := json.NewDecoder(bytes.NewReader(value))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return "", false
	}
	current := decoded
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			return "", false
		}
		obj, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = obj[segment]
		if !ok {
			return "", false
		}
	}
	rendered, err := json.Marshal(current)
	if err != nil {
		return "", false
	}
	return string(rendered), true
}
