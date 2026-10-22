package main

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestTodo_CHATSEED_003_Browser reads the reset-and-seed run the way a person
// does: its receipt line names the tenant, the scale and nonzero counts, the
// history span parses and both its ends fall in office hours, and nothing
// internal (connection strings, hosts, table names) leaks into the output.
func TestTodo_CHATSEED_003_Browser(t *testing.T) {
	store, _ := chatSeedStore(t)
	ctx := context.Background()
	const tenant = "harborcare-demo"
	office := chatSeedOfficeZone(tenant)
	now := time.Date(2026, time.September, 27, 0, 40, 0, 0, office).UTC()
	opts := chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), AssetDir: "../../internal/humanwork/workspace/assets", Now: now, Reset: true}
	var out bytes.Buffer
	if err := runChatSeedCommand(ctx, store, nil, opts, &out); err != nil {
		t.Fatalf("reset then seed: %v", err)
	}
	receipt := regexp.MustCompile(`(?m)^seeded chat demo for (\S+) \((\S+)\): (\d+) conversations, (\d+) memberships, (\d+) posts`).FindStringSubmatch(out.String())
	if receipt == nil {
		t.Fatalf("no seed receipt in the output:\n%s", out.String())
	}
	if receipt[1] != tenant || receipt[2] != "small" {
		t.Fatalf("receipt names %s (%s), want harborcare-demo (small)", receipt[1], receipt[2])
	}
	for i, what := range []string{"conversations", "memberships", "posts"} {
		if receipt[3+i] == "0" {
			t.Fatalf("receipt counts zero %s:\n%s", what, out.String())
		}
	}
	span := regexp.MustCompile(`(?m)^history spans (\S+) to (\S+)$`).FindStringSubmatch(out.String())
	if span == nil {
		t.Fatalf("no history span in the output:\n%s", out.String())
	}
	for _, end := range span[1:] {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(end))
		if err != nil {
			t.Fatalf("history span end %q does not parse: %v", end, err)
		}
		if !isWorkingMoment(at, office) {
			t.Fatalf("history span end %s is outside 08:00 to 18:00 on a weekday", at.In(office).Format("Mon 2 Jan 15:04"))
		}
	}
	for _, leak := range []string{"postgres://", "hcmnext.local", "password", "chat_post", "chat_membership"} {
		if strings.Contains(out.String(), leak) {
			t.Fatalf("the receipt leaks %q:\n%s", leak, out.String())
		}
	}
}
