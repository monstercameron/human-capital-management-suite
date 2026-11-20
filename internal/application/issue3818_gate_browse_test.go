package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

type fakeBrowseSummaries struct{ rows []chatstore.GateSummary }

func (f fakeBrowseSummaries) GateSummaries(context.Context, string, string) ([]chatstore.GateSummary, error) {
	return append([]chatstore.GateSummary(nil), f.rows...), nil
}

type fakeBrowseDirectory struct{ asked []string }

func (f *fakeBrowseDirectory) GateDirectory(context.Context, chatgate.Actor, chatgate.Scope) (map[string][]chatui.GateChoice, map[string]string, string, error) {
	return nil, nil, "", nil
}
func (f *fakeBrowseDirectory) GateReviewers(_ context.Context, _ chatgate.Actor, scope chatgate.Scope) ([]string, error) {
	f.asked = append(f.asked, scope.Conversation)
	return []string{"Mia Manager"}, nil
}

// TestIssue3818_BrowseNamesWhoARequestWaitsFor: the list of gates names the
// reviewers for a channel the person has a waiting request in, and asks for
// no other channel's reviewers.
func TestIssue3818_BrowseNamesWhoARequestWaitsFor(t *testing.T) {
	dir := &fakeBrowseDirectory{}
	app := &ChatgateApplication{Service: &chatgate.Service{}, Directory: dir, Summaries: fakeBrowseSummaries{rows: []chatstore.GateSummary{
		{Conversation: "sales", Questions: 1, Mode: "review", Request: "review"},
		{Conversation: "ops", Questions: 1, Mode: "review"},
		{Conversation: "hr", Questions: 1, Mode: "review", Request: "declined", RequestReason: "Full"},
	}}}
	reply, err := app.GateRequest(chatgateAs(t, "linh"), ChatgateRequest{Action: chatgateBrowseAction})
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := reply.Result.([]chatstore.GateSummary)
	if len(rows) != 3 || len(rows[0].Reviewers) != 1 || rows[0].Reviewers[0] != "Mia Manager" || len(rows[1].Reviewers) != 0 || len(rows[2].Reviewers) != 0 {
		t.Fatalf("rows: %+v", rows)
	}
	if len(dir.asked) != 1 || dir.asked[0] != "sales" {
		t.Fatalf("reviewers asked for %v", dir.asked)
	}
}
