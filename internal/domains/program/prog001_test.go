package program

import (
	"strings"
	"testing"
)

func TestTodo_PROGRAM_001(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	if def.Digest == "" {
		t.Fatal("defined program carries no digest")
	}
	for _, field := range []string{def.ID, def.Name, def.Owner, string(def.Type), string(def.Funding)} {
		if strings.TrimSpace(field) == "" {
			t.Fatal("definition omits type, owner, scope, funding, outcomes or extensions")
		}
	}
	if len(def.Scope) == 0 || len(def.Outcomes) == 0 {
		t.Fatal("definition omits scope or outcomes")
	}
	got, ok := c.Lookup("bonus-fy26")
	if !ok || got.Digest != def.Digest {
		t.Fatal("defined program is not retrievable by digest")
	}
}

func TestTodo_PROGRAM_001_Property(t *testing.T) {
	c := testCatalog(t)
	first := mustDefine(t, c)
	// Duplicate definition is refused and leaves the original untouched.
	_, err := c.Define(testCaller, Definition{
		ID: "bonus-fy26", Name: "duplicate", Type: ProgramBonus,
		Owner: "x", Scope: []string{"org:acme"}, Funding: FundingEmployer,
		Outcomes: []string{"payout"},
	})
	if err == nil {
		t.Fatal("duplicate definition accepted")
	}
	got, _ := c.Lookup("bonus-fy26")
	if got.Digest != first.Digest {
		t.Fatal("refused duplicate mutated the original")
	}
	// Every program type digests distinctly for otherwise identical input.
	seen := map[string]string{}
	for _, typ := range []ProgramType{ProgramBenefit, ProgramBonus, ProgramLearning, ProgramLeave} {
		d, err := c.Define(testCaller, Definition{
			ID: "prog-" + string(typ), Name: "n", Type: typ,
			Owner: "o", Scope: []string{"s"}, Funding: FundingEmployer,
			Outcomes: []string{"oc"},
		})
		if err != nil {
			t.Fatalf("Define %s: %v", typ, err)
		}
		for other, digest := range seen {
			if digest == d.Digest {
				t.Fatalf("type %s collides with %s", typ, other)
			}
		}
		seen[string(typ)] = d.Digest
	}
}

func TestTodo_PROGRAM_001_Golden(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	if def.Digest != readGolden(t, "program_001.golden") {
		t.Fatalf("definition digest mismatch:\n got %q", def.Digest)
	}
}

func TestTodo_PROGRAM_001_Conformance(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	// The shared oracle reseals the definition digest from its fields.
	if !VerifyDefinition(def) {
		t.Fatal("definition fails shared-oracle verification")
	}
	mut := def
	mut.Owner = "someone-else"
	if VerifyDefinition(mut) {
		t.Fatal("tampered definition verifies")
	}
}

func FuzzTodo_PROGRAM_001(f *testing.F) {
	f.Add([]byte("bonus-fy26"), []byte("total-rewards"), byte(1))
	f.Fuzz(func(t *testing.T, id, owner []byte, typ byte) {
		types := []ProgramType{ProgramBenefit, ProgramBonus, ProgramLearning, ProgramLeave, ProgramType("???")}
		def := Definition{
			ID: string(id), Name: "n", Type: types[int(typ)%len(types)],
			Owner: string(owner), Scope: []string{"s"}, Funding: FundingEmployer,
			Outcomes: []string{"oc"},
		}
		// Must never panic; invalid input must never validate.
		if err := ValidateDefinition(def); err == nil && (len(id) == 0 || len(owner) == 0) {
			t.Fatal("empty id or owner validated")
		}
	})
}

func TestTodo_PROGRAM_001_Security(t *testing.T) {
	c := testCatalog(t)
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	_, err := c.Define(rival, Definition{
		ID: "bonus-fy26", Name: "FY26 Annual Bonus", Type: ProgramBonus,
		Owner: "total-rewards", Scope: []string{"org:acme"}, Funding: FundingEmployer,
		Outcomes: []string{"payout"},
	})
	if err == nil {
		t.Fatal("cross-tenant definition accepted")
	}
	if strings.Contains(err.Error(), "tenant-acme") {
		t.Fatalf("denial leaks tenant existence: %v", err)
	}
	if len(c.Journal()) != 0 {
		t.Fatal("refused definition left journal effects")
	}
}

func TestTodo_PROGRAM_001_Mutation(t *testing.T) {
	// Mutant A: definition without a signed conformance gate must be killed.
	raw := NewCatalog()
	if _, err := raw.Define(testCaller, Definition{
		ID: "bonus-fy26", Name: "n", Type: ProgramBonus,
		Owner: "o", Scope: []string{"s"}, Funding: FundingEmployer,
		Outcomes: []string{"oc"},
	}); err == nil {
		t.Fatal("gate-bypass mutant survived")
	}
	// Mutant B: empty outcomes must be killed.
	c := testCatalog(t)
	if _, err := c.Define(testCaller, Definition{
		ID: "empty-out", Name: "n", Type: ProgramBonus,
		Owner: "o", Scope: []string{"s"}, Funding: FundingEmployer,
	}); err == nil {
		t.Fatal("empty-outcomes mutant survived")
	}
	// Mutant C: unknown program type must be killed.
	if _, err := c.Define(testCaller, Definition{
		ID: "weird", Name: "n", Type: ProgramType("ESOP-DAYS"),
		Owner: "o", Scope: []string{"s"}, Funding: FundingEmployer,
		Outcomes: []string{"oc"},
	}); err == nil {
		t.Fatal("unknown-type mutant survived")
	}
}
