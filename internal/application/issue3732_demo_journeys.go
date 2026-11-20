package application

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue #3732: a fresh local demo org used to have an empty Journeys list, so
// the chat journey card could not be exercised without proposing a promotion
// by hand. This seeds a handful of promotions through the engine's own governed
// operations (Propose, Execute, Decide) as the demo company's own personas, so
// every row is exactly what a person would have produced and stays visible to
// whoever the engine's own disclosure rules let see it.
//
// Nothing here writes a row directly: a stage beyond "proposed" is reached by
// the real approvals, and the seed is gated on the local-development profile,
// the demo workforce and a shipped demo company.

// demoJourneyScenario is one promotion the seed leaves at a given stage.
type demoJourneyScenario struct {
	want    workspace.JourneyStage
	finance *demoJourneyDecision
	manager *demoJourneyDecision
	reason  string
}

type demoJourneyDecision struct {
	approve bool
	reason  string
}

// demoJourneyScenarios are the stages a tester needs to see, most useful first:
// a company with few people in the proposer's reporting line gets the first
// ones. A promotion approved by both people waits for its effective date; it
// only becomes "completed" when that date passes and the payroll and access
// providers answer, which cannot be staged honestly at seed time.
func demoJourneyScenarios() []demoJourneyScenario {
	return []demoJourneyScenario{
		{want: workspace.JourneyStageFinanceApproval, reason: "Taking on the next level of the team's workload"},
		{want: workspace.JourneyStageManagerApproval, reason: "Consistently strong delivery this year",
			finance: &demoJourneyDecision{approve: true, reason: "Within the approved budget"}},
		{want: workspace.JourneyStageRejected, reason: "Early request ahead of the review cycle",
			finance: &demoJourneyDecision{approve: false, reason: "Not within this cycle's budget; revisit at the next review"}},
		{want: workspace.JourneyStageWaitingEffectiveDate, reason: "Leading the new onboarding programme",
			finance: &demoJourneyDecision{approve: true, reason: "Within the approved budget"},
			manager: &demoJourneyDecision{approve: true, reason: "Ready for the larger role"}},
	}
}

// demoJourneySeedResult reports what one seeding pass did, for the log.
type demoJourneySeedResult struct {
	Created int
	Notes   []string
}

type demoJourneyActor struct {
	slot      string
	principal *trust.Principal
}

// seedLocalDevDemoJourneys seeds every served demo company. It never fails the
// start of a development server: a company it cannot seed is reported in
// Notes and left as it was.
func seedLocalDevDemoJourneys(ctx context.Context, engine workspace.JourneyEngine, verifier trust.Verifier, cfg ServeConfig, now func() time.Time) demoJourneySeedResult {
	var result demoJourneySeedResult
	if engine == nil || !cfg.DevDemoJourneys || !cfg.DevBrowserLogin || !cfg.DevWorkforceBootstrap || !cfg.ExecutionAuthority {
		return result
	}
	if now == nil {
		now = time.Now
	}
	personas := composeDevPersonas(verifier, cfg, now)
	for _, pack := range devServedCompanies(cfg) {
		var actors []demoJourneyActor
		for _, persona := range personas {
			if persona.Company != pack.Key {
				continue
			}
			principal, err := verifier.Verify(ctx, trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
			if err != nil {
				result.Notes = append(result.Notes, fmt.Sprintf("%s: persona %s could not be verified: %v", pack.Key, persona.Slot, err))
				continue
			}
			actors = append(actors, demoJourneyActor{slot: persona.Slot, principal: principal})
		}
		created, notes := seedDemoJourneysFor(ctx, engine, actors, now())
		result.Created += created
		for _, note := range notes {
			result.Notes = append(result.Notes, pack.Key+": "+note)
		}
	}
	return result
}

func demoJourneyActorFor(actors []demoJourneyActor, slot string) *trust.Principal {
	for _, actor := range actors {
		if actor.slot == slot {
			return actor.principal
		}
	}
	return nil
}

func demoJourneyActorBySubject(actors []demoJourneyActor, subject string) *trust.Principal {
	for _, actor := range actors {
		if actor.principal.Subject() == subject {
			return actor.principal
		}
	}
	return nil
}

// maxDemoJourneyAttempts bounds how many subjects one scenario may try, so a
// company with few eligible people cannot make start-up loop.
const maxDemoJourneyAttempts = 4

func seedDemoJourneysFor(ctx context.Context, engine workspace.JourneyEngine, actors []demoJourneyActor, now time.Time) (int, []string) {
	proposer := demoJourneyActorFor(actors, "hiring-manager")
	operator := demoJourneyActorFor(actors, "admin")
	if proposer == nil || operator == nil {
		return 0, []string{"the proposer and operator personas are not both available; no demo journeys seeded"}
	}
	proposerCtx := trust.WithPrincipal(ctx, proposer)
	operatorCtx := trust.WithPrincipal(ctx, operator)

	// Replay safe: a tenant that already has any promotion is left alone, so a
	// restart never adds to or contends with what a person has since done.
	existing, err := engine.ListJourneys(operatorCtx)
	if err != nil {
		return 0, []string{"could not read the existing journeys: " + err.Error()}
	}
	if len(existing) > 0 {
		return 0, nil
	}
	workers, options, err := engine.ListWorkers(proposerCtx)
	if err != nil {
		return 0, []string{"could not list the workforce: " + err.Error()}
	}

	candidates := demoJourneyCandidates(workers, actors, proposer)
	effective := now.AddDate(0, 1, 0).Format(time.DateOnly)
	var notes []string
	created := 0
	usedPositions := map[string]bool{}
	for _, scenario := range demoJourneyScenarios() {
		placed := false
		// A scenario that ends before the manager's decision does not need a
		// persona to be that person's manager, so it takes the people who do
		// not have one and leaves those for the scenario that does.
		ordered := demoJourneyOrderFor(candidates, scenario)
		attempts := 0
		for _, subject := range ordered {
			if attempts >= maxDemoJourneyAttempts {
				break
			}
			input, ok := demoJourneyProposal(subject.worker, options, effective, scenario.reason, usedPositions)
			if !ok {
				continue
			}
			attempts++
			stage, note := runDemoJourney(ctx, engine, actors, proposer, operator, input, scenario)
			candidates = demoJourneyWithout(candidates, subject.worker.SubjectID)
			if note != "" {
				notes = append(notes, fmt.Sprintf("%s for %s: %s", scenario.want, subject.worker.WorkerRef, note))
			}
			if stage == scenario.want {
				created++
				placed = true
				break
			}
		}
		if !placed {
			notes = append(notes, fmt.Sprintf("%s: no eligible person with an open position was left to seed it", scenario.want))
		}
	}
	return created, notes
}

// demoJourneyCandidate is a person a promotion can be seeded for.
type demoJourneyCandidate struct {
	worker workspace.WorkerSummary
	// managerPersona is set when the person's current manager is a demo
	// persona other than the proposer, so a manager approval can be decided.
	managerPersona bool
	// persona is set when the person is themselves a quick-pick persona; those
	// are used last so a persona's own promotion tends to stay free to propose
	// by hand.
	persona bool
}

// demoJourneyCandidates lists the people a promotion can be seeded for: active
// people the proposer can read pay for and who sit below the proposer in the
// reporting line (the engine refuses anyone else at the proposal), excluding
// the proposer.
func demoJourneyCandidates(workers []workspace.WorkerSummary, actors []demoJourneyActor, proposer *trust.Principal) []demoJourneyCandidate {
	managerOf := make(map[string]string, len(workers))
	for _, worker := range workers {
		managerOf[worker.SubjectID] = strings.TrimSpace(worker.ManagerRef)
	}
	var picked []demoJourneyCandidate
	for _, worker := range workers {
		if strings.TrimSpace(worker.BasePay) == "" || worker.LifecycleStatus != "" && !strings.EqualFold(worker.LifecycleStatus, "ACTIVE") {
			continue
		}
		// The proposer's own direct reports are senior people whose proposals
		// the pay bands do not cover, and who would be proposed and approved by
		// the same person.
		if worker.SubjectID == proposer.Subject() || strings.TrimSpace(worker.ManagerRef) == proposer.Subject() {
			continue
		}
		if !demoJourneyInChain(worker.SubjectID, proposer.Subject(), managerOf) {
			continue
		}
		manager := managerOf[worker.SubjectID]
		picked = append(picked, demoJourneyCandidate{
			worker:         worker,
			managerPersona: manager != proposer.Subject() && demoJourneyActorBySubject(actors, manager) != nil,
			persona:        demoJourneyActorBySubject(actors, worker.SubjectID) != nil,
		})
	}
	return picked
}

// demoJourneyOrderFor returns the candidates a scenario may use, best first.
func demoJourneyOrderFor(candidates []demoJourneyCandidate, scenario demoJourneyScenario) []demoJourneyCandidate {
	var out []demoJourneyCandidate
	for _, candidate := range candidates {
		if scenario.manager != nil && !candidate.managerPersona {
			continue
		}
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].persona != out[j].persona {
			return !out[i].persona
		}
		return scenario.manager == nil && !out[i].managerPersona && out[j].managerPersona
	})
	return out
}

func demoJourneyWithout(candidates []demoJourneyCandidate, subject string) []demoJourneyCandidate {
	out := candidates[:0:0]
	for _, candidate := range candidates {
		if candidate.worker.SubjectID != subject {
			out = append(out, candidate)
		}
	}
	return out
}

// demoJourneyInChain reports whether the proposer sits above subject in the
// reporting line. The walk is bounded so a cyclic record cannot loop.
func demoJourneyInChain(subject, proposer string, managerOf map[string]string) bool {
	for hops, current := 0, managerOf[subject]; current != "" && hops < 32; hops, current = hops+1, managerOf[current] {
		if current == proposer {
			return true
		}
	}
	return false
}

// demoJourneyProposal builds the proposal for one person from the first
// published promotion path that starts at their job and grade.
//
// Each target position can be held by one proposal at a time, so positions in
// used are never offered again and the chosen one is added to it.
func demoJourneyProposal(worker workspace.WorkerSummary, options workspace.WorkforceOptions, effective, reason string, used map[string]bool) (workspace.ProposalInput, bool) {
	for _, path := range options.PromotionPaths {
		if path.SourceJobCode != worker.JobCode || path.SourceGrade != worker.Grade {
			continue
		}
		base, ok := demoJourneyProposedBase(worker.BasePay, path.MinimumBaseIncrease)
		if !ok {
			continue
		}
		position := ""
		for _, vacancy := range options.PositionVacancies {
			if vacancy.JobCode != path.TargetJobCode || used[vacancy.Reference] {
				continue
			}
			if position == "" || vacancy.OrgUnit == worker.OrgUnit {
				position = vacancy.Reference
			}
			if vacancy.OrgUnit == worker.OrgUnit {
				break
			}
		}
		if position == "" {
			continue
		}
		used[position] = true
		return workspace.ProposalInput{
			WorkerRef: worker.WorkerRef, TargetJobCode: path.TargetJobCode, TargetGrade: path.TargetGrade,
			TargetPositionID: position, ProposedBase: base, EffectiveDate: effective, BusinessReason: reason,
		}, true
	}
	return workspace.ProposalInput{}, false
}

// demoJourneyProposedBase is current*(1+increase) rounded up to the cent, or
// one cent over current when the path publishes no minimum.
func demoJourneyProposedBase(current, increase string) (string, bool) {
	cur, ok := new(big.Rat).SetString(strings.TrimSpace(current))
	if !ok {
		return "", false
	}
	next := new(big.Rat).Add(cur, big.NewRat(1, 100))
	if strings.TrimSpace(increase) != "" {
		inc, ok := new(big.Rat).SetString(strings.TrimSpace(increase))
		if !ok {
			return "", false
		}
		scaled := new(big.Rat).Mul(cur, new(big.Rat).Add(big.NewRat(1, 1), inc))
		if scaled.Cmp(next) > 0 {
			next = scaled
		}
	}
	cents := new(big.Rat).Mul(next, big.NewRat(100, 1))
	whole := new(big.Int).Div(cents.Num(), cents.Denom())
	if new(big.Rat).SetInt(whole).Cmp(cents) < 0 {
		whole.Add(whole, big.NewInt(1))
	}
	return new(big.Rat).SetFrac(whole, big.NewInt(100)).FloatString(2), true
}

// runDemoJourney proposes, executes and decides one promotion as the people
// the engine routes each step to, stopping where the scenario says. It returns
// the stage reached and a note when the run stopped short.
func runDemoJourney(
	ctx context.Context, engine workspace.JourneyEngine, actors []demoJourneyActor,
	proposer, operator *trust.Principal, input workspace.ProposalInput, scenario demoJourneyScenario,
) (workspace.JourneyStage, string) {
	proposed, err := engine.Propose(trust.WithPrincipal(ctx, proposer), input)
	if err != nil {
		return "", "propose: " + err.Error()
	}
	if proposed.Stage == workspace.JourneyStageBlocked {
		return proposed.Stage, "the proposal was blocked"
	}
	detail, err := engine.Execute(trust.WithPrincipal(ctx, operator), proposed.IntentID)
	if err != nil {
		return proposed.Stage, "execute: " + err.Error()
	}
	stage := detail.Summary.Stage
	for _, decision := range []*demoJourneyDecision{scenario.finance, scenario.manager} {
		if decision == nil {
			break
		}
		owner := demoJourneyRoutedApprover(detail)
		approver := demoJourneyActorBySubject(actors, owner)
		if approver == nil {
			return stage, "the approval is routed to " + owner + ", who is not a demo persona"
		}
		detail, err = engine.Decide(trust.WithPrincipal(ctx, approver), proposed.IntentID, workspace.Decision{Approve: decision.approve, Reason: decision.reason})
		if err != nil {
			return stage, "decide: " + err.Error()
		}
		stage = detail.Summary.Stage
	}
	if stage != scenario.want {
		return stage, "ended at " + string(stage)
	}
	return stage, ""
}

// demoJourneyRoutedApprover names who the journey's open approval is routed
// to, read from the routed WorkItem itself rather than assumed.
func demoJourneyRoutedApprover(detail workspace.JourneyDetail) string {
	for _, item := range detail.WorkItems {
		if item.Kind != workitem.KindApproval {
			continue
		}
		switch item.Status {
		case workitem.StatusRouted, workitem.StatusAssigned, workitem.StatusAvailable, workitem.StatusClaimed, workitem.StatusInProgress:
			if owner := item.Assignment.ChosenOwner; owner != "" {
				return owner
			}
		}
	}
	return detail.Summary.Approver
}
