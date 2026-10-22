package agentdocref

import (
	"errors"
	"testing"
)

// A reference pins an exact label or follows compatible versions: exact
// pins name one label, caret references name the oldest label they accept,
// and every anchor is a plain semantic label (AGENTUX-065).
func TestAgentUX065_ReferenceLabelModes(t *testing.T) {
	valid := []Reference{
		{DocumentID: "doc-1", VersionMode: ModePinnedLabel, PinnedLabel: "1.2.0", Label: "Exact"},
		{DocumentID: "doc-2", VersionMode: ModeCompatible, CompatibleBase: "1.4.2", Label: "Following"},
	}
	if err := Validate(valid, MaxPersonaReferences); err != nil {
		t.Fatalf("label modes rejected: %v", err)
	}
	if got := valid[0].VersionSelector(); got != "=1.2.0" {
		t.Fatalf("exact selector = %q; want =1.2.0", got)
	}
	if got := valid[1].VersionSelector(); got != "^1.4.2" {
		t.Fatalf("caret selector = %q; want ^1.4.2", got)
	}
	compact := Reference{DocumentID: "doc-3", VersionMode: ModeCompatible, CompatibleBase: "1.0.0", Label: "Major line"}
	if got := compact.VersionSelector(); got != "^1" {
		t.Fatalf("caret selector = %q; want ^1", got)
	}
	bad := []Reference{
		{DocumentID: "doc-1", VersionMode: ModePinnedLabel, Label: "Missing anchor"},
		{DocumentID: "doc-1", VersionMode: ModePinnedLabel, PinnedLabel: "yesterday", Label: "Unparsable anchor"},
		{DocumentID: "doc-1", VersionMode: ModePinnedLabel, PinnedLabel: "1.2.0", PinnedVersion: 2, Label: "Two anchors"},
		{DocumentID: "doc-1", VersionMode: ModeCompatible, Label: "Missing base"},
		{DocumentID: "doc-1", VersionMode: ModeCompatible, CompatibleBase: "1.2", Label: "Short base"},
		{DocumentID: "doc-1", VersionMode: ModeCompatible, CompatibleBase: "1.4.2", PinnedLabel: "1.4.2", Label: "Two anchors"},
		{DocumentID: "doc-1", VersionMode: ModePinned, PinnedVersion: 1, PinnedLabel: "1.0.0", Label: "Label on sequence pin"},
		{DocumentID: "doc-1", VersionMode: ModeLatestPublished, CompatibleBase: "1.0.0", Label: "Base on latest"},
	}
	for _, ref := range bad {
		if err := Validate([]Reference{ref}, MaxPersonaReferences); err == nil {
			t.Fatalf("invalid reference accepted: %+v", ref)
		}
	}
	if err := Validate([]Reference{{DocumentID: "doc-1", VersionMode: ModePinnedLabel, Label: "x"}}, MaxPersonaReferences); !errors.Is(err, ErrPinnedLabelRequired) {
		t.Fatalf("missing exact anchor err=%v; want ErrPinnedLabelRequired", err)
	}
}
