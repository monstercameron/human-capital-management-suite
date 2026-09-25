// Package oraclestrength classifies the test oracles declared by planning
// todos as strong or WEAK_ORACLE (GOV-021). It is kernel-pure: Markdown
// block scanning, reviewed-pattern classification and text emission only,
// with no database, network or mutable global state.
package oraclestrength

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// WeakOracle is the single classification code for an oracle that cannot
// satisfy a todo: it asserts only execution signals, leans on a coverage
// percentage, omits prohibited-effect bounds, snapshots unstable output,
// accepts contradictory outcomes or names no concrete failing case.
const WeakOracle = "WEAK_ORACLE"

// Finding is one source-located oracle-strength diagnostic.
type Finding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	TodoID  string `json:"todo_id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// String renders the stable form consumed by planning tools and CI.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s: %s", f.File, f.Line, f.TodoID, f.Code, f.Message)
}

// MarshalFindings renders findings as canonical indented JSON.
func MarshalFindings(findings []Finding) ([]byte, error) {
	ordered := append([]Finding(nil), findings...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Line != ordered[j].Line {
			return ordered[i].Line < ordered[j].Line
		}
		if ordered[i].TodoID != ordered[j].TodoID {
			return ordered[i].TodoID < ordered[j].TodoID
		}
		return ordered[i].Message < ordered[j].Message
	})
	rendered, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(rendered, '\n'), nil
}

type field struct {
	name  string
	value string
	line  int
}

type todoBlock struct {
	id     string
	line   int
	fields []field
}

var (
	todoTitleRe = regexp.MustCompile("^- \\[([ x])\\] `([^`]+)`")

	// weakSignalRe matches execution-only or coverage-percentage assertions:
	// no-panic, non-nil, status-200, mock-invocation and line-coverage
	// language. A match classifies WEAK_ORACLE only when the same oracle
	// carries no strong marker.
	weakSignalRe = regexp.MustCompile(`(?i)\b(no panic|without panic|does not panic|panic-free|panics? (only|alone)|non-?nil|not nil|nil (only|check)|status 200|200 ok|http 200|returns? 200\b|mock[^.]{0,60}(called|invoked|invocation|verified|interaction)|interaction[^.]{0,40}verified|verify[^.]{0,40}mock|line coverage|coverage[^.]{0,40}(percent|%|80|90|100)|covers?[^.]{0,40}%|%[^.]{0,40}cover)`)

	// strongMarkerRe matches the bounds that redeem an oracle: exact typed
	// outcomes, counts, digests and prohibited-effect language.
	strongMarkerRe = regexp.MustCompile(`(?i)\b(exact|typed|precisely|counts?|exactly|zero|digest|canonical|pinned|byte-identical|deterministic|stable|reproducib|prohibit|forbid|refus|reject|bound|identical)`)

	// effectRe matches GREEN language that claims persisted or emitted
	// effects. Such claims need a bound from strongMarkerRe.
	effectRe = regexp.MustCompile(`(?i)\b(persist|ledger|outbox|publish|emitt?|events?|effects?|writ|stor|send|notif|provis|payment|charg|delet|revok)\b`)

	// snapshotRe matches snapshot-style oracles that need a digest anchor.
	snapshotRe = regexp.MustCompile(`(?i)\b(snapshot|golden|recorded output|cassette)\b`)

	// contradictoryRe matches either/or spans in an accepted outcome. A span
	// fires only when it carries no strong marker: an enumerated typed
	// outcome set ("exactly one of APPROVED, REJECTED, ...") or a pinned
	// alternative is a precise oracle, while a bare "either this or that"
	// accepts contradictory outputs.
	contradictoryRe = regexp.MustCompile(`(?i)either\b.{1,80}?\bor\b`)

	// failureTokenRe matches the concrete-failure vocabulary a RED must use
	// to name its seeded defect. Stems intentionally cover inflections
	// ("fails", "rejected", "leaked", "violations") so a conjugated
	// failure verb still counts as a named failing case.
	failureTokenRe = regexp.MustCompile(`(?i)\b(fail|error|miss|invalid|wrong|unexpect|unknown|unauthori|forbidden|deni|expir|duplicat|stal|unregister|untrack|orphan|tamper|inject|fault|defect|mutant|violat|reject|refus|empt|malform|timeout|corrupt|conflict|contradict|placeholder|weak|surviv|leak|reorder|unorder|drop|los|race|deadlock|starv|overflow|truncat|skew|drift|regress|mismatch|diverg)`)

	backtickLiteralRe = regexp.MustCompile("`[^`]+`")
)

// CheckMarkdown classifies every todo oracle in a Markdown corpus,
// returning one WEAK_ORACLE finding per violated rule.
func CheckMarkdown(markdown, file string) ([]Finding, error) {
	_, _ = todoregistry.ParseTodos(markdown)
	blocks := parseBlocks(markdown)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no todo blocks found in %s", file)
	}
	var findings []Finding
	for _, block := range blocks {
		findings = append(findings, checkBlock(block, file)...)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Message < findings[j].Message
	})
	return findings, nil
}

func parseBlocks(markdown string) []todoBlock {
	var blocks []todoBlock
	var current *todoBlock
	for lineNumber, line := range strings.Split(markdown, "\n") {
		lineNumber++
		if match := todoTitleRe.FindStringSubmatch(line); match != nil {
			if current != nil {
				blocks = append(blocks, *current)
			}
			current = &todoBlock{id: match[2], line: lineNumber}
			continue
		}
		if current == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- **") {
			continue
		}
		body := strings.TrimPrefix(trimmed, "- **")
		separator := strings.Index(body, ":**")
		if separator < 0 {
			continue
		}
		name := strings.TrimSpace(body[:separator])
		value := strings.TrimSpace(body[separator+3:])
		current.fields = append(current.fields, field{name: name, value: value, line: lineNumber})
	}
	if current != nil {
		blocks = append(blocks, *current)
	}
	return blocks
}

func checkBlock(block todoBlock, file string) []Finding {
	var findings []Finding
	add := func(line int, message string) {
		findings = append(findings, Finding{File: file, Line: line, TodoID: block.id, Code: WeakOracle, Message: message})
	}
	var red, green string
	redLine, greenLine := block.line, block.line
	for _, f := range block.fields {
		switch canonicalField(f.name) {
		case "RED":
			red = f.value
			redLine = f.line
		case "GREEN":
			green = f.value
			greenLine = f.line
		}
	}
	combined := strings.ToLower(red + " " + green)
	if red == "" {
		add(redLine, "RED oracle is missing: an oracle must name its failing case")
		return findings
	}
	if green == "" {
		add(greenLine, "GREEN oracle is missing: an oracle must state the exact accepted outcome and prohibited effects")
	}
	if weak := weakSignalRe.FindString(combined); weak != "" && !strongMarkerRe.MatchString(combined) {
		add(greenLine, fmt.Sprintf("execution-only assertion (%q) without an exact typed outcome, count, digest or prohibited-effect bound", strings.TrimSpace(weak)))
	}
	if effectRe.MatchString(strings.ToLower(green)) && !strongMarkerRe.MatchString(strings.ToLower(green)) {
		add(greenLine, "GREEN states persisted or emitted effects without a prohibited-outcome bound: name exact counts and the zero side effects")
	}
	if snapshotRe.MatchString(combined) && !strongMarkerRe.MatchString(combined) {
		add(greenLine, "snapshot or golden oracle without a canonical digest, pinned bytes or deterministic repeat: unstable recorded output cannot satisfy a todo")
	}
	for _, span := range contradictoryRe.FindAllString(strings.ToLower(green), -1) {
		if strongMarkerRe.MatchString(span) {
			continue
		}
		add(greenLine, fmt.Sprintf("oracle accepts alternative outcomes (%q): contradictory outputs cannot share one GREEN", strings.TrimSpace(span)))
	}
	redLower := strings.ToLower(red)
	if !failureTokenRe.MatchString(redLower) && !backtickLiteralRe.MatchString(red) && !strongMarkerRe.MatchString(redLower) {
		add(redLine, "RED names no concrete failing case: cite the seeded defect, invalid fact or rejected transition by name")
	}
	return findings
}

func canonicalField(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	if strings.HasPrefix(name, "EVIDENCE (") {
		return "EVIDENCE"
	}
	return name
}
