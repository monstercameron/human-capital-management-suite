package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// RED for REV-070-01: worker lifecycle status and worker type die
// at the row-to-summary boundary. WorkerRow carries both tokens,
// but createdWorkerSummary never copies them, WorkerSummary has
// no field to receive them, toWorker has nothing to forward, and
// productui.Person has no status or type field — so a terminated
// or contingent worker renders identically to an active
// permanent employee, with no way to filter, mark, or gate
// actions on that state.
func TestTodo_REV_070_01(t *testing.T) {
	// The shared vocabulary parses the stored tokens.
	if ParseLifecycleStatus("terminated") != LifecycleTerminated {
		t.Fatal("terminated token unparsed")
	}
	if ParseLifecycleStatus("ACTIVE") != LifecycleActive {
		t.Fatal("active token unparsed")
	}
	if ParseLifecycleStatus("on-leave") != LifecycleOnLeave {
		t.Fatal("on-leave token unparsed")
	}
	if ParseWorkerType("contractor") != WorkerTypeContractor {
		t.Fatal("contractor token unparsed")
	}
	if ParseWorkerType("EMPLOYEE") != WorkerTypeEmployee {
		t.Fatal("employee token unparsed")
	}

	active := Person{ID: "a", Name: "Active Ann", LifecycleStatus: "ACTIVE", WorkerType: "EMPLOYEE"}
	terminated := Person{ID: "t", Name: "Term Ted", LifecycleStatus: "TERMINATED", WorkerType: "EMPLOYEE"}
	onLeave := Person{ID: "l", Name: "Leave Lou", LifecycleStatus: "ON_LEAVE", WorkerType: "EMPLOYEE"}
	unreported := Person{ID: "u", Name: "Unknown Uma"}
	population := []Person{active, terminated, onLeave, unreported}

	// The directory defaults to active-only: terminated and
	// on-leave workers are excluded, active and unreported stay.
	query := BuildPeopleQuery("", "", "", "", "", 1, 10)
	if query.Status != PeopleStatusActiveOnly {
		t.Fatalf("directory default = %v, want active-only", query.Status)
	}
	kept := matchingPeople(population, query)
	for _, person := range kept {
		if person.ID == "t" || person.ID == "l" {
			t.Fatalf("default directory keeps %q", person.ID)
		}
	}
	if len(kept) != 2 {
		t.Fatalf("default directory keeps %d people, want 2", len(kept))
	}

	// Explicit opt-in shows terminated and on-leave workers.
	optIn := BuildPeopleQueryWithStatus("", "", "", "", "", 1, 10, "all")
	if len(matchingPeople(population, optIn)) != 4 {
		t.Fatal("opt-in directory hides workers")
	}
	optTerminated := BuildPeopleQueryWithStatus("", "", "", "", "", 1, 10, "terminated")
	ids := map[string]bool{}
	for _, person := range matchingPeople(population, optTerminated) {
		ids[person.ID] = true
	}
	if !ids["a"] || !ids["t"] || !ids["u"] || ids["l"] {
		t.Fatalf("terminated opt-in admits %v", ids)
	}

	// The worker identity header marks the state distinctly.
	locale := ResolveProductLocale("en-US")
	gone := ResolveWorkerIdentity(locale, terminated, nil)
	if gone.StatusLabel == "" {
		t.Fatal("terminated header carries no status")
	}
	staying := ResolveWorkerIdentity(locale, active, nil)
	if staying.StatusLabel != "" {
		t.Fatalf("active header carries status %q", staying.StatusLabel)
	}
	header, err := ui.RenderToString(ui.CreateElement(PersonProfileHeader, PersonHeroProps{
		Name: "Term Ted", Role: "DES2", StatusLabel: gone.StatusLabel,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(header, gone.StatusLabel) {
		t.Fatal("header omits the lifecycle state")
	}

	// Contextual discovery withholds workforce-change actions
	// from a terminated worker and keeps them for active ones.
	registry := []PersonWorkflow{
		{ID: "wf-transfer", Name: "Transfer", Category: "Mobility", Href: "/transfer", WorkforceChange: true},
		{ID: "wf-time", Name: "Time off", Category: "Time", Href: "/time"},
	}
	if actions := DiscoverWorkerActions(registry, terminated, ""); len(actions) != 1 || actions[0].ID != "wf-time" {
		t.Fatalf("terminated discovery = %+v", actions)
	}
	if actions := DiscoverWorkerActions(registry, active, ""); len(actions) != 2 {
		t.Fatalf("active discovery = %+v", actions)
	}
	if actions := DiscoverWorkerActions(registry, unreported, ""); len(actions) != 2 {
		t.Fatalf("unreported discovery = %+v", actions)
	}
}

// Golden: lifecycle vocabulary, status filters and labels.
func TestTodo_REV_070_01_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	var builder strings.Builder
	for _, token := range []string{"ACTIVE", "active", "TERMINATED", "terminated", "ON_LEAVE", "on-leave", "", "bogus"} {
		status := ParseLifecycleStatus(token)
		builder.WriteString(token + "\x00" + string(status) + "\x00")
		builder.WriteString(lifecycleDisplayLabel(locale, status, WorkerTypeEmployee) + "\x00")
	}
	for _, token := range []string{"EMPLOYEE", "employee", "CONTRACTOR", "INTERN", "TEMPORARY", "", "bogus"} {
		builder.WriteString(token + "\x00" + string(ParseWorkerType(token)) + "\x00")
	}
	for _, raw := range []string{"", "active", "terminated", "on-leave", "all", "bogus"} {
		builder.WriteString(raw + "\x00" + string(rune(ParsePeopleStatusFilter(raw))) + "\x00")
	}
	builder.WriteString(lifecycleDisplayLabel(locale, LifecycleActive, WorkerTypeContractor) + "\x00")
	builder.WriteString(lifecycleDisplayLabel(locale, LifecycleActive, WorkerTypeIntern) + "\x00")
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "2a127cd666bafd8b5df5c886de9d4121293528573454bd738a03cc2e0c7fc7b2"
	if got != want {
		t.Fatalf("lifecycle digest = %s, want %s", got, want)
	}
}
