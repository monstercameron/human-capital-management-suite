package importdraft

import (
	"bytes"
	"strings"
	"testing"
)

func TestCanon_DigestIsStableForIdenticalWrites(t *testing.T) {
	build := func() string {
		return NewCanon("schema.test", 2).Str("a").U64(7).I64(-3).Bool(true).Strings([]string{"x", "y"}).DigestHex()
	}
	first, second := build(), build()
	if first != second {
		t.Fatalf("identical write sequences produced different digests: %q vs %q", first, second)
	}
	if !strings.HasPrefix(first, "sha256:") || len(first) != len("sha256:")+64 {
		t.Fatalf("digest %q is not the tagged lowercase sha256 form", first)
	}
}

func TestCanon_SchemaAndVersionAreBound(t *testing.T) {
	base := NewCanon("schema.a", 1).Str("payload").DigestHex()
	if other := NewCanon("schema.b", 1).Str("payload").DigestHex(); other == base {
		t.Fatal("a different schema name produced the same digest")
	}
	if other := NewCanon("schema.a", 2).Str("payload").DigestHex(); other == base {
		t.Fatal("a different schema version produced the same digest")
	}
}

func TestCanon_LengthPrefixPreventsConcatenationCollisions(t *testing.T) {
	ab := NewCanon("s", 1).Str("ab").Str("c").DigestHex()
	a := NewCanon("s", 1).Str("a").Str("bc").DigestHex()
	if ab == a {
		t.Fatal(`["ab","c"] and ["a","bc"] collided: the stream is not self-delimiting`)
	}
	listA := NewCanon("s", 1).Strings([]string{"ab", "c"}).DigestHex()
	listB := NewCanon("s", 1).Strings([]string{"a", "bc"}).DigestHex()
	if listA == listB {
		t.Fatal("string lists with shifted boundaries collided")
	}
}

func TestCanon_SignedZigzagSeparatesSignsAndMagnitudes(t *testing.T) {
	seen := map[string]int64{}
	for _, v := range []int64{-2, -1, 0, 1, 2, -1 << 40, 1 << 40} {
		d := NewCanon("s", 1).I64(v).DigestHex()
		if prior, ok := seen[d]; ok {
			t.Fatalf("I64(%d) and I64(%d) produced the same digest", prior, v)
		}
		seen[d] = v
	}
	if NewCanon("s", 1).I64(5).DigestHex() == NewCanon("s", 1).U64(5).DigestHex() {
		t.Fatal("zigzag I64(5) must not encode the same bytes as U64(5)")
	}
}

func TestCanon_BoolEncodesEachValueDistinctly(t *testing.T) {
	yes := NewCanon("s", 1).Bool(true).DigestHex()
	no := NewCanon("s", 1).Bool(false).DigestHex()
	if yes == no {
		t.Fatal("true and false produced the same digest")
	}
}

func TestCanon_BytesIsACopyOfTheStream(t *testing.T) {
	w := NewCanon("s", 1).Str("payload")
	before := w.DigestHex()
	out := w.Bytes()
	if len(out) == 0 {
		t.Fatal("Bytes returned an empty stream for a non-empty write")
	}
	out[0] ^= 0xff
	if w.DigestHex() != before {
		t.Fatal("mutating the bytes returned by Bytes changed the writer's digest")
	}
	if !bytes.Contains(w.Bytes(), []byte("payload")) {
		t.Fatal("the canonical stream does not carry the written string verbatim")
	}
}
