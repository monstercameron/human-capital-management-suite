// REV-003-02 adapter: the migrate binary's upgrade subcommand.
//
// TOOL-020 proved the rolling-upgrade protocol only inside its own package
// tests. This file is the adapter that makes one real binary persist the
// schemaupgrade journal durably and drive a version transition end to end:
// the operator hands migrate a plan file, a source-rows file and a journal
// path, and runUpgradeCommand resumes-or-starts the journaled upgrade and
// runs it to cutover, journaling every step. Killing the process mid-upgrade
// and re-running the same command resumes from the persisted checkpoint
// without duplicating history.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/schemaupgrade"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/upgradejournal"
)

// upgradeReceipt is the operator-visible evidence of one upgrade run. It is
// written only after the journaled state is durably committed, so a
// successful-looking receipt can never describe a lost transition.
type upgradeReceipt struct {
	PlanID     string `json:"plan"`
	Phase      string `json:"phase"`
	CopiedRows int    `json:"copied_rows"`
	History    int    `json:"history_events"`
}

// runUpgradeCommand drives one rolling upgrade through the durable journal
// at journalPath. planPath carries the JSON-encoded schemaupgrade.Plan the
// release process published; rowsPath carries the JSON-encoded source rows
// (key/digest pairs exported from the source representation). When no
// journal exists yet the plan is validated and recorded as the starting
// point; when one exists the recorded upgrade resumes from it, so the same
// invocation is both the starter and the crash-recovery path. A watermark
// below the plan's required adoption watermark is refused with the
// protocol's typed adoption-lag error and the journal is left untouched.
func runUpgradeCommand(journalPath, planPath, rowsPath string, watermark uint64, out io.Writer) error {
	if journalPath == "" {
		return fmt.Errorf("upgrade journal path must not be empty")
	}
	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("read upgrade plan %q: %w", planPath, err)
	}
	var plan schemaupgrade.Plan
	if err := json.Unmarshal(planRaw, &plan); err != nil {
		return fmt.Errorf("decode upgrade plan %q: %w", planPath, err)
	}
	rowsRaw, err := os.ReadFile(rowsPath)
	if err != nil {
		return fmt.Errorf("read upgrade rows %q: %w", rowsPath, err)
	}
	var source []schemaupgrade.Row
	if err := json.Unmarshal(rowsRaw, &source); err != nil {
		return fmt.Errorf("decode upgrade rows %q: %w", rowsPath, err)
	}
	coord, err := upgradejournal.OpenCoordinator(journalPath, nil)
	if err != nil {
		return err
	}
	if !coord.Started() {
		if _, err := coord.Start(plan); err != nil {
			return fmt.Errorf("start upgrade: %w", err)
		}
	}
	state, err := coord.RunToCutover(source, watermark)
	if err != nil {
		return fmt.Errorf("drive upgrade: %w", err)
	}
	receipt := upgradeReceipt{
		PlanID:     state.Plan.ID,
		Phase:      string(state.Phase),
		CopiedRows: state.Checkpoint.CopiedRows,
		History:    len(state.History),
	}
	if err := json.NewEncoder(out).Encode(receipt); err != nil {
		return fmt.Errorf("write upgrade receipt: %w", err)
	}
	return nil
}

// isUpgradeAdoptionLag reports whether err is the protocol's typed refusal
// to cut over before consumers adopted the new representation.
func isUpgradeAdoptionLag(err error) bool {
	return errors.Is(err, schemaupgrade.ErrAdoptionLag)
}
