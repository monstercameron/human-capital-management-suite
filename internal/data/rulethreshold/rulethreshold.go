// Package rulethreshold freezes the RULE-003 threshold decision one
// proposal revision was admitted under, so RULE-004 can re-evaluate it at
// the commit: the live inputs against the frozen inputs, tier and row, and
// the current table against the frozen table version. The writer is the
// raise_threshold branch inside the advancement transaction; the reader is
// the served RuleFacts. One revision is evaluated against one set of
// inputs: a second, different decision for the same key is refused rather
// than overwritten, and moved inputs need a new revision, not a rewritten
// past. Latest attempt wins the read.
package rulethreshold

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrConflict reports a second, different threshold decision for a key that
// already froze one. The frozen decision stands; the caller re-plans under
// a new revision instead.
var ErrConflict = errors.New("rulethreshold: threshold decision already frozen for this revision and attempt")

// InputsJSONKeys is the exact JSON shape of the frozen inputs. The percent
// carries its declared scale alongside its text, so the read-back decimal
// is the identical value and not a same-text different-scale cousin.
const (
	InputKeyIncreasePercent      = "increase_percent"
	InputKeyIncreasePercentScale = "increase_percent_scale"
	InputKeyBandPosition         = "band_position"
	InputKeyBudgetAuthority      = "budget_authority"
	InputKeyGradeChange          = "grade_change"
)

// Decision is one frozen threshold evaluation.
type Decision struct {
	TenantID   uuid.UUID
	IntentID   uuid.UUID
	Revision   uint64
	Attempt    int
	InstanceID uuid.UUID

	Tier         string
	MatchedRow   string
	TableID      string
	TableVersion string
	TableDigest  string
	InputDigest  string
	Input        rules.PromotionApprovalInput

	RecordedAt time.Time
}

// inputsPayload renders the inputs at their canonical text: a decimal at
// its declared scale, "true"/"false", strings verbatim.
func inputsPayload(in rules.PromotionApprovalInput) (map[string]any, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	return map[string]any{
		InputKeyIncreasePercent:      in.IncreasePercent.String(),
		InputKeyIncreasePercentScale: int(in.IncreasePercent.Scale()),
		InputKeyBandPosition:         string(in.BandPosition),
		InputKeyBudgetAuthority:      string(in.BudgetAuthority),
		InputKeyGradeChange:          in.GradeChange,
	}, nil
}

// parseInputs parses the frozen payload back, strictly: unknown shapes are
// refused rather than coerced.
func parseInputs(raw []byte) (rules.PromotionApprovalInput, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return rules.PromotionApprovalInput{}, fmt.Errorf("rulethreshold: decode frozen inputs: %w", err)
	}
	percentText, _ := payload[InputKeyIncreasePercent].(string)
	scaleFloat, _ := payload[InputKeyIncreasePercentScale].(float64)
	bandText, _ := payload[InputKeyBandPosition].(string)
	authorityText, _ := payload[InputKeyBudgetAuthority].(string)
	gradeChange, _ := payload[InputKeyGradeChange].(bool)
	if percentText == "" || bandText == "" || authorityText == "" {
		return rules.PromotionApprovalInput{}, fmt.Errorf("rulethreshold: frozen inputs miss a required field")
	}
	percent, err := values.NewDecimal(percentText, int32(scaleFloat), values.RoundingExactRequired)
	if err != nil {
		return rules.PromotionApprovalInput{}, fmt.Errorf("rulethreshold: frozen increase percent: %w", err)
	}
	in := rules.PromotionApprovalInput{
		IncreasePercent: percent,
		BandPosition:    rules.BandPosition(bandText),
		BudgetAuthority: rules.BudgetAuthority(authorityText),
		GradeChange:     gradeChange,
	}
	if err := in.Validate(); err != nil {
		return rules.PromotionApprovalInput{}, fmt.Errorf("rulethreshold: frozen inputs invalid: %w", err)
	}
	return in, nil
}

// Executor is the statements a threshold decision needs: one blind insert
// plus one read-back. Both dbport.Tx and runtime.Executor satisfy it, so
// the in-advance recorder and the test fixtures share the store without an
// adapter.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
	QueryRow(ctx context.Context, sql string, args ...any) dbport.Row
}

// Record freezes one threshold decision. Recording the identical decision
// twice is a no-op; a different decision for the same key is ErrConflict.
func Record(ctx context.Context, ex Executor, d Decision) error {
	if d.TenantID == uuid.Nil || d.IntentID == uuid.Nil || d.InstanceID == uuid.Nil {
		return fmt.Errorf("rulethreshold: tenant, intent and instance are required")
	}
	if d.Attempt < 1 {
		return fmt.Errorf("rulethreshold: attempt must be at least 1")
	}
	for _, req := range []struct{ field, value string }{
		{"tier", d.Tier}, {"matched_row", d.MatchedRow}, {"table_id", d.TableID},
		{"table_version", d.TableVersion}, {"table_digest", d.TableDigest}, {"input_digest", d.InputDigest},
	} {
		if req.value == "" {
			return fmt.Errorf("rulethreshold: %s is required", req.field)
		}
	}
	payload, err := inputsPayload(d.Input)
	if err != nil {
		return fmt.Errorf("rulethreshold: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("rulethreshold: encode frozen inputs: %w", err)
	}
	recorded := d.RecordedAt.UTC()
	if recorded.IsZero() {
		recorded = time.Now().UTC()
	}
	if _, err := ex.Exec(ctx, `INSERT INTO promotion_threshold_decision
		(tenant_id, intent_id, revision, attempt, instance_id, tier, matched_row, table_id, table_version, table_digest, input_digest, inputs, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (tenant_id, intent_id, revision, attempt) DO NOTHING`,
		d.TenantID, d.IntentID, d.Revision, d.Attempt, d.InstanceID,
		d.Tier, d.MatchedRow, d.TableID, d.TableVersion, d.TableDigest, d.InputDigest, body, recorded); err != nil {
		return fmt.Errorf("rulethreshold: record threshold decision: %w", err)
	}
	var stored struct {
		digest string
		body   []byte
	}
	if err := ex.QueryRow(ctx, `SELECT input_digest, inputs FROM promotion_threshold_decision
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3 AND attempt = $4`,
		d.TenantID, d.IntentID, d.Revision, d.Attempt).Scan(&stored.digest, &stored.body); err != nil {
		return fmt.Errorf("rulethreshold: read back threshold decision: %w", err)
	}
	if stored.digest != d.InputDigest {
		return fmt.Errorf("%w: revision %d attempt %d already froze %s", ErrConflict, d.Revision, d.Attempt, stored.digest)
	}
	return nil
}

// Latest returns the highest-attempt decision frozen for a revision, or
// false when the revision never ran the threshold node.
func Latest(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID, revision uint64) (Decision, bool, error) {
	var d Decision
	var body []byte
	err := ex.QueryRow(ctx, `SELECT intent_id, revision, attempt, instance_id, tier, matched_row,
		table_id, table_version, table_digest, input_digest, inputs, recorded_at
		FROM promotion_threshold_decision
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3
		ORDER BY attempt DESC LIMIT 1`, tenantID, intentID, revision).Scan(
		&d.IntentID, &d.Revision, &d.Attempt, &d.InstanceID, &d.Tier, &d.MatchedRow,
		&d.TableID, &d.TableVersion, &d.TableDigest, &d.InputDigest, &body, &d.RecordedAt)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Decision{}, false, nil
		}
		return Decision{}, false, fmt.Errorf("rulethreshold: load threshold decision: %w", err)
	}
	d.TenantID = tenantID
	in, err := parseInputs(body)
	if err != nil {
		return Decision{}, false, err
	}
	d.Input = in
	return d, true, nil
}
