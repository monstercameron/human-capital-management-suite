package chat

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestIssue3932_NormalizeSavedLabel(t *testing.T) {
	for in, want := range map[string]string{"  Open   enrollment ": "Open enrollment", "Hiring": "Hiring", "Übergabe": "Übergabe"} {
		if got, ok := NormalizeSavedLabel(in); !ok || got != want {
			t.Errorf("%q -> %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "   ", "a,b", "bad\x00", "tab\u0007bell", strings.Repeat("x", SavedLabelMaxRunes+1), "\xff"} {
		if _, ok := NormalizeSavedLabel(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
	if _, ok := NormalizeSavedLabel(strings.Repeat("ع", SavedLabelMaxRunes)); !ok {
		t.Error("a label counts characters, not bytes")
	}
}

func TestIssue3932_ApplySavedLabels(t *testing.T) {
	got, err := ApplySavedLabels([]string{"Hiring", "Handover"}, []string{"hiring", "Benefits"}, []string{"handover"})
	if err != nil || !reflect.DeepEqual(got, []string{"Hiring", "Benefits"}) {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err = ApplySavedLabels(nil, []string{"a,b"}, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid add: %v", err)
	}
	many := []string{}
	for _, r := range "abcdefghijk" {
		many = append(many, string(r))
	}
	if _, err = ApplySavedLabels(nil, many, nil); !errors.Is(err, ErrSavedLabelLimit) {
		t.Fatalf("per item limit: %v", err)
	}
	if got, err = ApplySavedLabels([]string{"a"}, nil, []string{"A"}); err != nil || len(got) != 0 {
		t.Fatalf("remove is case blind: %v %v", got, err)
	}
}
