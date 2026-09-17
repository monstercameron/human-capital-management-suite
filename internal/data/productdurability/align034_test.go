package productdurability

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_ALIGN_034 proves effective intervals are half-open: touching
// spans are accepted, overlapping spans are refused, and every instant
// resolves to at most one effective fact.
func TestTodo_ALIGN_034(t *testing.T) {
	set := NewIntervalSet()
	base := durabilityBase()
	first := Interval{Start: base, End: base.Add(time.Hour)}
	second := Interval{Start: base.Add(time.Hour), End: base.Add(2 * time.Hour)}
	if err := set.Add(durabilityTenant, "assignment", first); err != nil {
		t.Fatalf("Add(first): %v", err)
	}
	// A touching successor ends exactly where the next begins: accepted.
	if err := set.Add(durabilityTenant, "assignment", second); err != nil {
		t.Fatalf("Add(touching): %v", err)
	}
	got, ok := set.EffectiveAt(durabilityTenant, "assignment", base.Add(30*time.Minute))
	if !ok || !got.Start.Equal(first.Start) {
		t.Fatalf("EffectiveAt(mid-first) = %+v, %v", got, ok)
	}
	got, ok = set.EffectiveAt(durabilityTenant, "assignment", base.Add(time.Hour))
	if !ok || !got.Start.Equal(second.Start) {
		t.Fatalf("EffectiveAt(boundary) = %+v, %v, want the successor", got, ok)
	}
	if _, ok := set.EffectiveAt(durabilityTenant, "assignment", base.Add(-time.Second)); ok {
		t.Fatal("instant before all spans resolved to a fact")
	}
}

func TestTodo_ALIGN_034_Property(t *testing.T) {
	base := durabilityBase()
	cases := []struct {
		name    string
		spans   []Interval
		overlap bool
	}{
		{name: "contained", spans: []Interval{{Start: base, End: base.Add(2 * time.Hour)}, {Start: base.Add(30 * time.Minute), End: base.Add(time.Hour)}}, overlap: true},
		{name: "partial", spans: []Interval{{Start: base, End: base.Add(time.Hour)}, {Start: base.Add(30 * time.Minute), End: base.Add(2 * time.Hour)}}, overlap: true},
		{name: "identical", spans: []Interval{{Start: base, End: base.Add(time.Hour)}, {Start: base, End: base.Add(time.Hour)}}, overlap: true},
		{name: "open swallows", spans: []Interval{{Start: base}, {Start: base.Add(time.Hour), End: base.Add(2 * time.Hour)}}, overlap: true},
		{name: "touching", spans: []Interval{{Start: base, End: base.Add(time.Hour)}, {Start: base.Add(time.Hour), End: base.Add(2 * time.Hour)}}, overlap: false},
		{name: "disjoint", spans: []Interval{{Start: base, End: base.Add(time.Hour)}, {Start: base.Add(2 * time.Hour), End: base.Add(3 * time.Hour)}}, overlap: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := NewIntervalSet()
			if err := set.Add(durabilityTenant, "k", tc.spans[0]); err != nil {
				t.Fatalf("Add(first): %v", err)
			}
			err := set.Add(durabilityTenant, "k", tc.spans[1])
			if tc.overlap && !errors.Is(err, ErrIntervalOverlap) {
				t.Fatalf("Add(%s) = %v, want ErrIntervalOverlap", tc.name, err)
			}
			if !tc.overlap && err != nil {
				t.Fatalf("Add(%s) = %v, want success", tc.name, err)
			}
		})
	}
}

func TestTodo_ALIGN_034_Golden(t *testing.T) {
	set := NewIntervalSet()
	base := durabilityBase()
	if err := set.Add(durabilityTenant, "assignment", Interval{Start: base, End: base.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := set.Add(durabilityTenant, "assignment", Interval{Start: base.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:0cc8d7892e6ce3fc3530095122f12e4d469f4990a2d885109d9ff99b27a35539"
	if got := set.Digest(); got != wantDigest {
		t.Fatalf("interval digest=%q want=%q", got, wantDigest)
	}
}

func TestTodo_ALIGN_034_Security(t *testing.T) {
	set := NewIntervalSet()
	base := durabilityBase()
	// Another tenant's key is a different history.
	if err := set.Add(durabilityTenant, "assignment", Interval{Start: base, End: base.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := set.EffectiveAt(values.TenantId("vendor"), "assignment", base.Add(30*time.Minute)); ok {
		t.Fatal("foreign tenant resolved a fact")
	}
	// Degenerate spans are refused before any overlap check.
	for _, span := range []Interval{
		{},
		{Start: base, End: base},
		{Start: base, End: base.Add(-time.Hour)},
	} {
		if err := set.Add(durabilityTenant, "bad", span); !errors.Is(err, ErrIntervalInvalid) {
			t.Fatalf("Add(%+v) = %v, want ErrIntervalInvalid", span, err)
		}
	}
}

func TestTodo_ALIGN_034_Conformance(t *testing.T) {
	set := NewIntervalSet()
	base := durabilityBase()
	// An open-ended fact holds for all future time.
	if err := set.Add(durabilityTenant, "employment", Interval{Start: base}); err != nil {
		t.Fatal(err)
	}
	far := base.Add(100 * 365 * 24 * time.Hour)
	got, ok := set.EffectiveAt(durabilityTenant, "employment", far)
	if !ok || !got.End.IsZero() {
		t.Fatalf("open fact does not hold far future: %+v, %v", got, ok)
	}
	// The half-open end excludes exactly: the end instant belongs to the
	// successor, never to both.
	bounded := NewIntervalSet()
	if err := bounded.Add(durabilityTenant, "k", Interval{Start: base, End: base.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := bounded.Add(durabilityTenant, "k", Interval{Start: base.Add(time.Hour), End: base.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	at, ok := bounded.EffectiveAt(durabilityTenant, "k", base.Add(time.Hour))
	if !ok || !at.Start.Equal(base.Add(time.Hour)) {
		t.Fatalf("boundary instant resolved to %+v, %v", at, ok)
	}
}

func FuzzTodo_ALIGN_034_Fuzz(f *testing.F) {
	f.Add(int64(0), int64(3600), int64(7200))
	f.Fuzz(func(t *testing.T, a, b, c int64) {
		base := durabilityBase()
		spans := []Interval{
			{Start: base.Add(time.Duration(a%86400) * time.Second), End: base.Add(time.Duration(b%86400) * time.Second)},
			{Start: base.Add(time.Duration(c%86400) * time.Second)},
		}
		set := NewIntervalSet()
		_ = set.Add(durabilityTenant, "fuzz", spans[0])
		_ = set.Add(durabilityTenant, "fuzz", spans[1])
		at := base.Add(time.Duration((a+b+c)%86400) * time.Second)
		first, firstOK := set.EffectiveAt(durabilityTenant, "fuzz", at)
		second, secondOK := set.EffectiveAt(durabilityTenant, "fuzz", at)
		if firstOK != secondOK || first != second {
			t.Fatalf("lookup is not deterministic: %+v vs %+v", first, second)
		}
	})
}
