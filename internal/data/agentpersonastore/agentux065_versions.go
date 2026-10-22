// Sealed semantic labels on persona publications (AGENTUX-065).
//
// Persona versions are strictly immutable, so the label seals onto the
// publication event: the author's bump choice names the next label at
// publish time. A sealed label never moves: republishing the same version
// keeps it, rewriting it is refused, and a new version must advance past
// the persona's greatest label. Existing publications were labelled once
// by migration 00053 as n.0.0.
package agentpersonastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

// ErrVersionLabel refuses a publication whose semantic label is unparsable,
// moves backwards, or rewrites a sealed one.
var ErrVersionLabel = errors.New("agentpersonastore: invalid publication version label")

// PublishedLabel returns the sealed semantic label of one published
// version, or ErrNotFound when the version never published.
func (s *TenantStore) PublishedLabel(ctx context.Context, personaID string, version int64) (string, error) {
	if strings.TrimSpace(personaID) == "" || version <= 0 {
		return "", fmt.Errorf("%w: persona id and positive version are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var label string
	err = tx.QueryRow(ctx, `SELECT semantic_version FROM persona_lifecycle_events WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 AND to_state='PUBLISHED' AND semantic_version<>'' ORDER BY event_sequence ASC LIMIT 1`, s.tenantID, personaID, version).Scan(&label)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", fmt.Errorf("%w: publication label for %s/%d", ErrNotFound, personaID, version)
	}
	if err != nil {
		return "", err
	}
	return label, commit(ctx, tx)
}

// GreatestPublishedLabel returns the greatest sealed label across a
// persona's publications, or "" when nothing published yet. Unparsable
// labels are ignored: they predate the sealed rule and never block it.
func (s *TenantStore) GreatestPublishedLabel(ctx context.Context, personaID string) (string, error) {
	if strings.TrimSpace(personaID) == "" {
		return "", fmt.Errorf("%w: persona id is required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	greatest, err := greatestLabelTx(ctx, tx, s.tenantID, personaID)
	if err != nil {
		return "", err
	}
	if greatest == nil {
		return "", commit(ctx, tx)
	}
	return greatest.String(), commit(ctx, tx)
}

func greatestLabelTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, personaID string) (*semver.Version, error) {
	greatest, _, err := baselineTx(ctx, tx, tenantID, personaID)
	return greatest, err
}

// baselineTx returns the greatest sealed label and the version holding it:
// the baseline the next label must advance past, and the profile the next
// label's required bump is measured from. Ties keep the higher version.
func baselineTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, personaID string) (*semver.Version, int64, error) {
	rows, err := tx.Query(ctx, `SELECT persona_version, semantic_version FROM persona_lifecycle_events WHERE tenant_id=$1 AND persona_id=$2 AND to_state='PUBLISHED' AND semantic_version<>''`, tenantID, personaID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var greatest *semver.Version
	var baseline int64
	for rows.Next() {
		var n int64
		var raw string
		if err := rows.Scan(&n, &raw); err != nil {
			return nil, 0, fmt.Errorf("agentpersonastore: scan publication label: %w", err)
		}
		parsed, err := semver.Parse(raw)
		if err != nil {
			continue
		}
		if greatest == nil || greatest.Less(parsed) || (greatest.Compare(parsed) == 0 && n > baseline) {
			greatest = &parsed
			baseline = n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return greatest, baseline, nil
}

// resolvePublicationLabel seals the label one publication persists. A
// sealed version keeps its label; a new label must parse, advance past the
// greatest, and reach at least the bump the change from the baseline
// profile requires; an empty label computes exactly that floor.
func resolvePublicationLabelTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, personaID string, version int64, profile json.RawMessage, requested string) (string, error) {
	var sealed string
	err := tx.QueryRow(ctx, `SELECT semantic_version FROM persona_lifecycle_events WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 AND to_state='PUBLISHED' AND semantic_version<>'' ORDER BY event_sequence ASC LIMIT 1`, tenantID, personaID, version).Scan(&sealed)
	switch {
	case err == nil:
		if requested != "" && requested != sealed {
			return "", fmt.Errorf("%w: publication of %s/%d is sealed as %s", ErrVersionLabel, personaID, version, sealed)
		}
		return sealed, nil
	case !errors.Is(err, dbport.ErrNoRows):
		return "", fmt.Errorf("agentpersonastore: read sealed publication label: %w", err)
	}
	greatest, baseline, err := baselineTx(ctx, tx, tenantID, personaID)
	if err != nil {
		return "", err
	}
	if greatest == nil {
		if strings.TrimSpace(requested) == "" {
			return semver.FirstVersion().String(), nil
		}
		parsed, err := semver.Parse(requested)
		if err != nil {
			return "", fmt.Errorf("%w: %q is not a semantic label: %v", ErrVersionLabel, requested, err)
		}
		return parsed.String(), nil
	}
	required, err := requiredBumpTx(ctx, tx, tenantID, personaID, baseline, profile)
	if err != nil {
		return "", err
	}
	floor := greatest.Next(required)
	if strings.TrimSpace(requested) == "" {
		return floor.String(), nil
	}
	parsed, err := semver.Parse(requested)
	if err != nil {
		return "", fmt.Errorf("%w: %q is not a semantic label: %v", ErrVersionLabel, requested, err)
	}
	if parsed.Compare(*greatest) <= 0 {
		return "", fmt.Errorf("%w: %q does not advance past %s", ErrVersionLabel, requested, greatest.String())
	}
	if parsed.Compare(floor) < 0 {
		return "", fmt.Errorf("%w: %q is smaller than the change from %s requires", ErrVersionLabel, requested, greatest.String())
	}
	return parsed.String(), nil
}

// requiredBumpTx measures the smallest label the change from the baseline
// profile may publish under. A baseline lost to a partial restore keeps a
// patch floor so publication stays possible; a baseline that cannot be
// read as a profile fails closed.
func requiredBumpTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, personaID string, baseline int64, profile json.RawMessage) (semver.Bump, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT profile FROM persona_versions WHERE tenant_id=$1 AND persona_id=$2 AND version=$3`, tenantID, personaID, baseline).Scan(&raw); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return semver.BumpPatch, nil
		}
		return semver.BumpPatch, fmt.Errorf("agentpersonastore: read baseline profile: %w", err)
	}
	var prev, next agentpersona.PersonaProfile
	if err := json.Unmarshal(raw, &prev); err != nil {
		return semver.BumpPatch, fmt.Errorf("%w: baseline profile is not readable", ErrVersionLabel)
	}
	if err := json.Unmarshal(profile, &next); err != nil {
		return semver.BumpPatch, fmt.Errorf("%w: published profile is not readable", ErrVersionLabel)
	}
	return agentpersona.RequiredBump(prev, next), nil
}
