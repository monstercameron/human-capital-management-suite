package demoworkforce

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce/unitnames"
)

// The page's code-to-name table is the seed's: every seeded unit is named the
// same in both, and the table names no unit the seed does not have.
func TestS24_UnitNamesTableMatchesTheSeed(t *testing.T) {
	seeded := map[string]bool{}
	for _, pack := range Packs() {
		for _, unit := range pack.Company.Units {
			seeded[unit.Code] = true
			if got := unitnames.Name(unit.Code); got != unit.Name {
				t.Errorf("%s unit %q: table says %q, seed says %q", pack.Key, unit.Code, got, unit.Name)
			}
		}
	}
	for _, code := range unitnames.Codes() {
		if !seeded[code] {
			t.Errorf("the table names %q, which no seeded company has", code)
		}
	}
	if got := unitnames.Name("eng-platform"); got != "Engineering Platform" {
		t.Errorf("the older code eng-platform reads %q", got)
	}
	if got := unitnames.Name("no-such-unit"); got != "" {
		t.Errorf("an unknown code reads %q, want none", got)
	}
}
