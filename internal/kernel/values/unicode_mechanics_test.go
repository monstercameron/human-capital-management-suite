package values

import "testing"

func TestUnicodeMechanicsGolden(t *testing.T) {
	if got := NFKC("ａｇｅｎｔ"); got != "agent" {
		t.Fatalf("NFKC(fullwidth)=%q, want %q", got, "agent")
	}
	if got := NFKD("é"); got != "e\u0301" {
		t.Fatalf("NFKD(accent)=%q, want %q", got, "e\u0301")
	}
	for _, tc := range []struct{ input, want string }{
		{"Straße", "strasse"},
		{"ς", "σ"},
	} {
		if got := CaseFold(tc.input); got != tc.want {
			t.Errorf("CaseFold(%q)=%q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCaseFolderReusesFullFoldAcrossInputs(t *testing.T) {
	var folder CaseFolder
	if got := folder.Fold("ß"); got != "ss" {
		t.Fatalf("first Fold()=%q, want ss", got)
	}
	if !folder.initialized {
		t.Fatal("first Fold() did not initialize the lazy caser")
	}
	caser := folder.caser
	if got := folder.Fold("Σς"); got != "σσ" {
		t.Fatalf("second Fold()=%q, want σσ", got)
	}
	if folder.caser != caser {
		t.Fatal("repeated Fold() replaced the per-operation caser")
	}
}
