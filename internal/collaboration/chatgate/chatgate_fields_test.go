package chatgate

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTodo_CHATGATE_003_Kinds(t *testing.T) {
	r := NewRegistry()
	for _, test := range []struct{ kind, valid, invalid string }{{"short_text", `"Hello"`, `42`}, {"long_text", `"Hello"`, `null`}, {"single_choice", `"A"`, `"forged"`}, {"multiple_choice", `["A"]`, `["A","A"]`}, {"boolean", `false`, `"false"`}, {"date", `"2026-10-01"`, `"2026-02-30"`}, {"person", `"person"`, `true`}, {"team", `"team"`, `null`}, {"location", `"Denver"`, `false`}, {"acknowledgement", `true`, `false`}} {
		t.Run(test.kind, func(t *testing.T) {
			k, ok := r.Kind(test.kind, "1.0.0")
			if !ok {
				t.Fatal(test.kind)
			}
			f := Field{Label: "Question", Required: true, Options: []string{"A", "B"}}
			if e := k.Validate(f, json.RawMessage(test.valid)); e != nil {
				t.Fatal(e)
			}
			if e := k.Validate(f, json.RawMessage(test.invalid)); !errors.Is(e, ErrInvalid) {
				t.Fatal(e)
			}
			for _, locale := range []string{"en-US", "de-DE", "ar"} {
				if control := k.Render(f, locale); control.Name == "" || control.Type != test.kind {
					t.Fatal(control)
				}
			}
		})
	}
}
func TestTodo_CHATGATE_003_Limits(t *testing.T) {
	r := NewRegistry()
	_, _, d := fixture(t, "review")
	for _, test := range []struct {
		name   string
		edit   func(*Definition)
		policy Policy
		want   error
	}{{"field limit", func(d *Definition) { d.Fields = make([]Field, 13) }, Policy{}, ErrInvalid}, {"option limit", func(d *Definition) { d.Fields[0].Options = make([]string, 21) }, Policy{}, ErrInvalid}, {"retention", func(d *Definition) { d.Fields[0].RetentionDays = 0 }, Policy{}, ErrInvalid}, {"tenant category", func(d *Definition) { d.Fields[0].Help = "What is your political affiliation?" }, Policy{Forbidden: []string{"political affiliation"}}, ErrDenied}, {"ceiling", func(d *Definition) { d.Fields[0].DataClass = "PII" }, Policy{}, ErrInvalid}, {"unsafe id", func(d *Definition) { d.Fields[0].ID = `field"><script>` }, Policy{}, ErrInvalid}, {"duplicate document", func(d *Definition) {
		f := d.Fields[0]
		f.Kind = "acknowledgement"
		f.Options = nil
		f.DocumentID = "policy"
		f.DocumentVersion = "2.1.0"
		d.Fields = []Field{f, f}
		d.Fields[1].ID = "second"
	}, Policy{}, ErrInvalid}} {
		t.Run(test.name, func(t *testing.T) {
			b, _ := json.Marshal(d)
			var next Definition
			_ = json.Unmarshal(b, &next)
			test.edit(&next)
			if e := r.ValidateDefinition(next, test.policy); !errors.Is(e, test.want) {
				t.Fatal(e)
			}
		})
	}
	k, _ := r.Kind("short_text", "1.0.0")
	long, _ := json.Marshal(strings.Repeat("x", 201))
	if e := k.Validate(Field{}, long); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_004_DirectorySecurity(t *testing.T) {
	s, c, d := fixture(t, "automatic")
	d.Fields[0].Kind = "person"
	d.Fields[0].Options = nil
	c.Key = "directory"
	if e := s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	c.Key = "directory-publish"
	if _, e := s.Publish(t.Context(), c, Version{2, 0, 0}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	if _, e := s.Submit(t.Context(), applicant(c, "forged-person"), "2.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"forged"`)}); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}
