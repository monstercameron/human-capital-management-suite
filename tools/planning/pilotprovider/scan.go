package pilotprovider

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScanForIdentityLeak walks every ".go" file under root, skipping any
// directory whose path equals or is nested under one of skipDirs, and
// returns the sorted list of files whose bytes contain needle.
//
// This is REFACTOR's enforcement mechanism, not merely its assertion:
// "provider-specific facts remain configuration and adapter qualification
// inputs; semantic workflows and capabilities remain provider-neutral"
// means only the adapter/configuration-qualification boundary
// (internal/connectivity, per specs/integration-platform.md's own
// "Connector Definition + Connection" layer) may know this topology's
// vendor identity. refactor_test.go runs this scan against the real
// internal/ tree, excluding internal/connectivity and internal/generated,
// and asserts zero hits for the checked-in topology's Provider.VendorID: a
// future workflow or capability package that branches on
// "legacyhcm-incumbent-PLACEHOLDER-UNVERIFIED" (or a real vendor id
// substituted for it later) would show up here by name.
func ScanForIdentityLeak(root string, skipDirs []string, needle string) ([]string, error) {
	if strings.TrimSpace(needle) == "" {
		return nil, fmt.Errorf("ScanForIdentityLeak: empty needle would match every file")
	}
	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			for _, skip := range skipDirs {
				if path == skip || strings.HasPrefix(path, skip+string(filepath.Separator)) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if strings.Contains(string(b), needle) {
			hits = append(hits, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(hits)
	return hits, nil
}
