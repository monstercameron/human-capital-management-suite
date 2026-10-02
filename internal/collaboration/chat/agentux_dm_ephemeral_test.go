package chat

import (
	"strings"
	"testing"
)

func TestTodo_AGENTUX_032_DurableEphemeralCopyHasOneLabelledSourceLink(t *testing.T) {
	const link = "/chat/share/signed-source"
	got := durableEphemeralBody("  Approved answer.  ", link)
	if got != "Approved answer.\n\n[Open the original message](/chat/share/signed-source)" {
		t.Fatalf("durable body=%q", got)
	}
	if strings.Count(got, link) != 1 || strings.Contains(got, "Open the source conversation:") {
		t.Fatalf("durable body must contain one labelled backlink and no raw path: %q", got)
	}
}
