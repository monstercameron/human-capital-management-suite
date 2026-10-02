package productui

import (
	"strings"
	"testing"
)

func TestAgentUXQuality_DocumentSection(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := docsReaderTestView(locale)
		view.Document.Markdown = "# Leave policy\n\n## Carryover\nFirst.\n\n## Carryover\nSecond.\n\n#### Details\nDeep section."
		view.Document.Citation = &AgentAnswerDocumentCitation{VersionID: "version-7", Anchor: "carryover-2"}
		markup, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{`data-document-section="carryover"`, `data-document-section="carryover-2"`, `data-document-section="details"`, "docs-cited-section", "prefers-reduced-motion"} {
			if !strings.Contains(markup, expected) {
				t.Fatalf("%s missing %s", locale, expected)
			}
		}
		if strings.Contains(markup, agentAnswerDocsCopy(locale, "missing")) || !agentAnswerDocumentHasSection(view.Document.Markdown, "details") {
			t.Fatalf("existing or deep section treated as missing: %s", locale)
		}
		view.Document.Citation.Anchor = "deleted"
		markup, err = Render(view)
		if err != nil || !strings.Contains(markup, agentAnswerDocsCopy(locale, "missing")) {
			t.Fatalf("missing section lacks next step: %s %v", locale, err)
		}
	}
	if agentAnswerDocumentRoute() != (AgentAnswerDocumentCitation{}) || agentAnswerRevealSection("carryover") != nil {
		t.Fatal("native document navigation performed a browser action")
	}
	if agentAnswerDocumentHasSection("```markdown\n## Carryover\n```\n", "carryover") {
		t.Fatal("a heading inside a code example was treated as a document section")
	}
	if !agentAnswerDocumentHasSection("```markdown\n## Carryover\n```\n\n## Carryover\nActual section.", "carryover-2") {
		t.Fatal("actual section lost its store-derived anchor after a code example")
	}
}

func TestAgentUXQuality_CitedVersion_Security(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := docsReaderTestView(locale)
		view.Document.Citation = &AgentAnswerDocumentCitation{VersionID: "private-old-version", Anchor: "approvals"}
		markup, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, "Requests take 7 business days") || !strings.Contains(markup, agentAnswerDocsCopy(locale, "loading")) || !strings.Contains(markup, agentAnswerDocsCopy(locale, "current")) {
			t.Fatalf("cited version substituted current bytes or omitted next step: %s", locale)
		}
		for _, key := range []string{"viewing", "newer", "compare", "failed"} {
			if agentAnswerDocsCopy(locale, key) == "" {
				t.Fatalf("missing localized version copy %s/%s", locale, key)
			}
		}
	}
}

func TestAgentUXQuality_DocumentVersions(t *testing.T) {
	for _, value := range []string{"1", "v1", "version 1", "v1.0.0"} {
		if got := docsDisplayVersion(DocumentSummary{Version: value}); got != "v1.0.0" {
			t.Fatalf("document version %q: %q", value, got)
		}
	}
	versions := docsCompareSelectable([]DocumentVersionSummary{{VersionID: "first"}, {VersionID: "second", Version: "v2.3.1"}})
	if len(versions) != 2 || versions[0].Version != "v1.0.0" || versions[1].Version != "v2.3.1" {
		t.Fatalf("version picker: %+v", versions)
	}
	locale := ResolveProductLocale("en-US")
	if label := docsCompareOptionLabel(locale, func(key string) string { return key }, versions[0], false, false); !strings.HasPrefix(label, "v1.0.0 · ") {
		t.Fatalf("picker lost semantic label: %s", label)
	}
}
