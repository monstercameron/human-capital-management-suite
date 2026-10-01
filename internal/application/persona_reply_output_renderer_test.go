package application

import (
	"strings"
	"testing"
)

func TestTodo_AGENTP_012_OutputRendererRejectsUnapprovedLinksAndEmbeds(t *testing.T) {
	policy := PersonaReplyOutputPolicy{TenantOrigin: "https://tenant.example"}
	got := renderPersonaReplyText("See [the report](https://evil.example/report), https://giphy.com/gifs/party-abc123, and ![pixel](https://tracker.example/p.png).", "tenant-a", "room-a", policy)
	for _, want := range []string{"the report", "and"} {
		if !strings.Contains(got, want) {
			t.Errorf("sanitized reply %q lost safe text %q", got, want)
		}
	}
	for _, forbidden := range []string{"evil.example", "giphy.com", "tracker.example", "![", "]("} {
		if strings.Contains(got, forbidden) {
			t.Errorf("sanitized reply %q retained forbidden syntax %q", got, forbidden)
		}
	}
}

func TestTodo_AGENTP_012_OutputRendererAllowsOnlyTrustedOrigins(t *testing.T) {
	policy := PersonaReplyOutputPolicy{TenantOrigin: "https://tenant.example", AdminAllowedOrigins: []string{"https://admin.example"}}
	got := renderPersonaReplyText("[tenant](https://tenant.example/docs) [admin](https://admin.example/help) [other](https://other.example/help)", "tenant-a", "room-a", policy)
	if !strings.Contains(got, "[tenant](https://tenant.example/docs)") || !strings.Contains(got, "[admin](https://admin.example/help)") {
		t.Fatalf("approved links were not retained: %q", got)
	}
	if strings.Contains(got, "other.example") || strings.Contains(got, "[other](") {
		t.Fatalf("unapproved link survived: %q", got)
	}
}

func TestTodo_AGENTP_012_OutputRendererWithoutOriginsLeavesPlainText(t *testing.T) {
	got := renderPersonaReplyText("A useful answer with https://tenant.example/private", "tenant-a", "room-a", PersonaReplyOutputPolicy{})
	if strings.Contains(got, "https://") || !strings.Contains(got, "A useful answer with") {
		t.Fatalf("empty policy should preserve plain text only: %q", got)
	}
}
