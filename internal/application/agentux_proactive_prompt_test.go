package application

import (
	"context"
	"encoding/json"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"strings"
	"testing"
	"time"
)

func TestAgentUXProactive_PromptHasNoPersonalContext_Security(t *testing.T) {
	r := AgentAnnouncementRuntime{Now: func() time.Time { return time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC) }}
	goal, err := r.goal(context.Background(), agentstore.Announcement{Instruction: "Tell employees which company holidays are coming up.", OwnerID: "private-owner-subject", Zone: "America/New_York"})
	var fields map[string]string
	if err != nil || json.Unmarshal([]byte(goal), &fields) != nil {
		t.Fatalf("goal: %s %v", goal, err)
	}
	if fields["Today"] != "2026-10-01" || fields["TimeZone"] != "America/New_York" || fields["Format"] != agentAnnouncementFormatMessage || !strings.Contains(fields["Format"], "on or after Today") {
		t.Fatalf("served goal lost date, zone or format: %s", goal)
	}
	if strings.Contains(goal, "private-owner-subject") || strings.Contains(goal, "chat history") {
		t.Fatalf("personal context entered goal: %s", goal)
	}
	repair, err := r.goal(context.WithValue(context.Background(), announcementRepairKey{}, "no Source lines"), agentstore.Announcement{Instruction: "Post holidays", Zone: "UTC"})
	if err != nil || !strings.Contains(repair, "previous answer broke this rule: no Source lines") {
		t.Fatalf("repair rule missing: %s %v", repair, err)
	}
	if _, err := r.goal(context.Background(), agentstore.Announcement{Zone: "invalid"}); err == nil {
		t.Fatal("unknown zone accepted")
	}
}
