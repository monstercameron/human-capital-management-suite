package semver

import (
	"math/rand"
	"testing"

	"golang.org/x/mod/semver"
)

// TestTodo_AGENTUX_065_Property: ordering and compatibility follow the
// semantic versioning rules for every generated pair, checked against the
// canonical implementation as oracle.
func TestTodo_AGENTUX_065_Property(t *testing.T) {
	random := rand.New(rand.NewSource(65065))
	versions := make([]Version, 0, 256)
	for i := 0; i < 256; i++ {
		versions = append(versions, Version{Major: 1 + uint32(random.Intn(9)), Minor: uint32(random.Intn(20)), Patch: uint32(random.Intn(20))})
	}
	oracle := func(v Version) string { return "v" + v.String() }
	for i, left := range versions {
		if got := left.String(); got == "" {
			t.Fatal("an empty rendering")
		} else if round, err := Parse(got); err != nil || round != left {
			t.Fatalf("Parse(%q) = %v, %v", got, round, err)
		}
		for _, right := range versions[i:] {
			want := semver.Compare(oracle(left), oracle(right))
			if got := left.Compare(right); got != want {
				t.Fatalf("Compare(%v, %v) = %d, want %d", left, right, got, want)
			}
			if left.Less(right) != (want < 0) || right.Less(left) != (want > 0) {
				t.Fatalf("Less disagrees with Compare for %v and %v", left, right)
			}
			compatible := left.Major == right.Major && semver.Compare(oracle(left), oracle(right)) >= 0
			if left.SatisfiesCaret(right) != compatible {
				t.Fatalf("%v.SatisfiesCaret(%v) disagrees with the caret rule", left, right)
			}
		}
	}
	// Transitivity over generated triples.
	for i := 0; i < 512; i++ {
		a, b, c := versions[random.Intn(len(versions))], versions[random.Intn(len(versions))], versions[random.Intn(len(versions))]
		if a.Compare(b) <= 0 && b.Compare(c) <= 0 && a.Compare(c) > 0 {
			t.Fatalf("ordering is not transitive: %v <= %v <= %v", a, b, c)
		}
	}
}

func TestSemverParse(t *testing.T) {
	for input, want := range map[string]Version{"1.0.0": {1, 0, 0}, "v2.4.1": {2, 4, 1}, "10.20.30": {10, 20, 30}} {
		if got, err := Parse(input); err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v", input, got, err)
		}
	}
	for _, input := range []string{"", "1", "1.2", "1.2.3.4", "0.1.0", "01.2.3", "1.2.3-preview", "1.2.3+build", "v", "version 3", "private-version-id", "1.2.x", "4294967296.0.0"} {
		if got, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) = %v, want an error", input, got)
		}
	}
}

func TestSemverNext(t *testing.T) {
	base := Version{1, 2, 3}
	if got := base.Next(BumpPatch); got != (Version{1, 2, 4}) {
		t.Errorf("patch bump of %v = %v", base, got)
	}
	if got := base.Next(BumpMinor); got != (Version{1, 3, 0}) {
		t.Errorf("minor bump of %v = %v", base, got)
	}
	if got := base.Next(BumpMajor); got != (Version{2, 0, 0}) {
		t.Errorf("major bump of %v = %v", base, got)
	}
	if got := FirstVersion().String(); got != "1.0.0" {
		t.Errorf("first version = %q", got)
	}
	var zero Version
	if got := zero.Next(BumpMinor); got != (Version{1, 0, 0}) {
		t.Errorf("bump from nothing = %v, want the first version", got)
	}
}

func TestSemverDisplay(t *testing.T) {
	if got := (Version{1, 2, 0}).Display(); got != "v1.2.0" {
		t.Errorf("display = %q", got)
	}
	if got := DisplaySequence(6); got != "v6.0.0" {
		t.Errorf("legacy sequence display = %q", got)
	}
}
