package ownerops

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func TestTodo_AGENT2_022(t *testing.T) {
	record := TaskRecord{Task: agentrun.AgentTask{ID: "task", TenantID: "tenant", UserID: "user", Goal: "private goal", FailureDetail: "private detail"}, OwnerID: "owner", AgentID: "agent", SpendMicros: 42, Stalled: true, Steps: []StepMetric{{StepID: "read", Latency: time.Second, Retries: 2, DenialCodes: []string{"GRANT_MISSING"}}}}
	for _, audience := range []Audience{AudienceMember, AudienceOwner, AudienceOperator} {
		subject := "owner"
		purpose := PurposeOwnerDashboard
		if audience == AudienceMember {
			subject = "user"
		}
		if audience == AudienceOperator {
			subject = "operator"
			purpose = PurposeOperatorOps
		}
		views, err := ProjectTasks(Scope{TenantID: "tenant", SubjectID: subject, Audience: audience, Purpose: purpose, Capabilities: []string{CapabilityRead}}, []TaskRecord{record})
		if err != nil || len(views) != 1 {
			t.Fatalf("%s: %+v %v", audience, views, err)
		}
		if views[0].SpendMicros != 42 || !views[0].Stalled || views[0].Steps[0].Retries != 2 {
			t.Fatalf("missing task metrics: %+v", views)
		}
		body, _ := json.Marshal(views)
		if audience == AudienceMember {
			if !strings.Contains(string(body), "private goal") {
				t.Fatal("own trace missing")
			}
		} else if strings.Contains(string(body), "private") {
			t.Fatalf("content leak %s", body)
		}
	}
}
func TestTodo_AGENT2_022_Security(t *testing.T) {
	record := TaskRecord{Task: agentrun.AgentTask{ID: "task", TenantID: "other", UserID: "user"}, OwnerID: "owner"}
	scope := Scope{TenantID: "tenant", SubjectID: "user", Audience: AudienceMember, Purpose: PurposeOwnerDashboard, Capabilities: []string{CapabilityRead}}
	views, err := ProjectTasks(scope, []TaskRecord{record})
	if err != nil || len(views) != 0 {
		t.Fatalf("cross tenant: %+v %v", views, err)
	}
	scope.TenantID = ""
	scope.Audience = AudienceOperator
	scope.Purpose = PurposeOperatorOps
	if _, err = ProjectTasks(scope, []TaskRecord{record}); !errors.Is(err, ErrDenied) {
		t.Fatalf("unscoped operator: %v", err)
	}
}
