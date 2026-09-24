package dsr

import "testing"

func TestAppendFields_Deterministic(t *testing.T) {
	a := appendFields(nil, "a", "1", "b", "2")
	b := appendFields(nil, "a", "1", "b", "2")
	if digestHex(a) != digestHex(b) {
		t.Fatalf("identical field lists digested differently: %s vs %s", digestHex(a), digestHex(b))
	}
}

func TestAppendFields_LabelValueSplitIsUnambiguous(t *testing.T) {
	// "ab"/"c" and "a"/"bc" must not collide just because their
	// concatenation is the same string.
	x := appendFields(nil, "ab", "c")
	y := appendFields(nil, "a", "bc")
	if digestHex(x) == digestHex(y) {
		t.Fatalf("different label/value splits produced the same digest")
	}
}

func TestAppendFields_OddArgsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("appendFields with an odd argument count did not panic")
		}
	}()
	//lint:ignore SA5012 deliberate odd arity: this test proves appendFields panics on an odd argument count. owner=privacy-data-rights expires=2027-03-24
	appendFields(nil, "a")
}

func TestDigestHex_IsLowercaseHexSHA256Length(t *testing.T) {
	got := digestHex([]byte("anything"))
	if len(got) != 64 {
		t.Fatalf("digestHex length = %d, want 64", len(got))
	}
	for _, c := range got {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			t.Fatalf("digestHex contains non-lowercase-hex byte %q", c)
		}
	}
}
