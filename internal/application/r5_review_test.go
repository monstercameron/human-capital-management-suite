package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentcost"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcoststore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// r5CountingOwners counts the ownership questions the cost report asks the
// database layer. The report already holds the person's owned agents, so it
// should ask none.
type r5CountingOwners struct {
	AgentCostOwners
	asked []string
}

func (c *r5CountingOwners) IsOwner(tenant, agentID, actor string) bool {
	c.asked = append(c.asked, agentID)
	return c.AgentCostOwners.IsOwner(tenant, agentID, actor)
}

func r5Run(t *testing.T, s *agentAccessServed, persona, task string, micros int64) {
	t.Helper()
	request := agentinvoke.RunRequest{TenantID: s.tenant, PersonaID: persona, ConversationID: "general", InvokerID: agentTestWorker}
	metered := s.services.Gate.meter(servedCostInner{micros: micros}, request)
	if _, err := metered.Execute(context.Background(), AgentModelExecutorRequest{Task: TrustedModelTask{TenantID: s.tenant, TaskID: task, AgentID: persona}, StepID: "answer"}); err != nil {
		t.Fatal(err)
	}
}

// The cost tab asked the database "is this mine?" once for every agent that ran
// anywhere in the tenant. A tenant with many agents made the owner wait for a
// round trip per agent on every load.
func TestR5_CostReportDoesNotAskOwnershipPerAgent(t *testing.T) {
	s := newAgentAccessServed(t)
	for i := 0; i < 5; i++ {
		r5Run(t, s, "someone-elses-"+strconv.Itoa(i), "task-other-"+strconv.Itoa(i), 1_000)
	}
	r5Run(t, s, "policy-helper", "task-mine", 2_000)
	counter := &r5CountingOwners{AgentCostOwners: s.services.HTTP.Owners}
	s.services.HTTP.Owners = counter
	var report AgentCostReportView
	s.ok(http.MethodGet, AgentCostPath+"/report", "owner", nil, &report)
	if len(report.Report.Runs) != 1 || report.Report.MonthToDateMicros != 2_000 {
		t.Fatalf("report = %+v", report.Report)
	}
	if len(counter.asked) != 0 {
		t.Fatalf("the report asked the database about %v", counter.asked)
	}
}

// A person's 80 percent notice is about one day. The same sentence the next
// day is a new notice, and yesterday's must not be shown as today's.
func TestR5_AnEightyPercentNoticeBelongsToItsDay(t *testing.T) {
	s := newAgentAccessServed(t)
	s.ok(http.MethodPost, AgentCostPath+"/limit", "owner", agentCostLimitBody{AgentID: "policy-helper", MaxRunsPerDay: 5}, nil)
	var report AgentCostReportView
	for i := 0; i < 4; i++ {
		r5Run(t, s, "policy-helper", "day1-"+strconv.Itoa(i), 1_000)
	}
	s.ok(http.MethodGet, AgentCostPath+"/report", "owner", nil, &report)
	if len(report.Report.Notices) != 1 {
		t.Fatalf("notices on the day of the 80 percent = %v, want one", report.Report.Notices)
	}
	s.offset = 24 * time.Hour
	s.ok(http.MethodGet, AgentCostPath+"/report", "owner", nil, &report)
	if len(report.Report.Notices) != 0 {
		t.Fatalf("yesterday's notice is shown today: %v", report.Report.Notices)
	}
	for i := 0; i < 4; i++ {
		r5Run(t, s, "policy-helper", "day2-"+strconv.Itoa(i), 1_000)
	}
	s.ok(http.MethodGet, AgentCostPath+"/report", "owner", nil, &report)
	if len(report.Report.Notices) != 1 {
		t.Fatalf("notices on the second day = %v, want the second day's own", report.Report.Notices)
	}
}

// A limit that would wrap the gate's arithmetic, or that is negative, is a bad
// request the page can name; it never reaches the database.
func TestR5_LimitSaveRefusesValuesThePageCannotMean(t *testing.T) {
	s := newAgentAccessServed(t)
	const path = AgentCostPath + "/limit"
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, path, "owner", agentCostLimitBody{AgentID: "policy-helper", MaxRunsPerDay: 9_000_000_000_000_000_000})
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, path, "owner", agentCostLimitBody{AgentID: "policy-helper", MaxSpendMicrosPerDay: 9_000_000_000_000_000_000})
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, path, "owner", agentCostLimitBody{AgentID: "policy-helper", MaxRunsPerDay: -1})
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, path, "owner", agentCostLimitBody{AgentID: "policy-helper", MaxSpendMicrosPerDay: -1})
	var views []AgentSpendLimitView
	s.ok(http.MethodPost, AgentCostPath+"/limits", "owner", agentCostLimitsBody{AgentIDs: []string{"policy-helper"}}, &views)
	if len(views) != 1 || views[0].MaxRunsPerDay != 0 || views[0].MaxSpendMicrosPerDay != 0 {
		t.Fatalf("a refused save changed the limit: %+v", views)
	}
}

// The same names in another tenant are not the owner of this tenant's agent, and
// an administrator there cannot read or change this tenant's limits.
func TestR5_AnotherTenantCannotReadOrChangeLimits(t *testing.T) {
	s := newAgentAccessServed(t)
	s.ok(http.MethodPost, AgentCostPath+"/limit", "owner", agentCostLimitBody{AgentID: "policy-helper", MaxRunsPerDay: 7}, nil)
	s.tokens["foreign-owner"] = servedAccessPrincipal(t, "other-tenant", servedAccessOwner, []string{"worker_self"}, trust.AssuranceSubstantial)
	s.tokens["foreign-admin"] = servedAccessPrincipal(t, "other-tenant", servedAccessAdminOne, []string{"hcm_admin"}, trust.AssuranceSubstantial)
	for _, token := range []string{"foreign-owner", "foreign-admin"} {
		status, body := s.call(http.MethodPost, AgentCostPath+"/limit", token, agentCostLimitBody{AgentID: "policy-helper", MaxRunsPerDay: 1})
		if status == http.StatusOK {
			t.Fatalf("%s changed a limit of another tenant: %s", token, body)
		}
		var views []AgentSpendLimitView
		status, body = s.call(http.MethodPost, AgentCostPath+"/limits", token, agentCostLimitsBody{AgentIDs: []string{"policy-helper"}})
		if status == http.StatusOK {
			if err := json.Unmarshal(body, &views); err != nil || (len(views) == 1 && views[0].MaxRunsPerDay != 0) {
				t.Fatalf("%s reads the limit of another tenant: %s", token, body)
			}
		}
	}
	var views []AgentSpendLimitView
	s.ok(http.MethodPost, AgentCostPath+"/limits", "owner", agentCostLimitsBody{AgentIDs: []string{"policy-helper"}}, &views)
	if len(views) != 1 || views[0].MaxRunsPerDay != 7 {
		t.Fatalf("the owner's limit after the other tenant's attempts = %+v", views)
	}
}

type r5FailingRunner struct{}

func (r5FailingRunner) RunTenantTx(context.Context, uuid.UUID, func(dbport.Tx) error) error {
	return errors.New("connection refused")
}

// When the agent database cannot be read, the owner is told it is unavailable
// (a card with Try again), not that they may not change their own agent.
func TestR5_OwnerLookupFailureIsUnavailableNotDenied(t *testing.T) {
	store, err := agentcoststore.New(r5FailingRunner{}, func(string) uuid.UUID { return uuid.New() })
	if err != nil {
		t.Fatal(err)
	}
	owners := agentAccessAgentOwners{store: store, admins: newAgentRequestAdmins()}
	gate, err := agentcost.NewGate(owners, nil, time.UTC, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	err = gate.Set("olive", agentcost.Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 3})
	if !errors.Is(err, agentcost.ErrUnavailable) || errors.Is(err, agentcost.ErrDenied) {
		t.Fatalf("Set = %v, want unavailable", err)
	}
	if status, code := agentAccessErrorStatus(err); status != http.StatusServiceUnavailable || code != "unavailable" {
		t.Fatalf("answered %d %s", status, code)
	}
}
