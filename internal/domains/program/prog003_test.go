package program

import (
	"strings"
	"testing"
	"time"
)

func mustBind(t *testing.T, c *Catalog, rev Revision) Binding {
	t.Helper()
	b, err := c.BindProgram(testCaller, Binding{
		ProgramID:       rev.ProgramID,
		RevisionDigest:  rev.Digest,
		PopulationRef:   "pop-2026-q3",
		EligibilityRef:  "elig-2026-q3",
		CycleRef:        "cycle-2026-q3",
		PopulationCount: 1200, PopulationTotal: 1200,
		AsOf:         time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Completeness: CompletenessComplete,
		LateEntry:    LateEntryDeny,
	})
	if err != nil {
		t.Fatalf("BindProgram: %v", err)
	}
	return b
}

func TestTodo_PROGRAM_003(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	b := mustBind(t, c, rev)
	if b.Digest == "" {
		t.Fatal("binding carries no digest")
	}
	if b.Completeness != CompletenessComplete || b.LateEntry != LateEntryDeny {
		t.Fatal("binding hides completeness or late-entry policy")
	}
}

func TestTodo_PROGRAM_003_Property(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	// Partial population is bindable only with an explicit late-entry policy.
	partial, err := c.BindProgram(testCaller, Binding{
		ProgramID: rev.ProgramID, RevisionDigest: rev.Digest,
		PopulationRef: "pop-2026-q3", EligibilityRef: "elig-2026-q3",
		CycleRef:        "cycle-2026-q3",
		PopulationCount: 900, PopulationTotal: 1200,
		AsOf:         time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Completeness: CompletenessPartial, LateEntry: LateEntryAllowWithApproval,
	})
	if err != nil {
		t.Fatalf("explicit partial binding refused: %v", err)
	}
	if partial.Completeness != CompletenessPartial {
		t.Fatal("partial completeness not preserved")
	}
	// Partial population that claims COMPLETE is refused.
	_, err = c.BindProgram(testCaller, Binding{
		ProgramID: rev.ProgramID, RevisionDigest: rev.Digest,
		PopulationRef: "pop-2026-q3", EligibilityRef: "elig-2026-q3",
		CycleRef:        "cycle-2026-q3",
		PopulationCount: 900, PopulationTotal: 1200,
		AsOf:         time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Completeness: CompletenessComplete, LateEntry: LateEntryDeny,
	})
	if err == nil {
		t.Fatal("partial population bound as complete")
	}
}

func TestTodo_PROGRAM_003_Golden(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	b := mustBind(t, c, rev)
	if b.Digest != readGolden(t, "program_003.golden") {
		t.Fatalf("binding digest mismatch:\n got %q", b.Digest)
	}
}

func FuzzTodo_PROGRAM_003(f *testing.F) {
	f.Add([]byte("pop-2026-q3"), []byte("elig-2026-q3"), []byte("cycle-2026-q3"))
	f.Fuzz(func(t *testing.T, pop, elig, cycle []byte) {
		b := Binding{
			ProgramID: "p", RevisionDigest: "sha256:" + strings.Repeat("a", 64),
			PopulationRef: string(pop), EligibilityRef: string(elig),
			CycleRef:     string(cycle),
			AsOf:         time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			Completeness: CompletenessComplete, LateEntry: LateEntryDeny,
		}
		// Must never panic; empty refs must never validate.
		if err := ValidateBinding(b); err == nil &&
			(len(pop) == 0 || len(elig) == 0 || len(cycle) == 0) {
			t.Fatal("empty snapshot ref validated")
		}
	})
}

func TestTodo_PROGRAM_003_Security(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	_, err := c.BindProgram(rival, Binding{
		ProgramID: rev.ProgramID, RevisionDigest: rev.Digest,
		PopulationRef: "pop-2026-q3", EligibilityRef: "elig-2026-q3",
		CycleRef:     "cycle-2026-q3",
		AsOf:         time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Completeness: CompletenessComplete, LateEntry: LateEntryDeny,
	})
	if err == nil {
		t.Fatal("cross-tenant binding accepted")
	}
	_ = def
}

func TestTodo_PROGRAM_003_Mutation(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	// Mutant A: mismatched revision digest must be killed.
	_, err := c.BindProgram(testCaller, Binding{
		ProgramID: rev.ProgramID, RevisionDigest: "sha256:" + strings.Repeat("f", 64),
		PopulationRef: "pop-2026-q3", EligibilityRef: "elig-2026-q3",
		CycleRef:     "cycle-2026-q3",
		AsOf:         time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Completeness: CompletenessComplete, LateEntry: LateEntryDeny,
	})
	if err == nil {
		t.Fatal("mismatched-revision mutant survived")
	}
	// Mutant B: AsOf outside the revision interval must be killed.
	_, err = c.BindProgram(testCaller, Binding{
		ProgramID: rev.ProgramID, RevisionDigest: rev.Digest,
		PopulationRef: "pop-2026-q3", EligibilityRef: "elig-2026-q3",
		CycleRef:     "cycle-2026-q3",
		AsOf:         time.Date(2028, 7, 1, 0, 0, 0, 0, time.UTC),
		Completeness: CompletenessComplete, LateEntry: LateEntryDeny,
	})
	if err == nil {
		t.Fatal("out-of-interval mutant survived")
	}
	// Mutant C: partial population with DENY late-entry must be killed.
	_, err = c.BindProgram(testCaller, Binding{
		ProgramID: rev.ProgramID, RevisionDigest: rev.Digest,
		PopulationRef: "pop-2026-q3", EligibilityRef: "elig-2026-q3",
		CycleRef:        "cycle-2026-q3",
		PopulationCount: 900, PopulationTotal: 1200,
		AsOf:         time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Completeness: CompletenessPartial, LateEntry: LateEntryDeny,
	})
	if err == nil {
		t.Fatal("silent-partial mutant survived")
	}
}
