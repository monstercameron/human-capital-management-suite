package extract

// Unit tests for the generator's reviewed inputs: loading, validation,
// verbatim rendering and emission order. The byte-level agreement between
// Generate and the checked-in packs is TestTodo_LEGAL_010_Golden's job;
// these tests pin the mechanism that content rides on.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func writeReviewed(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash("internal/governance/legal/extract"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reviewed_overrides.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReviewed_LoadAndValidate(t *testing.T) {
	t.Run("missing file fails", func(t *testing.T) {
		if _, err := LoadReviewedOverrides(t.TempDir()); err == nil {
			t.Fatal("expected an error for a missing reviewed file")
		}
	})
	t.Run("unknown state fails", func(t *testing.T) {
		root := writeReviewed(t, `{"states":{"XX":{"basis":"test","kinds":{}}}}`)
		if _, err := LoadReviewedOverrides(root); err == nil {
			t.Fatal("expected an error for an unknown state")
		}
	})
	t.Run("empty basis fails", func(t *testing.T) {
		root := writeReviewed(t, `{"states":{"MI":{"basis":"","kinds":{}}}}`)
		if _, err := LoadReviewedOverrides(root); err == nil {
			t.Fatal("expected an error for an empty basis")
		}
	})
	t.Run("unknown kind fails", func(t *testing.T) {
		root := writeReviewed(t, `{"states":{"MI":{"basis":"test","kinds":{"VIBES":[]}}}}`)
		if _, err := LoadReviewedOverrides(root); err == nil {
			t.Fatal("expected an error for an unknown kind")
		}
	})
	t.Run("empty obligation id fails", func(t *testing.T) {
		root := writeReviewed(t, `{"states":{"MI":{"basis":"test","kinds":{"NOTICE":[{"id":"","section":"s","note":"n","confidence_marker":"VERIFY","body":{}}]}}}}`)
		if _, err := LoadReviewedOverrides(root); err == nil {
			t.Fatal("expected an error for an empty obligation id")
		}
	})
	t.Run("missing marker fails", func(t *testing.T) {
		root := writeReviewed(t, `{"states":{"MI":{"basis":"test","kinds":{"NOTICE":[{"id":"x","section":"s","note":"n","confidence_marker":"","body":{}}]}}}}`)
		if _, err := LoadReviewedOverrides(root); err == nil {
			t.Fatal("expected an error for a missing confidence marker")
		}
	})
	t.Run("unknown fields fail", func(t *testing.T) {
		root := writeReviewed(t, `{"states":{"MI":{"basis":"test","kinds":{},"typo_field":1}}}`)
		if _, err := LoadReviewedOverrides(root); err == nil {
			t.Fatal("expected an error for an unknown field")
		}
	})
	t.Run("suppression list loads", func(t *testing.T) {
		root := writeReviewed(t, `{"states":{"CO":{"basis":"test","kinds":{"NOTICE":[]}}}}`)
		ov, err := LoadReviewedOverrides(root)
		if err != nil {
			t.Fatalf("LoadReviewedOverrides: %v", err)
		}
		list, ok := ov.forState("CO").Kinds["NOTICE"]
		if !ok || list == nil || len(list) != 0 {
			t.Fatalf("suppression list = %#v, want an empty non-nil list", list)
		}
		if ov.forState("WY") != nil {
			t.Fatal("unlisted state must take the mechanical path")
		}
	})
}

func TestReviewed_CheckedInFile(t *testing.T) {
	root, err := legal.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	ov, err := LoadReviewedOverrides(root)
	if err != nil {
		t.Fatalf("LoadReviewedOverrides: %v", err)
	}
	// Spot-check the reviewed findings the conformance oracle depends on:
	// the Michigan F-cell non-compete, the New York locality rule, and
	// the two confirmed absences.
	mi, ok := ov.States["MI"]
	if !ok {
		t.Fatal("no reviewed entry for MI")
	}
	found := false
	for _, ro := range mi.Kinds[legal.ObligationTypeNonCompete.String()] {
		if strings.Contains(ro.Section, "445.774a") {
			found = true
		}
	}
	if !found {
		t.Error("MI reviewed non-compete does not cite MCL 445.774a")
	}
	ny, ok := ov.States["NY"]
	if !ok {
		t.Fatal("no reviewed entry for NY")
	}
	found = false
	for _, ro := range ny.Kinds[legal.ObligationTypeAutomatedDecision.String()] {
		if strings.Contains(ro.Section, "144") {
			found = true
		}
	}
	if !found {
		t.Error("NY reviewed automated-decision rule does not cite Local Law 144")
	}
	for _, tc := range []struct{ state, kind string }{
		{"CO", legal.ObligationTypeNotice.String()},
		{"NJ", legal.ObligationTypePersonnelFile.String()},
	} {
		list, ok := ov.States[tc.state].Kinds[tc.kind]
		if !ok || list == nil || len(list) != 0 {
			t.Errorf("%s/%s is not a suppression entry", tc.state, tc.kind)
		}
	}
}

func TestReviewed_ObligationRendering(t *testing.T) {
	state, _ := StateByCode("MI")
	file := &ResearchFile{Path: "planning/research/state-employment-law/michigan.md"}
	ro := ReviewedObligation{
		ID:               "us-mi-non-compete",
		Section:          "MCL 445.774a",
		Note:             "reasonableness test",
		ConfidenceMarker: legal.ConfidenceMarkerConfirmed.String(),
	}
	got := reviewedObligation(state, file, legal.ObligationTypeNonCompete, ro)
	if got.Kind != "NON_COMPETE" || got.ID != ro.ID {
		t.Errorf("identity = %s/%s, want NON_COMPETE/us-mi-non-compete", got.Kind, got.ID)
	}
	if got.Citation.SourceFile != file.Path {
		t.Errorf("source file = %q, want the research file", got.Citation.SourceFile)
	}
	if got.Citation.ReviewStatus != legal.ReviewStatusUnreviewed.String() {
		t.Errorf("review status = %q, reviewed content stays UNREVIEWED", got.Citation.ReviewStatus)
	}
	if got.Citation.Section != ro.Section || got.Citation.Note != ro.Note ||
		got.Citation.ConfidenceMarker != ro.ConfidenceMarker {
		t.Errorf("citation = %+v, want the verbatim reviewed fields", got.Citation)
	}
}

func TestReviewed_ApplyOrder(t *testing.T) {
	obs := func(ids ...string) []legal.ObligationJSON {
		out := make([]legal.ObligationJSON, 0, len(ids))
		for _, id := range ids {
			out = append(out, legal.ObligationJSON{ID: id})
		}
		return out
	}
	ids := func(list []legal.ObligationJSON) []string {
		out := make([]string, 0, len(list))
		for _, o := range list {
			out = append(out, o.ID)
		}
		return out
	}
	t.Run("reviewed order wins", func(t *testing.T) {
		got, err := applyOrder("MI", obs("a", "b", "c"), []string{"c", "a", "b"})
		if err != nil {
			t.Fatalf("applyOrder: %v", err)
		}
		if strings.Join(ids(got), ",") != "c,a,b" {
			t.Errorf("order = %v, want c,a,b", ids(got))
		}
	})
	t.Run("dropped id fails", func(t *testing.T) {
		if _, err := applyOrder("MI", obs("a", "b"), []string{"a"}); err == nil {
			t.Error("expected an error when the order drops an emitted id")
		}
	})
	t.Run("unknown id fails", func(t *testing.T) {
		if _, err := applyOrder("MI", obs("a"), []string{"a", "zzz"}); err == nil {
			t.Error("expected an error when the order names an unemitted id")
		}
	})
	t.Run("repeated id fails", func(t *testing.T) {
		if _, err := applyOrder("MI", obs("a", "b"), []string{"a", "a"}); err == nil {
			t.Error("expected an error when the order names an id twice")
		}
	})
	t.Run("duplicate emission fails", func(t *testing.T) {
		if _, err := applyOrder("MI", obs("a", "a"), []string{"a"}); err == nil {
			t.Error("expected an error on a duplicate emitted id")
		}
	})
}
