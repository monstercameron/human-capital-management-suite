package agentrollout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// VersionCandidate pins the actual installation and its independent grant at preview.
type VersionCandidate struct {
	InstallationID    string `json:"installation_id"`
	ConversationID    string `json:"conversation_id"`
	Version           int64  `json:"version"`
	Revision          int64  `json:"revision"`
	RevocationEpoch   int64  `json:"revocation_epoch"`
	AuthorityRevision uint64 `json:"authority_revision"`
	PolicyDigest      string `json:"policy_digest"`
}

// VersionRequest selects exact installations; it never accepts a standing selector.
type VersionRequest struct {
	ID            string             `json:"id"`
	TenantID      string             `json:"tenant_id"`
	AgentID       string             `json:"agent_id"`
	Version       int64              `json:"version"`
	ProfileDigest string             `json:"profile_digest"`
	EvaluationRef string             `json:"evaluation_ref"`
	ReviewRef     string             `json:"review_ref"`
	BatchLimit    int                `json:"batch_limit"`
	CanaryIDs     []string           `json:"canary_ids"`
	Candidates    []VersionCandidate `json:"candidates"`
}

// VersionPlan is an immutable desired version image. Progress lives separately.
type VersionPlan struct {
	VersionRequest
	Digest      string `json:"digest"`
	CanaryCount int    `json:"canary_count"`
}

// PreviewVersion canonicalizes an explicit staged upgrade or rollback scope.
func PreviewVersion(r VersionRequest) (VersionPlan, error) {
	if strings.TrimSpace(r.ID) == "" || r.TenantID == "" || r.AgentID == "" || r.Version <= 0 || r.ProfileDigest == "" || r.EvaluationRef == "" || r.ReviewRef == "" || r.BatchLimit < 1 || r.BatchLimit > 100 || len(r.Candidates) == 0 || len(r.Candidates) > 1000 || len(r.CanaryIDs) == 0 || !uniqueNonempty(r.CanaryIDs) {
		return VersionPlan{}, ErrInvalid
	}
	r.Candidates = append([]VersionCandidate(nil), r.Candidates...)
	r.CanaryIDs = append([]string(nil), r.CanaryIDs...)
	sort.Strings(r.CanaryIDs)
	seen := map[string]bool{}
	for _, c := range r.Candidates {
		if c.InstallationID == "" || seen[c.InstallationID] || c.ConversationID == "" || c.Version <= 0 || c.Revision <= 0 || c.RevocationEpoch <= 0 || c.AuthorityRevision == 0 || c.PolicyDigest == "" {
			return VersionPlan{}, ErrInvalid
		}
		seen[c.InstallationID] = true
	}
	for _, id := range r.CanaryIDs {
		if !seen[id] {
			return VersionPlan{}, ErrInvalid
		}
	}
	sort.Slice(r.Candidates, func(i, j int) bool {
		a, b := contains(r.CanaryIDs, r.Candidates[i].InstallationID), contains(r.CanaryIDs, r.Candidates[j].InstallationID)
		if a != b {
			return a
		}
		return r.Candidates[i].InstallationID < r.Candidates[j].InstallationID
	})
	raw, err := json.Marshal(r)
	if err != nil {
		return VersionPlan{}, err
	}
	sum := sha256.Sum256(raw)
	return VersionPlan{VersionRequest: r, Digest: "sha256:" + hex.EncodeToString(sum[:]), CanaryCount: len(r.CanaryIDs)}, nil
}

// Verify rejects changes to candidate scope, target evidence, or installation epochs.
func (p VersionPlan) Verify() error {
	expected, err := PreviewVersion(p.VersionRequest)
	if err != nil {
		return err
	}
	if expected.Digest != p.Digest || expected.CanaryCount != p.CanaryCount {
		return ErrPreviewStale
	}
	return nil
}
