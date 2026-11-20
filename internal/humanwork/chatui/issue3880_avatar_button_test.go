package chatui

import (
	"strings"
	"testing"
)

// issue3880RuleBody returns the declarations of the first rule that begins with
// selector, or fails the test.
func issue3880RuleBody(t *testing.T, selector string) string {
	t.Helper()
	i := strings.Index(Stylesheet, selector+"{")
	if i < 0 {
		t.Fatalf("stylesheet lacks a %q rule", selector)
	}
	rest := Stylesheet[i+len(selector)+1:]
	return rest[:strings.Index(rest, "}")]
}

// The message row is a grid; without an explicit alignment the avatar button
// stretches to the full row height and track width, so its focus ring becomes
// a tall oval (#3880). The button must shrink to the avatar it holds.
func TestIssue3880_AvatarButtonDoesNotStretchWithRow(t *testing.T) {
	body := issue3880RuleBody(t, ".message>.person-avatar-button")
	for _, need := range []string{"align-self:start", "justify-self:start"} {
		if !strings.Contains(body, need) {
			t.Fatalf("avatar button rule %q lacks %q", body, need)
		}
	}
	// The avatar is 36px on desktop and 28px on a phone; a fixed pixel size
	// would break one of them, so the button follows the avatar.
	for _, forbidden := range []string{"inline-size:", "block-size:", "width:", "height:"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("avatar button rule %q fixes a size with %q; it must follow the avatar", body, forbidden)
		}
	}
}

// The button is only as large as the avatar, so the 44px touch target is the
// enlarged hit area drawn by its ::after element, which must stay.
func TestIssue3880_TouchTargetStaysAdequate(t *testing.T) {
	const selector = ".chat-workspace :is(.reaction,.message-stats,.mention-chip,.person-avatar-button,.attachment-download,.chat-doc-reference,.agent-reply-source-link,.agent-cite)::after"
	body := issue3880RuleBody(t, selector)
	for _, need := range []string{"position:absolute", "min-width:44px", "min-height:44px"} {
		if !strings.Contains(body, need) {
			t.Fatalf("avatar hit-area rule %q lacks %q", body, need)
		}
	}
}
