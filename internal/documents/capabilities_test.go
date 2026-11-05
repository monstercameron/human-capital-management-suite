package documents

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/documents/template"
)

func stubHandler(_ context.Context, _ any) (any, error) { return nil, nil }

func publishedLetter(t *testing.T) template.Template {
	t.Helper()
	tpl, err := template.New(template.Definition{
		ID: "team_letter", Version: "3", Type: "LETTER", Purpose: "team notice",
		SourceLocale: "en-US", Locale: "en-US", Jurisdiction: "US-CA",
		Classification: "INTERNAL", AccessibilityVersion: "wcag22-aa",
		Source:       "Hello {{.name}}!",
		Placeholders: []template.Placeholder{{Name: "name", Kind: template.String, Required: true}},
	})
	if err != nil {
		t.Fatalf("template.New: %v", err)
	}
	published, err := tpl.Publish(template.Publication{
		TemplateDigest: tpl.Digest(),
		Approval:       template.Approval{ApprovalID: "ap-1", ApprovedBy: "hr-1"},
		Fixtures: []template.Fixture{{
			Name:     "ann",
			Bindings: map[string]template.Binding{"name": {Kind: template.String, Value: "Ann"}},
		}},
		Applicability: template.Applicability{
			Locales: []string{"en-US"}, Jurisdictions: []string{"US-CA"}, Classifications: []string{"INTERNAL"},
		},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return published
}

func TestDocumentsCapabilities(t *testing.T) {
	defs := Definitions()
	if len(defs) != 2 {
		t.Fatalf("definitions = %d, want render + request_signature", len(defs))
	}
	byID := map[string]capability.Definition{}
	for _, d := range defs {
		byID[d.ID] = d
	}
	if byID[CapRender].EffectClass != capability.EffectReadOnly {
		t.Fatalf("render effect = %s", byID[CapRender].EffectClass)
	}
	if byID[CapRequestSignature].EffectClass != capability.EffectInternalMutation {
		t.Fatalf("request_signature effect = %s", byID[CapRequestSignature].EffectClass)
	}
	reg := capability.NewRegistry()
	for _, d := range defs {
		if err := reg.Register(d, stubHandler); err != nil {
			t.Fatalf("register %s: %v", d.ID, err)
		}
	}
	for _, id := range []string{CapRender, CapRequestSignature} {
		rec, ok := reg.Lookup(capability.Key{ID: id, Version: 1})
		if !ok || rec.Digest == "" {
			t.Fatalf("%s not resolvable with a digest", id)
		}
	}
}

func TestDocumentsRender(t *testing.T) {
	published := publishedLetter(t)
	store := TemplateMap{"team_letter@3": published}
	out, err := Render(store, map[string]string{
		"template_id": "team_letter", "template_version": "3",
		"bindings_json": `{"name":"Jo"}`,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out.Content != "Hello Jo!" {
		t.Fatalf("content = %q", out.Content)
	}
	direct, err := published.Render(map[string]template.Binding{"name": {Kind: template.String, Value: "Jo"}})
	if err != nil {
		t.Fatalf("direct render: %v", err)
	}
	if out.ArtifactDigest != direct.Digest {
		t.Fatal("operation artifact differs from the plane render")
	}
}

func TestDocumentsRenderRefusals(t *testing.T) {
	store := TemplateMap{"team_letter@3": publishedLetter(t)}
	cases := []map[string]string{
		{"template_id": "missing", "template_version": "1", "bindings_json": `{}`},
		{"template_id": "team_letter", "template_version": "3", "bindings_json": `{"name":`},
		{"template_id": "team_letter", "template_version": "3", "bindings_json": `{}`},
		{"template_id": "team_letter", "template_version": "3", "bindings_json": `{"name":"x","extra":"y"}`},
	}
	for i, in := range cases {
		if _, err := Render(store, in); err == nil {
			t.Errorf("case %d rendered", i)
		}
	}
	if _, err := Render(nil, map[string]string{}); err == nil {
		t.Fatal("nil store accepted")
	}
}

func TestDocumentsRequestSignature(t *testing.T) {
	inputs := map[string]string{
		"artifact_hash": "sha256:abc", "signer_id": "worker-1",
		"assurance": "ENHANCED", "expires_in_seconds": "3600", "ceremony_key": "k-1",
	}
	out, err := RequestSignature(NewCeremonyRegistry(), inputs, time.Unix(1000, 0))
	if err != nil {
		t.Fatalf("request_signature: %v", err)
	}
	if out.State != "REQUESTED" || out.CeremonyID == "" || out.Digest == "" {
		t.Fatalf("outcome = %+v", out)
	}
	again, err := RequestSignature(NewCeremonyRegistry(), inputs, time.Unix(1000, 0))
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if again.CeremonyID != out.CeremonyID {
		t.Fatal("same ceremony key produced a different ceremony id")
	}
}

func TestDocumentsRequestSignatureRefusals(t *testing.T) {
	at := time.Unix(1000, 0)
	bad := map[string]string{
		"artifact_hash": "sha256:abc", "signer_id": "worker-1",
		"assurance": "ULTRA", "expires_in_seconds": "3600", "ceremony_key": "k-1",
	}
	if _, err := RequestSignature(NewCeremonyRegistry(), bad, at); err == nil {
		t.Fatal("unknown assurance accepted")
	}
	bad["assurance"] = "BASIC"
	bad["artifact_hash"] = ""
	if _, err := RequestSignature(NewCeremonyRegistry(), bad, at); err == nil {
		t.Fatal("empty artifact hash accepted")
	}
	bad["artifact_hash"] = "sha256:abc"
	bad["expires_in_seconds"] = "long"
	if _, err := RequestSignature(NewCeremonyRegistry(), bad, at); err == nil {
		t.Fatal("non-integer expiry accepted")
	}
}
