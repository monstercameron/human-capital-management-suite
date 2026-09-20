package docfix

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/librarystrategy"
)

// TestTodo_DOCFIX_001 is the PRIMARY test: the README License section and
// the LICENSE file must agree that the repository is MIT licensed.
func TestTodo_DOCFIX_001(t *testing.T) {
	root := repoRoot(t)
	readme := readText(t, root, "README.md")
	license := readText(t, root, "LICENSE")
	if !strings.Contains(license, "MIT License") {
		t.Errorf("LICENSE does not grant MIT")
	}
	sec := section(readme, "License")
	if !strings.Contains(sec, "MIT") || !strings.Contains(sec, "[LICENSE](LICENSE)") {
		t.Errorf("README License section does not name MIT and link LICENSE: %q", sec)
	}
	if strings.Contains(sec, "unlicensed for external use") {
		t.Errorf("README License section still claims the repo is unlicensed")
	}
}

// TestTodo_DOCFIX_001_Golden pins the License section bytes.
func TestTodo_DOCFIX_001_Golden(t *testing.T) {
	root := repoRoot(t)
	const want = `## License

This repository is MIT licensed. See [LICENSE](LICENSE) (Copyright (c) 2026
Earl Cameron).`
	if got := strings.TrimSpace(section(readText(t, root, "README.md"), "License")); got != want {
		t.Errorf("License section drifted:\n got: %q\nwant: %q", got, want)
	}
}

// TestTodo_DOCFIX_002 is the PRIMARY test: the README must describe the
// pinned buf generate path, never an unpinned toolchain.
func TestTodo_DOCFIX_002(t *testing.T) {
	root := repoRoot(t)
	readme := readText(t, root, "README.md")
	for _, stale := range []string{"not yet pinned", "no pinned root"} {
		if strings.Contains(readme, stale) {
			t.Errorf("README still claims generation is unpinned: %q", stale)
		}
	}
	if !strings.Contains(readme, "run `buf generate`") {
		t.Errorf("README does not document the pinned buf generate path")
	}
	if buf := readText(t, root, "buf.gen.yaml"); !strings.Contains(buf, "protoc-gen-go") {
		t.Errorf("buf.gen.yaml does not pin protoc-gen-go")
	}
}

// TestTodo_DOCFIX_002_Golden pins the Generating-contracts paragraph bytes.
func TestTodo_DOCFIX_002_Golden(t *testing.T) {
	root := repoRoot(t)
	const want = `Generating contracts is pinned: run ` + "`buf generate`" + ` from the repository root.
The ` + "`protoc-gen-go`" + ` and ` + "`protoc-gen-go-grpc`" + ` plugins resolve through ` + "`go.mod`" + `
tool directives, output lands in ` + "`gen/go`" + `, and generated files are checked in.
Do not check in handwritten Go duplicates of Protobuf messages. SchemaFlux YAML
(` + "`schema/schemaflux`" + `) is compiled by ` + "`tools/gen/schemaflux`" + `, not by ` + "`buf\ngenerate`."
	if got := readmeParagraph(readText(t, root, "README.md"), "Generating contracts is pinned:"); strings.TrimSpace(got) != want {
		t.Errorf("Generating-contracts paragraph drifted:\n got: %q\nwant: %q", got, want)
	}
}

// readmeParagraph returns the paragraph (blank-line delimited) containing needle.
func readmeParagraph(md, needle string) string {
	for _, p := range strings.Split(md, "\n\n") {
		if strings.Contains(p, needle) {
			return p
		}
	}
	return ""
}

// TestTodo_DOCFIX_003 is the PRIMARY test: the business-intent README must
// name tools/gen/schemaflux and only symbols the package exports.
func TestTodo_DOCFIX_003(t *testing.T) {
	root := repoRoot(t)
	doc := readText(t, root, "schema", "schemaflux", "business_intents", "v1", "README.md")
	if strings.Contains(doc, "do not yet exist") {
		t.Errorf("README still denies tooling that exists")
	}
	for _, want := range []string{"tools/gen/schemaflux", "LoadDefinitions", "CrossCheckCompiled"} {
		if !strings.Contains(doc, want) {
			t.Errorf("README does not name %q", want)
		}
	}
	sfx := filepath.Join(root, "tools", "gen", "schemaflux")
	checks := map[string]string{
		"loader.go":     "func LoadDefinitions",
		"crosscheck.go": "func CrossCheckCompiled",
		"generate.go":   "func (c *Catalog) RegistrySource",
	}
	for file, symbol := range checks {
		data, err := os.ReadFile(filepath.Join(sfx, file))
		if err != nil {
			t.Fatalf("docfix: read schemaflux %s: %v", file, err)
		}
		if !strings.Contains(string(data), symbol) {
			t.Errorf("tools/gen/schemaflux/%s does not export %q", file, symbol)
		}
	}
}

// TestTodo_DOCFIX_003_Golden pins the tooling paragraph bytes.
func TestTodo_DOCFIX_003_Golden(t *testing.T) {
	root := repoRoot(t)
	const want = "This directory is the SchemaFlux source for governed semantic intent metadata.\n" +
		"Protobuf remains authoritative for wire request/result types. These YAML files\n" +
		"are intended to relate those types to capabilities, governance, evidence,\n" +
		"reliability, and execution semantics. The deterministic HCM SchemaFlux tooling\n" +
		"lives in `tools/gen/schemaflux`: `LoadDefinitions` loads these files, the\n" +
		"generator emits the Go registry (`RegistrySource`), catalog Markdown\n" +
		"(`CatalogMarkdown`) and fixtures JSON (`FixturesJSON`), and\n" +
		"`CrossCheckCompiled` checks the compiled output against\n" +
		"`internal/intent/definitions`."
	doc := readText(t, root, "schema", "schemaflux", "business_intents", "v1", "README.md")
	if got := readmeParagraph(doc, "deterministic HCM SchemaFlux tooling"); strings.TrimSpace(got) != want {
		t.Errorf("tooling paragraph drifted:\n got: %q\nwant: %q", got, want)
	}
}

// TestTodo_DOCFIX_004 is the PRIMARY test: the delivery loop pushes only topic
// branches while the settings deny list agrees by denying main, master, force
// and delete pushes while allowing topic-branch pushes.
func TestTodo_DOCFIX_004(t *testing.T) {
	root := repoRoot(t)
	deny := settingsDeny(t, root)
	for _, entry := range []string{
		"Bash(git push origin main:*)",
		"Bash(git push origin master:*)",
		"Bash(git push --force:*)",
		"Bash(git push -f:*)",
		"Bash(git push --delete:*)",
	} {
		found := false
		for _, got := range deny {
			if got == entry {
				found = true
			}
		}
		if !found {
			t.Errorf("settings deny list lacks %q", entry)
		}
	}
	for _, got := range deny {
		if got == "Bash(git push:*)" {
			t.Errorf("settings deny list has a blanket push deny that forbids the documented topic-branch push")
		}
	}
	agents := readText(t, root, "AGENTS.md")
	loop := ""
	for _, line := range strings.Split(agents, "\n") {
		if strings.HasPrefix(line, "11. **Pull request.**") {
			loop += line
		}
		if strings.HasPrefix(line, "    files outside the change.") {
			loop += "\n" + line
		}
	}
	if loop == "" {
		t.Fatalf("AGENTS.md delivery loop has no Pull request step")
	}
	for _, want := range []string{"Push the topic branch", "topic branches only"} {
		if !strings.Contains(loop, want) {
			t.Errorf("delivery loop Pull request step does not state %q: %q", want, loop)
		}
	}
	if strings.Contains(loop, "Stop at the committed topic branch") {
		t.Errorf("delivery loop still tells the agent to stop instead of pushing the topic branch: %q", loop)
	}
	if got := strings.Count(agents, "git push origin"); got != 2 {
		t.Errorf("push command is stated %d times, want exactly twice (loop step and Git discipline)", got)
	}
	discipline := false
	for _, line := range strings.Split(agents, "\n") {
		if strings.HasPrefix(line, "- Push only topic branches") {
			discipline = true
		}
	}
	if !discipline {
		t.Errorf("Git discipline does not state the push rule once")
	}
}

// TestTodo_DOCFIX_004_Golden pins the Git-discipline push line and the
// delivery-loop Pull request step bytes.
func TestTodo_DOCFIX_004_Golden(t *testing.T) {
	root := repoRoot(t)
	const disciplineWant = "- Push only topic branches with an explicit `git push origin <branch>`, only to open or update a PR — never main or master, never force-push or delete; the agent settings deny those pushes. Merge into main only through a PR whose CI is green."
	const loopWant = "11. **Pull request.** Push the topic branch (`git push origin <branch>`, topic branches only — never main or master, never force-push or delete; see Git discipline) and open a PR naming the todos closed and their evidence. A PR never contains scratch directories, credentials or\n    files outside the change."
	agents := readText(t, root, "AGENTS.md")
	foundDiscipline := false
	for _, line := range strings.Split(agents, "\n") {
		if strings.HasPrefix(line, "- Push only topic branches") {
			foundDiscipline = true
			if strings.TrimSpace(line) != disciplineWant {
				t.Errorf("push rule drifted:\n got: %q\nwant: %q", line, disciplineWant)
			}
		}
	}
	if !foundDiscipline {
		t.Errorf("AGENTS.md has no topic-branch-only push rule")
	}
	lines := strings.Split(agents, "\n")
	foundLoop := false
	for i, line := range lines {
		if strings.HasPrefix(line, "11. **Pull request.**") && i+1 < len(lines) {
			foundLoop = true
			if got := line + "\n" + lines[i+1]; got != loopWant {
				t.Errorf("Pull request step drifted:\n got: %q\nwant: %q", got, loopWant)
			}
		}
	}
	if !foundLoop {
		t.Errorf("AGENTS.md delivery loop has no Pull request step")
	}
}

// TestTodo_DOCFIX_005 is the PRIMARY test: the layout manifest must describe
// humanwork as the production UI, the README row must match, and the
// manifest/ README match check must stay green.
func TestTodo_DOCFIX_005(t *testing.T) {
	root := repoRoot(t)
	manifest := readText(t, root, "definitions", "architecture", "repository-layout.yaml")
	block := manifestBlock(manifest, "humanwork")
	if block == "" {
		t.Fatalf("manifest has no humanwork row")
	}
	for _, want := range []string{"Production Go/WASM", "phase: P1A"} {
		if !strings.Contains(block, want) {
			t.Errorf("humanwork row lacks %q", want)
		}
	}
	if strings.Contains(block, "deferred beyond P1A scope") {
		t.Errorf("humanwork row still declares deferred messaging")
	}
	readme := readText(t, root, "README.md")
	if !strings.Contains(readme, "internal/humanwork [transport; P1A; owner=experience-and-transport]") {
		t.Errorf("README library row does not match the manifest humanwork row")
	}
	if err := librarystrategy.Check(root); err != nil {
		t.Errorf("librarystrategy match check is red: %v", err)
	}
}

// TestTodo_DOCFIX_005_Golden pins the manifest row and README row bytes.
func TestTodo_DOCFIX_005_Golden(t *testing.T) {
	root := repoRoot(t)
	const manifestWant = `- name: humanwork
    owner: experience-and-transport
    layer: transport
    phase: P1A
    description: Production Go/WASM user interface (workspace shell, productui pages and components, uicomponents); the served surface in the README "Review the production frontend" section.`
	manifest := readText(t, root, "definitions", "architecture", "repository-layout.yaml")
	if got := strings.TrimSpace(manifestBlock(manifest, "humanwork")); got != manifestWant {
		t.Errorf("humanwork row drifted:\n got: %q\nwant: %q", got, manifestWant)
	}
	const rowWant = `│   ├── internal/humanwork [transport; P1A; owner=experience-and-transport]`
	readme := readText(t, root, "README.md")
	found := false
	for _, line := range strings.Split(readme, "\n") {
		if strings.Contains(line, "internal/humanwork [") {
			found = true
			if strings.TrimSpace(line) != rowWant {
				t.Errorf("README row drifted:\n got: %q\nwant: %q", line, rowWant)
			}
		}
	}
	if !found {
		t.Errorf("README has no humanwork library row")
	}
}

// TestTodo_DOCFIX_006 is the PRIMARY test: every approved command must be
// listed in the README command list.
func TestTodo_DOCFIX_006(t *testing.T) {
	root := repoRoot(t)
	readme := readText(t, root, "README.md")
	if strings.Contains(readme, "four root commands") {
		t.Errorf("README still claims four root commands")
	}
	for _, cmd := range approvedCommands(t, root) {
		if !strings.Contains(readme, "`"+cmd+"`") {
			t.Errorf("README command list misses approved command %q", cmd)
		}
	}
	if !strings.Contains(readme, "go run ./cmd/scheduler") {
		t.Errorf("README does not show how to run scheduler")
	}
}

// TestTodo_DOCFIX_006_Golden pins the seven-commands paragraph bytes.
func TestTodo_DOCFIX_006_Golden(t *testing.T) {
	root := repoRoot(t)
	const want = "The seven root commands are `hcmnext` (the API cell), `migrate` (schema and\n" +
		"fixture seed), `projector` (projection reconciliation), `worker` (outbox\n" +
		"consumption), `scheduler` (workflow-frontier scheduling), `hcmctl` (operator\n" +
		"CLI), and `frontenddev` (local frontend development). The `hcm_next` database\n" +
		"must exist before `migrate up` (create it with any PostgreSQL client, e.g.\n" +
		"from a Go one-off using pgx, or an external server; the embedded PostgreSQL\n" +
		"binaries this repo's tests cache ship no `psql` or `createdb`). Start a fresh\n" +
		"database with:"
	readme := readText(t, root, "README.md")
	if got := readmeParagraph(readme, "seven root commands"); strings.TrimSpace(got) != want {
		t.Errorf("commands paragraph drifted:\n got: %q\nwant: %q", got, want)
	}
}

// TestTodo_DOCFIX_007 is the PRIMARY test: every test:all step must be named
// in the AGENTS.md gate description.
func TestTodo_DOCFIX_007(t *testing.T) {
	root := repoRoot(t)
	agents := readText(t, root, "AGENTS.md")
	for _, step := range testAllSteps(t, root) {
		token, ok := agentsTokenForStep(step)
		if !ok {
			t.Errorf("test:all step %q has no AGENTS.md token mapping", step)
			continue
		}
		if !strings.Contains(agents, token) {
			t.Errorf("AGENTS.md gate description omits test:all step %q (token %q)", step, token)
		}
	}
}

// TestTodo_DOCFIX_007_Golden pins the hook gate sentence bytes.
func TestTodo_DOCFIX_007_Golden(t *testing.T) {
	root := repoRoot(t)
	const want = "The hook also runs typecheck and the drift, API, substrate-coverage, engine-coverage, race-policy and decomposition gates, the nested-module tests and the build. Never bypass it: no `--no-verify`, ever. If the hook is red because of another session's half-written file, wait for that file to compile; do not edit it."
	agents := readText(t, root, "AGENTS.md")
	found := false
	for _, line := range strings.Split(agents, "\n") {
		if strings.HasPrefix(line, "The hook also runs") {
			found = true
			if strings.TrimSpace(line) != want {
				t.Errorf("hook sentence drifted:\n got: %q\nwant: %q", line, want)
			}
		}
	}
	if !found {
		t.Errorf("AGENTS.md has no hook gate sentence")
	}
}

// TestTodo_DOCFIX_008 is the PRIMARY test: the Format row must name
// check:go, and every npm-run command the gate table names must exist.
func TestTodo_DOCFIX_008(t *testing.T) {
	root := repoRoot(t)
	agents := readText(t, root, "AGENTS.md")
	formatRow := ""
	for _, line := range strings.Split(agents, "\n") {
		if strings.HasPrefix(line, "| Format") {
			formatRow = line
		}
	}
	if formatRow == "" {
		t.Fatalf("AGENTS.md gate table has no Format row")
	}
	if !strings.Contains(formatRow, "check:go") {
		t.Errorf("Format row does not name npm run check:go: %q", formatRow)
	}
	if strings.Contains(formatRow, "`, `gofmt -l`") || strings.Contains(formatRow, " `gofmt -l`") {
		t.Errorf("Format row still names bare gofmt -l as the Go gate: %q", formatRow)
	}
	scripts := packageScripts(t, root)
	for _, m := range npmRunPattern.FindAllStringSubmatch(formatRow, -1) {
		if _, ok := scripts[m[1]]; !ok {
			t.Errorf("Format row names npm script %q that package.json lacks", m[1])
		}
	}
}

var npmRunPattern = regexp.MustCompile(`npm run ([a-z0-9:.-]+)`)

// TestTodo_DOCFIX_008_Golden pins the Format row bytes.
func TestTodo_DOCFIX_008_Golden(t *testing.T) {
	root := repoRoot(t)
	agents := readText(t, root, "AGENTS.md")
	found := false
	for _, line := range strings.Split(agents, "\n") {
		if strings.HasPrefix(line, "| Format") {
			found = true
			if !strings.Contains(line, "`npm run check:go`") {
				t.Errorf("Format row drifted: %q", line)
			}
		}
	}
	if !found {
		t.Errorf("AGENTS.md gate table has no Format row")
	}
}

// TestTodo_DOCFIX_009 is the PRIMARY test: every file the productui
// Source-ownership section names must exist, and the section must disclaim
// exhaustiveness.
func TestTodo_DOCFIX_009(t *testing.T) {
	root := repoRoot(t)
	doc := readText(t, root, "internal", "humanwork", "productui", "README.md")
	sec := section(doc, "Source ownership")
	if sec == "" {
		t.Fatalf("productui README has no Source ownership section")
	}
	if !strings.Contains(sec, "not exhaustive") {
		t.Errorf("Source ownership does not disclaim exhaustiveness")
	}
	files := expandMigRange(backtickGoFiles(sec))
	if len(files) == 0 {
		t.Fatalf("Source ownership names no files")
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(root, "internal", "humanwork", "productui", f)); err != nil {
			t.Errorf("Source ownership names missing file %q", f)
		}
	}
}

// TestTodo_DOCFIX_009_Golden pins the Source-ownership section bytes.
func TestTodo_DOCFIX_009_Golden(t *testing.T) {
	root := repoRoot(t)
	doc := readText(t, root, "internal", "humanwork", "productui", "README.md")
	sec := section(doc, "Source ownership")
	for _, want := range []string{
		"Ownership is by file family, and this list is not exhaustive.",
		"over 130 `page_*.go` surfaces",
		"`typed_mig_A.go`–`typed_mig_F.go`",
		"Transport boundary: `provider.go`",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("Source ownership drifted; missing %q", want)
		}
	}
}

// TestTodo_DOCFIX_010 is the PRIMARY test: infra/README must not promise
// assets the directory lacks.
func TestTodo_DOCFIX_010(t *testing.T) {
	root := repoRoot(t)
	doc := readText(t, root, "infra", "README.md")
	for _, stale := range []string{"belong here", "V0 demo currently expects"} {
		if strings.Contains(doc, stale) {
			t.Errorf("infra README still promises %q", stale)
		}
	}
	if !strings.Contains(doc, "reserved and empty") {
		t.Errorf("infra README does not state the directory is reserved and empty")
	}
	entries, err := os.ReadDir(filepath.Join(root, "infra"))
	if err != nil {
		t.Fatalf("docfix: read infra dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("infra holds %d entries, want only the README", len(entries))
	}
}

// TestTodo_DOCFIX_010_Golden pins the infra README bytes.
func TestTodo_DOCFIX_010_Golden(t *testing.T) {
	root := repoRoot(t)
	const want = `# Infra

This directory is currently reserved and empty.

Run the prototype locally with the Go cell plus PostgreSQL as described in the
root README section "Run the prototype locally". There are no container,
compose, Dockerfile, or environment assets here, and no Node API or Go block
runner.
`
	if got := readText(t, root, "infra", "README.md"); got != want {
		t.Errorf("infra README drifted:\n got: %q\nwant: %q", got, want)
	}
}

// TestTodo_DOCFIX_011 is the PRIMARY test: the diagram labels must fit their
// branch columns and the code-level names must be stated once.
func TestTodo_DOCFIX_011(t *testing.T) {
	root := repoRoot(t)
	readme := readText(t, root, "README.md")
	if strings.Contains(readme, "Human Capital Management Suite state") {
		t.Errorf("diagram still uses the overrunning product label")
	}
	line := ""
	for _, l := range strings.Split(readme, "\n") {
		if strings.Contains(l, "HCM Suite state") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("diagram has no HCM Suite state label line")
	}
	if len(line) > 68 {
		t.Errorf("diagram label line overruns its columns (%d chars)", len(line))
	}
	if !strings.Contains(readme, "code-level names are `hcmnext`") {
		t.Errorf("README does not state the code-level product names once")
	}
}

// TestTodo_DOCFIX_011_Golden pins the diagram label line and naming bytes.
func TestTodo_DOCFIX_011_Golden(t *testing.T) {
	root := repoRoot(t)
	readme := readText(t, root, "README.md")
	const labelWant = `               HCM Suite state   External systems Human interaction`
	found := false
	for _, l := range strings.Split(readme, "\n") {
		if strings.Contains(l, "HCM Suite state") {
			found = true
			if l != labelWant {
				t.Errorf("diagram label drifted:\n got: %q\nwant: %q", l, labelWant)
			}
		}
	}
	if !found {
		t.Errorf("diagram label line missing")
	}
	const namingWant = "The product's code-level names are `hcmnext`, `HCMNEXT_*` environment\n" +
		"variables, and the `HCM_NEXT` database."
	if !strings.Contains(readme, namingWant) {
		t.Errorf("naming sentence drifted")
	}
}

// TestTodo_DOCFIX_012 is the PRIMARY test: the migrate/seed block appears
// exactly once, with the database prerequisite before it.
func TestTodo_DOCFIX_012(t *testing.T) {
	root := repoRoot(t)
	readme := readText(t, root, "README.md")
	if got := migrateUpCount(readme); got != 1 {
		t.Errorf("migrate-up block appears %d times, want exactly once", got)
	}
	prereq := strings.Index(readme, "must exist before `migrate up`")
	block := strings.Index(readme, "go run ./cmd/migrate up")
	if prereq < 0 || block < 0 {
		t.Fatalf("prerequisite or setup block missing")
	}
	if prereq > block {
		t.Errorf("database prerequisite comes after the setup commands")
	}
}

// TestDocfixHelpers exercises the pure helpers against inline fixtures so
// they can fail independently of the repository text.
func TestDocfixHelpers(t *testing.T) {
	if _, ok := agentsTokenForStep("nonexistent-step"); ok {
		t.Errorf("unknown step mapped to a token")
	}
	if token, ok := agentsTokenForStep("test:all"); ok || token != "" {
		t.Errorf("test:all itself mapped to %q", token)
	}
	if got := section("no headings here", "License"); got != "" {
		t.Errorf("section matched without heading: %q", got)
	}
	if got := section("## A\nbody\n## B\n", "A"); got != "## A\nbody" {
		t.Errorf("section extraction wrong: %q", got)
	}
	if got := fencedBlockContaining("no fences", "x"); got != "" {
		t.Errorf("fenced block matched without fences")
	}
	if got := manifestBlock("no rows", "humanwork"); got != "" {
		t.Errorf("manifest block matched without rows")
	}
	files := backtickGoFiles("`a.go` and `dir/b.go` plus `page_*.go`")
	if len(files) != 1 || files[0] != "a.go" {
		t.Errorf("backtick filtering wrong: %q", files)
	}
	single := expandMigRange([]string{"typed_mig_A.go", "other.go"})
	if len(single) != 2 {
		t.Errorf("single endpoint should pass through: %q", single)
	}
	expanded := expandMigRange([]string{"typed_mig_A.go", "typed_mig_F.go"})
	if len(expanded) != 6 {
		t.Errorf("endpoint pair should expand to six: %q", expanded)
	}
	if got := migrateUpCount("up\nup"); got != 0 {
		t.Errorf("migrate count matched bare word: %d", got)
	}
	if got := readmeParagraph("a\n\nb needle c\n\nd", "needle"); got != "b needle c" {
		t.Errorf("paragraph extraction wrong: %q", got)
	}
}

// TestTodo_DOCFIX_012_Golden pins the prerequisite and setup block bytes.
func TestTodo_DOCFIX_012_Golden(t *testing.T) {
	root := repoRoot(t)
	readme := readText(t, root, "README.md")
	const prereqWant = "The `hcm_next` database\n" +
		"must exist before `migrate up`"
	const blockWant = "```powershell\ngo run ./cmd/migrate up\ngo run ./cmd/migrate seed -tenant=harborcare-demo\n```"
	if !strings.Contains(readme, prereqWant) {
		t.Errorf("prerequisite bytes drifted")
	}
	if !strings.Contains(readme, blockWant) {
		t.Errorf("setup block bytes drifted")
	}
}
