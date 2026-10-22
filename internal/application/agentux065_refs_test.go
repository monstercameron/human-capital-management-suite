package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// Exact pins resolve the one labelled version and caret references resolve
// the newest compatible publication; both report the resolved label, and an
// unreadable document stays omitted whatever the mode (AGENTUX-065).
func TestAgentUX065_ResolveLabelModes(t *testing.T) {
	fake := &agentDocumentReadFake{
		labelPinned: map[string]map[string]agentDocumentVersion{
			agentDocumentIDPolicy: {"1.2.0": {number: 3, label: "1.2.0", title: "Policy", content: "# Leave\n\nExact text.\n"}},
		},
		compatible: map[string]agentDocumentVersion{
			agentDocumentIDHandbook: {number: 4, label: "1.4.2", title: "Team handbook", content: "# Intro\n\nFollowing text.\n"},
		},
		readable: map[string]bool{"alice\x00" + agentDocumentIDPolicy: true, "alice\x00" + agentDocumentIDHandbook: true},
	}
	resolver := newAgentDocumentResolver(fake)
	resolved, omitted, err := resolver.Resolve(context.Background(), agentdocref.Invoker{TenantID: "tenant-a", SubjectID: "alice"}, []agentdocref.Reference{
		{DocumentID: agentDocumentIDPolicy, VersionMode: agentdocref.ModePinnedLabel, PinnedLabel: "1.2.0", Label: "Exact policy"},
		{DocumentID: agentDocumentIDHandbook, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "1.0.0", Label: "Following handbook"},
	})
	if err != nil || len(omitted) != 0 || len(resolved) != 2 {
		t.Fatalf("resolved=%+v omitted=%+v err=%v", resolved, omitted, err)
	}
	if resolved[0].Version != 3 || resolved[0].VersionLabel != "1.2.0" || resolved[1].Version != 4 || resolved[1].VersionLabel != "1.4.2" {
		t.Fatalf("wrong labelled resolutions: %+v", resolved)
	}
	if fake.labelCalls != 1 || fake.compatibleCalls != 1 {
		t.Fatalf("resolver dispatch label=%d compatible=%d; want 1 each", fake.labelCalls, fake.compatibleCalls)
	}
	resolved, omitted, err = resolver.Resolve(context.Background(), agentdocref.Invoker{TenantID: "tenant-a", SubjectID: "mallory"}, []agentdocref.Reference{
		{DocumentID: agentDocumentIDPolicy, VersionMode: agentdocref.ModePinnedLabel, PinnedLabel: "1.2.0", Label: "Exact policy"},
		{DocumentID: agentDocumentIDHandbook, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "1.0.0", Label: "Following handbook"},
	})
	if err != nil || len(resolved) != 0 || len(omitted) != 2 {
		t.Fatalf("unreadable caret/pin resolved=%+v omitted=%+v err=%v", resolved, omitted, err)
	}
	for _, omission := range omitted {
		if omission.Reason != agentdocref.NotFound {
			t.Fatalf("unreadable omission reason = %q; want NOT_FOUND", omission.Reason)
		}
	}
}
