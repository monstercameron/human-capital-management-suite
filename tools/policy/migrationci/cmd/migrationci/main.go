// Command migrationci validates the tracked migration sequence and
// rehearses the clean-upgrade policy over it without applying anything.
//
// By default it scans the migrations directory, builds the manifest from
// the tracked files (version order, sha256 checksums, dependency chain and
// the five lifecycle phases) and validates that shape. With -rehearse it
// additionally runs migrationci.Rehearse with caller-asserted live
// preconditions, exiting non-zero unless the rehearsal reports PASS, so a
// CI or upgrade step fails closed on any structural or precondition gap.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/migrationci"
)

// scannedCompatibility is the compatibility label a directory scan assigns.
// A scan proves sequence integrity (ordering, checksums, dependency chain,
// phase shape), not semantic mixed-version compatibility; per-migration
// compatibility review remains a human gate.
const scannedCompatibility = "BACKWARD_COMPATIBLE"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrationci", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the repository root")
	dir := fs.String("dir", "migrations", "migrations directory, relative to -root")
	manifestPath := fs.String("manifest", "", "JSON manifest file; when empty the manifest is scanned from -dir")
	jsonOutput := fs.Bool("json", false, "emit the machine-readable result")
	rehearse := fs.Bool("rehearse", false, "run the full upgrade rehearsal with the precondition flags below")
	backfillComplete := fs.Bool("backfill-complete", false, "assert the backfill checkpoint reached the source watermark")
	shadowExact := fs.Bool("shadow-exact", false, "assert source and target digests match")
	mixedCompatible := fs.Bool("mixed-version-compatible", false, "assert old and new binaries overlap")
	dirty := fs.Bool("dirty", false, "assert uncommitted or generated drift is present (must be false for PASS)")
	lockHeld := fs.Bool("lock-held", false, "assert another migration owner holds the lock (must be false for PASS)")
	checksumMismatch := fs.Bool("checksum-mismatch", false, "assert recorded bytes differ from the manifest")
	abort := fs.Bool("abort", false, "fence the contract phase after cutover rehearsal")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	migrationsDir := *dir
	if !filepath.IsAbs(migrationsDir) {
		migrationsDir = filepath.Join(*root, migrationsDir)
	}
	manifest, err := loadManifest(*manifestPath, migrationsDir)
	if err != nil {
		fmt.Fprintf(stderr, "migrationci: %v\n", err)
		return 2
	}
	if err := migrationci.Validate(manifest); err != nil {
		fmt.Fprintf(stderr, "migrationci: %v\n", err)
		return 1
	}
	input := migrationci.Input{
		Manifest:               manifest,
		BackfillComplete:       *backfillComplete,
		ShadowExact:            *shadowExact,
		MixedVersionCompatible: *mixedCompatible,
		Dirty:                  *dirty,
		LockHeld:               *lockHeld,
		ChecksumMismatch:       *checksumMismatch,
		AbortRequested:         *abort,
	}
	if *manifestPath != "" {
		if err := verifyChecksums(migrationsDir, manifest); err != nil {
			input.ChecksumMismatch = true
			fmt.Fprintf(stderr, "migrationci: %v\n", err)
		}
	}
	if !*rehearse {
		if *jsonOutput {
			data, marshalErr := json.MarshalIndent(manifest, "", "  ")
			if marshalErr != nil {
				fmt.Fprintf(stderr, "migrationci: encode manifest: %v\n", marshalErr)
				return 2
			}
			fmt.Fprintln(stdout, string(data))
		} else {
			fmt.Fprintf(stdout, "migrationci: manifest valid: %d entries, digest %s\n", len(manifest.Entries), manifest.ReleaseDigest)
		}
		return 0
	}
	result, err := migrationci.Rehearse(input)
	if *jsonOutput {
		data, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			fmt.Fprintf(stderr, "migrationci: encode result: %v\n", marshalErr)
			return 2
		}
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "%s\n", migrationci.ExplainResult(result))
		for _, finding := range result.Findings {
			fmt.Fprintf(stdout, "  - %s %s: %s\n", finding.Code, finding.Field, finding.Detail)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "migrationci: %v\n", err)
		return 1
	}
	return 0
}

// loadManifest reads a checked-in manifest file when -manifest is set, or
// scans the migrations directory otherwise.
func loadManifest(manifestPath, migrationsDir string) (migrationci.Manifest, error) {
	if manifestPath != "" {
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return migrationci.Manifest{}, fmt.Errorf("read manifest %s: %w", manifestPath, err)
		}
		var manifest migrationci.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return migrationci.Manifest{}, fmt.Errorf("decode manifest %s: %w", manifestPath, err)
		}
		return manifest, nil
	}
	return scanManifest(migrationsDir)
}

// scanManifest builds a manifest from NNNNN_name.sql files: versions must
// be numeric prefixes, entries chain in order, and the digest covers the
// exact file bytes, so a renamed, reordered or edited migration changes
// the manifest.
func scanManifest(migrationsDir string) (migrationci.Manifest, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return migrationci.Manifest{}, fmt.Errorf("read migrations directory %s: %w", migrationsDir, err)
	}
	type scanned struct {
		version  int64
		name     string
		checksum string
	}
	found := make([]scanned, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".sql")
		underscore := strings.Index(base, "_")
		if underscore <= 0 {
			return migrationci.Manifest{}, fmt.Errorf("migration %q has no numeric version prefix", entry.Name())
		}
		version, err := strconv.ParseInt(base[:underscore], 10, 64)
		if err != nil || version <= 0 {
			return migrationci.Manifest{}, fmt.Errorf("migration %q has no numeric version prefix", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			return migrationci.Manifest{}, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(data)
		found = append(found, scanned{version: version, name: base, checksum: hex.EncodeToString(sum[:])})
	}
	if len(found) == 0 {
		return migrationci.Manifest{}, fmt.Errorf("no migrations found in %s", migrationsDir)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].version < found[j].version })
	manifest := migrationci.Manifest{
		Phases: []migrationci.Phase{
			migrationci.PhaseExpand,
			migrationci.PhaseBackfill,
			migrationci.PhaseShadow,
			migrationci.PhaseCutover,
			migrationci.PhaseContract,
		},
	}
	digest := sha256.New()
	for i, item := range found {
		entry := migrationci.Entry{
			Version:       item.version,
			Name:          item.name,
			Checksum:      item.checksum,
			Compatibility: scannedCompatibility,
			Direction:     "UP",
		}
		if i > 0 {
			entry.Requires = []int64{found[i-1].version}
		}
		manifest.Entries = append(manifest.Entries, entry)
		fmt.Fprintf(digest, "%d\x00%s\x00%s\x00", item.version, item.name, item.checksum)
	}
	manifest.ReleaseDigest = hex.EncodeToString(digest.Sum(nil))
	return manifest, nil
}

// verifyChecksums recomputes every manifest entry against the directory.
// Any unreadable file, unknown version or byte difference is reported; the
// caller marks the rehearsal input mismatched so Rehearse rejects it.
func verifyChecksums(migrationsDir string, manifest migrationci.Manifest) error {
	byVersion := make(map[int64]migrationci.Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		byVersion[entry.Version] = entry
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations directory %s: %w", migrationsDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".sql")
		underscore := strings.Index(base, "_")
		if underscore <= 0 {
			continue
		}
		version, err := strconv.ParseInt(base[:underscore], 10, 64)
		if err != nil {
			continue
		}
		want, ok := byVersion[version]
		if !ok {
			return fmt.Errorf("migration %s is not in the manifest", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want.Checksum {
			return fmt.Errorf("migration %s bytes differ from the manifest", entry.Name())
		}
	}
	return nil
}
