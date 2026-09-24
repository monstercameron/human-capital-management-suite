package traceability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

const TickedMissingCommandTarget = "MISSING_COMMAND_TARGET"

var evidenceCommandTokenRe = regexp.MustCompile("`([^`]+)`")

// CheckEvidenceCommandTargets checks that test commands cited by completed
// todos resolve to an existing package, script, or test file in the checkout.
// It does not execute the evidence command; it verifies that its named target
// exists so a plausible-looking but impossible command cannot satisfy the
// ticked-todo gate.
func CheckEvidenceCommandTargets(root string, todos []todoregistry.Todo) []TickedFinding {
	var findings []TickedFinding
	for _, td := range todos {
		if !td.Done || td.Retired || strings.TrimSpace(td.Evidence) == "" {
			continue
		}
		for _, token := range evidenceCommandTokenRe.FindAllStringSubmatch(td.Evidence, -1) {
			command := strings.TrimSpace(token[1])
			if !tickedCommandRe.MatchString("`" + command + "`") {
				continue
			}
			if reason := evidenceCommandTargetProblem(root, command); reason != "" {
				findings = append(findings, TickedFinding{
					ID:     td.ID,
					Kind:   TickedMissingCommandTarget,
					Detail: fmt.Sprintf("Evidence command %q has no repository target: %s", command, reason),
				})
			}
		}
	}
	return findings
}

func evidenceCommandTargetProblem(root, command string) string {
	words, ok := splitCommandWords(command)
	if !ok || len(words) == 0 {
		return "command could not be parsed"
	}
	switch {
	case len(words) >= 2 && words[0] == "go" && words[1] == "test":
		packages := goTestPackages(words[2:])
		for _, pkg := range packages {
			if !repositoryPackageExists(root, pkg) {
				return fmt.Sprintf("Go package %q does not exist", pkg)
			}
		}
	case len(words) >= 3 && words[0] == "npm" && words[1] == "run":
		if !npmScriptExists(root, words[2]) {
			return fmt.Sprintf("npm script %q does not exist", words[2])
		}
	case len(words) >= 3 && words[0] == "node" && words[1] == "--test":
		for _, arg := range words[2:] {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			if !repositoryFileExists(root, arg) {
				return fmt.Sprintf("Node test target %q does not exist", arg)
			}
		}
	case len(words) >= 3 && words[0] == "npx" && (words[1] == "vitest" || words[1] == "playwright"):
		for i := 2; i < len(words); i++ {
			arg := words[i]
			if strings.HasPrefix(arg, "-") {
				if strings.Contains(arg, "=") || arg == "--" {
					continue
				}
				if i+1 < len(words) && !strings.HasPrefix(words[i+1], "-") {
					i++
					if isFilePathArgument(words[i]) && !repositoryFileExists(root, words[i]) {
						return fmt.Sprintf("runner config target %q does not exist", words[i])
					}
				}
				continue
			}
			if isFilePathArgument(arg) && !repositoryFileExists(root, arg) {
				return fmt.Sprintf("test target %q does not exist", arg)
			}
		}
	}
	return ""
}

func splitCommandWords(command string) ([]string, bool) {
	var words []string
	var word strings.Builder
	var quote rune
	escaped := false
	started := false
	for _, r := range command {
		if escaped {
			word.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			started = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		word.WriteRune(r)
		started = true
	}
	if escaped || quote != 0 {
		return nil, false
	}
	if started {
		words = append(words, word.String())
	}
	return words, true
}

var goTestFlagsWithValue = map[string]bool{
	"-bench": true, "-benchtime": true, "-blockprofile": true,
	"-blockprofilerate": true, "-covermode": true, "-coverpkg": true,
	"-coverprofile": true, "-cpuprofile": true, "-cpu": true,
	"-exec": true, "-fuzz": true, "-fuzzminimizetime": true,
	"-fuzztime": true, "-gcflags": true, "-ldflags": true,
	"-list": true, "-memprofile": true, "-memprofilerate": true,
	"-mod": true, "-modfile": true, "-mutexprofile": true,
	"-mutexprofilefraction": true, "-outputdir": true, "-overlay": true,
	"-parallel": true, "-p": true, "-pkgdir": true, "-run": true,
	"-shuffle": true, "-skip": true, "-tags": true, "-testlogfile": true,
	"-timeout": true, "-toolexec": true, "-trace": true, "-vet": true,
}

func goTestPackages(args []string) []string {
	var packages []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-args" {
			break
		}
		if strings.HasPrefix(arg, "-") {
			if name, _, hasValue := strings.Cut(arg, "="); !hasValue && goTestFlagsWithValue[name] && i+1 < len(args) {
				i++
			}
			continue
		}
		packages = append(packages, arg)
	}
	return packages
}

func repositoryPackageExists(root, pkg string) bool {
	if !strings.HasPrefix(pkg, ".") {
		if modulePath := repositoryModulePath(root); modulePath != "" && strings.HasPrefix(pkg, modulePath+"/") {
			pkg = "./" + strings.TrimPrefix(pkg, modulePath+"/")
		} else {
			return true // External module package paths are resolved by go test.
		}
	}
	if strings.Contains(pkg, "...") {
		prefix, _, _ := strings.Cut(pkg, "...")
		prefix = strings.TrimSuffix(prefix, "/")
		if prefix == "" {
			prefix = "."
		}
		base := filepath.Clean(filepath.FromSlash(prefix))
		if !filepath.IsAbs(base) {
			base = filepath.Join(root, base)
		}
		info, err := os.Stat(base)
		return err == nil && info.IsDir() && containsGoSource(base)
	}
	clean := filepath.Clean(filepath.FromSlash(pkg))
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(root, clean)
	}
	info, err := os.Stat(clean)
	return err == nil && info.IsDir() && hasGoSource(clean)
}

func repositoryModulePath(root string) string {
	content, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	return ""
}

func containsGoSource(root string) bool {
	found := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || found {
			return err
		}
		if info.IsDir() && path != root && (info.Name() == "vendor" || info.Name() == "node_modules" || strings.HasPrefix(info.Name(), ".")) {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			found = true
		}
		return nil
	})
	return found
}

func hasGoSource(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			return true
		}
	}
	return false
}

func npmScriptExists(root, name string) bool {
	content, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return false
	}
	return strings.TrimSpace(manifest.Scripts[name]) != ""
}

func repositoryFileExists(root, path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(root, clean)
	}
	info, err := os.Stat(clean)
	return err == nil && !info.IsDir()
}

func isFilePathArgument(arg string) bool {
	return strings.ContainsAny(arg, "/\\") || strings.HasSuffix(arg, ".test.ts") || strings.HasSuffix(arg, ".spec.ts")
}
