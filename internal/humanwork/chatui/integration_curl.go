package chatui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// ConversationPostCurl returns shell-ready API examples for an installed
// agent to read and post in the given conversation. Credentials are always
// supplied by the person running the copied command.
func ConversationPostCurl(origin, conversationID string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || base == nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return "", fmt.Errorf("invalid chat API origin")
	}
	id := strings.TrimSpace(conversationID)
	if id == "" || len(id) > 200 || strings.ContainsAny(id, "\r\n\x00") {
		return "", fmt.Errorf("invalid conversation id")
	}
	idempotencyKey, err := newChatIdempotencyKey()
	if err != nil {
		return "", fmt.Errorf("generate chat idempotency key: %w", err)
	}
	conversation := strings.TrimRight(base.String(), "/") + "/v1/conversations/" + url.PathEscape(id)
	posts, events := conversation+"/posts", conversation+"/events"
	return strings.Join([]string{
		"# Requires installation in this conversation with chat.posts.read; posting also requires chat.posts.write.",
		"# Use a short-lived agent token from its authorized issuer, never a user token.",
		"export HCM_CHAT_TOKEN='<paste-agent-token-here>'",
		"",
		"# Conversation metadata",
		"curl --fail-with-body " + shellSingleQuote(conversation) + " \\",
		"  --header \"Authorization: Bearer ${HCM_CHAT_TOKEN}\"",
		"",
		"# Recent posts (newest first; use next_cursor for the next page)",
		"curl --fail-with-body --get " + shellSingleQuote(posts) + " \\",
		"  --header \"Authorization: Bearer ${HCM_CHAT_TOKEN}\" \\",
		"  --data-urlencode 'page_size=50'",
		"# For the next page, add: --data-urlencode \"cursor=${POSTS_NEXT_CURSOR}\"",
		"",
		"# Initial bounded event pull (max_events: 1..50; wait_ms: 1..5000)",
		"# The first request has no cursor. Keep the returned resume_cursor for later pulls.",
		"curl --fail-with-body --get " + shellSingleQuote(events) + " \\",
		"  --header \"Authorization: Bearer ${HCM_CHAT_TOKEN}\" \\",
		"  --data-urlencode 'max_events=20' \\",
		"  --data-urlencode 'wait_ms=1000'",
		"# For subsequent pulls, send the opaque cursor returned by the previous response:",
		"#   --data-urlencode \"resume_cursor=${EVENTS_RESUME_CURSOR}\"",
		"",
		"# Idempotent post (reuse this key when retrying)",
		"curl --fail-with-body --request POST " + shellSingleQuote(posts) + " \\",
		"  --header \"Authorization: Bearer ${HCM_CHAT_TOKEN}\" \\",
		"  --header \"Content-Type: application/json\" \\",
		"  --header \"Idempotency-Key: " + idempotencyKey + "\" \\",
		"  --data '{\"body\":\"Hello from an agent\"}'",
	}, "\n"), nil
}

func newChatIdempotencyKey() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return hex.EncodeToString(value[0:4]) + "-" + hex.EncodeToString(value[4:6]) + "-" + hex.EncodeToString(value[6:8]) + "-" + hex.EncodeToString(value[8:10]) + "-" + hex.EncodeToString(value[10:16]), nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `"'"'`) + "'"
}
