package productquery_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_ALIGN_020 proves the semantic absence mapping is total and
// closed: every repository cell state maps to exactly one response state,
// denied and withheld rulings omit uniformly, and a non-present cell
// carrying a value is refused rather than laundered.
func TestTodo_ALIGN_020(t *testing.T) {
	cases := []struct {
		name     string
		cell     productquery.Cell
		effect   authz.Effect
		included bool
		state    productquery.ValueState
		value    string
	}{
		{name: "present", cell: productquery.Cell{State: productquery.ValuePresent, Value: "W-1"}, effect: authz.EffectAllow, included: true, state: productquery.ValuePresent, value: "W-1"},
		{name: "absent", cell: productquery.Cell{State: productquery.ValueAbsent}, effect: authz.EffectAllow, included: true, state: productquery.ValueAbsent},
		{name: "unknown", cell: productquery.Cell{}, effect: authz.EffectAllow, included: true, state: productquery.ValueUnknown},
		{name: "unavailable", cell: productquery.Cell{State: productquery.ValueUnavailable}, effect: authz.EffectAllow, included: true, state: productquery.ValueUnavailable},
		{name: "redacted", cell: productquery.Cell{State: productquery.ValuePresent, Value: "125000.00"}, effect: authz.EffectRedacted, included: true, state: productquery.ValueRedacted},
		{name: "denied with value", cell: productquery.Cell{State: productquery.ValuePresent, Value: "W-1"}, effect: authz.EffectDenied, included: false},
		{name: "withheld without value", cell: productquery.Cell{State: productquery.ValueAbsent}, effect: authz.EffectWithheld, included: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field, included, err := productquery.MapAbsence(tc.cell, tc.effect)
			if err != nil {
				t.Fatalf("MapAbsence: %v", err)
			}
			if included != tc.included {
				t.Fatalf("included=%v want %v", included, tc.included)
			}
			if !included {
				return
			}
			if field.State != tc.state || field.Value != tc.value || field.Disposition != tc.effect {
				t.Fatalf("mapped=%+v want state=%s value=%q disposition=%s", field, tc.state, tc.value, tc.effect)
			}
		})
	}
}

func TestTodo_ALIGN_020_Property(t *testing.T) {
	states := []productquery.ValueState{
		productquery.ValuePresent, productquery.ValueAbsent,
		productquery.ValueUnknown, productquery.ValueUnavailable, productquery.ValueRedacted,
	}
	for _, state := range states {
		cell := productquery.Cell{State: state}
		value := ""
		if state == productquery.ValuePresent {
			cell.Value = "v"
			value = "v"
		}
		first, included, err := productquery.MapAbsence(cell, authz.EffectAllow)
		if err != nil {
			t.Fatalf("MapAbsence(%s): %v", state, err)
		}
		if !included || first.State != state || first.Value != value {
			t.Fatalf("MapAbsence(%s) = %+v, want the state preserved", state, first)
		}
		second, _, err := productquery.MapAbsence(cell, authz.EffectAllow)
		if err != nil || second != first {
			t.Fatalf("mapping is not deterministic for %s", state)
		}
	}
}

func TestTodo_ALIGN_020_Golden(t *testing.T) {
	env := freshEnvelope(t)
	summary, err := productquery.SummarizeAbsence(env)
	if err != nil {
		t.Fatalf("SummarizeAbsence: %v", err)
	}
	const wantDigest = "09baf96fb872b4be66b93962794f2659816b9d19941da1c85e9c94ea62e14c39"
	if summary.Digest != wantDigest {
		t.Fatalf("absence summary digest=%q want=%q", summary.Digest, wantDigest)
	}
	if summary.Subjects != len(env.Rows) {
		t.Fatalf("summary subjects=%d rows=%d", summary.Subjects, len(env.Rows))
	}
}

func TestTodo_ALIGN_020_Security(t *testing.T) {
	// A non-present cell smuggling a value is refused with a typed error.
	for _, state := range []productquery.ValueState{
		productquery.ValueAbsent, productquery.ValueUnknown,
		productquery.ValueUnavailable, productquery.ValueRedacted,
	} {
		if _, _, err := productquery.MapAbsence(productquery.Cell{State: state, Value: "smuggled"}, authz.EffectAllow); !errors.Is(err, productquery.ErrAbsenceInvalid) {
			t.Fatalf("MapAbsence(%s with value) = %v, want ErrAbsenceInvalid", state, err)
		}
	}
	// A ruling outside the closed effect set is refused, never dropped
	// silently into an ambiguous absence.
	if _, _, err := productquery.MapAbsence(productquery.Cell{State: productquery.ValuePresent, Value: "W-1"}, authz.Effect(99)); !errors.Is(err, productquery.ErrAbsenceInvalid) {
		t.Fatalf("MapAbsence(bogus effect) = %v, want ErrAbsenceInvalid", err)
	}
	// A tampered envelope (redacted field carrying a value) cannot be
	// summarized.
	env := freshEnvelope(t)
	if len(env.Rows) == 0 || len(env.Rows[0].Fields) == 0 {
		t.Skip("no response fields to tamper with")
	}
	tampered := env
	tampered.Rows = append([]productquery.Row(nil), env.Rows...)
	tampered.Rows[0].Fields = append([]productquery.Field(nil), env.Rows[0].Fields...)
	tampered.Rows[0].Fields[0].Disposition = authz.EffectRedacted
	tampered.Rows[0].Fields[0].Value = "leaked"
	if _, err := productquery.SummarizeAbsence(tampered); err == nil {
		t.Fatal("tampered envelope was summarized")
	}
}

func TestTodo_ALIGN_020_Conformance(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	candidate := allowedCandidate("00000000-0000-4000-8000-000000000001")
	candidate.Fields[authz.FieldWorkerNumber] = productquery.Cell{State: productquery.ValueAbsent}
	env, err := productquery.Project(request(p, []productquery.Candidate{candidate}))
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	// Every response field equals the governed mapping of its source cell
	// under its response disposition: the projection implements the
	// vocabulary, it does not improvise.
	for _, row := range env.Rows {
		for _, field := range row.Fields {
			source, ok := candidate.Fields[field.ID]
			if !ok {
				t.Fatalf("response field %q has no source cell", field.ID)
			}
			mapped, included, err := productquery.MapAbsence(source, field.Disposition)
			if err != nil || !included {
				t.Fatalf("response field %q is not mappable: %+v %v", field.ID, mapped, err)
			}
			if mapped.State != field.State || mapped.Value != field.Value {
				t.Fatalf("response field %+v != mapped %+v", field, mapped)
			}
		}
	}
	summary, err := productquery.SummarizeAbsence(env)
	if err != nil {
		t.Fatalf("SummarizeAbsence: %v", err)
	}
	total := 0
	for _, row := range env.Rows {
		total += len(row.Fields)
	}
	counted := 0
	for _, n := range summary.Counts {
		counted += n
	}
	if counted != total {
		t.Fatalf("summary counted %d of %d response fields", counted, total)
	}
	for _, row := range env.Rows {
		for _, field := range row.Fields {
			if field.ID == authz.FieldWorkerNumber && field.Disposition == authz.EffectAllow && field.State != productquery.ValueAbsent {
				t.Fatalf("absent source projected as %+v", field)
			}
		}
	}
	if got := productquery.ExplainAbsence(); got == "" {
		t.Fatal("absence mapping has no explanation")
	}
}

func FuzzTodo_ALIGN_020_Fuzz(f *testing.F) {
	f.Add(uint8(0), "v", uint8(0))
	f.Fuzz(func(t *testing.T, stateByte uint8, value string, effectByte uint8) {
		states := []productquery.ValueState{
			productquery.ValuePresent, productquery.ValueAbsent,
			productquery.ValueUnknown, productquery.ValueUnavailable, productquery.ValueRedacted,
		}
		effects := []authz.Effect{
			authz.EffectAllow, authz.EffectRedacted,
			authz.EffectDenied, authz.EffectWithheld, authz.Effect(99),
		}
		cell := productquery.Cell{State: states[stateByte%uint8(len(states))], Value: value}
		field, included, err := productquery.MapAbsence(cell, effects[effectByte%uint8(len(effects))])
		if err != nil {
			return
		}
		if !included {
			return
		}
		if field.State != productquery.ValuePresent && field.Value != "" {
			t.Fatalf("non-present mapped field carries a value: %+v", field)
		}
	})
}
