// Outgoing link validation for HUB-020: before an official scope deploys,
// every canonical link of the version must resolve in current policy — the
// target exists, the pinned version exists and is not retired, and the
// deploying principal can read the target. Placement scopes surface the
// same findings as warnings instead of refusals.
package documenthubstore

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Link finding codes. LinkMalformed reuses the extraction state: a stored
// malformed link is a finding, never a pass.
const (
	LinkUnknownTarget  = "unknown-target"
	LinkUnknownVersion = "unknown-version"
	LinkRetiredTarget  = "retired-target"
	LinkInaccessible   = "inaccessible"
)

// Finding severities: official scopes refuse on errors, placement scopes
// surface everything as warnings.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// ErrLinksUnresolved is the sentinel for refused official deploys;
// errors.As for *LinksUnresolved carries the blocking findings.
var ErrLinksUnresolved = errors.New("document links: targets do not resolve")

// LinkFinding is one unresolvable outgoing link.
type LinkFinding struct {
	Label, TargetDocID, PinnedVersion, Block, Code, Severity, Detail string
}

// LinkReport is the full validation outcome for one version and scope.
type LinkReport struct {
	Findings []LinkFinding
}

// LinksUnresolved carries the blocking findings of a refused deploy.
type LinksUnresolved struct {
	Findings []LinkFinding
}

func (e *LinksUnresolved) Error() string {
	return "document links: targets do not resolve in current policy"
}

func (e *LinksUnresolved) Unwrap() error { return ErrLinksUnresolved }

// ValidateLinks checks every stored link of a version as one principal in
// one scope. Official scopes mark failures as errors; placement scopes
// mark the same failures as warnings.
func (s *Store) ValidateLinks(ctx context.Context, tenantID, docID, versionID, scopeKind, scopeID, subjectKind, subjectID string) (LinkReport, error) {
	var report LinkReport
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		findings, err := validateLinksTx(ctx, tx, tenantID, docID, versionID, subjectKind, subjectID)
		if err != nil {
			return err
		}
		if scopeKind != "default" {
			for i := range findings {
				findings[i].Severity = SeverityWarning
			}
		}
		report = LinkReport{Findings: findings}
		return nil
	})
	if err != nil {
		return LinkReport{}, err
	}
	return report, nil
}

func validateLinksTx(ctx context.Context, tx dbport.Tx, tenantID, docID, versionID, subjectKind, subjectID string) ([]LinkFinding, error) {
	rows, err := tx.Query(ctx, `SELECT label,target_document_id,pinned_version_id,block_id,state FROM document_link WHERE tenant_id=$1 AND source_document_id=$2 AND source_version_id=$3 ORDER BY label,target_document_id`, tenantID, docID, versionID)
	if err != nil {
		return nil, err
	}
	type stored struct{ label, target, pinned, block, state string }
	var links []stored
	for rows.Next() {
		var l stored
		if err := rows.Scan(&l.label, &l.target, &l.pinned, &l.block, &l.state); err != nil {
			rows.Close()
			return nil, err
		}
		links = append(links, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	var findings []LinkFinding
	for _, l := range links {
		finding := LinkFinding{Label: l.label, TargetDocID: l.target, PinnedVersion: l.pinned, Block: l.block, Severity: SeverityError}
		if l.state != LinkValid {
			finding.Code, finding.Detail = LinkMalformed, "link target is not a canonical document reference"
			findings = append(findings, finding)
			continue
		}
		var target int
		if err := tx.QueryRow(ctx, `SELECT 1 FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, l.target).Scan(&target); err != nil {
			finding.Code, finding.Detail = LinkUnknownTarget, "target document does not exist"
			findings = append(findings, finding)
			continue
		}
		if l.pinned != "" {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM document_version WHERE tenant_id=$1 AND id=$2`, tenantID, l.pinned).Scan(&status); err != nil {
				finding.Code, finding.Detail = LinkUnknownVersion, "pinned version does not exist"
				findings = append(findings, finding)
				continue
			}
			if status == "retired" {
				finding.Code, finding.Detail = LinkRetiredTarget, "pinned version is retired"
				findings = append(findings, finding)
				continue
			}
		}
		if err := authorizeTx(ctx, tx, tenantID, l.target, subjectKind, subjectID, ActionRead); err != nil {
			finding.Code, finding.Detail = LinkInaccessible, "target is not readable in current policy"
			findings = append(findings, finding)
			continue
		}
	}
	return findings, nil
}

// requireLinksResolved refuses an official deploy whose version carries
// error-severity findings, naming them for the fix.
func requireLinksResolved(ctx context.Context, tx dbport.Tx, tenantID, docID, versionID, scopeKind, subjectKind, subjectID string) error {
	if scopeKind != "default" {
		return nil
	}
	findings, err := validateLinksTx(ctx, tx, tenantID, docID, versionID, subjectKind, subjectID)
	if err != nil {
		return err
	}
	if len(findings) > 0 {
		return &LinksUnresolved{Findings: findings}
	}
	return nil
}
