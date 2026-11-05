package agentcost

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// Agentcost005ScaleTo500 scales the AGENTCOST-001 seeded week (50 people) to
// a month at 500 people: ten times the people, thirty sevenths the days,
// rounded from 300/7.
func Agentcost005ScaleTo500() int64 { return 43 }

// CostLever is one cost lever costed against the others: the monthly saving
// at 500 people, what it does to quality and latency, what it costs to
// build, and what could go wrong. VendorNeeded marks the hosted decision
// models, which appear only where they beat the in-deployment options;
// Condition names what must hold before they may be adopted.
type CostLever struct {
	ID                  string
	Title               string
	MonthlySavingMicros int64
	SavingBasis         string
	Quality             string
	Latency             string
	Effort              string
	EffortScore         int
	Risk                string
	VendorNeeded        bool
	Condition           string
	SavingPerEffort     float64
}

// Agentcost005Levers costs every lever from the AGENTCOST-001 seeded replay,
// ranked by saving per unit of effort, best first. Each saving is standalone
// (against today's spend); savings overlap where levers remove the same
// calls, and the recommended order accounts for that by sequencing.
func Agentcost005Levers() []CostLever {
	scale := Agentcost005ScaleTo500()
	var screening, decisions, evaluations, answerGen, answerGenInputTokens, decisionCalls int64
	for _, line := range Agentcost001SeededLines() {
		if line.Stage == agentmodel.CallStageScreening {
			screening += line.CostMicros
		}
		if line.Kind == agentmodel.CallKindDecision {
			decisions += line.CostMicros
			decisionCalls++
		}
		if line.Stage == agentmodel.CallStageEvaluation {
			evaluations += line.CostMicros
		}
		if line.Stage == agentmodel.CallStageAnswer && line.Kind == agentmodel.CallKindGeneration {
			answerGen += line.CostMicros
			answerGenInputTokens += line.InputTokens - line.CachedInputTokens
		}
	}
	levers := []CostLever{
		{ID: "in-process-screen", Title: "In-process screen: rules and the in-deployment embedding model before any paid call",
			MonthlySavingMicros: screening * scale * 60 / 100,
			SavingBasis:         "60 percent of screening calls resolved with no model call (assumption, measurable per channel from the ledger), at seeded screening cost",
			Quality:             "unchanged where rules abstain to the model; a missed screen is a missed task, so rules stay conservative",
			Latency:             "faster: microseconds instead of a model round trip",
			Effort:              "small: word lists plus the existing embedding model behind the decision port's screen question", EffortScore: 2,
			Risk: "low: the model remains the fallback, and every rule decision is sealed like a model one"},
		{ID: "in-deployment-classifier", Title: "Small classifier that runs inside the deployment",
			MonthlySavingMicros: screening * scale * 90 / 100,
			SavingBasis:         "standalone: 90 percent of screening cost removed at zero marginal call cost (assumption, gated on the suite); after the in-process screen it takes the remainder",
			Quality:             "gated: must pass the ambient suites before enablement; below threshold it abstains to the model",
			Latency:             "faster: milliseconds in-process",
			Effort:              "medium: train, evaluate and serve a small model per decision point, starting with screens", EffortScore: 3,
			Risk: "medium: a second model to evaluate and monitor, but content never leaves the deployment so every data class applies"},
		{ID: "prompt-caching", Title: "Provider prompt caching for the stable instruction prefix",
			MonthlySavingMicros: answerGenInputTokens * (100000 - 10000) * scale / 3 / 1000000,
			SavingBasis:         "one third of answer input newly served from cache at $0.01 against $0.10 per million, at seeded answer input tokens",
			Quality:             "unchanged: identical answers",
			Latency:             "faster on cache hits",
			Effort:              "tiny: mark the stable prefix cacheable at the gateway", EffortScore: 1,
			Risk: "low: verify the provider bills cache reads at the cached rate before counting it"},
		{ID: "batch-evaluation", Title: "Batch processing for evaluations and other work nobody is waiting on",
			MonthlySavingMicros: evaluations * scale * 50 / 100,
			SavingBasis:         "half price for evaluation calls moved to batch (assumption: provider batch discount)",
			Quality:             "unchanged: same model, later answers",
			Latency:             "slower by design: hours, acceptable for nightly grades",
			Effort:              "small: route evaluation stages through the batch queue", EffortScore: 1,
			Risk: "low: evaluations already tolerate delay; keep an interactive path for release gates"},
		{ID: "difficulty-routing", Title: "Routing by difficulty to a smaller generation model",
			MonthlySavingMicros: answerGen * scale * 40 / 100 * 80 / 100,
			SavingBasis:         "40 percent of answers simple enough for the small profile at one fifth the price (assumption, measurable from the routing suite)",
			Quality:             "gated: the router's mistakes cost answer quality, so route only above its threshold and measure",
			Latency:             "faster for routed answers",
			Effort:              "medium: the model.profile decision point plus a small profile behind the route policy", EffortScore: 3,
			Risk: "medium: a weak answer where a strong one was affordable; the abstain path bounds it"},
		{ID: "context-trimming", Title: "Trimming context: thread parent only, cited sections only",
			MonthlySavingMicros: answerGenInputTokens * 100000 * scale * 40 / 100 / 1000000,
			SavingBasis:         "40 percent fewer answer input tokens (assumption) at the input rate, at seeded answer input tokens",
			Quality:             "watch it: less context can mean thinner answers; the grounding suite must still pass",
			Latency:             "faster: shorter prompts",
			Effort:              "small: retrieval returns parents and cited sections, the executor sends nothing else", EffortScore: 2,
			Risk: "low: deterministic change, measured by the existing suites"},
		{ID: "decision-caching", Title: "Caching identical decisions and answers where the sealed inputs are identical",
			MonthlySavingMicros: decisions * scale * 15 / 100,
			SavingBasis:         "15 percent of decision spend on repeated identical questions (assumption, countable from sealed digests)",
			Quality:             "unchanged: identical inputs, identical answers",
			Latency:             "faster on hits",
			Effort:              "small: a digest-keyed cache beside the decision port with the run record as evidence", EffortScore: 2,
			Risk: "low: cache only where every sealed input matches, including instruction and glossary versions"},
		{ID: "jev-hosted", Title: "Hosted Jev for decision points (AGENTCOST-003)",
			MonthlySavingMicros: decisions*scale - decisionCalls*scale*7,
			SavingBasis:         "seeded decision spend against the vendor-claimed $0.042 per million input tokens at ~150 tokens a call (7 micros); vendor-claimed, blocked on terms",
			Quality:             "unmeasured: needs our suites in German, Arabic and English before any verdict",
			Latency:             "70-500 ms vendor-claimed, against seconds for generation-backed decisions",
			Effort:              "medium: a new adapter behind the flag plus procurement of terms", EffortScore: 3,
			Risk:         "high until terms exist: unknown retention, training use, regions and sub-processors",
			VendorNeeded: true,
			Condition:    "adopt only where it beats the in-deployment options after terms cover the classes and measured calibration passes; today that is nowhere"},
		{ID: "decisions-api", Title: "OpenAI Decisions API for decision points (AGENTCOST-004)",
			MonthlySavingMicros: 0,
			SavingBasis:         "unknown: no published price; carried at zero until the provider prices it",
			Quality:             "unmeasured: model-reported confidence with third-party evidence of overconfidence",
			Latency:             "about 150 ms reported",
			Effort:              "medium: a new adapter behind the flag", EffortScore: 3,
			Risk:         "high: preview contract not final, terms unconfirmed, US-only residency reported",
			VendorNeeded: true,
			Condition:    "revisit after general availability with price, per-option probabilities or a threshold story, and matching data terms"},
	}
	for i := range levers {
		levers[i].SavingPerEffort = float64(levers[i].MonthlySavingMicros) / float64(levers[i].EffortScore)
	}
	sort.Slice(levers, func(i, j int) bool {
		if levers[i].SavingPerEffort != levers[j].SavingPerEffort {
			return levers[i].SavingPerEffort > levers[j].SavingPerEffort
		}
		return levers[i].ID < levers[j].ID
	})
	return levers
}

// Agentcost005RecommendedOrder is the adoption order: the shippable levers
// first, best saving per effort first, then the hosted models with their
// conditions, which beat the in-deployment options nowhere today.
func Agentcost005RecommendedOrder() []string {
	var order []string
	for _, lever := range Agentcost005Levers() {
		if !lever.VendorNeeded {
			order = append(order, lever.ID)
		}
	}
	for _, lever := range Agentcost005Levers() {
		if lever.VendorNeeded {
			order = append(order, lever.ID)
		}
	}
	return order
}

// RenderAgentcost005Record renders the checked-in lever comparison.
func RenderAgentcost005Record() string {
	var out strings.Builder
	out.WriteString("# Decision models against the levers that need no new vendor (AGENTCOST-005)\n\n")
	out.WriteString("Costed from the AGENTCOST-001 seeded replay, scaled to a month at 500 people (x43). The cheapest call is the one not made: each lever below carries its expected monthly saving, its effect on quality and latency, its engineering cost and its risk, ranked by saving per unit of effort. Assumptions are stated per lever and every one is measurable from the ledger once the instrumentation runs in production.\n\n")
	out.WriteString("## Ranked levers\n\n")
	for i, lever := range Agentcost005Levers() {
		fmt.Fprintf(&out, "### %d. %s\n\n- Monthly saving at 500 people: %s (%d micros). Basis: %s.\n- Quality: %s.\n- Latency: %s.\n- Engineering cost (%d of 5): %s.\n- Risk: %s.\n",
			i+1, lever.Title, agentcost001Micros(lever.MonthlySavingMicros), lever.MonthlySavingMicros, lever.SavingBasis, lever.Quality, lever.Latency, lever.EffortScore, lever.Effort, lever.Risk)
		if lever.VendorNeeded {
			fmt.Fprintf(&out, "- Hosted: %s.\n", lever.Condition)
		}
		out.WriteString("\n")
	}
	out.WriteString("## Recommended order\n\n")
	for i, id := range Agentcost005RecommendedOrder() {
		fmt.Fprintf(&out, "%d. %s\n", i+1, id)
	}
	out.WriteString("\nThe hosted decision models appear only where they beat the in-deployment options. Today that is nowhere: Jev is blocked on vendor terms and unmeasured calibration, and the Decisions API has no price and no final contract. Both stay conditional while the seven shippable levers are worked in order; the in-process screen comes before the in-deployment classifier because the classifier then needs only the remainder.\n")
	return out.String()
}
