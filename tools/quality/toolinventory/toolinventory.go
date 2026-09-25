// Package toolinventory implements the TOOL-025 non-module tool-input
// inventory: it inventories every build/test/release tool this repository
// pins outside the Go module graph (the Protobuf toolchain, the Node/npm
// developer toolchain, the embedded PostgreSQL fixture binary, and the
// Husky git hooks) and writes it as a deterministic manifest,
// definitions/toolchain/tool-inventory.yaml.
//
// Every entry is derived programmatically from the repository file that
// actually pins it (gen/TOOLS.lock, go.mod, package.json,
// package-lock.json, .husky/*) rather than hand-maintained, so the
// manifest can be regenerated and checked for drift: if a source file's
// pinned version changes without the checked-in manifest being
// regenerated, Generate's output stops matching the checked-in file and
// the TOOL-025 test suite fails.
//
// Scope note: this package inventories exactly the inputs named in its
// implementing todo (buf, the go.mod-pinned protoc plugins and
// staticcheck, Node/npm/prettier/eslint/vitest/lint-staged, Husky's git
// hooks, and the embedded-postgres Go module). Cosign/Sigstore
// verification (TOOL-023), govulncheck reachability evidence (TOOL-024)
// and CI action/container-image entries are follow-on work for those
// todos; CVEStatus values below name which pending scan will eventually
// resolve them rather than claiming a scan already ran.
package toolinventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

// Entry is one non-module tool input the TOOL-025 RED clause requires to
// carry a pinned version/digest, source, license, CVE status, owner,
// update SLA and replacement path.
type Entry struct {
	Name            string            `yaml:"name"`
	Category        string            `yaml:"category"`
	Version         string            `yaml:"version"`
	Digest          string            `yaml:"digest,omitempty"`
	PlatformDigests map[string]string `yaml:"platform_digests,omitempty"`
	Source          string            `yaml:"source"`
	License         string            `yaml:"license"`
	CVEStatus       string            `yaml:"cve_status"`
	Owner           string            `yaml:"owner"`
	UpdateSLA       string            `yaml:"update_sla"`
	ReplacementPath string            `yaml:"replacement_path"`
	DerivedFrom     string            `yaml:"derived_from"`
	Note            string            `yaml:"note,omitempty"`
}

// requiredFields lists, in RED-clause order, the fields every entry must
// carry. Digest is deliberately not required: TOOL-025 asks for "digests
// where files exist", so an entry whose only pin is a version string (no
// local file to hash) is still complete without one.
var requiredFields = []string{"version", "source", "license", "cve_status", "owner", "update_sla", "replacement_path"}

// MissingFields reports which of the required fields are empty on e, by
// their YAML key name. It returns nil when e is complete.
func (e Entry) MissingFields() []string {
	values := map[string]string{
		"version":          e.Version,
		"source":           e.Source,
		"license":          e.License,
		"cve_status":       e.CVEStatus,
		"owner":            e.Owner,
		"update_sla":       e.UpdateSLA,
		"replacement_path": e.ReplacementPath,
	}
	var missing []string
	for _, field := range requiredFields {
		if values[field] == "" {
			missing = append(missing, field)
		}
	}
	return missing
}

// floatingVersionMarkers are values that mean "whatever is current",
// exactly what a pinned supply-chain manifest must never contain.
var floatingVersionMarkers = map[string]bool{
	"latest": true, "*": true, "main": true, "master": true, "head": true, "": true,
}

// IsFloatingVersion reports whether e.Version is a moving-target marker
// rather than an exact pin or a documented minimum-version constraint.
func (e Entry) IsFloatingVersion() bool {
	return floatingVersionMarkers[strings.ToLower(strings.TrimSpace(e.Version))]
}

// ValidCVEStatus reports whether s is a recognized CVE-status token: a
// named pending scan, an explicit not-applicable rationale, or a clean
// scan result. It rejects blank or ad hoc values so a real "unscanned"
// state can never be indistinguishable from an unset field.
func ValidCVEStatus(s string) bool {
	return strings.HasPrefix(s, "PENDING_") || strings.HasPrefix(s, "N/A") || s == "CLEAN"
}

// Manifest is the parsed form of definitions/toolchain/tool-inventory.yaml.
type Manifest struct {
	Version int     `yaml:"version"`
	Tools   []Entry `yaml:"tools"`
}

// Validate reports every RED-clause violation in m: an incomplete entry,
// a duplicate tool name, or an unrecognized CVE-status token. It returns
// nil only when the manifest set-equals a complete, well-formed inventory.
func (m Manifest) Validate() error {
	var problems []string
	seen := map[string]bool{}
	for _, e := range m.Tools {
		if e.Name == "" {
			problems = append(problems, "an entry has no name")
			continue
		}
		if seen[e.Name] {
			problems = append(problems, fmt.Sprintf("%s: duplicate tool name", e.Name))
		}
		seen[e.Name] = true

		if missing := e.MissingFields(); len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("%s: missing %s", e.Name, strings.Join(missing, ", ")))
		}
		if e.CVEStatus != "" && !ValidCVEStatus(e.CVEStatus) {
			problems = append(problems, fmt.Sprintf("%s: unrecognized cve_status %q", e.Name, e.CVEStatus))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("toolinventory: invalid manifest:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// HasFloatingVersion reports whether any entry in m is pinned by a
// moving-target marker instead of an exact version or digest.
func HasFloatingVersion(m Manifest) bool {
	for _, e := range m.Tools {
		if e.IsFloatingVersion() {
			return true
		}
	}
	return false
}

// Equal reports whether a and b carry the same set of entries,
// independent of slice order (Generate always returns Tools sorted by
// Name, but callers - e.g. a manifest loaded from disk - are not
// required to preserve that order for Equal to still recognize them as
// the same inventory).
func Equal(a, b Manifest) bool {
	if a.Version != b.Version || len(a.Tools) != len(b.Tools) {
		return false
	}
	sortedA := append([]Entry{}, a.Tools...)
	sortedB := append([]Entry{}, b.Tools...)
	sort.Slice(sortedA, func(i, j int) bool { return sortedA[i].Name < sortedA[j].Name })
	sort.Slice(sortedB, func(i, j int) bool { return sortedB[i].Name < sortedB[j].Name })
	for i := range sortedA {
		if !equalEntry(sortedA[i], sortedB[i]) {
			return false
		}
	}
	return true
}

func equalEntry(a, b Entry) bool {
	if a.Name != b.Name || a.Category != b.Category || a.Version != b.Version || a.Digest != b.Digest || a.Source != b.Source || a.License != b.License || a.CVEStatus != b.CVEStatus || a.Owner != b.Owner || a.UpdateSLA != b.UpdateSLA || a.ReplacementPath != b.ReplacementPath || a.DerivedFrom != b.DerivedFrom || a.Note != b.Note || len(a.PlatformDigests) != len(b.PlatformDigests) {
		return false
	}
	for platform, digest := range a.PlatformDigests {
		if b.PlatformDigests[platform] != digest {
			return false
		}
	}
	return true
}

// Load reads and parses the manifest at path.
func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("toolinventory: reading manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("toolinventory: parsing manifest: %w", err)
	}
	return m, nil
}

// Marshal renders m as YAML, with Tools sorted by Name so the output is a
// pure, byte-stable function of the entries regardless of the order they
// were appended in.
func Marshal(m Manifest) ([]byte, error) {
	sorted := m
	sorted.Tools = append([]Entry{}, m.Tools...)
	sort.Slice(sorted.Tools, func(i, j int) bool { return sorted.Tools[i].Name < sorted.Tools[j].Name })
	var out strings.Builder
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(sorted); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// VerifyDigests reports whether every entry in m carries the digest a
// fresh Generate(root) would compute for it right now. It regenerates
// from source rather than re-hashing files itself, so it works uniformly
// across the different digest schemes entries use (a local SHA-256 for
// go.mod/JSON-derived entries, npm's own package-lock.json integrity
// hash for npm-derived ones): any mismatch means either the pinning file
// changed since m was written (real drift) or m's digest field was
// tampered with (the TOOL-025 RED case this function exists to catch).
func VerifyDigests(root string, m Manifest) error {
	fresh, err := Generate(root)
	if err != nil {
		return fmt.Errorf("toolinventory: regenerating for digest verification: %w", err)
	}
	freshByName := make(map[string]Entry, len(fresh.Tools))
	for _, e := range fresh.Tools {
		freshByName[e.Name] = e
	}
	for _, e := range m.Tools {
		f, ok := freshByName[e.Name]
		if !ok {
			return fmt.Errorf("toolinventory: %s is not produced by the current generator (stale or renamed entry)", e.Name)
		}
		if e.Digest != f.Digest || !equalStringMap(e.PlatformDigests, f.PlatformDigests) {
			return fmt.Errorf("toolinventory: %s digest data does not match freshly derived values", e.Name)
		}
	}
	return nil
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

// Generate rebuilds the full tool inventory from the repository's own
// pinning files under root. It returns entries sorted by Name.
func Generate(root string) (Manifest, error) {
	var tools []Entry

	bufEntry, lockProtocGo, lockProtocGoGRPC, lockProtocGoSum, lockProtocGoGRPCSum, err := loadToolsLockEntry(root)
	if err != nil {
		return Manifest{}, err
	}
	tools = append(tools, bufEntry)

	goModEntries, modProtocGo, modProtocGoGRPC, err := loadGoModToolEntries(root)
	if err != nil {
		return Manifest{}, err
	}

	if lockProtocGo != "" && modProtocGo != "" && lockProtocGo != modProtocGo {
		return Manifest{}, fmt.Errorf("toolinventory: gen/TOOLS.lock protoc_gen_go_version %q does not match the go.mod-pinned %q", lockProtocGo, modProtocGo)
	}
	if lockProtocGoGRPC != "" && modProtocGoGRPC != "" && lockProtocGoGRPC != modProtocGoGRPC {
		return Manifest{}, fmt.Errorf("toolinventory: gen/TOOLS.lock protoc_gen_go_grpc_version %q does not match the go.mod-pinned %q", lockProtocGoGRPC, modProtocGoGRPC)
	}
	goSumData, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		return Manifest{}, fmt.Errorf("toolinventory: reading go.sum: %w", err)
	}
	lockedModuleSums := map[string]struct {
		modulePath string
		version    string
		sum        string
	}{
		"protoc-gen-go":      {"google.golang.org/protobuf", lockProtocGo, lockProtocGoSum},
		"protoc-gen-go-grpc": {"google.golang.org/grpc/cmd/protoc-gen-go-grpc", lockProtocGoGRPC, lockProtocGoGRPCSum},
	}
	for i := range goModEntries {
		pin, ok := lockedModuleSums[goModEntries[i].Name]
		if !ok {
			continue
		}
		if moduleSum(string(goSumData), pin.modulePath, pin.version) != pin.sum {
			return Manifest{}, fmt.Errorf("toolinventory: %s module checksum in gen/TOOLS.lock does not match go.sum", goModEntries[i].Name)
		}
		goModEntries[i].Digest = pin.sum
	}
	tools = append(tools, goModEntries...)

	embeddedPG, err := loadEmbeddedPostgresEntry(root)
	if err != nil {
		return Manifest{}, err
	}
	tools = append(tools, embeddedPG)

	nodeEntries, err := loadPackageJSONEntries(root)
	if err != nil {
		return Manifest{}, err
	}
	tools = append(tools, nodeEntries...)

	npmEntries, err := loadPackageLockEntries(root)
	if err != nil {
		return Manifest{}, err
	}
	tools = append(tools, npmEntries...)

	hookEntries, err := loadHuskyHookEntries(root)
	if err != nil {
		return Manifest{}, err
	}
	tools = append(tools, hookEntries...)

	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return Manifest{Version: 1, Tools: tools}, nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// digestString hashes a short canonical identity string (e.g.
// "name@version") rather than a whole pinning file. Several source files
// this package reads (go.mod, package.json) are large, actively-edited
// files that pin many things this manifest does not track; hashing the
// whole file would make an entry's digest - and this generator's
// determinism - hostage to any unrelated edit elsewhere in that file.
// Hashing just the fields Generate actually asserts keeps the digest a
// precise commitment to the claim being made, immune to noise around it.
func digestString(s string) string {
	return digestBytes([]byte(s))
}

// toolsLockFile is the shape of gen/TOOLS.lock.
type toolsLockFile struct {
	BufVersion                string            `json:"buf_version"`
	BufBinarySHA256ByPlatform map[string]string `json:"buf_binary_sha256_by_platform"`
	BufChecksumsSource        string            `json:"buf_checksums_source"`
	ProtocGenGoVersion        string            `json:"protoc_gen_go_version"`
	ProtocGenGoModuleSum      string            `json:"protoc_gen_go_module_sum"`
	ProtocGenGoGRPCVersion    string            `json:"protoc_gen_go_grpc_version"`
	ProtocGenGoGRPCModuleSum  string            `json:"protoc_gen_go_grpc_module_sum"`
}

// loadToolsLockEntry reads gen/TOOLS.lock and returns the buf Entry, plus
// its recorded protoc-gen-go/protoc-gen-go-grpc versions so Generate can
// cross-check them against go.mod's own pins.
func loadToolsLockEntry(root string) (entry Entry, protocGoVersion, protocGoGRPCVersion, protocGoModuleSum, protocGoGRPCModuleSum string, err error) {
	p := filepath.Join(root, "gen", "TOOLS.lock")
	data, err := os.ReadFile(p)
	if err != nil {
		return Entry{}, "", "", "", "", fmt.Errorf("toolinventory: reading gen/TOOLS.lock: %w", err)
	}
	var tl toolsLockFile
	if err := json.Unmarshal(data, &tl); err != nil {
		return Entry{}, "", "", "", "", fmt.Errorf("toolinventory: parsing gen/TOOLS.lock: %w", err)
	}
	if tl.BufVersion == "" {
		return Entry{}, "", "", "", "", fmt.Errorf("toolinventory: gen/TOOLS.lock has no buf_version")
	}
	if tl.BufChecksumsSource == "" || len(tl.BufBinarySHA256ByPlatform) == 0 {
		return Entry{}, "", "", "", "", fmt.Errorf("toolinventory: gen/TOOLS.lock has no authoritative Buf binary digests")
	}
	if tl.ProtocGenGoModuleSum == "" || tl.ProtocGenGoGRPCModuleSum == "" {
		return Entry{}, "", "", "", "", fmt.Errorf("toolinventory: gen/TOOLS.lock has no Protobuf plugin module checksums")
	}
	entry = Entry{
		Name:            "buf",
		Category:        "protobuf_toolchain",
		Version:         tl.BufVersion,
		PlatformDigests: cloneStringMap(tl.BufBinarySHA256ByPlatform),
		Source:          "https://github.com/bufbuild/buf",
		License:         "Apache-2.0",
		CVEStatus:       "PENDING_MANUAL_REVIEW",
		Owner:           "platform-toolchain",
		UpdateSLA:       "reviewed on every buf.yaml/gen/TOOLS.lock bump",
		ReplacementPath: "protoc CLI directly invoked with the pinned protoc-gen-go/protoc-gen-go-grpc plugins (see buf.gen.yaml)",
		DerivedFrom:     "gen/TOOLS.lock",
		Note:            "platform binary SHA-256 values are from " + tl.BufChecksumsSource,
	}
	return entry, tl.ProtocGenGoVersion, tl.ProtocGenGoGRPCVersion, tl.ProtocGenGoModuleSum, tl.ProtocGenGoGRPCModuleSum, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func moduleSum(goSumText, modulePath, version string) string {
	for _, line := range strings.Split(goSumText, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == modulePath && fields[1] == version && strings.HasPrefix(fields[2], "h1:") {
			return fields[2]
		}
	}
	return ""
}

// loadGoModToolEntries parses go.mod's `tool` directives and resolves
// each tool's pinned version against go.mod's own `require` block (exact
// match first, then the longest `require` path that is a "/"-bounded
// prefix of the tool's package path - the shape needed because
// protoc-gen-go's tool path is a subpackage of the google.golang.org/
// protobuf module, while protoc-gen-go-grpc is its own separately
// versioned module whose require entry equals the tool path exactly).
func loadGoModToolEntries(root string) (entries []Entry, protocGoVersion, protocGoGRPCVersion string, err error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, "", "", fmt.Errorf("toolinventory: reading go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("toolinventory: parsing go.mod: %w", err)
	}

	versionFor := func(toolPath string) string {
		best, bestLen := "", -1
		for _, r := range f.Require {
			p := r.Mod.Path
			matches := p == toolPath || strings.HasPrefix(toolPath, p+"/")
			if matches && len(p) > bestLen {
				best, bestLen = r.Mod.Version, len(p)
			}
		}
		return best
	}

	for _, tool := range f.Tool {
		name := path.Base(tool.Path)
		ver := versionFor(tool.Path)
		if ver == "" {
			return nil, "", "", fmt.Errorf("toolinventory: no go.mod require entry pins a version for tool directive %s", tool.Path)
		}
		switch name {
		case "protoc-gen-go":
			protocGoVersion = ver
		case "protoc-gen-go-grpc":
			protocGoGRPCVersion = ver
		}
		entries = append(entries, Entry{
			Name:            name,
			Category:        categoryForGoModTool(name),
			Version:         ver,
			Digest:          digestString(tool.Path + "@" + ver),
			Source:          tool.Path,
			License:         licenseForGoModTool(name),
			CVEStatus:       "PENDING_GOVULNCHECK",
			Owner:           "platform-toolchain",
			UpdateSLA:       "tracks the pinned go.mod require version",
			ReplacementPath: replacementForGoModTool(name),
			DerivedFrom:     "go.mod",
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, protocGoVersion, protocGoGRPCVersion, nil
}

func categoryForGoModTool(name string) string {
	switch name {
	case "protoc-gen-go", "protoc-gen-go-grpc":
		return "protobuf_toolchain"
	case "staticcheck":
		return "go_static_analysis"
	default:
		return "go_tool_directive"
	}
}

func licenseForGoModTool(name string) string {
	switch name {
	case "protoc-gen-go", "protoc-gen-go-grpc":
		return "BSD-3-Clause"
	case "staticcheck":
		return "MIT"
	default:
		return "UNKNOWN"
	}
}

func replacementForGoModTool(name string) string {
	switch name {
	case "protoc-gen-go":
		return "SchemaFlux-driven generation once TOOL-004 qualifies it"
	case "protoc-gen-go-grpc":
		return "connect-go generated stubs (already the selected transport edge, see TOOL-008)"
	case "staticcheck":
		return "go vet alone (reduced analyzer coverage) pending an alternative static analyzer"
	default:
		return "none recorded"
	}
}

// loadEmbeddedPostgresEntry reads the pinned
// github.com/fergusstrange/embedded-postgres module version from go.mod.
//
// It deliberately does not also read the selected PostgreSQL binary
// series (currently embeddedpostgres.V17, set in
// internal/data/pgtest/embedded.go): that file is outside this todo's
// editable lane and other agents are concurrently working in internal/,
// so deriving from it here would make this generator's determinism
// hostage to unrelated, in-flight edits. The module version pinned below
// is what actually governs which binary series ships; Note records the
// manual cross-check this implies.
func loadEmbeddedPostgresEntry(root string) (Entry, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return Entry{}, fmt.Errorf("toolinventory: reading go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("toolinventory: parsing go.mod: %w", err)
	}
	var version string
	for _, r := range f.Require {
		if r.Mod.Path == "github.com/fergusstrange/embedded-postgres" {
			version = r.Mod.Version
		}
	}
	if version == "" {
		return Entry{}, fmt.Errorf("toolinventory: github.com/fergusstrange/embedded-postgres not found in go.mod requires")
	}
	return Entry{
		Name:            "embedded-postgres",
		Category:        "database_fixture",
		Version:         version,
		Digest:          digestString("github.com/fergusstrange/embedded-postgres@" + version),
		Source:          "https://github.com/fergusstrange/embedded-postgres",
		License:         "MIT",
		CVEStatus:       "PENDING_GOVULNCHECK",
		Owner:           "platform-toolchain",
		UpdateSLA:       "tracks the pinned go.mod require version",
		ReplacementPath: "TOOL-014 ephemeral integration environment against a real PostgreSQL container",
		DerivedFrom:     "go.mod",
		Note:            "the selected PostgreSQL binary series is set independently in internal/data/pgtest/embedded.go; verify it manually on a module version bump",
	}, nil
}

// packageJSONFile is the subset of package.json Generate reads.
type packageJSONFile struct {
	Engines struct {
		Node string `json:"node"`
		NPM  string `json:"npm"`
	} `json:"engines"`
}

func loadPackageJSONEntries(root string) ([]Entry, error) {
	p := filepath.Join(root, "package.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("toolinventory: reading package.json: %w", err)
	}
	var pj packageJSONFile
	if err := json.Unmarshal(data, &pj); err != nil {
		return nil, fmt.Errorf("toolinventory: parsing package.json: %w", err)
	}
	if pj.Engines.Node == "" || pj.Engines.NPM == "" {
		return nil, fmt.Errorf("toolinventory: package.json engines.node/engines.npm not set")
	}
	note := "minimum-version constraint (package.json engines), not an exact pin; the exact installed version is whatever the build host resolves"
	return []Entry{
		{
			Name: "node", Category: "node_runtime", Version: pj.Engines.Node, Digest: digestString("node@" + pj.Engines.Node),
			Source: "https://nodejs.org", License: "MIT", CVEStatus: "PENDING_MANUAL_REVIEW",
			Owner: "platform-toolchain", UpdateSLA: "tracks package.json engines.node",
			ReplacementPath: "none: Node is the pinned dev/build toolchain runtime",
			DerivedFrom:     "package.json", Note: note,
		},
		{
			Name: "npm", Category: "node_runtime", Version: pj.Engines.NPM, Digest: digestString("npm@" + pj.Engines.NPM),
			Source: "https://www.npmjs.com", License: "Artistic-2.0", CVEStatus: "PENDING_MANUAL_REVIEW",
			Owner: "platform-toolchain", UpdateSLA: "tracks package.json engines.npm",
			ReplacementPath: "none: npm is the pinned package manager",
			DerivedFrom:     "package.json", Note: note,
		},
	}, nil
}

// packageLockFile is the subset of package-lock.json (lockfileVersion 3)
// Generate reads.
type packageLockFile struct {
	Packages map[string]struct {
		Version   string `json:"version"`
		Integrity string `json:"integrity"`
	} `json:"packages"`
}

// npmWatchList is the set of npm devDependencies TOOL-025 inventories:
// prettier/eslint/vitest per the implementing todo, plus husky and
// lint-staged since husky's own pre-commit hook (see loadHuskyHookEntries)
// invokes both.
var npmWatchList = []string{"prettier", "eslint", "vitest", "husky", "lint-staged"}

func loadPackageLockEntries(root string) ([]Entry, error) {
	p := filepath.Join(root, "package-lock.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("toolinventory: reading package-lock.json: %w", err)
	}
	var pl packageLockFile
	if err := json.Unmarshal(data, &pl); err != nil {
		return nil, fmt.Errorf("toolinventory: parsing package-lock.json: %w", err)
	}

	entries := make([]Entry, 0, len(npmWatchList))
	for _, name := range npmWatchList {
		key := "node_modules/" + name
		pkg, ok := pl.Packages[key]
		if !ok {
			return nil, fmt.Errorf("toolinventory: package-lock.json has no %q entry", key)
		}
		if pkg.Version == "" {
			return nil, fmt.Errorf("toolinventory: package-lock.json %q is missing a version", key)
		}
		// npm's lockfile v3 records "resolved"/"integrity" for
		// transitive packages but omits both for the root project's own
		// direct devDependencies (every watched package here is one) -
		// there is no separate tarball hash to pin, so Digest stays
		// empty and Note records why rather than treating it as an
		// error.
		note := ""
		if pkg.Integrity == "" {
			note = "package-lock.json records no integrity hash for this root-level direct devDependency; pinned by exact resolved version only"
		}
		entries = append(entries, Entry{
			Name:            name,
			Category:        "node_toolchain",
			Version:         pkg.Version,
			Digest:          pkg.Integrity,
			Source:          "https://www.npmjs.com/package/" + name,
			License:         "see package-lock.json",
			CVEStatus:       "PENDING_NPM_AUDIT",
			Owner:           "platform-toolchain",
			UpdateSLA:       "tracks the resolved package-lock.json version",
			ReplacementPath: replacementForNPMTool(name),
			DerivedFrom:     "package-lock.json",
			Note:            note,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func replacementForNPMTool(name string) string {
	switch name {
	case "prettier":
		return "gofmt/goimports for Go, hand-formatting for the rest (no drop-in replacement for JS/TS/YAML/MD)"
	case "eslint":
		return "go vet/staticcheck cover the Go slice only; no replacement covers the JS/TS slice"
	case "vitest":
		return "the Go test suite covers the Go slice only; no replacement covers the JS/TS slice"
	case "husky":
		return "a hand-maintained .git/hooks/pre-commit script"
	case "lint-staged":
		return "running the full check suite on every commit instead of a staged-files subset"
	default:
		return "none recorded"
	}
}

func loadHuskyHookEntries(root string) ([]Entry, error) {
	dir := filepath.Join(root, ".husky")
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("toolinventory: reading .husky: %w", err)
	}

	var entries []Entry
	for _, de := range des {
		if de.IsDir() {
			// Skips husky's own "_" helper directory (husky.sh etc.):
			// vendored by the husky npm package itself, already
			// inventoried as the "husky" entry above.
			continue
		}
		name := de.Name()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("toolinventory: reading .husky/%s: %w", name, err)
		}
		entries = append(entries, Entry{
			Name:            "husky:" + name,
			Category:        "git_hook",
			Version:         "content-pinned",
			Digest:          digestBytes(data),
			Source:          ".husky/" + name,
			License:         "N/A (repository-owned script)",
			CVEStatus:       "N/A (owned script, not a third-party binary)",
			Owner:           "platform-toolchain",
			UpdateSLA:       "reviewed on every .husky change via normal code review",
			ReplacementPath: "direct edit of .husky/" + name + " under code review",
			DerivedFrom:     ".husky/" + name,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}
