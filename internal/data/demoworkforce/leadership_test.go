package demoworkforce

import (
	"strings"
	"testing"
)

// isLeadershipGrade reports whether a grade is a management or executive one.
//
// The staffing catalog's grades are the only input: P grades are individual
// contributors, M grades manage, E grades are officers. Deriving leadership
// from the grade rather than from a hand-kept list of titles means a role
// added at an M or E grade is covered without anybody remembering to list it.
func isLeadershipGrade(grade string) bool {
	return strings.HasPrefix(grade, "M") || strings.HasPrefix(grade, "E")
}

// The staffing catalog's own shape, which everything below depends on: each
// unit's leader is the role at index 0, and no later role is a leadership
// one. staffRoleAt hands index 0 to position 0 alone, so a leadership role
// sitting anywhere else in the list would silently be handed out repeatedly
// again.
func TestStaffingListsTheLeadershipRoleFirstAndOnlyOnce(t *testing.T) {
	for _, group := range staffing {
		if len(group.Roles) == 0 {
			t.Fatalf("%s staffs no roles", group.Code)
		}
		if !isLeadershipGrade(group.Roles[0].Grade) {
			t.Errorf("%s: leading role %s is grade %s, which is not a leadership grade",
				group.Code, group.Roles[0].Code, group.Roles[0].Grade)
		}
		// The executive office is the leadership team itself: four distinct
		// officers, not one leader and three reports.
		if group.Code == "executive-office" {
			continue
		}
		for _, role := range group.Roles[1:] {
			if isLeadershipGrade(role.Grade) {
				t.Errorf("%s: role %s (%s) is a leadership grade but is not the unit's leading role",
					group.Code, role.Code, role.Grade)
			}
		}
	}
}

// staffRoleAt hands out index 0 exactly once however large the headcount is,
// and is otherwise the mapping it replaced. The regression it fixes is the
// second half: `position % len(roles)` wrapped a fifth hire in a three-role
// unit back onto the director.
func TestStaffRoleAtGivesPositionZeroTheOnlyLeadershipSeat(t *testing.T) {
	roles := []Role{
		{Code: "X-DIR", Grade: "M4"},
		{Code: "X-P4", Grade: "P4"},
		{Code: "X-P3", Grade: "P3"},
	}

	// The leading role is issued once across a headcount that exceeds the
	// list twice over.
	leaders := 0
	for position := range 8 {
		if staffRoleAt(roles, position).Code == "X-DIR" {
			leaders++
		}
	}
	if leaders != 1 {
		t.Errorf("a unit of 8 staffed from 3 roles issued the leading role %d times, want 1", leaders)
	}

	// Positions that fit the list are untouched, which is what keeps the job
	// codes, pay bands and promotion ladder unchanged for units whose
	// headcount never overflowed.
	for position := 1; position < len(roles); position++ {
		if got := staffRoleAt(roles, position); got.Code != roles[position].Code {
			t.Errorf("position %d = %s, want the unchanged mapping %s", position, got.Code, roles[position].Code)
		}
	}

	// Overflow repeats individual contributors in order.
	for _, tc := range []struct {
		position int
		want     string
	}{{3, "X-P4"}, {4, "X-P3"}, {5, "X-P4"}} {
		if got := staffRoleAt(roles, tc.position).Code; got != tc.want {
			t.Errorf("position %d = %s, want %s", tc.position, got, tc.want)
		}
	}

	// A single-role unit has nowhere to cycle to and keeps its one role.
	only := []Role{{Code: "Y-MGR", Grade: "M2"}}
	if got := staffRoleAt(only, 3).Code; got != "Y-MGR" {
		t.Errorf("single-role unit position 3 = %s, want Y-MGR", got)
	}
}

// Exactly one person leads each unit, and no leadership title is held twice.
// A directory showing two Directors of Clinical Operations, one reporting to
// the other, is the defect this guards.
func TestEachUnitHasExactlyOneLeader(t *testing.T) {
	employees := planOrFail(t)

	leadersByUnit := map[string][]string{}
	titlesByUnit := map[string]map[string][]string{}
	for _, employee := range employees {
		unit := employee.Organization.Code
		if isLeadershipGrade(employee.Row.Grade) {
			leadersByUnit[unit] = append(leadersByUnit[unit], employee.Row.WorkerKey)
		}
		if titlesByUnit[unit] == nil {
			titlesByUnit[unit] = map[string][]string{}
		}
		titlesByUnit[unit][employee.JobTitle] = append(titlesByUnit[unit][employee.JobTitle], employee.Row.WorkerKey)
	}

	for unit, leaders := range leadersByUnit {
		// The executive office is the officer team; every other unit has one
		// leader.
		want := 1
		if unit == "executive-office" {
			want = 4
		}
		if len(leaders) != want {
			t.Errorf("%s has %d leadership-grade workers (%v), want %d", unit, len(leaders), leaders, want)
		}
	}
	if len(leadersByUnit) != len(staffing) {
		t.Errorf("%d of %d staffed units have a leader at all", len(leadersByUnit), len(staffing))
	}

	// A repeated title is only legitimate for individual contributors: two
	// senior nurses on one team is a team, two directors of it is a bug.
	gradeByKey := map[string]string{}
	for _, employee := range employees {
		gradeByKey[employee.Row.WorkerKey] = employee.Row.Grade
	}
	for unit, titles := range titlesByUnit {
		for title, holders := range titles {
			if len(holders) < 2 {
				continue
			}
			if isLeadershipGrade(gradeByKey[holders[0]]) {
				t.Errorf("%s: %d people hold the leadership title %q (%v)", unit, len(holders), title, holders)
			}
		}
	}
}

// Every worker's manager exists, leads their unit, and sits at a grade that
// can plausibly manage them. The reporting line and the job catalog have to
// agree: a manager who holds their report's own title, or who sits below
// them, is a record nobody can read.
func TestEverySeededWorkerIsConsistentWithTheirManager(t *testing.T) {
	employees := planOrFail(t)

	byKey := make(map[string]Employee, len(employees))
	for _, employee := range employees {
		byKey[employee.Row.WorkerKey] = employee
	}

	// The unit leader is the worker at position 0, which is also the worker
	// every other member of the unit reports to.
	leaderOfUnit := map[string]string{}
	for _, employee := range employees {
		unit := employee.Organization.Code
		if _, seen := leaderOfUnit[unit]; !seen {
			leaderOfUnit[unit] = employee.Row.WorkerKey
		}
	}

	rootsSeen := 0
	for _, employee := range employees {
		key := employee.Row.WorkerKey
		managerKey := employee.ManagerKey

		if strings.HasPrefix(managerKey, "board:") {
			rootsSeen++
			if key != employees[0].Row.WorkerKey {
				t.Errorf("%s reports to the board, which only the chief executive does", key)
			}
			continue
		}

		manager, found := byKey[managerKey]
		if !found {
			t.Errorf("%s reports to %q, who is not a planned worker", key, managerKey)
			continue
		}
		if managerKey == key {
			t.Errorf("%s reports to themselves", key)
			continue
		}

		// Nobody is managed by somebody holding their own job.
		if manager.JobTitle == employee.JobTitle {
			t.Errorf("%s (%s) reports to %s, who holds the same title", key, employee.JobTitle, managerKey)
		}
		if manager.Row.JobCode == employee.Row.JobCode {
			t.Errorf("%s reports to %s under the same job code %s", key, managerKey, employee.Row.JobCode)
		}

		// A manager outranks their report. The executive office is the one
		// place peers report to a peer: the officers all sit at E7 under the
		// chief executive.
		managerRank, ok := gradeRank[manager.Row.Grade]
		if !ok {
			t.Errorf("manager %s holds unranked grade %q", managerKey, manager.Row.Grade)
			continue
		}
		workerRank, ok := gradeRank[employee.Row.Grade]
		if !ok {
			t.Errorf("%s holds unranked grade %q", key, employee.Row.Grade)
			continue
		}
		if employee.Organization.Code == "executive-office" {
			if managerRank < workerRank {
				t.Errorf("%s (%s) reports to %s at the lower grade %s", key, employee.Row.Grade, managerKey, manager.Row.Grade)
			}
			continue
		}
		if managerRank <= workerRank {
			t.Errorf("%s (%s) reports to %s at grade %s, which does not outrank them",
				key, employee.Row.Grade, managerKey, manager.Row.Grade)
		}

		// Within a unit, everybody reports to that unit's one leader.
		if manager.Organization.Code == employee.Organization.Code {
			if managerKey != leaderOfUnit[employee.Organization.Code] {
				t.Errorf("%s reports to %s inside %s, but the unit is led by %s",
					key, managerKey, employee.Organization.Code, leaderOfUnit[employee.Organization.Code])
			}
			if !isLeadershipGrade(manager.Row.Grade) {
				t.Errorf("%s is managed by %s, who is not at a leadership grade", key, managerKey)
			}
		}
	}
	if rootsSeen != 1 {
		t.Errorf("%d workers report to the board, want exactly the chief executive", rootsSeen)
	}
}

// The job-code catalog is unchanged by the leadership fix, which is what
// keeps the families and FLSA statuses classified in employment_facts.go, the
// pay bands derived from the same table, and the promotion ladder built on it
// all still covering exactly the roles the company staffs.
func TestLeadershipFixLeavesTheJobCodeCatalogIntact(t *testing.T) {
	staffed := map[string]bool{}
	for _, group := range staffing {
		for _, role := range group.Roles {
			staffed[role.Code] = true
		}
	}

	planned := map[string]bool{}
	for _, employee := range planOrFail(t) {
		planned[employee.Row.JobCode] = true
	}

	if len(staffed) != 47 {
		t.Fatalf("the staffing catalog declares %d job codes, want 47", len(staffed))
	}
	// Every staffed code is actually held by somebody, and nobody holds a
	// code the catalog does not declare. A role list longer than its unit's
	// headcount would leave a classified code nobody occupies.
	for code := range staffed {
		if !planned[code] {
			t.Errorf("job code %s is declared but nobody is planned into it", code)
		}
	}
	for code := range planned {
		if !staffed[code] {
			t.Errorf("job code %s is planned but not declared by the staffing catalog", code)
		}
		if JobFamilyFor(code) == "" {
			t.Errorf("job code %s is planned but has no job family", code)
		}
	}
}
