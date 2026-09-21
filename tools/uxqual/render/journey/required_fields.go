package journey

import "strings"

// MissingRequired returns, in form order, every Required field whose
// submitted value is blank. submitted is keyed by Field.Name, the way a form
// submission is. Hidden fields carry no reader input and are never reported.
// This is the one client-side required rule: it reads Field.Required rather
// than keeping a second per-form list of which fields must be filled.
func MissingRequired(fields []Field, submitted map[string]string) []Field {
	missing := make([]Field, 0, len(fields))
	for _, field := range fields {
		if !field.Required || field.Kind == fieldKindHidden || field.Name == "" {
			continue
		}
		if strings.TrimSpace(submitted[field.Name]) == "" {
			missing = append(missing, field)
		}
	}
	return missing
}

// WithFieldErrors returns a copy of fields whose Error is set from errs,
// keyed by Field.ID. A field with no entry keeps its own Error. The renderer
// turns an Error into aria-invalid and an inline message linked through
// aria-describedby.
func WithFieldErrors(fields []Field, errs map[string]string) []Field {
	if len(errs) == 0 {
		return fields
	}
	out := append([]Field(nil), fields...)
	for i := range out {
		if message := strings.TrimSpace(errs[out[i].ID]); message != "" {
			out[i].Error = message
		}
	}
	return out
}
