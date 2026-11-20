package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func TestIssue3945_HTTPCarriesTheOrderAndRefusesAnUnknownOne(t *testing.T) {
	f := &chatsearchHTTPFixture{}
	h := chatsearchTestHTTP(t, f, nil)
	if w := chatsearchHTTPCall(h, "POST", ChatSearchPath, `{"Query":"deadline sort:oldest","Order":"oldest","Limit":20}`, "fixture"); w.Code != 200 || f.q.Order != "oldest" || f.q.Query != "deadline sort:oldest" {
		t.Fatalf("order not carried: %d %s %+v", w.Code, w.Body, f.q)
	}
	h = chatsearchTestHTTP(t, portFunc(t), nil)
	if w := chatsearchHTTPCall(h, "POST", ChatSearchPath, `{"Query":"deadline","Order":"sideways"}`, "fixture"); w.Code != 400 {
		t.Fatalf("unknown order = %d %s", w.Code, w.Body)
	}
}

func portFunc(t *testing.T) ChatSearchPort {
	t.Helper()
	return chatsearch.NewRegistry()
}
