package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func TestIssue3732_ProposedBaseRoundsUpToTheCent(t *testing.T) {
	for _, tc := range []struct{ current, increase, want string }{
		{"132000.00", "0.05", "138600.00"},
		{"100000.00", "0.0333", "103330.00"},
		{"100000.01", "0.0333", "103330.02"},
		{"68000.00", "", "68000.01"},
	} {
		got, ok := demoJourneyProposedBase(tc.current, tc.increase)
		if !ok || got != tc.want {
			t.Errorf("demoJourneyProposedBase(%q, %q) = %q, %v; want %q", tc.current, tc.increase, got, ok, tc.want)
		}
	}
	if _, ok := demoJourneyProposedBase("not a number", "0.05"); ok {
		t.Error("an unreadable current pay produced a proposal")
	}
}

func TestIssue3732_DemoJourneysRefuseAnyProfileButLocalDev(t *testing.T) {
	cfg := ServeConfig{DevDemoJourneys: true, Profile: ServeProfileStandard}
	if err := cfg.Validate(); err == nil {
		t.Fatal("a non-development profile accepted -dev-demo-journeys")
	}
}

func TestIssue3732_DemoJourneysDefaultOnlyUnderLocalDev(t *testing.T) {
	for _, tc := range []struct {
		profile string
		want    bool
	}{{ServeProfileLocalDev, true}, {ServeProfileStandard, false}} {
		values, err := bootstrap.ParseConfig(
			[]string{"-profile=" + tc.profile},
			func(string) (string, bool) { return "", false },
			ServeConfigFieldsForArgs([]string{"-profile=" + tc.profile}),
		)
		if err != nil {
			t.Fatalf("%s: %v", tc.profile, err)
		}
		got, err := values.Bool(FieldDevDemoJourneys)
		if err != nil || got != tc.want {
			t.Errorf("%s: -%s = %v, %v; want %v", tc.profile, FieldDevDemoJourneys, got, err, tc.want)
		}
	}
}
