package releaseevidence

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// ErrIrreversible means a rollback range crosses a migration that cannot be
// reversed, so the slice cannot prove it can return to the older release.
var ErrIrreversible = errors.New("releaseevidence: migration range is not reversible")

// Migration is one schema step the rollback planner may reverse. Reversible
// is false when the migration's Down section declares irreversibility.
type Migration struct {
	Version    int64 `json:"version"`
	Reversible bool  `json:"reversible"`
}

// EmbeddedHistory returns the embedded migration tree as planner input,
// oldest first. A migration is reversible unless its Down section declares
// the documented "is irreversible" convention.
func EmbeddedHistory() ([]Migration, error) {
	files, err := migrations.Files()
	if err != nil {
		return nil, err
	}
	out := make([]Migration, 0, len(files))
	for _, f := range files {
		body, err := migrations.FS.ReadFile(f.Name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", f.Name, err)
		}
		down := string(body)
		if idx := strings.Index(down, "-- +goose Down"); idx >= 0 {
			down = down[idx:]
		}
		out = append(out, Migration{Version: f.Version, Reversible: !strings.Contains(down, "is irreversible")})
	}
	return out, nil
}

// RollbackPlan is the ordered proof that a slice can move from one recorded
// release back to an older one. Steps names every schema version to reverse,
// newest first; an empty step list is a binary-only rollback at one schema
// version.
type RollbackPlan struct {
	Slice       string  `json:"slice"`
	FromVersion string  `json:"from_version"`
	ToVersion   string  `json:"to_version"`
	FromSchema  int64   `json:"from_schema"`
	ToSchema    int64   `json:"to_schema"`
	Steps       []int64 `json:"steps"`
	Authority   string  `json:"authority"`
}

// PlanRollback proves a slice can return from one recorded release to an
// older recorded release. Both releases must verify, the target schema must
// be older or equal, and every migration between them must be reversible.
// Unknown or irreversible steps fail closed with the blocking version named.
func PlanRollback(j *Journal, slice, fromVersion, toVersion, authority string, history []Migration) (RollbackPlan, error) {
	if j == nil {
		return RollbackPlan{}, fmt.Errorf("%w: journal is required", ErrInvalid)
	}
	if strings.TrimSpace(authority) == "" || strings.TrimSpace(authority) != authority {
		return RollbackPlan{}, fmt.Errorf("%w: rollback authority is required", ErrInvalid)
	}
	from, err := j.Get(slice, fromVersion)
	if err != nil {
		return RollbackPlan{}, err
	}
	to, err := j.Get(slice, toVersion)
	if err != nil {
		return RollbackPlan{}, err
	}
	if err := from.Verify(); err != nil {
		return RollbackPlan{}, fmt.Errorf("%w: rollback source: %v", ErrInvalid, err)
	}
	if err := to.Verify(); err != nil {
		return RollbackPlan{}, fmt.Errorf("%w: rollback target: %v", ErrInvalid, err)
	}
	if fromVersion == toVersion {
		return RollbackPlan{}, fmt.Errorf("%w: %s %s is already recorded", ErrInvalid, slice, fromVersion)
	}
	if to.SchemaVersion > from.SchemaVersion {
		return RollbackPlan{}, fmt.Errorf("%w: schema %d to %d is not a rollback", ErrInvalid, from.SchemaVersion, to.SchemaVersion)
	}
	known := make(map[int64]Migration, len(history))
	for _, m := range history {
		known[m.Version] = m
	}
	if _, ok := known[from.SchemaVersion]; !ok {
		return RollbackPlan{}, fmt.Errorf("%w: schema %d is not in the embedded history", ErrInvalid, from.SchemaVersion)
	}
	if _, ok := known[to.SchemaVersion]; !ok {
		return RollbackPlan{}, fmt.Errorf("%w: schema %d is not in the embedded history", ErrInvalid, to.SchemaVersion)
	}
	ordered := append([]Migration(nil), history...)
	sort.Slice(ordered, func(i, k int) bool { return ordered[i].Version > ordered[k].Version })
	steps := []int64{}
	for _, m := range ordered {
		if m.Version <= to.SchemaVersion || m.Version > from.SchemaVersion {
			continue
		}
		if !m.Reversible {
			return RollbackPlan{}, fmt.Errorf("%w: migration %d refuses rollback", ErrIrreversible, m.Version)
		}
		steps = append(steps, m.Version)
	}
	return RollbackPlan{Slice: slice, FromVersion: fromVersion, ToVersion: toVersion,
		FromSchema: from.SchemaVersion, ToSchema: to.SchemaVersion, Steps: steps, Authority: authority}, nil
}

// Verify proves a runtime now matches the rollback target: the target still
// verifies and skew detection against it is clean.
func (p RollbackPlan) Verify(target Release, rt Runtime) error {
	if err := target.Verify(); err != nil {
		return err
	}
	if target.Slice != p.Slice || target.Version != p.ToVersion || target.SchemaVersion != p.ToSchema {
		return fmt.Errorf("%w: plan rolls back to %s %s schema %d", ErrInvalid, p.Slice, p.ToVersion, p.ToSchema)
	}
	skews, err := Detect(target, rt)
	if err != nil {
		return err
	}
	if len(skews) != 0 {
		return fmt.Errorf("%w: runtime does not match rollback target %s: %v", ErrInvalid, target.Version, skews)
	}
	return nil
}

// Summary returns the redaction-safe one-line plan evidence.
func (p RollbackPlan) Summary() string {
	return fmt.Sprintf("rollback %s %s(schema %d) -> %s(schema %d) steps=%d authority=%s",
		p.Slice, p.FromVersion, p.FromSchema, p.ToVersion, p.ToSchema, len(p.Steps), p.Authority)
}
