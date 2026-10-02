package chatlang

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_CHATLANG_006_Decide(t *testing.T) {
	on := Workspace{Enabled: true, ExternalAllowed: true}
	for name, tc := range map[string]struct {
		w        Workspace
		c        Channel
		target   string
		external bool
		spent    int64
		want     Reason
	}{
		"workspace off is the default":       {Workspace{}, Channel{}, "de", true, 0, ReasonWorkspaceOff},
		"a channel cannot beat the ceiling":  {Workspace{}, Channel{Translation: On}, "de", true, 0, ReasonWorkspaceOff},
		"on":                                 {on, Channel{}, "de", true, 0, Allowed},
		"channel off":                        {on, Channel{Translation: Off}, "de", true, 0, ReasonChannelOff},
		"language not offered":               {Workspace{Enabled: true, ExternalAllowed: true, Languages: []string{"fr"}}, Channel{}, "de", true, 0, ReasonLanguage},
		"language offered":                   {Workspace{Enabled: true, ExternalAllowed: true, Languages: []string{"de"}}, Channel{}, "de", true, 0, Allowed},
		"channel barred from external":       {on, Channel{External: ExternalBarred}, "de", true, 0, ReasonExternalBarred},
		"barred channel, in-deployment":      {on, Channel{External: ExternalBarred}, "de", false, 0, Allowed},
		"workspace bars external":            {Workspace{Enabled: true}, Channel{}, "de", true, 0, ReasonExternalBarred},
		"channel cannot widen the workspace": {Workspace{Enabled: true}, Channel{External: ExternalAllowed}, "de", true, 0, ReasonExternalBarred},
		"budget spent":                       {Workspace{Enabled: true, ExternalAllowed: true, BudgetMicros: 100}, Channel{}, "de", true, 100, ReasonBudget},
		"budget left":                        {Workspace{Enabled: true, ExternalAllowed: true, BudgetMicros: 100}, Channel{}, "de", true, 99, Allowed},
		"default budget":                     {on, Channel{}, "de", true, DefaultMonthlyBudgetMicros, ReasonBudget},
	} {
		if got := Decide(tc.w, tc.c, tc.target, tc.external, tc.spent); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestTodo_CHATLANG_006_Normalize(t *testing.T) {
	w, err := Workspace{Enabled: true, Languages: []string{"DE", "fr", "de"}, Formality: map[string]string{"DE": "Formal", "fr": ""}, BudgetMicros: 10}.Normalize()
	if err != nil || len(w.Languages) != 2 || w.Languages[0] != "de" || w.Formality["de"] != "formal" || len(w.Formality) != 1 {
		t.Fatal(w, err)
	}
	for _, bad := range []Workspace{{Languages: []string{"xx"}}, {BudgetMicros: -1}, {Formality: map[string]string{"de": "casual"}}, {Formality: map[string]string{"xx": "formal"}}} {
		if _, err := bad.Normalize(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted", bad)
		}
	}
	c, err := Channel{}.Normalize()
	if err != nil || c.Translation != Inherit || c.External != ExternalInherit {
		t.Fatal(c, err)
	}
	if _, err := (Channel{Translation: "maybe"}).Normalize(); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad switch")
	}
	if _, err := (Channel{External: "sometimes"}).Normalize(); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad external")
	}
	for term, want := range map[Term]bool{
		{Source: "Acme"}: true,
		{Source: "time off", Language: "de", Target: "Urlaub"}: true,
		{Source: ""}:                               false,
		{Source: "x", Language: "de"}:              false,
		{Source: "x", Target: "y"}:                 false,
		{Source: "x", Language: "xx", Target: "y"}: false,
		{Source: "a⟦b"}:                            false,
	} {
		if ValidTerm(term) != want {
			t.Errorf("%+v valid=%v", term, !want)
		}
	}
	from, to := Month(time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC))
	if from != time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) || to != time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatal(from, to)
	}
	if len(SupportedLanguages()) != 8 || !(Workspace{}).Offers("ja") || (Workspace{}).Budget() != DefaultMonthlyBudgetMicros {
		t.Fatal("defaults")
	}
}
