package productquery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// ALIGN-021: a product response projects the semantic actions currently
// available against it — nothing more. Viewing is available only for rows
// the envelope actually discloses with readable values; a stale or
// unverifiable projection offers exactly one action, an authoritative
// refetch, and an empty response offers none. The projection is derived
// from the validated envelope, so it cannot grant what authorization did
// not disclose and cannot survive tampering with the envelope.

// Semantic action identifiers. The set is closed: surfaces may not invent
// actions, and every action names the response digest it was projected
// from so a replayed action set is detectable.
const (
	ActionViewRow = "product.row.view"
	ActionRefetch = "product.response.refetch"
)

// SemanticAction is one currently available action. Subject is set only
// for row views; a refetch names the projection instead of any subject.
type SemanticAction struct {
	ID             string `json:"id"`
	Subject        string `json:"subject,omitempty"`
	Projection     string `json:"projection,omitempty"`
	SourceSequence uint64 `json:"source_sequence,omitempty"`
	Watermark      uint64 `json:"watermark,omitempty"`
	EnvelopeDigest string `json:"envelope_digest"`
}

// ActionSet is the closed set of actions projected from one envelope.
type ActionSet struct {
	Tenant    string           `json:"tenant"`
	Freshness Freshness        `json:"freshness"`
	Actions   []SemanticAction `json:"actions"`
	Envelope  string           `json:"envelope_digest"`
	Digest    string           `json:"digest"`
}

// AvailableActions projects the currently available semantic actions from
// env at now. The envelope is validated and its freshness recomputed
// first: a tampered envelope or a freshness label that does not match the
// projection yields no actions and an error. Views require a CURRENT
// projection and at least one allowed field per row; anything else offers
// only a refetch, and a response with no disclosable rows offers nothing.
func AvailableActions(env Envelope, now time.Time) (ActionSet, error) {
	if err := VerifyFreshness(env, now); err != nil {
		return ActionSet{}, err
	}
	digest := env.Digest()
	set := ActionSet{Tenant: env.Tenant.String(), Freshness: env.Freshness, Envelope: digest}
	if env.Freshness != FreshnessCurrent {
		set.Actions = []SemanticAction{{
			ID:             ActionRefetch,
			Projection:     env.Projection.Name,
			SourceSequence: env.Projection.SourceSequence,
			Watermark:      env.Projection.Watermark,
			EnvelopeDigest: digest,
		}}
		set.Digest = set.computeDigest()
		return set, nil
	}
	for _, row := range env.Rows {
		viewable := false
		for _, field := range row.Fields {
			if field.Disposition == authz.EffectAllow && field.State == ValuePresent {
				viewable = true
				break
			}
		}
		if !viewable {
			continue
		}
		set.Actions = append(set.Actions, SemanticAction{
			ID:             ActionViewRow,
			Subject:        row.Subject.String(),
			EnvelopeDigest: digest,
		})
	}
	sort.Slice(set.Actions, func(i, j int) bool {
		if set.Actions[i].ID != set.Actions[j].ID {
			return set.Actions[i].ID < set.Actions[j].ID
		}
		return set.Actions[i].Subject < set.Actions[j].Subject
	})
	if set.Actions == nil {
		set.Actions = []SemanticAction{}
	}
	set.Digest = set.computeDigest()
	return set, nil
}

func (s ActionSet) computeDigest() string {
	b, err := json.Marshal(struct {
		Tenant    string           `json:"tenant"`
		Freshness Freshness        `json:"freshness"`
		Actions   []SemanticAction `json:"actions"`
		Envelope  string           `json:"envelope_digest"`
	}{s.Tenant, s.Freshness, s.Actions, s.Envelope})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// VerifyActions reports whether set is internally consistent: its digest
// matches its content and every action names the set envelope.
func (s ActionSet) VerifyActions() error {
	if s.Digest == "" || s.Digest != s.computeDigest() {
		return fmt.Errorf("productquery: action set digest does not match its content")
	}
	for _, action := range s.Actions {
		if action.EnvelopeDigest != s.Envelope || action.EnvelopeDigest == "" {
			return fmt.Errorf("productquery: action %q is not bound to this envelope", action.ID)
		}
		switch action.ID {
		case ActionViewRow:
			if action.Subject == "" {
				return fmt.Errorf("productquery: row view names no subject")
			}
		case ActionRefetch:
			if action.Projection == "" {
				return fmt.Errorf("productquery: refetch names no projection")
			}
		default:
			return fmt.Errorf("productquery: unknown semantic action %q", action.ID)
		}
	}
	return nil
}
