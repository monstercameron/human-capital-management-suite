//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"
)

func TestThreadLoadingDoesNotClaimThereAreNoReplies(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "room", ShowThread: true, ThreadParentID: "root", ThreadLoading: true,
		Conversations: []Conversation{{ID: "room", Name: "General", Kind: PublicChannel}},
		Messages:      []Message{{ID: "root", Author: "Ari", Body: "Question"}}}
	markup := render(t, m)
	for _, want := range []string{"Loading replies", `class="thread-loading"`, `aria-busy="true"`, `class="thread-loading-skeleton"`, `aria-hidden="true"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("thread loading state missing %q", want)
		}
	}
	if strings.Contains(markup, "No replies yet") {
		t.Fatalf("thread displayed a false empty state while loading")
	}
	if got := strings.Count(markup, `class="thread-loading-row"`); got != 2 {
		t.Fatalf("thread loading skeleton has %d reply rows, want 2", got)
	}
	m.ThreadLoading = false
	markup = render(t, m)
	if !strings.Contains(markup, "No replies yet") {
		t.Fatal("thread empty state missing after loading completed")
	}
	if strings.Contains(markup, `class="thread-loading"`) {
		t.Fatal("thread loading skeleton remained after loading completed")
	}
}

func TestChatLoadingSkeletonRespectsMotionAndDirection(t *testing.T) {
	css := Stylesheet
	for _, want := range []string{
		`.thread-loading{display:grid;gap:8px;min-block-size:112px`,
		`@media (prefers-reduced-motion:no-preference)`,
		`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"])`,
		`.chat-workspace[dir="rtl"]`,
		`@media(prefers-reduced-motion:reduce)`,
		`data-hcm-motion-preference="limited"`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("chat loading style missing %q", want)
		}
	}
}
