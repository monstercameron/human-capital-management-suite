// Package todoregistry parses planning/todos.md into a machine-readable
// registry of TodoContract summaries (GOV-002).
package todoregistry

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Todo represents a single parsed todo from the backlog.
type Todo struct {
	ID                    string            `json:"id"`
	Phase                 string            `json:"phase"`
	Model                 string            `json:"model"`
	Title                 string            `json:"title"`
	Section               string            `json:"section"`
	Depends               []string          `json:"depends"`
	Test                  string            `json:"test"`
	TestMatrix            map[string]string `json:"test_matrix"`
	Red                   string            `json:"red"`
	Green                 string            `json:"green"`
	Refactor              string            `json:"refactor"`
	Refs                  string            `json:"refs"`
	Retired               bool              `json:"retired"`
	Done                  bool              `json:"done"`
	Evidence              string            `json:"evidence,omitempty"`
	Disposition           string            `json:"disposition,omitempty"`
	CapabilityClass       string            `json:"capability_class,omitempty"`
	Owner                 string            `json:"owner,omitempty"`
	Role                  string            `json:"-"`
	IntentContextConflict bool              `json:"-"`
	CapabilityConflict    bool              `json:"-"`
	OwnerConflict         bool              `json:"-"`
	prePromotionTagSeen   bool
	// PrePromotionExploratory marks conformance evidence as exploratory
	// scaffolding that intentionally precedes workflow promotion.
	PrePromotionExploratory bool `json:"pre_promotion_exploratory,omitempty"`
	Line                    int  `json:"line,omitempty"`
}

// UnresolvedDependency names a dependency edge that does not resolve to a
// known todo ID.
type UnresolvedDependency struct {
	From string
	To   string
}

// Valid phases for a todo.
var validPhases = map[string]bool{
	"P0":          true,
	"GATE_A":      true,
	"GATE_B":      true,
	"GATE_C":      true,
	"PHASE_2":     true,
	"PHASE_3":     true,
	"PHASE_4":     true,
	"PHASE_5":     true,
	"CONFORMANCE": true,
	"DESIGN":      true,
	"RETIRED":     true,
	"OUT":         true,
}

// Valid model intelligence labels.
var validModels = map[string]bool{
	"LUNA":     true,
	"TERRA":    true,
	"SOL_LOW":  true,
	"SOL_HIGH": true,
}

// ParseTodos parses the markdown todos file and returns a slice of Todo structs.
func ParseTodos(content string) ([]Todo, []error) {
	var todos []Todo
	var errs []error

	lines := strings.Split(content, "\n")
	var currentTodo *Todo
	var currentSection string
	seenIDs := make(map[string]bool)

	for i, line := range lines {
		lineNum := i + 1

		// Track sections (headers starting with ##)
		if after, ok := strings.CutPrefix(line, "## "); ok {
			currentSection = strings.TrimSpace(after)
		}

		// Look for todo marker
		if strings.HasPrefix(line, "- [ ] `") || strings.HasPrefix(line, "- [x] `") {
			// Save previous todo if exists
			if currentTodo != nil {
				if err := validateTodo(currentTodo); err != nil {
					errs = append(errs, fmt.Errorf("line %d: %w", currentTodo.Line, err))
				} else {
					todos = append(todos, *currentTodo)
				}
			}

			// Parse new todo from title line
			t, err := parseTodoTitle(line, lineNum, currentSection)
			if err != nil {
				errs = append(errs, fmt.Errorf("line %d: %w", lineNum, err))
				currentTodo = nil
				continue
			}
			if seenIDs[t.ID] {
				errs = append(errs, fmt.Errorf("line %d: duplicate ID: %s", lineNum, t.ID))
			}
			currentTodo = t
			seenIDs[t.ID] = true
		} else if currentTodo != nil && strings.HasPrefix(strings.TrimSpace(line), "- **") {
			parseTodoField(currentTodo, line)
		}
	}

	// Save final todo
	if currentTodo != nil {
		if err := validateTodo(currentTodo); err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", currentTodo.Line, err))
		} else {
			todos = append(todos, *currentTodo)
		}
	}

	return todos, errs
}

// parseTodoTitle parses the title line and extracts ID, phase, model, and title.
func parseTodoTitle(line string, lineNum int, section string) (*Todo, error) {
	// Format: - [ ] `ID` **[PHASE][MODEL] Title.**
	done := false
	rest, ok := strings.CutPrefix(line, "- [ ] `")
	if !ok {
		rest, ok = strings.CutPrefix(line, "- [x] `")
		done = true
	}
	if !ok {
		return nil, fmt.Errorf("invalid todo title format: %s", line)
	}

	id, rest, ok := strings.Cut(rest, "`")
	if !ok {
		return nil, fmt.Errorf("invalid todo title format: missing closing backtick")
	}

	rest, ok = strings.CutPrefix(rest, " **[")
	if !ok {
		return nil, fmt.Errorf("invalid todo title format: expected space **[ after ID")
	}

	phase, rest, ok := strings.Cut(rest, "]")
	if !ok {
		return nil, fmt.Errorf("invalid todo title format: missing phase ]")
	}

	rest, ok = strings.CutPrefix(rest, "[")
	if !ok {
		return nil, fmt.Errorf("invalid todo title format: expected [ for model")
	}

	model, rest, ok := strings.Cut(rest, "]")
	if !ok {
		return nil, fmt.Errorf("invalid todo title format: missing model ]")
	}

	rest, ok = strings.CutPrefix(rest, " ")
	if !ok {
		return nil, fmt.Errorf("invalid todo title format: expected space after model")
	}

	// Title is everything before .**
	title, ok := strings.CutSuffix(rest, ".**")
	if !ok {
		return nil, fmt.Errorf("invalid todo title format: title doesn't end with .**")
	}

	return &Todo{
		ID:      id,
		Phase:   phase,
		Model:   model,
		Title:   title,
		Section: section,
		Retired: phase == "RETIRED",
		Done:    done,
		Line:    lineNum,
	}, nil
}

// parseTodoField parses a field line like "  - **Depends:** ...".
func parseTodoField(t *Todo, line string) {
	line = strings.TrimSpace(line)
	rest, ok := strings.CutPrefix(line, "- **")
	if !ok {
		return
	}

	fieldName, fieldValue, ok := strings.Cut(rest, ":")
	if !ok {
		return
	}

	fieldValue = cleanFieldValue(fieldValue)

	// Evidence fields carry the tick date, and partial evidence says so:
	// "Evidence (2026-09-05)", "Evidence (partial, 2026-09-06)". Every dated
	// form is the same field; a literal date list would silently drop every
	// tick made on another day.
	if fieldName == "Evidence" || strings.HasPrefix(fieldName, "Evidence (") {
		// A todo may carry several evidence fields (a partial one from an
		// earlier day and the final one); every test they name counts.
		if t.Evidence != "" {
			t.Evidence += " | "
		}
		t.Evidence += fieldValue
		return
	}
	switch fieldName {
	case "Depends":
		t.Depends = parseDependencies(fieldValue)
	case "TEST":
		t.Test = strings.Trim(fieldValue, "`")
	case "TEST MATRIX":
		t.TestMatrix = parseTestMatrix(fieldValue)
	case "RED":
		t.Red = fieldValue
	case "GREEN":
		t.Green = fieldValue
	case "REFACTOR":
		t.Refactor = fieldValue
	case "Refs":
		if t.Refs != "" {
			t.Refs += "\n"
		}
		t.Refs += fieldValue
	case "Disposition":
		t.Disposition = fieldValue
	case "INTENT CONTEXT":
		for _, part := range strings.Split(strings.Trim(fieldValue, "`"), ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "ROLE":
				role := strings.TrimSpace(value)
				if t.Role != "" && t.Role != role {
					t.IntentContextConflict = true
				} else if t.Role == "" {
					t.Role = role
				}
			case "PRE_PROMOTION_EXPLORATORY":
				exploratory := strings.TrimSpace(value) == "true"
				if t.prePromotionTagSeen && t.PrePromotionExploratory != exploratory {
					t.IntentContextConflict = true
				}
				t.PrePromotionExploratory = t.PrePromotionExploratory || exploratory
				t.prePromotionTagSeen = true
			case "CAPABILITY":
				capabilityClass := strings.TrimSpace(value)
				if t.CapabilityClass != "" && t.CapabilityClass != capabilityClass {
					t.CapabilityConflict = true
				} else if t.CapabilityClass == "" {
					t.CapabilityClass = capabilityClass
				}
			case "OWNER":
				owner := strings.TrimSpace(value)
				if t.Owner != "" && t.Owner != owner {
					t.OwnerConflict = true
				} else if t.Owner == "" {
					t.Owner = owner
				}
			}
		}
	}
}

// cleanFieldValue strips the markdown artifacts left around a field value:
// the closing "**" of the "**FIELD:**" label, surrounding whitespace and a
// single trailing period.
func cleanFieldValue(value string) string {
	value = strings.TrimSpace(value)
	if after, ok := strings.CutPrefix(value, "**"); ok {
		value = strings.TrimSpace(after)
	}
	if after, ok := strings.CutSuffix(value, "**"); ok {
		value = strings.TrimSpace(after)
	}
	value = strings.TrimSuffix(value, ".")
	return value
}

// parseTestMatrix parses a TEST MATRIX field value into a map of test class
// to test name. Format: `CLASS=Name`; `CLASS=Name`; ...
func parseTestMatrix(value string) map[string]string {
	matrix := make(map[string]string)
	if value == "" {
		return matrix
	}
	for tok := range strings.SplitSeq(value, ";") {
		tok = strings.TrimSpace(tok)
		tok = strings.Trim(tok, "`")
		class, name, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		matrix[strings.TrimSpace(class)] = strings.TrimSpace(name)
	}
	return matrix
}

// isRangeConnector reports whether s (already trimmed) denotes a
// start-end range between two backticked IDs.
func isRangeConnector(s string) bool {
	switch s {
	case "-", "–", "—":
		return true
	}
	return strings.EqualFold(s, "to")
}

// parseDependencies extracts dependency IDs from a Depends field value.
// Recognized forms: `ID`, `ID`-`ID` / `ID`–`ID` / `ID` to `ID` (range,
// expanded when the two IDs share a prefix), "none" (empty), and prose
// (ignored - only backtick-wrapped tokens are considered IDs).
func parseDependencies(depStr string) []string {
	deps := []string{}

	depStr = strings.TrimSpace(depStr)
	if strings.EqualFold(depStr, "none") || depStr == "" {
		return deps
	}

	// Find every backtick-wrapped token and its byte offsets.
	type token struct {
		text       string
		start, end int // offsets of the surrounding backticks (inclusive/exclusive)
	}
	var tokens []token
	for i := 0; i < len(depStr); {
		start := strings.IndexByte(depStr[i:], '`')
		if start == -1 {
			break
		}
		start += i
		end := strings.IndexByte(depStr[start+1:], '`')
		if end == -1 {
			break
		}
		end += start + 1
		tokens = append(tokens, token{text: depStr[start+1 : end], start: start, end: end + 1})
		i = end + 1
	}

	for i := 0; i < len(tokens); i++ {
		if i+1 < len(tokens) {
			gap := strings.TrimSpace(depStr[tokens[i].end:tokens[i+1].start])
			if isRangeConnector(gap) {
				deps = append(deps, expandIDRange(tokens[i].text, tokens[i+1].text)...)
				i++
				continue
			}
		}
		deps = append(deps, tokens[i].text)
	}

	return deps
}

// expandIDRange expands a range like TOOL-010-TOOL-015 to individual IDs.
// When the two IDs do not share a numeric prefix (or either fails to
// parse), both endpoints are returned as literal dependencies rather than
// expanded.
func expandIDRange(start, end string) []string {
	startPrefix, startNum, width, okStart := extractIDComponents(start)
	endPrefix, endNum, _, okEnd := extractIDComponents(end)

	if !okStart || !okEnd || startPrefix != endPrefix || startNum > endNum {
		return []string{start, end}
	}

	result := make([]string, 0, endNum-startNum+1)
	for n := startNum; n <= endNum; n++ {
		result = append(result, fmt.Sprintf("%s-%0*d", startPrefix, width, n))
	}
	return result
}

// extractIDComponents extracts the prefix, numeric value and zero-padded
// width of the trailing numeric segment from an ID like "TOOL-010" or
// "WF-STEP-001".
func extractIDComponents(id string) (prefix string, num int, width int, ok bool) {
	idx := strings.LastIndex(id, "-")
	if idx == -1 {
		return "", 0, 0, false
	}
	prefix = id[:idx]
	numStr := id[idx+1:]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return "", 0, 0, false
	}
	return prefix, n, len(numStr), true
}

// validateTodo checks that a todo has all required fields and valid values.
func validateTodo(t *Todo) error {
	if t.ID == "" {
		return fmt.Errorf("missing ID")
	}
	if t.Phase == "" {
		return fmt.Errorf("missing Phase")
	}
	if !validPhases[t.Phase] {
		return fmt.Errorf("invalid phase: %s", t.Phase)
	}
	if t.Model == "" {
		return fmt.Errorf("missing Model")
	}
	if !validModels[t.Model] {
		return fmt.Errorf("invalid model: %s", t.Model)
	}
	if t.Title == "" {
		return fmt.Errorf("missing Title")
	}
	if t.Test == "" {
		return fmt.Errorf("missing TEST field")
	}
	if len(t.TestMatrix) == 0 {
		return fmt.Errorf("missing TEST MATRIX field")
	}
	if t.Red == "" {
		return fmt.Errorf("missing RED field")
	}
	if t.Green == "" {
		return fmt.Errorf("missing GREEN field")
	}
	if t.Refactor == "" {
		return fmt.Errorf("missing REFACTOR field")
	}
	if t.Refs == "" {
		return fmt.Errorf("missing Refs field")
	}
	if t.CapabilityConflict {
		return fmt.Errorf("conflicting CAPABILITY declarations")
	}
	if t.OwnerConflict {
		return fmt.Errorf("conflicting OWNER declarations")
	}
	if t.CapabilityClass != "" && t.CapabilityClass != CapabilityRuntime && t.CapabilityClass != CapabilityLibrary {
		return fmt.Errorf("invalid CAPABILITY: %s", t.CapabilityClass)
	}
	if t.Owner != "" && !validOwnerSlug(t.Owner) {
		return fmt.Errorf("invalid OWNER: %s", t.Owner)
	}
	if (t.CapabilityClass == "") != (t.Owner == "") {
		return fmt.Errorf("CAPABILITY and OWNER must be declared together")
	}

	return nil
}

// Capability class values are explicit declarations. They are deliberately
// not inferred from TODO ID prefixes or package roots.
const (
	CapabilityRuntime = "RUNTIME"
	CapabilityLibrary = "LIBRARY"
)

func validOwnerSlug(owner string) bool {
	if owner == "" || owner[0] < 'A' || owner[0] > 'Z' {
		return false
	}
	for i := 1; i < len(owner); i++ {
		c := owner[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// ResolveDependencies checks that all dependencies resolve to an existing
// todo ID (RETIRED todos remain in the registry and resolve as satisfied).
// Returns every unresolved (from, to) edge.
func ResolveDependencies(todos []Todo) []UnresolvedDependency {
	idMap := make(map[string]bool, len(todos))
	for _, t := range todos {
		idMap[t.ID] = true
	}

	var unresolved []UnresolvedDependency
	for _, t := range todos {
		for _, dep := range t.Depends {
			if !idMap[dep] {
				unresolved = append(unresolved, UnresolvedDependency{From: t.ID, To: dep})
			}
		}
	}

	return unresolved
}

// KnownDefect is an allow-listed unresolved dependency edge, recorded with
// its rationale in definitions/planning/known-defects.yaml.
type KnownDefect struct {
	From   string `yaml:"from"`
	To     string `yaml:"to"`
	Reason string `yaml:"reason"`
}

type knownDefectsFile struct {
	KnownDefects []KnownDefect `yaml:"known_defects"`
}

// LoadKnownDefects reads the known-defects allow-list. A missing file is
// treated as an empty list.
func LoadKnownDefects(path string) ([]KnownDefect, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var parsed knownDefectsFile
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return parsed.KnownDefects, nil
}

// ToJSON marshals todos to JSON with stable key ordering: sorted by ID,
// LF-terminated, UTF-8 without BOM.
func ToJSON(todos []Todo) ([]byte, error) {
	sorted := make([]Todo, len(todos))
	copy(sorted, todos)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})

	// Line numbers are a parse-time artifact only; never emit them.
	output := make([]Todo, len(sorted))
	for i, t := range sorted {
		t.Line = 0
		if t.Depends == nil {
			t.Depends = []string{}
		}
		if t.TestMatrix == nil {
			t.TestMatrix = map[string]string{}
		}
		output[i] = t
	}

	b, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(b, '\n'), nil
}
