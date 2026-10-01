package version

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type PageBinding struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type PageBindingChangeKind string

const (
	PageBindingAdded   PageBindingChangeKind = "ADDED"
	PageBindingRemoved PageBindingChangeKind = "REMOVED"
	PageBindingRetyped PageBindingChangeKind = "RETYPED"
)

type PageBindingChange struct {
	Kind PageBindingChangeKind
	Path string
	From string
	To   string
}

type PageBindingRebase struct {
	Bindings   []PageBinding
	Changes    []PageBindingChange
	Unresolved []string
}

var (
	ErrPageBindingInvalid    = errors.New("workflow version: invalid page binding")
	ErrPageBindingUnresolved = errors.New("workflow version: page binding is unresolved")
)

// RebasePageBindings carries a page override onto a new workflow version. A
// removed or retyped input is never silently dropped; it remains in the
// unresolved list until an author repairs the override.
func RebasePageBindings(current, previousInputs, nextInputs []PageBinding) PageBindingRebase {
	previous := bindingMap(previousInputs)
	next := bindingMap(nextInputs)
	result := PageBindingRebase{Bindings: append([]PageBinding(nil), current...)}
	for path, before := range previous {
		after, exists := next[path]
		if !exists {
			result.Changes = append(result.Changes, PageBindingChange{Kind: PageBindingRemoved, Path: path, From: before.Type})
			result.Unresolved = append(result.Unresolved, path)
			continue
		}
		if before.Type != after.Type {
			result.Changes = append(result.Changes, PageBindingChange{Kind: PageBindingRetyped, Path: path, From: before.Type, To: after.Type})
			result.Unresolved = append(result.Unresolved, path)
		}
	}
	for path, after := range next {
		if _, existed := previous[path]; !existed {
			result.Changes = append(result.Changes, PageBindingChange{Kind: PageBindingAdded, Path: path, To: after.Type})
			result.Unresolved = append(result.Unresolved, path)
		}
	}
	sort.Slice(result.Changes, func(i, j int) bool { return result.Changes[i].Path < result.Changes[j].Path })
	sort.Strings(result.Unresolved)
	return result
}

func ValidatePageBindings(bindings, workflowInputs []PageBinding) error {
	inputs := bindingMap(workflowInputs)
	seen := make(map[string]bool, len(bindings))
	for _, binding := range bindings {
		path, typ := strings.TrimSpace(binding.Path), strings.TrimSpace(binding.Type)
		if path == "" || typ == "" || seen[path] {
			return fmt.Errorf("%w: duplicate or empty binding %q", ErrPageBindingInvalid, path)
		}
		seen[path] = true
		input, ok := inputs[path]
		if !ok {
			return fmt.Errorf("%w: %s", ErrPageBindingUnresolved, path)
		}
		if input.Type != typ {
			return fmt.Errorf("%w: %s changed from %s to %s", ErrPageBindingUnresolved, path, typ, input.Type)
		}
	}
	return nil
}

func bindingMap(values []PageBinding) map[string]PageBinding {
	result := make(map[string]PageBinding, len(values))
	for _, value := range values {
		value.Path, value.Type = strings.TrimSpace(value.Path), strings.TrimSpace(value.Type)
		if value.Path != "" {
			result[value.Path] = value
		}
	}
	return result
}
