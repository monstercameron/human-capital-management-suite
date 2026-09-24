// Command migrationci validates a pinned migration manifest against the exact
// SQL migration files in the checkout and can run the production migration
// command against a disposable PostgreSQL instance.
//
// Use -manifest to name the checked-in manifest. Directory scanning remains
// available for generating a candidate manifest with -json, but assigns no
// compatibility claim. The database rehearsal proves the clean-schema SQL
// upgrade path; it does not claim mixed-version or live backfill evidence.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/migrations"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/migrationci"
)

const scannedCompatibility = "UNREVIEWED"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrationci", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the repository root")
	dir := fs.String("dir", "migrations", "migrations directory, relative to -root")
	manifestPath := fs.String("manifest", "", "JSON manifest file; when empty the manifest is scanned from -dir")
	jsonOutput := fs.Bool("json", false, "emit the machine-readable result")
	rehearse := fs.Bool("rehearse", false, "apply the embedded migration tree to a disposable PostgreSQL instance")
	databaseURL := fs.String("database-url", os.Getenv("HCMNEXT_MIGRATIONCI_DATABASE_URL"), "disposable PostgreSQL URL (or HCMNEXT_MIGRATIONCI_DATABASE_URL); required with -rehearse")
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
	if *manifestPath != "" {
		if err := verifyChecksums(migrationsDir, manifest); err != nil {
			fmt.Fprintf(stderr, "migrationci: %v\n", err)
			return 1
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := rehearseDatabase(ctx, *root, *databaseURL, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "migrationci: database rehearsal failed: %v\n", err)
		return 1
	}
	target, err := migrations.TargetVersion()
	if err != nil {
		fmt.Fprintf(stderr, "migrationci: database rehearsal failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "migrationci: database rehearsal PASS: clean schema reached migration %d\n", target)
	return 0
}

// rehearseDatabase invokes the production migration composition root against
// CI's disposable PostgreSQL instance. Migration SQL includes cluster-wide
// role changes, so the target server must be discarded after the run.
func rehearseDatabase(ctx context.Context, root, databaseURL string, stdout, stderr io.Writer) error {
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("-database-url or HCMNEXT_MIGRATIONCI_DATABASE_URL is required with -rehearse")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	command := exec.CommandContext(ctx, "go", "run", "./cmd/migrate", "up", "-database-url", databaseURL)
	command.Dir = absoluteRoot
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("production migration runner: %w", err)
	}
	return nil
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

// verifyChecksums compares the migration directory's release digest, exact
// filename/version inventory, and each file checksum against the pinned
// manifest. Added, removed, renamed, or edited migration files are rejected.
func verifyChecksums(migrationsDir string, manifest migrationci.Manifest) error {
	actual, err := scanManifest(migrationsDir)
	if err != nil {
		return err
	}
	if actual.ReleaseDigest != manifest.ReleaseDigest {
		return fmt.Errorf("migration directory release digest differs from pinned manifest")
	}
	byVersion := make(map[int64]migrationci.Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		byVersion[entry.Version] = entry
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations directory %s: %w", migrationsDir, err)
	}
	seen := make(map[int64]bool, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".sql")
		underscore := strings.Index(base, "_")
		if underscore <= 0 {
			return fmt.Errorf("migration %s has no numeric version prefix", entry.Name())
		}
		version, err := strconv.ParseInt(base[:underscore], 10, 64)
		if err != nil || version <= 0 {
			return fmt.Errorf("migration %s has no numeric version prefix", entry.Name())
		}
		want, ok := byVersion[version]
		if !ok {
			return fmt.Errorf("migration %s is not in the manifest", entry.Name())
		}
		if want.Name != base {
			return fmt.Errorf("migration %s does not match manifest name %s", entry.Name(), want.Name+".sql")
		}
		if seen[version] {
			return fmt.Errorf("migration version %d appears more than once", version)
		}
		seen[version] = true
		data, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want.Checksum {
			return fmt.Errorf("migration %s bytes differ from the manifest", entry.Name())
		}
	}
	for version, expected := range byVersion {
		if !seen[version] {
			return fmt.Errorf("manifest migration %s.sql is missing", expected.Name)
		}
	}
	return nil
}
