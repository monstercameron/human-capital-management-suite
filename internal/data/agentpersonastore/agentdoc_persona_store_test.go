package agentpersonastore

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTDOC_002_Integration(t *testing.T) {
	const tenant values.TenantId = "agentdoc-persona-store"
	f := newFixture(t, tenant)
	refs := []agentdocref.Reference{
		{DocumentID: "doc-123e4567-e89b-42d3-a456-426614174000", VersionMode: agentdocref.ModePinned, PinnedVersion: 7, SectionAnchor: "leave-policy", Label: "Leave policy"},
		{DocumentID: "doc-223e4567-e89b-42d3-a456-426614174000", VersionMode: agentdocref.ModeLatestPublished, Label: "Tooling guide"},
	}
	profile, err := json.Marshal(struct {
		Owner              string                  `json:"owner"`
		DocumentReferences []agentdocref.Reference `json:"document_references,omitempty"`
	}{Owner: "user:business-owner", DocumentReferences: refs})
	if err != nil {
		t.Fatal(err)
	}
	v := version(tenant, "persona-agentdoc", 1)
	v.Profile = profile
	if err := f.store(t, tenant).PutVersion(context.Background(), v); err != nil {
		t.Fatalf("PutVersion() = %v", err)
	}

	// A new tenant-scoped store handle simulates a process restart: the result
	// must come from PostgreSQL rather than caller-owned memory.
	read, err := f.store(t, tenant).GetVersion(context.Background(), v.PersonaID, v.Version)
	if err != nil {
		t.Fatalf("GetVersion() after restart = %v", err)
	}
	var decoded struct {
		DocumentReferences []agentdocref.Reference `json:"document_references"`
	}
	if err := json.Unmarshal(read.Profile, &decoded); err != nil {
		t.Fatalf("decode stored profile: %v", err)
	}
	if !reflect.DeepEqual(decoded.DocumentReferences, refs) {
		t.Fatalf("stored references = %#v, want %#v", decoded.DocumentReferences, refs)
	}
	if read.ContentDigest != v.ContentDigest {
		t.Fatalf("stored digest = %q, want %q", read.ContentDigest, v.ContentDigest)
	}
}

func TestTodo_AGENTDOC_008_Integration(t *testing.T) {
	const tenant values.TenantId = "agentdoc-guidance-store"
	f := newFixture(t, tenant)
	guidance := "Follow {{doc:doc-policy}} before answering."
	refs := []agentdocref.Reference{{DocumentID: "doc-policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 7, Label: "Travel policy"}}
	profile, err := json.Marshal(struct {
		Owner              string                  `json:"owner"`
		Guidance           string                  `json:"guidance,omitempty"`
		DocumentReferences []agentdocref.Reference `json:"document_references,omitempty"`
	}{Owner: "user:business-owner", Guidance: guidance, DocumentReferences: refs})
	if err != nil {
		t.Fatal(err)
	}
	v := version(tenant, "persona-guidance", 1)
	v.Profile = profile
	if err := f.store(t, tenant).PutVersion(context.Background(), v); err != nil {
		t.Fatalf("PutVersion() = %v", err)
	}
	read, err := f.store(t, tenant).GetVersion(context.Background(), v.PersonaID, v.Version)
	if err != nil {
		t.Fatalf("GetVersion() = %v", err)
	}
	var decoded struct {
		Guidance           string                  `json:"guidance"`
		DocumentReferences []agentdocref.Reference `json:"document_references"`
	}
	if err := json.Unmarshal(read.Profile, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Guidance != guidance || !reflect.DeepEqual(decoded.DocumentReferences, refs) {
		t.Fatalf("stored profile = %+v", decoded)
	}
}
