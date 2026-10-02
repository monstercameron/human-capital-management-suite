package productui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTodo_AGENTDOC_002_Golden(t *testing.T) {
	encoded, err := json.Marshal(PersonaAdminPersona{DocumentReferences: []PersonaAdminDocumentReference{{DocumentID: "doc-123e4567-e89b-42d3-a456-426614174000", VersionMode: "PINNED", PinnedVersion: 2, SectionAnchor: "policy", Label: "Policy", Readable: false}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"document_references":[{"document_id":"doc-123e4567-e89b-42d3-a456-426614174000","version_mode":"PINNED","pinned_version":2,"section_anchor":"policy","label":"Policy","readable":false}]`) {
		t.Fatalf("catalog JSON = %s", encoded)
	}
	if strings.Contains(text, `"title"`) {
		t.Fatalf("unreadable reference leaked a title: %s", encoded)
	}
}
