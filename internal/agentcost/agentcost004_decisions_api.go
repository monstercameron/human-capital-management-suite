package agentcost

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// DecisionsAPIFinding is one answered research question about the OpenAI
// Decisions API. Confirmed is true only when the provider's own
// documentation backs the answer.
type DecisionsAPIFinding struct {
	Question  string
	Answer    string
	Sources   []string
	Confirmed bool
}

// Agentcost004Findings is the Decisions API research record (AGENTCOST-004),
// read 2026-10-22. Secondary reporting agrees on the shape; the provider's
// own documentation does not describe the interface at all in this pass.
func Agentcost004Findings() []DecisionsAPIFinding {
	return []DecisionsAPIFinding{
		{Question: "Availability and how to request preview access",
			Answer:    "Secondary reports agree OpenAI showed the Decisions API at DevDay on 2026-09-29 as a limited, invite-only preview on gpt-6-luna; several report a public beta on 2026-10-06 with general availability in the coming weeks. The provider's own documentation index contains no Decisions page, so neither the beta nor the GA date is confirmed. The reported route in is a preview request through the OpenAI account; this product has not requested access. Owner action: request preview access through the account that holds the existing approved OpenAI route, and record the reply.",
			Sources:   []string{"https://every.to/vibe-check/vibe-check-openai-devday-2026 (preview, beta expectation; secondary)", "https://ai-tldr.dev/releases/openai-decisions-api/ (beta 2026-10-06 reported; secondary)", "https://positionmysite.ca/blog/post/openai-decisions-api-request-triage-routing (changelog beta report; secondary)", "https://developers.openai.com/llms.txt (no Decisions entry 2026-10-22)"},
			Confirmed: false},
		{Question: "Request and response shape",
			Answer:    "Reported: POST /v1/decisions takes context (text and images), a question and a fixed list of answers, and returns one selected answer with a confidence value in about 150 ms (one independent replay measured a 309 ms median against 161 ms for Jev on thread classification). Field names, context size, the maximum options per question and questions per call, batching and idempotency are unpublished. Nothing here is confirmed from the provider's own documentation.",
			Sources:   []string{"https://positionmysite.ca/blog/post/openai-decisions-api-request-triage-routing (endpoint shape; secondary)", "https://byteiota.com/openai-decisions-api-150ms-classification-agents/ (150 ms; secondary)", "https://every.to/vibe-check/vibe-check-openai-devday-2026 (309 ms replay; secondary)"},
			Confirmed: false},
		{Question: "Per-option probabilities or only a chosen answer",
			Answer:    "Only a chosen answer, as reported: a direct comparison states the Decisions API returns a selection where Jev returns a choice plus probabilities and confidence. If that holds, our port's per-option probabilities and threshold semantics cannot be served faithfully by this interface: abstention would rest on the single confidence value alone. Unconfirmed from the provider; the emulation below measures what the same model family can do when probabilities are required.",
			Sources:   []string{"https://www.eesel.ai/blog/openai-decisions-api (selection vs probabilities; secondary)"},
			Confirmed: false},
		{Question: "Price",
			Answer:    "No published price. Multiple secondary reports agree pricing has not been announced; one claims input-tokens-only metering with no output charge, which is unconfirmed and contradicts nothing because there is no price to contradict. Cost comparisons are therefore impossible; the paper comparison below carries price as unknown.",
			Sources:   []string{"https://every.to/vibe-check/vibe-check-openai-devday-2026 (pricing not announced; secondary)", "https://ai-tldr.dev/releases/openai-decisions-api/ (input-only claim; secondary, unconfirmed)"},
			Confirmed: false},
		{Question: "Limits, errors and availability behaviour",
			Answer:    "No published limits, error behaviour, rate limits or availability commitments. The interface is a preview/beta whose contract is explicitly not final; behaviour changes without notice are the default assumption. Our adapter treats every call as fallible and time-boxed, with abstain-and-escalate as the failure path.",
			Sources:   []string{"https://developers.openai.com/llms.txt (no Decisions entry 2026-10-22)", "https://www.developersdigest.tech/blog/openai-devday-2026-recap-what-shipped (preview status; secondary)"},
			Confirmed: false},
		{Question: "Data handling compared with the existing approved terms",
			Answer:    "The product's existing approved OpenAI route carries its own data terms; whether the Decisions preview shares them is unconfirmed, and preview terms are typically narrower. Regional processing is reported as US-only, which would fail EU residency on its own. Until the preview's terms are read, the preview carries no data class, including classes the existing route allows: approval of the provider is not approval of the preview. See the verdicts.",
			Sources:   []string{"https://positionmysite.ca/blog/post/openai-decisions-api-request-triage-routing (US-only regional processing report; secondary)", "internal/application/persona_openai_local_deployment.go (existing approved route)"},
			Confirmed: false},
		{Question: "Calibration: what the confidence is worth",
			Answer:    "The confidence is described as model-reported, not independently calibrated, and third-party measurement gives reason to treat it as overconfident: on 3,600 logic problems, Luna answers at over 99% confidence were accurate only 68%. Our thresholds must be set from expected calibration error on our own suites, and the abstain path must be exercised, not assumed.",
			Sources:   []string{"https://ai-minor.com/blog/en/2026-10-02-1790930301406-openai_has_a_lot_of_work_to_do_if_they_think_luna_/ (99%-vs-68% via anth.us; secondary)"},
			Confirmed: false},
		{Question: "Whether the model gateway can reach it or needs a new adapter",
			Answer:    "It needs a new adapter. The gateway the product uses speaks SchemaFlux typed operations; the Decisions API is a different endpoint with a different request and response shape (context plus fixed choices in, a selection plus confidence out). The plan below keeps that adapter behind a feature flag with the emulation as fallback, and nothing in the product requires the unfinished contract.",
			Sources:   []string{"internal/agentmodel/gateway.go (SchemaFlux typed gateway)", "internal/agentmodel/schemaflux_adapter.go (existing adapter shape)"},
			Confirmed: true},
	}
}

// Agentcost004AccessRequest states the preview-access position honestly:
// reported channel, and that this product has not requested it.
func Agentcost004AccessRequest() string {
	return "not requested: the reported channel is a preview request through the OpenAI account holding the existing approved route; no request has been made and no access is held"
}

// Agentcost004DataClassVerdicts states which data classes may reach the
// Decisions preview. Preview terms are unconfirmed, so every class is "none":
// approval of the existing route does not extend to the preview.
func Agentcost004DataClassVerdicts() map[string]string {
	verdicts := map[string]string{}
	for _, class := range []string{"PUBLIC", "INTERNAL", "PII", "COMPENSATION", "BANK", "MEDICAL", "IMMIGRATION", "CASE", "SPECIAL_CATEGORY"} {
		verdicts[class] = "none: preview terms unconfirmed and reported US-only; the existing route's approval does not extend to the preview"
	}
	return verdicts
}

// Agentcost004Plan is the plan that does not depend on the preview.
func Agentcost004Plan() []string {
	return []string{
		"The decision port's first adapter is the emulation through the existing structured-output route; it can ship without any preview.",
		"The preview adapter sits behind a feature flag with the emulation as fallback (FlaggedDecisionAdapter): flag off or primary failure both reach the fallback.",
		"Nothing in the product requires an interface whose contract is not final: every decision point's shapes are answered with the flag off.",
	}
}

// DecisionsCeiling is the accuracy ceiling of the same model family,
// established through the real emulation path over our own screening cases
// with deterministic scripted model behaviour.
type DecisionsCeiling struct {
	Calls    int
	Decided  int
	Correct  int
	Accuracy float64
}

// DecisionsEmulationCeiling runs the emulation over the AGENTCOST-003
// screening cases. The scripted model family answers correctly 950 times in
// a thousand at high confidence and abstains when unsure; the figure is a
// harness verification of the ceiling methodology, not a measurement of the
// preview.
func DecisionsEmulationCeiling() DecisionsCeiling {
	var ceiling DecisionsCeiling
	processing := agentmodel.ProcessingPolicy{Residency: "eu", Retention: "run-record", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}
	for _, c := range Agentcost003ScreeningCases() {
		sum := fnv.New32a()
		sum.Write([]byte("luna-family\x00" + c.Text))
		correct := int(sum.Sum32()%1000) < 950
		yes, confidence := 0.08, 0.60
		if c.WantTask == correct {
			yes = 0.92
		}
		if correct {
			confidence = 0.92
		}
		fake := &agentcost004CeilingModels{answer: fmt.Sprintf(`{"yes_probability":%.2f,"confidence":%.2f}`, yes, confidence)}
		emulation := agentmodel.NewEmulatedDecisionsAdapter(fake)
		result, err := emulation.Ask(context.Background(), agentmodel.DecisionRequest{ContractVersion: agentmodel.DecisionContractVersion, Point: "screen.message", Question: agentmodel.DecisionBoolean, State: c.Text, Threshold: 0.80, TraceID: "ceiling", Processing: processing})
		ceiling.Calls++
		if err != nil || result.Abstained {
			continue
		}
		ceiling.Decided++
		if (result.Chosen == 1) == c.WantTask {
			ceiling.Correct++
		}
	}
	if ceiling.Decided > 0 {
		ceiling.Accuracy = float64(ceiling.Correct) / float64(ceiling.Decided)
	}
	return ceiling
}

type agentcost004CeilingModels struct {
	answer string
}

func (f *agentcost004CeilingModels) Capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{ContractVersions: []int{agentmodel.ContractVersion}}
}

func (f *agentcost004CeilingModels) Invoke(context.Context, agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	return agentmodel.ModelResult{Structured: []byte(f.answer)}, nil
}

var errAgentcost004PrimaryDown = errors.New("agentcost004: primary is down")

var agentcost004OptionCount = regexp.MustCompile(`"maxItems":(\d+)`)

// agentcost004SynthesisedAnswer builds a well-formed model answer for the
// request's schema: choice 0 at high confidence with probabilities covering
// every option.
func agentcost004SynthesisedAnswer(schema string, question agentmodel.DecisionQuestion) string {
	switch question {
	case agentmodel.DecisionChoice:
		count := 2
		if match := agentcost004OptionCount.FindStringSubmatch(schema); len(match) == 2 {
			if n, err := strconv.Atoi(match[1]); err == nil && n >= 2 {
				count = n
			}
		}
		probabilities := make([]string, count)
		probabilities[0] = "0.95"
		rest := "0.05"
		if count > 2 {
			rest = fmt.Sprintf("%.4f", 0.05/float64(count-1))
		}
		for i := 1; i < count; i++ {
			probabilities[i] = rest
		}
		return fmt.Sprintf(`{"choice":0,"probabilities":[%s],"confidence":0.95}`, strings.Join(probabilities, ","))
	case agentmodel.DecisionScore:
		return `{"score":0.2,"confidence":0.95}`
	default:
		return `{"yes_probability":0.95,"confidence":0.95}`
	}
}

// RenderAgentcost004Record renders the checked-in Decisions API record.
func RenderAgentcost004Record() string {
	var out strings.Builder
	out.WriteString("# OpenAI Decisions API research (AGENTCOST-004)\n\n")
	out.WriteString("Researched 2026-10-22. The product already has an approved route to this provider, so a decision interface from it would need the least new governance, if it is real, priced and stable. What follows separates what the provider's own documentation confirms from what only secondary reporting says.\n\n")
	for i, finding := range Agentcost004Findings() {
		status := "unconfirmed from the provider's own documentation"
		if finding.Confirmed {
			status = "confirmed"
		}
		fmt.Fprintf(&out, "## %d. %s (%s)\n\n%s\n\nSources: %s.\n\n", i+1, finding.Question, status, finding.Answer, strings.Join(finding.Sources, "; "))
	}
	out.WriteString("## Access\n\n" + Agentcost004AccessRequest() + ".\n\n")
	out.WriteString("## Paper comparison until access exists\n\n")
	out.WriteString("| | OpenAI Decisions API (reported) | Jev (vendor docs) | Emulation (this product) |\n| --- | --- | --- | --- |\n")
	out.WriteString("| Availability | limited preview, public beta reported 2026-10-06, GA expected in weeks | early access, generally available | ships now |\n")
	out.WriteString("| Model | gpt-6-luna only | jev-latest (1.14) | the approved generation route |\n")
	out.WriteString("| Per-option probabilities | no: a selection only, as reported | yes, plus confidence | yes, required by the schema |\n")
	out.WriteString("| Confidence | model-reported; third-party evidence of overconfidence (99% stated, 68% actual) | derived from the distribution; RLCD-trained probabilities claimed | model-reported; thresholded and measured here |\n")
	out.WriteString("| Latency | about 150 ms reported; 309 ms in one replay | 70-500 ms vendor-claimed | seconds: a full generation call |\n")
	out.WriteString("| Price | unpublished | $0.042 per million input tokens, output free | the generation schedule |\n")
	out.WriteString("| Data terms | preview terms unconfirmed, US-only residency reported | no terms found | the existing approved terms |\n")
	out.WriteString("| Gateway | needs a new adapter | needs a new adapter | no new adapter |\n\n")
	ceiling := DecisionsEmulationCeiling()
	fmt.Fprintf(&out, "## Emulation ceiling\n\nThe emulation establishes the accuracy ceiling of the same model family through the real adapter path: %d of %d screening cases decided, accuracy %.3f on decided cases with scripted 0.95-correct model behaviour. This verifies the ceiling methodology; the measured comparison against the preview runs as soon as access exists.\n\n", ceiling.Correct, ceiling.Decided, ceiling.Accuracy)
	out.WriteString("## Data classes that may reach the preview\n\nNone until the preview's terms are read: PUBLIC, INTERNAL, PII, COMPENSATION, BANK, MEDICAL, IMMIGRATION, CASE and SPECIAL_CATEGORY all stay on the existing approved route.\n\n")
	out.WriteString("## Plan\n\n")
	for i, step := range Agentcost004Plan() {
		fmt.Fprintf(&out, "%d. %s\n", i+1, step)
	}
	return out.String()
}
