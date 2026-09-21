package transfer

// REV-099-01: the transfer-safeguard vocabulary compiles to named, versioned
// rules. Empty sets and unknown names are refused; adequacy decisions and
// SCC modules resolve; the selector region list matches the validator.
//
// RED: the inventory tracked TransferAssessmentRefs as free-form names while
// no Go code compiled them; nothing named adequacy or scc:module existed.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodo_REV_099_01(t *testing.T) {
	if _, err := Compile(nil); !errors.Is(err, ErrEmptySafeguards) {
		t.Fatalf("empty safeguards = %v, want ErrEmptySafeguards", err)
	}
	if _, err := Compile([]string{"adequacy:JP", "handshake"}); !errors.Is(err, ErrUnknownSafeguard) {
		t.Fatalf("unknown safeguard = %v, want ErrUnknownSafeguard", err)
	} else if !strings.Contains(err.Error(), `"handshake"`) {
		t.Fatalf("unknown safeguard error = %v, want the name quoted", err)
	}
	guards, err := Compile([]string{"adequacy:JP", "scc:module-two"})
	if err != nil {
		t.Fatalf("compile real guards: %v", err)
	}
	if len(guards) != 2 || guards[0].Basis != "adequacy" || guards[1].Basis != "scc" {
		t.Fatalf("compiled guards = %+v, want adequacy then scc", guards)
	}
	if guards[0].Reference == "" || guards[1].Reference == "" {
		t.Fatalf("compiled guards carry no instrument reference: %+v", guards)
	}
	regions := Regions()
	for _, want := range []string{"JP", "US", "GB"} {
		found := false
		for _, region := range regions {
			if region == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("regions = %v, want %s offered", regions, want)
		}
	}
	for _, region := range regions {
		if _, err := Compile([]string{"adequacy:" + region}); err != nil {
			t.Fatalf("offered region %s does not compile: %v", region, err)
		}
	}
}

func TestTodo_REV_099_01_Conformance(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "transfer_rules.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got, want := CanonicalRules(), strings.TrimSpace(string(raw)); got != want {
		t.Fatalf("ruleset drifted:\n got: %q\nwant: %q", got, want)
	}
	if RulesVersion == "" {
		t.Fatal("ruleset carries no version")
	}
}
