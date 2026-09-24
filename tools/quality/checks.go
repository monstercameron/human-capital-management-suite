package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ignoredDirNames mirrors scripts/check-go-style.mjs's own ignore list,
// extended with "testdata" (Go's own package-discovery convention) so the
// quality gate never permanently flags a deliberately-bad fixture package
// that go build/vet/staticcheck already skip via "./...".
var ignoredDirNames = map[string]bool{
	".git":         true,
	".artifacts":   true,
	"node_modules": true,
	"dist":         true,
	"tmp":          true,
	"vendor":       true,
	"testdata":     true,
	"src":          true, // legacy module human-capital-management-suite-executor: a separate go.mod
}

// findGoFiles walks root, excluding ignoredDirNames, and returns every .go
// file it finds (relative to root, forward-slash separated).
func findGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && ignoredDirNames[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// gofmtViolations runs `gofmt -l` over every .go file findGoFiles reports
// under root and returns the ones with formatting drift (paths relative to
// root).
func gofmtViolations(root string) ([]string, error) {
	files, err := findGoFiles(root)
	if err != nil {
		return nil, fmt.Errorf("finding Go files: %w", err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Windows caps a command line at 32 KB and the tree holds more Go files
	// than fit in one argv, so gofmt runs over bounded chunks.
	const chunkSize = 200
	var stdout bytes.Buffer
	for start := 0; start < len(files); start += chunkSize {
		end := min(start+chunkSize, len(files))
		args := append([]string{"-l"}, files[start:end]...)
		cmd := exec.Command("gofmt", args...)
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("gofmt -l failed: %w\n%s", err, stderr.String())
		}
	}

	var violations []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			violations = append(violations, filepath.ToSlash(line))
		}
	}
	return violations, nil
}

// runGoVet runs `go vet <pattern>` from root and returns its combined
// output plus whether it succeeded.
func runGoVet(root, pattern string) (string, bool) {
	cmd := exec.Command("go", "vet", pattern)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), false
	}
	// Go vet's default analyzer set omits unreachable, so opt it in explicitly
	// to make the quality contract catch dead statements after return/panic.
	cmd = exec.Command("go", "vet", "-unreachable", pattern)
	cmd.Dir = root
	unreachableOut, unreachableErr := cmd.CombinedOutput()
	return string(out) + string(unreachableOut), unreachableErr == nil
}

// runStaticcheck runs `go tool staticcheck <pattern>` from root and returns
// its combined output plus whether it succeeded.
func runStaticcheck(root, pattern string) (string, bool) {
	cmd := exec.Command("go", "tool", "staticcheck", pattern)
	cmd.Dir = root
	if os.Getenv("STATICCHECK_CACHE") == "" {
		cache := filepath.Join(root, ".artifacts", "staticcheck-cache")
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return fmt.Sprintf("creating staticcheck cache: %v\n", err), false
		}
		cmd.Env = append(os.Environ(), "STATICCHECK_CACHE="+cache)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// staticcheckSuppressionViolations finds staticcheck suppression directives
// without an accountable owner, a useful reason, and a future expiry. Keeping
// these records beside the suppression makes each exception reviewable and
// naturally time-bounded.
func staticcheckSuppressionViolations(root string, today time.Time) ([]string, error) {
	files, err := findGoFiles(root)
	if err != nil {
		return nil, fmt.Errorf("finding Go files: %w", err)
	}
	var violations []string
	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("reading %s: %w", rel, readErr)
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "//") {
				continue
			}
			fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
			if len(fields) == 0 || (fields[0] != "lint:ignore" && fields[0] != "lint:file-ignore") {
				continue
			}
			if problem := validateStaticcheckSuppression(trimmed, today); problem != "" {
				violations = append(violations, fmt.Sprintf("%s:%d: %s", rel, lineNo+1, problem))
			}
		}
	}
	return violations, nil
}

func validateStaticcheckSuppression(line string, today time.Time) string {
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "//")))
	if len(fields) < 2 || (fields[0] != "lint:ignore" && fields[0] != "lint:file-ignore") {
		return "malformed staticcheck suppression"
	}
	// Staticcheck requires at least one check ID. The policy metadata must be
	// explicit tokens so a vague sentence cannot accidentally satisfy it.
	metadata := map[string]string{}
	var reason []string
	for _, field := range fields[2:] {
		if key, value, ok := strings.Cut(field, "="); ok && (key == "owner" || key == "expires") {
			if _, exists := metadata[key]; exists {
				return fmt.Sprintf("duplicate %s metadata in staticcheck suppression", key)
			}
			metadata[key] = value
			continue
		}
		reason = append(reason, field)
	}
	if strings.TrimSpace(strings.Join(reason, " ")) == "" {
		return "staticcheck suppression requires a reason"
	}
	owner := metadata["owner"]
	if !validSuppressionOwner(owner) {
		return "staticcheck suppression requires owner=<accountable-owner>"
	}
	expires := metadata["expires"]
	date, err := time.Parse("2006-01-02", expires)
	if err != nil || date.Format("2006-01-02") != expires {
		return "staticcheck suppression requires expires=YYYY-MM-DD"
	}
	if date.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
		return fmt.Sprintf("staticcheck suppression expired on %s", expires)
	}
	return ""
}

func validSuppressionOwner(owner string) bool {
	if owner == "" || strings.EqualFold(owner, "none") || strings.EqualFold(owner, "unknown") || strings.EqualFold(owner, "tbd") {
		return false
	}
	for i, r := range owner {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9' && i > 0) || (i > 0 && strings.ContainsRune("._/-", r)) {
			continue
		}
		return false
	}
	return true
}

// runFrontendExperienceGate executes the registry-driven production matrix
// rather than a hand-maintained page list. A newly registered page or locale
// therefore joins both the localization and accessibility release gate.
func runFrontendExperienceGate(root string) (string, bool) {
	cmd := frontendExperienceGateCommand(root)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func frontendExperienceGateCommand(root string) *exec.Cmd {
	cmd := exec.Command(
		"go", "test", "-count=1",
		"-run", "^TestFrontendI18nAccessibilityGateEveryRegisteredPage$",
		"./internal/humanwork/productui",
	)
	cmd.Dir = root
	return cmd
}
