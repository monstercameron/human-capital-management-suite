// Pre-write P1B authority evidence: NEXT-009 compiles the amendment
// that authorizes the first bounded write from complete current
// evidence, before rather than after the write pilot.
//
// Every pre-write criterion maps to its test command, fixture, exact
// oracle, result digest, owner, retention, expiry and sign-off. Missing,
// stale, circular or post-pilot proof, self-certifying evidence, open
// critical assurance findings and unowned rollback/repair/incident paths
// return GATE_BLOCKED with zero write authority. Success grants only
// the selected tenant/intent/capability/field/provider/time scope.
package authority

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PreWriteDecision is the closed amendment outcome.
type PreWriteDecision string

// The pre-write outcomes.
const (
	PreWriteGranted     PreWriteDecision = "GRANTED"
	PreWriteGateBlocked PreWriteDecision = "GATE_BLOCKED"
)

// PreWriteCriterion maps one pre-write gate to its current proof.
type PreWriteCriterion struct {
	ID           string
	TestCommand  string
	Fixture      string
	Oracle       string
	ResultDigest string
	Owner        string
	Retention    string
	Expiry       time.Time
	SignOff      string
	EvidenceAt   time.Time
	// PostPilot marks proof that only exists after the write pilot.
	// Pre-write authority never rests on it.
	PostPilot bool
}

// PreWriteScope is the granted write boundary on success.
type PreWriteScope struct {
	Tenant     string
	Intent     string
	Capability string
	Fields     []string
	Provider   string
	Window     string
}

// PreWriteInput is the complete amendment envelope.
type PreWriteInput struct {
	Amendment   Amendment
	Criteria    []PreWriteCriterion
	Scope       PreWriteScope
	Owner       string
	RollbackRef string
	RepairRef   string
	IncidentRef string
	// CriticalFindings are open critical assurance findings; any blocks.
	CriticalFindings []string
	EvaluatedAt      time.Time
}

// PreWriteFinding names one gate defect.
type PreWriteFinding struct {
	Code   string
	Detail string
}

// PreWriteVerdict is the deterministic amendment receipt.
type PreWriteVerdict struct {
	AmendmentID string
	Decision    PreWriteDecision
	Scope       PreWriteScope
	// WriteAuthority is empty on grant by construction: the scope above
	// is the entire grant, and the field exists so reviewers can assert
	// that a blocked verdict carries zero write authority.
	WriteAuthority string
	Findings       []PreWriteFinding
	Digest         string
}

// CompilePreWrite evaluates one pre-write evidence amendment.
func CompilePreWrite(in PreWriteInput) (PreWriteVerdict, error) {
	if strings.TrimSpace(in.Amendment.AmendmentID) == "" {
		return PreWriteVerdict{}, errors.New("authority: bound amendment is required")
	}
	if err := in.Amendment.VerifyDigest(); err != nil {
		return PreWriteVerdict{}, err
	}
	if in.EvaluatedAt.IsZero() {
		return PreWriteVerdict{}, errors.New("authority: evaluation time is required")
	}
	if err := in.Amendment.Active(in.EvaluatedAt); err != nil {
		return PreWriteVerdict{}, err
	}
	if strings.TrimSpace(in.Owner) == "" {
		return PreWriteVerdict{}, errors.New("authority: amendment owner is required")
	}
	verdict := PreWriteVerdict{AmendmentID: in.Amendment.AmendmentID, Decision: PreWriteGranted, Scope: in.Scope}
	block := func(code, detail string) {
		verdict.Decision = PreWriteGateBlocked
		verdict.Findings = append(verdict.Findings, PreWriteFinding{Code: code, Detail: detail})
	}
	if len(in.Criteria) == 0 {
		block("CRITERIA_MISSING", "at least one pre-write criterion is required")
	}
	seen := map[string]bool{}
	for _, criterion := range in.Criteria {
		id := strings.TrimSpace(criterion.ID)
		if id == "" || seen[id] {
			block("CRITERION_INVALID", "criterion ids must be unique and non-empty")
			continue
		}
		seen[id] = true
		for _, field := range []struct{ name, value string }{
			{"test command", criterion.TestCommand}, {"fixture", criterion.Fixture},
			{"oracle", criterion.Oracle}, {"result digest", criterion.ResultDigest},
			{"owner", criterion.Owner}, {"retention", criterion.Retention},
			{"sign-off", criterion.SignOff},
		} {
			if strings.TrimSpace(field.value) == "" {
				block("CRITERION_INCOMPLETE", fmt.Sprintf("criterion %s lacks %s", id, field.name))
			}
		}
		if criterion.Expiry.IsZero() || !criterion.Expiry.After(in.EvaluatedAt) {
			block("CRITERION_STALE", fmt.Sprintf("criterion %s proof is expired at evaluation", id))
		}
		if criterion.EvidenceAt.After(in.EvaluatedAt) {
			block("CRITERION_FUTURE", fmt.Sprintf("criterion %s relies on future proof", id))
		}
		if criterion.PostPilot {
			block("CRITERION_POST_PILOT", fmt.Sprintf("criterion %s rests on post-pilot proof", id))
		}
		if criterion.SignOff == criterion.Owner {
			block("CRITERION_SELF_CERTIFIED", fmt.Sprintf("criterion %s is self-certified", id))
		}
		if criterion.Owner == in.Owner {
			block("CRITERION_CIRCULAR", fmt.Sprintf("criterion %s owner certifies its own amendment", id))
		}
	}
	for _, finding := range in.CriticalFindings {
		block("ASSURANCE_FINDING_OPEN", "open critical assurance finding: "+finding)
	}
	for _, path := range []struct{ name, value string }{
		{"rollback", in.RollbackRef}, {"repair", in.RepairRef}, {"incident", in.IncidentRef},
	} {
		if strings.TrimSpace(path.value) == "" {
			block("RESPONSE_PATH_UNOWNED", path.name+" path is unowned")
		}
	}
	scope := in.Scope
	if strings.TrimSpace(scope.Tenant) == "" || strings.TrimSpace(scope.Intent) == "" ||
		strings.TrimSpace(scope.Capability) == "" || len(scope.Fields) == 0 ||
		strings.TrimSpace(scope.Provider) == "" || strings.TrimSpace(scope.Window) == "" {
		block("SCOPE_INCOMPLETE", "grant scope must name tenant, intent, capability, fields, provider and window")
	}
	if values.TenantId(scope.Tenant).Validate() != nil {
		block("SCOPE_INVALID", "grant scope tenant is invalid")
	}
	if scope.Tenant != string(in.Amendment.Tenant) {
		block("SCOPE_UNBOUND", "grant scope tenant is outside the bound amendment")
	}
	boundFields := map[string]bool{}
	for _, field := range in.Amendment.Fields {
		boundFields[field] = true
	}
	for _, field := range scope.Fields {
		if !boundFields[field] {
			block("SCOPE_UNBOUND", fmt.Sprintf("grant scope field %s is outside the bound amendment", field))
		}
	}
	sort.Slice(verdict.Findings, func(i, j int) bool {
		if verdict.Findings[i].Code != verdict.Findings[j].Code {
			return verdict.Findings[i].Code < verdict.Findings[j].Code
		}
		return verdict.Findings[i].Detail < verdict.Findings[j].Detail
	})
	verdict.Digest = digestPreWrite(verdict, in)
	return verdict, nil
}

func digestPreWrite(verdict PreWriteVerdict, in PreWriteInput) string {
	parts := []string{"next009-prewrite", verdict.AmendmentID, string(verdict.Decision),
		in.Scope.Tenant, in.Scope.Intent, in.Scope.Capability, in.Scope.Provider, in.Scope.Window,
		in.Owner, in.RollbackRef, in.RepairRef, in.IncidentRef}
	fields := append([]string(nil), in.Scope.Fields...)
	sort.Strings(fields)
	parts = append(parts, fields...)
	criteria := append([]PreWriteCriterion(nil), in.Criteria...)
	sort.Slice(criteria, func(i, j int) bool { return criteria[i].ID < criteria[j].ID })
	for _, criterion := range criteria {
		parts = append(parts, strings.Join([]string{
			criterion.ID, criterion.TestCommand, criterion.Fixture, criterion.Oracle,
			criterion.ResultDigest, criterion.Owner, criterion.Retention,
			criterion.Expiry.UTC().String(), criterion.SignOff, criterion.EvidenceAt.UTC().String(),
			fmt.Sprint(criterion.PostPilot),
		}, "\x00"))
	}
	for _, finding := range verdict.Findings {
		parts = append(parts, finding.Code+"\x00"+finding.Detail)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
