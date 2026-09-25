package demoworkforce

import (
	"sort"
	"strconv"
	"strings"
)

// gradeRank orders every grade this company's staffing catalog uses, lowest
// first. It exists once, here, so the ladder below and any caller that needs
// to compare two grades read the same ranking rather than each inventing
// its own.
var gradeRank = map[string]int{
	"P2": 1, "P3": 2, "P4": 3, "P5": 4,
	"M2": 5, "M3": 6, "M4": 7, "M5": 8,
	"E6": 9, "E7": 10,
}

// Published edge kinds, matching internal/domains/jobarch's own vocabulary.
const (
	PromotionKindUpward      = "UPWARD"
	PromotionKindCrossFamily = "CROSS_FAMILY"
)

// PromotionPathEdge is one deterministic career-ladder edge published among
// this demo company's own roles.
//
// A role publishes up to [MaxPromotionTargets] targets rather than the single
// next role it used to: a real ladder offers a senior-IC step, a management
// step where the unit has one, and a move into a neighbouring function, and a
// catalog that offers exactly one destination cannot tell a genuine refusal
// apart from a catalog with nothing else in it.
//
// Every edge is a strictly upward move in both grade and pay, stays inside
// the reachable pay window, and is a move somebody in the source role could
// actually make -- see [promotionTargets] and [publishableTarget] for the
// three rules and why the last one exists.
//
// A role with nothing plausible above it publishes nothing, and a role with
// exactly one plausible next step publishes one. That is a structural
// property of a 47-role catalog with a flat grade ladder, not an
// administrative refusal, and the two must stay distinguishable to whatever
// reads this catalog.
type PromotionPathEdge struct {
	// OrgUnit is the organization unit whose workers this edge applies to --
	// the source role's own unit. It is what scopes the edge for the ladder
	// gate and what decides where the target's vacancies are opened.
	OrgUnit string
	// Kind states how the two roles relate in the JOB ARCHITECTURE, which is
	// keyed by job family ([JobFamilyFor]) and not by organization unit:
	// UPWARD is a higher-graded role in the same family, CROSS_FAMILY a move
	// into another one. That is internal/domains/jobarch's own meaning, and
	// it is the meaning the job catalog's own job_family column carries, so
	// one tenant never publishes two readings of "family".
	Kind                                     string
	SourceJobCode, SourceGrade               string
	TargetJobCode, TargetGrade, TargetTitle  string
	MinimumBaseIncrease, MaximumBaseIncrease string
}

// These illustrative HarborCare ladder bounds are authored as exact decimal
// fractions, not inferred from an employee's current pay or a target role's
// example salary. Compensation preflight still applies its separate checks.
const (
	demoMinimumBaseIncrease = "0.0500"
	demoMaximumBaseIncrease = "0.5000"
)

// PromotionLadderVersion pins this published ladder, so a stored edge can say
// which authored ladder it came from.
const PromotionLadderVersion = "harborcare.promotion_ladder/2026.09.1"

// MaxPromotionTargets bounds how many targets one role publishes. Four is
// enough to carry the nearest IC step, a second IC option or staff step, the
// unit's management step and one cross-function move, which is the shape a
// reader recognizes as a career ladder.
const MaxPromotionTargets = 4

// A target must be reachable under the published rules, not merely senior.
// The ladder admits a base increase between demoMinimumBaseIncrease and
// demoMaximumBaseIncrease, and the target's own band (pay_bands.go) runs from
// 80% to 120% of its published base, so a proposal exists only when
// [1.05 x source, 1.50 x source] meets [0.80 x target, 1.20 x target]. That
// holds for every target paying at most 1.875 x the source; the ladder stops
// at 7/4 (1.75) so the admissible window is never a single boundary cent.
const (
	demoTargetPayNumerator   = 7
	demoTargetPayDenominator = 4
)

// ladderRole is one staffed role with everything an edge is judged on: where
// it sits in the organization, which discipline it belongs to, its grade rank
// and its published base pay in exact cents.
type ladderRole struct {
	Unit     string
	Division string
	Family   string
	Role     Role
	Rank     int
	Cents    int64
}

// adjacentFamily declares which disciplines a career actually moves between
// in this company. It is directed, because plausibility is: a nurse moves
// into care coordination or patient safety, but a care coordinator does not
// become a registered nurse without a licence nobody recorded.
//
// An edge across two families is published only when the pair appears here
// AND both roles sit in the same division (see [publishableTarget]). Without
// that rule the ladder picked cross-function targets by pay gap alone, which
// is how a senior product designer came to be offered Director of Clinical
// Operations: an option no reviewer would believe and the commit would still
// have had to honour.
var adjacentFamily = map[string][]string{
	// Care operations: clinical practice feeds coordination and safety, and
	// coordination and safety feed each other. Neither feeds back into
	// licensed nursing.
	"Nursing & Clinical Practice": {"Care Coordination", "Quality & Patient Safety"},
	"Care Coordination":           {"Quality & Patient Safety"},
	"Quality & Patient Safety":    {"Care Coordination"},

	// Product and technology: the engineering, data, security and platform
	// disciplines share a craft, and design and product management are the
	// classic pair.
	"Software Engineering": {"Data & Analytics", "Information Security", "Product Management"},
	"Data & Analytics":     {"Software Engineering"},
	"Information Security": {"Software Engineering", "IT Operations"},
	"IT Operations":        {"Information Security", "Software Engineering"},
	"Product Management":   {"Product Design", "Software Engineering"},
	"Product Design":       {"Product Management"},

	// Growth and customer: the commercial disciplines all sell to or keep
	// the same customers.
	"Customer Success":  {"Sales", "Sales Engineering"},
	"Sales":             {"Customer Success", "Sales Engineering"},
	"Sales Engineering": {"Sales", "Customer Success"},
	"Marketing":         {"Sales", "Customer Success"},

	// Corporate services: people, workplace, compliance and finance share
	// the governance and employment side of the company.
	"Human Resources":      {"Workplace Services", "Legal & Compliance"},
	"Workplace Services":   {"Human Resources"},
	"Legal & Compliance":   {"Human Resources", "Finance & Accounting"},
	"Finance & Accounting": {"Legal & Compliance"},
}

// executiveStep names the C-suite seats a discipline's own leadership feeds.
// Every function head can step into the company's general operating seat; the
// other entry is the seat that owns their function, where one exists. It
// applies only to a source already at [executiveEntryGrade] or above, so a
// senior engineer is never offered the chief technology officer's job as a
// next step.
var executiveStep = map[string][]string{
	"Software Engineering": {"EXEC-CTO", "EXEC-COO"},
	"Data & Analytics":     {"EXEC-CTO", "EXEC-COO"},
	"Information Security": {"EXEC-CTO", "EXEC-COO"},
	"IT Operations":        {"EXEC-CTO", "EXEC-COO"},
	"Product Management":   {"EXEC-CTO", "EXEC-COO"},
	"Product Design":       {"EXEC-CTO", "EXEC-COO"},

	"Human Resources":    {"EXEC-CPO", "EXEC-COO"},
	"Workplace Services": {"EXEC-CPO", "EXEC-COO"},

	"Nursing & Clinical Practice": {"EXEC-COO"},
	"Care Coordination":           {"EXEC-COO"},
	"Quality & Patient Safety":    {"EXEC-COO"},
	"Customer Success":            {"EXEC-COO"},
	"Sales":                       {"EXEC-COO"},
	"Sales Engineering":           {"EXEC-COO"},
	"Marketing":                   {"EXEC-COO"},
	"Finance & Accounting":        {"EXEC-COO"},

	"Legal & Compliance": {"LEG-GC"},

	// A division vice-president's own next seat. The C-suite has no
	// technology-flavoured general-management seat, so the two the company
	// does have are the ones a VP steps into.
	"General Management": {"EXEC-COO", "EXEC-CEO"},
}

// divisionVicePresident names the vice-president seat each division
// publishes. It is the rung this catalog was missing: without it a
// department director's only move was straight into the C-suite, so six of
// them published exactly one target and the ladder read as a cliff.
//
// The seat is reachable from the division's own senior roles and from nobody
// else's -- the match is on division, so a care director is never offered the
// technology division's vice-presidency.
var divisionVicePresident = map[string]string{
	"care-operations":    "CARE-VP",
	"product-technology": "PT-VP",
	"growth-customer":    "GC-VP",
	"corporate-services": "CORP-VP",
}

// divisionLeadershipGrade is the lowest grade offered its division's
// vice-presidency: the division's senior people, which here means a
// principal individual contributor, a manager or a director. Below it the
// seat is two tiers away and the offer would not be believable.
const divisionLeadershipGrade = "P5"

// executiveEntryGrade is the lowest grade that may be offered a C-suite seat:
// this company's function heads, and nobody below them.
const executiveEntryGrade = "M4"

// publishableTarget reports whether target is a career move somebody holding
// source could actually make. Grade, pay and reachability are checked
// separately (see [promotionTargets]); this answers only "does this move make
// sense for this person".
func (p *Pack) publishableTarget(source, target ladderRole) bool {
	switch {
	case target.Family == source.Family:
		// Stepping up inside your own discipline, wherever it is recorded.
		return true
	case target.Unit == source.Unit:
		// Your own team's next role, even when it is filed under a
		// neighbouring discipline: a product designer's director of product,
		// a compliance manager's general counsel.
		return true
	case target.Division == source.Division && p.familiesAdjacent(source.Family, target.Family):
		// A neighbouring discipline inside the same division.
		return true
	case p.divisionLeadershipSeat(source, target):
		// The division's own vice-presidency.
		return true
	default:
		return p.executiveSeat(source, target)
	}
}

// divisionLeadershipSeat reports whether target is the vice-president seat
// source's own division publishes, and whether source is senior enough to be
// offered it.
func divisionLeadershipSeat(source, target ladderRole) bool {
	return HarborCarePack.divisionLeadershipSeat(source, target)
}

func (p *Pack) divisionLeadershipSeat(source, target ladderRole) bool {
	if p.divisionLeadershipGrade == "" || source.Rank < p.gradeRank[p.divisionLeadershipGrade] {
		return false
	}
	seat, published := p.divisionVicePresident[source.Division]
	return published && seat == target.Role.Code && target.Role.Code != source.Role.Code
}

// familiesAdjacent reports whether a move from one discipline to the other is
// declared in [adjacentFamily].
func familiesAdjacent(from, to string) bool { return HarborCarePack.familiesAdjacent(from, to) }

func (p *Pack) familiesAdjacent(from, to string) bool {
	for _, candidate := range p.adjacentFamily[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// executiveSeat reports whether target is a C-suite seat source's own
// function feeds, and whether source is senior enough to be offered it.
func executiveSeat(source, target ladderRole) bool {
	return HarborCarePack.executiveSeat(source, target)
}

func (p *Pack) executiveSeat(source, target ladderRole) bool {
	if p.executiveEntryGrade == "" || source.Rank < p.gradeRank[p.executiveEntryGrade] {
		return false
	}
	for _, seat := range p.executiveStep[source.Family] {
		if seat == target.Role.Code {
			return true
		}
	}
	return false
}

// divisions maps every organization unit to the division it reports into, so
// a cross-discipline move can be held to the part of the company it happens
// in. A unit with no division above it -- the executive office -- resolves to
// the company root, which no staffed role belongs to, so it never widens
// anybody's ladder.
func divisions() map[string]string { return HarborCarePack.divisions() }

func (p *Pack) divisions() map[string]string {
	units := make(map[string]OrganizationUnit, len(p.Company.Units))
	for _, unit := range p.Company.Units {
		units[unit.Code] = unit
	}
	out := make(map[string]string, len(p.Company.Units))
	for _, unit := range p.Company.Units {
		code, seen := unit.Code, map[string]bool{}
		for code != "" && !seen[code] {
			seen[code] = true
			current, ok := units[code]
			if !ok {
				break
			}
			if current.Type == "DIVISION" || current.ParentCode == "" {
				break
			}
			code = current.ParentCode
		}
		out[unit.Code] = code
	}
	return out
}

// PromotionPaths computes the demo company's career ladder from its own
// staffing catalog. It is pure and deterministic: no clock, no I/O, no
// randomness, so two calls in the same build always agree.
func PromotionPaths() []PromotionPathEdge { return HarborCarePack.PromotionPaths() }

// PromotionPaths computes this company's career ladder.
func (p *Pack) PromotionPaths() []PromotionPathEdge {
	roles := p.ladderRoles()
	edges := make([]PromotionPathEdge, 0, len(roles)*MaxPromotionTargets)
	for _, source := range roles {
		for _, target := range p.promotionTargets(source, roles) {
			kind := PromotionKindUpward
			if p.JobFamilyFor(target.Role.Code) != p.JobFamilyFor(source.Role.Code) {
				kind = PromotionKindCrossFamily
			}
			edges = append(edges, PromotionPathEdge{
				OrgUnit: source.Unit, Kind: kind,
				SourceJobCode: source.Role.Code, SourceGrade: source.Role.Grade,
				TargetJobCode: target.Role.Code, TargetGrade: target.Role.Grade, TargetTitle: target.Role.Title,
				MinimumBaseIncrease: demoMinimumBaseIncrease, MaximumBaseIncrease: demoMaximumBaseIncrease,
			})
		}
	}
	// Stable, so the per-source preference order built above survives: the
	// nearest step a role can take is the first target it publishes.
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].OrgUnit != edges[j].OrgUnit {
			return edges[i].OrgUnit < edges[j].OrgUnit
		}
		return edges[i].SourceJobCode < edges[j].SourceJobCode
	})
	return edges
}

// ladderRoles flattens the staffing catalog into the one role list both ends
// of every edge are chosen from, sorted by job code so the result does not
// depend on the order a unit happens to be declared in.
//
// A role whose grade this package does not rank, or whose published base pay
// is not exact cents, is left out rather than compared against a guess; the
// package test proves the real catalog leaves nothing out.
func ladderRoles() []ladderRole { return HarborCarePack.ladderRoles() }

func (p *Pack) ladderRoles() []ladderRole {
	seats := p.allRoles()
	roles := make([]ladderRole, 0, len(seats))
	division := p.divisions()
	for _, seat := range seats {
		rank, ranked := p.gradeRank[seat.Role.Grade]
		cents, exact := p.annualCents(seat.Role)
		family := p.JobFamilyFor(seat.Role.Code)
		if !ranked || !exact || family == "" {
			continue
		}
		roles = append(roles, ladderRole{
			Unit: seat.Unit, Division: division[seat.Unit], Family: family,
			Role: seat.Role, Rank: rank, Cents: cents,
		})
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Role.Code < roles[j].Role.Code })
	return roles
}

// promotionTargets picks the roles source publishes an edge to, best first.
//
// A candidate clears four separate bars. It outranks the source's grade and
// pays strictly more, so the target band's minimum is above the source
// band's. It stays inside the reachable pay window above, so a base the
// ladder admits also sits inside the target's own band. And it is a move
// somebody could actually make ([publishableTarget]) -- the bar this catalog
// was missing, which let a senior product designer be offered a clinical
// directorship because the salary happened to fit.
//
// Preference is [targetTier]: the source's own team and craft first, then
// their craft elsewhere, then their team's other craft, then a neighbouring
// discipline, then a leadership seat; within a tier, the smallest grade step,
// then the smallest pay gap, then job code. That puts the next step first and
// the longer leap behind it.
func (p *Pack) promotionTargets(source ladderRole, roles []ladderRole) []ladderRole {
	type candidate struct {
		role       ladderRole
		tier       int
		gradeSteps int
		payGap     int64
	}
	candidates := make([]candidate, 0, len(roles))
	for _, role := range roles {
		if role.Role.Code == source.Role.Code || role.Rank <= source.Rank || role.Cents <= source.Cents {
			continue
		}
		if demoTargetPayDenominator*role.Cents > demoTargetPayNumerator*source.Cents {
			continue
		}
		if !p.publishableTarget(source, role) {
			continue
		}
		candidates = append(candidates, candidate{
			role: role, tier: p.targetTier(source, role),
			gradeSteps: role.Rank - source.Rank, payGap: role.Cents - source.Cents,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if a.gradeSteps != b.gradeSteps {
			return a.gradeSteps < b.gradeSteps
		}
		if a.payGap != b.payGap {
			return a.payGap < b.payGap
		}
		return a.role.Role.Code < b.role.Role.Code
	})
	if len(candidates) > MaxPromotionTargets {
		candidates = candidates[:MaxPromotionTargets]
	}
	targets := make([]ladderRole, 0, len(candidates))
	for _, chosen := range candidates {
		targets = append(targets, chosen.role)
	}
	return targets
}

// exactCents reads an authored base pay ("118000.00") as exact cents. It
// refuses anything that is not exactly two fractional digits rather than
// rounding, because a rounded salary would put a worker's recorded pay and
// their band's own midpoint a cent apart.
func exactCents(amount string) (int64, bool) {
	whole, fraction, found := strings.Cut(strings.TrimSpace(amount), ".")
	if !found || len(fraction) != 2 {
		return 0, false
	}
	dollars, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || dollars < 0 {
		return 0, false
	}
	cents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil || cents < 0 {
		return 0, false
	}
	return dollars*100 + cents, true
}

// PayZones returns the distinct pay zones the demo company's locations use,
// sorted. It is exported so a caller publishing placements for the ladder
// above (which is zone-agnostic) can cross every target job/grade with every
// zone actually in use, without hard-coding the location table a second time.
func PayZones() []string { return HarborCarePack.PayZones() }

// PayZones returns the distinct pay zones this company's locations use.
func (p *Pack) PayZones() []string {
	seen := make(map[string]bool, len(p.locations))
	zones := make([]string, 0, len(p.locations))
	for _, location := range p.locations {
		if !seen[location.Zone] {
			seen[location.Zone] = true
			zones = append(zones, location.Zone)
		}
	}
	sort.Strings(zones)
	return zones
}

// targetTier orders a publishable target by how near the move is.
//
// Craft is ranked before team, which is why the first two tiers are not just
// "same unit": a senior product manager and a principal product designer sit
// in one unit and hold different crafts, and offering the designer's rung
// ahead of the manager's own director would read as a sideways shove rather
// than a promotion.
func (p *Pack) targetTier(source, target ladderRole) int {
	switch {
	case target.Unit == source.Unit && target.Family == source.Family:
		return 0 // the next rung of your own craft, on your own team
	case target.Family == source.Family:
		return 1 // your craft elsewhere in the company
	case target.Unit == source.Unit:
		return 2 // your team's other craft: its management step, usually
	case p.familiesAdjacent(source.Family, target.Family) && target.Division == source.Division:
		return 3 // a neighbouring discipline in your division
	default:
		return 4 // a leadership seat
	}
}

// annualCents is a role's published base pay as annual cents. A salaried
// role's base is already annual; an hourly role's rate is annualized over
// the company's standard hours, so the ladder compares a foreman's rate and
// a superintendent's salary on one scale.
func (p *Pack) annualCents(role Role) (int64, bool) {
	cents, exact := exactCents(role.BasePay)
	if !exact || !p.IsHourlyJob(role.Code) {
		return cents, exact
	}
	return cents * int64(p.StandardHours()), true
}
