package defaultproduct

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ALIGN-042: the application shell is authorization-resolved. The seed
// lists every shell entry with the capability it requires; resolving
// against the admitted capabilities drops everything the caller may not
// see. An unadmitted entry never renders, and resolution is deterministic
// over the admitted set.

// Shell errors.
var (
	ErrShellInvalid = errors.New("defaultproduct: application shell is invalid")
)

// ShellEntry is one seeded shell slot: navigation, header, or rail item.
type ShellEntry struct {
	ID                 string `json:"id"`
	Label              string `json:"label"`
	Route              string `json:"route"`
	RequiredCapability string `json:"required_capability"`
}

// Shell is the resolved, renderable shell: only admitted entries.
type Shell struct {
	Entries []ShellEntry `json:"entries"`
	Digest  string       `json:"digest"`
}

// DefaultShellEntries seeds the shell with required capabilities.
func DefaultShellEntries() []ShellEntry {
	return []ShellEntry{
		{ID: "shell.nav.home", Label: "Home", Route: "/home", RequiredCapability: "product.view"},
		{ID: "shell.nav.promotion", Label: "Promotions", Route: "/promotion", RequiredCapability: "promotion.view"},
		{ID: "shell.nav.promotion.execute", Label: "Execute promotion", Route: "/promotion/execute", RequiredCapability: "promotion.execute"},
		{ID: "shell.nav.repair", Label: "Repair workbench", Route: "/operations/repair", RequiredCapability: "operations.repair"},
	}
}

// ValidateShell enforces unique safe IDs, labels, absolute routes, and a
// required capability per entry.
func ValidateShell(entries []ShellEntry) error {
	seen := make(map[string]bool)
	for _, entry := range entries {
		if !safeID(entry.ID) {
			return fmt.Errorf("%w: entry id %q", ErrShellInvalid, entry.ID)
		}
		if seen[entry.ID] {
			return fmt.Errorf("%w: duplicate entry %q", ErrShellInvalid, entry.ID)
		}
		seen[entry.ID] = true
		if strings.TrimSpace(entry.Label) == "" {
			return fmt.Errorf("%w: entry %q needs a label", ErrShellInvalid, entry.ID)
		}
		if !strings.HasPrefix(entry.Route, "/") || strings.Contains(entry.Route, "..") {
			return fmt.Errorf("%w: entry %q route %q is not absolute and clean", ErrShellInvalid, entry.ID, entry.Route)
		}
		if !safeID(entry.RequiredCapability) {
			return fmt.Errorf("%w: entry %q needs a capability", ErrShellInvalid, entry.ID)
		}
	}
	return nil
}

// ResolveShell keeps exactly the entries whose capability is admitted.
// Admitted entries render in seed order; the digest covers the admitted
// set, so two callers with the same grants resolve the same shell.
func ResolveShell(entries []ShellEntry, admitted []string) (Shell, error) {
	if err := ValidateShell(entries); err != nil {
		return Shell{}, err
	}
	grants := make(map[string]bool, len(admitted))
	for _, capability := range admitted {
		grants[capability] = true
	}
	shell := Shell{}
	for _, entry := range entries {
		if grants[entry.RequiredCapability] {
			shell.Entries = append(shell.Entries, entry)
		}
	}
	if shell.Entries == nil {
		shell.Entries = []ShellEntry{}
	}
	b, err := json.Marshal(shell.Entries)
	if err != nil {
		return Shell{}, fmt.Errorf("%w: %v", ErrShellInvalid, err)
	}
	sum := sha256.Sum256(b)
	shell.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return shell, nil
}

// ShellCapabilities reports the distinct capabilities the seed requires,
// in sorted order.
func ShellCapabilities(entries []ShellEntry) []string {
	set := make(map[string]bool)
	for _, entry := range entries {
		set[entry.RequiredCapability] = true
	}
	out := make([]string, 0, len(set))
	for capability := range set {
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
}
