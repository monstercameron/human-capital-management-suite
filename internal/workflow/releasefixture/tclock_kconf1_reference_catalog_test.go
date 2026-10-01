package releasefixture

import "testing"

func TestReferenceConformanceCatalog(t *testing.T) {
	catalog := ReferenceConformanceCatalog()
	if len(catalog) != 3 {
		t.Fatalf("reference conformance entries = %d, want 3", len(catalog))
	}
	seen := make(map[string]bool, len(catalog))
	for _, entry := range catalog {
		if entry.ID == "" || entry.WorkflowID == "" || entry.Version == 0 || entry.Definition == nil {
			t.Fatalf("incomplete reference conformance entry: %+v", entry)
		}
		if seen[entry.ID] {
			t.Fatalf("duplicate reference conformance id %q", entry.ID)
		}
		seen[entry.ID] = true
		definition := entry.Definition()
		if definition.WorkflowID != entry.WorkflowID || definition.Version != entry.Version {
			t.Fatalf("%s catalog identity (%s/%d) differs from definition (%s/%d)", entry.ID, entry.WorkflowID, entry.Version, definition.WorkflowID, definition.Version)
		}
	}
	for _, id := range []string{"CONF-002", "CONF-003", "CONF-004"} {
		if !seen[id] {
			t.Fatalf("catalog omits %s", id)
		}
	}
}
