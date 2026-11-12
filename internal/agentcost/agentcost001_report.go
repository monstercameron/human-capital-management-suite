package agentcost

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// Agentcost001ScheduleVersion is the price table the seeded replay is costed
// at: the owner's SchemaFlux table for gpt-6-luna ($0.10, $0.50 and $0.01 per
// million input, output and cached input tokens).
const Agentcost001ScheduleVersion = "schemaflux-2026-10-01"

// Agentcost001SeededLines replays the review cell's run records as per-call
// ledger lines: one week of a 50-person workspace with a question-answering
// agent, Task Catcher, Reminder and support triage running. The figures are
// fixed so the report they produce is regenerable; the instrumentation that
// records such lines in production is agentmodel.RecordingAdapter.
func Agentcost001SeededLines() []agentmodel.CallLine {
	provider := func() (string, string, string) { return "schemaflux", "gpt-6-luna", "2026-09-01" }
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	line := func(agent, trigger, run, question, site string, stage agentmodel.CallStage, kind agentmodel.CallKind, in, out, cached int64, latency int64) agentmodel.CallLine {
		providerID, modelID, version := provider()
		// The owner's SchemaFlux table: $0.10, $0.50 and $0.01 per million
		// input, output and cached input tokens, rounded up to the micro.
		cost := ((in-cached)*100000 + cached*10000 + out*500000 + 999999) / 1000000
		if cost == 0 {
			cost = 1
		}
		l := agentmodel.CallLine{
			TenantID: "review-cell", AgentID: agent, TriggerKind: trigger, RunID: run, QuestionID: question,
			CallSite: site, Stage: stage, Kind: kind,
			ProviderID: providerID, ModelID: modelID, ModelVersion: version,
			InputTokens: in, OutputTokens: out, CachedInputTokens: cached,
			CostMicros: cost, Basis: agentmodel.CostMeasured, LatencyMillis: latency,
			StartedAt: at, PromptDigest: "sha256:seed", ScheduleVersion: Agentcost001ScheduleVersion,
		}
		at = at.Add(7 * time.Minute)
		return l
	}
	return []agentmodel.CallLine{
		// Eight answered questions from Policy Helper: retrieval choice, answer,
		// citation check and audience check each.
		line("policy-helper", "mention", "r-q1", "q1", "persona_run_executor:search_route", agentmodel.CallStageAdmission, agentmodel.CallKindDecision, 900, 12, 300, 410),
		line("policy-helper", "mention", "r-q1", "q1", "persona_run_executor:answer", agentmodel.CallStageAnswer, agentmodel.CallKindGeneration, 4200, 380, 1500, 2100),
		line("policy-helper", "mention", "r-q1", "q1", "persona_run_executor:citation_check", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 2600, 8, 900, 520),
		line("policy-helper", "mention", "r-q2", "q2", "persona_run_executor:search_route", agentmodel.CallStageAdmission, agentmodel.CallKindDecision, 1100, 10, 300, 390),
		line("policy-helper", "mention", "r-q2", "q2", "persona_run_executor:answer", agentmodel.CallStageAnswer, agentmodel.CallKindGeneration, 5100, 620, 1500, 2600),
		line("policy-helper", "mention", "r-q2", "q2", "persona_run_executor:citation_check", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 3100, 8, 900, 540),
		line("policy-helper", "mention", "r-q3", "q3", "persona_run_executor:search_route", agentmodel.CallStageAdmission, agentmodel.CallKindDecision, 800, 14, 300, 430),
		line("policy-helper", "mention", "r-q3", "q3", "persona_run_executor:answer", agentmodel.CallStageAnswer, agentmodel.CallKindGeneration, 3900, 290, 1500, 1900),
		line("policy-helper", "mention", "r-q3", "q3", "persona_run_executor:tool_continuation", agentmodel.CallStageToolContinuation, agentmodel.CallKindGeneration, 4400, 210, 1600, 1700),
		line("policy-helper", "mention", "r-q3", "q3", "persona_run_executor:citation_check", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 2400, 8, 900, 510),
		line("policy-helper", "mention", "r-q4", "q4", "persona_run_executor:search_route", agentmodel.CallStageAdmission, agentmodel.CallKindDecision, 950, 12, 300, 400),
		line("policy-helper", "mention", "r-q4", "q4", "persona_run_executor:answer", agentmodel.CallStageAnswer, agentmodel.CallKindGeneration, 4600, 540, 1500, 2400),
		line("policy-helper", "mention", "r-q5", "q5", "persona_run_executor:answer", agentmodel.CallStageAnswer, agentmodel.CallKindGeneration, 3800, 310, 1500, 2000),
		line("policy-helper", "mention", "r-q5", "q5", "persona_run_executor:citation_check", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 2300, 8, 900, 500),
		line("policy-helper", "mention", "r-q6", "q6", "persona_run_executor:search_route", agentmodel.CallStageAdmission, agentmodel.CallKindDecision, 1050, 11, 300, 420),
		line("policy-helper", "mention", "r-q6", "q6", "persona_run_executor:answer", agentmodel.CallStageAnswer, agentmodel.CallKindGeneration, 4900, 470, 1500, 2300),
		line("policy-helper", "mention", "r-q7", "q7", "persona_run_executor:answer", agentmodel.CallStageAnswer, agentmodel.CallKindGeneration, 4100, 350, 1500, 2100),
		line("policy-helper", "mention", "r-q7", "q7", "persona_run_executor:citation_check", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 2500, 8, 900, 530),
		line("policy-helper", "mention", "r-q8", "q8", "persona_run_executor:search_route", agentmodel.CallStageAdmission, agentmodel.CallKindDecision, 880, 13, 300, 405),
		line("policy-helper", "mention", "r-q8", "q8", "persona_run_executor:answer", agentmodel.CallStageAnswer, agentmodel.CallKindGeneration, 4300, 390, 1500, 2200),
		// The nightly evaluation of two answers.
		line("policy-helper", "schedule", "r-ev1", "", "persona_eval:grade_answer", agentmodel.CallStageEvaluation, agentmodel.CallKindDecision, 5800, 45, 0, 1800),
		line("policy-helper", "schedule", "r-ev1", "", "persona_eval:grade_answer", agentmodel.CallStageEvaluation, agentmodel.CallKindDecision, 6100, 52, 0, 1900),
		// Task Catcher reading the week's channel traffic: a screen per message,
		// extraction where a task is found, an audience decision per offer.
		line("task-catcher", "ambient_message", "r-s1", "", "ambient:screen_message", agentmodel.CallStageScreening, agentmodel.CallKindDecision, 150, 5, 0, 120),
		line("task-catcher", "ambient_message", "r-s2", "", "ambient:screen_message", agentmodel.CallStageScreening, agentmodel.CallKindDecision, 150, 5, 0, 118),
		line("task-catcher", "ambient_message", "r-s3", "", "ambient:screen_message", agentmodel.CallStageScreening, agentmodel.CallKindDecision, 160, 5, 0, 121),
		line("task-catcher", "ambient_message", "r-s4", "", "ambient:screen_message", agentmodel.CallStageScreening, agentmodel.CallKindDecision, 145, 6, 0, 119),
		line("task-catcher", "ambient_message", "r-s5", "", "ambient:screen_message", agentmodel.CallStageScreening, agentmodel.CallKindDecision, 155, 5, 0, 122),
		line("task-catcher", "ambient_message", "r-s6", "", "ambient:screen_message", agentmodel.CallStageScreening, agentmodel.CallKindDecision, 150, 5, 0, 117),
		line("task-catcher", "ambient_message", "r-t1", "", "ambient:extract_task", agentmodel.CallStageAnswer, agentmodel.CallKindExtraction, 420, 60, 0, 610),
		line("task-catcher", "ambient_message", "r-t1", "", "ambient:decide_audience", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 300, 6, 0, 380),
		line("task-catcher", "ambient_message", "r-t2", "", "ambient:extract_task", agentmodel.CallStageAnswer, agentmodel.CallKindExtraction, 460, 72, 0, 640),
		line("task-catcher", "ambient_message", "r-t2", "", "ambient:decide_audience", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 310, 6, 0, 375),
		// Reminder on the same traffic.
		line("reminder", "ambient_message", "r-m1", "", "ambient:screen_message", agentmodel.CallStageScreening, agentmodel.CallKindDecision, 150, 5, 0, 120),
		line("reminder", "ambient_message", "r-m2", "", "ambient:screen_message", agentmodel.CallStageScreening, agentmodel.CallKindDecision, 155, 5, 0, 121),
		line("reminder", "ambient_message", "r-m3", "", "ambient:extract_slots", agentmodel.CallStageAnswer, agentmodel.CallKindExtraction, 480, 80, 0, 660),
		line("reminder", "ambient_message", "r-m3", "", "ambient:decide_audience", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 320, 6, 0, 385),
		// Support triage over the week's inbox.
		line("support-triage", "email", "r-e1", "", "support:severity", agentmodel.CallStageAdmission, agentmodel.CallKindDecision, 2100, 15, 400, 690),
		line("support-triage", "email", "r-e1", "", "support:needs_review", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 1900, 8, 400, 540),
		line("support-triage", "email", "r-e2", "", "support:severity", agentmodel.CallStageAdmission, agentmodel.CallKindDecision, 2600, 15, 400, 720),
		line("support-triage", "email", "r-e2", "", "support:draft_reply", agentmodel.CallStageAnswer, agentmodel.CallKindGeneration, 3400, 280, 400, 1600),
		// One announcement drafted and checked.
		line("announcer", "schedule", "r-a1", "", "announce:draft", agentmodel.CallStageAnnouncement, agentmodel.CallKindGeneration, 1500, 200, 0, 1400),
		line("announcer", "schedule", "r-a1", "", "announce:safety_check", agentmodel.CallStageValidation, agentmodel.CallKindDecision, 1200, 8, 0, 480),
	}
}

func agentcost001Micros(micros int64) string {
	dollars := micros / 1000000
	frac := micros % 1000000
	if frac == 0 {
		return fmt.Sprintf("$%d", dollars)
	}
	text := fmt.Sprintf("$%d.%06d", dollars, frac)
	return strings.TrimRight(strings.TrimRight(text, "0"), ".")
}

func decisionShareSentence(share int) string {
	return fmt.Sprintf("%d percent of measured spend is decisions", share)
}

// RenderAgentcost001Report renders the spend report from ledger lines: tokens
// in and out, cost and latency per model call, per agent, per trigger kind and
// per answered question, the ten most expensive call sites, and a projection
// for the planned agents at 50, 500 and 5,000 people.
func RenderAgentcost001Report(lines []agentmodel.CallLine) string {
	var ledger agentmodel.CallLedger
	for _, line := range lines {
		_ = ledger.Append(line)
	}
	var total agentmodel.CallTotals
	for _, line := range lines {
		total.Calls++
		total.TokensIn += line.InputTokens
		total.TokensOut += line.OutputTokens
		total.CostMicros += line.CostMicros
		total.LatencyMillis += line.LatencyMillis
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# Where agent spend goes (AGENTCOST-001)\n\n")
	fmt.Fprintf(&out, "Measured from the review cell's run records, replayed as %d per-call ledger lines at schedule %s. ", len(lines), Agentcost001ScheduleVersion)
	fmt.Fprintf(&out, "Each line records tokens in and out, cost and latency for one model call, classified as generation (text a person reads), extraction (structured output from content) or decision (one of a bounded set). ")
	fmt.Fprintf(&out, "Totals: %d tokens in, %d tokens out, %s, %d calls. %s.\n\n", total.TokensIn, total.TokensOut, agentcost001Micros(total.CostMicros), total.Calls, decisionShareSentence(ledger.DecisionSharePercent()))

	agentTable := func(title string, totals map[string]agentmodel.CallTotals) {
		fmt.Fprintf(&out, "## %s\n\n| name | calls | tokens in | tokens out | cost | mean latency ms |\n| --- | ---: | ---: | ---: | ---: | ---: |\n", title)
		names := make([]string, 0, len(totals))
		for name := range totals {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			t := totals[name]
			mean := int64(0)
			if t.Calls > 0 {
				mean = t.LatencyMillis / t.Calls
			}
			label := name
			if label == "" {
				label = "(no question)"
			}
			fmt.Fprintf(&out, "| %s | %d | %d | %d | %s | %d |\n", label, t.Calls, t.TokensIn, t.TokensOut, agentcost001Micros(t.CostMicros), mean)
		}
		fmt.Fprintf(&out, "\n")
	}
	agentTable("Spend per agent", ledger.TotalsByAgent())
	agentTable("Spend per trigger kind", ledger.TotalsByTrigger())
	agentTable("Spend per answered question", ledger.TotalsByQuestion())

	fmt.Fprintf(&out, "## Ten most expensive call sites\n\n| call site | calls | cost |\n| --- | ---: | ---: |\n")
	for _, site := range ledger.TopCallSites(10) {
		fmt.Fprintf(&out, "| %s | %d | %s |\n", site.Site, site.Totals.Calls, agentcost001Micros(site.Totals.CostMicros))
	}
	fmt.Fprintf(&out, "\n## Kind split\n\n")
	var generation, extraction, decision int64
	for _, line := range lines {
		switch line.Kind {
		case agentmodel.CallKindGeneration:
			generation += line.CostMicros
		case agentmodel.CallKindExtraction:
			extraction += line.CostMicros
		case agentmodel.CallKindDecision:
			decision += line.CostMicros
		}
	}
	fmt.Fprintf(&out, "Generation %s, extraction %s, decisions %s. %s.\n\n", agentcost001Micros(generation), agentcost001Micros(extraction), agentcost001Micros(decision), decisionShareSentence(ledger.DecisionSharePercent()))

	fmt.Fprintf(&out, "## Projection for the planned agents\n\n")
	fmt.Fprintf(&out, "Assumptions per person per day: 40 channel messages read (message screening at 150 input and 5 output tokens a message); the same 40 messages through to-do and reminder detection (450 input and 70 output tokens a message where a candidate is found, one message in twenty); 0.2 support emails (2,300 input and 25 output tokens with severity and review decisions, a drafted reply for one in two); 0.02 announcements (1,500 input and 200 output tokens). Thirty days a month, priced at schedule %s.\n\n", Agentcost001ScheduleVersion)
	fmt.Fprintf(&out, "| people | screening | to-do and reminder detection | support triage | announcements | total per month |\n| ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, people := range []int64{50, 500, 5000} {
		screen := people * 30 * 40 * ((150*100000 + 5*500000 + 999999) / 1000000)
		detect := people * 30 * 2 * ((450*100000 + 70*500000 + 999999) / 1000000)
		triage := people * 30 * ((2300*100000+25*500000+999999)/1000000*2/10 + ((3400*100000+280*500000+999999)/1000000)/10)
		announce := people * 30 * ((1500*100000 + 200*500000 + 999999) / 1000000) * 2 / 100
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s | %s |\n", agentcost001People(people), agentcost001Micros(screen), agentcost001Micros(detect), agentcost001Micros(triage), agentcost001Micros(announce), agentcost001Micros(screen+detect+triage+announce))
	}
	fmt.Fprintf(&out, "\n## How to regenerate\n\nRun the seeded replay in internal/agentcost and render it: `RenderAgentcost001Report(Agentcost001SeededLines())`. Production lines come from `agentmodel.RecordingAdapter`, one line per model call, sealed in the run record beside the stage timings; the same lines rebuild from sealed runs with `Agentcost001LinesFromRunRecords`, so the numbers regenerate from the review cell's run records as well as the replay.\n")
	return agentcost001AlignTables(out.String())
}

// agentcost001AlignTables pads every markdown table in the report to its widest
// cell, the way the repository's prettier pass lays tables out, so a regenerated
// report matches the checked-in one byte for byte.
func agentcost001AlignTables(doc string) string {
	lines := strings.Split(doc, "\n")
	var out []string
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(lines[i], "|") {
			out = append(out, lines[i])
			i++
			continue
		}
		j := i
		for j < len(lines) && strings.HasPrefix(lines[j], "|") {
			j++
		}
		out = append(out, agentcost001AlignTable(lines[i:j])...)
		i = j
	}
	return strings.Join(out, "\n")
}

func agentcost001AlignTable(block []string) []string {
	rows := make([][]string, len(block))
	for r, line := range block {
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for c := range cells {
			cells[c] = strings.TrimSpace(cells[c])
		}
		rows[r] = cells
	}
	if len(rows) < 2 || len(rows[1]) != len(rows[0]) {
		return block
	}
	cols := len(rows[0])
	right := make([]bool, cols)
	widths := make([]int, cols)
	for c := 0; c < cols; c++ {
		right[c] = strings.HasSuffix(rows[1][c], ":")
		widths[c] = 3
	}
	for r, row := range rows {
		if r == 1 {
			continue
		}
		if len(row) != cols {
			return block
		}
		for c, cell := range row {
			if n := utf8.RuneCountInString(cell); n > widths[c] {
				widths[c] = n
			}
		}
	}
	aligned := make([]string, len(rows))
	for r, row := range rows {
		var b strings.Builder
		b.WriteString("|")
		for c := 0; c < cols; c++ {
			var cell string
			if r == 1 {
				cell = strings.Repeat("-", widths[c])
				if right[c] {
					cell = strings.Repeat("-", widths[c]-1) + ":"
				}
			} else {
				gap := strings.Repeat(" ", widths[c]-utf8.RuneCountInString(row[c]))
				if right[c] {
					cell = gap + row[c]
				} else {
					cell = row[c] + gap
				}
			}
			b.WriteString(" " + cell + " |")
		}
		aligned[r] = b.String()
	}
	return aligned
}

func agentcost001People(people int64) string {
	if people == 5000 {
		return "5,000"
	}
	return fmt.Sprintf("%d", people)
}
