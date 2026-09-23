package openapi

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// IntegrationContractPath is the hand-authored integration design contract,
// relative to the repository root. Protobuf remains canonical; this
// document projects it for external partners.
const IntegrationContractPath = "schema/openapi/integration.openapi.yaml"

// TodosPath is the backlog the contract's x-hcmnext-todo extensions name,
// relative to the repository root.
const TodosPath = "planning/todos.md"

// IntegrationFinding is one place where the design contract drifted from
// the served surface or the backlog: an operation without an owning todo,
// a todo that does not exist, a SERVED operation with no matching
// generated RPC, or a PLANNED operation whose todo is already closed.
type IntegrationFinding struct {
	Path    string
	Method  string
	Message string
}

func (f IntegrationFinding) String() string {
	return fmt.Sprintf("%s %s: %s", f.Method, f.Path, f.Message)
}

// todoRef matches a ticked or unticked backlog entry and captures its state
// and ID: IDs look like INTAPI-001, RBAC-RT-011 or REV-030-01.
var todoRef = regexp.MustCompile(`^-\s*\[(x| )\]\s*` + "`" + `([A-Za-z]+(?:-[A-Za-z]+)?-[0-9]+(?:-[0-9]+)?)` + "`")

// servedRPCs returns the set of "Service/Method" RPCs the generated
// document declares, derived from its Connect paths: the path
// "/hcmnext.registry.v1.RegistryService/ListIntentDefinitions" declares
// the RPC "hcmnext.registry.v1.RegistryService/ListIntentDefinitions",
// which is exactly the form the design contract's x-hcmnext-rpc carries.
func servedRPCs(doc map[string]any) map[string]bool {
	out := map[string]bool{}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return out
	}
	for path := range paths {
		if name, ok := strings.CutPrefix(path, "/"); ok {
			out[name] = true
		}
	}
	return out
}

// todoState maps every backlog ID to whether its checkbox is ticked.
func todoState(data []byte) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := todoRef.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out[m[2]] = m[1] == "x"
		}
	}
	return out
}

var openapiMethods = []string{"get", "post", "put", "patch", "delete"}

func opString(op map[string]any, key string) (string, bool) {
	raw, ok := op[key]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", false
	}
	return strings.TrimSpace(s), true
}

// CheckIntegrationAlignment fails the INTAPI-009 contract: every operation
// in the design document must name an existing backlog todo, every
// operation marked SERVED must carry the backing RPC and that RPC must be
// in the generated document, and no operation may stay PLANNED once its
// todo is closed. Findings sort by path, method and message so output is
// stable across runs.
func CheckIntegrationAlignment(repoRoot string) ([]IntegrationFinding, error) {
	contractData, err := os.ReadFile(filepath.Join(repoRoot, IntegrationContractPath))
	if err != nil {
		return nil, fmt.Errorf("openapi: read %s: %w", IntegrationContractPath, err)
	}
	var contract map[string]any
	if err := yaml.Unmarshal(contractData, &contract); err != nil {
		return nil, fmt.Errorf("openapi: parse %s: %w", IntegrationContractPath, err)
	}
	generatedData, err := os.ReadFile(filepath.Join(repoRoot, DefaultOutputPath))
	if err != nil {
		return nil, fmt.Errorf("openapi: read %s: %w", DefaultOutputPath, err)
	}
	var generated map[string]any
	if err := yaml.Unmarshal(generatedData, &generated); err != nil {
		return nil, fmt.Errorf("openapi: parse %s: %w", DefaultOutputPath, err)
	}
	todosData, err := os.ReadFile(filepath.Join(repoRoot, TodosPath))
	if err != nil {
		return nil, fmt.Errorf("openapi: read %s: %w", TodosPath, err)
	}
	served := servedRPCs(generated)
	todos := todoState(todosData)

	var findings []IntegrationFinding
	paths, _ := contract["paths"].(map[string]any)
	for _, path := range sortedKeys(paths) {
		item, _ := paths[path].(map[string]any)
		for _, method := range openapiMethods {
			raw, ok := item[method]
			if !ok {
				continue
			}
			op, _ := raw.(map[string]any)
			at := IntegrationFinding{Path: path, Method: strings.ToUpper(method)}
			todo, hasTodo := opString(op, "x-hcmnext-todo")
			if !hasTodo {
				at.Message = "operation names no x-hcmnext-todo backlog owner"
				findings = append(findings, at)
				continue
			}
			closed, exists := todos[todo]
			if !exists {
				at.Message = fmt.Sprintf("x-hcmnext-todo %q names no backlog item in %s", todo, TodosPath)
				findings = append(findings, at)
				continue
			}
			status, hasStatus := opString(op, "x-hcmnext-status")
			if !hasStatus {
				at.Message = "operation names no x-hcmnext-status (SERVED or PLANNED)"
				findings = append(findings, at)
				continue
			}
			rpc, hasRPC := opString(op, "x-hcmnext-rpc")
			switch status {
			case "SERVED":
				if !hasRPC {
					at.Message = "operation is SERVED but names no x-hcmnext-rpc backing call"
					findings = append(findings, at)
					continue
				}
				if !served[rpc] {
					at.Message = fmt.Sprintf("x-hcmnext-rpc %q is SERVED but matches no operation in %s", rpc, DefaultOutputPath)
					findings = append(findings, at)
				}
			case "PLANNED":
				if closed {
					at.Message = fmt.Sprintf("x-hcmnext-todo %q is closed while the operation is still PLANNED", todo)
					findings = append(findings, at)
				}
			default:
				at.Message = fmt.Sprintf("x-hcmnext-status %q is neither SERVED nor PLANNED", status)
				findings = append(findings, at)
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		if findings[i].Method != findings[j].Method {
			return findings[i].Method < findings[j].Method
		}
		return findings[i].Message < findings[j].Message
	})
	return findings, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
