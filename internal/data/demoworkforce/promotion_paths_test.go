package demoworkforce

import (
	"strings"
	"testing"
)

// executiveTopOfLadder are the four E7 roles nothing in this company
// outranks. They publish no edge at all, which is a structural absence of a
// next role rather than an administrative refusal.
var executiveTopOfLadder = map[string]bool{
	"EXEC-CEO": true, "EXEC-COO": true, "EXEC-CTO": true, "EXEC-CPO": true,
}

// singleStepRoles are the roles this catalog gives exactly one plausible next
// step, with the reason. It is empty, and that is the point of the rung work
// in catalog_roles.go: before the principal individual-contributor steps and
// the division vice-presidencies existed, eleven roles published one target
// each, and for most of them the reason was a career cliff -- a senior
// designer whose only move was a directorship, a director with nothing
// between them and the C-suite.
//
// The map stays because a role genuinely can have one step (an executive
// seat's direct report, say). An entry here is a claim somebody has to write
// a reason for; its absence is what
// [TestPromotionPathsPublishTheTargetsEachJobActuallyHas] holds the catalog
// to.
var singleStepRoles = map[string]string{}

// TestPromotionPathsCoverEveryStaffedJobCode proves the ladder actually
// reaches the roles a real employee holds: PROMOUX-001's defect was that the
// live journey service's job-architecture catalog never mentioned any of
// this company's own job codes at all, so every seeded worker looked
// structurally ineligible for the same reason regardless of their real
// grade. Every job code appearing in staffing must appear as either a
// source or a target edge, except the executive roles at the top.
func TestPromotionPathsCoverEveryStaffedJobCode(t *testing.T) {
	edges := PromotionPaths()
	if len(edges) == 0 {
		t.Fatal("no promotion paths computed for the staffed job codes")
	}
	named := map[string]bool{}
	for _, edge := range edges {
		named[edge.SourceJobCode] = true
		named[edge.TargetJobCode] = true
	}
	for _, seat := range allRoles() {
		if named[seat.Role.Code] || executiveTopOfLadder[seat.Role.Code] {
			continue
		}
		t.Errorf("job code %s (%s) in unit %s is neither a ladder edge nor a declared top-of-ladder role",
			seat.Role.Code, seat.Role.Title, seat.Unit)
	}
}

// TestPromotionPathsPublishTheTargetsEachJobActuallyHas proves the shape of
// the published ladder: the executives publish none, the roles named in
// [singleStepRoles] publish exactly the one step they have, and every other
// staffed role publishes at least two so the target picker offers a real
// choice rather than a single forced destination.
func TestPromotionPathsPublishTheTargetsEachJobActuallyHas(t *testing.T) {
	targets := map[string]map[string]bool{}
	for _, edge := range PromotionPaths() {
		if targets[edge.SourceJobCode] == nil {
			targets[edge.SourceJobCode] = map[string]bool{}
		}
		if targets[edge.SourceJobCode][edge.TargetJobCode] {
			t.Fatalf("%s publishes %s twice", edge.SourceJobCode, edge.TargetJobCode)
		}
		targets[edge.SourceJobCode][edge.TargetJobCode] = true
	}
	for _, seat := range allRoles() {
		role := seat.Role
		count := len(targets[role.Code])
		switch {
		case executiveTopOfLadder[role.Code]:
			if count != 0 {
				t.Errorf("%s is the top of the company's ladder but publishes %d targets", role.Code, count)
			}
		case singleStepRoles[role.Code] != "":
			if count != 1 {
				t.Errorf("%s publishes %d targets; it is pinned to exactly one (%s)", role.Code, count, singleStepRoles[role.Code])
			}
		case count < 2:
			t.Errorf("%s (%s, unit %s) publishes %d promotion targets, want at least 2 or an entry in singleStepRoles",
				role.Code, role.Grade, seat.Unit, count)
		}
		if count > MaxPromotionTargets {
			t.Errorf("%s publishes %d promotion targets, more than the published maximum %d", role.Code, count, MaxPromotionTargets)
		}
	}
	for code := range singleStepRoles {
		if _, published := targets[code]; !published {
			t.Errorf("singleStepRoles pins %s, which publishes nothing at all", code)
		}
	}
}

// TestPromotionPathsOfferOnlyPlausibleMoves is the regression for the defect
// this pass closed: the ladder used to pick cross-function targets by pay gap
// alone, so a senior product designer was offered Director of Clinical
// Operations and Director of Quality & Safety, and a care role could be
// reached from an engineering one. Every published edge must now be one of
// the four moves a person could actually make.
func TestPromotionPathsOfferOnlyPlausibleMoves(t *testing.T) {
	byCode := map[string]ladderRole{}
	for _, role := range ladderRoles() {
		byCode[role.Role.Code] = role
	}
	crossFunction, leadership := 0, 0
	for _, edge := range PromotionPaths() {
		source, okSource := byCode[edge.SourceJobCode]
		target, okTarget := byCode[edge.TargetJobCode]
		if !okSource || !okTarget {
			t.Fatalf("edge %+v names a job code the staffing catalog does not hold", edge)
		}
		switch {
		case target.Family == source.Family:
		case target.Unit == source.Unit:
		case target.Division == source.Division && familiesAdjacent(source.Family, target.Family):
			crossFunction++
		case divisionLeadershipSeat(source, target):
			leadership++
			if source.Rank < gradeRank[divisionLeadershipGrade] {
				t.Errorf("%s (%s) is offered the division vice-presidency %s below %s",
					source.Role.Code, source.Role.Grade, target.Role.Code, divisionLeadershipGrade)
			}
			if divisionVicePresident[source.Division] != target.Role.Code {
				t.Errorf("%s (division %s) is offered %s, which is another division's vice-presidency",
					source.Role.Code, source.Division, target.Role.Code)
			}
		case executiveSeat(source, target):
			if source.Rank < gradeRank[executiveEntryGrade] {
				t.Errorf("%s (%s) is offered the C-suite seat %s below %s", source.Role.Code, source.Role.Grade, target.Role.Code, executiveEntryGrade)
			}
		default:
			t.Errorf("%s (%s, %s) is offered %s (%s, %s): the disciplines do not connect and the divisions differ",
				source.Role.Code, source.Family, source.Division, target.Role.Code, target.Family, target.Division)
		}
	}
	if crossFunction == 0 {
		t.Fatal("no cross-discipline edge is published at all; the rule under test would pass vacuously")
	}
	if leadership == 0 {
		t.Fatal("no division vice-presidency is published at all; the rung that closed the director cliff is missing")
	}
}

// TestPromotionPathsRefuseTheReportedImplausibleTargets pins the exact
// offers a reviewer flagged on the served propose form, so the defect cannot
// come back under a different ordering rule.
func TestPromotionPathsRefuseTheReportedImplausibleTargets(t *testing.T) {
	published := map[string]bool{}
	for _, edge := range PromotionPaths() {
		published[edge.SourceJobCode+"->"+edge.TargetJobCode] = true
	}
	for _, refused := range []struct{ edge, why string }{
		{"PRD-UX3->CLN-DIR", "a product designer is not offered a clinical directorship"},
		{"PRD-UX3->QLT-DIR", "a product designer is not offered a patient-safety directorship"},
		{"PRD-UX3->ENG-SWE4", "a product designer is not offered a staff engineering role"},
		{"ENG-QA3->CLN-DIR", "an engineer is not offered a clinical directorship"},
		{"ENG-SWE3->CARE-MGR", "an engineer is not offered a care coordination role"},
		{"CARE-CC2->CLN-RN2", "a care coordinator does not become a licensed nurse"},
		{"WRK-CO2->CLN-RN2", "a workplace coordinator does not become a licensed nurse"},
		{"IT-SA2->CARE-MGR", "a systems administrator is not offered a care coordination role"},
		{"ENG-SWE3->EXEC-CTO", "a senior engineer is not offered the chief technology officer's seat as a next step"},
		{"MKT-CNT3->CLN-DIR", "a marketer is not offered a clinical directorship"},
		{"CLN-DIR->PT-VP", "a clinical director is not offered the technology division's vice-presidency"},
		{"ENG-DIR->CARE-VP", "an engineering director is not offered the care division's vice-presidency"},
		{"MKT-DIR->CORP-VP", "a marketing director is not offered the corporate division's vice-presidency"},
		{"CARE-CC2->CARE-VP", "a care coordinator is not offered a division vice-presidency"},
		{"ENG-QA3->PT-VP", "a quality engineer is not offered a division vice-presidency"},
	} {
		if published[refused.edge] {
			t.Errorf("%s is published: %s", refused.edge, refused.why)
		}
	}
}

// TestPromotionLaddersOfRepresentativeRoles pins the whole published ladder
// of one designer, one nurse, one engineer and one coordinator, in order. A
// rule change that reintroduces an implausible target, or that reorders the
// picker so the nearest step is no longer first, fails here rather than in
// the demo.
func TestPromotionLaddersOfRepresentativeRoles(t *testing.T) {
	ladders := map[string][]string{}
	for _, edge := range PromotionPaths() {
		ladders[edge.SourceJobCode] = append(ladders[edge.SourceJobCode], edge.TargetJobCode)
	}
	for _, want := range []struct {
		source  string
		who     string
		targets []string
	}{
		{"PRD-UX3", "senior product designer", []string{"PRD-UX4", "PRD-DIR"}},
		{"PRD-UX4", "principal product designer", []string{"PRD-DIR", "PT-VP"}},
		{"CLN-RN2", "registered nurse", []string{"CLN-RN3", "CLN-NP2", "CLN-DIR", "CARE-MGR"}},
		{"ENG-QA3", "quality engineer", []string{"ENG-SWE3", "ENG-SRE3", "ENG-SWE4", "ENG-DIR"}},
		{"CARE-CC2", "care coordinator", []string{"CARE-CC3", "CARE-CC4", "CARE-MGR", "QLT-DA3"}},
		{"CARE-CC3", "senior care coordinator", []string{"CARE-CC4", "CARE-MGR"}},
		{"CLN-DIR", "director of clinical operations", []string{"CARE-VP", "EXEC-COO"}},
		{"PPL-HRBP3", "senior people partner", []string{"PPL-HRBP4", "PPL-DIR"}},
		{"WRK-MGR", "workplace services manager", []string{"WRK-SR", "PPL-DIR"}},
		{"IT-SA2", "IT systems administrator", []string{"SEC-SE3", "SEC-DIR", "ENG-SWE3", "ENG-SRE3"}},
		{"FIN-ACC3", "senior accountant", []string{"FIN-DIR", "LEG-CMP3"}},
	} {
		got := ladders[want.source]
		if strings.Join(got, ",") != strings.Join(want.targets, ",") {
			t.Errorf("the %s (%s) is offered %v, want %v", want.who, want.source, got, want.targets)
		}
	}
}

// TestPromotionPathsAreDeterministicAndUpward proves the ladder is pure
// (repeated calls agree) and every edge is a genuine upward move: the target
// outranks the source, pays strictly more (so the target band's minimum is
// above the source band's), and stays inside the pay window a proposal can
// actually be made in.
func TestPromotionPathsAreDeterministicAndUpward(t *testing.T) {
	first, second := PromotionPaths(), PromotionPaths()
	if len(first) != len(second) {
		t.Fatalf("PromotionPaths is nondeterministic: %d edges then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("PromotionPaths edge %d differs between calls: %+v vs %+v", i, first[i], second[i])
		}
	}
	pay, unit := map[string]int64{}, map[string]string{}
	for _, role := range ladderRoles() {
		pay[role.Role.Code], unit[role.Role.Code] = role.Cents, role.Unit
	}
	for _, edge := range first {
		if edge.SourceJobCode == edge.TargetJobCode {
			t.Fatalf("edge %+v targets its own source", edge)
		}
		sourceRank, targetRank := gradeRank[edge.SourceGrade], gradeRank[edge.TargetGrade]
		if targetRank <= sourceRank {
			t.Fatalf("edge %+v does not move to a strictly higher grade (source rank %d, target rank %d)", edge, sourceRank, targetRank)
		}
		if edge.TargetTitle == "" {
			t.Fatalf("edge %+v has no target title", edge)
		}
		source, target := pay[edge.SourceJobCode], pay[edge.TargetJobCode]
		if target <= source {
			t.Fatalf("edge %+v does not raise the band minimum: source pays %d cents, target %d", edge, source, target)
		}
		if demoTargetPayDenominator*target > demoTargetPayNumerator*source {
			t.Fatalf("edge %+v leaves the reachable pay window: source %d cents, target %d", edge, source, target)
		}
		// The kind is the job-architecture relationship, keyed by job
		// family rather than by organization unit: a product designer moving
		// to director of product stays in one unit and crosses a family.
		wantKind := PromotionKindUpward
		if JobFamilyFor(edge.TargetJobCode) != JobFamilyFor(edge.SourceJobCode) {
			wantKind = PromotionKindCrossFamily
		}
		if edge.Kind != wantKind {
			t.Fatalf("edge %+v declares kind %q, want %q", edge, edge.Kind, wantKind)
		}
		if unit[edge.SourceJobCode] != edge.OrgUnit {
			t.Fatalf("edge %+v is published under a unit its source does not belong to", edge)
		}
	}
}

// TestDivisionsResolveEveryOrganizationUnit proves the division lookup the
// cross-discipline rule leans on covers the whole organization, so a unit
// cannot silently resolve to an empty division and make every cross-family
// move look same-division.
func TestDivisionsResolveEveryOrganizationUnit(t *testing.T) {
	division := divisions()
	if len(division) != len(HarborCare.Units) {
		t.Fatalf("divisions resolved %d units, want %d", len(division), len(HarborCare.Units))
	}
	for _, unit := range HarborCare.Units {
		resolved, ok := division[unit.Code]
		if !ok || resolved == "" {
			t.Errorf("unit %s resolves to no division", unit.Code)
		}
	}
	for _, want := range []struct{ unit, division string }{
		{"clinical-operations", "care-operations"},
		{"care-coordination", "care-operations"},
		{"quality-safety", "care-operations"},
		{"engineering-platform", "product-technology"},
		{"product-management", "product-technology"},
		{"security-it", "product-technology"},
		{"sales", "growth-customer"},
		{"finance", "corporate-services"},
		{"legal-compliance", "corporate-services"},
		{"executive-office", "harborcare"},
		{"harborcare", "harborcare"},
	} {
		if got := division[want.unit]; got != want.division {
			t.Errorf("unit %s resolves to division %q, want %q", want.unit, got, want.division)
		}
	}
}

// TestAdjacentFamiliesAreRealDisciplines proves the adjacency table names
// only families the job catalog actually publishes, so a typo cannot quietly
// disable a connection and shrink somebody's ladder.
func TestAdjacentFamiliesAreRealDisciplines(t *testing.T) {
	families := map[string]bool{}
	codes := map[string]bool{}
	for _, seat := range allRoles() {
		families[JobFamilyFor(seat.Role.Code)] = true
		codes[seat.Role.Code] = true
	}
	for family, neighbours := range adjacentFamily {
		if !families[family] {
			t.Errorf("adjacentFamily declares %q, which no job code belongs to", family)
		}
		for _, neighbour := range neighbours {
			if !families[neighbour] {
				t.Errorf("adjacentFamily points %q at %q, which no job code belongs to", family, neighbour)
			}
			if neighbour == family {
				t.Errorf("adjacentFamily points %q at itself", family)
			}
		}
	}
	for family, seats := range executiveStep {
		if !families[family] {
			t.Errorf("executiveStep declares %q, which no job code belongs to", family)
		}
		for _, seat := range seats {
			if !codes[seat] {
				t.Errorf("executiveStep points %q at %q, which is not a staffed job code", family, seat)
			}
		}
	}
}

// TestExactCentsRefusesInexactAmounts proves the base-pay reader refuses
// anything it would have to round, which is what keeps a seeded salary and
// its band midpoint from landing a cent apart.
func TestExactCentsRefusesInexactAmounts(t *testing.T) {
	if cents, ok := exactCents("118000.00"); !ok || cents != 11800000 {
		t.Fatalf("exactCents(118000.00) = %d, %v", cents, ok)
	}
	for _, amount := range []string{"118000", "118000.0", "118000.000", "-1.00", "x.00", "118000.0x"} {
		if _, ok := exactCents(amount); ok {
			t.Errorf("exactCents(%q) was accepted", amount)
		}
	}
}

// TestPayZonesMatchesLocationTable proves PayZones is the exact distinct set
// used by the location table, not a hand-maintained list that can drift from
// it.
func TestPayZonesMatchesLocationTable(t *testing.T) {
	zones := PayZones()
	if len(zones) == 0 {
		t.Fatal("no pay zones computed")
	}
	want := map[string]bool{}
	for _, location := range locations {
		want[location.Zone] = true
	}
	if len(zones) != len(want) {
		t.Fatalf("PayZones returned %d zones, want %d distinct zones", len(zones), len(want))
	}
	for i, zone := range zones {
		if !want[zone] {
			t.Fatalf("PayZones returned %q, which is not in the location table", zone)
		}
		if i > 0 && zones[i-1] >= zone {
			t.Fatalf("PayZones is not sorted: %q before %q", zones[i-1], zone)
		}
	}
}
