package chatgate

import (
	"encoding/json"
	"maps"
	"strings"
	"time"
	"unicode/utf8"
)

type Kind struct {
	ID, Version, Schema, DataClass string
	Names                          map[string]string
	Validate                       func(Field, json.RawMessage) error
	Render                         func(Field, string) Control
}

// Control is a portable renderer contract. Extension kinds choose a supported
// accessible control without coupling the gate evaluator to a browser library.
type Control struct {
	Type, Name string
	Options    []string
}
type Registry struct {
	kinds     map[string]Kind
	consumers map[string]Consumer
}

func NewRegistry() *Registry {
	r := &Registry{kinds: map[string]Kind{}, consumers: map[string]Consumer{}}
	labels := [][]string{{"short_text", "Short text", "Kurzer Text", "نص قصير"}, {"long_text", "Long text", "Langer Text", "نص طويل"}, {"single_choice", "Choose one", "Eine Option wählen", "اختر واحدًا"}, {"multiple_choice", "Choose several", "Mehrere Optionen wählen", "اختر عدة خيارات"}, {"boolean", "Yes or no", "Ja oder nein", "نعم أو لا"}, {"date", "Date", "Datum", "التاريخ"}, {"person", "Person", "Person", "شخص"}, {"team", "Team", "Team", "فريق"}, {"location", "Location", "Standort", "الموقع"}, {"acknowledgement", "Acknowledge document", "Dokument bestätigen", "تأكيد المستند"}}
	for _, x := range labels {
		id := x[0]
		schema := `{"type":"string"}`
		if id == "boolean" || id == "acknowledgement" {
			schema = `{"type":"boolean"}`
		}
		if id == "multiple_choice" {
			schema = `{"type":"array","items":{"type":"string"}}`
		}
		names := map[string]string{"en-US": x[1], "de-DE": x[2], "ar": x[3]}
		_ = r.RegisterKind(Kind{ID: id, Version: "1.0.0", Schema: schema, DataClass: "INTERNAL", Names: names, Validate: func(f Field, v json.RawMessage) error { return validateBuiltin(id, f, v) }, Render: func(f Field, locale string) Control {
			return Control{Type: id, Name: names[locale], Options: f.Options}
		}})
	}
	return r
}
func (r *Registry) RegisterKind(k Kind) error {
	if k.ID == "" || k.Validate == nil || k.Render == nil || !json.Valid([]byte(k.Schema)) || k.DataClass == "" {
		return ErrInvalid
	}
	if _, e := ParseVersion(k.Version); e != nil {
		return e
	}
	for _, l := range []string{"en-US", "de-DE", "ar"} {
		if k.Names[l] == "" {
			return ErrInvalid
		}
	}
	key := k.ID + "@" + k.Version
	if _, ok := r.kinds[key]; ok {
		return ErrConflict
	}
	k.Names = maps.Clone(k.Names)
	r.kinds[key] = k
	return nil
}
func (r *Registry) Kind(id, version string) (Kind, bool) {
	k, ok := r.kinds[id+"@"+version]
	k.Names = maps.Clone(k.Names)
	return k, ok
}
func (r *Registry) RegisterConsumer(c Consumer) error {
	if c.ID == "" || c.Permission == "" || c.Effect == "" || (len(c.Fields) == 0 && len(c.Kinds) == 0) {
		return ErrInvalid
	}
	if _, e := ParseVersion(c.Version); e != nil {
		return e
	}
	key := c.ID + "@" + c.Version
	if _, ok := r.consumers[key]; ok {
		return ErrConflict
	}
	c.Fields = append([]string(nil), c.Fields...)
	c.Kinds = append([]string(nil), c.Kinds...)
	c.Names = maps.Clone(c.Names)
	r.consumers[key] = c
	return nil
}
func (r *Registry) Consumer(id, version string) (Consumer, bool) {
	c, ok := r.consumers[id+"@"+version]
	c.Fields = append([]string(nil), c.Fields...)
	c.Kinds = append([]string(nil), c.Kinds...)
	c.Names = maps.Clone(c.Names)
	return c, ok
}
func (r *Registry) ConsumerLabels(locale string) map[string]string {
	out := map[string]string{}
	for _, consumer := range r.consumers {
		label := consumer.Names[locale]
		if label == "" {
			label = consumer.Effect
		}
		out[consumer.ID] = label
	}
	return out
}

type Policy struct {
	Forbidden []string
	Ceiling   string
	Private   bool
}

func (r *Registry) ValidateDefinition(d Definition, p Policy) error {
	if len(d.Fields) == 0 || len(d.Fields) > 12 || len(d.Rules) > 20 || strings.TrimSpace(d.Purpose) == "" || !contains([]string{"automatic", "rule", "review"}, d.Mode) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	docs := map[string]bool{}
	forbidden := append([]string{"password", "credential", "secret", "api key", "access token", "private key", "passport", "national id", "tax id", "ssn", "government identifier", "social security", "health", "medical", "diagnosis", "disability", "personalausweis", "sozialversicherungsnummer", "zugangstoken", "رقم الهوية", "رمز الوصول", "مفتاح خاص", "passwort", "kennwort", "gesundheit", "كلمة المرور", "الصحة"}, p.Forbidden...)
	for _, f := range d.Fields {
		k, ok := r.Kind(f.Kind, f.KindVersion)
		if !ok || f.ID == "" || seen[f.ID] || f.Label == "" || f.Purpose == "" || f.RetentionDays < 1 || f.RetentionDays > 3650 || len(f.Options) > 20 || !allowedClass(f.DataClass, p.Ceiling) || !allowedClass(k.DataClass, p.Ceiling) {
			return ErrInvalid
		}
		seen[f.ID] = true
		if len(f.ID) > 80 || len(f.Label) > 240 || len(f.Help) > 1000 || len(f.Purpose) > 1000 {
			return ErrInvalid
		}
		for _, ch := range f.ID {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
				return ErrInvalid
			}
		}
		if !f.Visibility.Administrators && !f.Visibility.Members && len(f.Visibility.Consumers) == 0 {
			return ErrInvalid
		}
		text := strings.ToLower(f.Label + " " + f.Help)
		for _, word := range forbidden {
			if strings.TrimSpace(word) != "" && strings.Contains(text, strings.ToLower(word)) {
				return ErrDenied
			}
		}
		opts := map[string]bool{}
		for _, o := range f.Options {
			if o == "" || len(o) > 240 || opts[o] {
				return ErrInvalid
			}
			opts[o] = true
		}
		if (f.Kind == "single_choice" || f.Kind == "multiple_choice") && len(opts) == 0 {
			return ErrInvalid
		}
		if f.Kind == "acknowledgement" {
			if f.DocumentID == "" || f.DocumentVersion == "" || docs[f.DocumentID] {
				return ErrInvalid
			}
			docs[f.DocumentID] = true
		}
	}
	for _, rule := range d.Rules {
		if bytes, e := json.Marshal(rule.When); e != nil || len(bytes) > 16384 {
			return ErrInvalid
		}
		if !contains([]string{"admitted", "declined", "review"}, rule.Outcome) || rule.Reason == "" {
			return ErrInvalid
		}
		if e := validateExpression(rule.When, seen, 0); e != nil {
			return e
		}
	}
	return nil
}
func allowedClass(class, ceiling string) bool {
	ranks := map[string]int{"PUBLIC": 1, "INTERNAL": 2, "PII": 3}
	c, ok := ranks[class]
	if ceiling == "" {
		ceiling = "INTERNAL"
	}
	max := ranks[ceiling]
	return ok && c <= max
}
func validateBuiltin(id string, f Field, v json.RawMessage) error {
	if !json.Valid(v) {
		return ErrInvalid
	}
	if id == "boolean" || id == "acknowledgement" {
		var b bool
		if json.Unmarshal(v, &b) != nil || string(v) == "null" || (id == "acknowledgement" && !b) {
			return ErrInvalid
		}
		return nil
	}
	if id == "multiple_choice" {
		var xs []string
		if json.Unmarshal(v, &xs) != nil || len(xs) > 20 {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, x := range xs {
			if !contains(f.Options, x) || seen[x] {
				return ErrInvalid
			}
			seen[x] = true
		}
		if f.Required && len(xs) == 0 {
			return ErrInvalid
		}
		return nil
	}
	var s string
	if json.Unmarshal(v, &s) != nil || string(v) == "null" || !utf8.ValidString(s) {
		return ErrInvalid
	}
	limit := 200
	if id == "long_text" {
		limit = 500
	}
	if utf8.RuneCountInString(s) > limit || (f.Required && strings.TrimSpace(s) == "") {
		return ErrInvalid
	}
	if id == "single_choice" && !contains(f.Options, s) {
		return ErrInvalid
	}
	if id == "date" {
		if _, e := time.Parse("2006-01-02", s); e != nil {
			return ErrInvalid
		}
	}
	return nil
}
func validateExpression(e Expression, fields map[string]bool, depth int) error {
	if depth > 12 {
		return ErrInvalid
	}
	switch e.Operator {
	case "equals", "in":
		if e.Operator == "equals" && len(e.Values) != 1 {
			return ErrInvalid
		}
		if (e.Field == "" && e.Fact == "") || (e.Field != "" && e.Fact != "") || (e.Field != "" && !fields[e.Field]) || len(e.Values) == 0 || len(e.Values) > 20 || len(e.Children) != 0 {
			return ErrInvalid
		}
		if e.Fact != "" && !contains([]string{"team", "location", "person"}, e.Fact) {
			return ErrInvalid
		}
	case "and", "or", "not":
		if e.Field != "" || e.Fact != "" || len(e.Values) > 0 || len(e.Children) == 0 || len(e.Children) > 12 || (e.Operator == "not" && len(e.Children) != 1) {
			return ErrInvalid
		}
		for _, c := range e.Children {
			if err := validateExpression(c, fields, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	return nil
}
func evaluate(e Expression, answers map[string]json.RawMessage, facts map[string]string) bool {
	switch e.Operator {
	case "equals", "in":
		value := facts[e.Fact]
		if e.Field != "" {
			if err := json.Unmarshal(answers[e.Field], &value); err != nil {
				raw := string(answers[e.Field])
				if raw == "true" || raw == "false" {
					value = raw
				}
			}
		}
		return contains(e.Values, value)
	case "not":
		return !evaluate(e.Children[0], answers, facts)
	case "and":
		for _, c := range e.Children {
			if !evaluate(c, answers, facts) {
				return false
			}
		}
		return true
	case "or":
		for _, c := range e.Children {
			if evaluate(c, answers, facts) {
				return true
			}
		}
	}
	return false
}
