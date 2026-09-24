// Package dependencygraph validates the planning todo dependency graph
// (GOV-016). It accepts only registered, machine-readable todo IDs, expands
// well-formed ranges, rejects cycles, and prevents Gate A work from depending
// on later-gate implementation work.
package dependencygraph

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// Diagnostic is one stable graph validation finding.
type Diagnostic struct {
	Line       int
	TodoID     string
	Code       string
	Dependency string
	Message    string
}

// String renders a diagnostic in the stable file/line-oriented form used by
// the planning checkers. Line is the todo title line (or the Depends line for
// syntax diagnostics).
func (d Diagnostic) String() string {
	if d.TodoID == "" {
		return fmt.Sprintf("line %d: %s: %s", d.Line, d.Code, d.Message)
	}
	return fmt.Sprintf("line %d: %s: %s: %s", d.Line, d.TodoID, d.Code, d.Message)
}

var todoIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:-[A-Za-z0-9]+)*-[0-9]+$`)

// Check validates an already parsed todo registry. It reports unknown edges,
// Gate A phase inversions, duplicate IDs, and every independently discoverable
// dependency cycle in deterministic order.
func Check(todos []todoregistry.Todo) []Diagnostic {
	ids := make(map[string]todoregistry.Todo, len(todos))
	var findings []Diagnostic
	for _, todo := range todos {
		if previous, exists := ids[todo.ID]; exists {
			findings = append(findings, Diagnostic{
				Line:    todo.Line,
				TodoID:  todo.ID,
				Code:    "DUPLICATE_ID",
				Message: fmt.Sprintf("todo ID is already declared on line %d", previous.Line),
			})
			continue
		}
		ids[todo.ID] = todo
	}

	adjacency := make(map[string][]string, len(ids))
	inversions := make(map[string]bool)
	for _, todo := range todos {
		for _, dependency := range todo.Depends {
			dep, exists := ids[dependency]
			if !exists {
				findings = append(findings, Diagnostic{
					Line:       todo.Line,
					TodoID:     todo.ID,
					Code:       "UNKNOWN_DEPENDENCY",
					Dependency: dependency,
					Message:    fmt.Sprintf("dependency %s is not registered", dependency),
				})
				continue
			}
			if todo.Phase == "GATE_A" && laterGate(dep.Phase) && !inversions[todo.ID+"|"+dependency] {
				inversions[todo.ID+"|"+dependency] = true
				findings = append(findings, Diagnostic{
					Line:       todo.Line,
					TodoID:     todo.ID,
					Code:       "GATE_A_PHASE_INVERSION",
					Dependency: dependency,
					Message:    fmt.Sprintf("Gate A todo cannot depend on %s implementation %s", dep.Phase, dependency),
				})
			}
			adjacency[todo.ID] = append(adjacency[todo.ID], dependency)
		}
		sort.Strings(adjacency[todo.ID])
	}

	// A Gate A todo must not smuggle a later-gate implementation through an
	// otherwise innocuous intermediary. The generated graph's Gate A closure
	// is what matters, so inspect transitive paths as well as direct edges.
	for _, todo := range todos {
		if todo.Phase != "GATE_A" {
			continue
		}
		// A dependency diamond is common in the backlog. Traverse each
		// reachable node once per Gate A root so validation is linear in the
		// root's closure rather than exponential in the number of paths to a
		// shared prerequisite. cycleDiagnostics below remains responsible for
		// reporting the cycle itself; this pass only establishes reachability.
		var walk func(string, []string, map[string]bool)
		walk = func(node string, path []string, visited map[string]bool) {
			if visited[node] {
				return
			}
			visited[node] = true
			dep := ids[node]
			if len(path) > 1 && laterGate(dep.Phase) {
				key := todo.ID + "|" + node
				if !inversions[key] {
					inversions[key] = true
					findings = append(findings, Diagnostic{
						Line:       todo.Line,
						TodoID:     todo.ID,
						Code:       "GATE_A_PHASE_INVERSION",
						Dependency: node,
						Message:    fmt.Sprintf("Gate A dependency closure reaches %s implementation %s via %s", dep.Phase, node, strings.Join(append(path, node), " -> ")),
					})
				}
			}
			for _, child := range adjacency[node] {
				walk(child, append(path, node), visited)
			}
		}
		visited := map[string]bool{todo.ID: true}
		for _, dependency := range adjacency[todo.ID] {
			walk(dependency, []string{todo.ID}, visited)
		}
	}

	findings = append(findings, cycleDiagnostics(ids, adjacency)...)
	return sortDiagnostics(findings)
}

func laterGate(phase string) bool {
	switch phase {
	case "GATE_B", "GATE_C", "PHASE_2", "PHASE_3", "PHASE_4", "PHASE_5":
		return true
	default:
		return false
	}
}

// CheckMarkdown parses a planning Markdown corpus, validates each raw Depends
// field (including range syntax and prose), then validates the resulting graph.
func CheckMarkdown(markdown string) ([]Diagnostic, error) {
	todos, parseErrs := todoregistry.ParseTodos(markdown)
	if len(parseErrs) != 0 {
		return nil, fmt.Errorf("parse planning todos: %v", parseErrs)
	}

	depsByID := make(map[string][]string)
	var findings []Diagnostic
	var currentID string
	var currentTodoLine int
	for lineNumber, line := range strings.Split(markdown, "\n") {
		if id, ok := parseTodoID(line); ok {
			currentID = id
			currentTodoLine = lineNumber + 1
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "- **Depends:") {
			continue
		}
		body, ok := dependencyBody(line)
		if !ok {
			continue
		}
		deps, syntaxFindings := parseDependencyField(currentID, lineNumber+1, body)
		findings = append(findings, syntaxFindings...)
		depsByID[currentID] = deps
		_ = currentTodoLine
	}

	for i := range todos {
		if deps, ok := depsByID[todos[i].ID]; ok {
			todos[i].Depends = deps
		}
	}
	findings = append(findings, Check(todos)...)
	return sortDiagnostics(findings), nil
}

func parseTodoID(line string) (string, bool) {
	if !strings.HasPrefix(line, "- [ ") && !strings.HasPrefix(line, "- [x") {
		return "", false
	}
	start := strings.IndexByte(line, '`')
	if start == -1 {
		return "", false
	}
	end := strings.IndexByte(line[start+1:], '`')
	if end == -1 {
		return "", false
	}
	return line[start+1 : start+1+end], true
}

func dependencyBody(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	const prefix = "- **Depends:"
	if !strings.HasPrefix(trimmed, prefix) {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	if !strings.HasPrefix(rest, "**") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, "**")), true
}

type dependencyToken struct {
	value string
	start int
	end   int
}

func parseDependencyField(todoID string, line int, raw string) ([]string, []Diagnostic) {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "."))
	if strings.EqualFold(raw, "none") || raw == "" {
		return nil, nil
	}

	var findings []Diagnostic
	var tokens []dependencyToken
	for offset := 0; offset < len(raw); {
		start := strings.IndexByte(raw[offset:], '`')
		if start == -1 {
			break
		}
		start += offset
		end := strings.IndexByte(raw[start+1:], '`')
		if end == -1 {
			findings = append(findings, syntaxDiagnostic(todoID, line, "MALFORMED_DEPENDENCY", "dependency contains an unmatched backtick"))
			return nil, findings
		}
		end += start + 1
		tokens = append(tokens, dependencyToken{value: raw[start+1 : end], start: start, end: end + 1})
		offset = end + 1
	}
	if len(tokens) == 0 {
		return nil, append(findings, syntaxDiagnostic(todoID, line, "PROSE_DEPENDENCY", "dependency must contain backtick-wrapped todo IDs, not prose"))
	}

	deps := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if !todoIDRe.MatchString(token.value) {
			findings = append(findings, syntaxDiagnostic(todoID, line, "MALFORMED_DEPENDENCY", fmt.Sprintf("%q is not a todo ID", token.value)))
			continue
		}
		if i+1 < len(tokens) {
			gap := strings.TrimSpace(raw[token.end:tokens[i+1].start])
			if isRangeConnector(gap) {
				rangeDeps, ok := expandRange(token.value, tokens[i+1].value)
				if !ok {
					findings = append(findings, syntaxDiagnostic(todoID, line, "MALFORMED_RANGE", fmt.Sprintf("range %s %s %s is not a valid ordered range", token.value, gap, tokens[i+1].value)))
				} else {
					deps = append(deps, rangeDeps...)
				}
				// The range consumed both tokens. Without skipping the endpoint,
				// it is added a second time on the next iteration.
				i++
				continue
			}
			if !isListSeparator(gap) {
				findings = append(findings, syntaxDiagnostic(todoID, line, "PROSE_DEPENDENCY", fmt.Sprintf("unexpected prose between dependencies: %q", gap)))
			}
		}
		deps = append(deps, token.value)
	}

	first := tokens[0].start
	last := tokens[len(tokens)-1].end
	if !isListSeparator(strings.TrimSpace(raw[:first])) || !isListSeparator(strings.TrimSpace(raw[last:])) {
		findings = append(findings, syntaxDiagnostic(todoID, line, "PROSE_DEPENDENCY", "dependency contains prose outside ID tokens"))
	}
	return deps, findings
}

func syntaxDiagnostic(todoID string, line int, code, message string) Diagnostic {
	return Diagnostic{Line: line, TodoID: todoID, Code: code, Message: message}
}

func isRangeConnector(gap string) bool {
	return gap == "-" || gap == "–" || gap == "—" || strings.EqualFold(gap, "to")
}

func isListSeparator(gap string) bool {
	if gap == "" {
		return true
	}
	for _, r := range gap {
		if r != ',' && r != ';' && r != ' ' && r != '\t' && r != '\n' {
			return false
		}
	}
	return true
}

func expandRange(start, end string) ([]string, bool) {
	startPrefix, startNumber, startWidth, okStart := splitNumericID(start)
	endPrefix, endNumber, endWidth, okEnd := splitNumericID(end)
	if !okStart || !okEnd || startPrefix != endPrefix || startWidth != endWidth || startNumber > endNumber {
		return nil, false
	}
	if endNumber-startNumber > 10000 {
		return nil, false
	}
	result := make([]string, 0, endNumber-startNumber+1)
	for number := startNumber; number <= endNumber; number++ {
		result = append(result, fmt.Sprintf("%s-%0*d", startPrefix, startWidth, number))
	}
	return result, true
}

func splitNumericID(id string) (string, int, int, bool) {
	idx := strings.LastIndexByte(id, '-')
	if idx <= 0 || idx == len(id)-1 {
		return "", 0, 0, false
	}
	numberText := id[idx+1:]
	number, err := strconv.Atoi(numberText)
	if err != nil {
		return "", 0, 0, false
	}
	return id[:idx], number, len(numberText), true
}

func cycleDiagnostics(ids map[string]todoregistry.Todo, adjacency map[string][]string) []Diagnostic {
	state := make(map[string]uint8, len(ids))
	stack := []string{}
	positions := make(map[string]int, len(ids))
	seen := make(map[string]bool)
	var findings []Diagnostic

	var visit func(string)
	visit = func(node string) {
		state[node] = 1
		positions[node] = len(stack)
		stack = append(stack, node)
		for _, dependency := range adjacency[node] {
			switch state[dependency] {
			case 0:
				visit(dependency)
			case 1:
				cycle := append([]string(nil), stack[positions[dependency]:]...)
				cycle = append(cycle, dependency)
				key := canonicalCycle(cycle)
				if !seen[key] {
					seen[key] = true
					first := ids[cycle[0]]
					findings = append(findings, Diagnostic{
						Line:    first.Line,
						TodoID:  first.ID,
						Code:    "DEPENDENCY_CYCLE",
						Message: fmt.Sprintf("dependency cycle: %s", strings.Join(cycle, " -> ")),
					})
				}
			}
		}
		stack = stack[:len(stack)-1]
		delete(positions, node)
		state[node] = 2
	}

	nodes := make([]string, 0, len(ids))
	for id := range ids {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)
	for _, node := range nodes {
		if state[node] == 0 {
			visit(node)
		}
	}
	return findings
}

func canonicalCycle(cycle []string) string {
	if len(cycle) <= 1 {
		return strings.Join(cycle, " -> ")
	}
	withoutRepeat := cycle[:len(cycle)-1]
	best := ""
	for i := range withoutRepeat {
		rotated := append([]string{}, withoutRepeat[i:]...)
		rotated = append(rotated, withoutRepeat[:i]...)
		rotated = append(rotated, rotated[0])
		candidate := strings.Join(rotated, " -> ")
		if best == "" || candidate < best {
			best = candidate
		}
	}
	return best
}

func sortDiagnostics(findings []Diagnostic) []Diagnostic {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		if findings[i].TodoID != findings[j].TodoID {
			return findings[i].TodoID < findings[j].TodoID
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Message < findings[j].Message
	})
	return findings
}
