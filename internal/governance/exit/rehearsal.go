package exit

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Pilot exit rehearsal (TENANT-004) proves a tenant *could* leave before
// anyone must. It wraps the dry-run exit certification ([Build]) with the
// rehearsed operator steps, and returns the same verdict vocabulary:
// CERTIFIABLE only when the inventory is complete and every step was
// observed. Like [Build] it is kernel-pure and performs no irreversible
// production destruction; a rehearsal that cannot certify names its
// blockers instead of destroying anything.

// RehearsalStep is one operator step the pilot exit rehearsal performed.
// An unobserved step is a blocker, never an assumption.
type RehearsalStep struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Observed    bool   `json:"observed"`
	EvidenceRef string `json:"evidence_ref"`
}

// RestorePlanEntry is one copy's restore re-delete disposition: the
// tombstone and watermark a restore must reapply before the copy serves
// again, or a pending marker when that evidence does not exist yet.
type RestorePlanEntry struct {
	CopyID          string `json:"copy_id"`
	TombstoneDigest string `json:"tombstone_digest"`
	Watermark       string `json:"watermark"`
	Ready           bool   `json:"ready"`
}

// RehearsalRequest is the complete rehearsal envelope.
type RehearsalRequest struct {
	RehearsalID string          `json:"rehearsal_id"`
	Tenant      string          `json:"tenant"`
	RequestedBy string          `json:"requested_by"`
	At          time.Time       `json:"at"`
	Exit        Request         `json:"exit"`
	Steps       []RehearsalStep `json:"steps"`
}

// RehearsalReceipt is the immutable rehearsal result.
type RehearsalReceipt struct {
	RehearsalID      string              `json:"rehearsal_id"`
	Tenant           string              `json:"tenant"`
	Status           string              `json:"status"`
	Export           ExportReceipt       `json:"export"`
	ShutdownComplete bool                `json:"shutdown_complete"`
	Revocations      []RevocationReceipt `json:"revocations"`
	Exceptions       []HoldException     `json:"exceptions"`
	RestorePlan      []RestorePlanEntry  `json:"restore_plan"`
	Steps            []RehearsalStep     `json:"steps"`
	Blockers         []Blocker           `json:"blockers"`
	Digest           string              `json:"digest"`
}

// Explain returns a bounded summary suitable for an operator log.
func (r RehearsalReceipt) Explain() string {
	return fmt.Sprintf("tenant exit rehearsal v%d id=%s tenant=%s status=%s steps=%d revocations=%d restore_ready=%d blockers=%d digest=%s",
		Version(), r.RehearsalID, r.Tenant, r.Status, len(r.Steps), len(r.Revocations), readyRestore(r.RestorePlan), len(r.Blockers), r.Digest)
}

func readyRestore(plan []RestorePlanEntry) int {
	n := 0
	for _, e := range plan {
		if e.Ready {
			n++
		}
	}
	return n
}

// Rehearse evaluates one pilot exit rehearsal. Inventory certification
// comes from [Build]; step observation and the restore plan are layered
// here. A malformed envelope is an error; an incomplete rehearsal is a
// BLOCKED receipt, never a partial certification.
func Rehearse(req RehearsalRequest) (RehearsalReceipt, error) {
	if err := validateRehearsal(req); err != nil {
		return RehearsalReceipt{}, err
	}
	plan, err := Build(req.Exit)
	if err != nil {
		return RehearsalReceipt{}, err
	}
	blockers := append([]Blocker(nil), plan.Blockers...)
	blockers = append(blockers, stepBlockers(req.Steps)...)
	restore := restorePlan(req.Exit)
	for _, e := range restore {
		if !e.Ready {
			blockers = append(blockers, Blocker{"RESTORE_PLAN_INCOMPLETE", e.CopyID, "restore re-delete evidence is not ready"})
		}
	}
	sort.SliceStable(blockers, func(i, j int) bool {
		if blockers[i].Code != blockers[j].Code {
			return blockers[i].Code < blockers[j].Code
		}
		return blockers[i].Subject < blockers[j].Subject
	})
	rec := RehearsalReceipt{
		RehearsalID: req.RehearsalID, Tenant: req.Tenant, Status: StatusCertifiable,
		Export: req.Exit.Export, ShutdownComplete: req.Exit.ShutdownComplete,
		Revocations: cloneRevocations(req.Exit.Revocations),
		Exceptions:  cloneHolds(req.Exit.HoldExceptions),
		RestorePlan: restore, Steps: cloneSteps(req.Steps), Blockers: blockers,
	}
	if len(blockers) != 0 {
		rec.Status = StatusBlocked
	}
	rec.Digest = digestValue(canonicalReceipt(rec))
	return rec, nil
}

func validateRehearsal(req RehearsalRequest) error {
	switch {
	case strings.TrimSpace(req.RehearsalID) == "":
		return fmt.Errorf("%w: rehearsal id is required", ErrInvalidRequest)
	case strings.TrimSpace(req.Tenant) == "":
		return fmt.Errorf("%w: tenant is required", ErrInvalidRequest)
	case strings.TrimSpace(req.RequestedBy) == "":
		return fmt.Errorf("%w: requester is required", ErrInvalidRequest)
	case req.At.IsZero():
		return fmt.Errorf("%w: rehearsal time is required", ErrInvalidRequest)
	case req.Tenant != req.Exit.Tenant:
		return fmt.Errorf("%w: rehearsal tenant %q does not match exit tenant %q", ErrInvalidRequest, req.Tenant, req.Exit.Tenant)
	case len(req.Steps) == 0:
		return fmt.Errorf("%w: at least one rehearsed step is required", ErrInvalidRequest)
	}
	seen := map[string]bool{}
	for i, s := range req.Steps {
		switch {
		case strings.TrimSpace(s.ID) == "" || s.ID != strings.TrimSpace(s.ID):
			return fmt.Errorf("%w: step %d has no clean identity", ErrInvalidRequest, i)
		case seen[s.ID]:
			return fmt.Errorf("%w: step %q rehearsed twice", ErrInvalidRequest, s.ID)
		case strings.TrimSpace(s.Title) == "":
			return fmt.Errorf("%w: step %q has no title", ErrInvalidRequest, s.ID)
		case s.Observed && strings.TrimSpace(s.EvidenceRef) == "":
			return fmt.Errorf("%w: observed step %q cites no evidence", ErrInvalidRequest, s.ID)
		}
		seen[s.ID] = true
	}
	return nil
}

func stepBlockers(steps []RehearsalStep) []Blocker {
	var out []Blocker
	for _, s := range steps {
		if !s.Observed {
			out = append(out, Blocker{"STEP_UNOBSERVED", s.ID, "rehearsal step was not observed with evidence"})
		}
	}
	return out
}

func restorePlan(req Request) []RestorePlanEntry {
	ready := map[string]RestoreReDelete{}
	for _, r := range req.RestoreReDeletes {
		if r.Reapplied && r.TombstoneDigest != "" && r.Watermark != "" {
			ready[r.CopyID] = r
		}
	}
	var plan []RestorePlanEntry
	for _, c := range req.Copies {
		if !c.RestoreReDeleteNeeded {
			continue
		}
		if r, ok := ready[c.ID]; ok {
			plan = append(plan, RestorePlanEntry{CopyID: c.ID, TombstoneDigest: r.TombstoneDigest, Watermark: r.Watermark, Ready: true})
		} else {
			plan = append(plan, RestorePlanEntry{CopyID: c.ID, Ready: false})
		}
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i].CopyID < plan[j].CopyID })
	return plan
}

func canonicalReceipt(r RehearsalReceipt) RehearsalReceipt {
	r.Digest = ""
	return r
}

func cloneSteps(in []RehearsalStep) []RehearsalStep { return append([]RehearsalStep(nil), in...) }

func hasBlocker(blockers []Blocker, code string) bool {
	for _, b := range blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}
