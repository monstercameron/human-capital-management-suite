package workspace_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestTodo_UXAUDIT_002_Browser is the BROWSER matrix test for
// planning/todos.md's UXAUDIT-002. By this repo's testing convention (see
// TestTodo_UX_002_Browser) it is a Go-side check of the live served
// document, so it needs no JS engine to run: the product shell's
// journey-config island carries the server-resolved semantic-action
// projection, and the promotion path is discoverable from the browser only
// when that island offers promote-worker to the authorized actor and
// withholds it from everyone else.
func TestTodo_UXAUDIT_002_Browser(t *testing.T) {
	serverURL := uxaudit014Cell(t)
	peoplePath := workspace.PathProductPrefix + "people"

	// The authorized actor (hiring-manager: the proposer in the
	// reference workflow) is offered exactly one promotion path.
	manager := uxaudit014Login(t, serverURL, "hiring-manager")
	status, island := uxaudit002Island(t, manager, serverURL, peoplePath)
	if status != http.StatusOK {
		t.Fatalf("GET %s as hiring-manager = %d, want 200", peoplePath, status)
	}
	managerActions := uxaudit002LauncherActions(island)
	if len(managerActions) != 1 || managerActions[0].ID != "promote-worker" {
		t.Fatalf("hiring-manager launcher actions = %+v, want exactly the one discoverable promote-worker path", managerActions)
	}
	if managerActions[0].Availability != "available" {
		t.Fatalf("promote-worker availability = %q, want available for the authorized actor", managerActions[0].Availability)
	}

	// The finance approver reviews assigned promotion work but must not be
	// offered initiation from the browser chrome.
	finance := uxaudit014Login(t, serverURL, "finance-partner")
	financeStatus, financeIsland := uxaudit002Island(t, finance, serverURL, peoplePath)
	if financeStatus == http.StatusOK {
		for _, action := range uxaudit002LauncherActions(financeIsland) {
			if action.ID == "promote-worker" && action.Availability == "available" {
				t.Errorf("finance-partner is offered an available promote-worker action: initiation authority leaks past the approval role")
			}
		}
	}

	// The individual contributor is offered no promotion path at all.
	contributor := uxaudit014Login(t, serverURL, "individual-contributor")
	contribStatus, contribIsland := uxaudit002Island(t, contributor, serverURL, peoplePath)
	if contribStatus == http.StatusOK {
		if actions := uxaudit002LauncherActions(contribIsland); len(actions) != 0 {
			t.Errorf("individual-contributor launcher actions = %+v, want none: the promotion path is not discoverable without authority", actions)
		}
	}

	// The island never reaches outside itself: the discoverability verdict
	// is this cell's own projection, not a reference to another origin.
	for name, body := range map[string]string{"hiring-manager": island, "finance-partner": financeIsland, "individual-contributor": contribIsland} {
		if body == "" {
			continue
		}
		if m := externalURLEnabledRE.FindString(body); m != "" {
			t.Errorf("%s island references %q; the verdict must not reach another origin", name, m)
		}
	}
}

// uxaudit002LauncherAction is the island's launcher verdict in the shape
// the served document carries it.
type uxaudit002LauncherAction struct {
	ID           string `json:"id"`
	Availability string `json:"availability"`
}

var uxaudit002IslandRE = regexp.MustCompile(`(?s)<script type="application/json" id="journey-config">(.*?)</script>`)

// uxaudit002Island GETs one product route as the logged-in client and
// returns the status and the raw journey-config island.
func uxaudit002Island(t *testing.T, client *http.Client, serverURL, path string) (int, string) {
	t.Helper()
	res, err := client.Get(serverURL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b strings.Builder
	buf := make([]byte, 32768)
	for {
		n, err := res.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, ""
	}
	m := uxaudit002IslandRE.FindStringSubmatch(b.String())
	if m == nil {
		t.Fatalf("GET %s carries no journey-config island", path)
	}
	return res.StatusCode, m[1]
}

// uxaudit002LauncherActions reads the island's launcher verdicts.
func uxaudit002LauncherActions(island string) []uxaudit002LauncherAction {
	if strings.TrimSpace(island) == "" {
		return nil
	}
	var config struct {
		LauncherActions []uxaudit002LauncherAction `json:"launcher_actions"`
	}
	if err := json.Unmarshal([]byte(island), &config); err != nil {
		return nil
	}
	return config.LauncherActions
}
