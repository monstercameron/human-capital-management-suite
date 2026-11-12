package doctemplate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func definition() Definition {
	return Definition{ID: "offer", Version: "1", Type: "OFFER", Purpose: "employment", SourceLocale: "en-US", Locale: "en-US", Jurisdiction: "US-NC", Classification: "CONFIDENTIAL", AccessibilityVersion: "wcag-2.2", Format: "text/html", Source: "Hello {{.name}} ({{.start}})", Placeholders: []Placeholder{{Name: "name", Kind: String, Required: true}, {Name: "start", Kind: Date, Required: true}}}
}

func goodBindings() map[string]Binding {
	return map[string]Binding{"name": {Kind: String, Value: "Ada"}, "start": {Kind: Date, Value: "2026-09-03"}}
}

func published(t *testing.T) Template {
	t.Helper()
	draft, err := New(definition())
	if err != nil {
		t.Fatal(err)
	}
	got, err := draft.Publish(Publication{
		TemplateDigest: draft.Digest(),
		Fixtures:       []Fixture{{Name: "golden", Bindings: goodBindings()}},
		Approval:       Approval{ApprovalID: "approval-1", ApprovedBy: "hr"},
		Applicability:  Applicability{Locales: []string{"en-US"}, Jurisdictions: []string{"US-NC"}, Classifications: []string{"CONFIDENTIAL"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTodo_DOC_TEMPLATE_001(t *testing.T) {
	tpl := published(t)
	a, err := tpl.Render(goodBindings())
	if err != nil {
		t.Fatal(err)
	}
	b, err := tpl.Render(goodBindings())
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest == "" || a.Digest != b.Digest || string(a.Content) != "Hello Ada (2026-09-03)" {
		t.Fatalf("non-deterministic or unexpected artifact: %#v", a)
	}
}

func TestTodo_DOC_TEMPLATE_001_Golden(t *testing.T) {
	tpl := published(t)
	a, err := tpl.Render(goodBindings())
	if err != nil {
		t.Fatal(err)
	}
	want := "Hello Ada (2026-09-03)"
	if string(a.Content) != want {
		t.Fatalf("content = %q, want %q", a.Content, want)
	}
	sum := sha256.Sum256([]byte(want))
	if a.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("digest = %q", a.Digest)
	}
}

func TestTodo_DOC_TEMPLATE_001_Integration(t *testing.T) {
	tpl := published(t)
	if tpl.Publication == nil || tpl.Publication.TemplateDigest != tpl.Digest() || len(tpl.Publication.Fixtures) != 1 {
		t.Fatalf("publication did not retain approval and fixture metadata: %#v", tpl.Publication)
	}
	retired, err := tpl.Retire("superseded", "offer-v2")
	if err != nil {
		t.Fatal(err)
	}
	if retired.Publication.Successor != "offer-v2" || retired.Publication.RetiredReason != "superseded" {
		t.Fatalf("retirement metadata = %#v", retired.Publication)
	}
}

func TestTodo_DOC_TEMPLATE_001_Browser(t *testing.T) {
	tpl := published(t)
	if tpl.Definition.AccessibilityVersion == "" || tpl.Definition.Format == "" {
		t.Fatal("published template must carry accessibility and format metadata")
	}
}

func TestTodo_DOC_TEMPLATE_001_Security(t *testing.T) {
	tpl := published(t)
	_, err := tpl.Render(map[string]Binding{
		"name": {Kind: String, Value: "Ada"}, "start": {Kind: Date, Value: "2026-09-03"},
		"secret": {Kind: String, Value: "must not be traversed"},
	})
	if err == nil || !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown field binding err = %v, want ErrInvalid", err)
	}
}

func TestTodo_DOC_TEMPLATE_001_ApplicabilitySnapshot(t *testing.T) {
	locales := []string{"en-US"}
	draft, err := New(definition())
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := draft.Publish(Publication{
		TemplateDigest: draft.Digest(), Fixtures: []Fixture{{Name: "golden", Bindings: goodBindings()}},
		Approval:      Approval{ApprovalID: "approval-1", ApprovedBy: "hr"},
		Applicability: Applicability{Locales: locales, Jurisdictions: []string{"US-NC"}, Classifications: []string{"CONFIDENTIAL"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	locales[0] = "fr-FR"
	if tpl.Publication.Applicability.Locales[0] != "en-US" {
		t.Fatal("publication applicability aliases caller memory")
	}
}

func TestTypedBindingsAndMetadataAreRequired(t *testing.T) {
	tpl := published(t)
	for name, bindings := range map[string]map[string]Binding{
		"wrong kind": {"name": {Kind: Integer, Value: "1"}, "start": {Kind: Date, Value: "2026-09-03"}},
		"missing":    {"name": {Kind: String, Value: "Ada"}},
		"unknown":    {"name": {Kind: String, Value: "Ada"}, "start": {Kind: Date, Value: "2026-09-03"}, "secret": {Kind: String, Value: "x"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tpl.Render(bindings); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	bad, err := New(Definition{ID: "x", Version: "1", Type: "x", Purpose: "x", SourceLocale: "en-US", Locale: "en-US", Jurisdiction: "US-NC", Classification: "CONFIDENTIAL", AccessibilityVersion: "wcag-2.2", Source: "static body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Publish(Publication{
		TemplateDigest: bad.Digest(),
		Fixtures:       []Fixture{{Name: "golden", Bindings: map[string]Binding{}}},
		Approval:       Approval{ApprovalID: "a", ApprovedBy: "b"},
		Applicability:  Applicability{Locales: []string{"fr-FR"}, Jurisdictions: []string{"US-NC"}, Classifications: []string{"CONFIDENTIAL"}},
	}); err == nil {
		t.Fatal("expected locale applicability rejection")
	}
}

// TestPublishVerifiesEveryFixture proves the publication gate: a template
// cannot publish without at least one fixture, each fixture needs a name and
// a binding set the published source can actually render, and a stated
// expected digest must be the digest the render really produces.
func TestPublishVerifiesEveryFixture(t *testing.T) {
	draft, err := New(definition())
	if err != nil {
		t.Fatal(err)
	}
	pub := func(fixtures []Fixture) Publication {
		return Publication{
			TemplateDigest: draft.Digest(),
			Fixtures:       fixtures,
			Approval:       Approval{ApprovalID: "approval-1", ApprovedBy: "hr"},
			Applicability:  Applicability{Locales: []string{"en-US"}, Jurisdictions: []string{"US-NC"}, Classifications: []string{"CONFIDENTIAL"}},
		}
	}
	wantInvalid := func(t *testing.T, fixtures []Fixture) {
		t.Helper()
		_, err := draft.Publish(pub(fixtures))
		if err == nil || !errors.Is(err, ErrInvalid) {
			t.Fatalf("publish with %#v: err = %v, want ErrInvalid", fixtures, err)
		}
	}

	t.Run("no fixtures", func(t *testing.T) { wantInvalid(t, nil) })
	t.Run("unnamed fixture", func(t *testing.T) { wantInvalid(t, []Fixture{{Bindings: goodBindings()}}) })
	t.Run("fixture with a missing required binding", func(t *testing.T) {
		wantInvalid(t, []Fixture{{Name: "golden", Bindings: map[string]Binding{"name": {Kind: String, Value: "Ada"}}}})
	})
	t.Run("fixture with an unrenderable binding", func(t *testing.T) {
		wantInvalid(t, []Fixture{{Name: "golden", Bindings: map[string]Binding{"name": {Kind: String, Value: "Ada"}, "start": {Kind: Integer, Value: "1"}}}})
	})
	t.Run("fixture digest mismatch", func(t *testing.T) {
		wantInvalid(t, []Fixture{{Name: "golden", Bindings: goodBindings(), ExpectedDigest: "sha256:" + strings.Repeat("00", 32)}})
	})

	t.Run("fixture digest match publishes and records the fixture", func(t *testing.T) {
		sum := sha256.Sum256([]byte("Hello Ada (2026-09-03)"))
		tpl, err := draft.Publish(pub([]Fixture{
			{Name: "golden", Bindings: goodBindings(), ExpectedDigest: "sha256:" + hex.EncodeToString(sum[:])},
		}))
		if err != nil {
			t.Fatalf("publish with a correct fixture digest: %v", err)
		}
		if len(tpl.Publication.Fixtures) != 1 || tpl.Publication.Fixtures[0].Name != "golden" {
			t.Fatalf("publication did not record the verified fixture: %#v", tpl.Publication)
		}
	})
}

func TestChangedAfterApprovalAndRetirement(t *testing.T) {
	tpl := published(t)
	altered := tpl
	altered.Definition.Source = strings.ReplaceAll(altered.Definition.Source, "Hello", "Hi")
	if _, err := altered.Render(goodBindings()); err != ErrChangedAfterApproval {
		t.Fatalf("got %v, want changed-after-approval", err)
	}
	retired, err := tpl.Retire("superseded", "offer-v2")
	if err != nil {
		t.Fatal(err)
	}
	if retired.State != Retired {
		t.Fatal(retired.State)
	}
	if _, err := retired.Render(nil); err != ErrNotPublished {
		t.Fatalf("got %v, want not published", err)
	}
}
