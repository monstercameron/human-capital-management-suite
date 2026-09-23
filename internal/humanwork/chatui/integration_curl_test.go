package chatui

import (
	"strings"
	"testing"
)

func TestConversationPostCurlIncludesAgentReadWriteExamples(t *testing.T) {
	got, err := ConversationPostCurl("https://chat.example.test", "room-42")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://chat.example.test/v1/conversations/room-42",
		"https://chat.example.test/v1/conversations/room-42/posts",
		"https://chat.example.test/v1/conversations/room-42/events",
		"Authorization: Bearer ${HCM_CHAT_TOKEN}",
		"Content-Type: application/json",
		`--data '{"body":"Hello from an agent"}'`,
		"chat.posts.read",
		"chat.posts.write",
		"page_size=50",
		"cursor=${POSTS_NEXT_CURSOR}",
		"max_events=20",
		"wait_ms=1000",
		"resume_cursor=${EVENTS_RESUME_CURSOR}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("curl example missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "after_sequence") {
		t.Fatalf("event example uses the removed sequence offset: %s", got)
	}
	if !strings.Contains(got, "# The first request has no cursor.") {
		t.Fatalf("event example does not explain its initial cursor state: %s", got)
	}
	if !strings.Contains(got, `--header "Idempotency-Key: `) {
		t.Fatalf("curl example has no generated idempotency key: %s", got)
	}
	if strings.Contains(got, "Bearer ey") || strings.Contains(got, "current-user-secret") || strings.Contains(got, "localhost") {
		t.Fatalf("curl example contains a credential or local-only host: %s", got)
	}
}

func TestConversationPostCurlGeneratesFreshIdempotencyKey(t *testing.T) {
	first, err := ConversationPostCurl("https://chat.example.test", "room-42")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ConversationPostCurl("https://chat.example.test", "room-42")
	if err != nil {
		t.Fatal(err)
	}
	firstKey := curlIdempotencyKey(first)
	secondKey := curlIdempotencyKey(second)
	if firstKey == "" || secondKey == "" || firstKey == secondKey {
		t.Fatalf("generated keys = %q and %q; want distinct nonempty values", firstKey, secondKey)
	}
}

func curlIdempotencyKey(command string) string {
	const marker = `--header "Idempotency-Key: `
	start := strings.Index(command, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.Index(command[start:], `"`)
	if end < 0 {
		return ""
	}
	return command[start : start+end]
}

func TestConversationPostCurlQuotesAndEscapesConversationID(t *testing.T) {
	got, err := ConversationPostCurl("http://127.0.0.1:8868", "team/alpha's room")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `http://127.0.0.1:8868/v1/conversations/team%2Falpha%27s%20room/posts`) {
		t.Fatalf("conversation ID was not escaped in endpoint: %s", got)
	}
	if !strings.Contains(got, `curl --fail-with-body --request POST 'http://127.0.0.1:8868/v1/conversations/team%2Falpha%27s%20room/posts'`) {
		t.Fatalf("endpoint URL was not shell-quoted: %s", got)
	}
}

func TestConversationPostCurlRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct{ origin, id string }{
		{"", "room"},
		{"javascript:alert(1)", "room"},
		{"https://user:secret@chat.example.test", "room"},
		{"https://chat.example.test/path", "room"},
		{"https://chat.example.test?token=secret", "room"},
		{"https://chat.example.test", ""},
		{"https://chat.example.test", strings.Repeat("a", 201)},
		{"https://chat.example.test", "room\r\nX-Injected: true"},
	} {
		if _, err := ConversationPostCurl(tc.origin, tc.id); err == nil {
			t.Errorf("ConversationPostCurl(%q, %q) accepted invalid input", tc.origin, tc.id)
		}
	}
}
