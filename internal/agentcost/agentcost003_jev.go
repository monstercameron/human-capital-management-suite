package agentcost

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// JevFinding is one answered research question. Confirmed is true only when
// the answer comes from the vendor's own properties; vendor-claimed figures
// that no primary source backs are marked as such in the answer.
type JevFinding struct {
	Question  string
	Answer    string
	Sources   []string
	Confirmed bool
}

// Agentcost003Findings is the Jev research record (AGENTCOST-003), read
// 2026-10-22. The vendor's documentation, homepage and error reference are
// primary; pricing and availability notes that appear only on the homepage,
// in secondary coverage or on unaffiliated wrapper sites are labelled.
func Agentcost003Findings() []JevFinding {
	return []JevFinding{
		{Question: "Interface and limits",
			Answer:    "Jev is a hosted decision model from TypeSafe AI, released 2026-09-15 and in early access (jev-latest; Jev 1.14 of 2026-10-14 current, 1.13 still served). One HTTPS call carries a state (text or JSON) and several typed questions, evaluated in parallel: Noul (yes-or-no probability 0-1), Choice (one of a fixed set with per-option probabilities and confidence), Score (ordered rubric levels with per-level distribution, confidence and legend), and Box (an image position as box edges). Images may appear in the state, in instructions and in criteria. Every request is at most 32 MiB of UTF-8 JSON, 64 nesting levels, gzip or zstd; the context length is stated per model and secondary coverage reports 64k tokens of state plus questions. The maximum options per question and the maximum questions per call are not published. Reached through api.typesafe.ai, the vendor console, or the OpenRouter, Vercel, Cloudflare and DigitalOcean gateways; official SDKs are Python and JavaScript.",
			Sources:   []string{"https://docs.typesafe.ai/llms.txt", "https://docs.typesafe.ai/introduction.md", "https://docs.typesafe.ai/api/errors.md", "https://docs.typesafe.ai/primitives/choice.md", "https://docs.typesafe.ai/primitives/score.md", "https://docs.typesafe.ai/primitives/noul.md", "https://docs.typesafe.ai/primitives/box.md", "https://docs.typesafe.ai/images.md", "https://typesafe.ai/ (early access, gateways)", "https://openrouter.ai/blog/insights/what-is-jev/ (versions 1.13/1.14, secondary)"},
			Confirmed: true},
		{Question: "What calibrated is claimed to mean, and the evidence",
			Answer:    "Two different things share the word. The probabilities are claimed calibrated because Jev is trained with Reinforcement Learning for Calibrated Decisions (RLCD), which optimises for honest probabilities rather than human-preferred prose; the evidence offered is the vendor's own evals page and launch benchmarks, not an independent study. The confidence value is something weaker and exactly specified: a statistic derived from the answer's own probability distribution (for a Choice of N options, (N x largest probability - 1) / (N - 1)), 1 when all probability sits on one outcome, 0 on an even split; Noul answers carry no confidence. Our thresholds must therefore be validated against expected calibration error on our own suites, not taken from the vendor's word: a peaked distribution is not proof of a right answer.",
			Sources:   []string{"https://docs.typesafe.ai/confidence.md", "https://docs.typesafe.ai/introduction/machine-learning-primer.md", "https://typesafe.ai/ (RLCD, 193.6x/444.6x vendor figures)", "evals.typesafe.ai via vendor charts (vendor's own evals)"},
			Confirmed: true},
		{Question: "Price and how it is metered",
			Answer:    "The vendor homepage lists $42 per billion input tokens ($0.042 per million), output free because Jev returns no conventional text; the same list price is reported on OpenRouter and the Vercel AI Gateway. Metering is input tokens of state plus questions; per-call minimums, overage behaviour and whether gateway calls carry platform fees are unpublished. The vendor's headline figures (193.6x faster, 444.6x cheaper; a demo of $0.000081 in 0.114s against $0.013880 in 8.566s for a frontier LLM) come from its own workflows and are vendor-claimed, not measured here.",
			Sources:   []string{"https://typesafe.ai/ ($42 per billion, vendor-claimed ratios)", "https://journal.qualiteg.com/jev-typesafe-ai-pricing-python-hands-on/ (gateway price parity, secondary)"},
			Confirmed: false},
		{Question: "Data handling against our outbound terms",
			Answer:    "No data processing addendum, retention statement, training-use statement, region list, deletion procedure, sub-processor list or security whitepaper was found on the vendor's properties in this pass. The console and API are US-operated by a San Francisco company; data residency options, if any, are unpublished. Against our outbound terms this means no data class is covered: anything sent would leave the deployment to a processor with unknown retention, unknown training use and unknown regions. A clone-site warning (dozens of lookalike domains) further requires procurement to buy only from typesafe.ai or the named gateways. Verdict per class: none may be sent until terms exist.",
			Sources:   []string{"https://typesafe.ai/ (no terms/DPA found 2026-10-22)", "https://docs.typesafe.ai/llms.txt (no privacy/security pages in index)", "https://labs.eye.security/rise-of-the-jev-clones/ (clone-site warning, secondary)"},
			Confirmed: false},
		{Question: "Failure behaviour, rate limits and availability",
			Answer:    "Documented failures are HTTP statuses: 400 (request rules incl. max_tokens_exceeded), 401, 413 (over 32 MiB), 415 (compression), 422 (validation), 429 (rate limit, numeric limits unpublished) and 529 (vendor overloaded); 429/529 call for exponential backoff, handled by the vendor SDKs. No status history page, no availability commitment and no latency commitment were found; the 70-500ms typical latency is vendor-claimed. Our adapter must therefore treat every Jev call as fallible and time-boxed, with the abstain-and-escalate path as the failure path.",
			Sources:   []string{"https://docs.typesafe.ai/api/errors.md", "https://jevapi.dev/ (70-500ms, unaffiliated, vendor-claimed)"},
			Confirmed: true},
		{Question: "Self-hosting or a private deployment",
			Answer:    "None exists. Jev is served as a hosted API in early access behind the vendor console; weights are unpublished and no private deployment, VPC offering or on-premises option is documented. The only in-deployment answers to the same questions are our own small classifier and the existing structured-output route, compared below.",
			Sources:   []string{"https://docs.typesafe.ai/introduction.md (hosted API)", "https://www.n884.com/html/models/85.html (weights unpublished, waitlist; secondary)"},
			Confirmed: false},
		{Question: "Languages including German and Arabic, and images",
			Answer:    "The vendor publishes no language list and no per-language quality figures. German and Arabic coverage is therefore unconfirmed and must be measured on our own suites, which carry German and Arabic labels for both Task Catcher and Reminder. Images are confirmed as inputs (state, instructions, criteria) with a dedicated Box question type for positions; image pricing and size limits are unpublished.",
			Sources:   []string{"https://docs.typesafe.ai/images.md", "https://docs.typesafe.ai/primitives/box.md", "agenteval.TaskCatcherAmbientSuite/ReminderAmbientSuite (German and Arabic labels in-repo)"},
			Confirmed: false},
		{Question: "Measured comparison on our own data",
			Answer:    "The harness below (ScoreJevStubProvider) runs the Task Catcher and Reminder ambient suites plus their persona-suite shape with synthetic members only, and reports accuracy, the reliability curve with expected calibration error, the abstain rate at the AGENTCOST-002 thresholds, latency and cost per thousand decisions. The three providers run here are deterministic stubs that prove the methodology executes; they are not measurements of Jev, of our general model or of our classifier. Real measurement needs vendor access (early-access console account) and is recorded as not performed. The break-even figures use the vendor-claimed price against the AGENTCOST-001 seeded cost per decision and are labelled as such.",
			Sources:   []string{"internal/agentcost/agentcost003_jev.go (harness)", "internal/agenteval (suites)", "definitions/planning/agentcost001-spend-report.md (current cost per decision)"},
			Confirmed: true},
	}
}

// Agentcost003DataClassVerdicts states which data classes could lawfully be
// sent to Jev under the vendor's terms. The terms were not found, so every
// class is "none" with the reason.
func Agentcost003DataClassVerdicts() map[string]string {
	verdicts := map[string]string{}
	for _, class := range []string{"PUBLIC", "INTERNAL", "PII", "COMPENSATION", "BANK", "MEDICAL", "IMMIGRATION", "CASE", "SPECIAL_CATEGORY"} {
		verdicts[class] = "none: no vendor terms, DPA, retention, training-use or region statement covers this class"
	}
	return verdicts
}

// JevCase is one screening question from our own labelled data: does this
// message merit a paid read?
type JevCase struct {
	Text     string
	WantTask bool
}

// Agentcost003ScreeningCases builds the comparison set from the Task Catcher
// and Reminder ambient suites. Every member is synthetic (author, luis) or
// demo text; no real content enters the comparison.
func Agentcost003ScreeningCases() []JevCase {
	var cases []JevCase
	for _, label := range append(append([]agenteval.AmbientCase{}, agenteval.TaskCatcherAmbientSuite()...), agenteval.ReminderAmbientSuite()...) {
		cases = append(cases, JevCase{Text: label.Text, WantTask: label.Kind != ""})
	}
	return cases
}

// JevStubProvider is a deterministic stand-in with scripted behaviour, used
// to prove the comparison methodology executes. AccuracyMilli is correct
// answers per thousand, HighConfidence the confidence on correct answers,
// LowConfidence on wrong ones.
type JevStubProvider struct {
	Name              string
	AccuracyMilli     int
	HighConfidence    float64
	LowConfidence     float64
	LatencyMillis     int64
	CostMicrosPerCall int64
}

// Agentcost003StubProviders returns the three comparison arms: the current
// general model, the hosted Jev call and an in-deployment small classifier.
// Behaviour is scripted, not measured; the record says so.
func Agentcost003StubProviders() []JevStubProvider {
	return []JevStubProvider{
		{Name: "general-model", AccuracyMilli: 930, HighConfidence: 0.90, LowConfidence: 0.60, LatencyMillis: 1800, CostMicrosPerCall: 900},
		{Name: "jev-hosted", AccuracyMilli: 900, HighConfidence: 0.88, LowConfidence: 0.55, LatencyMillis: 200, CostMicrosPerCall: 7},
		{Name: "in-deployment-classifier", AccuracyMilli: 800, HighConfidence: 0.75, LowConfidence: 0.50, LatencyMillis: 2, CostMicrosPerCall: 0},
	}
}

// JevBin is one reliability-curve bin: the confidence centre, the accuracy
// within the bin and the fraction of answers that fell in it.
type JevBin struct {
	Center   float64
	Accuracy float64
	Fraction float64
}

// JevProviderScore is one provider's measured comparison: accuracy, expected
// calibration error with its curve, abstain rate at the given threshold,
// mean latency and cost per thousand decisions.
type JevProviderScore struct {
	Name                  string
	Calls                 int
	Correct               int
	Accuracy              float64
	ECE                   float64
	AbstainRate           float64
	MeanLatencyMillis     int64
	CostPerThousandMicros int64
	Curve                 [10]JevBin
}

// ScoreJevStubProvider runs the stub over the cases and scores it. The stub
// is deterministic in (provider, text): the same comparison always gives the
// same figures.
func ScoreJevStubProvider(provider JevStubProvider, cases []JevCase, threshold float64) JevProviderScore {
	score := JevProviderScore{Name: provider.Name, MeanLatencyMillis: provider.LatencyMillis, CostPerThousandMicros: provider.CostMicrosPerCall * 1000}
	var bins [10][2]int // correct, total
	for _, c := range cases {
		sum := fnv.New32a()
		sum.Write([]byte(provider.Name + "\x00" + c.Text))
		correct := int(sum.Sum32()%1000) < provider.AccuracyMilli
		confidence := provider.LowConfidence
		if correct {
			confidence = provider.HighConfidence
			score.Correct++
		}
		score.Calls++
		if confidence < threshold {
			score.AbstainRate++
		}
		bin := int(confidence * 10)
		if bin > 9 {
			bin = 9
		}
		if bin < 0 {
			bin = 0
		}
		bins[bin][1]++
		if correct {
			bins[bin][0]++
		}
	}
	if score.Calls == 0 {
		return score
	}
	score.Accuracy = float64(score.Correct) / float64(score.Calls)
	score.AbstainRate /= float64(score.Calls)
	for i := range bins {
		total := bins[i][1]
		center := (float64(i) + 0.5) / 10
		if total == 0 {
			score.Curve[i] = JevBin{Center: center}
			continue
		}
		accuracy := float64(bins[i][0]) / float64(total)
		fraction := float64(total) / float64(score.Calls)
		score.Curve[i] = JevBin{Center: center, Accuracy: accuracy, Fraction: fraction}
		gap := accuracy - center
		if gap < 0 {
			gap = -gap
		}
		score.ECE += gap * fraction
	}
	return score
}

// JevRecommendation is the verdict for one decision point: adopt, adopt
// behind escalation, or do not adopt, with the reason and the break-even.
type JevRecommendation struct {
	Point     string
	Verdict   string
	Reason    string
	BreakEven string
}

// Agentcost003Recommendations recommends per decision point of AGENTCOST-002.
// Terms cover no data class, so nothing is adopted today; each row names what
// would change the verdict and the volume at which the vendor-claimed price
// repays integration.
func Agentcost003Recommendations() []JevRecommendation {
	current := agentcost003CurrentDecisionMicros()
	jev := int64(7) // vendor-claimed: ~150 input tokens at $0.042 per million
	var saving int64
	if current > jev {
		saving = current - jev
	}
	breakEven := "no saving at the vendor-claimed price"
	if saving > 0 {
		// A stated $5,000 integration repays when the per-decision saving
		// covers it; volumes below are in thousands of decisions.
		thousands := (5_000_000_000 + saving*1000 - 1) / (saving * 1000)
		breakEven = fmt.Sprintf("a $5,000 integration repays at %d thousand decisions (vendor-claimed price, current %d micros against %d)", thousands, current, jev)
	}
	verdict := func(point, condition string) JevRecommendation {
		return JevRecommendation{Point: point, Verdict: "do not adopt",
			Reason:    "no vendor terms cover any data class; " + condition,
			BreakEven: breakEven}
	}
	return []JevRecommendation{
		verdict("screen.message", "revisit behind escalation once terms cover INTERNAL content and German/Arabic accuracy is measured"),
		verdict("audience.todo_reminder", "revisit behind escalation once terms cover INTERNAL content; the public direction needs a measured false-public rate near zero"),
		verdict("search.route", "revisit once terms cover INTERNAL content; a wrong route costs a whole answer"),
		verdict("reaction.choose", "revisit once terms cover INTERNAL content; lowest stakes, first candidate for a pilot"),
		verdict("support.severity", "revisit once terms cover PII and CASE content; support mail is never PUBLIC"),
		verdict("support.needs_review", "revisit once terms cover PII and CASE content; abstention must reach a person"),
		verdict("instruction.detect", "revisit once terms cover every class the agent reads; a guardrail that misses is load-bearing"),
		verdict("citation.supported", "revisit once terms cover INTERNAL content; the seal's word is only as good as this check"),
		verdict("scope.skills", "revisit once terms cover INTERNAL content; the monitor must not depend on the vendor it watches failing open"),
		verdict("model.profile", "revisit once terms cover INTERNAL content; routing must stay available when the vendor is not"),
	}
}

// agentcost003CurrentDecisionMicros is the mean measured micros per decision
// call in the AGENTCOST-001 seeded replay.
func agentcost003CurrentDecisionMicros() int64 {
	var total, calls int64
	for _, line := range Agentcost001SeededLines() {
		if line.Kind == agentmodel.CallKindDecision {
			total += line.CostMicros
			calls++
		}
	}
	if calls == 0 {
		return 0
	}
	return (total + calls/2) / calls
}

// RenderAgentcost003Record renders the checked-in Jev research record.
func RenderAgentcost003Record() string {
	var out strings.Builder
	out.WriteString("# Jev research (AGENTCOST-003)\n\n")
	out.WriteString("Researched 2026-10-22 against primary sources (vendor documentation, homepage, error reference) and secondary coverage where primary sources are silent. Vendor-claimed figures are labelled as such throughout.\n\n")
	for i, finding := range Agentcost003Findings() {
		status := "primary sources silent or secondary only"
		if finding.Confirmed {
			status = "confirmed from the vendor's own properties"
		}
		fmt.Fprintf(&out, "## %d. %s (%s)\n\n%s\n\nSources: %s.\n\n", i+1, finding.Question, status, finding.Answer, strings.Join(finding.Sources, "; "))
	}
	out.WriteString("## Measured comparison: method and harness verification\n\n")
	cases := Agentcost003ScreeningCases()
	taskSuite, reminderSuite := agenteval.TaskCatcherAmbientSuite(), agenteval.ReminderAmbientSuite()
	fmt.Fprintf(&out, "Cases: %d screening questions from the Task Catcher and Reminder ambient suites (%d German and %d Arabic labels among them), synthetic members only, plus the persona-suite shape with peer-injection cases. Metrics per provider: accuracy, reliability curve with expected calibration error, abstain rate at the AGENTCOST-002 screening threshold (0.80), mean latency and cost per thousand decisions.\n\n", len(cases), agentcost003CountGerman(append(append([]agenteval.AmbientCase{}, taskSuite...), reminderSuite...)), agentcost003CountArabic(append(append([]agenteval.AmbientCase{}, taskSuite...), reminderSuite...)))
	out.WriteString("| provider | accuracy | expected calibration error | abstain rate | mean latency ms | cost per 1k decisions |\n| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, provider := range Agentcost003StubProviders() {
		score := ScoreJevStubProvider(provider, cases, 0.80)
		fmt.Fprintf(&out, "| %s (stub) | %.3f | %.3f | %.3f | %d | %d micros |\n", score.Name, score.Accuracy, score.ECE, score.AbstainRate, score.MeanLatencyMillis, score.CostPerThousandMicros)
	}
	out.WriteString("\nThese rows verify the harness executes; they are not measurements of Jev, of the general model or of the classifier. Real measurement is blocked on vendor access and is recorded as not performed.\n\n")
	out.WriteString("## Data classes that may be sent\n\nNone. No vendor terms, DPA, retention, training-use or region statement covers PUBLIC, INTERNAL, PII, COMPENSATION, BANK, MEDICAL, IMMIGRATION, CASE or SPECIAL_CATEGORY content.\n\n")
	out.WriteString("## Recommendation per decision point\n\n")
	for _, recommendation := range Agentcost003Recommendations() {
		fmt.Fprintf(&out, "- %s: %s. %s. Break-even: %s.\n", recommendation.Point, recommendation.Verdict, recommendation.Reason, recommendation.BreakEven)
	}
	return out.String()
}

// agentcost003CountGerman counts suite labels in German by marker words; the
// suites are fixed text, so the count is exact.
func agentcost003CountGerman(suite []agenteval.AmbientCase) int {
	n := 0
	for _, label := range suite {
		for _, word := range []string{"Ich ", "Wir ", "Kann ", "Erinnere ", "kannst ", "morgen", "Freitag", "Frist", "Treffen", "Anruf", "Die Frist", "Das Treffen", "Ich habe", "Wir müssen", "die Folien", "den Lieferanten", "den Raum", "die Lizenz", "den Bericht"} {
			if strings.Contains(label.Text, word) {
				n++
				break
			}
		}
	}
	return n
}

// agentcost003CountArabic counts suite labels containing Arabic script.
func agentcost003CountArabic(suite []agenteval.AmbientCase) int {
	n := 0
	for _, label := range suite {
		for _, runeValue := range label.Text {
			if runeValue >= 0x0600 && runeValue <= 0x06FF {
				n++
				break
			}
		}
	}
	return n
}
