// Package workflowconformance ensures conformance todos do not present
// research workflows as contracted before a trusted promotion receipt exists.
// Explicit pre-promotion exploratory scaffolding is allowed.
package workflowconformance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowregistry"
)

var workflowRefPattern = regexp.MustCompile("(?:^|[^A-Za-z0-9_])((?:\\./)?(?:planning/)?workflows/[A-Za-z0-9._/-]+\\.md)")

// Finding identifies a completed conformance todo citing a workflow that is
// not truthfully exploratory or contracted with trusted promotion evidence.
type Finding struct {
	TodoID string
	Path   string
	State  string
	Issue  string
}

func (f Finding) String() string {
	if f.Issue != "" {
		return fmt.Sprintf("%s: workflow %s has invalid promotion metadata: %s", f.TodoID, f.Path, f.Issue)
	}
	if f.State == "CONTRACTED" {
		return fmt.Sprintf("%s: workflow %s declares CONTRACTED without a trusted workflow-bound promotion receipt", f.TodoID, f.Path)
	}
	return fmt.Sprintf("%s: ticked CONFORMANCE todo cites workflow %s in state %q without PRE_PROMOTION_EXPLORATORY=true", f.TodoID, f.Path, f.State)
}

// Check reads catalog workflow documents named in Refs for completed
// CONFORMANCE todos. Research states need an explicit exploratory tag.
// CONTRACTED is fail-closed until workflowpromotion receipts are bound to a
// workflow identity and verified by a repository trust root; today's receipt
// schema has neither binding nor a promotion trust root.
func Check(root string, todos []todoregistry.Todo) ([]Finding, error) {
	workflowRoot := filepath.Join(root, "planning", "workflows")
	registry, err := workflowregistry.Scan(workflowRoot)
	if err != nil {
		return nil, fmt.Errorf("scan workflow documents: %w", err)
	}
	catalogWorkflows := make(map[string]bool)
	for _, artifact := range registry.Artifacts {
		if artifact.Kind == workflowregistry.KindWorkflow {
			catalogWorkflows[filepath.ToSlash(filepath.Join("planning", "workflows", filepath.FromSlash(artifact.Path)))] = true
		}
	}

	var findings []Finding
	for _, todo := range todos {
		if !todo.Done || todo.Phase != "CONFORMANCE" {
			continue
		}
		for _, ref := range workflowRefs(todo.Refs) {
			path := normalizeWorkflowPath(ref)
			if path == "" || !catalogWorkflows[path] {
				continue
			}
			if todo.Role != "CONFORMANCE" && !todo.IntentContextConflict {
				continue
			}
			if todo.IntentContextConflict {
				findings = append(findings, Finding{TodoID: todo.ID, Path: path, Issue: "conflicting ROLE or exploratory declarations in INTENT CONTEXT"})
				continue
			}
			if todo.PrePromotionExploratory {
				continue
			}
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				return nil, fmt.Errorf("read workflow ref for %s (%s): %w", todo.ID, path, err)
			}
			metadata := parseMetadata(string(content))
			if len(metadata.WorkflowIDs) != 1 || strings.TrimSpace(metadata.WorkflowIDs[0]) == "" {
				findings = append(findings, Finding{TodoID: todo.ID, Path: path, Issue: "workflow_id must appear exactly once with a value in the leading metadata block"})
				continue
			}
			if len(metadata.States) != 1 || strings.TrimSpace(metadata.States[0]) == "" {
				findings = append(findings, Finding{TodoID: todo.ID, Path: path, Issue: "state must appear exactly once with a value in the leading metadata block"})
				continue
			}
			state := strings.TrimSpace(metadata.States[0])
			findings = append(findings, Finding{TodoID: todo.ID, Path: path, State: state})
		}
	}
	return findings, nil
}

type workflowMetadata struct {
	WorkflowIDs []string
	States      []string
}

// parseMetadata reads only YAML front matter or the leading fenced identity
// block. Occurrences in prose, examples, and later code samples cannot
// override or repair the authoritative declaration.
func parseMetadata(content string) workflowMetadata {
	content = strings.TrimPrefix(content, "\ufeff")
	lines := strings.Split(content, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	var block []string
	if start < len(lines) && strings.TrimSpace(lines[start]) == "---" {
		for i := start + 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				block = lines[start+1 : i]
				break
			}
		}
	} else {
		opening := -1
		inIdentity := false
		for i := start; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "## ") {
				if inIdentity {
					break
				}
				inIdentity = trimmed == "## Identity and scope"
				continue
			}
			if !inIdentity {
				continue
			}
			if strings.HasPrefix(trimmed, "```") {
				if opening < 0 {
					opening = i
					continue
				}
				block = lines[opening+1 : i]
				break
			}
		}
	}
	metadata := workflowMetadata{}
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "workflow_id":
			metadata.WorkflowIDs = append(metadata.WorkflowIDs, strings.TrimSpace(value))
		case "state":
			metadata.States = append(metadata.States, strings.TrimSpace(value))
		}
	}
	return metadata
}

func workflowRefs(refs string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, match := range workflowRefPattern.FindAllStringSubmatch(refs, -1) {
		ref := normalizeWorkflowPath(match[1])
		if ref == "" {
			continue
		}
		if !seen[ref] {
			seen[ref] = true
			result = append(result, ref)
		}
	}
	return result
}

func normalizeWorkflowPath(ref string) string {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.Trim(ref, "`<>"))))
	clean = strings.TrimPrefix(clean, "./")
	if strings.HasPrefix(clean, "workflows/") {
		return filepath.ToSlash(filepath.Join("planning", filepath.FromSlash(clean)))
	}
	if strings.HasPrefix(clean, "planning/workflows/") {
		return clean
	}
	return ""
}
