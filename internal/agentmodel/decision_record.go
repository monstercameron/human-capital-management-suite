package agentmodel

import (
	"fmt"
	"strings"
)

// DecisionPoint is one place in the agent architecture where the product asks
// a bounded question. WrongYes and WrongNo state the cost of a wrong answer
// in each direction; RequiredConfidence is the threshold below which the port
// abstains; Suite is the labelled suite the point must pass before it may be
// enabled; Escalation is where an abstention goes ("model" or "person").
type DecisionPoint struct {
	ID                 string
	Question           DecisionQuestion
	Options            []string
	WrongYes           string
	WrongNo            string
	RequiredConfidence float64
	Suite              string
	Escalation         string
}

// Agentcost002DecisionPoints inventories every decision point in the agent
// architecture (AGENTCOST-002). A point not listed here is not asked through
// the decision port.
func Agentcost002DecisionPoints() []DecisionPoint {
	boolean := []string{"no", "yes"}
	return []DecisionPoint{
		{ID: "screen.message", Question: DecisionBoolean, Options: boolean,
			WrongYes:           "A quiet message is read by a paid call: a fraction of a cent of waste per message, bounded.",
			WrongNo:            "A task or deadline nobody notices: the agent's whole purpose fails silently for that message.",
			RequiredConfidence: 0.80, Suite: "agenteval.TaskCatcherAmbientSuite+ReminderAmbientSuite", Escalation: "model"},
		{ID: "audience.todo_reminder", Question: DecisionChoice, Options: []string{"private", "public"},
			WrongYes:           "A private commitment offered publicly: another person reads what was meant for one. This direction is never taken on a bare majority.",
			WrongNo:            "A group task offered privately: one person sees it instead of the channel, and may still share it.",
			RequiredConfidence: 0.90, Suite: "agenteval.AmbientPersonaSuite(task-catcher,reminder)", Escalation: "model"},
		{ID: "search.route", Question: DecisionChoice, Options: []string{"no_search", "keyword", "meaning", "both"},
			WrongYes:           "A search that was not needed: latency and a few hundred tokens, and possibly a noisier answer.",
			WrongNo:            "An answer from memory where the documents hold the truth: a confident wrong answer with false citations.",
			RequiredConfidence: 0.70, Suite: "agenteval.AgentuxSearchSuite", Escalation: "model"},
		{ID: "reaction.choose", Question: DecisionChoice, Options: []string{"none", "like", "celebrate", "support"},
			WrongYes:           "A reaction the moment did not call for: mild embarrassment on a work message, removable.",
			WrongNo:            "No reaction where one was apt: nothing happens, which is the safe default.",
			RequiredConfidence: 0.85, Suite: "agenteval.AgentuxAmbientReactionSuite", Escalation: "model"},
		{ID: "support.severity", Question: DecisionScore, Options: []string{"0.00-0.25 routine", "0.25-0.50 normal", "0.50-0.75 urgent", "0.75-1.00 critical"},
			WrongYes:           "Routine mail scored urgent: a person is interrupted for what could have waited.",
			WrongNo:            "An urgent request scored calm: a customer waits while the queue looks quiet.",
			RequiredConfidence: 0.75, Suite: "agenteval.SupportTriageSuite", Escalation: "model"},
		{ID: "support.needs_review", Question: DecisionBoolean, Options: boolean,
			WrongYes:           "A draft a person must read that needed nothing: reviewer time, the cheap direction.",
			WrongNo:            "A wrong or unsafe reply sent to a customer without a person seeing it.",
			RequiredConfidence: 0.80, Suite: "agenteval.SupportTriageSuite", Escalation: "person"},
		{ID: "instruction.detect", Question: DecisionBoolean, Options: boolean,
			WrongYes:           "Honest content treated as hostile: the agent refuses help it could have given.",
			WrongNo:            "An instruction in untrusted input obeyed: documents, email, gate answers or transcripts steer the agent.",
			RequiredConfidence: 0.85, Suite: "agenteval.PersonaPeerInjectionSuite", Escalation: "person"},
		{ID: "citation.supported", Question: DecisionBoolean, Options: boolean,
			WrongYes:           "An unsupported sentence delivered as backed by its citations: the seal vouches for what the sources do not say.",
			WrongNo:            "A supported answer held back or reworked: latency and another paid call.",
			RequiredConfidence: 0.85, Suite: "agenteval.PersonaGroundingSuite", Escalation: "person"},
		{ID: "scope.skills", Question: DecisionBoolean, Options: boolean,
			WrongYes:           "A run flagged as out of scope that stayed inside its skills: an investigation that finds nothing.",
			WrongNo:            "A run that left its declared skills unnoticed: the monitor's miss is silent by design, so the threshold stays high.",
			RequiredConfidence: 0.90, Suite: "agenteval.RunScopeSuite", Escalation: "person"},
		{ID: "model.profile", Question: DecisionChoice, Options: []string{"small", "standard", "large"},
			WrongYes:           "Too large a profile for the question: the answer costs several times what it needed to.",
			WrongNo:            "Too small a profile for the question: a weak answer where a strong one was affordable.",
			RequiredConfidence: 0.70, Suite: "agenteval.ProfileRoutingSuite", Escalation: "model"},
	}
}

// Agentcost002ForbiddenSoloActions lists what a decision may never do alone:
// without a person or a governed step after it, these stay refused no matter
// how confident the answer.
func Agentcost002ForbiddenSoloActions() []string {
	return []string{
		"admit a person to anything: membership, access and hiring stay with people and grants",
		"publish anything where another person can read it: an offer is made, never posted, by a decision",
		"widen an audience: private stays private until a person widens it",
		"spend above a limit: budgets are enforced before the call, not decided at it",
		"execute a side effect whose refusal direction is unsafe: where refusing is the dangerous direction, a decision alone never refuses",
	}
}

// Agentcost002RoutePolicyName is the name the decision port carries in the
// model route policy, so routes name decisions exactly like generation.
const Agentcost002RoutePolicyName = "decision"

// RenderAgentcost002Record renders the checked-in decision record: the
// inventory, the port, and how the port is governed exactly like generation.
func RenderAgentcost002Record() string {
	var out strings.Builder
	out.WriteString("# Decision points and the decision port (AGENTCOST-002)\n\n")
	out.WriteString("Whichever decision model is chosen, the product asks typed questions through one governed interface, not a vendor call from ten places. Generation writes text through agentmodel.ModelRequest; decisions answer through agentmodel.DecisionRequest: yes or no, one of N, or a score, with per-option probabilities and a confidence, a threshold per decision point, and an explicit abstain that escalates to the general model or to a person.\n\n")
	out.WriteString("## Inventory\n\n")
	for _, point := range Agentcost002DecisionPoints() {
		fmt.Fprintf(&out, "### %s\n\n- Question: %s; options: %s.\n- If the answer is wrong towards yes: %s\n- If the answer is wrong towards no: %s\n- Required confidence: %.2f; below it the port abstains and escalates to %s.\n- Labelled suite that must pass before this point may be enabled: %s.\n\n",
			point.ID, point.Question, strings.Join(point.Options, ", "), point.WrongYes, point.WrongNo, point.RequiredConfidence, map[string]string{"model": "the general model", "person": "a person"}[point.Escalation], point.Suite)
	}
	out.WriteString("## Governance, exactly like generation\n\n")
	out.WriteString("- Named in the model route policy as \"decision\", beside the generation profiles, so a route names which adapter answers each point.\n")
	out.WriteString("- Covered by the outbound verifier's source classes and data-class ceilings: a decision call carries the same purpose and data classes as a generation call.\n")
	out.WriteString("- Charged to the run budget before it is made: the request's limits reserve against the same ledger.\n")
	out.WriteString("- Sealed in the run record with the question, the options, the probabilities and the threshold (DecisionSeal).\n")
	out.WriteString("- Evaluated by a labelled suite per decision point before it may be enabled; a point without a passing suite answers nothing.\n\n")
	out.WriteString("## What a decision may never do alone\n\n")
	for _, action := range Agentcost002ForbiddenSoloActions() {
		fmt.Fprintf(&out, "- Never %s.\n", action)
	}
	out.WriteString("\n## Thresholds\n\nEach point's threshold is the required confidence in the inventory. Confidence at or above the threshold delivers the choice; below it the port abstains and the question goes to the escalation named for that point, with the probabilities attached so the next step sees how close the call was.\n")
	return out.String()
}
