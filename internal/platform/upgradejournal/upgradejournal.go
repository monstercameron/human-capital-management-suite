// Package upgradejournal is the REV-003-02 durable adapter over the
// side-effect-free rolling-upgrade protocol in
// internal/platform/schemaupgrade.
//
// The protocol package never touches a disk or a database: it validates
// plans, advances value-object state and returns checkpoints an adapter may
// persist. This package owns the only persistence: a Coordinator journals
// every lifecycle transition to a file so a killed process resumes the same
// upgrade from the persisted checkpoint instead of restarting it, and a
// served binary (cmd/migrate's upgrade subcommand) drives real version
// transitions through it end to end.
package upgradejournal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/schemaupgrade"
)

// Coordinator persists each upgrade transition to a durable journal file and
// drives a real version transition end to end against that journal.
type Coordinator struct {
	path string
	now  func() time.Time
}

// OpenCoordinator binds a coordinator to a journal file path. now supplies
// transition timestamps; a nil now falls back to the system clock.
func OpenCoordinator(path string, now func() time.Time) (*Coordinator, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: journal path is required", schemaupgrade.ErrInvalidPlan)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Coordinator{path: path, now: now}, nil
}

// Started reports whether a journal file already exists at the coordinator's
// path, so a caller can tell a resume from a fresh start without reading the
// journal.
func (c *Coordinator) Started() bool {
	_, err := os.Stat(c.path)
	return err == nil
}

// Start validates the plan, records the initial state and journals it.
func (c *Coordinator) Start(plan schemaupgrade.Plan) (schemaupgrade.State, error) {
	state, err := schemaupgrade.New(plan, c.now())
	if err != nil {
		return schemaupgrade.State{}, err
	}
	if err := c.save(state); err != nil {
		return schemaupgrade.State{}, err
	}
	return state, nil
}

// Load returns the last journaled state.
func (c *Coordinator) Load() (schemaupgrade.State, error) {
	return c.load()
}

// RunToCutover resumes the journaled upgrade and drives it through expand,
// resumable backfill, exact shadow comparison and cutover, journaling every
// step. The shadow target defaults to the source: the new representation
// must reproduce the source projection exactly. It is idempotent:
// re-running it after a process kill resumes from the persisted checkpoint
// without duplicating history or copying rows twice.
func (c *Coordinator) RunToCutover(source []schemaupgrade.Row, watermark uint64) (schemaupgrade.State, error) {
	return c.RunToCutoverWithTarget(source, source, watermark)
}

// RunToCutoverWithTarget is RunToCutover with an explicit shadow target for
// callers whose new projection is built separately from the source rows. A
// target that differs from the source is refused with the protocol's typed
// shadow-mismatch error and the journal keeps the refused comparison.
func (c *Coordinator) RunToCutoverWithTarget(source, target []schemaupgrade.Row, watermark uint64) (schemaupgrade.State, error) {
	state, err := c.load()
	if err != nil {
		return schemaupgrade.State{}, err
	}
	if state.Phase == schemaupgrade.PhasePlanned {
		if err := state.Expand(c.now()); err != nil {
			return schemaupgrade.State{}, err
		}
		if err := c.save(state); err != nil {
			return schemaupgrade.State{}, err
		}
	}
	for !state.Checkpoint.Completed {
		before := len(state.History)
		if _, err := state.ResumeBackfill(source, state.Plan.BackfillBatchSize, c.now()); err != nil {
			return schemaupgrade.State{}, err
		}
		if err := c.save(state); err != nil {
			return schemaupgrade.State{}, err
		}
		if len(state.History) == before && state.Checkpoint.Completed {
			break
		}
	}
	if state.Phase == schemaupgrade.PhaseBackfilled {
		if _, err := state.CompareShadow(source, target, c.now()); err != nil {
			if saveErr := c.save(state); saveErr != nil {
				return schemaupgrade.State{}, errors.Join(err, saveErr)
			}
			return schemaupgrade.State{}, err
		}
		if err := c.save(state); err != nil {
			return schemaupgrade.State{}, err
		}
	}
	if state.Phase == schemaupgrade.PhaseShadowed {
		if err := state.Cutover(watermark, c.now()); err != nil {
			return schemaupgrade.State{}, err
		}
		if err := c.save(state); err != nil {
			return schemaupgrade.State{}, err
		}
	}
	return state, nil
}

func (c *Coordinator) load() (schemaupgrade.State, error) {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return schemaupgrade.State{}, fmt.Errorf("%w: read journal: %v", schemaupgrade.ErrCheckpointMismatch, err)
	}
	var state schemaupgrade.State
	if err := json.Unmarshal(raw, &state); err != nil {
		return schemaupgrade.State{}, fmt.Errorf("%w: decode journal: %v", schemaupgrade.ErrCheckpointMismatch, err)
	}
	return state, nil
}

func (c *Coordinator) save(state schemaupgrade.State) error {
	raw, err := json.Marshal(state.Snapshot())
	if err != nil {
		return fmt.Errorf("schema upgrade: encode journal: %w", err)
	}
	if dir := filepath.Dir(c.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("schema upgrade: create journal dir: %w", err)
		}
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("schema upgrade: write journal: %w", err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return fmt.Errorf("schema upgrade: commit journal: %w", err)
	}
	return nil
}
