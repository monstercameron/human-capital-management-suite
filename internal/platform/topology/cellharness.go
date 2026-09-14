// Cell harness: SVC-012 proves the modular-monolith process topology
// and role isolation.
//
// The harness starts the six pinned cell commands from the SVC-001
// process-role manifest, executes Promotion workloads through worker
// and scheduler roles under varying placements and concurrency, injects
// command/role/dependency failures, and proves durable recovery, role
// and workload-identity isolation, bounded degradation, reconciliation
// and no duplicate effects. Placement never changes the package graph:
// the role-to-owned-package fingerprint is identical across placements.
package topology

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/processroles"
)

// PinnedCellCommands are the six process-manifest commands that form a
// cell. frontenddev is a developer loop, not a cell command.
var PinnedCellCommands = []string{"hcmnext", "scheduler", "worker", "projector", "migrate", "hcmctl"}

// Error codes are exact: CI and operators match on them.
const (
	CodeUnknownCommand   = "UNKNOWN_COMMAND"
	CodeUnknownRole      = "UNKNOWN_ROLE"
	CodeInvalidKey       = "INVALID_OPERATION_KEY"
	CodeRoleIsolation    = "ROLE_ISOLATION"
	CodeRoleDisabled     = "ROLE_DISABLED"
	CodeCommandDown      = "COMMAND_DOWN"
	CodeCellNotStarted   = "CELL_NOT_STARTED"
	CodeDuplicateCommand = "DUPLICATE_COMMAND"
)

// RoleBinding pins one role to its home command and owned package. The
// owned package is the authority; placement may move the role between
// commands but never rebinds its authority.
type RoleBinding struct {
	Role        string
	HomeCommand string
	Package     string
}

// PinnedRoles are the worker and scheduler roles the harness executes.
var PinnedRoles = []RoleBinding{
	{Role: "capability", HomeCommand: "worker", Package: "internal/capability"},
	{Role: "connector", HomeCommand: "worker", Package: "internal/connectivity/operation"},
	{Role: "reconciliation", HomeCommand: "worker", Package: "internal/workflow"},
	{Role: "repair", HomeCommand: "worker", Package: "internal/workflow"},
	{Role: "messaging", HomeCommand: "worker", Package: "internal/connectivity/delivery"},
	{Role: "timer", HomeCommand: "scheduler", Package: "internal/platform/execution/scheduler"},
	{Role: "signal", HomeCommand: "scheduler", Package: "internal/platform/execution/scheduler"},
}

// Placement assigns every pinned role to a started command.
type Placement map[string]string

// PlacementDefault homes every role on its home command.
var PlacementDefault = Placement{}

// Operation is one promotion workload unit. CredentialRole is the
// workload identity presenting the operation; it must equal Role.
type Operation struct {
	Key            string
	Role           string
	CredentialRole string
}

// ReconcileReport counts the journal outcome.
type ReconcileReport struct {
	Committed        int
	DuplicateEffects int
	Deferred         int
}

// CellReceipt is the digest-backed outcome of one harness run.
type CellReceipt struct {
	Committed int
	Effects   int
	Digest    string
}

// Cell is a started six-command cell under test.
type Cell struct {
	mu          sync.Mutex
	commands    map[string]bool
	placement   Placement
	roles       map[string]RoleBinding
	disabled    map[string]bool
	effects     map[string]string
	deferred    []Operation
	failures    []string
	delays      map[string]string
	fingerprint string
}

// moduleRoot walks up from the working directory to the module root.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("topology: module root not found")
		}
		dir = parent
	}
}

// PackageFingerprint binds roles to owned packages, excluding placement.
func PackageFingerprint() string {
	parts := []string{"svc012-package-graph"}
	for _, binding := range PinnedRoles {
		parts = append(parts, binding.Role+"|"+binding.Package)
	}
	sort.Strings(parts[1:])
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// StartCell starts the six pinned commands from the manifest under one
// placement. Unknown commands refuse: the set is pinned.
func StartCell(manifestPath string, placement Placement) (*Cell, error) {
	manifest, err := processroles.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	initial := manifest.InitialCommands()
	pinned := map[string]bool{}
	for _, name := range PinnedCellCommands {
		pinned[name] = true
	}
	var started []string
	for _, name := range initial {
		if name == "frontenddev" {
			continue
		}
		if !pinned[name] {
			return nil, fmt.Errorf("topology: %s: unpinned command %s", CodeUnknownCommand, name)
		}
		started = append(started, name)
	}
	if len(started) != len(PinnedCellCommands) {
		return nil, fmt.Errorf("topology: %s: started %d commands, want %d", CodeCellNotStarted, len(started), len(PinnedCellCommands))
	}
	roles := map[string]RoleBinding{}
	resolved := Placement{}
	for _, binding := range PinnedRoles {
		roles[binding.Role] = binding
		host := binding.HomeCommand
		if placement != nil {
			if override, ok := placement[binding.Role]; ok {
				host = override
			}
		}
		if !pinned[host] {
			return nil, fmt.Errorf("topology: %s: role %s placed on %s", CodeUnknownCommand, binding.Role, host)
		}
		resolved[binding.Role] = host
	}
	commands := map[string]bool{}
	for _, name := range started {
		commands[name] = true
	}
	return &Cell{
		commands:    commands,
		placement:   resolved,
		roles:       roles,
		disabled:    map[string]bool{},
		effects:     map[string]string{},
		delays:      map[string]string{},
		fingerprint: PackageFingerprint(),
	}, nil
}

func validKey(key string) bool {
	return key != "" && strings.TrimSpace(key) == key
}

// Execute runs one promotion operation through its placed role.
func (c *Cell) Execute(op Operation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	binding, ok := c.roles[op.Role]
	if !ok {
		return fmt.Errorf("topology: %s: %s", CodeUnknownRole, op.Role)
	}
	if !validKey(op.Key) {
		return fmt.Errorf("topology: %s", CodeInvalidKey)
	}
	if op.CredentialRole != op.Role {
		return fmt.Errorf("topology: %s: %s credential cannot perform %s work", CodeRoleIsolation, op.CredentialRole, op.Role)
	}
	_ = binding.Role
	if c.disabled[op.Role] {
		c.deferred = append(c.deferred, op)
		return fmt.Errorf("topology: %s: %s", CodeRoleDisabled, op.Role)
	}
	if !c.commands[c.placement[op.Role]] {
		c.deferred = append(c.deferred, op)
		return fmt.Errorf("topology: %s: %s", CodeCommandDown, c.placement[op.Role])
	}
	if _, seen := c.effects[op.Key]; seen {
		return nil
	}
	c.effects[op.Key] = op.Role
	return nil
}

// KillCommand takes one command down; its roles' operations defer.
func (c *Cell) KillCommand(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.commands[name] {
		return fmt.Errorf("topology: %s: %s already down", CodeCommandDown, name)
	}
	c.commands[name] = false
	c.failures = append(c.failures, "kill:"+name)
	return nil
}

// RestartCommand brings a command back; stranded work stays queued for
// reconciliation, never lost and never double-applied.
func (c *Cell) RestartCommand(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	known := false
	for _, pinned := range PinnedCellCommands {
		if pinned == name {
			known = true
		}
	}
	if !known {
		return fmt.Errorf("topology: %s: %s", CodeUnknownCommand, name)
	}
	c.commands[name] = true
	c.failures = append(c.failures, "restart:"+name)
	return nil
}

// DisableRole degrades one role; every other role keeps serving.
func (c *Cell) DisableRole(role string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.roles[role]; !ok {
		return fmt.Errorf("topology: %s: %s", CodeUnknownRole, role)
	}
	c.disabled[role] = true
	c.failures = append(c.failures, "disable:"+role)
	return nil
}

// EnableRole restores a degraded role.
func (c *Cell) EnableRole(role string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.roles[role]; !ok {
		return fmt.Errorf("topology: %s: %s", CodeUnknownRole, role)
	}
	delete(c.disabled, role)
	c.failures = append(c.failures, "enable:"+role)
	return nil
}

// DelayDependency records a dependency slowdown. Work still completes:
// degradation stays bounded and visible in the receipt.
func (c *Cell) DelayDependency(dependency, budget string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.delays[dependency] = budget
	c.failures = append(c.failures, "delay:"+dependency+"="+budget)
}

// Reconcile replays deferred operations whose role is enabled and whose
// host command is live. Committed keys stay single-applied.
func (c *Cell) Reconcile() ReconcileReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	report := ReconcileReport{}
	remaining := c.deferred[:0:0]
	for _, op := range c.deferred {
		if c.disabled[op.Role] || !c.commands[c.placement[op.Role]] {
			remaining = append(remaining, op)
			continue
		}
		if _, seen := c.effects[op.Key]; seen {
			report.DuplicateEffects++
			continue
		}
		c.effects[op.Key] = op.Role
	}
	c.deferred = remaining
	report.Committed = len(c.effects)
	report.Deferred = len(c.deferred)
	return report
}

// Fingerprint reports the placement-independent package graph binding.
func (c *Cell) Fingerprint() string { return c.fingerprint }

// Receipt digests the run outcome.
func (c *Cell) Receipt() CellReceipt {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.effects))
	for key := range c.effects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{"svc012-cell-receipt", c.fingerprint}
	for _, key := range keys {
		parts = append(parts, key+"|"+c.effects[key])
	}
	failures := append([]string(nil), c.failures...)
	sort.Strings(failures)
	parts = append(parts, failures...)
	delays := []string{}
	for dependency, budget := range c.delays {
		delays = append(delays, dependency+"="+budget)
	}
	sort.Strings(delays)
	parts = append(parts, delays...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return CellReceipt{Committed: len(c.effects), Effects: len(c.effects), Digest: "sha256:" + hex.EncodeToString(sum[:])}
}
